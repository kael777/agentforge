package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON logger whose every record passes through redaction.
// Callers get the hardened handler by default, so forgetting to opt in is not
// possible.
func New(level, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     parseLevel(level),
		AddSource: strings.EqualFold(strings.TrimSpace(environment), "development"),
	})
	return NewWithHandler(handler)
}

// NewWithHandler wraps an arbitrary handler with the redaction layer. Tests use
// it to capture output; production reaches it through New.
func NewWithHandler(handler slog.Handler) *slog.Logger {
	return slog.New(newRedactingHandler(handler))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
