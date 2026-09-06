package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

type GCPCredentials struct {
	ProjectID          string `json:"projectId"`
	ServiceAccountJSON string `json:"serviceAccountJson"`
}
type GCP struct {
	bootstrap string
	region    string
	project   string
	principal string
	compute   *compute.Service
	projects  *cloudresourcemanager.Service
}

var gcpProject = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
var gcpRegion = regexp.MustCompile(`^[a-z]+-[a-z]+[0-9]+$`)
var gcpResource = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

func gcpJWT(c GCPCredentials) (*jwt.Config, error) {
	var key struct {
		Type       string `json:"type"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
		KeyID      string `json:"private_key_id"`
		TokenURI   string `json:"token_uri"`
	}
	if !gcpProject.MatchString(c.ProjectID) || json.Unmarshal([]byte(c.ServiceAccountJSON), &key) != nil || key.Type != "service_account" || !strings.HasSuffix(key.Email, ".iam.gserviceaccount.com") || strings.ContainsAny(key.Email, "\r\n ") || key.PrivateKey == "" || (key.TokenURI != "" && key.TokenURI != "https://oauth2.googleapis.com/token") {
		return nil, errors.New("explicit Google service-account credentials and project ID are required")
	}
	// Never accept credential-provided token URLs, external-account executable
	// sources, or the control-plane host's default credentials.
	return &jwt.Config{Email: key.Email, PrivateKey: []byte(key.PrivateKey), PrivateKeyID: key.KeyID, TokenURL: "https://oauth2.googleapis.com/token", Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"}}, nil
}
func NewGCP(ctx context.Context, region string, c GCPCredentials) (*GCP, error) {
	if !gcpRegion.MatchString(region) {
		return nil, errors.New("invalid Google Cloud region")
	}
	config, err := gcpJWT(c)
	if err != nil {
		return nil, err
	}
	tokens := config.TokenSource(ctx)
	machines, err := compute.NewService(ctx, option.WithTokenSource(tokens))
	if err != nil {
		return nil, err
	}
	projects, err := cloudresourcemanager.NewService(ctx, option.WithTokenSource(tokens))
	if err != nil {
		return nil, err
	}
	return &GCP{region: region, project: c.ProjectID, principal: config.Email, compute: machines, projects: projects}, nil
}
func (p *GCP) Name() string   { return "gcp" }
func (p *GCP) Region() string { return p.region }
func (p *GCP) Validate(ctx context.Context) (Identity, error) {
	project, err := p.projects.Projects.Get(p.project).Context(ctx).Do()
	if err != nil {
		return Identity{}, err
	}
	if project.ProjectId != p.project || project.LifecycleState != "ACTIVE" {
		return Identity{}, errors.New("Google Cloud project is not active")
	}
	return Identity{AccountID: p.project, Principal: p.principal}, nil
}

type GCPMachineSpec struct {
	OperationID string `json:"operationId"`
	ProjectID   string `json:"projectId"`
	Zone        string `json:"zone"`
	MachineType string `json:"machineType"`
	Image       string `json:"image"`
	Subnetwork  string `json:"subnetwork"`
	DiskGB      int64  `json:"diskGb"`
}

func (s GCPMachineSpec) Validate() error {
	if !uuidID.MatchString(s.OperationID) || s.OperationID == "00000000-0000-0000-0000-000000000000" || !gcpProject.MatchString(s.ProjectID) || !regexp.MustCompile(`^[a-z]+-[a-z]+[0-9]+-[a-z]$`).MatchString(s.Zone) || !gcpResource.MatchString(s.MachineType) || s.DiskGB < 10 || s.DiskGB > 65536 {
		return errors.New("invalid Google VM specification")
	}
	image := strings.Split(s.Image, "/")
	if len(image) != 5 || image[0] != "projects" || !gcpProject.MatchString(image[1]) || image[2] != "global" || image[3] != "images" || !gcpResource.MatchString(image[4]) {
		return errors.New("a pinned Google image resource is required")
	}
	subnet := strings.Split(s.Subnetwork, "/")
	if len(subnet) != 6 || subnet[0] != "projects" || subnet[1] != s.ProjectID || subnet[2] != "regions" || subnet[3] != s.Zone[:len(s.Zone)-2] || subnet[4] != "subnetworks" || !gcpResource.MatchString(subnet[5]) {
		return errors.New("subnetwork must belong to target project and region")
	}
	return nil
}
func (s GCPMachineSpec) instance() *compute.Instance {
	return &compute.Instance{Name: "rackops-" + strings.ToLower(s.OperationID), MachineType: "zones/" + s.Zone + "/machineTypes/" + s.MachineType,
		Labels:            map[string]string{"rackops-operation": strings.ToLower(s.OperationID), "rackops-spec": fingerprint(s)[:63]},
		Disks:             []*compute.AttachedDisk{{Boot: true, AutoDelete: false, Type: "PERSISTENT", InitializeParams: &compute.AttachedDiskInitializeParams{SourceImage: s.Image, DiskSizeGb: s.DiskGB, DiskType: "zones/" + s.Zone + "/diskTypes/pd-balanced"}, ForceSendFields: []string{"AutoDelete"}}},
		NetworkInterfaces: []*compute.NetworkInterface{{Subnetwork: s.Subnetwork}},
		ServiceAccounts:   []*compute.ServiceAccount{}, ForceSendFields: []string{"ServiceAccounts"},
		ShieldedInstanceConfig: &compute.ShieldedInstanceConfig{EnableVtpm: true, EnableIntegrityMonitoring: true},
	}
}
func (p *GCP) Start(ctx context.Context, s MachineSpec) (Receipt, error) {
	if err := s.Validate(p.Name()); err != nil {
		return Receipt{}, err
	}
	spec := s.GCP
	if spec.ProjectID != p.project || spec.Zone[:len(spec.Zone)-2] != p.region {
		return Receipt{}, errors.New("Google target mismatch")
	}
	instance := spec.instance()
	if p.bootstrap != "" {
		instance.Metadata = &compute.Metadata{Items: []*compute.MetadataItems{{Key: "startup-script", Value: &p.bootstrap}}}
	}
	operation, err := p.compute.Instances.Insert(p.project, spec.Zone, instance).RequestId(spec.OperationID).Context(ctx).Do()
	if err != nil {
		return Receipt{}, err
	}
	if operation.Name == "" {
		return Receipt{}, errors.New("Google operation identifier missing")
	}
	return Receipt{ResourceID: operation.TargetLink, OperationID: operation.Name, State: operation.Status}, nil
}
func (p *GCP) Reconcile(ctx context.Context, s MachineSpec, receipt Receipt) (Receipt, error) {
	if err := s.Validate(p.Name()); err != nil {
		return Receipt{}, err
	}
	spec := s.GCP
	if spec.ProjectID != p.project || spec.Zone[:len(spec.Zone)-2] != p.region {
		return Receipt{}, errors.New("Google target mismatch")
	}
	if receipt.OperationID != "" {
		operation, err := p.compute.ZoneOperations.Get(p.project, spec.Zone, receipt.OperationID).Context(ctx).Do()
		if err != nil {
			return Receipt{}, err
		}
		if operation.Error != nil {
			return Receipt{}, errors.New("Google provisioning operation failed; inspect cloud operation details")
		}
	}
	instance, err := p.compute.Instances.Get(p.project, spec.Zone, spec.instance().Name).Context(ctx).Do()
	if err != nil {
		return Receipt{}, err
	}
	if instance.Labels["rackops-operation"] != strings.ToLower(spec.OperationID) || instance.Labels["rackops-spec"] != fingerprint(*spec)[:63] || instance.SelfLink == "" {
		return Receipt{}, errors.New("Google resource ownership mismatch")
	}
	return Receipt{ResourceID: instance.SelfLink, OperationID: receipt.OperationID, State: instance.Status}, nil
}
