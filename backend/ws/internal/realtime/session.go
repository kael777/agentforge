package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kael777/agentforge/backend/ws/internal/observability"
)

// Application close codes. 4000-4999 is the range reserved for applications,
// so clients can distinguish them from protocol-level closures.
const (
	// CloseIdleTimeout is sent when no frame arrived within the read timeout.
	CloseIdleTimeout websocket.StatusCode = 4001
)

var topicPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Message types are allowlisted before they reach a metric label. Using the
// client-supplied string directly would let any client create unlimited series.
var knownMessageTypes = map[string]struct{}{
	"ping":        {},
	"subscribe":   {},
	"unsubscribe": {},
	"publish":     {},
}

// Protocol is the wire format shared with clients:
//
//	client -> server: {"type":"subscribe|unsubscribe|publish|ping","topic":"...","data":{...}}
//	server -> client: {"type":"subscribed|unsubscribed|message|pong|error","topic":"...","code":"...","data":{...},"ts":0}
type inbound struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic,omitempty"`
	Payload json.RawMessage `json:"data,omitempty"`
}

type outbound struct {
	Type   string          `json:"type"`
	Topic  string          `json:"topic,omitempty"`
	Code   string          `json:"code,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	UnixTS int64           `json:"ts,omitempty"`
}

type queued struct {
	payload []byte
	kind    string
}

// SessionConfig carries the per-connection limits.
type SessionConfig struct {
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	PingInterval  time.Duration
	SendQueueSize int
	MessageRate   float64
	MessageBurst  int
}

// Session is one accepted WebSocket connection. Its lifetime is exactly the
// lifetime of the socket: every goroutine it starts stops when the session
// closes.
type Session struct {
	hub    *Hub
	conn   *websocket.Conn
	userID string
	ip     string
	origin string
	id     string

	cfg     SessionConfig
	logger  *slog.Logger
	metrics *observability.Metrics

	// ctx is cancelled when the session closes. Cancelling it is what releases
	// a blocked Read so the close handshake can proceed.
	ctx    context.Context
	cancel context.CancelFunc

	send chan queued
	done chan struct{}

	// connMu guards the hand-off between the handshake goroutine and Close.
	// A shutdown can win the race against Accept, in which case the session is
	// closed before it has a socket to close.
	connMu sync.Mutex
	opened bool
	closed bool

	closeOnce sync.Once
	closeCode websocket.StatusCode

	bucketMu sync.Mutex
	tokens   float64
	lastFill time.Time
}

func newSession(h *Hub, conn *websocket.Conn, id, userID, ip, origin string, cfg SessionConfig, logger *slog.Logger, m *observability.Metrics) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		hub:      h,
		conn:     conn,
		id:       id,
		userID:   userID,
		ip:       ip,
		origin:   origin,
		cfg:      cfg,
		logger:   logger,
		metrics:  m,
		ctx:      ctx,
		cancel:   cancel,
		send:     make(chan queued, cfg.SendQueueSize),
		done:     make(chan struct{}),
		tokens:   float64(cfg.MessageBurst),
		lastFill: time.Now(),
	}
}

// Run starts the three loops and blocks until the read side ends.
func (s *Session) Run(parent context.Context) {
	// The parent is the request context, which is cancelled when the connection
	// handler returns. Tying the two together means either side can stop the
	// session.
	stop := context.AfterFunc(parent, s.cancel)
	defer stop()

	go s.writeLoop()
	go s.pingLoop()
	s.readLoop(s.ctx)
}

// Attach hands the accepted socket to the session. It reports false when the
// session was already closed — a real race, because a shutdown signal can land
// between Register and the protocol upgrade. Returning false lets the caller
// account for the connection, and the socket is closed here so it cannot leak.
func (s *Session) Attach(conn *websocket.Conn) bool {
	s.connMu.Lock()
	s.conn = conn
	alreadyClosed := s.closed
	if !alreadyClosed {
		s.opened = true
	}
	s.connMu.Unlock()

	if alreadyClosed {
		_ = conn.Close(websocket.StatusGoingAway, "server restart")
		return false
	}
	return true
}

func (s *Session) currentConn() (*websocket.Conn, bool) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn, s.opened
}

// Close is idempotent and safe to call from any goroutine. It returns as soon
// as the session is logically closed; the close frame is delivered by a
// background goroutine.
//
// Deliberately non-blocking: websocket.Conn.Close waits for the peer's close
// frame while holding the read lock, and our reader holds that lock until it
// returns. A synchronous close would therefore stall for the library's full
// five-second budget — once per connection, serially, during shutdown. The hub
// accounts for these in-flight closes through Wait.
func (s *Session) Close(code websocket.StatusCode, reason string) {
	s.closeOnce.Do(func() {
		s.closeCode = code
		close(s.done)

		s.connMu.Lock()
		s.closed = true
		conn, opened := s.conn, s.opened
		s.connMu.Unlock()

		s.hub.Unregister(s)
		if opened {
			s.metrics.ConnectionClosed(int(code))
		}
		s.logger.Info("websocket closed",
			"connection_id", s.id,
			"user_id", s.userID,
			"origin", s.origin,
			"code", int(code),
			"reason", reason,
		)

		if conn == nil {
			// The handshake never completed, so there is no socket to close and
			// no frame to send.
			s.cancel()
			return
		}

		s.hub.beginClose()
		go func() {
			defer s.hub.endClose()
			defer s.cancel()
			_ = conn.Close(code, reason)
		}()
	})
}

func (s *Session) readLoop(ctx context.Context) {
	defer s.Close(websocket.StatusNormalClosure, "read loop ended")

	for {
		readCtx, cancel := context.WithTimeout(ctx, s.cfg.ReadTimeout)
		messageType, data, err := s.conn.Read(readCtx)
		cancel()
		if err != nil {
			s.Close(closeCodeFor(err), "read failed")
			return
		}

		if messageType != websocket.MessageText {
			// Binary frames are not part of this protocol. Rejecting them keeps
			// the parser surface to exactly one format.
			s.Close(websocket.StatusUnsupportedData, "text frames only")
			return
		}
		if !s.allowMessage() {
			s.Close(websocket.StatusPolicyViolation, "message rate exceeded")
			return
		}

		var envelope inbound
		if err := json.Unmarshal(data, &envelope); err != nil {
			s.trySend(mustJSON(outbound{Type: "error", Code: "INVALID_MESSAGE"}), "error")
			continue
		}
		s.metrics.Received(metricType(envelope.Type))
		s.handle(envelope)
	}
}

func (s *Session) handle(envelope inbound) {
	switch envelope.Type {
	case "ping":
		s.trySend(mustJSON(outbound{Type: "pong", UnixTS: time.Now().Unix()}), "pong")

	case "subscribe", "unsubscribe":
		if !topicPattern.MatchString(envelope.Topic) {
			s.trySend(mustJSON(outbound{Type: "error", Code: "INVALID_TOPIC"}), "error")
			return
		}
		if envelope.Type == "subscribe" {
			s.hub.Subscribe(s, envelope.Topic)
			s.trySend(mustJSON(outbound{Type: "subscribed", Topic: envelope.Topic}), "subscribed")
			return
		}
		s.hub.Unsubscribe(s, envelope.Topic)
		s.trySend(mustJSON(outbound{Type: "unsubscribed", Topic: envelope.Topic}), "unsubscribed")

	case "publish":
		if !topicPattern.MatchString(envelope.Topic) {
			s.trySend(mustJSON(outbound{Type: "error", Code: "INVALID_TOPIC"}), "error")
			return
		}
		if len(envelope.Payload) > 0 && !json.Valid(envelope.Payload) {
			s.trySend(mustJSON(outbound{Type: "error", Code: "INVALID_PAYLOAD"}), "error")
			return
		}
		payload := mustJSON(outbound{Type: "message", Topic: envelope.Topic, Data: envelope.Payload})
		s.hub.Broadcast(envelope.Topic, payload, "message")

	default:
		s.trySend(mustJSON(outbound{Type: "error", Code: "UNKNOWN_TYPE"}), "error")
	}
}

func (s *Session) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case item := <-s.send:
			writeCtx, cancel := context.WithTimeout(context.Background(), s.cfg.WriteTimeout)
			err := s.conn.Write(writeCtx, websocket.MessageText, item.payload)
			cancel()
			if err != nil {
				// A failed write means the peer is gone or not reading. The
				// session is finished either way.
				s.Close(websocket.StatusInternalError, "write failed")
				return
			}
			s.metrics.Sent(item.kind)
		}
	}
}

// pingLoop proves the peer is still there. A TCP connection can stay open for
// a long time after the client disappeared; only a failed pong reveals it.
func (s *Session) pingLoop() {
	ticker := time.NewTicker(s.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(context.Background(), s.cfg.PingInterval)
			err := s.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				s.Close(CloseIdleTimeout, "ping timeout")
				return
			}
		}
	}
}

// trySend never blocks. A full queue means the client is not keeping up, and
// blocking here would stall the read loop that produces the data.
func (s *Session) trySend(payload []byte, kind string) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.send <- queued{payload: payload, kind: kind}:
		return true
	default:
		return false
	}
}

// allowMessage implements a token bucket so a single client cannot flood the
// fan-out with one publish per read.
func (s *Session) allowMessage() bool {
	if s.cfg.MessageRate <= 0 {
		return true
	}
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.lastFill).Seconds()
	s.lastFill = now
	s.tokens += elapsed * s.cfg.MessageRate
	if max := float64(s.cfg.MessageBurst); s.tokens > max {
		s.tokens = max
	}
	if s.tokens < 1 {
		return false
	}
	s.tokens--
	return true
}

func closeCodeFor(err error) websocket.StatusCode {
	if status := websocket.CloseStatus(err); status != -1 {
		return status
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return CloseIdleTimeout
	}
	return websocket.StatusInternalError
}

func metricType(messageType string) string {
	if _, ok := knownMessageTypes[messageType]; ok {
		return messageType
	}
	return "unknown"
}

func mustJSON(envelope outbound) []byte {
	payload, err := json.Marshal(envelope)
	if err != nil {
		// outbound contains only strings, a timestamp and pre-validated JSON, so
		// marshalling cannot fail; panic here would still be worse than a
		// well-formed error frame.
		return []byte(`{"type":"error","code":"ENCODING_FAILED"}`)
	}
	return payload
}
