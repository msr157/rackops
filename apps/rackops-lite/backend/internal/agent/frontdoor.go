package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
	"github.com/rackops/rackops/pkg/tunnel"
)

// frontDoor holds the agent's Front Door state: the install's signing
// identity and the supervised outbound session to the vendor edge.
//
// The client runs in-process rather than as a helper container, unlike the
// Cloudflare quick tunnel it supersedes. That removes the image pull that
// makes quick tunnels take ~15 seconds to produce a URL on first use, and it
// removes the dependency on a container runtime being present at all.
type frontDoor struct {
	mu sync.Mutex
	// key signs the POP's authentication challenge. Persisted once per
	// install and never transmitted.
	key ed25519.PrivateKey
	// cancel stops the running supervisor, if any.
	cancel context.CancelFunc
	// spec is the assignment currently being served.
	spec types.FrontDoorSpec
	// state is reported back to the control plane on every push so the
	// console can show the truth rather than an optimistic guess.
	state types.FrontDoorState
	// fwd serves streams for the live session; nil when disconnected.
	fwd *tunnel.Forwarder
}

// initFrontDoorKey loads or creates the install's Front Door signing key.
//
// The key lives beside the mesh key and the node identity, with the same
// 0600 permissions: it is the credential that proves this install owns its
// public hostnames, so leaking it would let someone else serve traffic under
// the customer's URL.
func (a *Agent) initFrontDoorKey() error {
	keyPath := filepath.Join(a.cfg.DataDir, "frontdoor.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		raw, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(raw) == ed25519.PrivateKeySize {
			a.fd.key = ed25519.PrivateKey(raw)
			return nil
		}
		// A corrupt key file is replaced rather than fatal: regenerating
		// costs one re-enrollment, while refusing to start would take the
		// whole node offline over a feature it may not even use.
		log.Printf("frontdoor: key file unreadable, generating a new identity")
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate front door key: %w", err)
	}
	a.fd.key = priv
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(priv)), 0o600); err != nil {
		return fmt.Errorf("write front door key: %w", err)
	}
	return nil
}

// frontDoorState returns a snapshot for the next agent report.
func (a *Agent) frontDoorState() types.FrontDoorState {
	a.fd.mu.Lock()
	defer a.fd.mu.Unlock()
	st := a.fd.state
	if a.fd.fwd != nil {
		streams, up, down := a.fd.fwd.Stats()
		st.OpenStreams = int(streams)
		st.BytesUp, st.BytesDown = up, down
	}
	st.PublicKey = a.frontDoorPublicKeyLocked()
	return st
}

// frontDoorPublicKeyLocked returns the install's hex public key. Callers
// must hold fd.mu — it exists as a separate method precisely so
// frontDoorState can reuse it without re-entering a non-reentrant mutex.
func (a *Agent) frontDoorPublicKeyLocked() string {
	if len(a.fd.key) != ed25519.PrivateKeySize {
		return ""
	}
	return hex.EncodeToString(a.fd.key.Public().(ed25519.PublicKey))
}

// syncFrontDoor reconciles the running session with the control plane's
// assignment. It is called on every report, so it must be cheap and
// idempotent: the common case is "nothing changed" and must do no work.
func (a *Agent) syncFrontDoor(ctx context.Context, spec *types.FrontDoorSpec) {
	a.fd.mu.Lock()
	defer a.fd.mu.Unlock()

	// No assignment, or explicitly disabled: stop whatever is running.
	if spec == nil || !spec.Enabled {
		if a.fd.cancel != nil {
			a.fd.cancel()
			a.fd.cancel = nil
			a.fd.fwd = nil
			a.fd.spec = types.FrontDoorSpec{}
			a.fd.state = types.FrontDoorState{}
			log.Printf("frontdoor: disconnected (disabled by the control plane)")
		}
		return
	}

	// Already serving this exact assignment: nothing to do.
	if a.fd.cancel != nil && a.fd.spec.Equal(*spec) {
		return
	}

	// Anything else is a change of substance — POP, tenant, or routes — so
	// restart the session against the new assignment.
	if a.fd.cancel != nil {
		a.fd.cancel()
		a.fd.cancel = nil
	}
	a.fd.spec = *spec
	a.fd.state = types.FrontDoorState{Status: types.FrontDoorConnecting}

	runCtx, cancel := context.WithCancel(ctx)
	a.fd.cancel = cancel
	go a.runFrontDoor(runCtx, *spec)
}

// closeFrontDoor tears the session down on agent shutdown.
func (a *Agent) closeFrontDoor() {
	a.fd.mu.Lock()
	defer a.fd.mu.Unlock()
	if a.fd.cancel != nil {
		a.fd.cancel()
		a.fd.cancel = nil
	}
}

