package edge

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rackops/rackops/pkg/tunnel"
)

// Config configures a point of presence.
type Config struct {
	// POPID names this POP in logs, challenges, and support conversations.
	POPID string
	// Zone is the DNS suffix this POP serves, e.g. "link.rackops.in".
	// Requests for anything outside it are refused rather than proxied, so
	// a stray DNS record cannot turn the POP into an open relay.
	Zone string
	// PublicAddr is the one address this POP serves everything on, normally
	// ":443". Visitor HTTPS and tunnel sessions share it:
	//
	//   TCP  — one TLS listener, demultiplexed by ALPN. Tunnel clients
	//          negotiate "rackops-fd/1"; browsers negotiate h2 or http/1.1.
	//   UDP  — QUIC, tunnel sessions only.
	//
	// Sharing is not a space saving. Visitors need 443 and tunnel clients
	// need 443 (that is what survives networks which allow only HTTPS), so
	// two listeners would mean one of them lands on a port that does not
	// work for its job.
	PublicAddr string
	// HTTPAddr serves the plain-HTTP redirect to HTTPS. Empty disables it.
	HTTPAddr string
	// Certs supplies the wildcard certificate for Zone.
	Certs *CertSource
	// Registry is the tenant store.
	Registry *Registry
	// FlushInterval is how often usage counters are persisted.
	FlushInterval time.Duration
	// SupportEmail appears in visitor-facing error pages.
	SupportEmail string
}

// Edge is a running point of presence.
type Edge struct {
	cfg      Config
	sessions *SessionTable
	tunnelS  *tunnel.Server
	proxy    *httputil.ReverseProxy

	httpsSrv *http.Server
	httpSrv  *http.Server
	// httpsLn is the shared public TCP listener, retained so the
	// actually-bound address can be reported after listening on port 0.
	httpsLn net.Listener
	// udpConn backs the QUIC listener and is held so Close can release it.
	udpConn *net.UDPConn
	// visitorLn feeds ALPN-sorted browser connections to httpsSrv.
	visitorLn *queueListener
	// flushDone is closed by flushLoop once it has written the final usage
	// snapshot. Shutdown waits on it: exiting while that write is still in
	// flight would drop billing data and, worse, could leave a half-written
	// registry behind.
	flushDone chan struct{}

	// reqCount and byteCount are process-lifetime counters for the health
	// endpoint. They are advisory; per-tenant accounting lives in Registry.
	reqCount  atomic.Int64
	byteCount atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc
}

// New validates configuration and builds a POP.
func New(cfg Config) (*Edge, error) {
	if cfg.Registry == nil {
		return nil, errors.New("edge: Registry is required")
	}
	if cfg.Certs == nil {
		return nil, errors.New("edge: Certs is required")
	}
	if cfg.Zone == "" {
		return nil, errors.New("edge: Zone is required")
	}
	cfg.Zone = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cfg.Zone), "*."))
	if cfg.POPID == "" {
		cfg.POPID = "pop"
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 15 * time.Second
	}
	if cfg.SupportEmail == "" {
		cfg.SupportEmail = "support@rackops.in"
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := &Edge{
		cfg:      cfg,
		sessions: NewSessionTable(),
		ctx:      ctx,
		cancel:   cancel,
	}
	e.proxy = e.newProxy()

	ts, err := tunnel.NewServer(tunnel.ServerConfig{
		POPID:     cfg.POPID,
		TLSConfig: cfg.Certs.TLSConfig(),
		Auth:      cfg.Registry,
		OnSession: e.handleSession,
		OnError: func(remote net.Addr, err error) {
			// Handshake failures are routine on a public port — scanners,
			// probes, stale agents. Log at low volume and never fail.
			log.Printf("edge: tunnel handshake from %s failed: %v", remote, err)
		},
	})
	if err != nil {
		cancel()
		return nil, err
	}
	e.tunnelS = ts
	return e, nil
}

