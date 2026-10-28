package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// Route patterns, not raw paths, must label the series. Otherwise every user
// identifier creates a new time series and the metrics endpoint becomes the
// most expensive thing the service runs.
func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	m := New(nil)
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/items/:id", func(c fiber.Ctx) error { return c.SendString("ok") })

	for _, path := range []string{"/items/1", "/items/2"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
	}

	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}

	exposition := string(body)
	want := `http_requests_total{method="GET",route="/items/:id",status="200"} 2`
	if !strings.Contains(exposition, want) {
		t.Fatalf("expected %q in exposition, got:\n%s", want, exposition)
	}
	if strings.Contains(exposition, "/items/1") {
		t.Fatal("raw path leaked into the metric label, which would explode cardinality")
	}
}
