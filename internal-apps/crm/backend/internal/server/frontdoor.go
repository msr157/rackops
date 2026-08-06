package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/pkg/edge"
)

// Front Door inside RackOps Admin.
//
// The POP runs in this process rather than as a second service, so one
// machine and one console cover the whole vendor side: support tickets,
// install analytics, and the tunnel edge customers connect to. That removes
// the thing most likely to go wrong in a small vendor deployment — two
// daemons, two config files, two sets of credentials, and an admin API that
// had to be exposed over the network for the two to talk.
//
// With them merged, the tunnel registry is reachable as an ordinary
// authenticated dashboard API guarded by the same login, and nothing has to
// listen on a second port.

// FrontDoor wires the edge into the admin API. It is nil when the operator
// has not enabled the tunnel, in which case every route below answers 503
// with an explanation rather than a blank failure.
type FrontDoor struct {
	Edge     *edge.Edge
	Registry *edge.Registry
	Zone     string
	POPID    string
	// PublicAddr is what installs are told to dial. It is reported to the
	// console so an operator can copy the exact value into a customer's
	// server flags instead of reconstructing it.
	PublicAddr string
	// EnrollToken authenticates customer control planes on the /admin routes.
	// It is a machine credential, distinct from the operator's login: a
	// daemon cannot do an interactive sign-in, and no customer should hold
	// an operator password.
	EnrollToken string
	// adminAPI serves those routes.
	adminAPI *edge.AdminAPI
}

// frontDoorRoutes registers the tunnel surface on the admin API.
func (a *API) frontDoorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/frontdoor", a.handleFrontDoorStatus)
	mux.HandleFunc("GET /api/frontdoor/tenants", a.handleListTenants)
	mux.HandleFunc("PUT /api/frontdoor/tenants/{id}", a.handleUpsertTenant)
	mux.HandleFunc("DELETE /api/frontdoor/tenants/{id}", a.handleDeleteTenant)
	mux.HandleFunc("POST /api/frontdoor/tenants/{id}/suspend", a.handleSuspendTenant)
	mux.HandleFunc("GET /api/frontdoor/sessions", a.handleFrontDoorSessions)

	// Machine-facing enrollment, on /admin/* with a static bearer token.
	// These sit outside /api/* so the console's session middleware lets them
	// through to the edge's own constant-time token check.
	if a.FrontDoor != nil && a.FrontDoor.adminAPI != nil {
		a.FrontDoor.adminAPI.Register(mux)
	}
}

// frontDoorReady reports whether the tunnel is running, writing the 503 if
// not. Every handler below starts with it.
func (a *API) frontDoorReady(w http.ResponseWriter) bool {
	if a.FrontDoor == nil || a.FrontDoor.Edge == nil {
		writeError(w, http.StatusServiceUnavailable,
			"the Front Door tunnel is not enabled on this server — start rackops-admin with --frontdoor")
		return false
	}
	return true
}

// FrontDoorStatus is the console's summary of the tunnel.
type FrontDoorStatus struct {
	Enabled bool `json:"enabled"`
	// Reason explains a disabled tunnel in words an operator can act on.
	Reason     string `json:"reason,omitempty"`
	POPID      string `json:"popId,omitempty"`
	Zone       string `json:"zone,omitempty"`
	PublicAddr string `json:"publicAddr,omitempty"`
	// EnrollToken is shown to the signed-in operator so they can paste a
	// complete, correct set of flags to a customer rather than assembling
	// them from three places.
	EnrollToken  string    `json:"enrollToken,omitempty"`
	Tenants      int       `json:"tenants"`
	Connected    int       `json:"connected"`
	Requests     int64     `json:"requests"`
	Bytes        int64     `json:"bytes"`
	CertExpiry   time.Time `json:"certExpiry,omitzero"`
	CertDaysLeft int       `json:"certDaysLeft"`
	// SelfSigned warns that this POP is serving a certificate browsers will
	// reject. It is the single most common reason a new deployment "doesn't
	// work", so the console shows it rather than making someone read logs.
	SelfSigned bool `json:"selfSigned"`
}

func (a *API) handleFrontDoorStatus(w http.ResponseWriter, _ *http.Request) {
	if a.FrontDoor == nil || a.FrontDoor.Edge == nil {
		writeJSON(w, FrontDoorStatus{
			Enabled: false,
			Reason:  "Not enabled. Start rackops-admin with --frontdoor to run the tunnel edge on this machine.",
		})
		return
	}
	fd := a.FrontDoor
	stats := fd.Edge.Stats()
	writeJSON(w, FrontDoorStatus{
		Enabled:      true,
		POPID:        fd.POPID,
		Zone:         fd.Zone,
		PublicAddr:   fd.PublicAddr,
		EnrollToken:  fd.EnrollToken,
		Tenants:      len(fd.Registry.List()),
		Connected:    fd.Edge.SessionCount(),
		Requests:     stats.Requests,
		Bytes:        stats.Bytes,
		CertExpiry:   stats.CertExpiry,
		CertDaysLeft: int(time.Until(stats.CertExpiry).Hours() / 24),
		SelfSigned:   stats.SelfSigned,
	})
}

