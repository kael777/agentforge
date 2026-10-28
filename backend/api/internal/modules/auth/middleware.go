package auth

import (
	"log/slog"

	jwtware "github.com/gofiber/contrib/v3/jwt"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
)

func NewJWTMiddleware(secret string, logger *slog.Logger) fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey: jwtware.SigningKey{Key: []byte(secret)},
		Extractor:  extractors.FromAuthHeader("Bearer"),
		ErrorHandler: func(c fiber.Ctx, err error) error {
			logger.Debug("jwt rejected", "error", err)
			return fiber.ErrUnauthorized
		},
	})
}
