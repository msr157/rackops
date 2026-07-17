package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rackops/rackops/internal/types"
)

// Organizations are the multi-tenancy boundary (Dokploy model): projects,
// machines, domains, notification targets and Copilot settings belong to one
// org; the edge proxy, mesh, HA cluster, updates and platform users stay
// install-wide. Every request resolves an ACTIVE org (X-Rackops-Org header,
// SSE ?org=) and list handlers filter to it; membership is validated per
// request, so switching orgs is a header change, not a new session.

const orgHeader = "X-Rackops-Org"

// EnsureDefaultOrg migrates pre-org state: creates "My Organization", adopts
// every unowned project/node/domain, and enrolls every existing user
// (platform owner/admin become org owner/admin). Idempotent — new installs
// and every startup run it; later objects always carry an OrgID.
func (s *Store) EnsureDefaultOrg() {
	s.Mutate(func() {
		var def *types.Organization
		// Reuse the oldest org as the default (stable across restarts).
		for _, o := range s.Orgs {
			if def == nil || o.CreatedAt.Before(def.CreatedAt) {
				def = o
			}
		}
		if def == nil {
			def = &types.Organization{
				ID: NewID(), Name: "My Organization", CreatedAt: time.Now(),
			}
			s.Orgs[def.ID] = def
		}
		// Enroll users that belong to no org at all.
		inSomeOrg := map[string]bool{}
		for _, o := range s.Orgs {
			for _, m := range o.Members {
				inSomeOrg[m.UserID] = true
			}
		}
		for _, u := range s.Users {
			if inSomeOrg[u.ID] {
				continue
			}
			role := types.OrgRoleMember
			switch u.Role {
			case types.RoleOwner:
				role = types.OrgRoleOwner
			case types.RoleAdmin:
				role = types.OrgRoleAdmin
			}
			def.Members = append(def.Members, types.OrgMember{
				UserID: u.ID, Role: role, AddedAt: time.Now(),
				Permissions: defaultMemberPermissions(),
			})
		}
		// Adopt unowned resources.
		for _, p := range s.Projects {
			if p.OrgID == "" {
				p.OrgID = def.ID
			}
		}
		for _, n := range s.Nodes {
			if n.OrgID == "" {
				n.OrgID = def.ID
			}
		}
		for _, d := range s.Domains {
			if d.OrgID == "" {
				d.OrgID = def.ID
			}
		}
		// Move the legacy install-wide settings blob under the default org.
		if s.OrgSettings == nil {
			s.OrgSettings = map[string]*Settings{}
		}
		if s.OrgSettings[def.ID] == nil {
			legacy := s.Settings
			s.OrgSettings[def.ID] = &legacy
			s.Settings = Settings{}
		}
	})
}

// SettingsFor returns the org's settings (creating an empty entry lazily).
func (s *Store) SettingsFor(orgID string) Settings {
	var out Settings
	s.View(func() {
		if cfg := s.OrgSettings[orgID]; cfg != nil {
			out = *cfg
		}
	})
	return out
}

// defaultMemberPermissions: new members can build things but not destroy
// them; destructive and API powers are explicit grants (Dokploy defaults).
func defaultMemberPermissions() types.OrgPermissions {
	return types.OrgPermissions{
		CreateProjects: true,
		CreateServices: true,
	}
}

// DefaultOrgID returns the oldest org (the migration default).
func (s *Store) DefaultOrgID() string {
	id, oldest := "", time.Time{}
	s.View(func() {
		for _, o := range s.Orgs {
			if id == "" || o.CreatedAt.Before(oldest) {
				id, oldest = o.ID, o.CreatedAt
			}
		}
	})
	return id
}

// ---- Active-org resolution & authorization ----

