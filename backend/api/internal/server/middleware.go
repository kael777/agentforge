package server

import (
	"errors"
	"log/slog"
	"time"

	"github.com/kael777/agentforge/backend/api/internal/config"
	"github.com/kael777/agentforge/backend/api/internal/transport/httpx"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

// authRateLimit throttles unauthenticated credential endpoints. Without it,
// password guessing is limited only by bandwidth.
//
// The counter is per process. A multi-instance deployment must move this to a
// shared store, otherwise the effective limit multiplies by the instance count.
func authRateLimit(cfg config.RateLimitConfig, logger *slog.Logger) fiber.Handler {
	if cfg.Max <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}
	return limiter.New(limiter.Config{
		Max:          cfg.Max,
		Expiration:   cfg.Window,
		KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
		LimitReached: func(c fiber.Ctx) error {
			logger.Warn("auth rate limit reached", "path", c.Path())
			return httpx.Error(c, fiber.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
		},
	})
}

// hardenResponse removes technology fingerprints and applies conservative
// defaults. Fingerprints are not a vulnerability on their own, but they turn
// every other weakness into a targeted attack, and they cost nothing to drop.
func hardenResponse() fiber.Handler {
	return func(c fiber.Ctx) error {
		setHardeningHeaders(c)

		err := c.Next()

		// Upstream handlers may set these again, so removal happens on the way
		// out as well as on the way in.
		stripFingerprints(c)
		return err
	}
}

func setHardeningHeaders(c fiber.Ctx) {
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderXFrameOptions, "DENY")
	c.Set(fiber.HeaderReferrerPolicy, "no-referrer")
	// API responses are per-user data. Nothing here is cacheable by a shared
	// proxy, and an intermediate cache would be a data leak.
	c.Set(fiber.HeaderCacheControl, "no-store")
	stripFingerprints(c)
}

func stripFingerprints(c fiber.Ctx) {
	header := &c.Response().Header
	for _, name := range []string{
		fiber.HeaderServer,
		"X-Powered-By",
		"X-Runtime",
		"X-AspNet-Version",
		"X-Generator",
	} {
		header.Del(name)
	}
}

// accessLog records one line per request. It intentionally logs neither the
// query string nor any header: a token, an email or a password reset code can
// all appear there, and the access log is the least protected log a service
// has.
func accessLog(logger *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		started := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) && fiberErr != nil {
			status = fiberErr.Code
		}

		logger.Info("http request",
			"request_id", requestid.FromContext(c),
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
		return err
	}
}
