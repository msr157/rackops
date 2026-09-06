package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/rackops/rackops/internal/cloud"
	"time"
)

// prepareCloudBootstrap seals a single startup payload into a planned job.
// Repeated preparation preserves the original enrollment token and script.
func (a *API) prepareCloudBootstrap(orgID, id string, config cloud.BootstrapConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if config.OrgID != orgID {
		return errors.New("bootstrap organization mismatch")
	}
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.Cluster != nil {
		return errors.New("cloud bootstrap requires a single server")
	}
	var job CloudProvisioning
	a.Store.View(func() {
		if found := a.Store.CloudProvisioning[id]; found != nil && found.OrgID == orgID {
			job = *found
		}
	})
	if job.ID == "" || job.State != "planned" {
		return errors.New("bootstrap requires an unsubmitted provisioning plan")
	}
	if job.BootstrapConfig != nil {
		if *job.BootstrapConfig != config {
			return errors.New("bootstrap is immutable; create a new resource plan")
		}
		return a.Store.persistOnce()
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	proof := "rkenroll_" + hex.EncodeToString(raw)
	script, err := cloud.RenderBootstrap(config, proof)
	if err != nil {
		return err
	}
	sealed, err := encryptState(a.Store.stateKey, []byte(script))
	if err != nil {
		return err
	}
	a.Store.Mutate(func() {
		current := a.Store.CloudProvisioning[id]
		current.BootstrapConfig = &config
		current.SealedBootstrap = sealed
		current.EnrollmentHash = sha256Hex(proof)
		if a.Store.NodeEnrollments == nil {
			a.Store.NodeEnrollments = map[string]*NodeEnrollment{}
		}
		a.Store.NodeEnrollments[current.EnrollmentHash] = &NodeEnrollment{OrgID: orgID, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	})
	return a.Store.persistOnce()
}
