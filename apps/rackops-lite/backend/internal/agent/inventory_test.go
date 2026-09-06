package agent

import (
	"context"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func TestInventoryReadDoesNotWaitForRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, done := make(chan struct{}), make(chan struct{})
	var inventory inventory
	go func() {
		defer close(done)
		inventory.run(ctx, time.Second, func(ctx context.Context) ([]types.ContainerStatus, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}()
	<-started
	read := make(chan bool, 1)
	go func() { _, valid := inventory.take(); read <- valid }()
	select {
	case valid := <-read:
		if valid {
			t.Fatal("pending inventory was authoritative")
		}
	case <-time.After(time.Second):
		t.Fatal("runtime blocked inventory read")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("inventory did not stop")
	}
}

func TestInventorySamplesConsumedOnceAndExpire(t *testing.T) {
	i := inventory{containers: []types.ContainerStatus{{ID: "one"}}, fresh: true, finished: time.Now()}
	containers, valid := i.take()
	if !valid || len(containers) != 1 {
		t.Fatal("fresh sample unavailable")
	}
	containers[0].ID = "modified"
	if cached, valid := i.take(); valid || cached[0].ID != "one" {
		t.Fatal("stale sample reused or cache mutated")
	}
	i.fresh = true
	i.finished = time.Now().Add(-time.Minute)
	if _, valid := i.take(); valid {
		t.Fatal("expired sample authoritative")
	}
}
