package tunnel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- signing ----

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789abcdef0123456789abcdef")
	exporter := []byte("channel-binding")

	sig := Sign(priv, nonce, "tenant-1", exporter)
	if !Verify(pub, nonce, "tenant-1", exporter, sig) {
		t.Fatal("valid signature did not verify")
	}
}

// A signature must not be replayable with any field altered. Each case below
// is a real attack: a replayed nonce, a signature lifted onto another
// tenant, or a signature forwarded onto a different TLS session.
func TestVerifyRejectsTamperedTranscript(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789abcdef0123456789abcdef")
	exporter := []byte("channel-binding")
	sig := Sign(priv, nonce, "tenant-1", exporter)

	cases := []struct {
		name     string
		nonce    []byte
		tenant   string
		exporter []byte
	}{
		{"different nonce", []byte("ffffffffffffffffffffffffffffffff"), "tenant-1", exporter},
		{"different tenant", nonce, "tenant-2", exporter},
		{"different channel", nonce, "tenant-1", []byte("other-binding")},
		{"absent channel", nonce, "tenant-1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Verify(pub, tc.nonce, tc.tenant, tc.exporter, sig) {
				t.Fatal("signature verified against a tampered transcript")
			}
		})
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	nonce := []byte("0123456789abcdef0123456789abcdef")

	sig := Sign(priv, nonce, "tenant-1", nil)
	if Verify(otherPub, nonce, "tenant-1", nil, sig) {
		t.Fatal("signature verified under the wrong public key")
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	nonce := []byte("0123456789abcdef0123456789abcdef")
	sig := Sign(priv, nonce, "t", nil)

	if Verify(ed25519.PublicKey("short"), nonce, "t", nil, sig) {
		t.Fatal("accepted an undersized public key")
	}
	if Verify(pub, nonce, "t", nil, []byte("short")) {
		t.Fatal("accepted an undersized signature")
	}
}

// Length-prefixing every transcript field means field boundaries cannot be
// shifted: a tenant ID of "ab"+"c" must not sign the same bytes as "a"+"bc".
func TestTranscriptFieldsAreUnambiguous(t *testing.T) {
	a := Transcript([]byte("ab"), "c", nil)
	b := Transcript([]byte("a"), "bc", nil)
	if bytes.Equal(a, b) {
		t.Fatal("transcript is ambiguous across field boundaries")
	}
}

// ---- framing ----

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Challenge{Version: ProtocolVersion, Nonce: "abcd", POPID: "pop-1"}
	if err := writeFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out Challenge
	if err := readFrame(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip mismatch: got %+v want %+v", out, in)
	}
}

