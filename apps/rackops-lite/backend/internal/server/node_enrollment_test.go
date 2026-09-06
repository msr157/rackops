package server

import (
	"encoding/json"
	"fmt"
	"github.com/rackops/rackops/internal/types"
	"strings"
	"testing"
	"time"
)

func TestNodeEnrollmentBindsOneNodeAndOrganization(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.JoinToken = "legacy-global"
	org := a.Store.DefaultOrgID()
	response := do(a, "POST", "/api/node-enrollments", `{}`, map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")})
	var grant struct {
		Token string `json:"token"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &grant) != nil || grant.Token == "" {
		t.Fatal("enrollment issue failed")
	}
	token := "rknode_" + strings.Repeat("a", 64)
	body := fmt.Sprintf(`{"nodeId":"node-fixture","credential":%q,"orgId":"attacker-org"}`, token)
	headers := map[string]string{"X-Rackops-Token": grant.Token}
	if r := do(a, "POST", "/api/agent/enroll", body, headers); r.Code != 200 {
		t.Fatalf("enroll: %d %s", r.Code, r.Body)
	}
	if a.Store.Nodes["node-fixture"].OrgID != org || !a.agentAuthorized("node-fixture", token) {
		t.Fatal("node not bound to issuing organization")
	}
	if a.agentAuthorized("node-fixture", a.JoinToken) || a.agentAuthorized("another-node", token) {
		t.Fatal("node credential scope bypassed")
	}
	if r := do(a, "POST", "/api/agent/enroll", strings.Replace(body, "node-fixture", "another-node", 1), headers); r.Code != 403 {
		t.Fatal("grant enrolled a second node")
	}
	a.Store.Mutate(func() { a.Store.NodeEnrollments[sha256Hex(grant.Token)].ExpiresAt = time.Now().Add(-time.Hour) })
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	if r := do(a, "POST", "/api/agent/enroll", body, headers); r.Code != 200 {
		t.Fatal("same node cannot recover a lost enrollment response")
	}
	if r := do(a, "POST", "/api/agent/ingest", `{"nodeId":"node-fixture"}`, map[string]string{"X-Rackops-Token": a.JoinToken}); r.Code != 401 {
		t.Fatal("global token impersonated scoped node")
	}
	if r := do(a, "POST", "/api/agent/logstream", "", map[string]string{"X-Rackops-Token": token, "X-Rackops-Node": "another-node"}); r.Code != 401 {
		t.Fatal("cross-node log authentication accepted")
	}
	a.Store.Mutate(func() { delete(a.Store.Nodes, "node-fixture") })
	if a.agentAuthorized("node-fixture", token) {
		t.Fatal("removed node credential remained usable")
	}
}

func TestExpiredEnrollmentCannotClaimExistingMachine(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.NodeEnrollments = map[string]*NodeEnrollment{sha256Hex("expired"): {OrgID: org, ExpiresAt: time.Now().Add(-time.Second)}, sha256Hex("valid"): {OrgID: org, ExpiresAt: time.Now().Add(time.Hour)}}
		a.Store.Nodes["existing-node"] = &types.Node{ID: "existing-node", OrgID: org}
	})
	for _, item := range []struct{ proof, node string }{{"expired", "new-node"}, {"valid", "existing-node"}} {
		r := do(a, "POST", "/api/agent/enroll", fmt.Sprintf(`{"nodeId":%q,"credential":%q}`, item.node, "rknode_"+strings.Repeat("b", 64)), map[string]string{"X-Rackops-Token": item.proof})
		if r.Code != 403 {
			t.Fatalf("unauthorized enrollment: %d", r.Code)
		}
	}
}
