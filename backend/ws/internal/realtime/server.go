package realtime

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/kael777/agentforge/backend/ws/internal/config"
	"github.com/kael777/agentforge/backend/ws/internal/observability"
	"github.com/kael777/agentforge/backend/ws/internal/ticket"
)

// Server wires configuration, ticketing and the hub into two HTTP handlers.
type Server struct {
	cfg       config.Config
	hub       *Hub
	verifier  *ticket.Verifier
	logger    *slog.Logger
	metrics   *observability.Metrics
	startedAt time.Time
	trusted   []*net.IPNet
}

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	verifier, err := ticket.NewVerifier(ticket.Config{
		Secret:      cfg.TicketSecret,
		Issuer:      cfg.TicketIssuer,
		Audience:    cfg.TicketAudience,
		MaxLifetime: 2 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	trusted, err := parseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg: cfg,
		hub: NewHub(Limits{
			MaxConnections:        cfg.Limits.MaxConnections,
			MaxConnectionsPerUser: cfg.Limits.MaxConnectionsPerUser,
			MaxConnectionsPerIP:   cfg.Limits.MaxConnectionsPerIP,
		}),
		verifier:  verifier,
		logger:    logger,
		metrics:   observability.New(),
		startedAt: time.Now(),
		trusted:   trusted,
	}, nil
}

// PublicHandler serves the connector endpoint and nothing else useful.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/ws", s.handleWebSocket)
	return s.accessLog(mux)
}

// ManagementHandler is meant for the loopback listener only.
func (s *Server) ManagementHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", s.metrics.Handler())
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if s.HubClosing() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"shutting_down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/version", s.handleVersion)
	return mux
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	body, err := json.Marshal(map[string]any{
		"service":     s.cfg.ServiceName,
		"env":         s.cfg.Environment,
		"uptime_sec":  int64(time.Since(s.startedAt).Seconds()),
		"connections": s.hub.Count(),
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// handleWebSocket runs the whole admission sequence. Every check happens before
// the protocol upgrade, so a rejected client receives a plain HTTP status and
// never occupies a slot in the hub.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.reject(w, http.StatusMethodNotAllowed, "method")
		return
	}

	origin := r.Header.Get("Origin")
	if !originAllowed(origin, s.cfg.AllowedOrigins, s.cfg.AllowEmptyOrigin) {
		// Cross-site WebSocket hijacking is the reason this check exists: the
		// browser will happily attach cookies to a handshake from any origin.
		s.reject(w, http.StatusForbidden, "origin")
		return
	}

	claims, err := s.verifier.Verify(r.URL.Query().Get("ticket"))
	if err != nil {
		s.reject(w, http.StatusUnauthorized, "ticket")
		return
	}

	ip := clientIP(r, s.trusted)
	session := newSession(
		s.hub,
		nil,
		uuid.NewString(),
		claims.Subject,
		ip,
		origin,
		SessionConfig{
			ReadTimeout:   s.cfg.Limits.ReadTimeout,
			WriteTimeout:  s.cfg.Limits.WriteTimeout,
			PingInterval:  s.cfg.Limits.PingInterval,
			SendQueueSize: s.cfg.Limits.SendQueueSize,
			MessageRate:   s.cfg.Limits.MessageRate,
			MessageBurst:  s.cfg.Limits.MessageBurst,
		},
		s.logger,
		s.metrics,
	)
	if err := s.hub.Register(session); err != nil {
		s.reject(w, statusForRegistrationErr(err), reasonForRegistrationErr(err))
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Accept performs its own origin check, which treats a missing Origin
		// as acceptable. The policy here is stricter and already enforced
		// above, so the library check is disabled instead of duplicated.
		InsecureSkipVerify: true,
	})
	if err != nil {
		// The session has no socket yet, so closing it only releases the hub
		// slot. Reporting the gauge here would be wrong: the client never
		// completed a handshake.
		session.Close(websocket.StatusInternalError, "accept failed")
		s.metrics.UpgradeFailed("accept")
		s.logger.Warn("websocket upgrade failed", "error", err)
		return
	}

	conn.SetReadLimit(s.cfg.Limits.MaxMessageBytes)
	if !session.Attach(conn) {
		// A shutdown landed between Register and the upgrade. Attach already
		// closed the socket, so there is nothing left to serve.
		return
	}
	s.metrics.ConnectionOpened()
	s.logger.Info("websocket opened",
		"connection_id", session.id,
		"user_id", session.userID,
		"origin", session.origin,
		"remote", session.ip,
	)
	session.Run(r.Context())
}

func (s *Server) reject(w http.ResponseWriter, status int, reason string) {
	s.metrics.UpgradeFailed(reason)
	w.WriteHeader(status)
}

// accessLog deliberately logs the path without the query string: the ticket
// travels in the query, and a log line is the least protected artifact a
// service produces.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap lets the WebSocket library reach the underlying ResponseWriter when it
// hijacks the connection, matching the http.ResponseController contract.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// originAllowed compares the Origin header against an exact allowlist.
func originAllowed(origin string, allowed []string, allowEmpty bool) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return allowEmpty
	}
	normalized, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if value, valid := normalizeOrigin(candidate); valid && value == normalized {
			return true
		}
	}
	return false
}

func normalizeOrigin(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(parsed.Host), true
}

// clientIP returns the peer address, or the client address when the peer is a
// configured proxy. Trusting X-Forwarded-For unconditionally would let any
// client choose its own identity and bypass the per-address limit.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(trusted) == 0 {
		return host
	}
	peer := net.ParseIP(host)
	if peer == nil || !isTrusted(peer, trusted) {
		return host
	}
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return host
	}
	// The left-most entry is the original client; the rest are proxies.
	first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
	if net.ParseIP(first) == nil {
		return host
	}
	return first
}

func isTrusted(ip net.IP, trusted []*net.IPNet) bool {
	for _, network := range trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseTrustedProxies(values []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			networks = append(networks, network)
			continue
		}
		ip := net.ParseIP(value)
		if ip == nil {
			return nil, &net.AddrError{Err: "invalid trusted proxy entry", Addr: value}
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return networks, nil
}

func statusForRegistrationErr(err error) int {
	switch err {
	case ErrTooManyConnections, ErrTooManyAddressConns, ErrTooManyUserConnections:
		return http.StatusTooManyRequests
	case ErrHubClosing:
		return http.StatusServiceUnavailable
	default:
		return http.StatusServiceUnavailable
	}
}

func reasonForRegistrationErr(err error) string {
	switch err {
	case ErrTooManyConnections:
		return "limit_global"
	case ErrTooManyUserConnections:
		return "limit_user"
	case ErrTooManyAddressConns:
		return "limit_address"
	case ErrHubClosing:
		return "shutting_down"
	default:
		return "unknown"
	}
}

// HubClosing reports whether the server has started draining.
func (s *Server) HubClosing() bool {
	return s.hub.isClosing()
}

// Hub exposes the connection set for tests and for main's shutdown sequence.
func (s *Server) Hub() *Hub { return s.hub }

// Metrics exposes the registry for tests.
func (s *Server) Metrics() *observability.Metrics { return s.metrics }
