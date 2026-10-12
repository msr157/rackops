package server

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/rackops/rackops/internal/cloud"
)

func (a *API) recalculatePlanCosts(p *CloudReviewPlan) {
	if p.ComputeQuote != nil && p.DiskQuote != nil && p.TransferQuote != nil {
		total := math.Round((p.ComputeQuote.Monthly + p.DiskQuote.Monthly + p.TransferQuote.Monthly)*100) / 100
		p.MonthlyCost = &total
		cost := p.TransferQuote.Monthly
		p.TransferCost = &cost
		if len(p.Inspection.Blockers) == 0 && time.Now().Before(p.ExpiresAt) {
			p.Status = "ready_for_approval"
			p.CanApprove = true
			p.Blockers = nil
		}
	} else {
		p.MonthlyCost = nil
		p.CanApprove = false
	}
}

func (a *API) handleCloudTransferQuote(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "cloud review not found", http.StatusNotFound)
		return
	}
	if time.Now().After(plan.ExpiresAt) {
		http.Error(w, "review expired; create a fresh resource review", http.StatusConflict)
		return
	}

	var req struct {
		TransferGB    *int64 `json:"transferGb"`
		DataSizeBytes *int64 `json:"dataSizeBytes"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	}

	var dataBytes int64 = 50 * 1024 * 1024 * 1024 // 50 GiB default baseline
	if req.DataSizeBytes != nil && *req.DataSizeBytes > 0 {
		dataBytes = *req.DataSizeBytes
	}
	var transferGB int64 = int64(math.Ceil(float64(dataBytes) / (1024 * 1024 * 1024)))
	if req.TransferGB != nil && *req.TransferGB >= 0 {
		transferGB = *req.TransferGB
		dataBytes = transferGB * 1024 * 1024 * 1024
	}

	var quote cloud.TransferQuote
	var err error
	switch plan.Provider {
	case "aws":
		quote, err = cloud.AWSTransferQuote(plan.Region, transferGB)
	case "azure":
		quote, err = cloud.AzureTransferQuote(plan.Region, transferGB)
	case "gcp":
		quote, err = cloud.GCPTransferQuote(plan.Region, transferGB)
	default:
		http.Error(w, "transfer pricing is not supported for this provider", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "transfer quote calculation failed: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	// Estimate downtime (transfer at ~25 MB/s, restore at ~30 MB/s + 30s buffer)
	secTransfer := dataBytes / (25 * 1024 * 1024)
	secRestore := dataBytes / (30 * 1024 * 1024)
	estimatedDowntime := secTransfer + secRestore + 30
	if estimatedDowntime < 60 {
		estimatedDowntime = 60
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
		p.TransferQuote = &quote
		cost := quote.Monthly
		p.TransferCost = &cost
		p.DataSizeBytes = &dataBytes
		p.DowntimeSeconds = &estimatedDowntime
		a.recalculatePlanCosts(p)
		saved = true
	})
	if !saved {
		http.Error(w, "review or connection no longer available", http.StatusConflict)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not save transfer estimate", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, quote)
}
