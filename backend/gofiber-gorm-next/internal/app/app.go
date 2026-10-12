package app

import (
	"errors"
	"log/slog"

	"gofiber-gorm-next/internal/buildinfo"
	"gofiber-gorm-next/internal/config"
	"gofiber-gorm-next/internal/modules/auth"
	"gofiber-gorm-next/internal/modules/health"
	"gofiber-gorm-next/internal/modules/todo"
	"gofiber-gorm-next/internal/platform/database"
	"gofiber-gorm-next/internal/server"

	"github.com/gofiber/fiber/v3"
)

type Application struct {
	HTTP *fiber.App
}

func New(cfg config.Config, logger *slog.Logger, db *database.Connection) (*Application, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if db == nil || db.GORM == nil || db.SQL == nil {
		return nil, errors.New("database connection is required")
	}

	userRepository := auth.NewGormUserRepository(db.GORM)
	authService, err := auth.NewService(userRepository, cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	if err != nil {
		return nil, err
	}
	authHandler := auth.NewHandler(authService, logger)
	todoService, err := todo.NewService(todo.NewGormRepository(db.GORM))
	if err != nil {
		return nil, err
	}
	todoHandler := todo.NewHandler(todoService, logger)
	healthHandler := health.NewHandler(db.SQL, health.BuildInfo{
		Service:   cfg.ServiceName,
		Version:   buildinfo.Version,
		Commit:    buildinfo.Commit,
		BuildTime: buildinfo.BuildTime,
		Env:       cfg.AppEnv,
	})

	httpApp := server.New(server.Dependencies{
		Config:        cfg,
		Logger:        logger,
		Health:        healthHandler,
		Auth:          authHandler,
		Todo:          todoHandler,
		JWTMiddleware: auth.NewJWTMiddleware(cfg.Auth.JWTSecret, logger),
	})
	return &Application{HTTP: httpApp}, nil
}
