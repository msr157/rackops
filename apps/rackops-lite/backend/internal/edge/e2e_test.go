package edge_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/edge"
	"github.com/rackops/rackops/internal/tunnel"
)

// harness is a complete Front Door in one process: a real POP with a real
// certificate, a real tunnel client dialling it, and a real HTTP backend
// standing in for a customer's app.
//
// This is the test that matters. Every other test in the package checks one
// component in isolation; this one proves that a visitor's HTTP request
// actually reaches an app on a machine with no inbound connectivity, which
// is the entire product claim.
type harness struct {
	edge     *edge.Edge
	registry *edge.Registry
	backend  *httptest.Server
	popAddr  string
	hostname string
	tenantID string
	clientTL *tls.Config
	priv     ed25519.PrivateKey
}

func newHarness(t *testing.T, backendHandler http.Handler) *harness {
	t.Helper()

	backend := httptest.NewServer(backendHandler)
	t.Cleanup(backend.Close)

	certs, err := edge.NewSelfSignedCertSource("link.rackops.in")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := edge.OpenRegistry(filepath.Join(t.TempDir(), "tenants.json"))
	if err != nil {
		t.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const hostname = "immich-acme.link.rackops.in"
	if _, err := registry.Upsert(edge.Tenant{
		ID:        "tenant-e2e",
		PublicKey: hex.EncodeToString(pub),
		Workspace: "acme",
		Hostnames: []string{hostname},
	}); err != nil {
		t.Fatal(err)
	}

	// Bind the tunnel and HTTPS listeners on free loopback ports.
	e, err := edge.New(edge.Config{
		POPID:      "pop-e2e",
		Zone:       "link.rackops.in",
		PublicAddr: "127.0.0.1:" + freePort(t),
		Certs:      certs,
		Registry:   registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Shutdown(ctx)
	})

	return &harness{
		edge:     e,
		registry: registry,
		backend:  backend,
		popAddr:  e.PublicAddr(),
		hostname: hostname,
		tenantID: "tenant-e2e",
		clientTL: trustFor(t, certs),
		priv:     priv,
	}
}

// trustFor builds a client TLS config that trusts the POP's certificate.
func trustFor(t *testing.T, certs *edge.CertSource) *tls.Config {
	t.Helper()
	cert, err := certs.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return &tls.Config{RootCAs: pool, ServerName: "localhost"}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// connect brings up the customer side: dial the POP, authenticate, and serve
// incoming streams by bridging them to the backend.
func (h *harness) connect(t *testing.T, transport string) *tunnel.Connected {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn, err := tunnel.Connect(ctx, tunnel.ClientConfig{
		POPAddr:        h.popAddr,
		TenantID:       h.tenantID,
		PrivateKey:     h.priv,
		AgentVersion:   "test",
		ForceTransport: transport,
		TLSConfig:      h.clientTL,
	})
	if err != nil {
		t.Fatalf("client could not connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Session.Close() })

	backendAddr := strings.TrimPrefix(h.backend.URL, "http://")
	fwd := &tunnel.Forwarder{
		Target: func(string) (string, error) { return backendAddr, nil },
	}
	go func() { _ = fwd.Serve(ctx, conn.Session) }()

	// The POP registers the session asynchronously; wait for it to be
	// routable rather than racing the first request.
	waitForSession(t, h)
	return conn
}

func waitForSession(t *testing.T, h *harness) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.edge.SessionCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("POP never registered the tunnel session")
}

// visit makes a real HTTPS request to the POP for the tenant's hostname.
func (h *harness) visit(t *testing.T, method, path string, body io.Reader) *http.Response {
	t.Helper()
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: h.clientTL,
			// Every hostname resolves to the POP's loopback listener.
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, h.edge.PublicAddr())
			},
		},
	}
	req, err := http.NewRequest(method, "https://"+h.hostname+path, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// ---- the headline test ----

// A visitor's HTTPS request must reach an app that never accepted an
// inbound connection, over both transports.
func TestEndToEndRequestReachesBackend(t *testing.T) {
	for _, transport := range []string{tunnel.TransportQUIC, tunnel.TransportTCP} {
		t.Run(transport, func(t *testing.T) {
			var gotHost, gotFwdProto, gotFwdFor string
			h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHost = r.Host
				gotFwdProto = r.Header.Get("X-Forwarded-Proto")
				gotFwdFor = r.Header.Get("X-Forwarded-For")
				fmt.Fprint(w, "hello from behind CGNAT")
			}))
			h.connect(t, transport)

			resp := h.visit(t, "GET", "/", nil)
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "hello from behind CGNAT" {
				t.Fatalf("body = %q", body)
			}

			// The backend must see the original hostname, or virtual-host
			// routing on the customer's side breaks.
			if gotHost != h.hostname {
				t.Errorf("backend saw Host %q, want %q", gotHost, h.hostname)
			}
			// It must believe the visitor used HTTPS, or apps generate
			// http:// links and redirect loops.
			if gotFwdProto != "https" {
				t.Errorf("X-Forwarded-Proto = %q, want https", gotFwdProto)
			}
			if gotFwdFor == "" {
				t.Error("X-Forwarded-For was not set")
			}
			if pop := resp.Header.Get("X-Rackops-Pop"); pop != "" && pop != "pop-e2e" {
				t.Errorf("X-Rackops-Pop = %q", pop)
			}
		})
	}
}

