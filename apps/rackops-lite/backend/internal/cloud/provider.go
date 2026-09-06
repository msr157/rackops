package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// MachineSpec is stored unchanged after approval; provider-specific networking
// and image identifiers are part of the same immutable plan.
type MachineSpec struct {
	AWS   *AWSMachineSpec   `json:"aws,omitempty"`
	Azure *AzureMachineSpec `json:"azure,omitempty"`
	GCP   *GCPMachineSpec   `json:"gcp,omitempty"`
}

type Receipt struct {
	ResourceID  string `json:"resourceId,omitempty"`
	OperationID string `json:"operationId,omitempty"`
	State       string `json:"state"`
}

func (s MachineSpec) Fingerprint() string { return fingerprint(s) }

type Provider interface {
	Name() string
	Region() string
	Validate(context.Context) (Identity, error)
	Start(context.Context, MachineSpec) (Receipt, error)
	Reconcile(context.Context, MachineSpec, Receipt) (Receipt, error)
}

func (s MachineSpec) Validate(provider string) error {
	count := 0
	if s.AWS != nil {
		count++
	}
	if s.Azure != nil {
		count++
	}
	if s.GCP != nil {
		count++
	}
	if count != 1 {
		return errors.New("exactly one provider specification is required")
	}
	switch {
	case provider == "aws" && s.AWS != nil:
		return s.AWS.Validate()
	case provider == "azure" && s.Azure != nil:
		return s.Azure.Validate()
	case provider == "gcp" && s.GCP != nil:
		return s.GCP.Validate()
	}
	return errors.New("specification provider mismatch")
}

func (s MachineSpec) ID() string {
	if s.AWS != nil {
		return s.AWS.OperationID
	}
	if s.Azure != nil {
		return s.Azure.OperationID
	}
	if s.GCP != nil {
		return s.GCP.OperationID
	}
	return ""
}
func fingerprint(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (p *AWS) Name() string { return "aws" }
func (p *AWS) Start(ctx context.Context, s MachineSpec) (Receipt, error) {
	if err := s.Validate(p.Name()); err != nil {
		return Receipt{}, err
	}
	id, err := p.Provision(ctx, *s.AWS)
	return Receipt{ResourceID: id, OperationID: s.ID(), State: "submitted"}, err
}
func (p *AWS) Reconcile(ctx context.Context, s MachineSpec, _ Receipt) (Receipt, error) {
	if err := s.Validate(p.Name()); err != nil {
		return Receipt{}, err
	}
	id, state, err := p.Lookup(ctx, s.ID())
	return Receipt{ResourceID: id, OperationID: s.ID(), State: state}, err
}
