package migration

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryJournal struct {
	saved []Job
	err   error
}

func (m *memoryJournal) Save(_ context.Context, j Job) error {
	if m.err != nil {
		return m.err
	}
	m.saved = append(m.saved, j)
	return nil
}

type mockExecutor struct {
	executeReceipt   string
	executeDone      bool
	executeErr       error
	reconcileReceipt string
	reconcileDone    bool
	reconcileErr     error
	executedStages   []Stage
	reconciledStages []Stage
}

func (m *mockExecutor) Execute(_ context.Context, stage Stage, opID string) (string, bool, error) {
	m.executedStages = append(m.executedStages, stage)
	return m.executeReceipt, m.executeDone, m.executeErr
}

func (m *mockExecutor) Reconcile(_ context.Context, stage Stage, opID string) (string, bool, error) {
	m.reconciledStages = append(m.reconciledStages, stage)
	return m.reconcileReceipt, m.reconcileDone, m.reconcileErr
}

func TestAdvance_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	journal := &memoryJournal{}
	exec := &mockExecutor{}

	// Incomplete identity
	_, err := Advance(ctx, journal, exec, Job{})
	if err == nil || !strings.Contains(err.Error(), "incomplete migration identity") {
		t.Fatalf("expected incomplete identity error, got %v", err)
	}

	// Unknown stage
	invalidStageJob := Job{
		ID:     "job-1",
		OrgID:  "org-1",
		PlanID: "plan-1",
		Stage:  Stage("unknown-stage"),
		Status: "ready",
	}
	_, err = Advance(ctx, journal, exec, invalidStageJob)
	if err == nil || !strings.Contains(err.Error(), "unknown migration stage") {
		t.Fatalf("expected unknown stage error, got %v", err)
	}

	// Missing prerequisite receipt
	missingPrereqJob := Job{
		ID:       "job-1",
		OrgID:    "org-1",
		PlanID:   "plan-1",
		Stage:    Backup,
		Status:   "ready",
		Receipts: map[Stage]string{Provision: "receipt-prov"}, // Quiesce is missing!
	}
	_, err = Advance(ctx, journal, exec, missingPrereqJob)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing prerequisite error, got %v", err)
	}

	// Paused or non-runnable status
	pausedJob := Job{
		ID:     "job-1",
		OrgID:  "org-1",
		PlanID: "plan-1",
		Stage:  Provision,
		Status: "paused",
	}
	_, err = Advance(ctx, journal, exec, pausedJob)
	if err == nil || !strings.Contains(err.Error(), "requires operator recovery") {
		t.Fatalf("expected paused error, got %v", err)
	}
}

func TestAdvance_HappyPathTransitions(t *testing.T) {
	ctx := context.Background()
	journal := &memoryJournal{}

	job := Job{
		ID:       "job-1",
		OrgID:    "org-1",
		PlanID:   "plan-1",
		Stage:    Provision,
		Status:   "ready",
		Receipts: make(map[Stage]string),
	}

	for _, stage := range stages[:len(stages)-1] {
		exec := &mockExecutor{
			executeReceipt: "receipt-" + string(stage),
			executeDone:    true,
		}

		nextJob, err := Advance(ctx, journal, exec, job)
		if err != nil {
			t.Fatalf("stage %s failed: %v", stage, err)
		}
		if nextJob.Receipts[stage] != "receipt-"+string(stage) {
			t.Fatalf("missing receipt for stage %s", stage)
		}
		job = nextJob
	}

	if job.Stage != Complete {
		t.Fatalf("expected job to reach Complete, got %s", job.Stage)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed status, got %s", job.Status)
	}

	// Advancing an already Complete job is a no-op returning nil
	completeJob, err := Advance(ctx, journal, &mockExecutor{}, job)
	if err != nil || completeJob.Stage != Complete {
		t.Fatalf("expected no-op on complete job, got %v", err)
	}
}

