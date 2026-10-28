package realtime

import (
	"log/slog"

	"github.com/kael777/agentforge/backend/api/internal/modules/auth"

	"github.com/gofiber/fiber/v3"
)

// Handler exposes ticket issuance. The route sits behind the normal bearer
// token middleware, so a caller can only obtain a ticket for itself.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Issue responds with a short-lived ticket. Nothing about the connection is
// decided here: the realtime service re-checks origin, signature and scope.
func (h *Handler) Issue(c fiber.Ctx) error {
	userID, ok := auth.UserID(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	ticket, err := h.service.Issue(userID)
	if err != nil {
		h.logger.Error("ws ticket issue failed", "error", err)
		return fiber.ErrInternalServerError
	}

	// Tickets are credentials in transit: never let a proxy or browser cache
	// the response.
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.JSON(ticket)
}
