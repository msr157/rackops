package server

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Early-access capture for the public marketing site.
//
// apps/website is served from rackops.in while this control plane answers on
// api.rackops.in, so every submission is a cross-origin POST with a JSON body
// — which means the browser sends a CORS preflight first. Both the preflight
// and the POST are unauthenticated by design: the form is public.
//
// The status contract is fixed by the form in
// apps/website/src/website/components/sections/CTA.tsx, which distinguishes
// 409 ("already requested") from every other failure. Changing these codes
// changes what visitors see.

// earlyAccessOrigins are the origins allowed to submit the form. The list is
// explicit rather than a wildcard so this endpoint cannot be driven from
// arbitrary sites.
var earlyAccessOrigins = map[string]bool{
	"https://rackops.in":     true,
	"https://www.rackops.in": true,
	"http://localhost:5173":  true, // vite dev server
}

// applyEarlyAccessCORS echoes the request origin when it is allow-listed.
// Origin is echoed rather than wildcarded so the header stays valid if the
// form ever needs credentials.
func applyEarlyAccessCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" || !earlyAccessOrigins[origin] {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Access-Control-Max-Age", "86400")
	// Caches must not serve one origin's response to another.
	h.Add("Vary", "Origin")
}

// handleEarlyAccessPreflight answers the browser's CORS preflight.
func (a *API) handleEarlyAccessPreflight(w http.ResponseWriter, r *http.Request) {
	applyEarlyAccessCORS(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleEarlyAccess records an enterprise early-access request.
func (a *API) handleEarlyAccess(w http.ResponseWriter, r *http.Request) {
	applyEarlyAccessCORS(w, r)

	var req types.EarlyAccessLead
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)
	req.Industry = strings.TrimSpace(req.Industry)
	req.Notes = strings.TrimSpace(req.Notes)

	if req.Email == "" || req.Name == "" || req.Industry == "" {
		http.Error(w, "email, name, and industry are required", http.StatusBadRequest)
		return
	}
	address, err := mail.ParseAddress(req.Email)
	if err != nil || address.Address != req.Email || len(req.Email) > 320 {
		http.Error(w, "invalid email address", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 || len(req.Industry) > 200 || len(req.Notes) > 8000 {
		http.Error(w, "name or industry exceeds 200 bytes, or notes exceed 8000 bytes", http.StatusBadRequest)
		return
	}

	key := strings.ToLower(req.Email)

	duplicate := false
	a.Store.Mutate(func() {
		if _, exists := a.Store.EarlyAccess[key]; exists {
			duplicate = true
			return
		}
		a.Store.EarlyAccess[key] = &types.EarlyAccessLead{
			ID:        NewID(),
			Email:     req.Email,
			Name:      req.Name,
			Industry:  req.Industry,
			Notes:     req.Notes,
			CreatedAt: time.Now(),
		}
	})
	if duplicate {
		http.Error(w, "this email has already requested early access", http.StatusConflict)
		return
	}

	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not persist request", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]string{"status": "recorded"})
}

// retryEarlyAccess forwards explicitly submitted enterprise enquiries only;
// no infrastructure configuration or telemetry is included.
func (a *API) retryEarlyAccess() {
	if a.InstallID == "" {
		return
	}
	var pending []types.EarlyAccessLead
	a.Store.View(func() {
		for _, lead := range a.Store.EarlyAccess {
			if !lead.Forwarded {
				pending = append(pending, *lead)
			}
			if len(pending) == 20 {
				break
			}
		}
	})
	if len(pending) == 0 {
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		return
	}
	for _, lead := range pending {
		err := crmPost("/ingest/v1/leads", map[string]any{
			"installId": a.InstallID, "id": lead.ID, "email": lead.Email,
			"name": lead.Name, "industry": lead.Industry, "notes": lead.Notes,
		}, nil)
		if err != nil {
			return
		}
		a.Store.Mutate(func() {
			if current := a.Store.EarlyAccess[strings.ToLower(lead.Email)]; current != nil && current.ID == lead.ID {
				current.Forwarded = true
			}
		})
		if err := a.Store.persistOnce(); err != nil {
			return
		}
	}
}
