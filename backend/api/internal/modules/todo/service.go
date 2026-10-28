package todo

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrInvalidInput = errors.New("invalid todo input")

type Service struct {
	repository Repository
	now        func() time.Time
}

type View struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("todo repository is required")
	}
	return &Service{repository: repository, now: time.Now}, nil
}

func (s *Service) Create(ctx context.Context, userID, title string) (View, error) {
	title = strings.TrimSpace(title)
	if _, err := uuid.Parse(userID); err != nil || title == "" || utf8.RuneCountInString(title) > 200 {
		return View{}, ErrInvalidInput
	}

	now := s.now().UTC()
	item := &Todo{
		ID:        uuid.NewString(),
		UserID:    userID,
		Title:     title,
		Completed: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.Create(ctx, item); err != nil {
		return View{}, err
	}
	return toView(item), nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (View, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return View{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(id); err != nil {
		return View{}, ErrInvalidInput
	}

	item, err := s.repository.FindByID(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	return toView(item), nil
}

func toView(item *Todo) View {
	return View{
		ID:        item.ID,
		Title:     item.Title,
		Completed: item.Completed,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}
