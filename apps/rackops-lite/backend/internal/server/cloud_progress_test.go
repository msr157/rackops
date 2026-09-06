package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
)

func TestCloudProgressSeparatesProviderAndAgentEvidence(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	now := time.Now().UTC()
	job := &CloudProvisioning{ID: "job", OrgID: org, Provider: "aws", State: "submitted", Receipt: cloud.Receipt{ResourceID: "instance", State: "running"},
		BootstrapConfig: &cloud.BootstrapConfig{Architecture: "amd64"}, EnrollmentHash: "private-hash", SealedBootstrap: []byte("private-payload")}
	grant := &NodeEnrollment{OrgID: org, ExpiresAt: now.Add(time.Hour)}
	a.Store.Mutate(func() {
		a.Store.CloudProvisioning = map[string]*CloudProvisioning{job.ID: job}
		a.Store.NodeEnrollments = map[string]*NodeEnrollment{job.EnrollmentHash: grant}
	})
	check := func(want string) {
		t.Helper()
		a.Store.View(func() {
			got := a.cloudProgressLocked(job, now)
			if got.AgentStage != want || got.CanMigrate {
				t.Fatalf("progress: %+v; want %s", got, want)
			}
		})
	}
	check("awaiting_enrollment")
	a.Store.Mutate(func() { grant.ExpiresAt = now.Add(-time.Minute) })
	check("enrollment_expired")
	a.Store.Mutate(func() {
		grant.NodeID = "cloud-node"
		grant.ExpiresAt = now.Add(time.Hour)
		a.Store.Nodes[grant.NodeID] = &types.Node{ID: grant.NodeID, OrgID: org, Status: types.NodeJoining}
		a.Store.NodeCredentials = map[string]*NodeCredential{grant.NodeID: {OrgID: org, Hash: "credential-hash"}}
	})
	check("awaiting_heartbeat")
	a.Store.Mutate(func() { grant.ExpiresAt = now.Add(-time.Minute) })
	check("heartbeat_timed_out")
	a.Store.Mutate(func() {
		n := a.Store.Nodes[grant.NodeID]
		n.LastSeen, n.Status, n.OS, n.Arch = now, types.NodeOnline, "linux", "arm64"
	})
	check("agent_incompatible")
	a.Store.Mutate(func() { a.Store.Nodes[grant.NodeID].Arch = "amd64" })
	// Expiry does not revoke the already-enrolled node identity.
	check("agent_connected")
	headers := map[string]string{"Authorization": "Bearer " + loginToken(t, a, "owner@test.local")}
	r := do(a, "GET", "/api/cloud-operations/job", "", headers)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("progress response: %d %s", r.Code, r.Body)
	}
	for _, secret := range []string{"private-hash", "private-payload", "credential-hash", "sealedBootstrap", "bootstrapConfig"} {
		if strings.Contains(r.Body.String(), secret) {
			t.Fatal("private bootstrap evidence leaked")
		}
	}
	var response cloudProgress
	if json.Unmarshal(r.Body.Bytes(), &response) != nil || response.AgentStage != "agent_connected" {
		t.Fatal("invalid progress response")
	}
	a.Store.Mutate(func() { a.Store.Nodes[grant.NodeID].LastSeen = now.Add(-2 * time.Minute) })
	check("agent_offline")
	a.Store.Mutate(func() { a.Store.Nodes[grant.NodeID].OrgID = "another-org" })
	check("agent_identity_unavailable")
	a.Store.Mutate(func() { job.OrgID = "another-org" })
	if r := do(a, "GET", "/api/cloud-operations/job", "", headers); r.Code != 404 {
		t.Fatal("cross-org operation exposed")
	}
	if r := do(a, "GET", "/api/cloud-operations/job", "", nil); r.Code == 200 {
		t.Fatal("unauthenticated operation exposed")
	}
}

func TestCloudLaunchRejectsInvalidEnrollmentBeforeProviderCall(t *testing.T) {
	for _, mode := range []string{"expired", "consumed", "missing", "wrong-org"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			org := a.Store.DefaultOrgID()
			spec := cloudFixture(t, "aws")
			grant := &NodeEnrollment{OrgID: org, ExpiresAt: time.Now().Add(time.Hour)}
			switch mode {
			case "expired":
				grant.ExpiresAt = time.Now().Add(-time.Minute)
			case "consumed":
				grant.NodeID = "already-enrolled"
			case "wrong-org":
				grant.OrgID = "another-org"
			}
			a.Store.Mutate(func() {
				a.Store.CloudProvisioning = map[string]*CloudProvisioning{spec.ID(): {ID: spec.ID(), OrgID: org, Provider: "aws", Region: "region", AccountID: "account", Spec: spec, State: "planned", CreatedAt: time.Now(), EnrollmentHash: "hash"}}
				a.Store.NodeEnrollments = map[string]*NodeEnrollment{}
				if mode != "missing" {
					a.Store.NodeEnrollments["hash"] = grant
				}
			})
			p := &fakeCloudProvider{name: "aws", region: "region", account: "account"}
			if err := a.runCloudProvisioning(context.Background(), org, spec.ID(), p); err == nil || p.starts != 0 || p.lookups != 0 {
				t.Fatal("invalid enrollment reached launch")
			}
			a.Store.Mutate(func() { a.Store.CloudProvisioning[spec.ID()].State = "uncertain" })
			if err := a.runCloudProvisioning(context.Background(), org, spec.ID(), p); err != nil || p.lookups != 1 || p.starts != 0 {
				t.Fatal("expired enrollment prevented resource recovery")
			}
		})
	}
}