func TestFrameSequenceIsOrdered(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, Challenge{POPID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(&buf, Challenge{POPID: "second"}); err != nil {
		t.Fatal(err)
	}
	var a, b Challenge
	if err := readFrame(&buf, &a); err != nil {
		t.Fatal(err)
	}
	if err := readFrame(&buf, &b); err != nil {
		t.Fatal(err)
	}
	if a.POPID != "first" || b.POPID != "second" {
		t.Fatalf("frames out of order: %q then %q", a.POPID, b.POPID)
	}
}

// A hostile peer must not be able to make us allocate an arbitrary buffer by
// announcing a huge length.
func TestReadFrameRejectsOversizedLength(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	var out Challenge
	if err := readFrame(&buf, &out); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("got %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameRejectsTruncatedBody(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 64}) // claims 64 bytes
	buf.WriteString("{}")          // delivers 2
	var out Challenge
	if err := readFrame(&buf, &out); err == nil {
		t.Fatal("accepted a truncated frame")
	}
}

// ---- test PKI ----

// testTLS builds a throwaway CA and leaf certificate for "localhost" so
// tests exercise real TLS (and therefore real channel binding) rather than
// InsecureSkipVerify, which would leave the exporter path untested.
func testTLS(t *testing.T) (server *tls.Config, client *tls.Config) {
	t.Helper()

	caPub, caPriv, _ := ed25519.GenerateKey(rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rackops-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, caPub, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	leafPub, leafPriv, _ := ed25519.GenerateKey(rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, leafPub, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafPriv)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return &tls.Config{Certificates: []tls.Certificate{pair}},
		&tls.Config{RootCAs: pool, ServerName: "localhost"}
}

// ---- test authenticator ----

type testAuth struct {
	mu        sync.Mutex
	keys      map[string]ed25519.PublicKey
	hostnames []string
	quota     int64
	used      int64
	denyCode  string
	denyErr   error
}

func (a *testAuth) Lookup(tenantID string) (ed25519.PublicKey, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k, ok := a.keys[tenantID]
	return k, ok
}

func (a *testAuth) Authorize(string) ([]string, int64, int64, time.Time, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.denyErr != nil {
		return nil, 0, 0, time.Time{}, a.denyCode, a.denyErr
	}
	return a.hostnames, a.quota, a.used, time.Now().Add(time.Hour), "", nil
}

// newPair brings up a real tunnel server on loopback and returns its
// address, a client TLS config trusting it, and the channel of accepted
// sessions. testTLS mints a fresh CA per call, so the server's config and
// the client's must come from one invocation — hence the single helper.
func newPair(t *testing.T, auth Authenticator) (addr string, clientTLS *tls.Config, sessions chan *AcceptedSession) {
	t.Helper()
	serverTLS, clientTLS := testTLS(t)
	sessions = make(chan *AcceptedSession, 4)
	srv, err := NewServer(ServerConfig{
		POPID:     "pop-test",
		TLSConfig: serverTLS,
		Auth:      auth,
		OnSession: func(s *AcceptedSession) { sessions <- s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv.Addr().String(), clientTLS, sessions
}

// ---- end-to-end handshake, both transports ----

func TestConnectAndProxyBothTransports(t *testing.T) {
	for _, transport := range []string{TransportQUIC, TransportTCP} {
		t.Run(transport, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			auth := &testAuth{
				keys:      map[string]ed25519.PublicKey{"tenant-1": pub},
				hostnames: []string{"app.link.rackops.in"},
				quota:     100,
			}
			addr, clientTLS, sessions := newPair(t, auth)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			conn, err := Connect(ctx, ClientConfig{
				POPAddr:        addr,
				TenantID:       "tenant-1",
				PrivateKey:     priv,
				ForceTransport: transport,
				TLSConfig:      clientTLS,
			})
			if err != nil {
				t.Fatalf("connect: %v", err)
			}
			defer conn.Session.Close()

			if conn.Session.Transport() != transport {
				t.Fatalf("transport = %q, want %q", conn.Session.Transport(), transport)
			}
			if conn.POPID != "pop-test" {
				t.Fatalf("popID = %q", conn.POPID)
			}
			if len(conn.Hostnames) != 1 || conn.Hostnames[0] != "app.link.rackops.in" {
				t.Fatalf("hostnames = %v", conn.Hostnames)
			}

			var accepted *AcceptedSession
			select {
			case accepted = <-sessions:
			case <-time.After(10 * time.Second):
				t.Fatal("server never surfaced the session")
			}
			if accepted.TenantID != "tenant-1" {
				t.Fatalf("tenant = %q", accepted.TenantID)
			}

			// Channel binding must actually be in play on both transports —
			// if the exporter were silently nil, the handshake would still
			// pass and we would lose the MITM protection without noticing.
			if len(accepted.Session.Exporter()) == 0 {
				t.Fatal("no TLS exporter: channel binding is not active")
			}

			// Prove real data flows: POP opens a stream, client echoes.
			go func() {
				st, err := conn.Session.Accept(ctx)
				if err != nil {
					return
				}
				defer st.Close()
				var hdr StreamHeader
				if err := readFrame(st, &hdr); err != nil {
					return
				}
				_ = writeFrame(st, StreamAccept{OK: true})
				_, _ = io.Copy(st, st)
			}()

			st, err := accepted.Session.Open(ctx)
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			defer st.Close()
			if err := writeFrame(st, StreamHeader{Host: "app.link.rackops.in", RemoteAddr: "203.0.113.7:1234", TLS: true}); err != nil {
				t.Fatal(err)
			}
			var ack StreamAccept
			if err := readFrame(st, &ack); err != nil {
				t.Fatal(err)
			}
			if !ack.OK {
				t.Fatalf("stream rejected: %s", ack.Error)
			}
			payload := []byte("hello front door")
			if _, err := st.Write(payload); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(st, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("echo = %q, want %q", got, payload)
			}
		})
	}
}

func TestConnectRejectsUnknownTenant(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	auth := &testAuth{keys: map[string]ed25519.PublicKey{}}
	addr, clientTLS, _ := newPair(t, auth)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := Connect(ctx, ClientConfig{
		POPAddr:        addr,
		TenantID:       "nobody",
		PrivateKey:     priv,
		ForceTransport: TransportTCP,
		TLSConfig:      clientTLS,
	})
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("got %v, want ErrAuthFailed", err)
	}
}

// An install holding the wrong key for a real tenant must be rejected —
// this is the case that matters if a tenant ID ever leaks.
func TestConnectRejectsWrongKey(t *testing.T) {
	enrolledPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, attackerPriv, _ := ed25519.GenerateKey(rand.Reader)
	auth := &testAuth{keys: map[string]ed25519.PublicKey{"tenant-1": enrolledPub}}
	addr, clientTLS, _ := newPair(t, auth)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := Connect(ctx, ClientConfig{
		POPAddr:        addr,
		TenantID:       "tenant-1",
		PrivateKey:     attackerPriv,
		ForceTransport: TransportTCP,
		TLSConfig:      clientTLS,
	})
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("got %v, want ErrAuthFailed", err)
	}
}

func TestConnectSurfacesSuspension(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	auth := &testAuth{
		keys:     map[string]ed25519.PublicKey{"tenant-1": pub},
		denyCode: "suspended",
		denyErr:  errors.New("monthly transfer limit reached"),
	}
	addr, clientTLS, _ := newPair(t, auth)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := Connect(ctx, ClientConfig{
		POPAddr:        addr,
		TenantID:       "tenant-1",
		PrivateKey:     priv,
		ForceTransport: TransportTCP,
		TLSConfig:      clientTLS,
	})
	if !errors.Is(err, ErrSuspended) {
		t.Fatalf("got %v, want ErrSuspended", err)
	}
	// The operator's message must reach the customer intact; a generic
	// "connection failed" would send them to support instead of to billing.
	if !strings.Contains(err.Error(), "monthly transfer limit reached") {
		t.Fatalf("error lost the reason: %v", err)
	}
}

// Auth failures must be final. Retrying a rejected identity on the fallback
// transport would double the load on the POP and bury the real error.
func TestConnectDoesNotFallBackAfterAuthFailure(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	auth := &testAuth{keys: map[string]ed25519.PublicKey{}}
	addr, clientTLS, _ := newPair(t, auth)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := Connect(ctx, ClientConfig{
		POPAddr:    addr,
		TenantID:   "nobody",
		PrivateKey: priv,
		TLSConfig:  clientTLS,
		// No ForceTransport: QUIC is tried first and must not be followed
		// by a TCP attempt once the identity is rejected.
	})
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("got %v, want ErrAuthFailed", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("took %v — looks like it retried the second transport", elapsed)
	}
}

func TestConnectValidatesConfig(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	if _, err := Connect(ctx, ClientConfig{TenantID: "t", PrivateKey: priv}); err == nil {
		t.Fatal("accepted an empty POP address")
	}
	if _, err := Connect(ctx, ClientConfig{POPAddr: "pop:443", TenantID: "t"}); err == nil {
		t.Fatal("accepted a missing private key")
	}
	if _, err := Connect(ctx, ClientConfig{POPAddr: "pop:443", PrivateKey: priv}); err == nil {
		t.Fatal("accepted a missing tenant ID")
	}
}

func TestNewServerValidatesConfig(t *testing.T) {
	serverTLS, _ := testTLS(t)
	auth := &testAuth{}

	if _, err := NewServer(ServerConfig{TLSConfig: serverTLS, OnSession: func(*AcceptedSession) {}}); err == nil {
		t.Fatal("accepted a nil Authenticator")
	}
	if _, err := NewServer(ServerConfig{TLSConfig: serverTLS, Auth: auth}); err == nil {
		t.Fatal("accepted a nil OnSession")
	}
	if _, err := NewServer(ServerConfig{Auth: auth, OnSession: func(*AcceptedSession) {}}); err == nil {
		t.Fatal("accepted a missing certificate")
	}
}

// ---- splice ----

// Splice must not truncate a response when the request side finishes first —
// the classic half-close bug that corrupts large downloads.
func TestSpliceHalfCloseDeliversFullResponse(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	auth := &testAuth{keys: map[string]ed25519.PublicKey{"tenant-1": pub}}
	addr, clientTLS, sessions := newPair(t, auth)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := Connect(ctx, ClientConfig{
		POPAddr:        addr,
		TenantID:       "tenant-1",
		PrivateKey:     priv,
		ForceTransport: TransportTCP,
		TLSConfig:      clientTLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Session.Close()
	accepted := <-sessions

	// A local backend that reads a request to EOF, then writes a large body.
	body := bytes.Repeat([]byte("x"), 1<<20)
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		c, err := backend.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(io.Discard, c) // read until the peer half-closes
		_, _ = c.Write(body)
	}()

	// Client side: bridge an incoming tunnel stream to the backend.
	go func() {
		st, err := conn.Session.Accept(ctx)
		if err != nil {
			return
		}
		local, err := net.Dial("tcp", backend.Addr().String())
		if err != nil {
			_ = st.Close()
			return
		}
		Splice(st, local)
		_ = local.Close()
	}()

	st, err := accepted.Session.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	// Finish the request. The response must still arrive in full.
	if s, ok := st.(interface{ Close() error }); ok {
		_ = s.Close()
	}
	got, err := io.ReadAll(st)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read response: %v", err)
	}
	if len(got) != len(body) {
		t.Fatalf("response truncated: got %d bytes, want %d", len(got), len(body))
	}
}
