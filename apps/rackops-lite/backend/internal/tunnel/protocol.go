// Package tunnel implements the RackOps Front Door wire protocol: the
// transport that carries public HTTPS traffic from a vendor-run edge (POP)
// down to a customer's own Traefik edge running behind NAT/CGNAT.
//
// The shape is deliberately the inverse of a normal reverse proxy. The
// customer never accepts an inbound connection: the agent DIALS OUT to the
// POP over 443 and holds the session open, and the POP multiplexes each
// public request back down that session as a new stream. This is the same
// CGNAT-safe invariant the WireGuard mesh already relies on, and the same
// one the CRM channel holds — nothing ever dials into a customer network.
//
// Two transports are supported, negotiated by trying in order:
//
//	QUIC over UDP/443   — preferred. Native multiplexing, no head-of-line
//	                      blocking between concurrent requests, fast recovery
//	                      on the lossy home links this product targets.
//	TLS+yamux over TCP/443 — fallback. Indistinguishable from ordinary HTTPS
//	                      on the wire, so it survives hotel/corporate/campus
//	                      networks that drop UDP wholesale.
//
// Both are reduced to one interface (Session) so that everything above this
// package — the POP router, the agent client — is transport-agnostic.
//
// # Authentication
//
// Each RackOps install holds an Ed25519 key pair. The public key is
// registered with the vendor control plane out of band (enrollment); the
// private key never leaves the customer's machine. On connect, the POP sends
// a random 32-byte challenge, and the client must return a signature over a
// domain-separated transcript binding the challenge, the tenant ID, and the
// TLS channel. Signing a server-chosen nonce (rather than presenting a
// bearer token) means a compromised or malicious POP cannot replay a
// customer's credential to another POP.
//
// # Framing
//
// Control messages are length-prefixed JSON: a 4-byte big-endian length
// followed by that many bytes. Data streams carry one length-prefixed JSON
// header (StreamHeader) and are raw bidirectional bytes after that, so
// proxying is a plain io.Copy in both directions with no per-chunk overhead.
package tunnel

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Protocol identifiers and limits.
const (
	// ALPN is the TLS application protocol negotiated on both transports. It
	// keeps Front Door sessions from being confused with ordinary HTTPS by
	// anything terminating TLS in front of the POP.
	ALPN = "rackops-fd/1"

	// ProtocolVersion is bumped on incompatible wire changes. The POP
	// rejects clients whose major version it does not implement, with a
	// message the customer can act on rather than a silent hang.
	ProtocolVersion = 1

	// maxControlFrame bounds a single control message. Control frames are
	// small JSON objects; this exists so a hostile or broken peer cannot
	// make the other side allocate without limit.
	maxControlFrame = 1 << 20 // 1 MiB

	// handshakeTimeout bounds the whole authentication exchange. A client
	// that connects and then stalls holds a slot on the POP, so the deadline
	// is tight — this is not a user-facing operation.
	handshakeTimeout = 20 * time.Second

	// quicProbeTimeout bounds only the QUIC dial, before falling back to
	// TCP. It is deliberately far shorter than handshakeTimeout: on a
	// network that silently drops UDP there is nothing to wait for, and the
	// user is staring at a spinner the whole time. Five seconds is generous
	// for a handshake that normally completes in one round trip, and keeps
	// the worst-case "click to public URL" well inside a few seconds.
	quicProbeTimeout = 5 * time.Second

	// KeepAlive is how often an idle session sends traffic to keep NAT
	// bindings alive. Home routers routinely drop idle UDP mappings after
	// 30s, so this must stay comfortably below that.
	KeepAlive = 15 * time.Second

	// IdleTimeout is how long a session may go without any activity before
	// it is considered dead. Must be greater than KeepAlive.
	IdleTimeout = 60 * time.Second
)

