package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/rackops/rackops/internal/cloud"
)

// A review snapshot, not an approved launch. Credentials and bootstrap grants
// never enter a plan. Unknown costs are represented as null, never zero.
type CloudReviewPlan struct {
	ID              string                  `json:"id"`
	OrgID           string                  `json:"orgId"`
	ConnectionID    string                  `json:"connectionId"`
	Provider        string                  `json:"provider"`
	AccountID       string                  `json:"accountId"`
	Region          string                  `json:"region"`
	Architecture    string                  `json:"architecture"`
	Spec            cloud.MachineSpec       `json:"spec"`
	Inspection      cloud.MachineInspection `json:"inspection"`
	CreatedAt       time.Time               `json:"createdAt"`
	ExpiresAt       time.Time               `json:"expiresAt"`
	Status          string                  `json:"status"`
	CanApprove      bool                    `json:"canApprove"`
	MonthlyCost     *float64                `json:"monthlyCost"`
	ComputeQuote    *cloud.ComputeQuote     `json:"computeQuote,omitempty"`
	DiskQuote       *cloud.DiskQuote        `json:"diskQuote,omitempty"`
	TransferQuote   *cloud.TransferQuote    `json:"transferQuote,omitempty"`
	TransferCost    *float64                `json:"transferCost"`
	DataSizeBytes   *int64                  `json:"dataSizeBytes,omitempty"`
	DowntimeSeconds *int64                  `json:"downtimeSeconds"`
	Blockers        []string                `json:"blockers"`
}

func (a *API) handleCreateCloudPlan(w http.ResponseWriter, r *http.Request) {
	a.handleCloudMachineReview(w, r, true)
}

func (a *API) handleCloudPlans(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	plans := []CloudReviewPlan{}
	a.Store.View(func() {
		for _, p := range a.Store.CloudReviewPlans {
			if p.OrgID != org.ID || (r.PathValue("id") != "" && p.ID != r.PathValue("id")) {
				continue
			}
			copy := *p
			if time.Now().After(copy.ExpiresAt) {
				copy.Status = "expired"
				copy.CanApprove = false
			}
			c := a.Store.CloudConnections[p.ConnectionID]
			if c == nil || c.OrgID != p.OrgID || c.AccountID != p.AccountID || c.Provider != p.Provider || c.Region != p.Region {
				copy.Status = "connection_unavailable"
				copy.CanApprove = false
			}
			plans = append(plans, copy)
		}
	})
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("id") != "" {
		if len(plans) == 0 {
			http.Error(w, "cloud plan not found", 404)
			return
		}
		writeJSON(w, plans[0])
		return
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].CreatedAt.After(plans[j].CreatedAt) })
	writeJSON(w, plans)
}