// activeOrg resolves the org a request operates in and the caller's
// membership. Precedence: explicit header/query > sole membership > default
// org. Platform owners/admins may operate in any org (synthetic admin
// membership) — they run the install. Returns nil, nil when no org can be
// resolved (e.g. setup mode with zero users).
func (a *API) activeOrg(r *http.Request) (*types.Organization, *types.OrgMember) {
	claims := claimsFrom(r)

	want := r.Header.Get(orgHeader)
	if want == "" {
		want = r.URL.Query().Get("org") // SSE can't set headers
	}

	var org *types.Organization
	var member *types.OrgMember
	a.Store.View(func() {
		if want != "" {
			org = a.Store.Orgs[want]
		} else if claims != nil {
			// No explicit org: the user's first membership (stable order).
			var ids []string
			for id := range a.Store.Orgs {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				if m := a.Store.Orgs[id].Member(claims.Sub); m != nil {
					org = a.Store.Orgs[id]
					member = m
					break
				}
			}
		}
		if org != nil && member == nil && claims != nil {
			member = org.Member(claims.Sub)
		}
	})
	if org == nil {
		if id := a.Store.DefaultOrgID(); id != "" {
			a.Store.View(func() { org = a.Store.Orgs[id] })
			if org != nil && claims != nil {
				member = org.Member(claims.Sub)
			}
		}
	}
	if org == nil {
		return nil, nil
	}
	// Platform owner/admin act as org admin everywhere (synthetic, not stored).
	if member == nil && claims != nil &&
		(claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin)) {
		member = &types.OrgMember{UserID: claims.Sub, Role: types.OrgRoleAdmin}
	}
	// Setup mode (zero users): the whole API is open; grant admin so the
	// first-run flows work before the owner account exists.
	if member == nil && claims == nil {
		users := 0
		a.Store.View(func() { users = len(a.Store.Users) })
		if users == 0 {
			member = &types.OrgMember{Role: types.OrgRoleAdmin}
		}
	}
	if member == nil {
		return nil, nil // authenticated but not a member of this org
	}
	return org, member
}

// requireOrg is the guard every org-scoped handler starts with. Writes a 403
// and returns nil when the caller has no seat in the resolved org.
func (a *API) requireOrg(w http.ResponseWriter, r *http.Request) (*types.Organization, *types.OrgMember) {
	org, member := a.activeOrg(r)
	if org == nil || member == nil {
		http.Error(w, "you are not a member of this organization", http.StatusForbidden)
		return nil, nil
	}
	return org, member
}

// orgCan evaluates one capability for a member (owners/admins hold all).
func orgCan(m *types.OrgMember, check func(types.OrgPermissions) bool) bool {
	if m == nil {
		return false
	}
	if m.Role == types.OrgRoleOwner || m.Role == types.OrgRoleAdmin {
		return true
	}
	return check(m.Permissions)
}

// projectVisible applies the member's project checkbox tree.
func projectVisible(m *types.OrgMember, projectID string) bool {
	if m.Role != types.OrgRoleMember || len(m.Permissions.AccessedProjects) == 0 {
		return true
	}
	for _, id := range m.Permissions.AccessedProjects {
		if id == projectID {
			return true
		}
	}
	return false
}

// ---- Org CRUD ----

func (a *API) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	platformAdmin := claims != nil &&
		(claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin))
	var out []*types.Organization
	a.Store.View(func() {
		for _, o := range a.Store.Orgs {
			if platformAdmin || claims == nil || o.Member(claims.Sub) != nil {
				out = append(out, o)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	writeJSON(w, out)
}

func (a *API) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	// Dokploy self-hosted: only platform owner/admin create orgs.
	if claims.Role != string(types.RoleOwner) && claims.Role != string(types.RoleAdmin) {
		http.Error(w, "only platform admins can create organizations", http.StatusForbidden)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "organization name required", http.StatusBadRequest)
		return
	}
	org := &types.Organization{
		ID: NewID(), Name: req.Name, CreatedBy: claims.Sub, CreatedAt: time.Now(),
		Members: []types.OrgMember{{UserID: claims.Sub, Role: types.OrgRoleOwner, AddedAt: time.Now()}},
	}
	a.Store.Mutate(func() { a.Store.Orgs[org.ID] = org })
	a.Store.AddEvent("org", "Organization created: "+org.Name, "", "")
	writeJSON(w, org)
}

func (a *API) handleUpdateOrg(w http.ResponseWriter, r *http.Request) {
	org, member := a.orgForManage(w, r)
	if org == nil {
		return
	}
	_ = member
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "organization name required", http.StatusBadRequest)
		return
	}
	a.Store.Mutate(func() { org.Name = req.Name })
	writeJSON(w, org)
}

func (a *API) handleDeleteOrg(w http.ResponseWriter, r *http.Request) {
	org, _ := a.orgForManage(w, r)
	if org == nil {
		return
	}
	// Guards: never the last org, never a non-empty one.
	count, empty := 0, true
	a.Store.View(func() {
		count = len(a.Store.Orgs)
		for _, p := range a.Store.Projects {
			if p.OrgID == org.ID {
				empty = false
			}
		}
		for _, n := range a.Store.Nodes {
			if n.OrgID == org.ID {
				empty = false
			}
		}
		for _, d := range a.Store.Domains {
			if d.OrgID == org.ID {
				empty = false
			}
		}
	})
	if count <= 1 {
		http.Error(w, "the last organization cannot be deleted", http.StatusConflict)
		return
	}
	if !empty {
		http.Error(w, "move or delete this organization's projects, machines, and domains first", http.StatusConflict)
		return
	}
	a.Store.Mutate(func() {
		delete(a.Store.Orgs, org.ID)
		for id, inv := range a.Store.Invitations {
			if inv.OrgID == org.ID {
				delete(a.Store.Invitations, id)
			}
		}
	})
	a.Store.AddEvent("org", "Organization deleted: "+org.Name, "", "")
	w.WriteHeader(http.StatusNoContent)
}

