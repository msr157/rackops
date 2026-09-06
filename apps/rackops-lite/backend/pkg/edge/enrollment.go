package edge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Enrollment credentials are separate from the vendor administration bearer.
// They authorize hostname updates for one pre-registered identity only. Key,
// workspace, quota, suspension and tenant creation stay operator-controlled.
type enrollmentClaims struct {
	TenantID  string `json:"tenantId"`
	PublicKey string `json:"publicKey"`
	Workspace string `json:"workspace"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (a *AdminAPI) enrollmentMAC(payload string) []byte {
	m := hmac.New(sha256.New, []byte(a.token))
	m.Write([]byte("rackops-enrollment-v1:" + payload))
	return m.Sum(nil)
}

// IssueEnrollmentToken is called only through operator-authenticated routes.
// Rotating the tenant key/workspace or vendor secret invalidates old grants.
func (a *AdminAPI) IssueEnrollmentToken(id string) (string, time.Time, error) {
	tenant, ok := a.edge.cfg.Registry.Get(id)
	if !ok || tenant.Suspended || tenant.Workspace == "" {
		return "", time.Time{}, errors.New("register an active tenant with a workspace and public key first")
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	b, err := json.Marshal(enrollmentClaims{id, tenant.PublicKey, tenant.Workspace, expires.Unix()})
	if err != nil {
		return "", time.Time{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	return "re1." + payload + "." + base64.RawURLEncoding.EncodeToString(a.enrollmentMAC(payload)), expires, nil
}

// RegisterEnrollment deliberately exposes no global administrative routes.
func (a *AdminAPI) RegisterEnrollment(mux *http.ServeMux) {
	mux.HandleFunc("PUT /enroll/v1/tenants/{id}", a.handleEnrollment)
}

func (a *AdminAPI) handleEnrollment(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	deny := func() { http.Error(w, "invalid or expired enrollment credential", http.StatusForbidden) }
	if len(parts) != 3 || parts[0] != "re1" || len(parts[1]) > 4096 {
		deny()
		return
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, a.enrollmentMAC(parts[1])) {
		deny()
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims enrollmentClaims
	if err != nil || json.Unmarshal(b, &claims) != nil || claims.TenantID != r.PathValue("id") || claims.ExpiresAt <= time.Now().Unix() {
		deny()
		return
	}
	var req struct {
		PublicKey string   `json:"publicKey"`
		Workspace string   `json:"workspace"`
		Hostnames []string `json:"hostnames"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || len(req.Hostnames) > 100 || req.PublicKey != claims.PublicKey || req.Workspace != claims.Workspace {
		deny()
		return
	}
	if err := a.edge.cfg.Registry.updateEnrollment(claims, req.Hostnames, a.edge.cfg.Zone); err != nil {
		deny()
		return
	}
	if err := a.edge.cfg.Registry.Flush(); err != nil {
		http.Error(w, "could not persist enrollment", http.StatusInternalServerError)
		return
	}
	// Refresh assignments; key and quota were not changed by this operation.
	a.edge.sessions.CloseTenant(claims.TenantID)
	writeJSON(w, map[string]bool{"ok": true})
}

func (r *Registry) updateEnrollment(c enrollmentClaims, hosts []string, zone string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.tenants[c.TenantID]
	if t == nil || t.Suspended || t.PublicKey != c.PublicKey || t.Workspace != c.Workspace {
		return errors.New("enrollment identity no longer active")
	}
	suffix := "-" + t.Workspace + "." + zone
	for _, host := range hosts {
		if !validHostname(host) || host != strings.ToLower(host) || !strings.HasSuffix(host, suffix) || strings.Contains(strings.TrimSuffix(host, suffix), ".") || strings.TrimSuffix(host, suffix) == "" {
			return errors.New("hostname outside tenant namespace")
		}
		if owner := r.byHost[host]; owner != "" && owner != t.ID {
			return errors.New("hostname already assigned")
		}
	}
	for _, host := range t.Hostnames {
		delete(r.byHost, host)
	}
	t.Hostnames = append([]string(nil), hosts...)
	for _, host := range hosts {
		r.byHost[host] = t.ID
	}
	t.UpdatedAt = time.Now().UTC()
	r.dirty = true
	return nil
}
