package server

import (
	"log/slog"

	"github.com/kael777/agentforge/backend/api/internal/config"
	"github.com/kael777/agentforge/backend/api/internal/modules/auth"
	"github.com/kael777/agentforge/backend/api/internal/modules/health"
	"github.com/kael777/agentforge/backend/api/internal/modules/todo"
	"github.com/kael777/agentforge/backend/api/internal/platform/metrics"
	"github.com/kael777/agentforge/backend/api/internal/transport/httpx"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

// Dependencies is the composition root input for both listeners.
type Dependencies struct {
	Config        config.Config
	Logger        *slog.Logger
	Health        *health.Handler
	Auth          *auth.Handler
	Todo          *todo.Handler
	JWTMiddleware fiber.Handler
	Metrics       *metrics.Metrics
}

// NewPublic builds the internet-facing application. Only routes a customer or
// client legitimately needs are registered here; anything an operator uses for
// diagnosis lives on the management listener instead.
func NewPublic(deps Dependencies) *fiber.App {
	app := newApp(deps, false)

	app.Use(cors.New(cors.Config{
		AllowOrigins: deps.Config.HTTP.AllowedOrigins,
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID"},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	}))

	app.Get("/healthz", deps.Health.Liveness)

	api := app.Group("/api/v1")
	api.Post("/auth/register", authRateLimit(deps.Config.HTTP.AuthRateLimit, deps.Logger), deps.Auth.Register)
	api.Post("/auth/login", authRateLimit(deps.Config.HTTP.AuthRateLimit, deps.Logger), deps.Auth.Login)
	api.Get("/users/me", deps.JWTMiddleware, deps.Auth.Me)
	api.Post("/todos", deps.JWTMiddleware, deps.Todo.Create)
	api.Get("/todos/:id", deps.JWTMiddleware, deps.Todo.Get)

	return app
}

// NewManagement builds the operator-facing application. It is expected to be
// bound to a loopback or private interface only.
func NewManagement(deps Dependencies) *fiber.App {
	app := newApp(deps, true)

	// Go's HTTP handler is the canonical way to serve the exposition format, so
	// the Prometheus dependency keeps its own semantics instead of being
	// reimplemented against Fiber.
	app.Get("/metrics", adaptor.HTTPHandler(deps.Metrics.Handler()))
	app.Get("/version", deps.Health.Version)
	app.Get("/readyz", deps.Health.Readiness)

	return app
}

func newApp(deps Dependencies, management bool) *fiber.App {
	bodyLimit := deps.Config.HTTP.BodyLimit
	if management {
		// Nothing on the management surface needs a request body.
		bodyLimit = 4 * 1024
	}

	app := fiber.New(fiber.Config{
		ReadTimeout:  deps.Config.HTTP.ReadTimeout,
		WriteTimeout: deps.Config.HTTP.WriteTimeout,
		IdleTimeout:  deps.Config.HTTP.IdleTimeout,
		BodyLimit:    bodyLimit,
		ErrorHandler: httpx.ErrorHandler(deps.Logger),
		// X-Forwarded-For is honoured only when the peer is a configured proxy.
		// Trusting it unconditionally would let any client pick its own source
		// address and walk around the per-IP rate limit.
		TrustProxy: len(deps.Config.HTTP.TrustedProxies) > 0,
		TrustProxyConfig: fiber.TrustProxyConfig{
			Proxies: deps.Config.HTTP.TrustedProxies,
		},
	})

	app.Use(requestid.New())
	app.Use(recoverer.New())
	app.Use(hardenResponse())
	if deps.Metrics != nil {
		app.Use(deps.Metrics.Middleware())
	}
	app.Use(accessLog(deps.Logger))

	return app
}
