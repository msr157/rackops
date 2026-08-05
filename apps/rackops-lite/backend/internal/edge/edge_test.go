package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := OpenRegistry(filepath.Join(t.TempDir(), "tenants.json"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func testKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(pub)
}

// ---- registry ----

func TestUpsertAssignsFreeTierQuota(t *testing.T) {
	r := newTestRegistry(t)
	got, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), Hostnames: []string{"app-acme.link.rackops.in"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.QuotaBytes != DefaultQuotaBytes {
		t.Fatalf("quota = %d, want the 100 GB free tier (%d)", got.QuotaBytes, DefaultQuotaBytes)
	}
}

func TestUpsertRejectsBadKey(t *testing.T) {
	r := newTestRegistry(t)
	for _, key := range []string{"", "nothex", hex.EncodeToString([]byte("too-short"))} {
		if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: key}); err == nil {
			t.Fatalf("accepted invalid public key %q", key)
		}
	}
}

func TestUpsertRejectsBadHostname(t *testing.T) {
	r := newTestRegistry(t)
	for _, h := range []string{"nodots", "-leading.example.com", "up_score.example.com", "UPPER CASE.com", ""} {
		if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), Hostnames: []string{h}}); err == nil {
			t.Fatalf("accepted invalid hostname %q", h)
		}
	}
}

// One tenant must never be able to take over a hostname already routed to
// another — this is the check that stands between a bad enrollment and a
// customer's URL being hijacked.
func TestUpsertRefusesHostnameTheft(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.Upsert(Tenant{ID: "victim", PublicKey: testKey(t), Hostnames: []string{"app.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	_, err := r.Upsert(Tenant{ID: "attacker", PublicKey: testKey(t), Hostnames: []string{"app.link.rackops.in"}})
	if err == nil {
		t.Fatal("a second tenant claimed a hostname already in use")
	}
	if !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("unexpected error: %v", err)
	}
	// The victim must still own it.
	owner, ok := r.TenantForHost("app.link.rackops.in")
	if !ok || owner.ID != "victim" {
		t.Fatalf("hostname owner = %+v, want victim", owner)
	}
}

// Re-upserting the same tenant with a smaller hostname set must release the
// dropped names, or a removed app would keep routing forever.
func TestUpsertReleasesRemovedHostnames(t *testing.T) {
	r := newTestRegistry(t)
	key := testKey(t)
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: key, Hostnames: []string{"a.link.rackops.in", "b.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: key, Hostnames: []string{"a.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.TenantForHost("b.link.rackops.in"); ok {
		t.Fatal("removed hostname still routes")
	}
	if _, ok := r.TenantForHost("a.link.rackops.in"); !ok {
		t.Fatal("retained hostname stopped routing")
	}
}

func TestTenantForHostIgnoresCaseAndPort(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), Hostnames: []string{"app.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"app.link.rackops.in", "APP.Link.RackOps.in", "app.link.rackops.in:443", "app.link.rackops.in."} {
		if _, ok := r.TenantForHost(host); !ok {
			t.Fatalf("host %q did not resolve", host)
		}
	}
}

func TestAddUsageReportsQuotaExhaustion(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), QuotaBytes: 1000}); err != nil {
		t.Fatal(err)
	}
	if over := r.AddUsage("t1", 999); over {
		t.Fatal("reported over quota at 999/1000")
	}
	if over := r.AddUsage("t1", 1); !over {
		t.Fatal("did not report over quota at 1000/1000")
	}
	used, quota, _, ok := r.Usage("t1")
	if !ok || used != 1000 || quota != 1000 {
		t.Fatalf("usage = %d/%d ok=%v", used, quota, ok)
	}
}

func TestAuthorizeRefusesOverQuota(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), QuotaBytes: 100}); err != nil {
		t.Fatal(err)
	}
	r.AddUsage("t1", 100)
	_, _, _, _, code, err := r.Authorize("t1")
	if err == nil {
		t.Fatal("authorized a tenant that is over quota")
	}
	if code != "suspended" {
		t.Fatalf("code = %q, want suspended", code)
	}
	// The customer must be told what happened and when it clears.
	if !strings.Contains(err.Error(), "transfer limit") || !strings.Contains(err.Error(), "resets") {
		t.Fatalf("unhelpful message: %v", err)
	}
}

func TestAuthorizeRefusesSuspended(t *testing.T) {
	r := newTestRegistry(t)
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t)}); err != nil {
		t.Fatal(err)
	}
	r.SetSuspended("t1", true, "abuse report #42")
	_, _, _, _, code, err := r.Authorize("t1")
	if err == nil || code != "suspended" {
		t.Fatalf("authorized a suspended tenant (code=%q err=%v)", code, err)
	}
	if !strings.Contains(err.Error(), "abuse report #42") {
		t.Fatalf("reason not surfaced: %v", err)
	}
}

