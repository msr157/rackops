package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/rackops/rackops/internal/cloud"
	"time"
)

// Internal until enrollment and the concrete resource/cost confirmation flow
// are available. Provider-ready never means the application passed validation.
type CloudProvisioning struct {
	BootstrapConfig *cloud.BootstrapConfig `json:"bootstrapConfig,omitempty"`
	SealedBootstrap []byte                 `json:"sealedBootstrap,omitempty"`
	EnrollmentHash  string                 `json:"enrollmentHash,omitempty"`
	ID              string                 `json:"id"`
	OrgID           string                 `json:"orgId"`
	ConnectionID    string                 `json:"connectionId"`
	Provider        string                 `json:"provider"`
	Region          string                 `json:"region"`
	AccountID       string                 `json:"accountId"`
	Spec            cloud.MachineSpec      `json:"spec"`
	State           string                 `json:"state"`
	Receipt         cloud.Receipt          `json:"receipt"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
}

func (a *API) saveCloudProvisioning(job CloudProvisioning) error {
	if err := job.Spec.Validate(job.Provider); err != nil {
		return err
	}
	if job.ID == "" || job.ID != job.Spec.ID() {
		return errors.New("operation identity mismatch")
	}
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil {
		return errors.New("cloud provisioning requires a single server")
	}
	var failure error
	a.Store.Mutate(func() {
		c := a.Store.CloudConnections[job.ConnectionID]
		if c == nil || c.OrgID != job.OrgID || c.Provider != job.Provider || c.AccountID != job.AccountID || c.Region != job.Region {
			failure = errors.New("connection mismatch")
			return
		}
		if old := a.Store.CloudProvisioning[job.ID]; old != nil {
			if old.Spec.Fingerprint() != job.Spec.Fingerprint() || old.OrgID != job.OrgID || old.ConnectionID != job.ConnectionID || old.Region != job.Region || old.AccountID != job.AccountID || old.Provider != job.Provider {
				failure = errors.New("operation ID belongs to another immutable plan")
			}
			return
		}
		// A caller cannot mutate the persisted plan through its nested pointers.
		raw, _ := json.Marshal(job.Spec)
		var cloned cloud.MachineSpec
		_ = json.Unmarshal(raw, &cloned)
		job.Spec = cloned
		job.State = "planned"
		job.BootstrapConfig = nil
		job.SealedBootstrap = nil
		job.EnrollmentHash = ""
		job.Receipt = cloud.Receipt{}
		job.CreatedAt = time.Now().UTC()
		job.UpdatedAt = job.CreatedAt
		if a.Store.CloudProvisioning == nil {
			a.Store.CloudProvisioning = map[string]*CloudProvisioning{}
		}
		a.Store.CloudProvisioning[job.ID] = &job
	})
	if failure != nil {
		return failure
	}
	return a.Store.persistOnce()
}

func (a *API) runCloudProvisioning(ctx context.Context, orgID, id string, p cloud.Provider) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil {
		return errors.New("cloud provisioning requires a single server")
	}
	var job CloudProvisioning
	a.Store.View(func() {
		if found := a.Store.CloudProvisioning[id]; found != nil && found.OrgID == orgID {
			raw, _ := json.Marshal(found)
			_ = json.Unmarshal(raw, &job)
		}
	})
	if job.ID == "" || p.Name() != job.Provider || p.Region() != job.Region {
		return errors.New("operation scope mismatch")
	}
	// Do not spend money on a launch with an expired or already-consumed grant.
	// Reconciliation must remain possible after expiry to recover resource IDs.
	if job.State == "planned" && job.EnrollmentHash != "" {
		valid := false
		a.Store.View(func() {
			grant := a.Store.NodeEnrollments[job.EnrollmentHash]
			valid = grant != nil && grant.OrgID == orgID && grant.NodeID == "" && time.Now().Before(grant.ExpiresAt)
		})
		if !valid {
			return errors.New("bootstrap enrollment is expired, consumed or unavailable; no launch attempted")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if len(job.SealedBootstrap) > 0 {
		plain, err := decryptState(a.Store.stateKey, job.SealedBootstrap)
		if err != nil {
			return errors.New("bootstrap key unavailable; restore the original state key")
		}
		p, err = cloud.WithBootstrap(p, string(plain))
		if err != nil {
			return errors.New("provider bootstrap preparation failed")
		}
	}
	identity, err := p.Validate(ctx)
	if err != nil || identity.AccountID != job.AccountID {
		return errors.New("cloud account validation failed")
	}
	var receipt cloud.Receipt
	if job.State == "planned" {
		if time.Since(job.CreatedAt) > 30*time.Minute {
			return errors.New("plan approval expired")
		}
		a.Store.Mutate(func() {
			a.Store.CloudProvisioning[id].State = "submitting"
			a.Store.CloudProvisioning[id].UpdatedAt = time.Now().UTC()
		})
		if err := a.Store.persistOnce(); err != nil {
			return errors.New("could not persist launch intent; no launch attempted")
		}
		receipt, err = p.Start(ctx, job.Spec)
	} else {
		receipt, err = p.Reconcile(ctx, job.Spec, job.Receipt)
	}
	uncertain := err != nil || (receipt.ResourceID == "" && (job.State != "planned" || receipt.OperationID == ""))
	a.Store.Mutate(func() {
		current := a.Store.CloudProvisioning[id]
		current.UpdatedAt = time.Now().UTC()
		// Retain provider operation identifiers even when the response also reports
		// an error, so later reconciliation has all available evidence.
		if receipt.ResourceID != "" {
			current.Receipt.ResourceID = receipt.ResourceID
		}
		if receipt.OperationID != "" {
			current.Receipt.OperationID = receipt.OperationID
		}
		if receipt.State != "" {
			current.Receipt.State = receipt.State
		}
		if uncertain {
			current.State = "uncertain"
		} else {
			current.State = "submitted"
		}
	})
	if failure := a.Store.persistOnce(); failure != nil {
		return errors.New("could not persist provider result; reconcile before proceeding")
	}
	if uncertain {
		return errors.New("provider outcome uncertain; reconcile before proceeding")
	}
	return nil
}
