package realtime

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret   = "ticket-secret-0123456789012345678901"
	testIssuer   = "agentforge-api"
	testAudience = "agentforge-ws"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := NewService(Config{
		Secret:   testSecret,
		Issuer:   testIssuer,
		Audience: testAudience,
		TTL:      30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestIssueProducesScopedShortLivedTicket(t *testing.T) {
	service := newTestService(t)
	ticket, err := service.Issue("user-1")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if ticket.ExpiresIn != 30 {
		t.Fatalf("expires_in = %d, want 30", ticket.ExpiresIn)
	}

	parsed := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(ticket.Ticket, parsed, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("ticket is not verifiable: %v", err)
	}
	if parsed.Subject != "user-1" {
		t.Fatalf("subject = %q", parsed.Subject)
	}
	if parsed.Issuer != testIssuer {
		t.Fatalf("issuer = %q", parsed.Issuer)
	}
	if len(parsed.Audience) != 1 || parsed.Audience[0] != testAudience {
		t.Fatalf("audience = %#v", parsed.Audience)
	}
	if parsed.ID == "" {
		t.Fatal("ticket has no id, so it cannot be made single-use")
	}
	lifetime := parsed.ExpiresAt.Sub(parsed.IssuedAt.Time)
	if lifetime > 30*time.Second {
		t.Fatalf("lifetime = %s, want at most 30s", lifetime)
	}
}

func TestIssueRejectsEmptySubject(t *testing.T) {
	service := newTestService(t)
	if _, err := service.Issue("  "); err == nil {
		t.Fatal("expected an error for an empty subject")
	}
}

func TestNewServiceRejectsUnsafeConfig(t *testing.T) {
	cases := map[string]Config{
		"short secret":    {Secret: "too-short", Issuer: testIssuer, Audience: testAudience, TTL: time.Second},
		"missing issuer":  {Secret: testSecret, Issuer: "", Audience: testAudience, TTL: time.Second},
		"missing audience": {Secret: testSecret, Issuer: testIssuer, Audience: "", TTL: time.Second},
		"zero ttl":        {Secret: testSecret, Issuer: testIssuer, Audience: testAudience, TTL: 0},
		"long ttl":        {Secret: testSecret, Issuer: testIssuer, Audience: testAudience, TTL: 10 * time.Minute},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewService(cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