// Usage resets on a calendar-month boundary, and a quota suspension lifts
// with it — but an abuse suspension must survive, or every abuser would be
// automatically un-suspended on the 1st.
func TestPeriodRollResetsQuotaButNotAbuseHolds(t *testing.T) {
	r := newTestRegistry(t)
	key := testKey(t)
	if _, err := r.Upsert(Tenant{ID: "quota", PublicKey: key, QuotaBytes: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upsert(Tenant{ID: "abuse", PublicKey: key, QuotaBytes: 100}); err != nil {
		t.Fatal(err)
	}
	r.AddUsage("quota", 100)
	r.SetSuspended("quota", true, "monthly Front Door transfer limit reached")
	r.SetSuspended("abuse", true, "phishing report")

	// Backdate both tenants into last month.
	r.mu.Lock()
	last := monthStart(time.Now().UTC()).AddDate(0, -1, 0)
	r.tenants["quota"].PeriodStart = last
	r.tenants["abuse"].PeriodStart = last
	r.mu.Unlock()

	if _, _, _, _, _, err := r.Authorize("quota"); err != nil {
		t.Fatalf("quota suspension did not lift at period roll: %v", err)
	}
	if used, _, _, _ := r.Usage("quota"); used != 0 {
		t.Fatalf("usage did not reset: %d", used)
	}
	if _, _, _, _, _, err := r.Authorize("abuse"); err == nil {
		t.Fatal("abuse suspension lifted at period roll — it must not")
	}
}

func TestRegistryPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.json")
	r, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	key := testKey(t)
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: key, Hostnames: []string{"app.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	r.AddUsage("t1", 4242)
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get("t1")
	if !ok {
		t.Fatal("tenant did not survive reopen")
	}
	if got.UsedBytes != 4242 {
		t.Fatalf("usage = %d, want 4242", got.UsedBytes)
	}
	if _, ok := reopened.TenantForHost("app.link.rackops.in"); !ok {
		t.Fatal("hostname index was not rebuilt on load")
	}
}

// The registry file must be written atomically: a crash mid-write must not
// be able to leave a truncated file that fails to parse on restart.
func TestFlushIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.json")
	r, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upsert(Tenant{ID: "t1", PublicKey: testKey(t)}); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file %s left behind after flush", e.Name())
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:               "512 B",
		1024:              "1.0 KiB",
		100 << 20:         "100.0 MiB",
		DefaultQuotaBytes: "100.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// ---- request routing ----

// newTestEdge builds a POP with a self-signed certificate, wired to a
// registry, without binding any real ports — enough to drive ServeHTTP
// directly and assert on routing decisions.
func newTestEdge(t *testing.T, reg *Registry) *Edge {
	t.Helper()
	certs, err := NewSelfSignedCertSource("link.rackops.in")
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(Config{
		POPID:    "pop-test",
		Zone:     "link.rackops.in",
		Certs:    certs,
		Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.cancel() })
	return e
}

// A hostname outside the POP's zone must be refused. Without this the POP
// would proxy for any domain whose DNS points at it — an open relay running
// on our IP's reputation.
func TestServeHTTPRefusesForeignZone(t *testing.T) {
	reg := newTestRegistry(t)
	e := newTestEdge(t, reg)

	req := httptest.NewRequest("GET", "http://evil.example.com/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a foreign zone", rec.Code)
	}
}

func TestServeHTTPUnknownHostname(t *testing.T) {
	reg := newTestRegistry(t)
	e := newTestEdge(t, reg)

	req := httptest.NewRequest("GET", "http://nobody.link.rackops.in/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServeHTTPSuspendedTenantIsRefusedWithReason(t *testing.T) {
	reg := newTestRegistry(t)
	if _, err := reg.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), Hostnames: []string{"app.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	reg.SetSuspended("t1", true, "taken offline after an abuse report")
	e := newTestEdge(t, reg)

	req := httptest.NewRequest("GET", "http://app.link.rackops.in/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "abuse report") {
		t.Fatalf("reason not shown to the visitor: %s", rec.Body.String())
	}
}

func TestServeHTTPOverQuotaIsRefused(t *testing.T) {
	reg := newTestRegistry(t)
	if _, err := reg.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), Hostnames: []string{"app.link.rackops.in"}, QuotaBytes: 10}); err != nil {
		t.Fatal(err)
	}
	reg.AddUsage("t1", 10)
	e := newTestEdge(t, reg)

	req := httptest.NewRequest("GET", "http://app.link.rackops.in/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "transfer") {
		t.Fatalf("quota not explained: %s", rec.Body.String())
	}
}

// With a valid tenant but nobody connected, the visitor gets a clear 502
// rather than a hang.
func TestServeHTTPOfflineTenant(t *testing.T) {
	reg := newTestRegistry(t)
	if _, err := reg.Upsert(Tenant{ID: "t1", PublicKey: testKey(t), Hostnames: []string{"app.link.rackops.in"}}); err != nil {
		t.Fatal(err)
	}
	e := newTestEdge(t, reg)

	req := httptest.NewRequest("GET", "http://app.link.rackops.in/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// API clients must get JSON, not an HTML error page — a fetch() that
// receives HTML produces a far more confusing bug report.
func TestStatusPageNegotiatesJSON(t *testing.T) {
	reg := newTestRegistry(t)
	e := newTestEdge(t, reg)

	req := httptest.NewRequest("GET", "http://nobody.link.rackops.in/", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want JSON", ct)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("not a JSON error body: %s", rec.Body.String())
	}
}

func TestHostnameFormat(t *testing.T) {
	if got := Hostname("immich", "nikish", "link.rackops.in"); got != "immich-nikish.link.rackops.in" {
		t.Fatalf("Hostname = %q", got)
	}
}
