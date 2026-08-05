package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// frontDoorAPI builds an API with Front Door configured, one owner, one
// online node, and one running service on it.
func frontDoorAPI(t *testing.T) (*API, string, *types.Service) {
	t.Helper()
	a, users := testAPI(t, types.RoleOwner)
	a.FrontDoorPOP = "pop.link.rackops.in:443"
	a.FrontDoorZone = "link.rackops.in"
	a.FrontDoorAdmin = "http://127.0.0.1:1" // unreachable on purpose
	a.FrontDoorToken = "test-token"

	org := a.Store.DefaultOrgID()
	svc := &types.Service{
		ID: NewID(), Name: "Immich", NodeID: "node-1", Port: 2283,
		Status: types.StatusRunning, ProjectID: "p1", CreatedAt: time.Now(),
	}
	a.Store.Mutate(func() {
		a.Store.Nodes["node-1"] = &types.Node{
			ID: "node-1", Name: "nuc", OrgID: org, Status: types.NodeOnline,
			FrontDoorKey: strings.Repeat("ab", 32),
		}
		a.Store.Projects["p1"] = &types.Project{ID: "p1", Name: "proj", OrgID: org}
		a.Store.Services[svc.ID] = svc
	})
	return a, users[types.RoleOwner], svc
}

func ownerToken(t *testing.T, a *API) string {
	t.Helper()
	return loginToken(t, a, "owner@test.local")
}

func TestFrontDoorHostnameFormat(t *testing.T) {
	got := frontDoorHostname("My Photos!", "acme", "link.rackops.in")
	if got != "my-photos-acme.link.rackops.in" {
		t.Fatalf("hostname = %q", got)
	}
}

// Turning Front Door on must reject a blank or useless workspace name: it
// becomes part of every public address, so it cannot be guessed for the user.
func TestSetFrontDoorRequiresWorkspace(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	tok := ownerToken(t, a)

	for _, ws := range []string{"", "   ", "!!!"} {
		body, _ := json.Marshal(map[string]any{"enabled": true, "workspace": ws})
		rec := do(a, http.MethodPost, "/api/frontdoor", string(body),
			map[string]string{"Authorization": "Bearer " + tok})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("workspace %q: status = %d, want 400", ws, rec.Code)
		}
	}
}

// An install with no POP configured must say so rather than offering a
// feature that cannot work.
func TestSetFrontDoorRefusedWithoutPOP(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	a.FrontDoorPOP = ""
	tok := ownerToken(t, a)

	rec := do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`,
		map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatalf("unhelpful error: %s", rec.Body)
	}
}

// Enabling Front Door must start the edge proxy itself. Requiring the user
// to go set up Traefik first is the exact friction this feature removes.
func TestSetFrontDoorStartsIngressAutomatically(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	tok := ownerToken(t, a)

	var before bool
	a.Store.View(func() { before = a.Store.Ingress.Enabled })
	if before {
		t.Fatal("ingress was already enabled; the test proves nothing")
	}

	rec := do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`,
		map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var cfg types.IngressConfig
	var fd types.FrontDoorConfig
	a.Store.View(func() { cfg, fd = a.Store.Ingress, a.Store.FrontDoor })
	if !cfg.Enabled || cfg.NodeID != "node-1" {
		t.Fatalf("ingress not started automatically: %+v", cfg)
	}
	if !fd.Enabled || fd.Workspace != "acme" || fd.TenantID == "" {
		t.Fatalf("front door config = %+v", fd)
	}
	// A tunnel node must be chosen, and it must be where Traefik landed.
	if fd.NodeID != cfg.NodeID {
		t.Fatalf("tunnel node %q != ingress node %q", fd.NodeID, cfg.NodeID)
	}
}

