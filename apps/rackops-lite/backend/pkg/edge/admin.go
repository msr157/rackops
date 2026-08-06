package edge

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AdminAPI is the POP's control surface: enrollment, usage, and the abuse
// kill switch. It is bound to a private address and guarded by a bearer
// token, because everything on it is destructive in the wrong hands.
type AdminAPI struct {
	edge  *Edge
	token string
}

// NewAdminAPI builds the admin surface. An empty token is refused rather
// than defaulted: a POP that silently accepted unauthenticated admin calls
// would be one misconfigured firewall away from anyone suspending any
// customer.
func NewAdminAPI(e *Edge, token string) (*AdminAPI, error) {
	if len(token) < 16 {
		return nil, errors.New("edge: admin token must be at least 16 characters")
	}
	return &AdminAPI{edge: e, token: token}, nil
}

// Handler returns the admin routes.
func (a *AdminAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealth)
	a.Register(mux)
	return mux
}

// Register adds the token-guarded /admin routes to an existing mux, without
// the /healthz route.
//
// It exists so RackOps Admin can host these alongside its own console API:
// the two serve different audiences and therefore need different credentials.
// Operators log in with a password and get a session; customer control
// planes are machines and present a static token. Collapsing them would mean
// either handing every install an operator login or teaching a daemon to do
// an interactive one.
//
// It also skips /healthz, which the host app already serves — registering it
// twice on one mux panics at startup.
func (a *AdminAPI) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/tenants", a.guard(a.handleListTenants))
	mux.HandleFunc("PUT /admin/tenants/{id}", a.guard(a.handleUpsertTenant))
	mux.HandleFunc("DELETE /admin/tenants/{id}", a.guard(a.handleDeleteTenant))
	mux.HandleFunc("GET /admin/tenants/{id}/usage", a.guard(a.handleUsage))
	mux.HandleFunc("POST /admin/tenants/{id}/suspend", a.guard(a.handleSuspend))
	mux.HandleFunc("GET /admin/sessions", a.guard(a.handleSessions))
}

// guard enforces the bearer token in constant time.
func (a *AdminAPI) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// handleHealth is unauthenticated so load balancers can probe it. It
// deliberately exposes no tenant data — only whether this POP is fit to
// serve.
func (a *AdminAPI) handleHealth(w http.ResponseWriter, _ *http.Request) {
	expiry := a.edge.cfg.Certs.Expiry()
	writeJSON(w, map[string]any{
		"status":       "ok",
		"pop":          a.edge.cfg.POPID,
		"zone":         a.edge.cfg.Zone,
		"sessions":     a.edge.sessions.Count(),
		"requests":     a.edge.reqCount.Load(),
		"bytes":        a.edge.byteCount.Load(),
		"certExpiry":   expiry,
		"certDaysLeft": int(time.Until(expiry).Hours() / 24),
	})
}

func (a *AdminAPI) handleListTenants(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, a.edge.cfg.Registry.List())
}

// handleUpsertTenant enrolls or updates a tenant. This is the call the
// vendor control plane makes when a customer turns Front Door on.
func (a *AdminAPI) handleUpsertTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicKey  string   `json:"publicKey"`
		Workspace  string   `json:"workspace"`
		Hostnames  []string `json:"hostnames"`
		QuotaBytes int64    `json:"quotaBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	t, err := a.edge.cfg.Registry.Upsert(Tenant{
		ID:         r.PathValue("id"),
		PublicKey:  req.PublicKey,
		Workspace:  req.Workspace,
		Hostnames:  req.Hostnames,
		QuotaBytes: req.QuotaBytes,
	})
	if err != nil {
		// Hostname collisions are a conflict, not a malformed request: the
		// caller's payload is fine, the world just disagrees with it.
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "already belongs") {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	// Persist immediately. Enrollment is rare and the customer is waiting on
	// it, so this is the one write that must not wait for the flush loop.
	if err := a.edge.cfg.Registry.Flush(); err != nil {
		http.Error(w, "could not persist the tenant", http.StatusInternalServerError)
		return
	}
	// A key rotation must take effect now, not whenever the old session
	// happens to die — otherwise a revoked key keeps serving traffic.
	a.edge.sessions.CloseTenant(t.ID)
	writeJSON(w, t)
}

func (a *AdminAPI) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !a.edge.cfg.Registry.Delete(id) {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	a.edge.sessions.CloseTenant(id)
	if err := a.edge.cfg.Registry.Flush(); err != nil {
		http.Error(w, "could not persist the change", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) handleUsage(w http.ResponseWriter, r *http.Request) {
	used, quota, resets, ok := a.edge.cfg.Registry.Usage(r.PathValue("id"))
	if !ok {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	_, live := a.edge.sessions.Get(r.PathValue("id"))
	writeJSON(w, map[string]any{
		"usedBytes":  used,
		"quotaBytes": quota,
		"resetsAt":   resets,
		"connected":  live,
		"pop":        a.edge.cfg.POPID,
	})
}

// handleSuspend is the abuse kill switch. It takes effect immediately: the
// tenant is marked suspended and any live session is dropped in the same
// call, so a phishing report can be actioned in one request.
func (a *AdminAPI) handleSuspend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Suspended bool   `json:"suspended"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Suspended && strings.TrimSpace(req.Reason) == "" {
		// The reason is shown to the customer and to visitors. Suspending
		// without one produces a silent outage nobody can explain.
		http.Error(w, "a reason is required when suspending", http.StatusBadRequest)
		return
	}
	if !a.edge.SuspendTenant(r.PathValue("id"), req.Suspended, req.Reason) {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	if err := a.edge.cfg.Registry.Flush(); err != nil {
		http.Error(w, "could not persist the change", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"suspended": req.Suspended, "reason": req.Reason})
}

func (a *AdminAPI) handleSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, a.edge.sessions.Snapshot())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Hostname builds the public hostname for an app, matching the format the
// control plane and the console both expect: <app>-<workspace>.<zone>.
//
// A single DNS label keeps every name inside one wildcard certificate. The
// prettier <app>.<workspace>.<zone> would need a certificate per workspace,
// which means per-tenant ACME, per-tenant renewal, and per-tenant rate
// limits — real machinery that buys only cosmetics, and a one-way door once
// customers have bookmarks.
func Hostname(app, workspace, zone string) string {
	return fmt.Sprintf("%s-%s.%s", app, workspace, zone)
}
