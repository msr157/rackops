package raftstore

import (
	"bytes"
	"io"
	"testing"

	"github.com/hashicorp/raft"
)

type dummySink struct {
	buf bytes.Buffer
}

func (d *dummySink) Write(p []byte) (n int, err error) {
	return d.buf.Write(p)
}

func (d *dummySink) Close() error {
	return nil
}

func (d *dummySink) ID() string {
	return "dummy-sink-1"
}

func (d *dummySink) Cancel() error {
	return nil
}

func TestFSM_Apply(t *testing.T) {
	applied := false
	var appliedData []byte

	f := &fsm{
		apply: func(data []byte) {
			applied = true
			appliedData = data
		},
	}

	testPayload := []byte(`{"version":1,"nodes":["node-1"]}`)
	logEntry := &raft.Log{
		Data: testPayload,
	}

	res := f.Apply(logEntry)
	if res != nil {
		t.Errorf("expected nil return from Apply, got %v", res)
	}
	if !applied {
		t.Error("expected apply callback to be called")
	}
	if !bytes.Equal(f.state, testPayload) {
		t.Errorf("fsm state mismatch: got %s, want %s", string(f.state), string(testPayload))
	}
	if !bytes.Equal(appliedData, testPayload) {
		t.Errorf("applied callback data mismatch: got %s, want %s", string(appliedData), string(testPayload))
	}
}

func TestFSM_SnapshotAndRestore(t *testing.T) {
	f := &fsm{
		state: []byte(`{"cluster":"test-cluster"}`),
	}

	snap, err := f.Snapshot()
	if err != nil {
		t.Fatalf("failed to take snapshot: %v", err)
	}

	sink := &dummySink{}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("failed to persist snapshot: %v", err)
	}

	if sink.buf.String() != `{"cluster":"test-cluster"}` {
		t.Errorf("persisted snapshot mismatch: got %s", sink.buf.String())
	}

	// Now test restore on a new FSM
	restoredCalled := false
	newF := &fsm{
		apply: func(d []byte) {
			restoredCalled = true
		},
	}

	rc := io.NopCloser(bytes.NewReader(sink.buf.Bytes()))
	if err := newF.Restore(rc); err != nil {
		t.Fatalf("failed to restore from snapshot: %v", err)
	}

	if !restoredCalled {
		t.Error("expected apply callback on restore")
	}
	if !bytes.Equal(newF.state, f.state) {
		t.Errorf("restored state mismatch: got %s, want %s", string(newF.state), string(f.state))
	}
}

func TestStateJSON(t *testing.T) {
	type Sample struct {
		Key string `json:"key"`
		Num int    `json:"num"`
	}

	raw := []byte(`{"key":"abc","num":42}`)
	var s Sample
	if err := StateJSON(raw, &s); err != nil {
		t.Fatalf("StateJSON failed: %v", err)
	}

	if s.Key != "abc" || s.Num != 42 {
		t.Errorf("StateJSON unmarshaled unexpected values: %+v", s)
	}
}
