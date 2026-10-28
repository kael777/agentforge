package realtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Config describes how the API signs WebSocket tickets. The secret must be the
// same value the realtime service verifies with, and it must be different from
// the access token secret: a leaked ticket secret then cannot mint access
// tokens, and the reverse.
type Config struct {
	Secret   string
	Issuer   string
	Audience string
	TTL      time.Duration
}

// Ticket is the response body of POST /api/v1/ws/ticket.
type Ticket struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int    `json:"expires_in"`
}

type Service struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	if len(cfg.Secret) < 32 {
		return nil, errors.New("WS ticket secret must be at least 32 characters")
	}
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, errors.New("WS ticket issuer is required")
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, errors.New("WS ticket audience is required")
	}
	if cfg.TTL <= 0 || cfg.TTL > 2*time.Minute {
		return nil, errors.New("WS ticket TTL must be between 1s and 2m")
	}
	return &Service{
		secret:   []byte(cfg.Secret),
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		ttl:      cfg.TTL,
		now:      time.Now,
	}, nil
}

// Issue mints a single-use ticket. The realtime service is the one that
// enforces single use; this side only keeps the lifetime short and the scope
// narrow (one audience, one subject).
func (s *Service) Issue(userID string) (Ticket, error) {
	if strings.TrimSpace(userID) == "" {
		return Ticket{}, errors.New("user id is required")
	}

	now := s.now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    s.issuer,
		Audience:  jwt.ClaimStrings{s.audience},
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	})
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return Ticket{}, fmt.Errorf("sign ws ticket: %w", err)
	}
	return Ticket{Ticket: signed, ExpiresIn: int(s.ttl.Seconds())}, nil
}
