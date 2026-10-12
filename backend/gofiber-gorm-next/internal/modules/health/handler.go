package health

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
)

type Pinger interface {
	PingContext(context.Context) error
}

type BuildInfo struct {
	Service   string
	Version   string
	Commit    string
	BuildTime string
	Env       string
}

type Handler struct {
	pinger    Pinger
	build     BuildInfo
	startedAt time.Time
}

func NewHandler(pinger Pinger, build BuildInfo) *Handler {
	return &Handler{pinger: pinger, build: build, startedAt: time.Now()}
}

func (h *Handler) Liveness(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":     "ok",
		"service":    h.build.Service,
		"version":    h.build.Version,
		"commit":     h.build.Commit,
		"build_time": h.build.BuildTime,
		"env":        h.build.Env,
		"uptime_sec": int64(time.Since(h.startedAt).Seconds()),
		"time":       time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) Readiness(c fiber.Ctx) error {
	if h.pinger == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "error",
			"checks": fiber.Map{"database": "not_configured"},
		})
	}

	ctx, cancel := context.WithTimeout(c.Context(), time.Second)
	defer cancel()
	if err := h.pinger.PingContext(ctx); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "error",
			"checks": fiber.Map{"database": "error"},
		})
	}

	return c.JSON(fiber.Map{
		"status": "ok",
		"checks": fiber.Map{"database": "ok"},
	})
}