// newProxy builds the reverse proxy that carries visitor traffic into
// tunnels.
//
// Using net/http's ReverseProxy rather than piping raw bytes is deliberate.
// It gives correct hop-by-hop header handling, X-Forwarded-* population,
// websocket and SSE passthrough, HTTP/2 from the browser, and connection
// reuse — all things a byte pipe would have to reimplement, badly. The
// custom DialContext is the only unusual part: instead of dialling a
// backend address, it opens a stream on the right tenant's tunnel.
func (e *Edge) newProxy() *httputil.ReverseProxy {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			// addr is the outbound URL host, which Rewrite set to the
			// public hostname — that is how the pool stays keyed per tenant.
			host := normalizeHost(addr)
			t, ok := e.cfg.Registry.TenantForHost(host)
			if !ok {
				return nil, errNoSession
			}
			meta, _ := ctx.Value(dialMetaKey{}).(*dialMeta)
			remote, isTLS := "", true
			if meta != nil {
				remote, isTLS = meta.remoteAddr, meta.tls
			}
			return e.dialThroughSession(ctx, t.ID, host, remote, isTLS)
		},
		// The tunnel already multiplexes, so per-host connection limits here
		// only add queueing. Keep a healthy pool and let QUIC/yamux manage
		// concurrency.
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: time.Second,
		// Compression is left to the origin: re-compressing here would
		// break byte accounting and waste POP CPU on already-gzipped bodies.
		DisableCompression: true,
	}

	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = normalizeHost(pr.In.Host)
			// SetXForwarded writes X-Forwarded-For/Host/Proto from the
			// inbound connection, discarding any client-supplied values —
			// which is what stops a visitor from spoofing their own IP.
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			pr.Out.Header.Set("X-Rackops-Pop", e.cfg.POPID)
		},
		ErrorHandler:  e.proxyError,
		FlushInterval: -1, // stream responses immediately: SSE and log tails
		// must not sit in a buffer waiting for it to fill.
	}
}

// dialMeta carries per-request facts down to DialContext, which otherwise
// only sees an address.
type dialMeta struct {
	remoteAddr string
	tls        bool
}

type dialMetaKey struct{}

// ServeHTTP is the public entry point for visitor traffic.
func (e *Edge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := normalizeHost(r.Host)
	e.reqCount.Add(1)

	// Refuse anything outside the zone this POP serves. Without this a
	// dangling CNAME from any domain on the internet would make us proxy
	// for it — an open relay wearing our IP's reputation.
	if host != e.cfg.Zone && !strings.HasSuffix(host, "."+e.cfg.Zone) {
		e.writeStatus(w, r, http.StatusNotFound, "Unknown address",
			"This address is not served by RackOps Front Door.")
		return
	}

	t, ok := e.cfg.Registry.TenantForHost(host)
	if !ok {
		e.writeStatus(w, r, http.StatusNotFound, "No such app",
			"Nothing is published at this address. If you just created it, give it a few seconds.")
		return
	}
	if t.Suspended {
		reason := t.SuspendReason
		if reason == "" {
			reason = "This app has been taken offline."
		}
		e.writeStatus(w, r, http.StatusServiceUnavailable, "Temporarily offline", reason)
		return
	}
	used, quota, resets, _ := e.cfg.Registry.Usage(t.ID)
	if quota > 0 && used >= quota {
		e.writeStatus(w, r, http.StatusServiceUnavailable, "Transfer limit reached",
			fmt.Sprintf("This app has used its %s of monthly transfer. It comes back on %s.",
				humanBytes(quota), resets.Format("2 January")))
		return
	}
	if _, live := e.sessions.Get(t.ID); !live {
		e.writeStatus(w, r, http.StatusBadGateway, "App is offline",
			"The machine hosting this app is not connected right now.")
		return
	}

	// Meter the response. Request bodies are counted too: an upload costs
	// the POP the same bandwidth as a download.
	mw := &meteredWriter{ResponseWriter: w}
	var body *countingReadCloser
	if r.Body != nil {
		body = &countingReadCloser{ReadCloser: r.Body}
		r.Body = body
	}

	ctx := context.WithValue(r.Context(), dialMetaKey{}, &dialMeta{
		remoteAddr: r.RemoteAddr,
		tls:        r.TLS != nil,
	})
	e.proxy.ServeHTTP(mw, r.WithContext(ctx))

	// Read the counters inline, not from a deferred call: a defer would run
	// after this total is computed and silently bill every upload as zero.
	total := mw.n.Load()
	if body != nil {
		total += body.n.Load()
	}
	e.byteCount.Add(total)
	if over := e.cfg.Registry.AddUsage(t.ID, total); over {
		// Crossing the limit mid-session drops the tunnel rather than
		// waiting for the next connect. Leaving it up would let a single
		// runaway app keep spending after its allowance is gone.
		if e.sessions.CloseTenant(t.ID) {
			log.Printf("edge: tenant %s hit its transfer limit — session closed", t.ID)
		}
	}
}