func TestAdvance_AsynchronousAndReconciliation(t *testing.T) {
	ctx := context.Background()
	journal := &memoryJournal{}

	job := Job{
		ID:       "job-async",
		OrgID:    "org-1",
		PlanID:   "plan-1",
		Stage:    Provision,
		Status:   "ready",
		Receipts: make(map[Stage]string),
	}

	// 1. Initial execution starts asynchronous operation (done = false)
	exec := &mockExecutor{
		executeReceipt: "",
		executeDone:    false,
	}
	runningJob, err := Advance(ctx, journal, exec, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runningJob.Status != "running" {
		t.Fatalf("expected status running, got %s", runningJob.Status)
	}

	// 2. Reconcile while still pending (done = false)
	execReconcilePending := &mockExecutor{
		reconcileDone: false,
	}
	pendingJob, err := Advance(ctx, journal, execReconcilePending, runningJob)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pendingJob.Status != "running" {
		t.Fatalf("expected status to remain running, got %s", pendingJob.Status)
	}

	// 3. Reconcile completes (done = true)
	execReconcileDone := &mockExecutor{
		reconcileReceipt: "receipt-provision-async",
		reconcileDone:    true,
	}
	doneJob, err := Advance(ctx, journal, execReconcileDone, pendingJob)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doneJob.Stage != Quiesce {
		t.Fatalf("expected stage to advance to Quiesce, got %s", doneJob.Stage)
	}
	if doneJob.Receipts[Provision] != "receipt-provision-async" {
		t.Fatalf("expected receipt to be recorded")
	}
}

func TestAdvance_ExecutionErrorCausesUncertainState(t *testing.T) {
	ctx := context.Background()
	journal := &memoryJournal{}
	job := Job{
		ID:       "job-fail",
		OrgID:    "org-1",
		PlanID:   "plan-1",
		Stage:    Backup,
		Status:   "ready",
		Receipts: map[Stage]string{Provision: "r-prov", Quiesce: "r-quiesce"},
	}

	exec := &mockExecutor{
		executeErr: errors.New("network timeout during backup"),
	}

	uncertainJob, err := Advance(ctx, journal, exec, job)
	if err == nil || !strings.Contains(err.Error(), "uncertain") {
		t.Fatalf("expected uncertain error, got %v", err)
	}
	if uncertainJob.Status != "uncertain" {
		t.Fatalf("expected job status uncertain, got %s", uncertainJob.Status)
	}
}

func TestRequestRecovery(t *testing.T) {
	ctx := context.Background()
	journal := &memoryJournal{}

	// Cannot request recovery while operation is running or uncertain
	runningJob := Job{
		ID:     "job-rec",
		OrgID:  "org-1",
		PlanID: "plan-1",
		Stage:  Backup,
		Status: "running",
	}
	_, err := RequestRecovery(ctx, journal, runningJob)
	if err == nil || !strings.Contains(err.Error(), "reconcile") {
		t.Fatalf("expected error requesting recovery while running, got %v", err)
	}

	// When writes are not possible at destination: source recovery requested
	readyJob := Job{
		ID:                        "job-rec",
		OrgID:                     "org-1",
		PlanID:                    "plan-1",
		Stage:                     Backup,
		Status:                    "recovery_required",
		DestinationWritesPossible: false,
	}
	recJob, err := RequestRecovery(ctx, journal, readyJob)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recJob.Status != "source_recovery_requested" {
		t.Fatalf("expected source_recovery_requested, got %s", recJob.Status)
	}

	// When destination writes were possible: data reconciliation required
	writesJob := Job{
		ID:                        "job-rec-2",
		OrgID:                     "org-1",
		PlanID:                    "plan-1",
		Stage:                     EnableWrites,
		Status:                    "recovery_required",
		DestinationWritesPossible: true,
	}
	reconJob, err := RequestRecovery(ctx, journal, writesJob)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reconJob.Status != "data_reconciliation_required" {
		t.Fatalf("expected data_reconciliation_required, got %s", reconJob.Status)
	}
}

func TestArtifact_EncryptionAndDecryption(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x42}, 32)
	payload := []byte("hello rackops zero loss migration payload with AES-GCM encryption verification")

	var encrypted bytes.Buffer
	digest, err := EncryptArtifact(ctx, &encrypted, bytes.NewReader(payload), key)
	if err != nil {
		t.Fatalf("EncryptArtifact failed: %v", err)
	}
	if digest.Bytes != int64(len(payload)) {
		t.Fatalf("expected %d bytes in digest, got %d", len(payload), digest.Bytes)
	}

	var decrypted bytes.Buffer
	decDigest, err := DecryptArtifact(ctx, &decrypted, bytes.NewReader(encrypted.Bytes()), key, 1024*1024)
	if err != nil {
		t.Fatalf("DecryptArtifact failed: %v", err)
	}
	if !bytes.Equal(decrypted.Bytes(), payload) {
		t.Fatalf("decrypted payload does not match original: got %q, want %q", decrypted.String(), payload)
	}
	if decDigest.SHA256 != digest.SHA256 {
		t.Fatalf("digest SHA256 mismatch: got %s, want %s", decDigest.SHA256, digest.SHA256)
	}

	// Invalid key length check
	shortKey := []byte("short")
	_, err = EncryptArtifact(ctx, &encrypted, bytes.NewReader(payload), shortKey)
	if err == nil {
		t.Fatalf("expected error for invalid key length")
	}

	// Corrupted payload check
	corrupted := append([]byte(nil), encrypted.Bytes()...)
	if len(corrupted) > 20 {
		corrupted[20] ^= 0xFF
	}
	var corruptOut bytes.Buffer
	_, err = DecryptArtifact(ctx, &corruptOut, bytes.NewReader(corrupted), key, 1024*1024)
	if err == nil {
		t.Fatalf("expected decryption failure for corrupted stream")
	}
}
