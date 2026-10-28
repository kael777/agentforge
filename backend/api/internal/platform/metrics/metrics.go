package metrics

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns a dedicated registry instead of the process-wide default. A
// private registry keeps tests isolated and makes it obvious which series the
// service publishes.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight prometheus.Gauge
}

// New builds the registry. A nil database is allowed so the type stays usable
// in tests that do not need a pool.
func New(database *sql.DB) *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	if database != nil {
		// Pool statistics are how a saturated connection pool becomes visible
		// before it turns into request timeouts.
		registry.MustRegister(collectors.NewDBStatsCollector(database, "primary"))
	}

	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests by method, route and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_inflight_requests",
			Help: "Number of HTTP requests currently being served.",
		}),
	}
	registry.MustRegister(m.requests, m.duration, m.inflight)
	return m
}

// Handler exposes the registry in Prometheus text format. It is mounted on the
// management listener only.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		Registry:          m.registry,
		EnableOpenMetrics: false,
	})
}

// Middleware records latency and outcome per route pattern. It uses the route
// pattern rather than the raw path so a request for /todos/<uuid> does not
// create one series per identifier.
func (m *Metrics) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		m.inflight.Inc()
		started := time.Now()
		err := c.Next()
		m.inflight.Dec()

		status := c.Response().StatusCode()
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) && fiberErr != nil {
			status = fiberErr.Code
		}

		route := "unmatched"
		if matched := c.Route(); matched != nil && matched.Path != "" {
			route = matched.Path
		}

		m.requests.WithLabelValues(c.Method(), route, strconv.Itoa(status)).Inc()
		m.duration.WithLabelValues(c.Method(), route).Observe(time.Since(started).Seconds())
		return err
	}
}