func (a *API) handleListTenants(w http.ResponseWriter, _ *http.Request) {
	if !a.frontDoorReady(w) {
		return
	}
	// Decorate each tenant with whether it is connected right now: a list of
	// registrations without liveness is the wrong answer to "who is up?".
	type row struct {
		edge.Tenant
		Connected bool   `json:"connected"`
		Transport string `json:"transport,omitempty"`
	}
	live := map[string]string{}
	for _, s := range a.FrontDoor.Edge.Sessions() {
		live[s.TenantID] = s.Transport
	}
	tenants := a.FrontDoor.Registry.List()
	out := make([]row, 0, len(tenants))
	for _, t := range tenants {
		transport, connected := live[t.ID]
		out = append(out, row{Tenant: t, Connected: connected, Transport: transport})
	}
	writeJSON(w, out)
}

func (a *API) handleUpsertTenant(w http.ResponseWriter, r *http.Request) {
	if !a.frontDoorReady(w) {
		return
	}
	var req struct {
		PublicKey  string   `json:"publicKey"`
		Workspace  string   `json:"workspace"`
		Hostnames  []string `json:"hostnames"`
		QuotaBytes int64    `json:"quotaBytes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := a.FrontDoor.Registry.Upsert(edge.Tenant{
		ID:         r.PathValue("id"),
		PublicKey:  req.PublicKey,
		Workspace:  req.Workspace,
		Hostnames:  req.Hostnames,
		QuotaBytes: req.QuotaBytes,
	})
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "already belongs") {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	if err := a.FrontDoor.Registry.Flush(); err != nil {
		serverError(w, "persist tenant", err)
		return
	}
	// A rotated key must take effect now, not whenever the old session
	// happens to die.
	a.FrontDoor.Edge.DisconnectTenant(t.ID)
	writeJSON(w, t)
}

func (a *API) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if !a.frontDoorReady(w) {
		return
	}
	id := r.PathValue("id")
	if !a.FrontDoor.Registry.Delete(id) {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	a.FrontDoor.Edge.DisconnectTenant(id)
	if err := a.FrontDoor.Registry.Flush(); err != nil {
		serverError(w, "persist tenant removal", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSuspendTenant is the abuse kill switch, reachable from the same
// console the abuse report lands in.
func (a *API) handleSuspendTenant(w http.ResponseWriter, r *http.Request) {
	if !a.frontDoorReady(w) {
		return
	}
	var req struct {
		Suspended bool   `json:"suspended"`
		Reason    string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Suspended && strings.TrimSpace(req.Reason) == "" {
		// The reason reaches the customer and their visitors. Suspending
		// without one produces an outage nobody can explain.
		writeError(w, http.StatusBadRequest, "a reason is required when suspending")
		return
	}
	if !a.FrontDoor.Edge.SuspendTenant(r.PathValue("id"), req.Suspended, req.Reason) {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if err := a.FrontDoor.Registry.Flush(); err != nil {
		serverError(w, "persist suspension", err)
		return
	}
	writeJSON(w, map[string]any{"suspended": req.Suspended, "reason": req.Reason})
}

func (a *API) handleFrontDoorSessions(w http.ResponseWriter, _ *http.Request) {
	if !a.frontDoorReady(w) {
		return
	}
	writeJSON(w, a.FrontDoor.Edge.Sessions())
}

// StartFrontDoor brings the tunnel edge up inside this process.
//
// It is called from main once, and returns a FrontDoor the API can serve
// from. A failure here is fatal to the tunnel but not to the console:
// support tickets must keep working even if the edge cannot bind a port.
func StartFrontDoor(cfg edge.Config, registry *edge.Registry, enrollToken string) (*FrontDoor, error) {
	e, err := edge.New(cfg)
	if err != nil {
		return nil, err
	}
	adminAPI, err := edge.NewAdminAPI(e, enrollToken)
	if err != nil {
		return nil, err
	}
	if err := e.Start(); err != nil {
		return nil, err
	}
	return &FrontDoor{
		Edge:        e,
		Registry:    registry,
		Zone:        cfg.Zone,
		POPID:       cfg.POPID,
		PublicAddr:  e.PublicAddr(),
		EnrollToken: enrollToken,
		adminAPI:    adminAPI,
	}, nil
}

// Shutdown stops the tunnel, draining in-flight requests.
func (f *FrontDoor) Shutdown(ctx context.Context) error {
	if f == nil || f.Edge == nil {
		return nil
	}
	return f.Edge.Shutdown(ctx)
}

// EnrollHint returns the exact flags a customer should add to their RackOps
// server so the operator can copy them out of the console.
//
// Setup instructions that must be assembled by hand are setup instructions
// that get typed wrong, and every one of those becomes a support ticket in
// the same app this is rendered in.
func (f *FrontDoor) EnrollHint(adminURL string) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf(
		"--frontdoor-pop %s --frontdoor-zone %s --frontdoor-admin %s",
		f.PublicAddr, f.Zone, strings.TrimSuffix(adminURL, "/"))
}

// logFrontDoor reports the tunnel's state at startup in the house style.
func logFrontDoor(f *FrontDoor, selfSigned bool) {
	if f == nil {
		log.Printf("admin: Front Door disabled (no --frontdoor)")
		return
	}
	log.Printf("admin: Front Door serving *.%s on %s (pop %s)", f.Zone, f.PublicAddr, f.POPID)
	if selfSigned {
		log.Printf("admin: WARNING — Front Door is using a self-signed certificate; browsers will reject it")
	}
}
