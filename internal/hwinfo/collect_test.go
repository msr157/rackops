package hwinfo

import (
	"context"
	"testing"
	"time"
)

func TestCollect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	snap, err := Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if snap.Hostname == "" {
		t.Error("hostname is empty")
	}
	if snap.OS == "" {
		t.Error("OS is empty")
	}
	if snap.Memory.TotalBytes == 0 {
		t.Error("total memory is zero")
	}
	if len(snap.Disks) == 0 {
		t.Error("no disks discovered")
	}
	if snap.CollectedAt.IsZero() {
		t.Error("CollectedAt not set")
	}
}
