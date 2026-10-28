package logging

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// redactedValue is the only text a sensitive attribute is ever allowed to
// become. Keeping it a constant makes both review and testing mechanical.
const redactedValue = "[REDACTED]"

// sensitiveKeySuffixes catch attribute names that end with a secret-ish word,
// such as "refresh_token" or "db_password". Matching on the whole name would
// miss the many ways callers name the same field.
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

// sensitiveKeys is the exact-match list. It exists because a suffix match is
// deliberately not broad enough to catch names such as "authorization" or
// "session", which do not end with a sensitive suffix.
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
	// bearerPattern scrubs "Authorization: Bearer <token>" style fragments that
	// end up inside free-form strings such as upstream error messages.
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)

	// jwtPattern matches a compact JWS: three base64url segments.
	jwtPattern = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`)

	// dsnPattern matches a MySQL/Go style DSN and keeps the scheme readable so
	// the log still says which dependency broke.
	dsnPattern = regexp.MustCompile(`(?i)\b([a-z0-9_]+):([^\s@/]+)@tcp\(([^)]*)\)`)

	// passwordPairPattern catches "password=..." / "password: ..." fragments.
	passwordPairPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret)(\s*[=:]\s*)\S+`)
)

// newRedactingHandler wraps an existing handler. Redaction happens on the way
// in, so no unrelated handler or writer can bypass it.
func newRedactingHandler(inner slog.Handler) slog.Handler {
	return &redactingHandler{inner: inner}
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
		// Structured values (errors, fmt.Stringer, maps) can carry a DSN or a
		// token. Flattening them to a scrubbed string is the only way to be
		// sure nothing slips through an encoder.
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

// Scrub removes credential-shaped fragments from a free-form string. It is
// exported so callers that build their own log lines can reuse the same rules.
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
