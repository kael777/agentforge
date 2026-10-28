package realtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/kael777/agentforge/backend/ws/internal/config"
	"github.com/kael777/agentforge/backend/ws/internal/observability"
)

const (
	testTicketSecret = "ticket-secret-0123456789012345678901"
	testIssuer       = "agentforge-api"
	testAudience     = "agentforge-ws"
	allowedOrigin    = "http://localhost:5173"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() config.Config {
	return config.Config{
		ServiceName:       "agentforge-ws-test",
		Environment:       "test",
		Address:           ":0",
		ManagementAddress: "127.0.0.1:0",
		AllowedOrigins:    []string{allowedOrigin},
		TicketSecret:      testTicketSecret,
		TicketIssuer:      testIssuer,
		TicketAudience:    testAudience,
		ShutdownTimeout:   2 * time.Second,
		Limits: config.Limits{
			MaxConnections:        100,
			MaxConnectionsPerUser: 5,
			MaxConnectionsPerIP:   20,
			MaxMessageBytes:       1024,
			SendQueueSize:         8,
			ReadTimeout:           5 * time.Second,
			WriteTimeout:          2 * time.Second,
			PingInterval:          30 * time.Second,
			MessageRate:           1000,
			MessageBurst:          1000,
		},
	}
}

type harness struct {
	server *Server
	http   *httptest.Server
	wsURL  string
}

func newHarness(t *testing.T, mutate func(*config.Config)) *harness {
	t.Helper()
	cfg := testConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ts := httptest.NewServer(srv.PublicHandler())
	t.Cleanup(ts.Close)
	return &harness{
		server: srv,
		http:   ts,
		wsURL:  "ws" + strings.TrimPrefix(ts.URL, "http"),
	}
}

func mintTicket(t *testing.T, subject string, mutate func(*jwt.RegisteredClaims)) string {
	t.Helper()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    testIssuer,
		Audience:  jwt.ClaimStrings{testAudience},
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(30 * time.Second)),
	}
	if mutate != nil {
		mutate(&claims)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testTicketSecret))
	if err != nil {
		t.Fatalf("sign ticket: %v", err)
	}
	return signed
}

// dial performs the handshake and returns the resulting connection or the HTTP
// response that refused it.
func (h *harness) dial(t *testing.T, ticket, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	options := &websocket.DialOptions{HTTPHeader: http.Header{}}
	if origin != "" {
		options.HTTPHeader.Set("Origin", origin)
	}
	target := h.wsURL + "/ws"
	if ticket != "" {
		target += "?ticket=" + ticket
	}
	return websocket.Dial(ctx, target, options)
}

func (h *harness) mustDial(t *testing.T, ticket, origin string) *websocket.Conn {
	t.Helper()
	conn, resp, err := h.dial(t, ticket, origin)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial failed: %v (status %d)", err, status)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func readEnvelope(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return decoded
}

func send(t *testing.T, conn *websocket.Conn, body string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(body)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// ---- handshake rejections -------------------------------------------------

func TestHandshakeRejectsMissingOrigin(t *testing.T) {
	h := newHarness(t, nil)
	_, resp, err := h.dial(t, mintTicket(t, "user-1", nil), "")
	if err == nil {
		t.Fatal("expected the handshake to fail without an Origin header")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", resp)
	}
}

func TestHandshakeRejectsForeignOrigin(t *testing.T) {
	h := newHarness(t, nil)
	_, resp, err := h.dial(t, mintTicket(t, "user-1", nil), "http://evil.example.com")
	if err == nil {
		t.Fatal("expected the handshake to fail for a foreign origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", resp)
	}
}

func TestHandshakeRejectsBadTickets(t *testing.T) {
	h := newHarness(t, nil)

	cases := map[string]string{
		"missing": "",
		"garbage": "not-a-jwt",
		"expired": mintTicket(t, "user-1", func(c *jwt.RegisteredClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		}),
		"wrong audience": mintTicket(t, "user-1", func(c *jwt.RegisteredClaims) {
			c.Audience = jwt.ClaimStrings{"another-service"}
		}),
		"wrong issuer": mintTicket(t, "user-1", func(c *jwt.RegisteredClaims) {
			c.Issuer = "someone-else"
		}),
		"no subject": mintTicket(t, "", nil),
		"long lived": mintTicket(t, "user-1", func(c *jwt.RegisteredClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
		}),
	}

	for name, ticket := range cases {
		t.Run(name, func(t *testing.T) {
			_, resp, err := h.dial(t, ticket, allowedOrigin)
			if err == nil {
				t.Fatal("expected the handshake to fail")
			}
			if resp == nil || resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %v", resp)
			}
		})
	}
}