// proxyError turns a transport failure into a page a human can act on.
// These are the moments a customer decides whether the product is reliable,
// so they get real sentences rather than "502 Bad Gateway".
func (e *Edge) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNoSession):
		e.writeStatus(w, r, http.StatusBadGateway, "App is offline",
			"The machine hosting this app disconnected. It returns automatically when the machine is back.")
	case errors.Is(err, errBackendDown):
		e.writeStatus(w, r, http.StatusBadGateway, "App is not responding",
			"The machine is connected, but the app behind this address did not answer. Check that it is running.")
	case errors.Is(err, context.Canceled):
		// The visitor went away mid-request. Nothing to report.
	default:
		log.Printf("edge: proxy error for %s: %v", r.Host, err)
		e.writeStatus(w, r, http.StatusBadGateway, "Could not reach the app",
			"Something went wrong forwarding this request.")
	}
}

// Start brings up the public listener and the plain-HTTP redirect.
func (e *Edge) Start() error {
	// One TCP socket for visitors and tunnels alike. The ALPN list decides
	// which is which after the handshake.
	tlsConf := e.cfg.Certs.TLSConfig()
	tlsConf.NextProtos = []string{tunnel.ALPN, "h2", "http/1.1"}

	ln, err := net.Listen("tcp", e.cfg.PublicAddr)
	if err != nil {
		return fmt.Errorf("edge: listen %s: %w", e.cfg.PublicAddr, err)
	}
	e.httpsLn = ln

	// QUIC rides the same port number on UDP. Binding it to whatever port TCP
	// actually got keeps ":0" working, which tests rely on.
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   ln.Addr().(*net.TCPAddr).IP,
		Port: ln.Addr().(*net.TCPAddr).Port,
	})
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("edge: listen udp %s: %w", ln.Addr(), err)
	}
	e.udpConn = udpConn
	if err := e.tunnelS.ServeQUIC(udpConn); err != nil {
		_ = ln.Close()
		_ = udpConn.Close()
		return err
	}

	e.httpsSrv = &http.Server{
		Handler: http.HandlerFunc(e.ServeHTTP),
		// Read headers promptly, but do not bound the whole request: uploads
		// and websockets are legitimate long-lived traffic.
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(discardWriter{}, "", 0),
	}
	// Visitor connections reach the HTTP server through a queue rather than a
	// socket, because the accept loop below has already consumed the socket
	// and completed TLS.
	e.visitorLn = newQueueListener(ln.Addr())
	go func() {
		if err := e.httpsSrv.Serve(e.visitorLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("edge: https server stopped: %v", err)
		}
	}()
	go e.acceptPublic(ln, tlsConf)

	log.Printf("edge: %s serving *.%s on %s (https + tunnels, tcp+quic)",
		e.cfg.POPID, e.cfg.Zone, ln.Addr())

	if e.cfg.HTTPAddr != "" {
		e.httpSrv = &http.Server{
			Addr:              e.cfg.HTTPAddr,
			Handler:           http.HandlerFunc(e.redirectToHTTPS),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if err := e.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("edge: http redirect server stopped: %v", err)
			}
		}()
	}

	e.flushDone = make(chan struct{})
	go e.flushLoop()
	return nil
}

// acceptPublic is the single front door onto the POP's TCP port. It performs
// the TLS handshake and routes by negotiated ALPN: tunnel clients go to the
// tunnel server, everything else to the HTTP server.
//
// Doing the handshake here rather than inside http.Server is what makes the
// split possible at all — ALPN is only known once the handshake completes.
func (e *Edge) acceptPublic(ln net.Listener, tlsConf *tls.Config) {
	for {
		raw, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		go func() {
			tc := tls.Server(raw, tlsConf)
			// An unauthenticated peer must not be able to hold a socket open
			// by starting a handshake and stalling.
			_ = tc.SetDeadline(time.Now().Add(30 * time.Second))
			if err := tc.HandshakeContext(e.ctx); err != nil {
				_ = tc.Close()
				return // scan, probe, or a client that gave up: routine noise
			}
			_ = tc.SetDeadline(time.Time{})

			if tunnel.ALPNMatches(tc.ConnectionState()) {
				e.tunnelS.ServeConn(tc)
				return
			}
			e.visitorLn.deliver(tc)
		}()
	}
}

