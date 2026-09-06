package cloud

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type fakeAWSInventory struct {
	t         *testing.T
	image     types.Image
	size      types.InstanceTypeInfo
	subnet    types.Subnet
	group     types.SecurityGroup
	offerings []types.InstanceTypeOffering
	err       error
	imagePage *string
	calls     int
}

func inventoryFixture(t *testing.T) *fakeAWSInventory {
	s := fixtureSpec()
	return &fakeAWSInventory{t: t,
		image:     types.Image{ImageId: aws.String(s.ImageID), State: types.ImageStateAvailable, Architecture: types.ArchitectureValuesX8664, PlatformDetails: aws.String("Linux/UNIX"), RootDeviceType: types.DeviceTypeEbs, RootDeviceName: aws.String(s.RootDevice), VirtualizationType: types.VirtualizationTypeHvm, BlockDeviceMappings: []types.BlockDeviceMapping{{DeviceName: aws.String(s.RootDevice), Ebs: &types.EbsBlockDevice{VolumeSize: aws.Int32(10)}}}},
		size:      types.InstanceTypeInfo{InstanceType: types.InstanceType(s.InstanceType), ProcessorInfo: &types.ProcessorInfo{SupportedArchitectures: []types.ArchitectureType{types.ArchitectureTypeX8664}}, VCpuInfo: &types.VCpuInfo{DefaultVCpus: aws.Int32(2)}, MemoryInfo: &types.MemoryInfo{SizeInMiB: aws.Int64(2048)}},
		subnet:    types.Subnet{SubnetId: aws.String(s.SubnetID), State: types.SubnetStateAvailable, VpcId: aws.String("vpc-fixture"), AvailabilityZone: aws.String("us-east-1a"), AvailableIpAddressCount: aws.Int32(10)},
		group:     types.SecurityGroup{GroupId: aws.String(s.SecurityGroupID), VpcId: aws.String("vpc-fixture")},
		offerings: []types.InstanceTypeOffering{{InstanceType: types.InstanceType(s.InstanceType), LocationType: types.LocationTypeAvailabilityZone, Location: aws.String("us-east-1a")}},
	}
}
func (f *fakeAWSInventory) DescribeImages(_ context.Context, r *ec2.DescribeImagesInput, _ ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	f.calls++
	if len(r.ImageIds) != 1 || r.ImageIds[0] != fixtureSpec().ImageID {
		f.t.Fatal("unscoped image query")
	}
	return &ec2.DescribeImagesOutput{Images: []types.Image{f.image}, NextToken: f.imagePage}, f.err
}
func (f *fakeAWSInventory) DescribeInstanceTypes(_ context.Context, r *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	f.calls++
	if len(r.InstanceTypes) != 1 || string(r.InstanceTypes[0]) != fixtureSpec().InstanceType {
		f.t.Fatal("unscoped size query")
	}
	return &ec2.DescribeInstanceTypesOutput{InstanceTypes: []types.InstanceTypeInfo{f.size}}, f.err
}
func (f *fakeAWSInventory) DescribeSubnets(_ context.Context, r *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	f.calls++
	if len(r.SubnetIds) != 1 || r.SubnetIds[0] != fixtureSpec().SubnetID {
		f.t.Fatal("unscoped subnet query")
	}
	return &ec2.DescribeSubnetsOutput{Subnets: []types.Subnet{f.subnet}}, f.err
}
func (f *fakeAWSInventory) DescribeSecurityGroups(_ context.Context, r *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	f.calls++
	if len(r.GroupIds) != 1 || r.GroupIds[0] != fixtureSpec().SecurityGroupID {
		f.t.Fatal("unscoped security group query")
	}
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []types.SecurityGroup{f.group}}, f.err
}
func (f *fakeAWSInventory) DescribeInstanceTypeOfferings(_ context.Context, r *ec2.DescribeInstanceTypeOfferingsInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
	f.calls++
	if r.LocationType != types.LocationTypeAvailabilityZone || len(r.Filters) != 2 || len(r.Filters[1].Values) != 1 || r.Filters[1].Values[0] != "us-east-1a" {
		f.t.Fatal("offering query not bound to subnet zone")
	}
	return &ec2.DescribeInstanceTypeOfferingsOutput{InstanceTypeOfferings: f.offerings}, f.err
}

