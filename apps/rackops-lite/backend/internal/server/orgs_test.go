package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func orgID(t *testing.T, a *API) string {
	t.Helper()
	id := a.Store.DefaultOrgID()
	if id == "" {
		t.Fatal("no default org after EnsureDefaultOrg")
	}
	return id
}

func TestEnsureDefaultOrgMigration(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Pre-org world: users, a project, a node, a domain — none org-stamped —
	// plus a legacy install-wide settings blob.
	owner := &types.User{ID: NewID(), Email: "o@x", Role: types.RoleOwner, CreatedAt: time.Now()}
	member := &types.User{ID: NewID(), Email: "m@x", Role: types.RoleMember, CreatedAt: time.Now()}
	store.Mutate(func() {
		store.Users[owner.ID] = owner
		store.Users[member.ID] = member
		store.Projects["p1"] = &types.Project{ID: "p1", Name: "legacy"}
		store.Nodes["n1"] = &types.Node{ID: "n1", Name: "box"}
		store.Domains["d1"] = &types.Domain{ID: "d1", Name: "x.example.com"}
		store.Settings = Settings{DiscordWebhook: "https://discord/hook"}
	})

	store.EnsureDefaultOrg()
	store.EnsureDefaultOrg() // idempotent

	if len(store.Orgs) != 1 {
		t.Fatalf("orgs = %d, want exactly 1", len(store.Orgs))
	}
	def := store.Orgs[store.DefaultOrgID()]
	if def.Member(owner.ID) == nil || def.Member(owner.ID).Role != types.OrgRoleOwner {
		t.Fatal("platform owner should be org owner")
	}
	if def.Member(member.ID) == nil || def.Member(member.ID).Role != types.OrgRoleMember {
		t.Fatal("platform member should be org member")
	}
	if store.Projects["p1"].OrgID != def.ID || store.Nodes["n1"].OrgID != def.ID || store.Domains["d1"].OrgID != def.ID {
		t.Fatal("legacy resources not adopted into the default org")
	}
	if store.SettingsFor(def.ID).DiscordWebhook != "https://discord/hook" {
		t.Fatal("legacy settings not migrated to the default org")
	}
}

func TestOrgScopingHidesOtherTenants(t *testing.T) {
	a, ids := testAPI(t, types.RoleOwner)
	defOrg := orgID(t, a)
	ownerJWT := loginToken(t, a, "owner@test.local")

	// A second org with its own user, node and project.
	outsider := &types.User{ID: NewID(), Email: "eve@test.local", Role: types.RoleMember, CreatedAt: time.Now()}
	org2 := &types.Organization{ID: NewID(), Name: "Tenant B", CreatedAt: time.Now().Add(time.Second),
		Members: []types.OrgMember{{UserID: outsider.ID, Role: types.OrgRoleOwner, AddedAt: time.Now()}}}
	a.Store.Mutate(func() {
		a.Store.Users[outsider.ID] = outsider
		a.Store.Orgs[org2.ID] = org2
		a.Store.Nodes["nb"] = &types.Node{ID: "nb", Name: "b-box", OrgID: org2.ID, Status: types.NodeOnline}
		a.Store.Nodes["na"] = &types.Node{ID: "na", Name: "a-box", OrgID: defOrg, Status: types.NodeOnline}
		a.Store.Projects["pb"] = &types.Project{ID: "pb", Name: "b-proj", OrgID: org2.ID}
	})
	_ = ids

	// Default org sees only its own node.
	rec := do(a, http.MethodGet, "/api/nodes", "", map[string]string{"Authorization": "Bearer " + ownerJWT})
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "b-box") {
		t.Fatalf("default org must not see tenant B's node: %d %s", rec.Code, rec.Body)
	}
	// Platform owner may switch into org2 explicitly (they run the install)…
	rec = do(a, http.MethodGet, "/api/nodes", "", map[string]string{
		"Authorization": "Bearer " + ownerJWT, "X-Rackops-Org": org2.ID})
	if !strings.Contains(rec.Body.String(), "b-box") {
		t.Fatalf("platform owner switched to org2 should see b-box: %s", rec.Body)
	}
	// …but a plain member of the default org may NOT.
	memberU := &types.User{ID: NewID(), Email: "bob@test.local", Role: types.RoleMember, CreatedAt: time.Now(),
		PasswordHash: a.mustHash(t, "correct-horse")}
	a.Store.Mutate(func() {
		a.Store.Users[memberU.ID] = memberU
		def := a.Store.Orgs[defOrg]
		def.Members = append(def.Members, types.OrgMember{UserID: memberU.ID, Role: types.OrgRoleMember, AddedAt: time.Now()})
	})
	bobJWT := loginToken(t, a, "bob@test.local")
	rec = do(a, http.MethodGet, "/api/nodes", "", map[string]string{
		"Authorization": "Bearer " + bobJWT, "X-Rackops-Org": org2.ID})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member switching orgs: status %d, want 403", rec.Code)
	}
	// Cross-org project access: bob cannot list envs of tenant B's project.
	rec = do(a, http.MethodGet, "/api/projects/pb/envs", "", map[string]string{"Authorization": "Bearer " + bobJWT})
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "production") {
		t.Fatalf("cross-org env listing leaked: %s", rec.Body)
	}
}

