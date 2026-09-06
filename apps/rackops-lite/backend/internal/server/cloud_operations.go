package server

import (
	"context"
	"errors"
	"github.com/rackops/rackops/internal/cloud"
	"time"
)

// Internal until resource/cost confirmation and single-use enrollment are
// complete. No HTTP route launches these operations.
type AWSProvisioning struct {
	ID            string               `json:"id"`
	OrgID         string               `json:"orgId"`
	ConnectionID  string               `json:"connectionId"`
	Region        string               `json:"region"`
	AccountID     string               `json:"accountId"`
	Spec          cloud.AWSMachineSpec `json:"spec"`
	State         string               `json:"state"`
	ResourceID    string               `json:"resourceId,omitempty"`
	ResourceState string               `json:"resourceState,omitempty"`
	CreatedAt     time.Time            `json:"createdAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
}

type awsProvisioner interface {
	Region() string
	Validate(context.Context) (cloud.Identity, error)
	Provision(context.Context, cloud.AWSMachineSpec) (string, error)
	Lookup(context.Context, string) (string, string, error)
}

// Caller must have approval of the final immutable resource plan.
func (a *API) saveAWSProvisioning(job AWSProvisioning) error {
	if err := job.Spec.Validate(); err != nil {
		return err
	}
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil {
		return errors.New("cloud provisioning is unavailable in HA")
	}
	if job.ID == "" || job.ID != job.Spec.OperationID {
		return errors.New("operation identity missing")
	}
	var failure error
	a.Store.Mutate(func() {
		connection := a.Store.CloudConnections[job.ConnectionID]
		if connection == nil || connection.OrgID != job.OrgID || connection.AccountID != job.AccountID || connection.Region != job.Region || connection.Provider != "aws" {
			failure = errors.New("connection does not match operation")
			return
		}
		if previous := a.Store.AWSProvisioning[job.ID]; previous != nil {
			if previous.Spec != job.Spec || previous.ConnectionID != job.ConnectionID || previous.OrgID != job.OrgID || previous.AccountID != job.AccountID || previous.Region != job.Region {
				failure = errors.New("operation ID already belongs to a different plan")
			}
			return
		}
		if a.Store.AWSProvisioning == nil {
			a.Store.AWSProvisioning = map[string]*AWSProvisioning{}
		}
		job.State = "planned"
		job.ResourceID = ""
		job.ResourceState = ""
		job.CreatedAt = time.Now().UTC()
		job.UpdatedAt = job.CreatedAt
		a.Store.AWSProvisioning[job.ID] = &job
	})
	if failure != nil {
		return failure
	}
	return a.Store.persistOnce()
}

func (a *API) runAWSProvisioning(ctx context.Context, orgID, id string, provider awsProvisioner) error {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil {
		return errors.New("cloud provisioning is unavailable in HA")
	}
	var job AWSProvisioning
	a.Store.View(func() {
		if found := a.Store.AWSProvisioning[id]; found != nil && found.OrgID == orgID {
			job = *found
		}
	})
	if job.ID == "" {
		return errors.New("operation not found")
	}
	if provider.Region() != job.Region {
		return errors.New("provider region does not match saved operation")
	}
	if job.ResourceID != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	identity, err := provider.Validate(ctx)
	if err != nil || identity.AccountID != job.AccountID {
		return errors.New("cloud account validation failed")
	}
	if job.State != "planned" {
		// Empty lookup is not proof that an uncertain launch created nothing.
		resource, state, err := provider.Lookup(ctx, job.ID)
		if err != nil || resource == "" {
			return errors.New("launch outcome remains uncertain; inspect the saved operation in AWS")
		}
		a.Store.Mutate(func() {
			current := a.Store.AWSProvisioning[id]
			current.ResourceID = resource
			current.ResourceState = state
			current.State = "created"
			current.UpdatedAt = time.Now().UTC()
		})
		return a.Store.persistOnce()
	}
	if time.Since(job.CreatedAt) > 30*time.Minute {
		return errors.New("approved resource plan expired; create and review a new plan")
	}
	a.Store.Mutate(func() {
		current := a.Store.AWSProvisioning[id]
		current.State = "submitting"
		current.UpdatedAt = time.Now().UTC()
	})
	if err := a.Store.persistOnce(); err != nil {
		return errors.New("could not persist launch intent; no launch attempted")
	}
	resource, launchErr := provider.Provision(ctx, job.Spec)
	a.Store.Mutate(func() {
		current := a.Store.AWSProvisioning[id]
		current.UpdatedAt = time.Now().UTC()
		if launchErr != nil || resource == "" {
			current.State = "uncertain"
		} else {
			current.State = "created"
			current.ResourceID = resource
		}
	})
	if err := a.Store.persistOnce(); err != nil {
		return errors.New("could not persist launch result; reconcile before proceeding")
	}
	if launchErr != nil || resource == "" {
		return errors.New("AWS launch result is uncertain; reconcile before proceeding")
	}
	return nil
}
