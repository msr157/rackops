// Package cloud contains provider operations. Callers must durably save and
// authorize an immutable plan before invoking a mutating operation.
package cloud

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"regexp"
)

type AWSCredentials struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken,omitempty"`
}

type Identity struct {
	AccountID string `json:"accountId"`
	Principal string `json:"principal"`
}

type AWS struct {
	bootstrap string
	region    string
	compute   ec2API
	inventory awsInventoryAPI
	identity  stsAPI
}
type stsAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}
type ec2API interface {
	RunInstances(context.Context, *ec2.RunInstancesInput, ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

var awsRegion = regexp.MustCompile(`^(af|ap|ca|eu|il|me|mx|sa|us)-[a-z]+-[0-9]+$`)

func NewAWS(region string, cred AWSCredentials) (*AWS, error) {
	if !awsRegion.MatchString(region) || cred.AccessKeyID == "" || cred.SecretAccessKey == "" {
		return nil, errors.New("AWS region and explicit credentials are required")
	}
	// No default credential chain or environment endpoint overrides: a customer
	// connection must never silently use the control-plane host's IAM identity.
	config := aws.Config{Region: region, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cred.AccessKeyID, cred.SecretAccessKey, cred.SessionToken)), RetryMaxAttempts: 3}
	compute := ec2.NewFromConfig(config)
	return &AWS{region: region, compute: compute, inventory: compute, identity: sts.NewFromConfig(config)}, nil
}

func (p *AWS) Region() string { return p.region }

func (p *AWS) Validate(ctx context.Context) (Identity, error) {
	result, err := p.identity.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return Identity{}, err
	}
	if result == nil || aws.ToString(result.Account) == "" || aws.ToString(result.Arn) == "" {
		return Identity{}, errors.New("AWS identity response was incomplete")
	}
	return Identity{AccountID: aws.ToString(result.Account), Principal: aws.ToString(result.Arn)}, nil
}

// AWSMachineSpec is an internal immutable resource specification. This is not
// yet exposed as a launch API: image inspection, prices, enrollment and customer
// confirmation must be wired before allowing a production launch.
type AWSMachineSpec struct {
	OperationID     string `json:"operationId"`
	ImageID         string `json:"imageId"`
	InstanceType    string `json:"instanceType"`
	SubnetID        string `json:"subnetId"`
	SecurityGroupID string `json:"securityGroupId"`
	RootDevice      string `json:"rootDevice"`
	DiskGB          int32  `json:"diskGb"`
}

var resourceID = regexp.MustCompile(`^[a-z0-9-]{8,64}$`)
var instanceType = regexp.MustCompile(`^[a-z0-9]+\.[a-z0-9]+$`)

func (s AWSMachineSpec) Validate() error { _, err := s.request(true); return err }

func (s AWSMachineSpec) request(dryRun bool) (*ec2.RunInstancesInput, error) {
	if !resourceID.MatchString(s.OperationID) || !resourceID.MatchString(s.ImageID) || !resourceID.MatchString(s.SubnetID) || !resourceID.MatchString(s.SecurityGroupID) || !instanceType.MatchString(s.InstanceType) || (s.RootDevice != "/dev/sda1" && s.RootDevice != "/dev/xvda") || s.DiskGB < 8 || s.DiskGB > 16384 {
		return nil, errors.New("incomplete AWS resource specification")
	}
	return &ec2.RunInstancesInput{
		ClientToken: aws.String(s.OperationID), ImageId: aws.String(s.ImageID), InstanceType: ec2types.InstanceType(s.InstanceType), MinCount: aws.Int32(1), MaxCount: aws.Int32(1), DryRun: aws.Bool(dryRun),
		NetworkInterfaces:   []ec2types.InstanceNetworkInterfaceSpecification{{DeviceIndex: aws.Int32(0), SubnetId: aws.String(s.SubnetID), Groups: []string{s.SecurityGroupID}, AssociatePublicIpAddress: aws.Bool(false), DeleteOnTermination: aws.Bool(true)}},
		MetadataOptions:     &ec2types.InstanceMetadataOptionsRequest{HttpTokens: ec2types.HttpTokensStateRequired, HttpEndpoint: ec2types.InstanceMetadataEndpointStateEnabled},
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{{DeviceName: aws.String(s.RootDevice), Ebs: &ec2types.EbsBlockDevice{Encrypted: aws.Bool(true), DeleteOnTermination: aws.Bool(false), VolumeType: ec2types.VolumeTypeGp3, VolumeSize: aws.Int32(s.DiskGB)}}},
		TagSpecifications:   []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypeInstance, Tags: []ec2types.Tag{{Key: aws.String("rackops-operation"), Value: aws.String(s.OperationID)}}}, {ResourceType: ec2types.ResourceTypeVolume, Tags: []ec2types.Tag{{Key: aws.String("rackops-operation"), Value: aws.String(s.OperationID)}}}},
	}, nil
}

func (p *AWS) CheckLaunchPermission(ctx context.Context, s AWSMachineSpec) error {
	req, err := s.request(true)
	if err != nil {
		return err
	}
	if p.bootstrap != "" {
		req.UserData = aws.String(base64.StdEncoding.EncodeToString([]byte(p.bootstrap)))
	}
	_, err = p.compute.RunInstances(ctx, req)
	var apiError smithy.APIError
	if errors.As(err, &apiError) && apiError.ErrorCode() == "DryRunOperation" {
		return nil
	}
	if err == nil {
		return errors.New("AWS dry-run did not confirm permissions")
	}
	return err
}

func (p *AWS) Provision(ctx context.Context, s AWSMachineSpec) (string, error) {
	req, err := s.request(false)
	if err != nil {
		return "", err
	}
	if p.bootstrap != "" {
		req.UserData = aws.String(base64.StdEncoding.EncodeToString([]byte(p.bootstrap)))
	}
	result, err := p.compute.RunInstances(ctx, req)
	if err != nil {
		return "", err
	}
	if result == nil || len(result.Instances) != 1 || aws.ToString(result.Instances[0].InstanceId) == "" {
		return "", errors.New("AWS launch outcome is uncertain; reconcile using the saved operation ID")
	}
	return aws.ToString(result.Instances[0].InstanceId), nil
}

// Lookup reconciles a lost launch response without starting another instance.
func (p *AWS) Lookup(ctx context.Context, operationID string) (string, string, error) {
	if !resourceID.MatchString(operationID) {
		return "", "", errors.New("invalid operation ID")
	}
	result, err := p.compute.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: []ec2types.Filter{{Name: aws.String("client-token"), Values: []string{operationID}}}})
	if err != nil {
		return "", "", err
	}
	if result == nil || result.NextToken != nil {
		return "", "", errors.New("AWS reconciliation response incomplete")
	}
	var id, state string
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if id != "" {
				return "", "", errors.New("multiple instances match operation; manual reconciliation required")
			}
			id = aws.ToString(instance.InstanceId)
			if instance.State != nil {
				state = string(instance.State.Name)
			}
		}
	}
	return id, state, nil
}
