package app

import (
	"errors"
	"log/slog"

	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/buildinfo"
	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/config"
	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/modules/auth"
	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/modules/health"
	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/modules/todo"
	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/platform/database"
	"github.com/kael777/agentforge/backend/gofiber-gorm-next/internal/server"

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
