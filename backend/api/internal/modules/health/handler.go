package health

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Pinger is the narrow database surface readiness needs. Keeping it an
// interface means the probe cannot accidentally run a query.
type Pinger interface {
	PingContext(context.Context) error
}

// BuildInfo is deployment metadata. It is only ever served on the management
// listener, never from the public one.
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

// Liveness answers only "is this process able to reply". It must not touch
// dependencies and must not disclose version, commit, environment or uptime:
// that information belongs on the management listener.
func (h *Handler) Liveness(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// Readiness proves the dependencies a request actually needs are usable.
// It is routed on the management listener so the public edge cannot reach it.
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

// Version reports build metadata to operators on the management listener.
func (h *Handler) Version(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"service":    h.build.Service,
		"version":    h.build.Version,
		"commit":     h.build.Commit,
		"build_time": h.build.BuildTime,
		"env":        h.build.Env,
		"uptime_sec": int64(time.Since(h.startedAt).Seconds()),
	})
}
