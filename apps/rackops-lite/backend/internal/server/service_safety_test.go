package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestMachineMoveRejectedWithoutMutation(t *testing.T) {
	for _, body := range []string{
		`{"nodeId":"new","image":"changed"}`,
		`{"nodeId":"new","redeploy":true,"mounts":[]}`,
	} {
		a, _ := testAPI(t, types.RoleOwner)
		org := a.Store.DefaultOrgID()
		svc := &types.Service{ID: "app", NodeID: "old", ProjectID: "p", Image: "postgres:16", Status: types.StatusRunning}
		a.Store.Mutate(func() {
			a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
			a.Store.Nodes["old"] = &types.Node{ID: "old", OrgID: org}
			a.Store.Nodes["new"] = &types.Node{ID: "new", OrgID: org}
			a.Store.Services[svc.ID] = svc
		})
		rec := do(a, "PATCH", "/api/services/app", body, map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")})
		if rec.Code != http.StatusConflict {
			t.Fatalf("move: %d %s", rec.Code, rec.Body)
		}
		if svc.NodeID != "old" || svc.Image != "postgres:16" || svc.Status != types.StatusRunning || len(a.Store.TaskQueue) != 0 {
			t.Fatal("rejected migration changed the source or queued work")
		}
	}
}

func TestServiceAndHostControlsDenyCrossOrgAndMissingCapabilities(t *testing.T) {
	for _, crossOrg := range []bool{false, true} {
		a, ids := testAPI(t, types.RoleOwner, types.RoleMember)
		org := a.Store.DefaultOrgID()
		targetOrg := org
		if crossOrg {
			targetOrg = "other"
		}
		a.Store.Mutate(func() {
			for i := range a.Store.Orgs[org].Members {
				if a.Store.Orgs[org].Members[i].UserID == ids[types.RoleMember] {
					a.Store.Orgs[org].Members[i].Permissions = types.OrgPermissions{}
				}
			}
			a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: targetOrg}
			a.Store.Nodes["node"] = &types.Node{ID: "node", OrgID: targetOrg}
			a.Store.Services["app"] = &types.Service{ID: "app", NodeID: "node", ProjectID: "p", Port: 8080, Status: types.StatusRunning}
		})
		headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "member@test.local")}
		for _, req := range []struct{ method, path, body string }{
			{"GET", "/api/nodes/node/fs", ""},
			{"POST", "/api/services/app/migration-preflight", `{"provider":"aws","region":"us-east-1"}`},
			{"POST", "/api/services/app/clear-logs", ""},
			{"PATCH", "/api/services/app", `{"image":"changed"}`},
			{"POST", "/api/services/app/stop", ""},
			{"POST", "/api/services/app/operations/task/resolve", `{"inspected":true}`},
			{"POST", "/api/services/app/quick-tunnel", ""},
			{"DELETE", "/api/services/app/quick-tunnel", ""},
			{"POST", "/api/services/app/expose", ""},
			{"POST", "/api/services/app/domain", `{"domain":"example.com"}`},
		} {
			rec := do(a, req.method, req.path, req.body, headers)
			want := http.StatusForbidden
			if crossOrg {
				want = http.StatusNotFound
			}
			if rec.Code != want {
				t.Fatalf("crossOrg=%v %s %s: %d %s", crossOrg, req.method, req.path, rec.Code, rec.Body)
			}
		}
		if len(a.Store.TaskQueue) != 0 || a.Store.Services["app"].LogsClearedAt != (time.Time{}) || a.Store.Services["app"].Status != types.StatusRunning {
			t.Fatal("denied operation changed state or queued agent work")
		}
	}
}
