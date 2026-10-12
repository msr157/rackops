package server

import (
	"context"
	"testing"
	"time"
)

func TestCloudRecoveryControlsRetainResourcesAndRetryBudget(t *testing.T) {
	for _, name := range []string{"aws", "azure", "gcp"} {
		t.Run(name, func(t *testing.T) {
			a, p, id := recoveryFixture(t, name)
			org := a.Store.DefaultOrgID()
			a.Store.Mutate(func() { a.Store.CloudProvisioning[id].OrgID = org; a.Store.CloudConnections["connection"].OrgID = org })
			headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
			path := "/api/cloud-operations/" + id + "/recovery"
			if r := do(a, "POST", path, `{"action":"pause"}`, headers); r.Code != 200 {
				t.Fatal("pause failed", r.Code)
			}
			a.recoverCloudJobs(context.Background(), time.Now())
			if p.lookups != 0 || p.starts != 0 {
				t.Fatal("paused job executed")
			}
			job := a.Store.CloudProvisioning[id]
			persisted, err := NewStore(a.Store.dataDir)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.CloudProvisioning[id].RecoveryState != "paused" {
				t.Fatal("pause not persisted")
			}
			persisted.Close()
			a.Store.Mutate(func() { job.RecoveryAttempts = 36; job.CreatedAt = time.Now().Add(-48 * time.Hour) })
			if r := do(a, "POST", path, `{"action":"resume"}`, headers); r.Code != 200 {
				t.Fatal("resume failed", r.Code)
			}
			if job.RecoveryPreviousAttempts != 36 || job.RecoveryAttempts != 0 {
				t.Fatal("lost attempt history")
			}
			a.recoverCloudJobs(context.Background(), time.Now().Add(time.Second))
			if p.lookups != 1 || p.starts != 0 {
				t.Fatal("resume did not reconcile existing operation")
			}
			if r := do(a, "POST", path, `{"action":"resume"}`, headers); r.Code != 200 || job.RecoveryAttempts != 1 {
				t.Fatal("retry reset active budget")
			}
			a.Store.Mutate(func() { job.OrgID = "other" })
			if r := do(a, "POST", path, `{"action":"pause"}`, headers); r.Code != 404 {
				t.Fatal("cross-org recovery allowed")
			}
			if r := do(a, "GET", "/api/cloud-operations", "", headers); r.Code != 200 || r.Body.String() != "[]\n" {
				t.Fatal("cross-org list leaked", r.Body.String())
			}
		})
	}
}
