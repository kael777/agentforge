package observability

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics uses a private registry so tests stay isolated and the published
// series set is exactly the one declared here.
type Metrics struct {
	registry *prometheus.Registry

	connectionsActive prometheus.Gauge
	connectionsTotal  prometheus.Counter
	upgradeFailures   *prometheus.CounterVec
	messagesReceived  *prometheus.CounterVec
	messagesSent      *prometheus.CounterVec
	closes            *prometheus.CounterVec
	queueDropped      prometheus.Counter
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		registry: registry,
		connectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ws_connections_active",
			Help: "Number of WebSocket connections currently open.",
		}),
		connectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ws_connections_total",
			Help: "Total number of accepted WebSocket connections.",
		}),
		upgradeFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ws_upgrade_failures_total",
			Help: "WebSocket handshakes rejected before the upgrade, by reason.",
		}, []string{"reason"}),
		messagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ws_messages_received_total",
			Help: "Application messages read from clients, by message type.",
		}, []string{"type"}),
		messagesSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ws_messages_sent_total",
			Help: "Application messages written to clients, by message type.",
		}, []string{"type"}),
		closes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ws_connections_closed_total",
			Help: "Closed connections by close code.",
		}, []string{"code"}),
		queueDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ws_slow_consumer_closed_total",
			Help: "Connections closed because the outbound queue stayed full.",
		}),
	}
	registry.MustRegister(
		m.connectionsActive,
		m.connectionsTotal,
		m.upgradeFailures,
		m.messagesReceived,
		m.messagesSent,
		m.closes,
		m.queueDropped,
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}

func (m *Metrics) ConnectionOpened() {
	m.connectionsTotal.Inc()
	m.connectionsActive.Inc()
}

func (m *Metrics) ConnectionClosed(code int) {
	m.connectionsActive.Dec()
	m.closes.WithLabelValues(strconv.Itoa(code)).Inc()
}

func (m *Metrics) UpgradeFailed(reason string) {
	m.upgradeFailures.WithLabelValues(reason).Inc()
}

func (m *Metrics) Received(messageType string) {
	m.messagesReceived.WithLabelValues(messageType).Inc()
}

func (m *Metrics) Sent(messageType string) {
	m.messagesSent.WithLabelValues(messageType).Inc()
}

func (m *Metrics) SlowConsumer() {
	m.queueDropped.Inc()
}
