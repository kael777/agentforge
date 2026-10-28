package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/kael777/agentforge/backend/ws/internal/config"
	"github.com/kael777/agentforge/backend/ws/internal/platform/logging"
	"github.com/kael777/agentforge/backend/ws/internal/realtime"
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

	logger := logging.New(cfg.LogLevel, cfg.Environment)
	slog.SetDefault(logger)

	server, err := realtime.New(cfg, logger)
	if err != nil {
		return err
	}

	// ReadTimeout and WriteTimeout are deliberately left unset: they apply to
	// the underlying connection and would cut a healthy WebSocket mid-session.
	// ReadHeaderTimeout still bounds the handshake, and every WebSocket read and
	// write carries its own deadline.
	publicServer := &http.Server{
		Handler:           server.PublicHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// The management listener is plain request/response, so normal timeouts
	// apply and a stuck scrape cannot hold a connection forever.
	managementServer := &http.Server{
		Handler:           server.ManagementHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	publicListener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Address, err)
	}
	managementListener, err := net.Listen("tcp", cfg.ManagementAddress)
	if err != nil {
		_ = publicListener.Close()
		return fmt.Errorf("listen on management address %s: %w", cfg.ManagementAddress, err)
	}

	logger.Info("servers starting",
		"public_address", cfg.Address,
		"management_address", cfg.ManagementAddress,
		"environment", cfg.Environment,
		"allowed_origins", cfg.AllowedOrigins,
	)

	serverErr := make(chan error, 2)
	go func() { serverErr <- publicServer.Serve(publicListener) }()
	go func() { serverErr <- managementServer.Serve(managementListener) }()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Order matters. Stop accepting first, then tell live connections to go
	// away, then wait for them to finish their close handshake. A load balancer
	// that removes this instance from the pool gives clients time to reconnect
	// elsewhere instead of receiving a TCP reset.
	var shutdownErr error
	if err := publicServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown public listener: %w", err))
	}

	server.Hub().CloseAll(websocket.StatusGoingAway, "server restart")
	if err := server.Hub().Wait(shutdownCtx); err != nil {
		logger.Warn("websocket drain did not finish in time", "error", err)
	}

	if err := managementServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown management listener: %w", err))
	}
	return shutdownErr
}