// orgForManage loads the org in the {id} path element and requires the caller
// to be an org owner/admin (or platform owner/admin).
func (a *API) orgForManage(w http.ResponseWriter, r *http.Request) (*types.Organization, *types.OrgMember) {
	claims := claimsFrom(r)
	var org *types.Organization
	a.Store.View(func() { org = a.Store.Orgs[r.PathValue("id")] })
	if org == nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return nil, nil
	}
	if claims == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil, nil
	}
	member := org.Member(claims.Sub)
	platformAdmin := claims.Role == string(types.RoleOwner) || claims.Role == string(types.RoleAdmin)
	if !platformAdmin && (member == nil || member.Role == types.OrgRoleMember) {
		http.Error(w, "organization admin access required", http.StatusForbidden)
		return nil, nil
	}
	if member == nil {
		member = &types.OrgMember{UserID: claims.Sub, Role: types.OrgRoleAdmin}
	}
	return org, member
}

// ---- Members ----

func (a *API) handleSetOrgMember(w http.ResponseWriter, r *http.Request) {
	org, _ := a.orgForManage(w, r)
	if org == nil {
		return
	}
	userID := r.PathValue("userId")
	var req struct {
		Role        *types.OrgRole        `json:"role,omitempty"`
		Permissions *types.OrgPermissions `json:"permissions,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Role != nil {
		switch *req.Role {
		case types.OrgRoleOwner, types.OrgRoleAdmin, types.OrgRoleMember:
		default:
			http.Error(w, "role must be owner, admin, or member", http.StatusBadRequest)
			return
		}
	}
	status := 0
	a.Store.Mutate(func() {
		m := org.Member(userID)
		if m == nil {
			status = http.StatusNotFound
			return
		}
		// The last owner cannot be demoted — someone must hold the org.
		if req.Role != nil && m.Role == types.OrgRoleOwner && *req.Role != types.OrgRoleOwner {
			owners := 0
			for _, om := range org.Members {
				if om.Role == types.OrgRoleOwner {
					owners++
				}
			}
			if owners <= 1 {
				status = http.StatusConflict
				return
			}
		}
		if req.Role != nil {
			m.Role = *req.Role
		}
		if req.Permissions != nil {
			m.Permissions = *req.Permissions
		}
	})
	switch status {
	case http.StatusNotFound:
		http.Error(w, "not a member of this organization", http.StatusNotFound)
	case http.StatusConflict:
		http.Error(w, "an organization needs at least one owner — promote someone else first", http.StatusConflict)
	default:
		writeJSON(w, org)
	}
}

func (a *API) handleRemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	org, _ := a.orgForManage(w, r)
	if org == nil {
		return
	}
	userID := r.PathValue("userId")
	conflict := false
	a.Store.Mutate(func() {
		m := org.Member(userID)
		if m != nil && m.Role == types.OrgRoleOwner {
			owners := 0
			for _, om := range org.Members {
				if om.Role == types.OrgRoleOwner {
					owners++
				}
			}
			if owners <= 1 {
				conflict = true
				return
			}
		}
		kept := org.Members[:0]
		for _, om := range org.Members {
			if om.UserID != userID {
				kept = append(kept, om)
			}
		}
		org.Members = kept
	})
	if conflict {
		http.Error(w, "an organization needs at least one owner — promote someone else first", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Invitations ----

func (a *API) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	org, member := a.orgForManage(w, r)
	if org == nil {
		return
	}
	var req struct {
		Email string        `json:"email"`
		Role  types.OrgRole `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(req.Email, "@") {
		http.Error(w, "valid email required", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = types.OrgRoleMember
	}
	if req.Role == types.OrgRoleOwner {
		// Ownership is granted by promotion, never by invite link.
		req.Role = types.OrgRoleAdmin
	}
	// Already a member? Point that out instead of minting a useless link.
	already := false
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			if u.Email == req.Email && org.Member(u.ID) != nil {
				already = true
			}
		}
	})
	if already {
		http.Error(w, req.Email+" is already a member of this organization", http.StatusConflict)
		return
	}

	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	token := hex.EncodeToString(raw)
	inv := &types.Invitation{
		ID: NewID(), OrgID: org.ID, Email: req.Email, Role: req.Role,
		TokenHash: sha256Hex(token), Status: types.InvitePending,
		CreatedBy: member.UserID, CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(48 * time.Hour),
	}
	a.Store.Mutate(func() { a.Store.Invitations[inv.ID] = inv })
	a.Store.AddEvent("org", "Invitation created for "+req.Email+" → "+org.Name, "", "")

	// The raw token exists only in this response — the admin copies the link.
	writeJSON(w, map[string]any{
		"invitation": inv.Sanitized(),
		"link":       a.dashboardURL(r) + "/invitation?token=" + token,
	})
}

