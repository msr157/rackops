package agent

import (
	"context"
	"github.com/rackops/rackops/internal/types"
	"testing"
	"time"
)

func TestWorkersKeepServiceOrderAndRunOtherServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan string, 4)
	release := make(chan struct{})
	w := newTaskWorkers(ctx, func(ctx context.Context, task types.AgentTask) *types.TaskResult {
		started <- task.ID
		if task.ID == "first" {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return &types.TaskResult{TaskID: task.ID, OK: true}
	})
	defer func() { cancel(); w.wg.Wait() }()
	w.enqueue(types.AgentTask{ID: "first", ServiceID: "same"})
	w.enqueue(types.AgentTask{ID: "second", ServiceID: "same"})
	w.enqueue(types.AgentTask{ID: "other", ServiceID: "different"})
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("unrelated service blocked behind long task")
		}
	}
	if !seen["first"] || !seen["other"] || seen["second"] {
		t.Fatalf("service ordering failed: %v", seen)
	}
	close(release)
	select {
	case id := <-started:
		if id != "second" {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("same-service queue stalled")
	}
	// Completed but unacknowledged IDs remain deduplicated.
	if w.slots() != taskQueueCapacity-3 {
		t.Fatal("in-flight/result capacity not reserved")
	}
	w.enqueue(types.AgentTask{ID: "first", ServiceID: "same"})
	if w.slots() != taskQueueCapacity-3 {
		t.Fatal("duplicate consumed a slot")
	}
}

func TestWorkerCapacityAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := newTaskWorkers(ctx, func(ctx context.Context, task types.AgentTask) *types.TaskResult { <-ctx.Done(); return nil })
	defer func() { cancel(); w.wg.Wait() }()
	for i := 0; i < taskQueueCapacity; i++ {
		if !w.enqueue(types.AgentTask{ID: string(rune(100 + i)), ServiceID: "app"}) {
			t.Fatal("early rejection")
		}
	}
	if w.slots() != 0 || w.enqueue(types.AgentTask{ID: "overflow"}) {
		t.Fatal("capacity exceeded")
	}
	cancel()
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not stop")
	}
}