// Errors returned by the handshake. They are distinguishable so the client
// can decide whether to retry the same POP, try another, or stop and surface
// a message to the user — retrying forever against a rejected key is how you
// build a bill instead of a product.
var (
	// ErrAuthFailed means the signature or tenant identity was rejected.
	// Retrying will not help until the operator fixes enrollment.
	ErrAuthFailed = errors.New("tunnel: authentication rejected")

	// ErrVersionMismatch means the POP does not speak this client's
	// protocol version. The customer needs to update; retrying will not help.
	ErrVersionMismatch = errors.New("tunnel: protocol version not supported")

	// ErrSuspended means the tenant exists but is administratively disabled
	// (quota exhausted, abuse hold, unpaid). Retrying is pointless until the
	// state changes, so the client backs off hard rather than hammering.
	ErrSuspended = errors.New("tunnel: tenant suspended")

	// ErrFrameTooLarge means the peer announced an oversized control frame.
	ErrFrameTooLarge = errors.New("tunnel: control frame too large")
)

// ConnectRequest is the client's opening control message.
type ConnectRequest struct {
	Version int `json:"version"`
	// TenantID identifies the RackOps install. Hostnames are mapped to
	// tenants at the POP, so this is the only identity the client asserts.
	TenantID string `json:"tenantId"`
	// PublicKey is the install's Ed25519 public key, hex-encoded. The POP
	// checks it against the enrolled key for TenantID rather than trusting
	// it — it is sent so the POP can produce a precise error when an install
	// presents a key that was rotated or never enrolled.
	PublicKey string `json:"publicKey"`
	// Signature is hex-encoded Ed25519 over the transcript (see Transcript).
	Signature string `json:"signature"`
	// AgentVersion is reported for support and for staged rollouts.
	AgentVersion string `json:"agentVersion,omitempty"`
	// Hostnames the client believes it serves. The POP treats these as a
	// hint for logging only; the authoritative mapping lives at the POP so a
	// compromised client cannot claim another tenant's hostname.
	Hostnames []string `json:"hostnames,omitempty"`
}

// Challenge is the POP's first message, sent before the client authenticates.
type Challenge struct {
	Version int `json:"version"`
	// Nonce is 32 random bytes, hex-encoded, generated per connection.
	Nonce string `json:"nonce"`
	// POPID identifies which point of presence answered, so the client can
	// report it and support can trace a session to a machine.
	POPID string `json:"popId"`
}

// ConnectResponse is the POP's verdict on a ConnectRequest.
type ConnectResponse struct {
	OK bool `json:"ok"`
	// Code is a machine-readable rejection reason: "auth", "version",
	// "suspended", or "internal". Empty when OK.
	Code string `json:"code,omitempty"`
	// Error is a human-readable message shown to the customer verbatim, so
	// it must be written for them and not for a log grep.
	Error string `json:"error,omitempty"`
	// Hostnames the POP will route to this session. This is authoritative
	// and may differ from what the client requested.
	Hostnames []string `json:"hostnames,omitempty"`
	// QuotaBytes is the tenant's monthly transfer allowance, 0 for
	// unlimited. Sent so the console can show usage without a second API.
	QuotaBytes int64 `json:"quotaBytes,omitempty"`
	// UsedBytes is consumption so far in the current period.
	UsedBytes int64 `json:"usedBytes,omitempty"`
	// PeriodEnd is when UsedBytes resets. omitzero (not omitempty, which has
	// no effect on structs) keeps an unset time out of the wire format.
	PeriodEnd time.Time `json:"periodEnd,omitzero"`
}

// StreamHeader is the first message on every data stream, telling the client
// which of its local backends to connect the stream to.
type StreamHeader struct {
	// Host is the public hostname the visitor requested, already validated
	// at the POP against this tenant's routes.
	Host string `json:"host"`
	// RemoteAddr is the visitor's IP:port, forwarded so the customer's
	// Traefik can log and rate-limit real client addresses.
	RemoteAddr string `json:"remoteAddr"`
	// TLS reports whether the visitor's connection to the POP used HTTPS,
	// so the customer's edge can set X-Forwarded-Proto correctly.
	TLS bool `json:"tls"`
}