// runFrontDoor supervises one Front Door assignment for as long as it is
// current, reconnecting with backoff whenever the session drops.
//
// Reconnection policy is the difference between a tunnel that survives a
// home internet connection and one that needs a human. Transient failures
// (ISP blips, POP restarts, laptop lid) retry with exponential backoff and
// jitter; permanent ones (rejected key, suspended account) stop and surface
// a message, because retrying those forever would spend the customer's
// battery and our capacity to accomplish nothing.
func (a *Agent) runFrontDoor(ctx context.Context, spec types.FrontDoorSpec) {
	const (
		minBackoff = 2 * time.Second
		maxBackoff = 2 * time.Minute
	)
	backoff := minBackoff

	for ctx.Err() == nil {
		conn, err := tunnel.Connect(ctx, tunnel.ClientConfig{
			POPAddr:      spec.POPAddr,
			TenantID:     spec.TenantID,
			PrivateKey:   a.frontDoorKey(),
			AgentVersion: a.cfg.Version,
			Hostnames:    spec.Hostnames,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Permanent rejections stop the loop. The state carries the
			// reason so the console can show it instead of a spinner.
			if errors.Is(err, tunnel.ErrAuthFailed) ||
				errors.Is(err, tunnel.ErrVersionMismatch) ||
				errors.Is(err, tunnel.ErrSuspended) {
				a.setFrontDoorState(types.FrontDoorState{
					Status:  types.FrontDoorRejected,
					Message: cleanTunnelError(err),
				})
				log.Printf("frontdoor: refused by %s: %v", spec.POPAddr, err)
				return
			}
			a.setFrontDoorState(types.FrontDoorState{
				Status:  types.FrontDoorConnecting,
				Message: cleanTunnelError(err),
			})
			log.Printf("frontdoor: connect to %s failed: %v (retrying in %s)", spec.POPAddr, err, backoff.Round(time.Second))
			if !sleepCtx(ctx, jitter(backoff)) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Connected. Reset the backoff so the next unrelated drop retries
		// promptly rather than inheriting a long delay from an old outage.
		backoff = minBackoff
		fwd := &tunnel.Forwarder{Target: a.frontDoorTarget(spec)}

		a.fd.mu.Lock()
		a.fd.fwd = fwd
		a.fd.state = types.FrontDoorState{
			Status:      types.FrontDoorConnected,
			POPID:       conn.POPID,
			Transport:   conn.Session.Transport(),
			Hostnames:   conn.Hostnames,
			QuotaBytes:  conn.QuotaBytes,
			UsedBytes:   conn.UsedBytes,
			ConnectedAt: time.Now(),
		}
		a.fd.mu.Unlock()
		log.Printf("frontdoor: connected to %s over %s serving %s",
			conn.POPID, conn.Session.Transport(), strings.Join(conn.Hostnames, ", "))

		err = fwd.Serve(ctx, conn.Session)
		_ = conn.Session.Close()

		a.fd.mu.Lock()
		a.fd.fwd = nil
		if ctx.Err() == nil {
			a.fd.state = types.FrontDoorState{Status: types.FrontDoorConnecting, Message: "reconnecting"}
		}
		a.fd.mu.Unlock()

		if ctx.Err() != nil {
			return
		}
		log.Printf("frontdoor: session to %s ended: %v (reconnecting)", conn.POPID, err)
		if !sleepCtx(ctx, jitter(minBackoff)) {
			return
		}
	}
}

// frontDoorTarget builds the hostname-to-local-address resolver for a
// session.
//
// Every Front Door hostname resolves to this node's Traefik edge, which then
// routes by Host header exactly as it does for a domain the customer owns.
// Reusing that path means Front Door inherits per-app routing, middlewares
// and health checks for free, and there is only one place where "which app
// answers this hostname" is decided.
func (a *Agent) frontDoorTarget(spec types.FrontDoorSpec) func(string) (string, error) {
	allowed := make(map[string]struct{}, len(spec.Hostnames))
	for _, h := range spec.Hostnames {
		allowed[strings.ToLower(h)] = struct{}{}
	}
	target := spec.LocalTarget
	return func(host string) (string, error) {
		if target == "" {
			return "", errors.New("this machine has no edge proxy running to serve the request")
		}
		// Defence in depth: the POP already refuses hostnames a tenant does
		// not own, but a compromised POP must not be able to aim this
		// machine's tunnel at an arbitrary local service.
		if _, ok := allowed[strings.ToLower(stripPort(host))]; !ok {
			return "", errors.New("this address is not served by this machine")
		}
		return target, nil
	}
}

func (a *Agent) frontDoorKey() ed25519.PrivateKey {
	a.fd.mu.Lock()
	defer a.fd.mu.Unlock()
	return a.fd.key
}

func (a *Agent) setFrontDoorState(st types.FrontDoorState) {
	a.fd.mu.Lock()
	defer a.fd.mu.Unlock()
	a.fd.state = st
}

// stripPort removes a trailing :port from a Host header value.
func stripPort(host string) string {
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i+1:], "]") {
		return host[:i]
	}
	return host
}

// cleanTunnelError trims Go error decoration so the console shows the
// sentence a customer can act on rather than a chain of wrapped prefixes.
func cleanTunnelError(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i > -1 && i+2 < len(msg) {
		tail := msg[i+2:]
		// Keep the tail only when it reads like a sentence, not a bare
		// syscall string.
		if strings.Contains(tail, " ") {
			return tail
		}
	}
	return msg
}

// jitter spreads reconnect attempts so a POP restart does not bring every
// install on it back in the same instant — a thundering herd that would
// turn a five-second blip into a minutes-long outage.
func jitter(d time.Duration) time.Duration {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return d
	}
	n := uint64(0)
	for _, c := range b {
		n = n<<8 | uint64(c)
	}
	// Spread across [d, 1.5d).
	return d + time.Duration(n%uint64(d/2+1))
}
