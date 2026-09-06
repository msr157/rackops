package server

import (
	"context"
	"encoding/json"
	"github.com/rackops/rackops/internal/cloud"
	"github.com/rackops/rackops/internal/types"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Credentials are encrypted before entering Store, so even a plaintext Raft
// export contains ciphertext. The enclosing state remains encrypted as well.
type CloudConnection struct {
	ID                string    `json:"id"`
	OrgID             string    `json:"orgId"`
	Name              string    `json:"name"`
	Provider          string    `json:"provider"`
	Region            string    `json:"region"`
	AccountID         string    `json:"accountId"`
	Principal         string    `json:"principal"`
	ValidatedAt       time.Time `json:"validatedAt"`
	SealedCredentials []byte    `json:"sealedCredentials,omitempty"`
}

func (a *API) cloudOrg(w http.ResponseWriter, r *http.Request) *types.Organization {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return nil
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageMachines }) {
		http.Error(w, "machine management permission is required", 403)
		return nil
	}
	// Multi-server key distribution and operation ownership are not yet solved.
	if a.Cluster != nil {
		http.Error(w, "cloud connections currently require a single-server control plane", 409)
		return nil
	}
	return org
}

func (a *API) validateAWS(ctx context.Context, region string, credentials cloud.AWSCredentials) (cloud.Identity, error) {
	if a.awsIdentityCheck != nil {
		return a.awsIdentityCheck(ctx, region, credentials)
	}
	provider, err := cloud.NewAWS(region, credentials)
	if err != nil {
		return cloud.Identity{}, err
	}
	return provider.Validate(ctx)
}

func (a *API) validateCloud(ctx context.Context, name, region string, raw json.RawMessage) (cloud.Identity, error) {
	if a.cloudIdentityCheck != nil {
		return a.cloudIdentityCheck(ctx, name, region, raw)
	}
	if name == "aws" {
		var c cloud.AWSCredentials
		if err := json.Unmarshal(raw, &c); err != nil {
			return cloud.Identity{}, err
		}
		return a.validateAWS(ctx, region, c)
	}
	provider, err := cloud.NewProvider(ctx, name, region, raw)
	if err != nil {
		return cloud.Identity{}, err
	}
	return provider.Validate(ctx)
}

func (a *API) handleCloudConnections(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	out := []CloudConnection{}
	a.Store.View(func() {
		for _, connection := range a.Store.CloudConnections {
			if connection.OrgID == org.ID {
				copy := *connection
				copy.SealedCredentials = nil
				out = append(out, copy)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, out)
}

func (a *API) handleCreateCloudConnection(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	var req struct {
		Name        string          `json:"name"`
		Provider    string          `json:"provider"`
		Region      string          `json:"region"`
		Credentials json.RawMessage `json:"credentials"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid cloud connection request", 400)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		http.Error(w, "connection name must contain 1 to 100 bytes", 400)
		return
	}
	if _, err := cloud.NewProvider(r.Context(), req.Provider, req.Region, req.Credentials); err != nil {
		http.Error(w, "valid provider, region and explicit credentials are required", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	identity, err := a.validateCloud(ctx, req.Provider, req.Region, req.Credentials)
	if err != nil {
		http.Error(w, "cloud identity validation failed; check credentials, account access, expiration and connectivity", 422)
		return
	}
	sealed, err := encryptState(a.Store.stateKey, req.Credentials)
	if err != nil {
		http.Error(w, "could not protect credentials", 500)
		return
	}
	connection := CloudConnection{ID: NewID(), OrgID: org.ID, Name: req.Name, Provider: req.Provider, Region: req.Region, AccountID: identity.AccountID, Principal: identity.Principal, ValidatedAt: time.Now().UTC(), SealedCredentials: sealed}
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	if a.cloudOrg(w, r) == nil {
		return
	}
	a.Store.Mutate(func() {
		if a.Store.CloudConnections == nil {
			a.Store.CloudConnections = map[string]*CloudConnection{}
		}
		a.Store.CloudConnections[connection.ID] = &connection
	})
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not persist connection; inspect the connection list before retrying", 503)
		return
	}
	out := connection
	out.SealedCredentials = nil
	writeJSON(w, out)
}

func (a *API) handleDeleteCloudConnection(w http.ResponseWriter, r *http.Request) {
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	found := false
	referenced := false
	a.Store.Mutate(func() {
		if connection := a.Store.CloudConnections[r.PathValue("id")]; connection != nil && connection.OrgID == org.ID {
			for _, job := range a.Store.CloudProvisioning {
				if job.ConnectionID == connection.ID {
					referenced = true
					return
				}
			}
			for _, job := range a.Store.AWSProvisioning {
				if job.ConnectionID == connection.ID {
					referenced = true
					return
				}
			}
			delete(a.Store.CloudConnections, connection.ID)
			found = true
		}
	})
	if referenced {
		http.Error(w, "connection is referenced by a provisioning operation; resolve its resources before removing credentials", 409)
		return
	}
	if !found {
		http.Error(w, "connection not found", 404)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not persist removal", 503)
		return
	}
	w.WriteHeader(204)
}

func (a *API) handleValidateCloudConnection(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	var connection CloudConnection
	a.Store.View(func() {
		if found := a.Store.CloudConnections[r.PathValue("id")]; found != nil && found.OrgID == org.ID {
			connection = *found
		}
	})
	if connection.ID == "" {
		http.Error(w, "connection not found", 404)
		return
	}
	plain, err := decryptState(a.Store.stateKey, connection.SealedCredentials)
	if err != nil {
		http.Error(w, "connection key unavailable; restore the original state key or replace this connection", 409)
		return
	}
	if !json.Valid(plain) {
		http.Error(w, "invalid stored credentials", 409)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	identity, err := a.validateCloud(ctx, connection.Provider, connection.Region, plain)
	if err != nil {
		http.Error(w, "cloud identity validation failed; check credentials, account access, expiration and connectivity", 422)
		return
	}
	if identity.AccountID != connection.AccountID || identity.Principal != connection.Principal {
		http.Error(w, "cloud identity changed; replace the connection", 409)
		return
	}
	found := false
	a.Store.Mutate(func() {
		if current := a.Store.CloudConnections[connection.ID]; current != nil && current.OrgID == org.ID {
			current.ValidatedAt = time.Now().UTC()
			connection = *current
			found = true
		}
	})
	if !found {
		http.Error(w, "connection not found", 404)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not persist validation", 503)
		return
	}
	connection.SealedCredentials = nil
	writeJSON(w, connection)
}
