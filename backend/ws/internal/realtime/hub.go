package realtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Registration failures. The handshake translates each one into a distinct
// status code so a client can tell "slow down" from "server is restarting".
var (
	ErrTooManyConnections     = errors.New("global connection limit reached")
	ErrTooManyUserConnections = errors.New("per-user connection limit reached")
	ErrTooManyAddressConns    = errors.New("per-address connection limit reached")
	ErrHubClosing             = errors.New("server is shutting down")
)

// Limits is the subset of configuration the hub needs.
type Limits struct {
	MaxConnections        int
	MaxConnectionsPerUser int
	MaxConnectionsPerIP   int
}

// Hub owns the live connection set and the topic fan-out.
//
// All bookkeeping lives behind one mutex. Fan-out is then a single pass over a
// topic's members instead of a per-connection scan, which is what keeps a
// publish storm from turning into O(n²) work.
type Hub struct {
	mu       sync.Mutex
	limits   Limits
	sessions map[*Session]struct{}
	perUser  map[string]int
	perIP    map[string]int
	topics   map[string]map[*Session]struct{}
	closing  bool
	// pending counts close-frame deliveries still in flight. A connection is
	// removed from sessions immediately, but its socket may take a few seconds
	// to finish the close handshake.
	pending int
}

func NewHub(limits Limits) *Hub {
	return &Hub{
		limits:   limits,
		sessions: make(map[*Session]struct{}),
		perUser:  make(map[string]int),
		perIP:    make(map[string]int),
		topics:   make(map[string]map[*Session]struct{}),
	}
}

// Register reserves a slot for the session. The handshake calls it before the
// protocol upgrade, so an overloaded server rejects with an HTTP status instead
// of accepting a connection it cannot serve.
func (h *Hub) Register(s *Session) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closing {
		return ErrHubClosing
	}
	if h.limits.MaxConnections > 0 && len(h.sessions) >= h.limits.MaxConnections {
		return ErrTooManyConnections
	}
	if h.limits.MaxConnectionsPerUser > 0 && h.perUser[s.userID] >= h.limits.MaxConnectionsPerUser {
		return ErrTooManyUserConnections
	}
	if h.limits.MaxConnectionsPerIP > 0 && h.perIP[s.ip] >= h.limits.MaxConnectionsPerIP {
		return ErrTooManyAddressConns
	}

	h.sessions[s] = struct{}{}
	h.perUser[s.userID]++
	h.perIP[s.ip]++
	return nil
}

// Unregister releases the slot. It is idempotent: Close and the read loop both
// call it, and only the first call has an effect.
func (h *Hub) Unregister(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.sessions[s]; !ok {
		return
	}
	delete(h.sessions, s)
	if h.perUser[s.userID]--; h.perUser[s.userID] <= 0 {
		delete(h.perUser, s.userID)
	}
	if h.perIP[s.ip]--; h.perIP[s.ip] <= 0 {
		delete(h.perIP, s.ip)
	}
	for topic := range h.topics {
		if members, ok := h.topics[topic]; ok {
			if _, subscribed := members[s]; subscribed {
				delete(members, s)
				if len(members) == 0 {
					delete(h.topics, topic)
				}
			}
		}
	}
}

func (h *Hub) Subscribe(s *Session, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessions[s]; !ok {
		return
	}
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[*Session]struct{})
	}
	h.topics[topic][s] = struct{}{}
}

func (h *Hub) Unsubscribe(s *Session, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if members, ok := h.topics[topic]; ok {
		delete(members, s)
		if len(members) == 0 {
			delete(h.topics, topic)
		}
	}
}

// Broadcast delivers one payload to every subscriber of a topic and returns how
// many connections accepted it. A connection whose send queue is full is
// treated as a slow consumer: it is closed instead of being allowed to apply
// backpressure to everyone else.
func (h *Hub) Broadcast(topic string, payload []byte, messageType string) int {
	h.mu.Lock()
	members := make([]*Session, 0, len(h.topics[topic]))
	for s := range h.topics[topic] {
		members = append(members, s)
	}
	h.mu.Unlock()

	delivered := 0
	for _, s := range members {
		if s.trySend(payload, messageType) {
			delivered++
			continue
		}
		s.Close(websocket.StatusTryAgainLater, "slow consumer")
	}
	return delivered
}

// CloseAll asks every connection to go away and stops accepting new ones.
// Closes run in parallel so one unresponsive peer cannot serialize shutdown for
// everyone else; Wait blocks until the sessions are gone.
func (h *Hub) CloseAll(code websocket.StatusCode, reason string) {
	h.mu.Lock()
	h.closing = true
	sessions := make([]*Session, 0, len(h.sessions))
	for s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(sessions))
	for _, s := range sessions {
		go func(s *Session) {
			defer wg.Done()
			s.Close(code, reason)
		}(s)
	}
	wg.Wait()
}

// Wait blocks until every connection has unregistered and every close frame has
// been delivered or abandoned, or the context expires.
//
// It polls instead of waiting on a condition variable: the drain path runs at
// most once per process, and a 10ms tick is cheaper than the bookkeeping needed
// to wake a waiter from two different counters.
func (h *Hub) Wait(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		h.mu.Lock()
		remaining := len(h.sessions) + h.pending
		h.mu.Unlock()
		if remaining == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (h *Hub) beginClose() {
	h.mu.Lock()
	h.pending++
	h.mu.Unlock()
}

func (h *Hub) endClose() {
	h.mu.Lock()
	h.pending--
	h.mu.Unlock()
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

func (h *Hub) isClosing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closing
}

func (h *Hub) TopicCount(topic string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.topics[topic])
}

// sessionCounts exists so tests can assert that per-user and per-IP counters do
// not leak after connections close.
func (h *Hub) sessionCounts() (users, addresses int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.perUser), len(h.perIP)
}
