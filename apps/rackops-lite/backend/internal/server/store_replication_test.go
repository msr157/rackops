package server

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestReplicationIncludesTenantAndSupportState(t *testing.T) {
	leader, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer leader.Close()
	follower, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()
	leader.Mutate(func() {
		leader.Orgs["org"] = &types.Organization{ID: "org"}
		leader.Invitations["invite"] = &types.Invitation{}
		leader.Tickets["ticket"] = &types.SupportTicket{}
		leader.EarlyAccess["lead"] = &types.EarlyAccessLead{}
		leader.OrgSettings = map[string]*Settings{"org": {OpenAIKey: "private-key"}}
		leader.DashboardURL = "https://example.test"
		leader.InstanceName = "replicated install"
	})
	follower.EnqueueTask("local", types.AgentTask{ID: "local-task"})
	follower.Mutate(func() {
		follower.TaskOperations["local-operation"] = &TaskOperation{NodeID: "local", State: "delivered"}
	})
	var data []byte
	leader.View(func() { data, err = json.Marshal(leader) })
	if err != nil {
		t.Fatal(err)
	}
	var callbacks atomic.Int32
	follower.OnDirty = func([]byte) { callbacks.Add(1) }
	if err := follower.ImportJSON(data); err != nil {
		t.Fatal(err)
	}
	if err := follower.persistOnce(); err != nil {
		t.Fatal(err)
	}
	if callbacks.Load() != 0 {
		t.Fatal("import was echoed to replication")
	}
	var expected, actual map[string]json.RawMessage
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	follower.View(func() { data, err = json.Marshal(follower) })
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"taskQueue", "taskOperations"} {
		delete(expected, key)
		delete(actual, key)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("replication omitted or changed persisted product fields")
	}
	if len(follower.TaskQueue["local"]) != 1 || follower.TaskOperations["local-operation"] == nil {
		t.Fatal("replication replaced local delivery ownership")
	}
	follower.Mutate(func() { follower.InstanceName = "local mutation" })
	if err := follower.persistOnce(); err != nil {
		t.Fatal(err)
	}
	if callbacks.Load() != 1 {
		t.Fatal("local mutation was not proposed")
	}
	if err := follower.ImportJSON([]byte(`{"schemaVersion":999}`)); err == nil {
		t.Fatal("accepted newer schema")
	}
	if err := follower.ImportJSON([]byte(`{}`)); err == nil {
		t.Fatal("accepted incomplete state")
	}
	if follower.InstanceName != "local mutation" {
		t.Fatal("rejected import mutated state")
	}
}
