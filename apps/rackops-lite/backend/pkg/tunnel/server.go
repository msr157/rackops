package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/quic-go/quic-go"
)

// Authenticator resolves an incoming ConnectRequest to a decision. The POP
// supplies this; the protocol layer stays free of tenant storage policy.
type Authenticator interface {
	// Lookup returns the enrolled public key for a tenant. It reports
	// found=false for unknown tenants — indistinguishable, from the
	// client's perspective, from a bad signature.
	Lookup(tenantID string) (key ed25519.PublicKey, found bool)
	// Authorize runs after the signature verifies. It is where quota
	// exhaustion, abuse holds, and billing state are enforced. Returning a
	// non-nil error refuses the session; the code is one of the
	// ConnectResponse codes.
	Authorize(tenantID string) (hostnames []string, quota, used int64, periodEnd time.Time, code string, err error)
}

// ServerConfig configures the accepting side.
type ServerConfig struct {
	// POPID identifies this point of presence in challenges and logs.
	POPID string
	// TLSConfig must carry the POP's certificate. ALPN and minimum version
	// are set by Listen.
	TLSConfig *tls.Config
	// Auth resolves tenants. Required.
	Auth Authenticator
	// OnSession is called with each authenticated session. It owns the
	// session's lifetime and must call Close when done. Required.
	OnSession func(*AcceptedSession)
	// OnError reports handshake failures for observability. Optional; a nil
	// value discards them. Failures here are routine (port scans, probes),
	// so they must never be fatal.
	OnError func(remote net.Addr, err error)
}

// AcceptedSession is an authenticated inbound session plus the identity the
// POP resolved for it.
type AcceptedSession struct {
	Session   Session
	TenantID  string
	Hostnames []string
	AgentVer  string
	QuotaByte int64
	UsedBytes int64
	PeriodEnd time.Time
}

// Server accepts tunnel sessions on both transports.
type Server struct {
	cfg ServerConfig

	quicLn *quic.Listener
	tcpLn  net.Listener
	// udpConn backs quicLn. It is held so Close can release the socket:
	// closing the QUIC listener alone leaves the UDP port bound.
	udpConn *net.UDPConn

	ctx    context.Context
	cancel context.CancelFunc
}

// NewServer validates configuration and returns a Server ready to Listen.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Auth == nil {
		return nil, errors.New("tunnel: ServerConfig.Auth is required")
	}
	if cfg.OnSession == nil {
		return nil, errors.New("tunnel: ServerConfig.OnSession is required")
	}
	if cfg.TLSConfig == nil || len(cfg.TLSConfig.Certificates) == 0 && cfg.TLSConfig.GetCertificate == nil {
		return nil, errors.New("tunnel: ServerConfig.TLSConfig needs a certificate")
	}
	if cfg.POPID == "" {
		cfg.POPID = "pop"
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, ctx: ctx, cancel: cancel}, nil
}

// Listen starts accepting on the given address for both transports: QUIC on
// UDP and TLS+yamux on TCP, same port. Serving both on 443 is what lets a
// client fall back without a second address to configure.
//
// The TCP listener is bound first and the UDP socket is then pinned to
// whatever port it landed on. That ordering matters when the address uses
// port 0 (tests, ephemeral POPs): binding each independently would hand the
// two transports different ports, and the client — which derives both from
// one address — would silently fail to reach QUIC.
//
// It returns once both listeners are open; accept loops run in the
// background until Close.
func (s *Server) Listen(addr string) error {
	tlsConf := s.cfg.TLSConfig.Clone()
	tlsConf.NextProtos = []string{ALPN}
	tlsConf.MinVersion = tls.VersionTLS13

	tcpLn, err := tls.Listen("tcp", addr, tlsConf)
	if err != nil {
		return fmt.Errorf("tunnel: listen tcp %s: %w", addr, err)
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   tcpLn.Addr().(*net.TCPAddr).IP,
		Port: tcpLn.Addr().(*net.TCPAddr).Port,
	})
	if err != nil {
		_ = tcpLn.Close()
		return fmt.Errorf("tunnel: listen udp %s: %w", tcpLn.Addr(), err)
	}
	quicLn, err := quic.Listen(udpConn, tlsConf, quicConfig())
	if err != nil {
		_ = tcpLn.Close()
		_ = udpConn.Close()
		return fmt.Errorf("tunnel: serve quic %s: %w", tcpLn.Addr(), err)
	}
	s.quicLn, s.tcpLn, s.udpConn = quicLn, tcpLn, udpConn

	go s.acceptQUIC()
	go s.acceptTCP()
	return nil
}