// mustHash is a test helper on API to keep bcrypt in one place.
func (a *API) mustHash(t *testing.T, pw string) string {
	t.Helper()
	rec := do(a, http.MethodPost, "/api/auth/login", `{"email":"owner@test.local","password":"`+pw+`"}`, nil)
	_ = rec // we only need the shared bcrypt hash generation from testAPI; reuse it
	var h string
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			if u.PasswordHash != "" {
				h = u.PasswordHash
				break
			}
		}
	})
	return h
}

func TestOrgMemberPermissions(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	defOrg := orgID(t, a)
	ownerJWT := loginToken(t, a, "owner@test.local")

	// A member without CreateProjects.
	u := &types.User{ID: NewID(), Email: "carol@test.local", Role: types.RoleMember, CreatedAt: time.Now(),
		PasswordHash: a.mustHash(t, "correct-horse")}
	a.Store.Mutate(func() {
		a.Store.Users[u.ID] = u
		def := a.Store.Orgs[defOrg]
		def.Members = append(def.Members, types.OrgMember{UserID: u.ID, Role: types.OrgRoleMember, AddedAt: time.Now()})
	})
	carolJWT := loginToken(t, a, "carol@test.local")

	if rec := do(a, http.MethodPost, "/api/projects", `{"name":"nope"}`,
		map[string]string{"Authorization": "Bearer " + carolJWT}); rec.Code != http.StatusForbidden {
		t.Fatalf("member without permission creating project: %d, want 403", rec.Code)
	}
	// Grant the permission via the members endpoint.
	rec := do(a, http.MethodPatch, "/api/orgs/"+defOrg+"/members/"+u.ID,
		`{"permissions":{"createProjects":true,"createServices":true}}`,
		map[string]string{"Authorization": "Bearer " + ownerJWT})
	if rec.Code != http.StatusOK {
		t.Fatalf("set member permissions: %d %s", rec.Code, rec.Body)
	}
	if rec := do(a, http.MethodPost, "/api/projects", `{"name":"allowed"}`,
		map[string]string{"Authorization": "Bearer " + carolJWT}); rec.Code != http.StatusOK {
		t.Fatalf("member with permission creating project: %d %s", rec.Code, rec.Body)
	}
}

func TestInvitationLifecycle(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	defOrg := orgID(t, a)
	ownerJWT := loginToken(t, a, "owner@test.local")

	// Create an invitation and pull the link's token.
	rec := do(a, http.MethodPost, "/api/orgs/"+defOrg+"/invitations",
		`{"email":"newbie@test.local","role":"member"}`,
		map[string]string{"Authorization": "Bearer " + ownerJWT})
	if rec.Code != http.StatusOK {
		t.Fatalf("create invitation: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Link string `json:"link"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	tok := out.Link[strings.LastIndex(out.Link, "=")+1:]
	if tok == "" {
		t.Fatalf("no token in link %q", out.Link)
	}

	// Public preview shows email + org.
	rec = do(a, http.MethodGet, "/api/auth/invitation/"+tok, "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "newbie@test.local") {
		t.Fatalf("invitation preview: %d %s", rec.Code, rec.Body)
	}

	// Accept: creates the account, enrolls it, signs in.
	rec = do(a, http.MethodPost, "/api/auth/invitation",
		`{"token":"`+tok+`","fullName":"New Bie","password":"super-secret-1"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept invitation: %d %s", rec.Code, rec.Body)
	}
	enrolled := false
	a.Store.View(func() {
		for _, u := range a.Store.Users {
			if u.Email == "newbie@test.local" && a.Store.Orgs[defOrg].Member(u.ID) != nil {
				enrolled = true
			}
		}
	})
	if !enrolled {
		t.Fatal("accepted invitee not enrolled in org")
	}

	// Single use: the same token is dead now.
	rec = do(a, http.MethodPost, "/api/auth/invitation",
		`{"token":"`+tok+`","password":"whatever-123"}`, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invitation reuse: %d, want 404", rec.Code)
	}
}

func TestOrgDeleteGuards(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	defOrg := orgID(t, a)
	ownerJWT := loginToken(t, a, "owner@test.local")

	// Last org: refuse.
	if rec := do(a, http.MethodDelete, "/api/orgs/"+defOrg, "",
		map[string]string{"Authorization": "Bearer " + ownerJWT}); rec.Code != http.StatusConflict {
		t.Fatalf("delete last org: %d, want 409", rec.Code)
	}
	// Second org, but non-empty: refuse; empty: allow.
	rec := do(a, http.MethodPost, "/api/orgs", `{"name":"Second"}`,
		map[string]string{"Authorization": "Bearer " + ownerJWT})
	var org2 types.Organization
	_ = json.Unmarshal(rec.Body.Bytes(), &org2)
	a.Store.Mutate(func() {
		a.Store.Nodes["nx"] = &types.Node{ID: "nx", OrgID: org2.ID}
	})
	if rec := do(a, http.MethodDelete, "/api/orgs/"+org2.ID, "",
		map[string]string{"Authorization": "Bearer " + ownerJWT}); rec.Code != http.StatusConflict {
		t.Fatalf("delete non-empty org: %d, want 409", rec.Code)
	}
	a.Store.Mutate(func() { delete(a.Store.Nodes, "nx") })
	if rec := do(a, http.MethodDelete, "/api/orgs/"+org2.ID, "",
		map[string]string{"Authorization": "Bearer " + ownerJWT}); rec.Code != http.StatusNoContent {
		t.Fatalf("delete empty org: %d, want 204", rec.Code)
	}
}
