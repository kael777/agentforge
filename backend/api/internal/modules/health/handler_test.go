package health

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func TestLivenessDoesNotDependOnDatabase(t *testing.T) {
	app := fiber.New()
	handler := NewHandler(nil, BuildInfo{Service: "test", Version: "v1"})
	app.Get("/healthz", handler.Liveness)

	resp, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestReadinessReflectsDatabaseState(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "ready", status: fiber.StatusOK},
		{name: "not ready", err: errors.New("down"), status: fiber.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			handler := NewHandler(fakePinger{err: tt.err}, BuildInfo{})
			app.Get("/readyz", handler.Readiness)

			resp, err := app.Test(httptest.NewRequest("GET", "/readyz", nil))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Fatalf("expected %d, got %d", tt.status, resp.StatusCode)
			}
		})
	}
}