func TestSetFrontDoorRequiresOnlineNode(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	a.Store.Mutate(func() { a.Store.Nodes["node-1"].Status = types.NodeOffline })
	tok := ownerToken(t, a)

	rec := do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`,
		map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "online") {
		t.Fatalf("unhelpful error: %s", rec.Body)
	}
}

// Publishing an app must set the hostname and the FrontDoor flag together:
// the flag is what keeps ACME away from a name the customer cannot validate.
func TestEnableServiceFrontDoorAssignsHostname(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}

	if rec := do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr); rec.Code != http.StatusOK {
		t.Fatalf("enable front door: %d %s", rec.Code, rec.Body)
	}
	rec := do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct{ Domain, URL string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Domain != "immich-acme.link.rackops.in" {
		t.Fatalf("domain = %q", out.Domain)
	}
	if !strings.HasPrefix(out.URL, "https://") {
		t.Fatalf("url = %q, want https", out.URL)
	}
	a.Store.View(func() {
		if !svc.FrontDoor {
			t.Error("FrontDoor flag not set — ACME would chase this hostname")
		}
		if svc.Domain != out.Domain {
			t.Errorf("svc.Domain = %q, want %q", svc.Domain, out.Domain)
		}
	})
}

func TestEnableServiceFrontDoorNeedsItEnabled(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)

	rec := do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "",
		map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestEnableServiceFrontDoorNeedsAPort(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	a.Store.Mutate(func() { svc.Port = 0 })
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)

	rec := do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// Two apps whose names collide must not both claim one address — the second
// would silently steal the first one's traffic.
func TestEnableServiceFrontDoorRefusesDuplicateHostname(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	org := a.Store.DefaultOrgID()
	other := &types.Service{
		ID: NewID(), Name: "immich", NodeID: "node-1", Port: 3000,
		Status: types.StatusRunning, ProjectID: "p1", CreatedAt: time.Now(),
	}
	_ = org
	a.Store.Mutate(func() { a.Store.Services[other.ID] = other })

	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)

	if rec := do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr); rec.Code != http.StatusOK {
		t.Fatalf("first claim: %d %s", rec.Code, rec.Body)
	}
	rec := do(a, http.MethodPost, "/api/services/"+other.ID+"/frontdoor", "", hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second claim: status = %d, want 409", rec.Code)
	}
}

func TestDisableServiceFrontDoorClearsRoute(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)
	do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)

	rec := do(a, http.MethodDelete, "/api/services/"+svc.ID+"/frontdoor", "", hdr)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	a.Store.View(func() {
		if svc.FrontDoor || svc.Domain != "" || svc.Ingress != types.IngressNone {
			t.Fatalf("route not cleared: frontDoor=%v domain=%q ingress=%q", svc.FrontDoor, svc.Domain, svc.Ingress)
		}
	})
}

// The product audit found the quick-tunnel handlers fetched services by raw
// ID with no ownership check, so any logged-in user could tunnel another
// org's app. The Front Door handlers must not repeat it.
func TestFrontDoorHandlersAreOrgScoped(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)

	// Move the service's project into a different org: the caller is no
	// longer a member of the org that owns it.
	a.Store.Mutate(func() {
		a.Store.Orgs["other-org"] = &types.Organization{ID: "other-org", Name: "other", CreatedAt: time.Now()}
		a.Store.Projects["p1"].OrgID = "other-org"
		a.Store.Nodes["node-1"].OrgID = "other-org"
	})

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/services/" + svc.ID + "/frontdoor"},
		{http.MethodDelete, "/api/services/" + svc.ID + "/frontdoor"},
	} {
		rec := do(a, tc.method, tc.path, "", hdr)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 for a service in another org",
				tc.method, tc.path, rec.Code)
		}
	}
}

func TestFrontDoorEndpointsRequireAuth(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	// A user must exist, or the API is in setup mode and lets everything by.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/frontdoor"},
		{http.MethodPost, "/api/frontdoor"},
		{http.MethodPost, "/api/services/" + svc.ID + "/frontdoor"},
		{http.MethodDelete, "/api/services/" + svc.ID + "/frontdoor"},
	} {
		rec := do(a, tc.method, tc.path, `{}`, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// ---- spec delivery ----

// Only the tunnel node gets a spec; every other node must be told nothing,
// or several machines would open competing tunnels for one install.
func TestFrontDoorSpecOnlyForTunnelNode(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)
	do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)

	spec := a.frontDoorSpecFor("node-1")
	if spec == nil || !spec.Enabled {
		t.Fatal("tunnel node got no spec")
	}
	if len(spec.Hostnames) != 1 || spec.Hostnames[0] != "immich-acme.link.rackops.in" {
		t.Fatalf("hostnames = %v", spec.Hostnames)
	}
	// It must point at the dedicated Front Door entrypoint, not the web one
	// that carries the HTTPS redirect.
	if !strings.HasSuffix(spec.LocalTarget, ":8099") {
		t.Fatalf("localTarget = %q, want the Front Door entrypoint", spec.LocalTarget)
	}
	if other := a.frontDoorSpecFor("node-2"); other != nil {
		t.Fatalf("a non-tunnel node received a spec: %+v", other)
	}
}

func TestFrontDoorSpecEmptyWhenDisabled(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	if spec := a.frontDoorSpecFor("node-1"); spec != nil {
		t.Fatalf("spec issued while Front Door is off: %+v", spec)
	}
}

// The spec is re-sent on every agent report, so its hostname order must be
// stable — otherwise the agent sees a change that is not one and reconnects
// the tunnel in a loop.
func TestFrontDoorSpecHostnameOrderIsStable(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)

	// Several services, so map iteration order has room to vary.
	for _, name := range []string{"zebra", "alpha", "mango"} {
		s := &types.Service{
			ID: NewID(), Name: name, NodeID: "node-1", Port: 8000,
			Status: types.StatusRunning, ProjectID: "p1", CreatedAt: time.Now(),
		}
		a.Store.Mutate(func() { a.Store.Services[s.ID] = s })
		do(a, http.MethodPost, "/api/services/"+s.ID+"/frontdoor", "", hdr)
	}
	do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)

	first := a.frontDoorSpecFor("node-1")
	for range 20 {
		if got := a.frontDoorSpecFor("node-1"); !got.Equal(*first) {
			t.Fatalf("spec churned between reports:\n%v\n%v", first.Hostnames, got.Hostnames)
		}
	}
}

// ---- report folding ----

// The node's public key must be recorded even when Front Door is off: it is
// what enrollment needs, so capturing it early means turning the feature on
// does not wait a full report interval.
func TestApplyFrontDoorReportRecordsKeyWhenDisabled(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	a.Store.Mutate(func() { a.Store.Nodes["node-1"].FrontDoorKey = "" })

	a.applyFrontDoorReport("node-1", &types.FrontDoorState{
		Status:    types.FrontDoorOff,
		PublicKey: "deadbeef",
	})
	var got string
	a.Store.View(func() { got = a.Store.Nodes["node-1"].FrontDoorKey })
	if got != "deadbeef" {
		t.Fatalf("key = %q, want it recorded even while off", got)
	}
}

func TestApplyFrontDoorReportMirrorsStatus(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	tok := ownerToken(t, a)
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`,
		map[string]string{"Authorization": "Bearer " + tok})

	a.applyFrontDoorReport("node-1", &types.FrontDoorState{
		Status:     types.FrontDoorConnected,
		PublicKey:  strings.Repeat("ab", 32),
		POPID:      "blr1",
		Transport:  "quic",
		UsedBytes:  4096,
		QuotaBytes: 100 << 30,
	})
	var fd types.FrontDoorConfig
	a.Store.View(func() { fd = a.Store.FrontDoor })
	if fd.Status != types.FrontDoorConnected || fd.POPID != "blr1" || fd.Transport != "quic" {
		t.Fatalf("status not mirrored: %+v", fd)
	}
	if fd.UsedBytes != 4096 || fd.QuotaBytes != 100<<30 {
		t.Fatalf("usage not mirrored: used=%d quota=%d", fd.UsedBytes, fd.QuotaBytes)
	}
}

