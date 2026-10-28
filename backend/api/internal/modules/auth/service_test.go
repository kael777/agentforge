package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret   = "01234567890123456789012345678901"
	testIssuer   = "agentforge-api"
	testAudience = "agentforge-web"
)

type fakeUserRepository struct {
	users map[string]*User
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[string]*User)}
}

func (f *fakeUserRepository) Create(_ context.Context, user *User) error {
	if _, exists := f.users[user.Email]; exists {
		return ErrEmailTaken
	}
	copy := *user
	f.users[user.Email] = &copy
	return nil
}

func (f *fakeUserRepository) FindByEmail(_ context.Context, email string) (*User, error) {
	user, exists := f.users[email]
	if !exists {
		return nil, ErrUserNotFound
	}
	copy := *user
	return &copy, nil
}

func newTestService(t *testing.T, repo UserRepository) *Service {
	t.Helper()
	service, err := NewService(repo, testSecret, testIssuer, testAudience, time.Hour)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestRegisterAndLogin(t *testing.T) {
	repo := newFakeUserRepository()
	service := newTestService(t, repo)

	registered, err := service.Register(context.Background(), " USER@example.com ", "password123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.User.Email != "user@example.com" || registered.Token == "" {
		t.Fatalf("unexpected registration result: %#v", registered)
	}
	if repo.users["user@example.com"].PasswordHash == "password123" {
		t.Fatal("password was not hashed")
	}

	loggedIn, err := service.Login(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.User.ID != registered.User.ID || loggedIn.Token == "" {
		t.Fatalf("unexpected login result: %#v", loggedIn)
	}
}

func TestIssuedTokenCarriesScopeClaims(t *testing.T) {
	service := newTestService(t, newFakeUserRepository())
	result, err := service.Register(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	parsed := &claims{}
	token, err := jwt.ParseWithClaims(result.Token, parsed, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("token is not verifiable: %v", err)
	}
	if parsed.Issuer != testIssuer {
		t.Fatalf("issuer = %q, want %q", parsed.Issuer, testIssuer)
	}
	if len(parsed.Audience) != 1 || parsed.Audience[0] != testAudience {
		t.Fatalf("audience = %#v, want [%q]", parsed.Audience, testAudience)
	}
	if parsed.ID == "" {
		t.Fatal("token has no JWT ID, so it cannot be revoked individually")
	}
	if parsed.Subject == "" {
		t.Fatal("token has no subject")
	}
}

func TestLoginRejectsInvalidPassword(t *testing.T) {
	service := newTestService(t, newFakeUserRepository())
	_, _ = service.Register(context.Background(), "user@example.com", "password123")

	_, err := service.Login(context.Background(), "user@example.com", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginHidesUnknownAccounts(t *testing.T) {
	service := newTestService(t, newFakeUserRepository())

	for _, email := range []string{"missing@example.com", "not-an-email"} {
		_, err := service.Login(context.Background(), email, "password123")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: expected ErrInvalidCredentials, got %v", email, err)
		}
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	service := newTestService(t, newFakeUserRepository())
	_, _ = service.Register(context.Background(), "user@example.com", "password123")

	_, err := service.Register(context.Background(), "user@example.com", "password456")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestNewServiceRejectsIncompleteScope(t *testing.T) {
	cases := map[string]struct {
		issuer   string
		audience string
	}{
		"missing issuer":   {issuer: "", audience: testAudience},
		"missing audience": {issuer: testIssuer, audience: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewService(newFakeUserRepository(), testSecret, tc.issuer, tc.audience, time.Hour); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
