package server

import (
	"encoding/json"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestUnavailableInventoryDoesNotTriggerHealing(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	a.History = NewHistory(t.TempDir())
	a.JoinToken = "test-join"
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.Nodes["n"] = &types.Node{ID: "n", OrgID: org}
		a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
		a.Store.Services["s"] = &types.Service{ID: "s", ProjectID: "p", NodeID: "n", ContainerID: "existing", Status: types.StatusRunning}
		a.Store.Services["foreign"] = &types.Service{ID: "foreign", ProjectID: "p", NodeID: "other", ContainerID: "original", Status: types.StatusRunning}
	})
	valid := false
	report := types.AgentReport{NodeID: "n", ContainersValid: &valid, Containers: []types.ContainerStatus{{ID: "forged", ServiceID: "foreign", State: "exited"}}}
	for _, authoritative := range []bool{false, true} {
		valid = authoritative
		if valid {
			report.Containers = append(report.Containers, types.ContainerStatus{ID: "existing", ServiceID: "s", State: "running"})
		}
		body, _ := json.Marshal(report)
		response := do(a, "POST", "/api/agent/ingest", string(body), map[string]string{"X-Rackops-Token": a.JoinToken})
		if response.Code != 200 {
			t.Fatalf("ingest: %d %s", response.Code, response.Body)
		}
		if a.Store.Services["s"].Status != types.StatusRunning || len(a.Store.Deployments) != 0 {
			t.Fatal("unavailable inventory triggered healing")
		}
		if a.Store.Services["foreign"].ContainerID != "original" {
			t.Fatal("agent changed another node's service")
		}
	}
}