// A ticket is single use: replaying it must fail even though the signature is
// still valid.
func TestTicketCannotBeReplayed(t *testing.T) {
	h := newHarness(t, nil)
	ticket := mintTicket(t, "user-1", nil)

	conn := h.mustDial(t, ticket, allowedOrigin)
	if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, resp, err := h.dial(t, ticket, allowedOrigin)
	if err == nil {
		t.Fatal("expected the replayed ticket to be refused")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

func TestPerUserConnectionLimit(t *testing.T) {
	h := newHarness(t, func(cfg *config.Config) {
		cfg.Limits.MaxConnectionsPerUser = 1
	})

	_ = h.mustDial(t, mintTicket(t, "user-1", nil), allowedOrigin)

	_, resp, err := h.dial(t, mintTicket(t, "user-1", nil), allowedOrigin)
	if err == nil {
		t.Fatal("expected the second connection to be refused")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %v", resp)
	}
}

// ---- protocol -------------------------------------------------------------

func TestSubscribeAndBroadcast(t *testing.T) {
	h := newHarness(t, nil)

	subscriber := h.mustDial(t, mintTicket(t, "user-1", nil), allowedOrigin)
	send(t, subscriber, `{"type":"subscribe","topic":"demo"}`)
	if got := readEnvelope(t, subscriber)["type"]; got != "subscribed" {
		t.Fatalf("expected subscribed, got %v", got)
	}

	publisher := h.mustDial(t, mintTicket(t, "user-2", nil), allowedOrigin)
	send(t, publisher, `{"type":"publish","topic":"demo","data":{"hello":"world"}}`)

	message := readEnvelope(t, subscriber)
	if message["type"] != "message" || message["topic"] != "demo" {
		t.Fatalf("unexpected message: %#v", message)
	}
	data, ok := message["data"].(map[string]any)
	if !ok || data["hello"] != "world" {
		t.Fatalf("payload was not delivered: %#v", message["data"])
	}
}

func TestUnknownTypeReturnsError(t *testing.T) {
	h := newHarness(t, nil)
	conn := h.mustDial(t, mintTicket(t, "user-1", nil), allowedOrigin)

	send(t, conn, `{"type":"nonsense"}`)
	if got := readEnvelope(t, conn)["code"]; got != "UNKNOWN_TYPE" {
		t.Fatalf("expected UNKNOWN_TYPE, got %v", got)
	}
}

func TestInvalidTopicIsRejected(t *testing.T) {
	h := newHarness(t, nil)
	conn := h.mustDial(t, mintTicket(t, "user-1", nil), allowedOrigin)

	send(t, conn, `{"type":"subscribe","topic":"../etc/passwd"}`)
	if got := readEnvelope(t, conn)["code"]; got != "INVALID_TOPIC" {
		t.Fatalf("expected INVALID_TOPIC, got %v", got)
	}
}

// Oversized frames must be refused by the read limit, not buffered.
func TestOversizedMessageClosesConnection(t *testing.T) {
	h := newHarness(t, nil)
	conn := h.mustDial(t, mintTicket(t, "user-1", nil), allowedOrigin)

	payload := `{"type":"publish","topic":"demo","data":"` + strings.Repeat("x", 4096) + `"}`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("expected the connection to be closed")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusMessageTooBig {
		t.Fatalf("expected close code %d, got %d (%v)", websocket.StatusMessageTooBig, status, err)
	}
}

func TestGracefulShutdownSendsGoingAway(t *testing.T) {
	h := newHarness(t, nil)
	conn := h.mustDial(t, mintTicket(t, "user-1", nil), allowedOrigin)

	// Wait until the hub sees the connection, otherwise the close would race
	// with registration.
	deadline := time.Now().Add(2 * time.Second)
	for h.server.Hub().Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	h.server.Hub().CloseAll(websocket.StatusGoingAway, "server restart")

	// The library's close handshake can take up to five seconds while a read is
	// in flight, so the drain budget must be larger than that.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("expected the server to close the connection")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusGoingAway {
		t.Fatalf("expected 1001, got %d (%v)", status, err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := h.server.Hub().Wait(waitCtx); err != nil {
		t.Fatalf("hub did not drain: %v", err)
	}
}

// A client that stops reading must be disconnected instead of growing an
// unbounded outbound queue.
func TestSlowConsumerIsDisconnected(t *testing.T) {
	hub := NewHub(Limits{})
	metrics := observability.New()
	session := newSession(hub, nil, "c1", "user-1", "127.0.0.1", allowedOrigin,
		SessionConfig{SendQueueSize: 1, MessageRate: 0},
		testLogger(), metrics)

	if !session.trySend([]byte(`{"type":"pong"}`), "pong") {
		t.Fatal("first send should be accepted")
	}
	if session.trySend([]byte(`{"type":"pong"}`), "pong") {
		t.Fatal("second send should be refused once the queue is full")
	}
}
