package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func getJSON(t *testing.T, app *fiber.App, path string) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", path, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return resp.StatusCode, decoded
}

// The public liveness probe must disclose nothing beyond "the process answers".
// An attacker should not be able to learn the version, commit, environment or
// uptime of the service from it.
func TestLivenessExposesStatusOnly(t *testing.T) {
	app := fiber.New()
	app.Get("/healthz", NewHandler(nil, BuildInfo{
		Service: "agentforge-api", Version: "v9.9.9", Commit: "deadbeef",
		BuildTime: "2026-10-28T00:00:00Z", Env: "production",
	}).Liveness)

	status, body := getJSON(t, app, "/healthz")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("liveness body must be exactly {\"status\":\"ok\"}, got %#v", body)
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
			app.Get("/readyz", NewHandler(fakePinger{err: tt.err}, BuildInfo{}).Readiness)

			status, body := getJSON(t, app, "/readyz")
			if status != tt.status {
				t.Fatalf("expected %d, got %d", tt.status, status)
			}
			if _, ok := body["checks"]; !ok {
				t.Fatalf("readiness response has no checks: %#v", body)
			}
		})
	}
}

func TestVersionIsServedForOperators(t *testing.T) {
	app := fiber.New()
	app.Get("/version", NewHandler(nil, BuildInfo{
		Service: "agentforge-api", Version: "v1.2.3", Commit: "abc123",
		BuildTime: "2026-10-28T00:00:00Z", Env: "production",
	}).Version)

	status, body := getJSON(t, app, "/version")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if body["version"] != "v1.2.3" || body["commit"] != "abc123" {
		t.Fatalf("unexpected version payload: %#v", body)
	}
}
