package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func capture(t *testing.T, log func(*slog.Logger)) string {
	t.Helper()
	var buffer bytes.Buffer
	logger := NewWithHandler(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log(logger)
	return buffer.String()
}

func TestSensitiveAttributesAreRedacted(t *testing.T) {
	output := capture(t, func(logger *slog.Logger) {
		logger.Info("user login",
			"user_id", "user-1",
			"authorization", "Bearer super-secret-token",
			"password", "hunter2",
			"refresh_token", "refresh-value",
			"database_dsn", "agentforge:hunter2@tcp(127.0.0.1:3306)/agentforge",
		)
	})

	for _, leaked := range []string{"super-secret-token", "hunter2", "refresh-value"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("log output leaked %q: %s", leaked, output)
		}
	}
	if !strings.Contains(output, "user-1") {
		t.Fatalf("non-sensitive fields must survive: %s", output)
	}
	if !strings.Contains(output, redactedValue) {
		t.Fatalf("expected a redaction marker: %s", output)
	}
}

func TestErrorsAndMessagesAreScrubbed(t *testing.T) {
	err := errors.New("dial agentforge:s3cr3t-pass@tcp(127.0.0.1:3306)/agentforge: connection refused")

	output := capture(t, func(logger *slog.Logger) {
		logger.Error("database error", "error", err)
		logger.Info("incoming " + "Bearer abcdefghijklmnop")
	})

	if strings.Contains(output, "s3cr3t-pass") {
		t.Fatalf("log output leaked a DSN password: %s", output)
	}
	if strings.Contains(output, "abcdefghijklmnop") {
		t.Fatalf("log output leaked a bearer token: %s", output)
	}
	if !strings.Contains(output, "127.0.0.1:3306") {
		t.Fatalf("scrubbing must keep the diagnosable host: %s", output)
	}
}

func TestNestedGroupsAreRedacted(t *testing.T) {
	output := capture(t, func(logger *slog.Logger) {
		logger.Info("request", slog.Group("http",
			slog.String("path", "/api/v1/auth/login"),
			slog.String("cookie", "session=abc123"),
		))
	})

	if strings.Contains(output, "abc123") {
		t.Fatalf("group attribute leaked a cookie: %s", output)
	}

	// The remaining structure must still be valid JSON.
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("log line is not valid JSON: %v (%s)", err, output)
	}
}

func TestScrubKeepsOrdinaryValues(t *testing.T) {
	cases := map[string]string{
		"request completed in 12ms": "request completed in 12ms",
		"user_id=user-1":            "user_id=user-1",
	}
	for input, want := range cases {
		if got := Scrub(input); got != want {
			t.Fatalf("Scrub(%q) = %q, want %q", input, got, want)
		}
	}
}
