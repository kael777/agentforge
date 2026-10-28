package server

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// startListener runs a Fiber app on an ephemeral loopback port. Unlike
// app.Test, this goes through the real server implementation, which is the
// only way to observe headers the HTTP layer adds by itself.
func startListener(t *testing.T, app *fiber.App) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = app.Listener(listener) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return "http://" + listener.Addr().String()
}

func fetch(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp, string(body)
}

// This is the acceptance test for the public surface: minimum disclosure on
// /healthz, no framework fingerprint, and no operator endpoints.
func TestLivePublicSurface(t *testing.T) {
	base := startListener(t, NewPublic(testDependencies()))

	resp, body := fetch(t, base+"/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d", resp.StatusCode)
	}
	if strings.TrimSpace(body) != `{"status":"ok"}` {
		t.Fatalf("/healthz body = %q, want exactly {\"status\":\"ok\"}", body)
	}
	if got := resp.Header.Get("Server"); got != "" {
		t.Fatalf("Server header leaked: %q", got)
	}
	if got := resp.Header.Get("X-Powered-By"); got != "" {
		t.Fatalf("X-Powered-By header leaked: %q", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}

	for _, path := range []string{"/readyz", "/metrics", "/version"} {
		operatorResp, _ := fetch(t, base+path)
		if operatorResp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s must not be reachable on the public listener, got %d", path, operatorResp.StatusCode)
		}
	}
}

func TestLiveManagementSurface(t *testing.T) {
	base := startListener(t, NewManagement(testDependencies()))

	versionResp, versionBody := fetch(t, base+"/version")
	if versionResp.StatusCode != http.StatusOK {
		t.Fatalf("/version status = %d", versionResp.StatusCode)
	}
	if !strings.Contains(versionBody, "service") {
		t.Fatalf("/version body = %q", versionBody)
	}

	metricsResp, metricsBody := fetch(t, base+"/metrics")
	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status = %d", metricsResp.StatusCode)
	}
	for _, series := range []string{"http_requests_total", "http_request_duration_seconds", "http_inflight_requests"} {
		if !strings.Contains(metricsBody, series) {
			t.Fatalf("/metrics is missing %s", series)
		}
	}

	// A handler without a database is by definition not ready.
	readyResp, _ := fetch(t, base+"/readyz")
	if readyResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503", readyResp.StatusCode)
	}
}
