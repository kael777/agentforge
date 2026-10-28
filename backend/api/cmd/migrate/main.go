package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/kael777/agentforge/backend/api/internal/config"
	"github.com/kael777/agentforge/backend/api/internal/platform/database"
	"github.com/kael777/agentforge/backend/api/internal/platform/logging"
	"github.com/kael777/agentforge/backend/api/internal/platform/migrate"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
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
	defer db.Close()

	if err := migrate.Run(context.Background(), db.GORM); err != nil {
		return err
	}

	logger.Info("database migrations completed")
	return nil
}
