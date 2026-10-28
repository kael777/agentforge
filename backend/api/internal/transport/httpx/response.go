package httpx

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func Error(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(errorEnvelope{Error: errorBody{
		Code: code, Message: message, RequestID: requestid.FromContext(c),
	}})
}

func ErrorHandler(logger *slog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		status := fiber.StatusInternalServerError
		code := "INTERNAL_ERROR"
		message := "internal server error"

		if fiberErr, ok := err.(*fiber.Error); ok {
			status = fiberErr.Code
			message = fiberErr.Message
			switch status {
			case fiber.StatusBadRequest:
				code = "BAD_REQUEST"
			case fiber.StatusUnauthorized:
				code = "UNAUTHORIZED"
			case fiber.StatusForbidden:
				code = "FORBIDDEN"
			case fiber.StatusNotFound:
				code = "NOT_FOUND"
			default:
				if status < 500 {
					code = "REQUEST_ERROR"
				}
			}
		}

		if status >= 500 {
			logger.Error("request failed", "request_id", requestid.FromContext(c), "error", err)
		}
		return Error(c, status, code, message)
	}
}
