package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestJournalSurvivesRestartAndRejectsChangedPayload(t *testing.T) {
	dir := t.TempDir()
	j, err := openTaskJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	task := types.AgentTask{ID: "task", ServiceID: "svc", Payload: "secret payload"}
	calls := 0
	run := func(context.Context, types.AgentTask) *types.TaskResult {
		calls++
		return &types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID, OK: true, Log: "secret log"}
	}
	if !j.execute(context.Background(), task, run).OK {
		t.Fatal("first execution failed")
	}
	j, err = openTaskJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := j.execute(context.Background(), task, run); !got.OK || got.Log != "secret log" || calls != 1 {
		t.Fatalf("replayed execution: %+v, calls %d", got, calls)
	}
	task.Payload = "changed"
	if j.execute(context.Background(), task, run).OK || calls != 1 {
		t.Fatal("accepted reused task ID")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "task-journal", "*.receipt"))
	for _, path := range files {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), "secret") {
			t.Fatal("journal leaked secret")
		}
	}
}

func TestJournalInterruptedExecutionNeverReruns(t *testing.T) {
	dir := t.TempDir()
	j, err := openTaskJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	task := types.AgentTask{ID: "interrupted"}
	func() {
		defer func() { _ = recover() }()
		j.execute(context.Background(), task, func(context.Context, types.AgentTask) *types.TaskResult { panic("simulated process termination") })
	}()
	j, err = openTaskJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := j.execute(context.Background(), task, func(context.Context, types.AgentTask) *types.TaskResult { t.Fatal("reran uncertain task"); return nil })
	if res.OK || !strings.Contains(res.Log, "uncertain") {
		t.Fatalf("missing recovery outcome: %+v", res)
	}
	if err := os.Remove(filepath.Join(dir, "task-journal", "key")); err != nil {
		t.Fatal(err)
	}
	if _, err := openTaskJournal(dir); err == nil {
		t.Fatal("regenerated missing journal key")
	}
}
