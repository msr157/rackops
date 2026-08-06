package tunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/quic-go/quic-go"
)

// Transport names, as reported to the console so support can tell at a
// glance whether a customer fell back to TCP (and therefore sits behind
// something that blocks UDP).
const (
	TransportQUIC = "quic"
	TransportTCP  = "tcp"
)

// Stream is one multiplexed bidirectional byte stream inside a session.
// Both transports satisfy it, so the proxy logic above never branches on
// which one is in use.
type Stream interface {
	io.ReadWriteCloser
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// Session is an authenticated tunnel between a POP and one install.
type Session interface {
	// Open starts a new outbound stream.
	Open(ctx context.Context) (Stream, error)
	// Accept waits for the peer to open a stream.
	Accept(ctx context.Context) (Stream, error)
	// Close tears the session down, ending all streams.
	Close() error
	// Transport reports TransportQUIC or TransportTCP.
	Transport() string
	// RemoteAddr is the peer's address, for logging.
	RemoteAddr() net.Addr
	// Exporter returns the TLS keying material used for channel binding, or
	// nil if the transport cannot supply it.
	Exporter() []byte
	// Done is closed when the session dies, so supervisors can wait on it
	// instead of polling.
	Done() <-chan struct{}
}

// exporterLabel is the RFC 5705 exporter label used for channel binding.
// The label is specific to this protocol, so material exported here cannot
// collide with any other use of the same TLS connection.
const exporterLabel = "EXPORTER-rackops-frontdoor-v1"

// exportKeyingMaterial pulls 32 bytes of channel-binding material from a
// completed TLS handshake. A transport that cannot export returns nil rather
// than failing: the transcript stays valid, just without channel binding.
func exportKeyingMaterial(cs tls.ConnectionState) []byte {
	km, err := cs.ExportKeyingMaterial(exporterLabel, nil, 32)
	if err != nil {
		return nil
	}
	return km
}

// ---- QUIC ----

// quicSession adapts a QUIC connection to Session. QUIC gives us native
// stream multiplexing, so this is mostly a naming layer.
type quicSession struct {
	conn     *quic.Conn
	exporter []byte
}

func (s *quicSession) Open(ctx context.Context) (Stream, error) {
	st, err := s.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *quicSession) Accept(ctx context.Context) (Stream, error) {
	st, err := s.conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *quicSession) Close() error {
	return s.conn.CloseWithError(0, "")
}

func (s *quicSession) Transport() string { return TransportQUIC }

func (s *quicSession) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

func (s *quicSession) Exporter() []byte { return s.exporter }

func (s *quicSession) Done() <-chan struct{} { return s.conn.Context().Done() }

// ---- TLS + yamux ----

// tcpSession adapts a yamux session over TLS to Session. yamux supplies the
// multiplexing that QUIC has natively; the TLS layer below it makes the
// traffic look like ordinary HTTPS to anything on the path.
type tcpSession struct {
	sess     *yamux.Session
	conn     net.Conn
	exporter []byte
}

func (s *tcpSession) Open(ctx context.Context) (Stream, error) {
	// yamux has no context-aware open, so race the open against the context
	// and close the loser. OpenStream itself is bounded by the session's own
	// keepalive/timeout settings.
	type result struct {
		st  *yamux.Stream
		err error
	}
	ch := make(chan result, 1)
	go func() {
		st, err := s.sess.OpenStream()
		ch <- result{st, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.err == nil {
				_ = r.st.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.st, nil
	}
}

func (s *tcpSession) Accept(ctx context.Context) (Stream, error) {
	type result struct {
		st  net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		st, err := s.sess.Accept()
		ch <- result{st, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.err == nil {
				_ = r.st.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.st, nil
	}
}

func (s *tcpSession) Close() error {
	err := s.sess.Close()
	_ = s.conn.Close()
	return err
}

func (s *tcpSession) Transport() string { return TransportTCP }

func (s *tcpSession) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

func (s *tcpSession) Exporter() []byte { return s.exporter }

func (s *tcpSession) Done() <-chan struct{} { return s.sess.CloseChan() }

// yamuxConfig returns the multiplexer settings shared by both ends. Both
// sides must agree on keepalive or the session dies under load.
func yamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = KeepAlive
	cfg.ConnectionWriteTimeout = 20 * time.Second
	// 4 MiB per stream: enough to keep a fast visitor's download saturated
	// over a high-latency link without letting one stream monopolize memory.
	cfg.MaxStreamWindowSize = 4 * 1024 * 1024
	// yamux logs to stderr by default, which would interleave with the
	// structured logging above it. Silence it and rely on our own errors.
	cfg.LogOutput = io.Discard
	return cfg
}

// quicConfig returns the QUIC settings shared by both ends.
func quicConfig() *quic.Config {
	return &quic.Config{
		MaxIdleTimeout:        IdleTimeout,
		KeepAlivePeriod:       KeepAlive,
		HandshakeIdleTimeout:  handshakeTimeout,
		MaxIncomingStreams:    1024,
		MaxIncomingUniStreams: -1, // unidirectional streams are unused
	}
}

// ---- proxying ----

// Splice copies bytes in both directions between a tunnel stream and a local
// connection until either side finishes, and reports how many bytes flowed
// each way. It is the single place bytes are moved, so it is also the single
// place traffic can be metered.
//
// Both directions are half-closed independently: when the visitor finishes
// its request body, the backend must still be able to send its response.
// Collapsing that into a single Close is the classic way to truncate large
// responses, so each direction closes only its own write side.
func Splice(a Stream, b net.Conn) (fromA, fromB int64) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fromA, _ = io.Copy(b, a)
		halfCloseWrite(b)
	}()
	fromB, _ = io.Copy(a, b)
	// The tunnel stream's write side is closed to signal EOF downstream;
	// the read side stays open until the other direction drains.
	_ = a.Close()
	<-done
	return fromA, fromB
}

// halfCloseWrite shuts down only the write half of a connection when the
// type supports it, so the peer sees EOF while we can still read.
func halfCloseWrite(c net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

// ---- shared dial/accept plumbing ----

// dialQUIC establishes a QUIC session to addr.
func dialQUIC(ctx context.Context, addr string, tlsConf *tls.Config) (Session, error) {
	conn, err := quic.DialAddr(ctx, addr, tlsConf, quicConfig())
	if err != nil {
		return nil, fmt.Errorf("quic dial %s: %w", addr, err)
	}
	return &quicSession{conn: conn, exporter: exportKeyingMaterial(conn.ConnectionState().TLS)}, nil
}

// dialTCP establishes a TLS+yamux session to addr. The client is the yamux
// client; the POP is the yamux server.
func dialTCP(ctx context.Context, addr string, tlsConf *tls.Config) (Session, error) {
	d := &net.Dialer{Timeout: handshakeTimeout}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", addr, err)
	}
	conn := tls.Client(raw, tlsConf)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("tls handshake %s: %w", addr, err)
	}
	sess, err := yamux.Client(conn, yamuxConfig())
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("yamux client: %w", err)
	}
	return &tcpSession{sess: sess, conn: conn, exporter: exportKeyingMaterial(conn.ConnectionState())}, nil
}

// isTimeout reports whether err is a network timeout, which the client
// treats as "this transport is blocked here" rather than a hard failure.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
