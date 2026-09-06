package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestAcknowledgedComposeLifecycleUpdatesStatus(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
		a.Store.Services["s"] = &types.Service{ID: "s", ProjectID: "p", NodeID: "n", ComposeYML: "services: {}", Status: types.StatusStopped}
	})
	for _, tc := range []struct {
		kind   types.TaskKind
		status types.ServiceStatus
	}{
		{types.TaskStart, types.StatusRunning}, {types.TaskStop, types.StatusStopped}, {types.TaskRestart, types.StatusRunning},
	} {
		id := string(tc.kind)
		a.Store.Mutate(func() {
			a.Store.TaskOperations[id] = &TaskOperation{NodeID: "n", State: "delivered", Task: types.AgentTask{ID: id, ServiceID: "s", Kind: tc.kind}}
		})
		if !a.acceptTaskResult("n", types.TaskResult{TaskID: id, ServiceID: "s", OK: true}) || a.Store.Services["s"].Status != tc.status {
			t.Fatalf("lifecycle %s did not update status", tc.kind)
		}
	}
}

func TestUncertainTaskPausesHealingUntilInspection(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.Projects["p"] = &types.Project{ID: "p", OrgID: org}
		a.Store.Nodes["n"] = &types.Node{ID: "n", OrgID: org}
		a.Store.Services["s"] = &types.Service{ID: "s", NodeID: "n", ProjectID: "p", Status: types.StatusFailed}
		a.Store.TaskOperations["t"] = &TaskOperation{NodeID: "n", State: "recovery_required", Task: types.AgentTask{ID: "t", ServiceID: "s", Payload: "private configuration"}}
	})
	a.reconcileNode("n", nil)
	if len(a.Store.TaskQueue["n"]) != 0 {
		t.Fatal("uncertain task automatically redeployed")
	}
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	response := do(a, "GET", "/api/services/s/operations", "", headers)
	if response.Code != 200 || strings.Contains(response.Body.String(), "private") {
		t.Fatalf("unsafe operation response: %s", response.Body)
	}
	response = do(a, "POST", "/api/services/s/operations/t/resolve", `{}`, headers)
	if response.Code != http.StatusBadRequest {
		t.Fatal("inspection not required")
	}
	response = do(a, "POST", "/api/services/s/operations/t/resolve", `{"inspected":true}`, headers)
	if response.Code != 200 || a.Store.TaskOperations["t"].State != "completed" {
		t.Fatalf("inspection failed: %s", response.Body)
	}
}

func TestTaskDeliverySurvivesLostResponseAndRestart(t *testing.T) {
	t.Setenv("RACKOPS_STATE_KEY_FILE", "")
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.EnqueueTask("node", types.AgentTask{ID: "task", ServiceID: "svc", Payload: "secret", CreatedAt: now})
	tasks, err := s.deliverTasks("node", 1, now)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("first delivery: %v %v", tasks, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if tasks, err = s.deliverTasks("other", 64, now.Add(time.Minute)); err != nil || len(tasks) != 0 {
		t.Fatal("foreign delivery")
	}
	// Retransmit even if the agent has no free slots: it may already be
	// running this ID and will deduplicate it locally.
	tasks, err = s.deliverTasks("node", 0, now.Add(time.Minute))
	if err != nil || len(tasks) != 1 || tasks[0].ID != "task" {
		t.Fatalf("lost assignment: %v %v", tasks, err)
	}
	a := &API{Store: s}
	if a.acceptTaskResult("other", types.TaskResult{TaskID: "task", ServiceID: "svc"}) {
		t.Fatal("foreign result accepted")
	}
	if a.acceptTaskResult("node", types.TaskResult{TaskID: "task", ServiceID: "other"}) {
		t.Fatal("forged service accepted")
	}
	// Mark terminal directly to isolate transport deduplication from UI effects.
	s.Mutate(func() { s.TaskOperations["task"].State = "completed"; s.TaskOperations["task"].Task.Payload = "" })
	if !a.acceptTaskResult("node", types.TaskResult{TaskID: "task", ServiceID: "svc"}) {
		t.Fatal("duplicate result was not acknowledged")
	}
	if tasks, err = s.deliverTasks("node", 64, now.Add(time.Hour)); err != nil || len(tasks) != 0 {
		t.Fatal("terminal operation redelivered")
	}
}