// A report from a node that does not hold the tunnel must not overwrite the
// install's status.
func TestApplyFrontDoorReportIgnoresOtherNodes(t *testing.T) {
	a, _, _ := frontDoorAPI(t)
	tok := ownerToken(t, a)
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`,
		map[string]string{"Authorization": "Bearer " + tok})
	a.Store.Mutate(func() { a.Store.Nodes["node-2"] = &types.Node{ID: "node-2"} })

	a.applyFrontDoorReport("node-1", &types.FrontDoorState{Status: types.FrontDoorConnected, POPID: "blr1"})
	a.applyFrontDoorReport("node-2", &types.FrontDoorState{Status: types.FrontDoorRejected, Message: "not mine"})

	var fd types.FrontDoorConfig
	a.Store.View(func() { fd = a.Store.FrontDoor })
	if fd.Status != types.FrontDoorConnected || fd.StatusMsg == "not mine" {
		t.Fatalf("a non-tunnel node overwrote install status: %+v", fd)
	}
}

// ---- Traefik config ----

// The whole point of the FrontDoor flag: these hostnames must land on the
// dedicated loopback entrypoint with no TLS and no certResolver, because the
// customer cannot validate a vendor-owned name and the web entrypoint would
// redirect the tunnel into a loop.
func TestTraefikConfigKeepsFrontDoorOffACME(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)
	do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)
	// ACME on, which is what makes the redirect dangerous.
	a.Store.Mutate(func() {
		a.Store.Ingress.ACMEEmail = "ops@example.com"
		a.Store.Ingress.NodeID = "node-1"
		a.Store.Ingress.Enabled = true
	})

	rec := do(a, http.MethodGet, "/api/traefik/config", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var cfg struct {
		HTTP struct {
			Routers map[string]struct {
				Rule        string   `json:"rule"`
				EntryPoints []string `json:"entryPoints"`
				TLS         *struct {
					CertResolver string `json:"certResolver"`
				} `json:"tls"`
			} `json:"routers"`
		} `json:"http"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	router, ok := cfg.HTTP.Routers["rackops-"+svc.ID]
	if !ok {
		t.Fatalf("no router for the Front Door service: %s", rec.Body)
	}
	if router.TLS != nil {
		t.Errorf("TLS set on a Front Door router (certResolver=%q) — ACME would fail forever on a name the customer does not own", router.TLS.CertResolver)
	}
	if len(router.EntryPoints) != 1 || router.EntryPoints[0] != "frontdoor" {
		t.Errorf("entryPoints = %v, want [frontdoor]: the web entrypoint redirects HTTP→HTTPS and would loop the tunnel", router.EntryPoints)
	}
}