// StreamAccept is the client's reply on a data stream, sent before any
// payload. It lets the POP return a proper 502 to the visitor when the
// customer's backend is down, instead of the connection dying mid-response.
type StreamAccept struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Transcript builds the bytes signed during authentication.
//
// It is domain-separated by a fixed prefix and binds three things: the POP's
// nonce (so a signature cannot be replayed), the tenant ID (so a signature
// for one tenant cannot be presented for another), and the TLS exporter
// value for the underlying connection (so a signature captured by a
// man-in-the-middle cannot be forwarded onto a different TLS session).
//
// The exporter may be nil for transports that cannot provide one; the
// transcript stays well-defined, and channel binding is simply absent.
func Transcript(nonce []byte, tenantID string, exporter []byte) []byte {
	h := sha256.New()
	// Fixed prefix: makes this signature meaningless in any other protocol
	// that might one day use the same key.
	h.Write([]byte("rackops-frontdoor-v1\x00"))
	writeLenPrefixed(h, nonce)
	writeLenPrefixed(h, []byte(tenantID))
	writeLenPrefixed(h, exporter)
	return h.Sum(nil)
}

// writeLenPrefixed writes a 4-byte length followed by the bytes, so that
// distinct field boundaries cannot be forged by shifting bytes between
// adjacent fields.
func writeLenPrefixed(w io.Writer, b []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	_, _ = w.Write(n[:])
	_, _ = w.Write(b)
}

// Sign produces the authentication signature for a challenge.
func Sign(key ed25519.PrivateKey, nonce []byte, tenantID string, exporter []byte) []byte {
	return ed25519.Sign(key, Transcript(nonce, tenantID, exporter))
}

// Verify checks an authentication signature.
func Verify(key ed25519.PublicKey, nonce []byte, tenantID string, exporter []byte, sig []byte) bool {
	if len(key) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(key, Transcript(nonce, tenantID, exporter), sig)
}

// ---- data stream handshake ----
//
// Every data stream begins with a StreamHeader from the POP and a
// StreamAccept from the client. The acknowledgement is what lets the POP
// return a clean 502 when the customer's backend is down: without it, the
// POP would have already committed to the visitor's response before
// discovering there was nothing to proxy to, and the visitor would see a
// truncated response instead of an error page.

// WriteStreamHeader sends the opening header on a data stream.
func WriteStreamHeader(w io.Writer, h StreamHeader) error { return writeFrame(w, h) }

// ReadStreamHeader reads the opening header on a data stream.
func ReadStreamHeader(r io.Reader) (StreamHeader, error) {
	var h StreamHeader
	err := readFrame(r, &h)
	return h, err
}

// WriteStreamAccept sends the client's verdict on a data stream.
func WriteStreamAccept(w io.Writer, a StreamAccept) error { return writeFrame(w, a) }

// ReadStreamAccept reads the client's verdict on a data stream.
func ReadStreamAccept(r io.Reader) (StreamAccept, error) {
	var a StreamAccept
	err := readFrame(r, &a)
	return a, err
}

// ---- control frame codec ----

// writeFrame writes one length-prefixed JSON control message.
func writeFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("tunnel: encode frame: %w", err)
	}
	if len(body) > maxControlFrame {
		return ErrFrameTooLarge
	}
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(body)))
	copy(buf[4:], body)
	// One Write for header+body: on a stream transport a torn write would
	// otherwise let a slow peer see a length with no payload behind it.
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("tunnel: write frame: %w", err)
	}
	return nil
}

// readFrame reads one length-prefixed JSON control message into v.
func readFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return fmt.Errorf("tunnel: read frame header: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxControlFrame {
		return ErrFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return fmt.Errorf("tunnel: read frame body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("tunnel: decode frame: %w", err)
	}
	return nil
}
