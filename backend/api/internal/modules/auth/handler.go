package auth

import (
	"errors"
	"log/slog"

	"github.com/kael777/agentforge/backend/api/internal/transport/httpx"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Register(c fiber.Ctx) error {
	var request credentialsRequest
	if err := c.Bind().Body(&request); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "INVALID_JSON", "request body is invalid")
	}
	result, err := h.service.Register(c.Context(), request.Email, request.Password)
	if err != nil {
		return h.writeServiceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}

func (h *Handler) Login(c fiber.Ctx) error {
	var request credentialsRequest
	if err := c.Bind().Body(&request); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "INVALID_JSON", "request body is invalid")
	}
	result, err := h.service.Login(c.Context(), request.Email, request.Password)
	if err != nil {
		return h.writeServiceError(c, err)
	}
	return c.JSON(result)
}

func (h *Handler) Me(c fiber.Ctx) error {
	parsed, ok := ClaimsFromContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}
	return c.JSON(UserView{ID: parsed.Subject, Email: parsed.Email})
}

func (h *Handler) writeServiceError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return httpx.Error(c, fiber.StatusBadRequest, "INVALID_INPUT", "email or password is invalid")
	case errors.Is(err, ErrEmailTaken):
		return httpx.Error(c, fiber.StatusConflict, "EMAIL_TAKEN", "email is already registered")
	case errors.Is(err, ErrInvalidCredentials):
		return httpx.Error(c, fiber.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is incorrect")
	default:
		h.logger.Error("auth request failed", "error", err)
		return fiber.ErrInternalServerError
	}
}
