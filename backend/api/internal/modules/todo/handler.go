package todo

import (
	"errors"
	"log/slog"

	"github.com/kael777/agentforge/backend/api/internal/transport/httpx"

	jwtware "github.com/gofiber/contrib/v3/jwt"
	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

type createRequest struct {
	Title string `json:"title"`
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Create(c fiber.Ctx) error {
	userID, err := currentUserID(c)
	if err != nil {
		return err
	}

	var request createRequest
	if err := c.Bind().Body(&request); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "INVALID_JSON", "request body is invalid")
	}
	result, err := h.service.Create(c.Context(), userID, request.Title)
	if err != nil {
		return h.writeServiceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}

func (h *Handler) Get(c fiber.Ctx) error {
	userID, err := currentUserID(c)
	if err != nil {
		return err
	}
	result, err := h.service.Get(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.writeServiceError(c, err)
	}
	return c.JSON(result)
}

func (h *Handler) writeServiceError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return httpx.Error(c, fiber.StatusBadRequest, "INVALID_INPUT", "todo input is invalid")
	case errors.Is(err, ErrNotFound):
		return httpx.Error(c, fiber.StatusNotFound, "TODO_NOT_FOUND", "todo was not found")
	default:
		h.logger.Error("todo request failed", "error", err)
		return fiber.ErrInternalServerError
	}
}

func currentUserID(c fiber.Ctx) (string, error) {
	token := jwtware.FromContext(c)
	if token == nil {
		return "", fiber.ErrUnauthorized
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fiber.ErrUnauthorized
	}
	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return "", fiber.ErrUnauthorized
	}
	return subject, nil
}
