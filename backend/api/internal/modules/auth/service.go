package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// dummyPasswordHash is compared against when the email is unknown. Without it
// a login for a missing account returns in microseconds while a real account
// takes a full bcrypt round, which turns response time into an account
// enumeration oracle.
var dummyPasswordHash = func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("agentforge-timing-equalization"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("generate dummy password hash: %v", err))
	}
	return hash
}()

type Service struct {
	users     UserRepository
	jwtSecret []byte
	issuer    string
	audience  string
	tokenTTL  time.Duration
	now       func() time.Time
}

type UserView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type AuthResult struct {
	Token string   `json:"token"`
	User  UserView `json:"user"`
}

// Claims is the token payload the service signs and the middleware accepts.
// Issuer and Audience keep tokens from one service from being replayed at
// another, and the JWT ID makes a single token individually revocable later.
type claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func NewService(users UserRepository, jwtSecret, issuer, audience string, tokenTTL time.Duration) (*Service, error) {
	if users == nil {
		return nil, errors.New("user repository is required")
	}
	if len(jwtSecret) < 32 {
		return nil, errors.New("JWT secret must be at least 32 characters")
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("JWT issuer is required")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, errors.New("JWT audience is required")
	}
	if tokenTTL <= 0 {
		return nil, errors.New("token TTL must be positive")
	}
	return &Service{
		users:     users,
		jwtSecret: []byte(jwtSecret),
		issuer:    issuer,
		audience:  audience,
		tokenTTL:  tokenTTL,
		now:       time.Now,
	}, nil
}

func (s *Service) Register(ctx context.Context, email, password string) (AuthResult, error) {
	email, err := normalizeEmail(email)
	if err != nil || len(password) < 8 || len(password) > 72 {
		return AuthResult{}, ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AuthResult{}, fmt.Errorf("hash password: %w", err)
	}
	now := s.now().UTC()
	user := &User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return AuthResult{}, err
	}
	return s.resultFor(user)
}

func (s *Service) Login(ctx context.Context, email, password string) (AuthResult, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		// Even a malformed email costs one bcrypt round, so the timing of an
		// invalid request matches the timing of a wrong password.
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return AuthResult{}, ErrInvalidCredentials
	}
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return AuthResult{}, ErrInvalidCredentials
	}
	return s.resultFor(user)
}

func (s *Service) resultFor(user *User) (AuthResult, error) {
	now := s.now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenTTL)),
		},
	})
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return AuthResult{}, fmt.Errorf("sign token: %w", err)
	}
	return AuthResult{Token: signed, User: UserView{ID: user.ID, Email: user.Email}}, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return "", ErrInvalidInput
	}
	return value, nil
}
