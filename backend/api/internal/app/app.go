package app

import (
	"errors"
	"log/slog"

	"github.com/kael777/agentforge/backend/api/internal/buildinfo"
	"github.com/kael777/agentforge/backend/api/internal/config"
	"github.com/kael777/agentforge/backend/api/internal/modules/auth"
	"github.com/kael777/agentforge/backend/api/internal/modules/health"
	"github.com/kael777/agentforge/backend/api/internal/modules/todo"
	"github.com/kael777/agentforge/backend/api/internal/platform/database"
	"github.com/kael777/agentforge/backend/api/internal/platform/metrics"
	"github.com/kael777/agentforge/backend/api/internal/server"

	"github.com/gofiber/fiber/v3"
)

// Application owns the two listeners the process exposes. They are separate
// Fiber instances on purpose: the public one can be reached from the internet,
// the management one is meant for the host.
type Application struct {
	HTTP       *fiber.App
	Management *fiber.App
}

func New(cfg config.Config, logger *slog.Logger, db *database.Connection) (*Application, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if db == nil || db.GORM == nil || db.SQL == nil {
		return nil, errors.New("database connection is required")
	}

	userRepository := auth.NewGormUserRepository(db.GORM)
	authService, err := auth.NewService(
		userRepository,
		cfg.Auth.JWTSecret,
		cfg.Auth.Issuer,
		cfg.Auth.Audience,
		cfg.Auth.TokenTTL,
	)
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

	deps := server.Dependencies{
		Config:  cfg,
		Logger:  logger,
		Health:  healthHandler,
		Auth:    authHandler,
		Todo:    todoHandler,
		Metrics: metrics.New(db.SQL),
		JWTMiddleware: auth.NewJWTMiddleware(auth.MiddlewareConfig{
			Secret:   cfg.Auth.JWTSecret,
			Issuer:   cfg.Auth.Issuer,
			Audience: cfg.Auth.Audience,
			Logger:   logger,
		}),
	}

	return &Application{
		HTTP:       server.NewPublic(deps),
		Management: server.NewManagement(deps),
	}, nil
}
