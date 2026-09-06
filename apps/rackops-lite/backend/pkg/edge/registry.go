// Package edge implements the RackOps Front Door point of presence: the
// vendor-run process that holds the public IP, terminates TLS for
// *.link.rackops.in, and forwards each request down a tunnel session to the
// customer's own edge running behind NAT.
//
// The POP is the only part of RackOps that is not on the customer's
// hardware, which makes it the only part that carries hosting-provider
// liability. Three things are therefore not optional and are enforced here
// rather than bolted on later: every byte is metered, every tenant can be
// suspended instantly, and no tenant can route a hostname it does not own.
package edge

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultQuotaBytes is the free tier: 100 GB of public transfer per tenant
// per calendar month. Metering is on from the first byte even for tenants
// with no cap — retrofitting usage accounting onto live workloads means
// either guessing at history or surprising customers with a limit they had
// no way to see coming.
const DefaultQuotaBytes int64 = 100 << 30

// Tenant is one RackOps install's Front Door registration.
type Tenant struct {
	ID string `json:"id"`
	// PublicKey is the install's enrolled Ed25519 key, hex-encoded. This is
	// the sole authority on identity; a client's claimed key is checked
	// against it, never trusted in its place.
	PublicKey string `json:"publicKey"`
	// Workspace is the DNS label the tenant's hostnames are built from,
	// reserved at enrollment so two installs cannot collide.
	Workspace string `json:"workspace"`
	// Hostnames this tenant may serve. Authoritative: the POP routes only
	// what is listed here, so a compromised agent cannot claim someone
	// else's name by asserting it at connect time.
	Hostnames []string `json:"hostnames"`
	// QuotaBytes caps transfer per period; 0 means unlimited.
	QuotaBytes int64 `json:"quotaBytes"`
	// UsedBytes is consumption in the current period.
	UsedBytes int64 `json:"usedBytes"`
	// PeriodStart is when the current metering period began. Periods are
	// calendar months in UTC.
	PeriodStart time.Time `json:"periodStart"`
	// Suspended stops all traffic and refuses new sessions. This is the
	// abuse kill switch; it must work without a restart and without a
	// deploy, because the first phishing report will not wait for either.
	Suspended bool `json:"suspended"`
	// SuspendReason is shown to the customer and returned to visitors, so
	// it is written for them: "monthly transfer limit reached", not "ERR_Q".
	SuspendReason string `json:"suspendReason,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// overQuota reports whether the tenant has exhausted its allowance.
func (t *Tenant) overQuota() bool {
	return t.QuotaBytes > 0 && t.UsedBytes >= t.QuotaBytes
}

// Registry is the POP's tenant store: an in-memory index kept consistent
// with a JSON file on disk.
//
// Reads happen on every request and must not touch the disk; writes are
// frequent but tiny (usage counters) and are flushed on an interval rather
// than per-request, so a busy POP does not turn every HTTP response into an
// fsync. A crash loses at most one flush interval of usage accounting, which
// is the right trade: under-counting a few seconds of transfer is cheaper
// than serializing every request behind a disk write.
type Registry struct {
	path string

	mu       sync.RWMutex
	flushMu  sync.Mutex
	tenants  map[string]*Tenant // tenant ID -> tenant
	byHost   map[string]string  // hostname -> tenant ID
	dirty    bool
	suspOnce map[string]bool // tenants already logged as suspended, to keep logs quiet
}

// OpenRegistry loads the registry at path, creating an empty one if absent.
func OpenRegistry(path string) (*Registry, error) {
	r := &Registry{
		path:     path,
		tenants:  map[string]*Tenant{},
		byHost:   map[string]string{},
		suspOnce: map[string]bool{},
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("edge: create state dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("edge: read registry: %w", err)
	}
	var tenants []*Tenant
	if err := json.Unmarshal(data, &tenants); err != nil {
		return nil, fmt.Errorf("edge: parse registry: %w", err)
	}
	for _, t := range tenants {
		r.tenants[t.ID] = t
		for _, h := range t.Hostnames {
			r.byHost[strings.ToLower(h)] = t.ID
		}
	}
	return r, nil
}

// Flush writes the registry to disk if anything changed since the last call.
// Writes go to a temp file and are renamed into place, so a crash mid-write
// leaves the previous good registry rather than a truncated one.
func (r *Registry) Flush() error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return nil
	}
	list := make([]*Tenant, 0, len(r.tenants))
	for _, t := range r.tenants {
		cp := *t
		list = append(list, &cp)
	}
	r.dirty = false
	r.mu.Unlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		r.markDirty()
		return fmt.Errorf("edge: encode registry: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		r.markDirty()
		return fmt.Errorf("edge: write registry: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		r.markDirty()
		return fmt.Errorf("edge: replace registry: %w", err)
	}
	return nil
}

func (r *Registry) markDirty() {
	r.mu.Lock()
	r.dirty = true
	r.mu.Unlock()
}

// Upsert creates or updates a tenant, replacing its hostname set.
func (r *Registry) Upsert(t Tenant) (*Tenant, error) {
	if t.ID == "" {
		return nil, errors.New("tenant id is required")
	}
	key, err := hex.DecodeString(t.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("public key must be a hex-encoded Ed25519 key")
	}
	for _, h := range t.Hostnames {
		if !validHostname(h) {
			return nil, fmt.Errorf("invalid hostname %q", h)
		}
	}
	if t.Workspace != "" && (len(t.Workspace) > 63 || strings.Contains(t.Workspace, ".") || strings.ToLower(t.Workspace) != t.Workspace || !validHostname(t.Workspace+".example")) {
		return nil, errors.New("workspace must be a lowercase DNS label")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if t.Workspace != "" {
		for id, existing := range r.tenants {
			if id != t.ID && existing.Workspace == t.Workspace {
				return nil, errors.New("workspace already belongs to another tenant")
			}
		}
	}

	// A hostname may belong to exactly one tenant. Rejecting the whole
	// upsert (rather than silently stealing the name) is what stops a
	// misconfigured enrollment from hijacking a live customer's URL.
	for _, h := range t.Hostnames {
		if owner, ok := r.byHost[strings.ToLower(h)]; ok && owner != t.ID {
			return nil, fmt.Errorf("hostname %q already belongs to another tenant", h)
		}
	}

	now := time.Now().UTC()
	existing, ok := r.tenants[t.ID]
	if !ok {
		if t.QuotaBytes == 0 {
			t.QuotaBytes = DefaultQuotaBytes
		}
		t.CreatedAt, t.UpdatedAt = now, now
		t.PeriodStart = monthStart(now)
		cp := t
		r.tenants[t.ID] = &cp
		existing = &cp
	} else {
		// Drop the old hostname index entries before rebuilding, or a
		// removed hostname would keep routing to this tenant forever.
		for _, h := range existing.Hostnames {
			delete(r.byHost, strings.ToLower(h))
		}
		existing.PublicKey = t.PublicKey
		existing.Workspace = t.Workspace
		existing.Hostnames = t.Hostnames
		if t.QuotaBytes != 0 {
			existing.QuotaBytes = t.QuotaBytes
		}
		existing.UpdatedAt = now
	}
	for _, h := range existing.Hostnames {
		r.byHost[strings.ToLower(h)] = existing.ID
	}
	r.dirty = true
	cp := *existing
	return &cp, nil
}

// Delete removes a tenant and all its hostname routes.
func (r *Registry) Delete(tenantID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tenants[tenantID]
	if !ok {
		return false
	}
	for _, h := range t.Hostnames {
		delete(r.byHost, strings.ToLower(h))
	}
	delete(r.tenants, tenantID)
	delete(r.suspOnce, tenantID)
	r.dirty = true
	return true
}

// Get returns a copy of one tenant.
func (r *Registry) Get(tenantID string) (Tenant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tenants[tenantID]
	if !ok {
		return Tenant{}, false
	}
	return *t, true
}

// List returns a copy of every tenant, for the admin API.
func (r *Registry) List() []Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tenant, 0, len(r.tenants))
	for _, t := range r.tenants {
		out = append(out, *t)
	}
	return out
}

// TenantForHost resolves a public hostname to its tenant. The lookup is
// case-insensitive and ignores any port, since Host headers carry both.
func (r *Registry) TenantForHost(host string) (Tenant, bool) {
	host = normalizeHost(host)
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byHost[host]
	if !ok {
		return Tenant{}, false
	}
	t, ok := r.tenants[id]
	if !ok {
		return Tenant{}, false
	}
	return *t, true
}

// Lookup implements tunnel.Authenticator: it returns the enrolled key.
func (r *Registry) Lookup(tenantID string) (ed25519.PublicKey, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tenants[tenantID]
	if !ok {
		return nil, false
	}
	key, err := hex.DecodeString(t.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(key), true
}

// Authorize implements tunnel.Authenticator: it decides whether an
// authenticated tenant may hold a session right now. This runs after the
// signature checks out, so it is purely about account state.
func (r *Registry) Authorize(tenantID string) ([]string, int64, int64, time.Time, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tenants[tenantID]
	if !ok {
		return nil, 0, 0, time.Time{}, "auth", errors.New("unknown tenant")
	}
	r.rollPeriodLocked(t, time.Now().UTC())

	if t.Suspended {
		reason := t.SuspendReason
		if reason == "" {
			reason = "this Front Door account is suspended — contact support@rackops.in"
		}
		return nil, 0, 0, time.Time{}, "suspended", errors.New(reason)
	}
	if t.overQuota() {
		return nil, 0, 0, time.Time{}, "suspended", fmt.Errorf(
			"monthly Front Door transfer limit reached (%s of %s) — it resets on %s",
			humanBytes(t.UsedBytes), humanBytes(t.QuotaBytes),
			periodEnd(t.PeriodStart).Format("2 Jan"))
	}
	hosts := append([]string(nil), t.Hostnames...)
	return hosts, t.QuotaBytes, t.UsedBytes, periodEnd(t.PeriodStart), "", nil
}

// AddUsage records transferred bytes and reports whether the tenant is now
// over quota. It is called on every completed request, so it does the
// minimum possible work: no disk, no allocation, one lock.
func (r *Registry) AddUsage(tenantID string, n int64) (over bool) {
	if n <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tenants[tenantID]
	if !ok {
		return false
	}
	r.rollPeriodLocked(t, time.Now().UTC())
	t.UsedBytes += n
	r.dirty = true
	return t.overQuota()
}

// Usage returns current consumption for a tenant.
func (r *Registry) Usage(tenantID string) (used, quota int64, resets time.Time, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, found := r.tenants[tenantID]
	if !found {
		return 0, 0, time.Time{}, false
	}
	r.rollPeriodLocked(t, time.Now().UTC())
	return t.UsedBytes, t.QuotaBytes, periodEnd(t.PeriodStart), true
}

// SetSuspended flips the kill switch. Returns false for unknown tenants.
func (r *Registry) SetSuspended(tenantID string, suspended bool, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tenants[tenantID]
	if !ok {
		return false
	}
	t.Suspended = suspended
	t.SuspendReason = reason
	t.UpdatedAt = time.Now().UTC()
	if !suspended {
		delete(r.suspOnce, tenantID)
	}
	r.dirty = true
	return true
}

// rollPeriodLocked resets usage when the calendar month has turned over.
// Callers must hold the write lock.
//
// This is checked lazily on access rather than by a scheduled job: a POP
// that was down at midnight on the 1st would otherwise keep charging last
// month's usage against this month's allowance.
func (r *Registry) rollPeriodLocked(t *Tenant, now time.Time) {
	start := monthStart(now)
	if t.PeriodStart.Before(start) {
		t.PeriodStart = start
		t.UsedBytes = 0
		// A tenant suspended purely for quota comes back automatically when
		// the period rolls; one suspended for abuse must not.
		if t.Suspended && strings.Contains(t.SuspendReason, "transfer limit") {
			t.Suspended = false
			t.SuspendReason = ""
		}
		r.dirty = true
	}
}

// ---- helpers ----

// monthStart returns the first instant of now's calendar month in UTC.
func monthStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// periodEnd returns when the period beginning at start rolls over.
func periodEnd(start time.Time) time.Time {
	return start.AddDate(0, 1, 0)
}

// normalizeHost lowercases a Host header and strips any port, so
// "App.Link.RackOps.in:443" and "app.link.rackops.in" resolve identically.
func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	// Strip the port, taking care not to mangle a bare IPv6 literal.
	if i := strings.LastIndex(host, ":"); i > -1 && !strings.Contains(host[i+1:], "]") {
		if _, err := parsePort(host[i+1:]); err == nil {
			host = host[:i]
		}
	}
	return strings.TrimSuffix(host, ".")
}

func parsePort(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a port")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// validHostname checks a DNS name conservatively: lowercase labels of
// letters, digits and hyphens, no leading or trailing hyphen.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	labels := strings.Split(strings.ToLower(h), ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// humanBytes formats a byte count for messages a customer will read.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