// queueListener adapts already-accepted connections into a net.Listener so
// http.Server can drive them.
type queueListener struct {
	conns  chan net.Conn
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func newQueueListener(addr net.Addr) *queueListener {
	return &queueListener{
		// A small buffer absorbs bursts without letting a stalled HTTP server
		// queue connections indefinitely.
		conns:  make(chan net.Conn, 64),
		addr:   addr,
		closed: make(chan struct{}),
	}
}

func (l *queueListener) deliver(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.closed:
		_ = c.Close()
	}
}

func (l *queueListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *queueListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *queueListener) Addr() net.Addr { return l.addr }

// redirectToHTTPS sends plain HTTP visitors to the secure address. Front
// Door hostnames always have a valid certificate, so there is never a reason
// to serve them over plaintext.
func (e *Edge) redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	target := "https://" + normalizeHost(r.Host) + r.URL.RequestURI()
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// flushLoop persists usage counters on an interval.
func (e *Edge) flushLoop() {
	defer close(e.flushDone)
	t := time.NewTicker(e.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			// Final flush on the way out so a clean shutdown never loses
			// accounting.
			if err := e.cfg.Registry.Flush(); err != nil {
				log.Printf("edge: final registry flush failed: %v", err)
			}
			return
		case <-t.C:
			if err := e.cfg.Registry.Flush(); err != nil {
				log.Printf("edge: registry flush failed: %v", err)
			}
		}
	}
}

// Shutdown stops the POP, giving in-flight requests a chance to finish.
func (e *Edge) Shutdown(ctx context.Context) error {
	e.cancel()
	var firstErr error
	if e.httpSrv != nil {
		if err := e.httpSrv.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}
	if e.httpsSrv != nil {
		if err := e.httpsSrv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if e.visitorLn != nil {
		_ = e.visitorLn.Close()
	}
	if e.httpsLn != nil {
		_ = e.httpsLn.Close()
	}
	if err := e.tunnelS.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if e.udpConn != nil {
		_ = e.udpConn.Close()
	}
	// Wait for the flush loop's final write rather than racing it. If it
	// does not land in time, flush inline so usage is still persisted.
	if e.flushDone != nil {
		select {
		case <-e.flushDone:
		case <-ctx.Done():
			if err := e.cfg.Registry.Flush(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	if err := e.cfg.Registry.Flush(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Stats is a snapshot of a POP's lifetime counters and certificate health,
// for the operator console.
type Stats struct {
	Requests   int64
	Bytes      int64
	Sessions   int
	CertExpiry time.Time
	// SelfSigned reports that this POP is serving a development certificate
	// browsers will reject. It is the most common reason a fresh deployment
	// appears broken, so it is surfaced rather than left in the logs.
	SelfSigned bool
}

// Stats returns the current counters.
func (e *Edge) Stats() Stats {
	return Stats{
		Requests:   e.reqCount.Load(),
		Bytes:      e.byteCount.Load(),
		Sessions:   e.sessions.Count(),
		CertExpiry: e.cfg.Certs.Expiry(),
		SelfSigned: e.cfg.Certs.SelfSigned(),
	}
}

// Sessions lists the installs currently connected, for the console.
func (e *Edge) Sessions() []Snapshot { return e.sessions.Snapshot() }

// DisconnectTenant drops a tenant's live session without changing its
// registration. Used after a key rotation, where the old session is still
// authenticated against a credential that no longer exists.
func (e *Edge) DisconnectTenant(tenantID string) bool {
	return e.sessions.CloseTenant(tenantID)
}

// PublicAddr reports the address this POP actually bound, which differs from
// the configured one when the config asked for port 0. Visitors and tunnel
// clients both use it.
func (e *Edge) PublicAddr() string {
	if e.httpsLn != nil {
		return e.httpsLn.Addr().String()
	}
	return e.cfg.PublicAddr
}

// SessionCount reports how many installs are currently connected.
func (e *Edge) SessionCount() int { return e.sessions.Count() }

// SuspendTenant flips the kill switch and drops any live session in one
// step, so an abuse report can be acted on with a single call.
func (e *Edge) SuspendTenant(tenantID string, suspended bool, reason string) bool {
	if !e.cfg.Registry.SetSuspended(tenantID, suspended, reason) {
		return false
	}
	if suspended {
		e.sessions.CloseTenant(tenantID)
		log.Printf("edge: tenant %s suspended: %s", tenantID, reason)
	}
	return true
}

// discardWriter silences net/http's per-connection error logging, which is
// dominated by TLS scan noise on a public port.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
