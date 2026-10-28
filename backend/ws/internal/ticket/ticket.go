package ticket

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Verification failures. The distinction matters only for metrics and logs; the
// client always receives a generic 401.
var (
	ErrMalformed = errors.New("malformed ticket")
	ErrExpired   = errors.New("expired ticket")
	ErrReplayed  = errors.New("ticket already used")
	ErrScope     = errors.New("ticket scope mismatch")
	ErrAlgorithm = errors.New("unexpected ticket signing method")
)

// Claims is the verified content of a ticket.
type Claims struct {
	Subject   string
	ID        string
	ExpiresAt time.Time
}

// Config describes how tickets are verified.
type Config struct {
	Secret   string
	Issuer   string
	Audience string
	// MaxLifetime rejects tickets that claim to live longer than this. The API
	// mints 30 second tickets; anything far beyond that is not a ticket this
	// service should honour.
	MaxLifetime time.Duration
}

type rawClaims struct {
	jwt.RegisteredClaims
}

// Verifier checks signatures and enforces single use.
//
// The replay cache is in-process. A horizontally scaled deployment needs a
// shared store (Redis or the API itself) for the cache to stay sound.
type Verifier struct {
	secret      []byte
	issuer      string
	audience    string
	maxLifetime time.Duration
	now         func() time.Time

	mu     sync.Mutex
	used   map[string]time.Time
	lastGC time.Time
}

const (
	replayRetention = 5 * time.Minute
	gcInterval      = time.Minute
)

func NewVerifier(cfg Config) (*Verifier, error) {
	if len(cfg.Secret) < 32 {
		return nil, errors.New("ticket secret must be at least 32 characters")
	}
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, errors.New("ticket issuer is required")
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, errors.New("ticket audience is required")
	}
	if cfg.MaxLifetime <= 0 {
		return nil, errors.New("ticket max lifetime must be positive")
	}
	return &Verifier{
		secret:      []byte(cfg.Secret),
		issuer:      cfg.Issuer,
		audience:    cfg.Audience,
		maxLifetime: cfg.MaxLifetime,
		now:         time.Now,
		used:        make(map[string]time.Time),
	}, nil
}

// Verify validates a ticket and atomically marks it used. A second call with
// the same ticket fails with ErrReplayed even if the first one was milliseconds
// earlier.
func (v *Verifier) Verify(raw string) (Claims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Claims{}, ErrMalformed
	}

	parsed := &rawClaims{}
	token, err := jwt.ParseWithClaims(raw, parsed, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, ErrAlgorithm
		}
		return v.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return Claims{}, ErrExpired
		case errors.Is(err, ErrAlgorithm):
			return Claims{}, ErrAlgorithm
		case errors.Is(err, jwt.ErrTokenInvalidAudience), errors.Is(err, jwt.ErrTokenInvalidIssuer):
			return Claims{}, ErrScope
		default:
			return Claims{}, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
	}
	if !token.Valid {
		return Claims{}, ErrMalformed
	}
	if parsed.Subject == "" || parsed.ID == "" {
		return Claims{}, ErrScope
	}
	if parsed.IssuedAt == nil || parsed.ExpiresAt == nil {
		return Claims{}, ErrScope
	}
	if parsed.ExpiresAt.Sub(parsed.IssuedAt.Time) > v.maxLifetime {
		return Claims{}, fmt.Errorf("%w: ticket lifetime exceeds %s", ErrScope, v.maxLifetime)
	}
	if !parsed.ExpiresAt.After(v.now()) {
		return Claims{}, ErrExpired
	}

	if err := v.consume(parsed.ID); err != nil {
		return Claims{}, err
	}
	return Claims{
		Subject:   parsed.Subject,
		ID:        parsed.ID,
		ExpiresAt: parsed.ExpiresAt.Time,
	}, nil
}

func (v *Verifier) consume(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.now()
	if _, seen := v.used[id]; seen {
		return ErrReplayed
	}
	if now.Sub(v.lastGC) > gcInterval {
		for key, at := range v.used {
			if now.Sub(at) > replayRetention {
				delete(v.used, key)
			}
		}
		v.lastGC = now
	}
	v.used[id] = now
	return nil
}

// Tracked reports how many ticket IDs are currently remembered. Tests and
// operators use it to confirm the cache is bounded.
func (v *Verifier) Tracked() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.used)
}