// Addr returns the TCP address the server is listening on, which is also the
// UDP port used by QUIC. Useful for tests that bind port 0.
func (s *Server) Addr() net.Addr {
	if s.tcpLn == nil {
		return nil
	}
	return s.tcpLn.Addr()
}

// Close stops the listeners. Sessions already handed to OnSession are the
// caller's to close.
func (s *Server) Close() error {
	s.cancel()
	var firstErr error
	if s.quicLn != nil {
		if err := s.quicLn.Close(); err != nil {
			firstErr = err
		}
	}
	if s.tcpLn != nil {
		if err := s.tcpLn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.udpConn != nil {
		if err := s.udpConn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ServeQUIC accepts tunnel sessions on an existing UDP socket.
//
// It exists so the POP can own the socket and share the port number with its
// public HTTPS listener: visitors and tunnels both want 443, and a product
// that told operators to publish apps on :8443 would not be deployable.
func (s *Server) ServeQUIC(udpConn net.PacketConn) error {
	tlsConf := s.cfg.TLSConfig.Clone()
	tlsConf.NextProtos = []string{ALPN}
	tlsConf.MinVersion = tls.VersionTLS13

	ln, err := quic.Listen(udpConn, tlsConf, quicConfig())
	if err != nil {
		return fmt.Errorf("tunnel: serve quic: %w", err)
	}
	s.quicLn = ln
	go s.acceptQUIC()
	return nil
}

// ServeConn adopts an already-accepted TCP connection as a tunnel session.
//
// The POP calls this after completing the TLS handshake and seeing ALPN
// negotiate to this protocol, which is how one 443 listener serves both
// visitor HTTPS and tunnel sessions. The connection is consumed: this method
// closes it on any failure and never returns it to the caller.
func (s *Server) ServeConn(tc *tls.Conn) {
	mux, err := yamux.Server(tc, yamuxConfig())
	if err != nil {
		s.reportError(tc.RemoteAddr(), fmt.Errorf("yamux server: %w", err))
		_ = tc.Close()
		return
	}
	s.handshake(&tcpSession{sess: mux, conn: tc, exporter: exportKeyingMaterial(tc.ConnectionState())})
}

// ALPNMatches reports whether a completed TLS handshake negotiated the Front
// Door protocol, so the POP can decide where to send the connection.
func ALPNMatches(cs tls.ConnectionState) bool { return cs.NegotiatedProtocol == ALPN }

func (s *Server) acceptQUIC() {
	for {
		conn, err := s.quicLn.Accept(s.ctx)
		if err != nil {
			return // listener closed
		}
		sess := &quicSession{conn: conn, exporter: exportKeyingMaterial(conn.ConnectionState().TLS)}
		go s.handshake(sess)
	}
}

func (s *Server) acceptTCP() {
	for {
		raw, err := s.tcpLn.Accept()
		if err != nil {
			return // listener closed
		}
		go func() {
			tc, ok := raw.(*tls.Conn)
			if !ok {
				_ = raw.Close()
				return
			}
			// Bound the TLS handshake: an unauthenticated peer must not be
			// able to hold a goroutine and a socket open indefinitely.
			_ = tc.SetDeadline(time.Now().Add(handshakeTimeout))
			if err := tc.HandshakeContext(s.ctx); err != nil {
				s.reportError(raw.RemoteAddr(), fmt.Errorf("tls handshake: %w", err))
				_ = raw.Close()
				return
			}
			_ = tc.SetDeadline(time.Time{})
			mux, err := yamux.Server(tc, yamuxConfig())
			if err != nil {
				s.reportError(raw.RemoteAddr(), fmt.Errorf("yamux server: %w", err))
				_ = raw.Close()
				return
			}
			s.handshake(&tcpSession{sess: mux, conn: tc, exporter: exportKeyingMaterial(tc.ConnectionState())})
		}()
	}
}

// handshake authenticates one freshly accepted session. On any failure the
// session is closed and the error reported — never fatal, since anything on
// the internet can open a connection here.
func (s *Server) handshake(sess Session) {
	accepted, err := s.authenticate(sess)
	if err != nil {
		s.reportError(sess.RemoteAddr(), err)
		_ = sess.Close()
		return
	}
	s.cfg.OnSession(accepted)
}

func (s *Server) authenticate(sess Session) (*AcceptedSession, error) {
	ctx, cancel := context.WithTimeout(s.ctx, handshakeTimeout)
	defer cancel()

	// The POP opens the control stream, not the client. QUIC does not
	// surface a stream to the peer until bytes are written on it, so
	// whichever side speaks first must also be the side that opens: if the
	// client opened and then waited for the challenge, neither end would
	// ever see the stream and the handshake would hang until timeout.
	st, err := sess.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open control stream: %w", err)
	}
	defer st.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	}

	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	if err := writeFrame(st, Challenge{
		Version: ProtocolVersion,
		Nonce:   hex.EncodeToString(nonce[:]),
		POPID:   s.cfg.POPID,
	}); err != nil {
		return nil, fmt.Errorf("send challenge: %w", err)
	}

	var req ConnectRequest
	if err := readFrame(st, &req); err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	if req.Version != ProtocolVersion {
		_ = writeFrame(st, ConnectResponse{
			Code:  "version",
			Error: fmt.Sprintf("this edge speaks Front Door v%d; your RackOps agent speaks v%d — run `rackops update`", ProtocolVersion, req.Version),
		})
		return nil, fmt.Errorf("%w: client v%d", ErrVersionMismatch, req.Version)
	}

	// Reject unknown tenants and bad signatures identically, so this
	// endpoint cannot be used to enumerate which tenant IDs exist.
	key, found := s.cfg.Auth.Lookup(req.TenantID)
	sig, sigErr := hex.DecodeString(req.Signature)
	ok := found && sigErr == nil && Verify(key, nonce[:], req.TenantID, sess.Exporter(), sig)
	if ok {
		// The client also sends the public key it thinks it holds. It is not
		// trusted for the decision — the enrolled key is authoritative — but
		// a mismatch means a rotated or stale key, which deserves a clearer
		// message than "signature failed".
		if claimed, err := hex.DecodeString(req.PublicKey); err == nil {
			if subtle.ConstantTimeCompare(claimed, key) != 1 {
				ok = false
			}
		}
	}
	if !ok {
		_ = writeFrame(st, ConnectResponse{
			Code:  "auth",
			Error: "this machine's Front Door key is not recognised by the edge — re-enroll from the RackOps console",
		})
		return nil, fmt.Errorf("%w: tenant %q", ErrAuthFailed, req.TenantID)
	}

	hostnames, quota, used, periodEnd, code, err := s.cfg.Auth.Authorize(req.TenantID)
	if err != nil {
		if code == "" {
			code = "internal"
		}
		_ = writeFrame(st, ConnectResponse{Code: code, Error: err.Error()})
		return nil, fmt.Errorf("authorize %q: %w", req.TenantID, err)
	}

	if err := writeFrame(st, ConnectResponse{
		OK:         true,
		Hostnames:  hostnames,
		QuotaBytes: quota,
		UsedBytes:  used,
		PeriodEnd:  periodEnd,
	}); err != nil {
		return nil, fmt.Errorf("send verdict: %w", err)
	}
	_ = st.SetDeadline(time.Time{})

	return &AcceptedSession{
		Session:   sess,
		TenantID:  req.TenantID,
		Hostnames: hostnames,
		AgentVer:  req.AgentVersion,
		QuotaByte: quota,
		UsedBytes: used,
		PeriodEnd: periodEnd,
	}, nil
}

func (s *Server) reportError(remote net.Addr, err error) {
	if s.cfg.OnError != nil {
		s.cfg.OnError(remote, err)
	}
}
