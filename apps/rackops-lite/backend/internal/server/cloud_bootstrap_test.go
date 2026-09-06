package server

import (
	"bytes"
	"encoding/json"
	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
	"strings"
	"testing"
)

func TestCloudBootstrapIsEncryptedImmutableAndRestorable(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	spec := cloudFixture(t, "aws")
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: "aws", Region: "us-east-1", AccountID: "account"}}
	})
	job := CloudProvisioning{ID: spec.ID(), OrgID: org, ConnectionID: "connection", Provider: "aws", Region: "us-east-1", AccountID: "account", Spec: spec}
	if err := a.saveCloudProvisioning(job); err != nil {
		t.Fatal(err)
	}
	config := cloud.BootstrapConfig{ServerURL: "https://control.example.test", OrgID: org, Architecture: "amd64", BinaryURL: "https://github.com/msr157/rackops/releases/download/v0.1.0/rackops-linux-amd64", SHA256: strings.Repeat("a", 64)}
	if err := a.prepareCloudBootstrap(org, job.ID, config); err != nil {
		t.Fatal(err)
	}
	saved := a.Store.CloudProvisioning[job.ID]
	cipher := append([]byte(nil), saved.SealedBootstrap...)
	grant := saved.EnrollmentHash
	plain, err := decryptState(a.Store.stateKey, cipher)
	if err != nil || !bytes.Contains(plain, []byte("rkenroll_")) {
		t.Fatal("bootstrap not recoverable")
	}
	raw, _ := json.Marshal(a.Store)
	if bytes.Contains(raw, []byte("rkenroll_")) || bytes.Contains(raw, []byte("#!/bin/sh")) {
		t.Fatal("startup secret leaked in state export")
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewStore(a.Store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a.Store = restored
	if err := a.prepareCloudBootstrap(org, job.ID, config); err != nil {
		t.Fatal(err)
	}
	saved = a.Store.CloudProvisioning[job.ID]
	if !bytes.Equal(saved.SealedBootstrap, cipher) || saved.EnrollmentHash != grant || len(a.Store.NodeEnrollments) != 1 {
		t.Fatal("restart changed bootstrap or enrollment identity")
	}
	config.SHA256 = strings.Repeat("b", 64)
	if err := a.prepareCloudBootstrap(org, job.ID, config); err == nil {
		t.Fatal("saved bootstrap changed without a new plan")
	}
	if err := a.prepareCloudBootstrap("other-org", job.ID, config); err == nil {
		t.Fatal("cross-org bootstrap mutation")
	}
}
