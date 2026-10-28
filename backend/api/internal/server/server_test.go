package server

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kael777/agentforge/backend/api/internal/config"
	"github.com/kael777/agentforge/backend/api/internal/modules/auth"
	"github.com/kael777/agentforge/backend/api/internal/modules/health"
	"github.com/kael777/agentforge/backend/api/internal/modules/realtime"
	"github.com/kael777/agentforge/backend/api/internal/modules/todo"
	"github.com/kael777/agentforge/backend/api/internal/platform/metrics"

	"github.com/gofiber/fiber/v3"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testDependencies() Dependencies {
	logger := discardLogger()
	return Dependencies{
		Config:   config.Config{},
		Logger:   logger,
		Health:   health.NewHandler(nil, health.BuildInfo{}),
		Auth:     auth.NewHandler(nil, logger),
		Todo:     todo.NewHandler(nil, logger),
		Realtime: realtime.NewHandler(nil, logger),
		Metrics:  metrics.New(nil),
		JWTMiddleware: auth.NewJWTMiddleware(auth.MiddlewareConfig{
			Secret:   "01234567890123456789012345678901",
			Issuer:   "agentforge-api",
			Audience: "agentforge-web",
			Logger:   logger,
		}),
	}
}

func routePaths(app *fiber.App) map[string]bool {
	paths := make(map[string]bool)
	for _, route := range app.GetRoutes() {
		paths[route.Path] = true
	}
	return paths
}

// Everything an operator needs for diagnosis must be unreachable from the
// public listener. This test fails if someone ever re-adds those routes there.
func TestPublicListenerDoesNotExposeManagementRoutes(t *testing.T) {
	publicRoutes := routePaths(NewPublic(testDependencies()))

	for _, forbidden := range []string{"/readyz", "/metrics", "/version"} {
		if publicRoutes[forbidden] {
			t.Fatalf("%s must not be registered on the public listener", forbidden)
		}
	}
	if !publicRoutes["/healthz"] {
		t.Fatal("/healthz must stay available on the public listener")
	}
}

func TestManagementListenerExposesDiagnostics(t *testing.T) {
	managementRoutes := routePaths(NewManagement(testDependencies()))

	for _, required := range []string{"/readyz", "/metrics", "/version"} {
		if !managementRoutes[required] {
			t.Fatalf("%s must be registered on the management listener", required)
		}
	}
}

// Fingerprints are cheap to remove and expensive to leave in place: they turn a
// generic scanner into a version-specific exploit attempt.
func TestHardeningMiddlewareRemovesFingerprints(t *testing.T) {
	app := fiber.New()
	app.Use(hardenResponse())
	app.Get("/probe", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderServer, "exposed-server/1.0")
		c.Set("X-Powered-By", "framework/9.9")
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/probe", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(fiber.HeaderServer); got != "" {
		t.Fatalf("Server header must be removed, got %q", got)
	}
	if got := resp.Header.Get("X-Powered-By"); got != "" {
		t.Fatalf("X-Powered-By header must be removed, got %q", got)
	}
	if got := resp.Header.Get(fiber.HeaderXContentTypeOptions); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestAuthRateLimitStopsRepeatedAttempts(t *testing.T) {
	app := fiber.New()
	app.Post("/login", authRateLimit(config.RateLimitConfig{Max: 2, Window: time.Minute}, discardLogger()), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	statuses := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		resp, err := app.Test(httptest.NewRequest("POST", "/login", nil))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		statuses = append(statuses, resp.StatusCode)
		resp.Body.Close()
	}

	if statuses[0] != fiber.StatusOK || statuses[1] != fiber.StatusOK {
		t.Fatalf("first two attempts must pass, got %v", statuses)
	}
	if statuses[2] != fiber.StatusTooManyRequests {
		t.Fatalf("third attempt must be rate limited, got %v", statuses)
	}
}
