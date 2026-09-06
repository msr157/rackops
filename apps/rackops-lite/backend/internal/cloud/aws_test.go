package cloud

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"testing"
)

type fakeEC2 struct {
	requests  []*ec2.RunInstancesInput
	err       error
	instances []ec2types.Instance
}

func (f *fakeEC2) RunInstances(_ context.Context, r *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	f.requests = append(f.requests, r)
	return &ec2.RunInstancesOutput{Instances: f.instances}, f.err
}
func (f *fakeEC2) DescribeInstances(_ context.Context, r *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: f.instances}}}, f.err
}
func fixtureSpec() AWSMachineSpec {
	return AWSMachineSpec{OperationID: "op-fixture-123", ImageID: "ami-fixture1", InstanceType: "t3.small", SubnetID: "subnet-fixture1", SecurityGroupID: "sg-fixture1", RootDevice: "/dev/sda1", DiskGB: 20}
}

func TestAWSLaunchUsesStableTokenAndConservativeDefaults(t *testing.T) {
	f := &fakeEC2{instances: []ec2types.Instance{{InstanceId: aws.String("i-fixture1")}}}
	p := &AWS{compute: f}
	spec := fixtureSpec()
	for i := 0; i < 2; i++ {
		id, err := p.Provision(context.Background(), spec)
		if err != nil || id != "i-fixture1" {
			t.Fatal("launch failed")
		}
	}
	for _, r := range f.requests {
		if aws.ToString(r.ClientToken) != spec.OperationID || aws.ToInt32(r.MaxCount) != 1 || aws.ToBool(r.DryRun) {
			t.Fatal("retry lost request identity")
		}
		if r.MetadataOptions.HttpTokens != ec2types.HttpTokensStateRequired || aws.ToBool(r.NetworkInterfaces[0].AssociatePublicIpAddress) {
			t.Fatal("unsafe networking or metadata")
		}
		disk := r.BlockDeviceMappings[0].Ebs
		if !aws.ToBool(disk.Encrypted) || aws.ToBool(disk.DeleteOnTermination) {
			t.Fatal("disk not protected")
		}
	}
}

func TestAWSOnlyRecognizesExplicitDryRunSuccess(t *testing.T) {
	f := &fakeEC2{}
	p := &AWS{compute: f}
	for _, code := range []string{"DryRunOperation", "UnauthorizedOperation", "RequestLimitExceeded", ""} {
		f.err = nil
		if code != "" {
			f.err = &smithy.GenericAPIError{Code: code}
		}
		err := p.CheckLaunchPermission(context.Background(), fixtureSpec())
		if (err == nil) != (code == "DryRunOperation") {
			t.Fatalf("incorrect dry run classification: %s", code)
		}
		if !aws.ToBool(f.requests[len(f.requests)-1].DryRun) {
			t.Fatal("permission check created infrastructure")
		}
	}
}

func TestAWSLostResponseReconcilesWithoutAnotherLaunch(t *testing.T) {
	f := &fakeEC2{err: errors.New("lost response")}
	p := &AWS{compute: f}
	if _, err := p.Provision(context.Background(), fixtureSpec()); err == nil {
		t.Fatal("expected uncertain launch")
	}
	f.err = nil
	f.instances = []ec2types.Instance{{InstanceId: aws.String("i-fixture1"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNamePending}}}
	id, state, err := p.Lookup(context.Background(), fixtureSpec().OperationID)
	if err != nil || id != "i-fixture1" || state != "pending" || len(f.requests) != 1 {
		t.Fatal("failed safe reconciliation")
	}
	f.instances = append(f.instances, f.instances[0])
	if _, _, err := p.Lookup(context.Background(), fixtureSpec().OperationID); err == nil {
		t.Fatal("ambiguous ownership accepted")
	}
}
