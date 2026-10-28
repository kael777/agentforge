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

type Service struct {
	users     UserRepository
	jwtSecret []byte
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

type tokenClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func NewService(users UserRepository, jwtSecret string, tokenTTL time.Duration) (*Service, error) {
	if users == nil {
		return nil, errors.New("user repository is required")
	}
	if len(jwtSecret) < 32 {
		return nil, errors.New("JWT secret must be at least 32 characters")
	}
	if tokenTTL <= 0 {
		return nil, errors.New("token TTL must be positive")
	}
	return &Service{users: users, jwtSecret: []byte(jwtSecret), tokenTTL: tokenTTL, now: time.Now}, nil
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
		return AuthResult{}, ErrInvalidCredentials
	}
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
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
	claims := tokenClaims{
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
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
