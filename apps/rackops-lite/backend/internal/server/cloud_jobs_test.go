package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
	"golang.org/x/crypto/ssh"
	"testing"
)

type fakeCloudProvider struct {
	name, region, account string
	starts, lookups       int
	missing               bool
}

func (p *fakeCloudProvider) Name() string   { return p.name }
func (p *fakeCloudProvider) Region() string { return p.region }
func (p *fakeCloudProvider) Validate(context.Context) (cloud.Identity, error) {
	return cloud.Identity{AccountID: p.account}, nil
}
func (p *fakeCloudProvider) Start(context.Context, cloud.MachineSpec) (cloud.Receipt, error) {
	p.starts++
	return cloud.Receipt{OperationID: "provider-operation"}, errors.New("lost response")
}
func (p *fakeCloudProvider) Reconcile(context.Context, cloud.MachineSpec, cloud.Receipt) (cloud.Receipt, error) {
	p.lookups++
	if p.missing {
		return cloud.Receipt{OperationID: "provider-operation"}, nil
	}
	return cloud.Receipt{OperationID: "provider-operation", ResourceID: "resource-fixture", State: "running"}, nil
}

func cloudFixture(t *testing.T, name string) cloud.MachineSpec {
	t.Helper()
	const op = "11111111-1111-4111-8111-111111111111"
	switch name {
	case "aws":
		return cloud.MachineSpec{AWS: &cloud.AWSMachineSpec{OperationID: op, ImageID: "ami-fixture", InstanceType: "t3.small", SubnetID: "subnet-fixture", SecurityGroupID: "sg-fixture", RootDevice: "/dev/sda1", DiskGB: 20}}
	case "azure":
		key, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public())
		if err != nil {
			t.Fatal(err)
		}
		prefix := "/subscriptions/22222222-2222-4222-8222-222222222222/resourceGroups/fixture/providers/"
		return cloud.MachineSpec{Azure: &cloud.AzureMachineSpec{OperationID: op, SubscriptionID: "22222222-2222-4222-8222-222222222222", ResourceGroup: "fixture", Size: "Standard_B2s", ImageID: prefix + "Microsoft.Compute/images/fixture", NetworkInterfaceID: prefix + "Microsoft.Network/networkInterfaces/fixture", SSHPublicKey: string(ssh.MarshalAuthorizedKey(key)), DiskGB: 30}}
	default:
		return cloud.MachineSpec{GCP: &cloud.GCPMachineSpec{OperationID: op, ProjectID: "fixture-project", Zone: "us-central1-a", MachineType: "e2-small", Image: "projects/ubuntu-os-cloud/global/images/fixture-image", Subnetwork: "projects/fixture-project/regions/us-central1/subnetworks/fixture", DiskGB: 20}}
	}
}

func TestCloudJobsReconcileAfterRestartAcrossProviders(t *testing.T) {
	for _, name := range []string{"aws", "azure", "gcp"} {
		t.Run(name, func(t *testing.T) {
			a, _ := testAPI(t, types.RoleOwner)
			org := a.Store.DefaultOrgID()
			spec := cloudFixture(t, name)
			a.Store.Mutate(func() {
				a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, Provider: name, Region: "region", AccountID: "account"}}
			})
			job := CloudProvisioning{ID: spec.ID(), OrgID: org, ConnectionID: "connection", Provider: name, Region: "region", AccountID: "account", Spec: spec}
			if err := a.saveCloudProvisioning(job); err != nil {
				t.Fatal(err)
			}
			expected := spec.Fingerprint()
			switch name {
			case "aws":
				spec.AWS.DiskGB++
			case "azure":
				spec.Azure.DiskGB++
			case "gcp":
				spec.GCP.DiskGB++
			}
			if a.Store.CloudProvisioning[job.ID].Spec.Fingerprint() != expected {
				t.Fatal("caller changed persisted plan")
			}
			if err := a.saveCloudProvisioning(job); err == nil {
				t.Fatal("changed plan reused operation ID")
			}
			p := &fakeCloudProvider{name: name, region: "region", account: "account", missing: true}
			if err := a.runCloudProvisioning(context.Background(), org, job.ID, p); err == nil {
				t.Fatal("lost response accepted")
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
			if err := a.runCloudProvisioning(context.Background(), org, job.ID, p); err == nil {
				t.Fatal("empty reconciliation claimed success")
			}
			p.missing = false
			if err := a.runCloudProvisioning(context.Background(), org, job.ID, p); err != nil {
				t.Fatal(err)
			}
			if p.starts != 1 || p.lookups != 2 {
				t.Fatal("operation relaunched after restart")
			}
			if a.Store.CloudProvisioning[job.ID].Receipt.ResourceID != "resource-fixture" {
				t.Fatal("resource not retained")
			}
			if err := a.runCloudProvisioning(context.Background(), "other-org", job.ID, p); err == nil {
				t.Fatal("cross-org operation accepted")
			}
			raw, _ := json.Marshal(a.Store.CloudProvisioning[job.ID])
			if !json.Valid(raw) {
				t.Fatal("invalid durable record")
			}
		})
	}
}
