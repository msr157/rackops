package edge

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rackops/rackops/pkg/tunnel"
)

// liveSession is one authenticated install currently connected to this POP.
type liveSession struct {
	tenantID string
	sess     tunnel.Session
	popID    string
	agentVer string
	connCnt  atomic.Int64 // open data streams, for observability
	openedAt time.Time
	closed   atomic.Bool
}

// Close tears the session down. It is safe to call more than once, which
// matters because a session can be closed concurrently by the reaper, by an
// admin suspending the tenant, and by the peer hanging up.
func (s *liveSession) Close() {
	if s.closed.Swap(true) {
		return
	}
	_ = s.sess.Close()
}

// SessionTable tracks live sessions by tenant.
//
// An install may hold more than one session at a time: the client keeps
// connections to two POPs for failover, and during a reconnect the old and
// new sessions briefly overlap. Within one POP we keep only the newest —
// an install that reconnects because its previous session was half-dead
// must not have traffic routed onto the corpse.
type SessionTable struct {
	mu       sync.RWMutex
	byTenant map[string]*liveSession
}

// NewSessionTable returns an empty table.
func NewSessionTable() *SessionTable {
	return &SessionTable{byTenant: map[string]*liveSession{}}
}

// Add registers a session, displacing and closing any previous one for the
// same tenant. It returns the newly registered session.
func (t *SessionTable) Add(s *liveSession) {
	t.mu.Lock()
	old := t.byTenant[s.tenantID]
	t.byTenant[s.tenantID] = s
	t.mu.Unlock()

	if old != nil && old != s {
		// Closed outside the lock: Close can block on network teardown, and
		// holding the table lock through it would stall every in-flight
		// request on this POP.
		old.Close()
	}
}

// Remove deregisters a session, but only if it is still the current one for
// its tenant. Without the identity check, a slow teardown of a displaced
// session would delete its healthy replacement.
func (t *SessionTable) Remove(s *liveSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.byTenant[s.tenantID]; ok && cur == s {
		delete(t.byTenant, s.tenantID)
	}
}

// Get returns the live session for a tenant.
func (t *SessionTable) Get(tenantID string) (*liveSession, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.byTenant[tenantID]
	return s, ok
}

// CloseTenant drops a tenant's session immediately. This is what makes the
// abuse kill switch instant: suspending a tenant in the registry stops new
// sessions, and this stops the one already running.
func (t *SessionTable) CloseTenant(tenantID string) bool {
	t.mu.Lock()
	s, ok := t.byTenant[tenantID]
	if ok {
		delete(t.byTenant, tenantID)
	}
	t.mu.Unlock()
	if ok {
		s.Close()
	}
	return ok
}

// Count returns the number of live sessions.
func (t *SessionTable) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.byTenant)
}

// Snapshot describes one live session for the admin API.
type Snapshot struct {
	TenantID  string    `json:"tenantId"`
	Transport string    `json:"transport"`
	RemoteRaw string    `json:"remoteAddr"`
	AgentVer  string    `json:"agentVersion,omitempty"`
	Streams   int64     `json:"openStreams"`
	OpenedAt  time.Time `json:"openedAt"`
}

// Snapshot lists live sessions.
func (t *SessionTable) Snapshot() []Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Snapshot, 0, len(t.byTenant))
	for _, s := range t.byTenant {
		out = append(out, Snapshot{
			TenantID:  s.tenantID,
			Transport: s.sess.Transport(),
			RemoteRaw: s.sess.RemoteAddr().String(),
			AgentVer:  s.agentVer,
			Streams:   s.connCnt.Load(),
			OpenedAt:  s.openedAt,
		})
	}
	return out
}

// ---- dialing through a session ----

// errNoSession means no install is currently connected for this hostname.
// The proxy turns it into a 502 with an explanation the visitor can act on.
var errNoSession = errors.New("no live tunnel session for this hostname")

// errBackendDown means the tunnel is up but the customer's own edge refused
// the connection — their problem to fix, and the message says so.
var errBackendDown = errors.New("the tunnel is connected but the app behind it did not answer")

// dialThroughSession opens a data stream and completes the per-stream
// handshake, returning a net.Conn the HTTP transport can drive.
//
// The handshake matters: without the client's StreamAccept we would hand
// the transport a stream that is open at the QUIC level but doomed at the
// application level, and the visitor would see a truncated response instead
// of a clean 502.
func (e *Edge) dialThroughSession(ctx context.Context, tenantID, host string, remoteAddr string, isTLS bool) (net.Conn, error) {
	live, ok := e.sessions.Get(tenantID)
	if !ok {
		return nil, errNoSession
	}

	st, err := live.sess.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	// Bound the per-stream handshake so a wedged client cannot pin a
	// visitor's request open indefinitely.
	_ = st.SetDeadline(time.Now().Add(15 * time.Second))

	if err := tunnel.WriteStreamHeader(st, tunnel.StreamHeader{
		Host:       host,
		RemoteAddr: remoteAddr,
		TLS:        isTLS,
	}); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("send stream header: %w", err)
	}
	ack, err := tunnel.ReadStreamAccept(st)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("read stream ack: %w", err)
	}
	if !ack.OK {
		_ = st.Close()
		if ack.Error != "" {
			return nil, fmt.Errorf("%w: %s", errBackendDown, ack.Error)
		}
		return nil, errBackendDown
	}
	// Clear the handshake deadline: the stream now carries a real request,
	// which may legitimately be a long download or a websocket.
	_ = st.SetDeadline(time.Time{})

	live.connCnt.Add(1)
	return &streamConn{
		Stream: st,
		live:   live,
		local:  live.sess.RemoteAddr(),
	}, nil
}

// streamConn adapts a tunnel stream to net.Conn so net/http can use it as a
// transport connection, and decrements the session's stream counter exactly
// once when it closes.
type streamConn struct {
	tunnel.Stream
	live   *liveSession
	local  net.Addr
	closed atomic.Bool
}

func (c *streamConn) Close() error {
	if !c.closed.Swap(true) {
		c.live.connCnt.Add(-1)
	}
	return c.Stream.Close()
}

func (c *streamConn) LocalAddr() net.Addr  { return c.local }
func (c *streamConn) RemoteAddr() net.Addr { return c.local }

// ---- session lifecycle ----

// handleSession takes ownership of a newly authenticated session: registers
// it, waits for it to die, and cleans up. It runs for the life of the
// session, one goroutine per connected install.
func (e *Edge) handleSession(acc *tunnel.AcceptedSession) {
	live := &liveSession{
		tenantID: acc.TenantID,
		sess:     acc.Session,
		popID:    e.cfg.POPID,
		agentVer: acc.AgentVer,
		openedAt: time.Now(),
	}
	e.sessions.Add(live)
	log.Printf("edge: tenant %s connected over %s from %s (agent %s)",
		acc.TenantID, acc.Session.Transport(), acc.Session.RemoteAddr(), orUnknown(acc.AgentVer))

	<-acc.Session.Done()

	e.sessions.Remove(live)
	live.Close()
	log.Printf("edge: tenant %s disconnected (session lasted %s)",
		acc.TenantID, time.Since(live.openedAt).Round(time.Second))
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