func (a *API) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	org, _ := a.orgForManage(w, r)
	if org == nil {
		return
	}
	var out []*types.Invitation
	a.Store.View(func() {
		for _, inv := range a.Store.Invitations {
			if inv.OrgID == org.ID && inv.Status == types.InvitePending && time.Now().Before(inv.ExpiresAt) {
				out = append(out, inv.Sanitized())
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	writeJSON(w, out)
}

func (a *API) handleDeleteInvitation(w http.ResponseWriter, r *http.Request) {
	org, _ := a.orgForManage(w, r)
	if org == nil {
		return
	}
	a.Store.Mutate(func() {
		if inv := a.Store.Invitations[r.PathValue("invId")]; inv != nil && inv.OrgID == org.ID {
			delete(a.Store.Invitations, inv.ID)
		}
	})
	w.WriteHeader(http.StatusNoContent)
}

// findInvitation resolves a raw invite token to a pending, unexpired invitation.
func (a *API) findInvitation(token string) *types.Invitation {
	want := sha256Hex(strings.TrimSpace(token))
	var found *types.Invitation
	a.Store.View(func() {
		now := time.Now()
		for _, inv := range a.Store.Invitations {
			if inv.Status == types.InvitePending && now.Before(inv.ExpiresAt) &&
				subtle.ConstantTimeCompare([]byte(inv.TokenHash), []byte(want)) == 1 {
				found = inv
			}
		}
	})
	return found
}

// handleGetInvitation lets the invitation page render before signup (open route).
func (a *API) handleGetInvitation(w http.ResponseWriter, r *http.Request) {
	inv := a.findInvitation(r.PathValue("token"))
	if inv == nil {
		http.Error(w, "this invitation is invalid or has expired — ask for a new link", http.StatusNotFound)
		return
	}
	var orgName string
	a.Store.View(func() {
		if o := a.Store.Orgs[inv.OrgID]; o != nil {
			orgName = o.Name
		}
	})
	writeJSON(w, map[string]any{"email": inv.Email, "org": orgName, "role": inv.Role})
}

// handleAcceptInvitation creates the account (or enrolls an existing one with
// the invited email) and consumes the invitation. Open route: the whole point
// is onboarding someone who has no account yet.
func (a *API) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		FullName string `json:"fullName"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	inv := a.findInvitation(req.Token)
	if inv == nil {
		http.Error(w, "this invitation is invalid or has expired — ask for a new link", http.StatusNotFound)
		return
	}

	var user *types.User
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			if u.Email == inv.Email {
				user = u
			}
		}
	})
	if user == nil {
		if len(req.Password) < 8 {
			http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		user = &types.User{
			ID: NewID(), Email: inv.Email, FullName: strings.TrimSpace(req.FullName),
			PasswordHash: string(hash), Role: types.RoleMember, CreatedAt: time.Now(),
		}
		a.Store.Mutate(func() { a.Store.Users[user.ID] = user })
	}

	var orgName string
	a.Store.Mutate(func() {
		org := a.Store.Orgs[inv.OrgID]
		if org == nil {
			return
		}
		orgName = org.Name
		if org.Member(user.ID) == nil {
			org.Members = append(org.Members, types.OrgMember{
				UserID: user.ID, Role: inv.Role, AddedAt: time.Now(),
				Permissions: defaultMemberPermissions(),
			})
		}
		inv.Status = types.InviteAccepted
	})
	if orgName == "" {
		http.Error(w, "the organization behind this invitation no longer exists", http.StatusGone)
		return
	}
	a.Store.AddEvent("org", "Invitation accepted: "+user.Email+" joined "+orgName, "", "")

	token := signJWT(a.Store.SessionSecret(), jwtClaims{
		Sub: user.ID, Role: string(user.Role), Exp: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	writeJSON(w, map[string]any{"token": token, "user": user.Sanitized(), "org": orgName})
}
