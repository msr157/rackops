package server

import (
	"context"
	"errors"
	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
	"testing"
)

type fakeAWSProvisioner struct {
	calls       int
	lookupCalls int
	fail        bool
	region      string
}

func (p *fakeAWSProvisioner) Region() string { return p.region }
func (p *fakeAWSProvisioner) Validate(context.Context) (cloud.Identity, error) {
	return cloud.Identity{AccountID: "account-fixture"}, nil
}
func (p *fakeAWSProvisioner) Provision(context.Context, cloud.AWSMachineSpec) (string, error) {
	p.calls++
	if p.fail {
		return "", errors.New("lost response")
	}
	return "i-fixture", nil
}
func (p *fakeAWSProvisioner) Lookup(context.Context, string) (string, string, error) {
	p.lookupCalls++
	return "i-fixture", "pending", nil
}

func TestAWSProvisioningRestartReconcilesInsteadOfRelaunching(t *testing.T) {
	a, _ := testAPI(t, types.RoleOwner)
	org := a.Store.DefaultOrgID()
	a.Store.Mutate(func() {
		a.Store.CloudConnections = map[string]*CloudConnection{"connection": {ID: "connection", OrgID: org, AccountID: "account-fixture", Region: "us-east-1", Provider: "aws"}}
	})
	job := AWSProvisioning{ID: "operation-fixture", OrgID: org, ConnectionID: "connection", AccountID: "account-fixture", Region: "us-east-1", Spec: cloud.AWSMachineSpec{OperationID: "operation-fixture", ImageID: "ami-fixture", InstanceType: "t3.small", SubnetID: "subnet-fixture", SecurityGroupID: "sg-fixture", RootDevice: "/dev/sda1", DiskGB: 20}}
	if err := a.saveAWSProvisioning(job); err != nil {
		t.Fatal(err)
	}
	changed := job
	changed.Spec.ImageID = "different-image"
	if err := a.saveAWSProvisioning(changed); err == nil {
		t.Fatal("immutable operation changed")
	}
	provider := &fakeAWSProvisioner{fail: true, region: "us-east-1"}
	if err := a.runAWSProvisioning(context.Background(), org, job.ID, provider); err == nil {
		t.Fatal("expected uncertain result")
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
	provider.region = "eu-west-1"
	if err := a.runAWSProvisioning(context.Background(), org, job.ID, provider); err == nil {
		t.Fatal("operation replayed in another region")
	}
	provider.region = "us-east-1"
	if err := a.runAWSProvisioning(context.Background(), org, job.ID, provider); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || provider.lookupCalls != 1 {
		t.Fatal("restart duplicated launch")
	}
	if got := a.Store.AWSProvisioning[job.ID]; got.ResourceID != "i-fixture" || got.State != "created" {
		t.Fatal("resource identity not saved")
	}
	if err := a.runAWSProvisioning(context.Background(), "other-org", job.ID, provider); err == nil {
		t.Fatal("cross-org operation allowed")
	}
}
