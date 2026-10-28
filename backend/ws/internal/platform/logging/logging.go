package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// redactedValue is what a sensitive attribute is allowed to become.
const redactedValue = "[REDACTED]"

// This package deliberately duplicates the API service's redaction rules
// instead of importing them. The two services share a contract (ticket format,
// probe semantics), not Go code: a compromised or buggy realtime binary must
// not be able to widen what the API logs, or the other way around.

var sensitiveKeySuffixes = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"apikey",
	"credential",
	"credentials",
	"dsn",
	"cookie",
}

var sensitiveKeys = map[string]struct{}{
	"authorization":      {},
	"proxyauthorization": {},
	"cookie":             {},
	"setcookie":          {},
	"session":            {},
	"sessionid":          {},
	"sessiontoken":       {},
	"jwt":                {},
	"jwtsecret":          {},
	"ticket":             {},
	"wsticket":           {},
	"accesstoken":        {},
	"refreshtoken":       {},
	"idtoken":            {},
	"clientsecret":       {},
	"privatekey":         {},
	"databaseurl":        {},
	"connectionstring":   {},
}

var (
	bearerPattern       = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)
	jwtPattern          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`)
	dsnPattern          = regexp.MustCompile(`(?i)\b([a-z0-9_]+):([^\s@/]+)@tcp\(([^)]*)\)`)
	passwordPairPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret)(\s*[=:]\s*)\S+`)
)

// New returns a JSON logger whose every record passes through redaction.
func New(level, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     parseLevel(level),
		AddSource: strings.EqualFold(strings.TrimSpace(environment), "development"),
	})
	return NewWithHandler(handler)
}

// NewWithHandler wraps an arbitrary handler with the redaction layer.
func NewWithHandler(handler slog.Handler) *slog.Logger {
	return slog.New(&redactingHandler{inner: handler})
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

type redactingHandler struct {
	inner slog.Handler
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, Scrub(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(redactAttr(attr))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		clean = append(clean, redactAttr(attr))
	}
	return &redactingHandler{inner: h.inner.WithAttrs(clean)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(attr slog.Attr) slog.Attr {
	if attr.Key == "" {
		return attr
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		clean := make([]slog.Attr, 0, len(group))
		for _, child := range group {
			clean = append(clean, redactAttr(child))
		}
		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(clean...)}
	}
	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, redactedValue)
	}
	switch attr.Value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, Scrub(attr.Value.String()))
	case slog.KindAny:
		value := attr.Value.Any()
		if value == nil {
			return attr
		}
		return slog.String(attr.Key, Scrub(fmt.Sprint(value)))
	default:
		return attr
	}
}

func isSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	if _, ok := sensitiveKeys[normalized]; ok {
		return true
	}
	for _, suffix := range sensitiveKeySuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func normalizeKey(key string) string {
	replacer := strings.NewReplacer("-", "", "_", "", ".", "", " ", "")
	return strings.ToLower(replacer.Replace(key))
}

// Scrub removes credential-shaped fragments from a free-form string.
func Scrub(value string) string {
	if value == "" {
		return value
	}
	value = jwtPattern.ReplaceAllString(value, redactedValue)
	value = bearerPattern.ReplaceAllString(value, "${1}"+redactedValue)
	value = dsnPattern.ReplaceAllString(value, "${1}:"+redactedValue+"@tcp(${3})")
	value = passwordPairPattern.ReplaceAllString(value, "${1}${2}"+redactedValue)
	return value
}
