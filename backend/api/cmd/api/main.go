package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	listenErr := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "address", cfg.HTTP.Address)
		listenErr <- application.HTTP.Listen(cfg.HTTP.Address)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-listenErr:
		if err != nil {
			return err
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := application.HTTP.ShutdownWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	select {
	case err := <-listenErr:
		return err
	case <-time.After(time.Second):
		return nil
	}
}
