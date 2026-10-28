package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/kael777/agentforge/backend/api/internal/app"
	"github.com/kael777/agentforge/backend/api/internal/config"
	"github.com/kael777/agentforge/backend/api/internal/platform/database"
	"github.com/kael777/agentforge/backend/api/internal/platform/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.LogLevel, cfg.AppEnv)
	slog.SetDefault(logger)

	db, err := database.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()

	application, err := app.New(cfg, logger, db)
	if err != nil {
		return err
	}

	// Listeners are created before the goroutines start so a bind failure is a
	// startup error, not a background surprise after the service reports ready.
	publicListener, err := net.Listen("tcp", cfg.HTTP.Address)
	if err != nil {
		return fmt.Errorf("listen on public address %s: %w", cfg.HTTP.Address, err)
	}
	managementListener, err := net.Listen("tcp", cfg.HTTP.ManagementAddress)
	if err != nil {
		_ = publicListener.Close()
		return fmt.Errorf("listen on management address %s: %w", cfg.HTTP.ManagementAddress, err)
	}

	logger.Info("servers starting",
		"public_address", cfg.HTTP.Address,
		"management_address", cfg.HTTP.ManagementAddress,
		"environment", cfg.AppEnv,
	)

	serverErr := make(chan error, 2)
	go func() { serverErr <- application.HTTP.Listener(publicListener) }()
	go func() { serverErr <- application.Management.Listener(managementListener) }()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErr:
		_ = application.HTTP.Shutdown()
		_ = application.Management.Shutdown()
		if err != nil {
			return err
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	var shutdownErr error
	if err := application.HTTP.ShutdownWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown public listener: %w", err))
	}
	if err := application.Management.ShutdownWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown management listener: %w", err))
	}
	return shutdownErr
}
