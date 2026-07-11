package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Organisations group projects and members, Dokploy-style. Membership rules:
//   - platform owners/admins see and manage every organisation
//   - everyone else sees only organisations they are a member of
//   - org owners/admins manage members; the last org can't be deleted

// orgVisible reports whether the requesting user may see an organisation.
func orgVisible(o *types.Organization, claims *jwtClaims) bool {
	if claims == nil {
		return true // token-only auth (no user) sees everything, like before orgs
	}
	if claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin) {
		return true
	}
	return o.HasMember(claims.Sub)
}

// orgManageable reports whether the requesting user may administer it.
func orgManageable(o *types.Organization, claims *jwtClaims) bool {
	if claims == nil {
		return true
	}
	if claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin) {
		return true
	}
	role := o.MemberRole(claims.Sub)
	return role == types.OrgRoleOwner || role == types.OrgRoleAdmin
}

func (a *API) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	var out []*types.Organization
	a.Store.View(func() {
		for _, o := range a.Store.Orgs {
			if orgVisible(o, claims) {
				out = append(out, o)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	writeJSON(w, out)
}

func (a *API) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "organisation name required", http.StatusBadRequest)
		return
	}
	claims := claimsFrom(r)
	o := &types.Organization{ID: NewID(), Name: strings.TrimSpace(req.Name), CreatedAt: time.Now()}
	if claims != nil {
		o.CreatedBy = claims.Sub
		o.Members = []types.OrgMember{{UserID: claims.Sub, Role: types.OrgRoleOwner, AddedAt: time.Now()}}
	}
	a.Store.Mutate(func() { a.Store.Orgs[o.ID] = o })
	a.Store.AddEvent("org", "Organisation created: "+o.Name, "", "")
	writeJSON(w, o)
}

func (a *API) handleUpdateOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "organisation name required", http.StatusBadRequest)
		return
	}
	claims := claimsFrom(r)
	var o *types.Organization
	a.Store.View(func() { o = a.Store.Orgs[r.PathValue("id")] })
	if o == nil {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}
	if !orgManageable(o, claims) {
		http.Error(w, "only organisation admins can rename it", http.StatusForbidden)
		return
	}
	a.Store.Mutate(func() { o.Name = strings.TrimSpace(req.Name) })
	writeJSON(w, o)
}

func (a *API) handleDeleteOrg(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	id := r.PathValue("id")
	var o *types.Organization
	projects := 0
	total := 0
	a.Store.View(func() {
		o = a.Store.Orgs[id]
		total = len(a.Store.Orgs)
		for _, p := range a.Store.Projects {
			if p.OrgID == id {
				projects++
			}
		}
	})
	if o == nil {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}
	if !orgManageable(o, claims) {
		http.Error(w, "only organisation admins can delete it", http.StatusForbidden)
		return
	}
	if total <= 1 {
		http.Error(w, "you can't delete the last organisation", http.StatusBadRequest)
		return
	}
	if projects > 0 {
		http.Error(w, "this organisation still contains projects — move or delete them first", http.StatusConflict)
		return
	}
	a.Store.Mutate(func() { delete(a.Store.Orgs, id) })
	a.Store.AddEvent("org", "Organisation deleted: "+o.Name, "", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleOrgMembers manages membership: POST adds/updates, DELETE removes.
func (a *API) handleAddOrgMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"userId"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims := claimsFrom(r)
	var o *types.Organization
	var user *types.User
	a.Store.View(func() {
		o = a.Store.Orgs[r.PathValue("id")]
		if req.UserID != "" {
			user = a.Store.Users[req.UserID]
		} else if req.Email != "" {
			email := strings.ToLower(strings.TrimSpace(req.Email))
			for _, u := range a.Store.Users {
				if u.Email == email {
					user = u
				}
			}
		}
	})
	if o == nil {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}
	if !orgManageable(o, claims) {
		http.Error(w, "only organisation admins can manage members", http.StatusForbidden)
		return
	}
	if user == nil {
		http.Error(w, "no account with that email — create the user first (Settings → Users)", http.StatusNotFound)
		return
	}
	role := types.OrgRole(req.Role)
	if role != types.OrgRoleOwner && role != types.OrgRoleAdmin {
		role = types.OrgRoleMember
	}
	a.Store.Mutate(func() {
		for i, m := range o.Members {
			if m.UserID == user.ID {
				o.Members[i].Role = role
				return
			}
		}
		o.Members = append(o.Members, types.OrgMember{UserID: user.ID, Role: role, AddedAt: time.Now()})
	})
	writeJSON(w, o)
}

func (a *API) handleRemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	var o *types.Organization
	a.Store.View(func() { o = a.Store.Orgs[r.PathValue("id")] })
	if o == nil {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}
	if !orgManageable(o, claims) {
		http.Error(w, "only organisation admins can manage members", http.StatusForbidden)
		return
	}
	userID := r.PathValue("userId")
	owners := 0
	for _, m := range o.Members {
		if m.Role == types.OrgRoleOwner {
			owners++
		}
	}
	if o.MemberRole(userID) == types.OrgRoleOwner && owners <= 1 {
		http.Error(w, "an organisation needs at least one owner", http.StatusBadRequest)
		return
	}
	a.Store.Mutate(func() {
		kept := o.Members[:0]
		for _, m := range o.Members {
			if m.UserID != userID {
				kept = append(kept, m)
			}
		}
		o.Members = kept
	})
	w.WriteHeader(http.StatusNoContent)
}