// A customer's own domain must still get ACME — the fix above must not have
// disabled TLS for everyone.
func TestTraefikConfigStillIssuesCertsForOwnDomains(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	a.Store.Mutate(func() {
		svc.Domain = "photos.example.com"
		svc.FrontDoor = false
		a.Store.Ingress.ACMEEmail = "ops@example.com"
		a.Store.Ingress.NodeID = "node-1"
		a.Store.Ingress.Enabled = true
	})

	rec := do(a, http.MethodGet, "/api/traefik/config", "", nil)
	if !strings.Contains(rec.Body.String(), `"certResolver":"le"`) {
		t.Fatalf("a customer-owned domain lost its certificate resolver: %s", rec.Body)
	}
}

// ---- domain interactions ----

// Setting a domain the customer owns must take the app off Front Door: both
// write Domain, and a stale flag would misreport what the vendor edge routes.
func TestSetServiceDomainClearsFrontDoor(t *testing.T) {
	a, _, svc := frontDoorAPI(t)
	tok := ownerToken(t, a)
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	do(a, http.MethodPost, "/api/frontdoor", `{"enabled":true,"workspace":"acme"}`, hdr)
	do(a, http.MethodPost, "/api/services/"+svc.ID+"/frontdoor", "", hdr)

	rec := do(a, http.MethodPost, "/api/services/"+svc.ID+"/domain",
		`{"domain":"photos.example.com","ingress":"static"}`, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	a.Store.View(func() {
		if svc.FrontDoor {
			t.Fatal("FrontDoor flag survived being pointed at a customer domain")
		}
		if svc.Domain != "photos.example.com" {
			t.Fatalf("domain = %q", svc.Domain)
		}
	})
}
