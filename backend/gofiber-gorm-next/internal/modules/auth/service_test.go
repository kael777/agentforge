package auth

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestRegisterAndLogin(t *testing.T) {
	repo := newFakeUserRepository()
	service, err := NewService(repo, "01234567890123456789012345678901", time.Hour)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

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

func TestLoginRejectsInvalidPassword(t *testing.T) {
	repo := newFakeUserRepository()
	service, _ := NewService(repo, "01234567890123456789012345678901", time.Hour)
	_, _ = service.Register(context.Background(), "user@example.com", "password123")

	_, err := service.Login(context.Background(), "user@example.com", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	repo := newFakeUserRepository()
	service, _ := NewService(repo, "01234567890123456789012345678901", time.Hour)
	_, _ = service.Register(context.Background(), "user@example.com", "password123")

	_, err := service.Register(context.Background(), "user@example.com", "password456")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}
