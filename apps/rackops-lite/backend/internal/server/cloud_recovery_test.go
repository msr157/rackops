package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
)

func recoveryFixture(t *testing.T, name string) (*API, *fakeCloudProvider, string) {
	t.Helper()
	a, _ := testAPI(t, types.RoleOwner)
	spec := cloudFixture(t, name)
	sealed, err := encryptState(a.Store.stateKey, []byte(`{"fixture":true}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: "org", Provider: name, Region: "region", AccountID: "account", SealedCredentials: sealed}}
		a.Store.CloudProvisioning = map[string]*CloudProvisioning{spec.ID(): {ID: spec.ID(), OrgID: "org", ConnectionID: "connection", Provider: name, Region: "region", AccountID: "account", Spec: spec, State: "submitting", CreatedAt: time.Now().UTC()}}
	})
	provider := &fakeCloudProvider{name: name, region: "region", account: "account", missing: true}
	a.cloudRecoveryProvider = func(ctx context.Context, name, region string, raw json.RawMessage) (cloud.Provider, error) {
		if string(raw) != `{"fixture":true}` {
			t.Error("credentials not recovered")
		}
		return provider, nil
	}
	return a, provider, spec.ID()
}

func TestCloudRecoveryResumesWithoutLaunching(t *testing.T) {
	for _, name := range []string{"aws", "azure", "gcp"} {
		t.Run(name, func(t *testing.T) {
			a, p, id := recoveryFixture(t, name)
			now := time.Now().UTC()
			a.recoverCloudJobs(context.Background(), now)
			job := a.Store.CloudProvisioning[id]
			if p.starts != 0 || p.lookups != 1 || job.RecoveryAttempts != 1 || job.RecoveryState != "waiting" {
				t.Fatal("invalid recovery attempt")
			}
			a.recoverCloudJobs(context.Background(), now.Add(time.Second))
			if p.lookups != 1 {
				t.Fatal("retry backoff ignored")
			}
			a.Store.Close()
			restored, err := NewStore(a.Store.dataDir)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			a.Store = restored
			a.recoverCloudJobs(context.Background(), now.Add(time.Second))
			if p.lookups != 1 {
				t.Fatal("restart reset backoff")
			}
			p.missing = false
			a.recoverCloudJobs(context.Background(), now.Add(time.Minute))
			job = restored.CloudProvisioning[id]
			if job.RecoveryState != "provider_ready" || p.starts != 0 || p.lookups != 2 || job.Receipt.ResourceID == "" {
				t.Fatal("recovery failed to retain resource")
			}
			a.recoverCloudJobs(context.Background(), now.Add(time.Hour))
			if p.lookups != 2 {
				t.Fatal("completed provider recovery kept polling")
			}
		})
	}
}

func TestCloudRecoverySkipsPlansAndStopsAtLimits(t *testing.T) {
	for _, mode := range []string{"planned", "budget", "deadline", "connection", "account", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, p, id := recoveryFixture(t, "aws")
			now := time.Now().UTC()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.Store.Mutate(func() {
				job := a.Store.CloudProvisioning[id]
				switch mode {
				case "planned":
					job.State = "planned"
				case "budget":
					job.RecoveryAttempts = 36
				case "deadline":
					job.CreatedAt = now.Add(-25 * time.Hour)
				case "connection":
					a.Store.CloudConnections["connection"].OrgID = "other"
				case "account":
					p.account = "other"
				case "cancel":
					cancel()
				}
			})
			a.recoverCloudJobs(ctx, now)
			if p.starts != 0 || p.lookups != 0 {
				t.Fatal("unsafe provider call")
			}
			if mode == "budget" || mode == "deadline" || mode == "connection" {
				if a.Store.CloudProvisioning[id].RecoveryState != "needs_attention" {
					t.Fatal("missing actionable terminal state")
				}
			}
		})
	}
}

type canceledRecoveryProvider struct {
	*fakeCloudProvider
	entered chan struct{}
}

func (p *canceledRecoveryProvider) Reconcile(ctx context.Context, _ cloud.MachineSpec, _ cloud.Receipt) (cloud.Receipt, error) {
	close(p.entered)
	<-ctx.Done()
	return cloud.Receipt{}, ctx.Err()
}

func TestCloudRecoveryStopsWithStore(t *testing.T) {
	a, p, _ := recoveryFixture(t, "aws")
	blocked := &canceledRecoveryProvider{fakeCloudProvider: p, entered: make(chan struct{})}
	a.cloudRecoveryProvider = func(context.Context, string, string, json.RawMessage) (cloud.Provider, error) { return blocked, nil }
	done := make(chan struct{})
	go func() { a.RunCloudRecovery(); close(done) }()
	select {
	case <-blocked.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel provider access")
	}
	if p.starts != 0 {
		t.Fatal("shutdown recovery launched a resource")
	}
}
