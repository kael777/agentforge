package todo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	items map[string]*Todo
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{items: make(map[string]*Todo)}
}

func (f *fakeRepository) Create(_ context.Context, item *Todo) error {
	copy := *item
	f.items[item.ID] = &copy
	return nil
}

func (f *fakeRepository) FindByID(_ context.Context, userID, id string) (*Todo, error) {
	item, ok := f.items[id]
	if !ok || item.UserID != userID {
		return nil, ErrNotFound
	}
	copy := *item
	return &copy, nil
}

func TestCreateAndGet(t *testing.T) {
	repository := newFakeRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	fixedTime := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }
	userID := uuid.NewString()

	created, err := service.Create(context.Background(), userID, "  learn Fiber  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Title != "learn Fiber" || created.Completed || created.CreatedAt != fixedTime {
		t.Fatalf("unexpected created todo: %#v", created)
	}

	found, err := service.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found != created {
		t.Fatalf("Get() = %#v, want %#v", found, created)
	}
}

func TestCreateRejectsInvalidTitle(t *testing.T) {
	service, _ := NewService(newFakeRepository())
	_, err := service.Create(context.Background(), uuid.NewString(), "   ")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestGetDoesNotExposeAnotherUsersTodo(t *testing.T) {
	service, _ := NewService(newFakeRepository())
	ownerID := uuid.NewString()
	created, _ := service.Create(context.Background(), ownerID, "private")

	_, err := service.Get(context.Background(), uuid.NewString(), created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
