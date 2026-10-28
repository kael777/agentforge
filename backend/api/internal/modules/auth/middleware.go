package auth

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

// userClaimsLocalKey is where the verified claims are stored for downstream
// handlers. Handlers read it through UserID or ClaimsFromContext so no request
// path has to re-parse the token.
const userClaimsLocalKey = "auth.claims"

// ErrUnexpectedSigningMethod is returned when a token is not HS256. Checking
// the method before returning the key blocks algorithm-confusion attacks.
var ErrUnexpectedSigningMethod = errors.New("unexpected jwt signing method")

// MiddlewareConfig configures token verification.
type MiddlewareConfig struct {
	Secret   string
	Issuer   string
	Audience string
	Logger   *slog.Logger
}

// NewJWTMiddleware verifies signature, algorithm, issuer, audience and
// expiry before a handler runs. Every rule is explicit here rather than
// inherited from a default, because defaults are what silently change.
func NewJWTMiddleware(cfg MiddlewareConfig) fiber.Handler {
	parserOptions := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithAudience(cfg.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30 * time.Second),
	}
	secret := []byte(cfg.Secret)

	return func(c fiber.Ctx) error {
		raw, ok := bearerToken(c.Get(fiber.HeaderAuthorization))
		if !ok {
			return fiber.ErrUnauthorized
		}

		parsed := &claims{}
		token, err := jwt.ParseWithClaims(raw, parsed, func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, ErrUnexpectedSigningMethod
			}
			return secret, nil
		}, parserOptions...)
		if err != nil || !token.Valid || parsed.Subject == "" {
			// The reason is worth a debug line for the operator, but the client
			// only ever learns that authentication failed.
			cfg.Logger.Debug("rejected bearer token", "reason", err)
			return fiber.ErrUnauthorized
		}

		c.Locals(userClaimsLocalKey, parsed)
		return c.Next()
	}
}

// UserID returns the authenticated subject. The second return value is false
// when the route is not protected or the middleware did not run.
func UserID(c fiber.Ctx) (string, bool) {
	parsed, ok := ClaimsFromContext(c)
	if !ok {
		return "", false
	}
	return parsed.Subject, parsed.Subject != ""
}

// ClaimsFromContext exposes the verified claims to handlers in the same
// package.
func ClaimsFromContext(c fiber.Ctx) (*claims, bool) {
	value := c.Locals(userClaimsLocalKey)
	parsed, ok := value.(*claims)
	if !ok || parsed == nil {
		return nil, false
	}
	return parsed, true
}

func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
