package auth

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newProtectedApp() *fiber.App {
	app := fiber.New()
	app.Get("/private", NewJWTMiddleware(MiddlewareConfig{
		Secret:   testSecret,
		Issuer:   testIssuer,
		Audience: testAudience,
		Logger:   testLogger(),
	}), func(c fiber.Ctx) error {
		userID, ok := UserID(c)
		if !ok {
			return fiber.ErrUnauthorized
		}
		return c.SendString(userID)
	})
	return app
}

func mintToken(t *testing.T, method jwt.SigningMethod, key any, mutate func(*claims)) string {
	t.Helper()
	now := time.Now()
	payload := &claims{
		Email: "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    testIssuer,
			Audience:  jwt.ClaimStrings{testAudience},
			ID:        "token-1",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	if mutate != nil {
		mutate(payload)
	}
	signed, err := jwt.NewWithClaims(method, payload).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func requestWithToken(t *testing.T, token string) (int, string) {
	t.Helper()
	request := httptest.NewRequest("GET", "/private", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := newProtectedApp().Test(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body := make([]byte, 64)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, string(body[:n])
}

func TestMiddlewareAcceptsValidToken(t *testing.T) {
	status, body := requestWithToken(t, mintToken(t, jwt.SigningMethodHS256, []byte(testSecret), nil))
	if status != fiber.StatusOK || body != "user-1" {
		t.Fatalf("expected 200 with subject, got %d %q", status, body)
	}
}

func TestMiddlewareRejectsInvalidTokens(t *testing.T) {
	tests := map[string]string{
		"missing header":  "",
		"wrong audience":  mintToken(t, jwt.SigningMethodHS256, []byte(testSecret), func(c *claims) { c.Audience = jwt.ClaimStrings{"other-service"} }),
		"wrong issuer":    mintToken(t, jwt.SigningMethodHS256, []byte(testSecret), func(c *claims) { c.Issuer = "other-issuer" }),
		"expired":         mintToken(t, jwt.SigningMethodHS256, []byte(testSecret), func(c *claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }),
		"missing subject": mintToken(t, jwt.SigningMethodHS256, []byte(testSecret), func(c *claims) { c.Subject = "" }),
		"wrong secret":    mintToken(t, jwt.SigningMethodHS256, []byte("another-secret-another-secret-123"), nil),
		"unexpected alg":  mintToken(t, jwt.SigningMethodHS512, []byte(testSecret), nil),
		"no expiry claim": mintToken(t, jwt.SigningMethodHS256, []byte(testSecret), func(c *claims) { c.ExpiresAt = nil }),
	}

	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			status, _ := requestWithToken(t, token)
			if status != fiber.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", status)
			}
		})
	}
}
