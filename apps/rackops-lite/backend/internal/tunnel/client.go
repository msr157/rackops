package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"
)

// ClientConfig configures one outbound tunnel session.
type ClientConfig struct {
	// POPAddr is the point of presence to connect to, as host:port. The
	// hostname is also used for TLS verification, so it must be the POP's
	// real name and not an IP.
	POPAddr string
	// TenantID identifies this install to the POP.
	TenantID string
	// PrivateKey signs the authentication challenge. It never leaves the
	// machine and is never sent.
	PrivateKey ed25519.PrivateKey
	// AgentVersion is reported for support and staged rollouts.
	AgentVersion string
	// Hostnames this install believes it serves (advisory; the POP decides).
	Hostnames []string
	// ForceTransport pins the transport to TransportQUIC or TransportTCP.
	// Empty means try QUIC and fall back to TCP, which is what production
	// should do — this exists for diagnostics and tests.
	ForceTransport string
	// TLSConfig overrides TLS settings. Nil means verify against the system
	// roots, which is correct in production. Tests supply their own roots.
	TLSConfig *tls.Config
}

// Connected is the outcome of a successful Connect: the live session plus
// what the POP told us about our own tenancy.
type Connected struct {
	Session   Session
	POPID     string
	Hostnames []string
	// QuotaBytes is 0 when the tenant has no transfer cap.
	QuotaBytes int64
	UsedBytes  int64
	PeriodEnd  time.Time
}

// Connect dials a POP, authenticates, and returns a live session.
//
// Transport selection is "try the good one, fall back to the one that always
// works": QUIC first, then TLS+yamux over TCP if QUIC cannot be established.
// UDP-blocking networks are common enough (hotels, campuses, corporate
// guest wifi, some mobile carriers) that a product promising "it just works"
// cannot ship QUIC-only.
//
// Authentication failures are NOT retried across transports: if the POP
// rejected our identity over QUIC it will reject it over TCP too, and
// retrying only obscures the real error.
func Connect(ctx context.Context, cfg ClientConfig) (*Connected, error) {
	if cfg.POPAddr == "" {
		return nil, errors.New("tunnel: no POP address configured")
	}
	if len(cfg.PrivateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("tunnel: invalid private key")
	}
	if cfg.TenantID == "" {
		return nil, errors.New("tunnel: no tenant ID configured")
	}

	tlsConf := cfg.TLSConfig.Clone()
	if tlsConf == nil {
		tlsConf = &tls.Config{}
	}
	if tlsConf.ServerName == "" {
		host, _, err := net.SplitHostPort(cfg.POPAddr)
		if err != nil {
			return nil, fmt.Errorf("tunnel: bad POP address %q: %w", cfg.POPAddr, err)
		}
		tlsConf.ServerName = host
	}
	tlsConf.NextProtos = []string{ALPN}
	tlsConf.MinVersion = tls.VersionTLS13

	order := []string{TransportQUIC, TransportTCP}
	if cfg.ForceTransport != "" {
		order = []string{cfg.ForceTransport}
	}

	var lastErr error
	for _, transport := range order {
		// QUIC gets a short leash so a UDP-blocking network costs seconds,
		// not a timeout the user watches. TCP is the last resort and gets
		// the full budget.
		budget := handshakeTimeout
		if transport == TransportQUIC && len(order) > 1 {
			budget = quicProbeTimeout
		}
		dialCtx, cancel := context.WithTimeout(ctx, budget)
		var sess Session
		var err error
		if transport == TransportQUIC {
			sess, err = dialQUIC(dialCtx, cfg.POPAddr, tlsConf)
		} else {
			sess, err = dialTCP(dialCtx, cfg.POPAddr, tlsConf)
		}
		cancel()
		if err != nil {
			// A QUIC dial that times out (rather than being refused) is the
			// signature of a network that silently drops UDP. Say so: it is
			// the difference between a support ticket that reads "tunnel is
			// slow to start" and one that reads "our firewall blocks UDP".
			if transport == TransportQUIC && (isTimeout(err) || errors.Is(err, context.DeadlineExceeded)) {
				err = fmt.Errorf("%w (UDP appears to be blocked on this network; falling back to TCP)", err)
			}
			lastErr = err
			continue // transport blocked or POP unreachable: try the next
		}

		res, err := authenticate(ctx, sess, cfg)
		if err != nil {
			_ = sess.Close()
			// A rejected identity is final — the next transport would be
			// rejected identically, and hiding that behind a fallback turns
			// a clear error into a mystery.
			if errors.Is(err, ErrAuthFailed) || errors.Is(err, ErrVersionMismatch) || errors.Is(err, ErrSuspended) {
				return nil, err
			}
			lastErr = err
			continue
		}
		return res, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no transport available")
	}
	return nil, fmt.Errorf("tunnel: connect to %s: %w", cfg.POPAddr, lastErr)
}

// authenticate runs the challenge/response exchange on a fresh session. The
// exchange happens on the first stream the client opens; data streams are
// opened by the POP afterwards, so there is no ambiguity about direction.
func authenticate(ctx context.Context, sess Session, cfg ClientConfig) (*Connected, error) {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	st, err := sess.Accept(ctx)
	if err != nil {
		return nil, fmt.Errorf("accept control stream: %w", err)
	}
	defer st.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	}

	var ch Challenge
	if err := readFrame(st, &ch); err != nil {
		return nil, fmt.Errorf("read challenge: %w", err)
	}
	if ch.Version != ProtocolVersion {
		return nil, fmt.Errorf("%w: POP speaks v%d, this agent speaks v%d", ErrVersionMismatch, ch.Version, ProtocolVersion)
	}
	nonce, err := hex.DecodeString(ch.Nonce)
	if err != nil || len(nonce) != 32 {
		return nil, errors.New("malformed challenge nonce")
	}

	pub := cfg.PrivateKey.Public().(ed25519.PublicKey)
	sig := Sign(cfg.PrivateKey, nonce, cfg.TenantID, sess.Exporter())
	req := ConnectRequest{
		Version:      ProtocolVersion,
		TenantID:     cfg.TenantID,
		PublicKey:    hex.EncodeToString(pub),
		Signature:    hex.EncodeToString(sig),
		AgentVersion: cfg.AgentVersion,
		Hostnames:    cfg.Hostnames,
	}
	if err := writeFrame(st, req); err != nil {
		return nil, fmt.Errorf("send credentials: %w", err)
	}

	var resp ConnectResponse
	if err := readFrame(st, &resp); err != nil {
		return nil, fmt.Errorf("read verdict: %w", err)
	}
	if !resp.OK {
		msg := resp.Error
		if msg == "" {
			msg = "the edge rejected this connection"
		}
		switch resp.Code {
		case "auth":
			return nil, fmt.Errorf("%w: %s", ErrAuthFailed, msg)
		case "version":
			return nil, fmt.Errorf("%w: %s", ErrVersionMismatch, msg)
		case "suspended":
			return nil, fmt.Errorf("%w: %s", ErrSuspended, msg)
		default:
			return nil, errors.New(msg)
		}
	}
	// Clear the handshake deadline: the session is long-lived from here and
	// a lingering deadline would kill it mid-conversation.
	_ = st.SetDeadline(time.Time{})

	return &Connected{
		Session:    sess,
		POPID:      ch.POPID,
		Hostnames:  resp.Hostnames,
		QuotaBytes: resp.QuotaBytes,
		UsedBytes:  resp.UsedBytes,
		PeriodEnd:  resp.PeriodEnd,
	}, nil
}
