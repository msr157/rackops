package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rackops/rackops/internal/cloud"
)

func (a *API) handleCloudComputeQuote(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	var plan CloudReviewPlan
	a.Store.View(func() {
		if p := a.Store.CloudReviewPlans[r.PathValue("id")]; p != nil && p.OrgID == org.ID {
			plan = *p
		}
	})
	if plan.ID == "" {
		http.Error(w, "cloud review not found", 404)
		return
	}
	if (plan.Provider != "azure" || plan.Spec.Azure == nil) && (plan.Provider != "aws" || plan.Spec.AWS == nil) && (plan.Provider != "gcp" || plan.Spec.GCP == nil) {
		http.Error(w, "compute pricing is not yet supported for this provider", 409)
		return
	}
	if time.Now().After(plan.ExpiresAt) {
		http.Error(w, "review expired; create a fresh resource review", 409)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var quote cloud.ComputeQuote
	var err error
	if plan.Provider == "azure" {
		estimate := a.azureComputeQuote
		if estimate == nil {
			estimate = cloud.AzureComputeQuote
		}
		quote, err = estimate(ctx, plan.Region, plan.Spec.Azure.Size)
	} else {
		var connection CloudConnection
		a.Store.View(func() {
			if c := a.Store.CloudConnections[plan.ConnectionID]; c != nil && c.OrgID == org.ID && c.Provider == plan.Provider && c.AccountID == plan.AccountID && c.Region == plan.Region {
				connection = *c
			}
		})
		if connection.ID == "" {
			http.Error(w, "original cloud connection unavailable", 409)
			return
		}
		plain, decryptErr := decryptState(a.Store.stateKey, connection.SealedCredentials)
		if decryptErr != nil || !json.Valid(plain) {
			http.Error(w, "connection credentials unavailable", 409)
			return
		}
		identity, identityErr := a.validateCloud(ctx, connection.Provider, connection.Region, plain)
		if identityErr != nil || identity.AccountID != connection.AccountID || identity.Principal != connection.Principal {
			http.Error(w, "cloud identity validation failed; revalidate the connection", 422)
			return
		}
		if plan.Provider == "aws" {
			var credentials cloud.AWSCredentials
			if json.Unmarshal(plain, &credentials) != nil {
				http.Error(w, "invalid AWS credentials", 409)
				return
			}
			estimate := a.awsComputeQuote
			if estimate == nil {
				estimate = cloud.AWSComputeQuote
			}
			quote, err = estimate(ctx, plan.Region, plan.Spec.AWS.InstanceType, credentials)
		} else {
			var credentials cloud.GCPCredentials
			if json.Unmarshal(plain, &credentials) != nil {
				http.Error(w, "invalid GCP credentials", 409)
				return
			}
			estimate := a.gcpComputeQuote
			if estimate == nil {
				estimate = cloud.GCPComputeQuote
			}
			quote, err = estimate(ctx, plan.Region, plan.Spec.GCP.MachineType, credentials, plan.Inspection)
		}
	}
	if err != nil {
		http.Error(w, "verified base VM price unavailable; check pricing read permissions and connectivity; costs remain unknown", 422)
		return
	}
	if a.cloudOrg(w, r) == nil {
		return
	}
	a.cloudOperationMu.Lock()
	defer a.cloudOperationMu.Unlock()
	saved := false
	a.Store.Mutate(func() {
		p := a.Store.CloudReviewPlans[plan.ID]
		c := a.Store.CloudConnections[plan.ConnectionID]
		if p == nil || p.OrgID != org.ID || time.Now().After(p.ExpiresAt) || c == nil || c.OrgID != org.ID || c.AccountID != plan.AccountID || c.Provider != plan.Provider || c.Region != plan.Region {
			return
		}
		p.ComputeQuote = &quote
		a.recalculatePlanCosts(p)
		saved = true
	})
	if !saved {
		http.Error(w, "review or original connection no longer available", 409)
		return
	}
	if a.Store.persistOnce() != nil {
		http.Error(w, "could not save compute estimate", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, quote)
}
