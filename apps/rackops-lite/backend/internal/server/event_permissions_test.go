package server

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestLogEventsRecheckProjectAccessAndRevocation(t *testing.T) {
	a, ids := testAPI(t, types.RoleMember, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	userID := ids[types.RoleMember]
	a.Store.Mutate(func() {
		a.Store.Projects["visible"] = &types.Project{ID: "visible", OrgID: org}
		a.Store.Projects["hidden"] = &types.Project{ID: "hidden", OrgID: org}
		a.Store.Services["app"] = &types.Service{ID: "app", ProjectID: "visible", NodeID: "node"}
		a.Store.Services["secret"] = &types.Service{ID: "secret", ProjectID: "hidden", NodeID: "node"}
		a.Store.Orgs[org].Member(userID).Permissions.AccessedProjects = []string{"visible"}
	})
	token := loginToken(t, a, "member@test.local")
	r := httptest.NewRequest("GET", "/api/stream?token="+token, nil)
	message := func(id string) []byte {
		return []byte(fmt.Sprintf("event: log_chunk\ndata: {\"serviceId\":%q,\"text\":\"private log\"}\n\n", id))
	}
	if !a.eventVisible(r, org, message("app")) || a.eventVisible(r, org, message("secret")) {
		t.Fatal("project isolation failed")
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	for _, req := range []struct{ method, path, body string }{
		{"POST", "/api/services/secret/logs/stream", `{"streamId":"stream"}`},
		{"DELETE", "/api/services/secret/logs/stream?streamId=stream", ""},
	} {
		if response := do(a, req.method, req.path, req.body, headers); response.Code != 404 {
			t.Fatalf("hidden log control: %d", response.Code)
		}
	}
	a.Store.Mutate(func() { a.Store.Orgs[org].Member(userID).Permissions.AccessedProjects = []string{"hidden"} })
	if a.eventVisible(r, org, message("app")) {
		t.Fatal("queued event survived permission revocation")
	}
	a.Store.Mutate(func() { delete(a.Store.Users, userID) })
	if a.eventVisible(r, org, message("secret")) {
		t.Fatal("deleted user retained open stream access")
	}
	if response := do(a, "GET", "/api/services", "", headers); response.Code != 401 {
		t.Fatal("deleted user's JWT remained authorized")
	}
}

func TestLogIngestRejectsForgedDeployment(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.JoinToken = "test-join"
	a.Store.Mutate(func() {
		a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
		a.Store.Services["s"] = &types.Service{ID: "s", ProjectID: "p", NodeID: "n"}
		a.Store.Deployments["other"] = &types.Deployment{ID: "other", ServiceID: "another"}
	})
	headers := map[string]string{"X-Rackops-Token": a.JoinToken, "X-Rackops-Node": "n", "X-Rackops-Service": "s", "X-Rackops-Kind": "deploy", "X-Rackops-Stream": "other"}
	response := do(a, "POST", "/api/agent/logstream", "injected log", headers)
	if response.Code != 403 || a.Store.Deployments["other"].Log != "" {
		t.Fatal("forged deployment log accepted")
	}
}