func TestAWSInspectionRejectsIncompatibleResources(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*fakeAWSInventory)
	}{
		{"compatible", "", func(*fakeAWSInventory) {}},
		{"wrong architecture", "image_architecture", func(f *fakeAWSInventory) { f.image.Architecture = types.ArchitectureValuesArm64 }},
		{"Windows", "image_os", func(f *fakeAWSInventory) { f.image.PlatformDetails = aws.String("Windows") }},
		{"missing OS", "image_os", func(f *fakeAWSInventory) { f.image.PlatformDetails = nil }},
		{"denied image", "image_unavailable", func(f *fakeAWSInventory) { f.image.ImageAllowed = aws.Bool(false) }},
		{"wrong root device", "image_boot", func(f *fakeAWSInventory) { f.image.RootDeviceName = aws.String("/dev/xvda") }},
		{"extra disks", "image_storage", func(f *fakeAWSInventory) {
			f.image.BlockDeviceMappings = append(f.image.BlockDeviceMappings, types.BlockDeviceMapping{})
		}},
		{"large image", "disk_size", func(f *fakeAWSInventory) { f.image.BlockDeviceMappings[0].Ebs.VolumeSize = aws.Int32(50) }},
		{"unknown CPU", "size_architecture", func(f *fakeAWSInventory) { f.size.ProcessorInfo = nil }},
		{"unknown memory", "size_unknown", func(f *fakeAWSInventory) { f.size.MemoryInfo = nil }},
		{"GPU", "unsupported_hardware", func(f *fakeAWSInventory) { f.size.GpuInfo = &types.GpuInfo{} }},
		{"different VPC", "network_mismatch", func(f *fakeAWSInventory) { f.group.VpcId = aws.String("vpc-other") }},
		{"full subnet", "subnet_unavailable", func(f *fakeAWSInventory) { f.subnet.AvailableIpAddressCount = aws.Int32(0) }},
		{"IPv6 only", "subnet_unavailable", func(f *fakeAWSInventory) { f.subnet.Ipv6Native = aws.Bool(true) }},
		{"no zone offering", "size_not_offered", func(f *fakeAWSInventory) { f.offerings = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := inventoryFixture(t)
			tc.change(f)
			p := &AWS{region: "us-east-1", inventory: f}
			review, err := p.InspectMachine(context.Background(), fixtureSpec(), "amd64")
			if err != nil {
				t.Fatal(err)
			}
			if review.CanLaunch || review.MonthlyCostEstimate != nil || len(review.RemainingChecks) == 0 || f.calls != 5 {
				t.Fatal("inspection overstated launch readiness or skipped reads")
			}
			if tc.code == "" {
				if !review.ResourceCompatible || review.VCPUs != 2 || review.MemoryMB != 2048 || len(review.Blockers) != 0 {
					t.Fatalf("valid resource rejected: %+v", review)
				}
			} else {
				found := false
				for _, finding := range review.Blockers {
					found = found || finding.Code == tc.code
				}
				if !found || review.ResourceCompatible {
					t.Fatalf("missing blocker %s: %+v", tc.code, review)
				}
			}
		})
	}
}

func TestAWSInspectionFailsClosedOnIncompleteMetadata(t *testing.T) {
	for _, mode := range []string{"error", "pagination", "wrong-resource"} {
		f := inventoryFixture(t)
		switch mode {
		case "error":
			f.err = errors.New("permission denied")
		case "pagination":
			f.imagePage = aws.String("next")
		case "wrong-resource":
			f.image.ImageId = aws.String("ami-unrequested")
		}
		p := &AWS{inventory: f}
		if review, err := p.InspectMachine(context.Background(), fixtureSpec(), "amd64"); err == nil || review.ResourceCompatible || review.CanLaunch {
			t.Fatal("incomplete read treated as compatible")
		}
		if f.calls != 1 {
			t.Fatal("continued after failed image lookup")
		}
	}
}