// Visitor HTTPS and tunnel sessions must share one TCP port, sorted by ALPN.
//
// This is what makes the POP deployable: a URL with no port means 443, and
// 443 is also the only outbound port restrictive networks reliably allow, so
// both roles need that exact number. If this regresses, one of them ends up
// on a port that cannot do its job.
func TestEndToEndSharesOnePublicPort(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "served")
	}))
	// The tunnel client dialled this address...
	h.connect(t, tunnel.TransportTCP)

	// ...and the visitor uses the very same one.
	if got := h.edge.PublicAddr(); got != h.popAddr {
		t.Fatalf("visitor address %q != tunnel address %q — the port is not shared", got, h.popAddr)
	}
	resp := h.visit(t, "GET", "/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "served" {
		t.Fatalf("body = %q", body)
	}

	// A plain TLS client that does not ask for the tunnel ALPN must be
	// treated as a browser, not handed to the tunnel server.
	plain := h.clientTL.Clone()
	plain.NextProtos = []string{"http/1.1"}
	c, err := tls.Dial("tcp", h.edge.PublicAddr(), plain)
	if err != nil {
		t.Fatalf("plain TLS dial rejected: %v", err)
	}
	defer c.Close()
	if got := c.ConnectionState().NegotiatedProtocol; got != "http/1.1" {
		t.Fatalf("negotiated %q, want http/1.1", got)
	}
}

// Uploads and downloads both cross the POP, so both must be billed.
func TestEndToEndMetersBothDirections(t *testing.T) {
	const downloadSize = 64 * 1024
	const uploadSize = 32 * 1024

	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		if n != uploadSize {
			t.Errorf("backend received %d upload bytes, want %d", n, uploadSize)
		}
		_, _ = w.Write(make([]byte, downloadSize))
	}))
	h.connect(t, tunnel.TransportTCP)

	before, _, _, _ := h.registry.Usage(h.tenantID)
	resp := h.visit(t, "POST", "/upload", strings.NewReader(strings.Repeat("u", uploadSize)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}

	// Usage is recorded once ServeHTTP returns; give it a moment to land.
	var after int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		after, _, _, _ = h.registry.Usage(h.tenantID)
		if after-before >= downloadSize+uploadSize {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	billed := after - before
	if billed < downloadSize+uploadSize {
		t.Fatalf("billed %d bytes, want at least %d (upload %d + download %d) — "+
			"one direction is not being metered",
			billed, downloadSize+uploadSize, uploadSize, downloadSize)
	}
}

// Server-sent events must stream, not buffer. A tunnel that holds the first
// chunk until the buffer fills turns a live log tail into a hung page.
func TestEndToEndStreamsWithoutBuffering(t *testing.T) {
	release := make(chan struct{})
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release // hold the response open
		fmt.Fprint(w, "data: second\n\n")
	}))
	h.connect(t, tunnel.TransportTCP)
	defer close(release)

	resp := h.visit(t, "GET", "/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// The first chunk must arrive while the handler is still blocked.
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	buf := make([]byte, 64)
	go func() {
		n, err := resp.Body.Read(buf)
		ch <- readResult{n, err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read: %v", got.err)
		}
		if !strings.Contains(string(buf[:got.n]), "first") {
			t.Fatalf("first chunk = %q", buf[:got.n])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first chunk never arrived — the response is being buffered")
	}
}

// Websocket-style upgrades ride the tunnel as raw bytes after the 101.
func TestEndToEndSupportsConnectionUpgrade(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "expected an upgrade", http.StatusBadRequest)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
		_, _ = io.Copy(conn, conn) // echo
	}))
	h.connect(t, tunnel.TransportTCP)

	conn, err := tls.Dial("tcp", h.edge.PublicAddr(), h.clientTL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n", h.hostname)
	buf := make([]byte, 128)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read upgrade response: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "101") {
		t.Fatalf("upgrade response = %q", buf[:n])
	}

	// Bytes must now flow raw in both directions.
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "ping") {
		t.Fatalf("echo = %q", buf[:n])
	}
}

// Suspending a tenant must cut traffic immediately, without a restart. This
// is the abuse kill switch, and "immediately" is the whole point of it.
func TestEndToEndSuspensionCutsTrafficImmediately(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "still here")
	}))
	h.connect(t, tunnel.TransportTCP)

	if resp := h.visit(t, "GET", "/", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("baseline status = %d", resp.StatusCode)
	}

	if !h.edge.SuspendTenant(h.tenantID, true, "phishing report #7") {
		t.Fatal("suspend returned false")
	}

	resp := h.visit(t, "GET", "/", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status after suspension = %d, want 503", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "phishing report #7") {
		t.Fatalf("suspension reason not shown: %s", body)
	}

	// The live session must have been dropped, not merely marked.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.edge.SessionCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session survived suspension — the kill switch only marked the tenant")
}

// Concurrent requests must not interfere: the multiplexer has to keep
// streams independent under load.
func TestEndToEndConcurrentRequests(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.URL.Path)
	}))
	h.connect(t, tunnel.TransportQUIC)

	const n = 25
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := fmt.Sprintf("/req-%d", i)
			resp := h.visit(t, "GET", path, nil)
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errs <- err
				return
			}
			if string(body) != path {
				errs <- fmt.Errorf("got %q, want %q — streams crossed", body, path)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
