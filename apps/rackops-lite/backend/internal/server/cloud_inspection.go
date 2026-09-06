package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/rackops/rackops/internal/cloud"
)

func (a *API) inspectAWSMachine(ctx context.Context, region string, credentials cloud.AWSCredentials, spec cloud.AWSMachineSpec, architecture string) (cloud.MachineInspection, error) {
	if a.awsMachineCheck != nil {
		return a.awsMachineCheck(ctx, region, credentials, spec, architecture)
	}
	p, err := cloud.NewAWS(region, credentials)
	if err != nil {
		return cloud.MachineInspection{}, err
	}
	return p.InspectMachine(ctx, spec, architecture)
}

func (a *API) handleCloudMachinePreflight(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	var connection CloudConnection
	a.Store.View(func() {
		if c := a.Store.CloudConnections[r.PathValue("id")]; c != nil && c.OrgID == org.ID {
			connection = *c
		}
	})
	if connection.ID == "" {
		http.Error(w, "cloud connection not found", 404)
		return
	}
	if connection.Provider != "aws" && connection.Provider != "gcp" && connection.Provider != "azure" {
		http.Error(w, "unsupported cloud provider", 501)
		return
	}
	var req struct {
		Spec         cloud.MachineSpec `json:"spec"`
		Architecture string            `json:"architecture"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || req.Spec.Validate(connection.Provider) != nil || (req.Architecture != "amd64" && req.Architecture != "arm64") {
		http.Error(w, "a matching provider specification and amd64 or arm64 architecture are required", 400)
		return
	}
	plain, err := decryptState(a.Store.stateKey, connection.SealedCredentials)
	if err != nil {
		http.Error(w, "connection key unavailable; restore the original state key", 409)
		return
	}
	if !json.Valid(plain) {
		http.Error(w, "invalid stored cloud credentials", 409)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	identity, err := a.validateCloud(ctx, connection.Provider, connection.Region, plain)
	if err != nil {
		http.Error(w, "cloud identity validation failed; check credentials and connectivity", 422)
		return
	}
	if identity.AccountID != connection.AccountID || identity.Principal != connection.Principal {
		http.Error(w, "cloud identity changed; replace the connection", 409)
		return
	}
	review, err := a.inspectCloudMachine(ctx, connection.Provider, connection.Region, plain, req.Spec, req.Architecture)
	if err != nil {
		// SDK errors can contain request details. Never return raw provider errors.
		http.Error(w, "cloud resource inspection failed; check credentials, selected resource IDs, provider read permissions and connectivity", 422)
		return
	}
	// Cloud requests can outlive an organization membership or connection.
	if a.cloudOrg(w, r) == nil {
		return
	}
	found := false
	a.Store.View(func() {
		c := a.Store.CloudConnections[connection.ID]
		found = c != nil && c.OrgID == org.ID && c.AccountID == connection.AccountID && c.Principal == connection.Principal
	})
	if !found {
		http.Error(w, "cloud connection no longer available", 404)
		return
	}
	review.CanLaunch = false
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, review)
}

func (a *API) inspectCloudMachine(ctx context.Context, name, region string, raw json.RawMessage, spec cloud.MachineSpec, architecture string) (cloud.MachineInspection, error) {
	if a.cloudMachineCheck != nil {
		return a.cloudMachineCheck(ctx, name, region, raw, spec, architecture)
	}
	if name == "aws" {
		var credentials cloud.AWSCredentials
		if err := json.Unmarshal(raw, &credentials); err != nil {
			return cloud.MachineInspection{}, err
		}
		return a.inspectAWSMachine(ctx, region, credentials, *spec.AWS, architecture)
	}
	provider, err := cloud.NewProvider(ctx, name, region, raw)
	if err != nil {
		return cloud.MachineInspection{}, err
	}
	switch p := provider.(type) {
	case *cloud.GCP:
		return p.InspectMachine(ctx, *spec.GCP, architecture)
	case *cloud.Azure:
		return p.InspectMachine(ctx, *spec.Azure, architecture)
	default:
		return cloud.MachineInspection{}, errors.New("unsupported cloud inspection provider")
	}
}
