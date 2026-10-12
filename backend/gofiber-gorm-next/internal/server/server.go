package server

import (
	"log/slog"
	"time"

	"gofiber-gorm-next/internal/config"
	"gofiber-gorm-next/internal/modules/auth"
	"gofiber-gorm-next/internal/modules/health"
	"gofiber-gorm-next/internal/modules/todo"
	"gofiber-gorm-next/internal/transport/httpx"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

type Dependencies struct {
	Config        config.Config
	Logger        *slog.Logger
	Health        *health.Handler
	Auth          *auth.Handler
	Todo          *todo.Handler
	JWTMiddleware fiber.Handler
}

func New(deps Dependencies) *fiber.App {
	app := fiber.New(fiber.Config{
		ReadTimeout:  deps.Config.HTTP.ReadTimeout,
		WriteTimeout: deps.Config.HTTP.WriteTimeout,
		IdleTimeout:  deps.Config.HTTP.IdleTimeout,
		BodyLimit:    deps.Config.HTTP.BodyLimit,
		ErrorHandler: httpx.ErrorHandler(deps.Logger),
	})

	app.Use(requestid.New())
	app.Use(recoverer.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: deps.Config.HTTP.AllowedOrigins,
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID"},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	}))
	app.Use(accessLog(deps.Logger))

	app.Get("/healthz", deps.Health.Liveness)
	app.Get("/readyz", deps.Health.Readiness)

	api := app.Group("/api/v1")
	api.Post("/auth/register", deps.Auth.Register)
	api.Post("/auth/login", deps.Auth.Login)
	api.Get("/users/me", deps.JWTMiddleware, deps.Auth.Me)
	api.Post("/todos", deps.JWTMiddleware, deps.Todo.Create)
	api.Get("/todos/:id", deps.JWTMiddleware, deps.Todo.Get)

	return app
}

func accessLog(logger *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		started := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		if fiberErr, ok := err.(*fiber.Error); ok {
			status = fiberErr.Code
		}
		logger.Info("http request",
			"request_id", requestid.FromContext(c),
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
		return err
	}
}
