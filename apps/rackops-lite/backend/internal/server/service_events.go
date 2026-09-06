package server

import "github.com/rackops/rackops/internal/types"

// Service notifications contain no configuration or credentials. Consumers
// reload through the authenticated, project-filtered API. The generic org
// invalidation carries no service ID, so project-restricted viewers cannot
// enumerate services they cannot otherwise read.
func (a *API) serviceChanged(svc *types.Service) {
	if svc == nil {
		return
	}
	var orgID string
	a.Store.View(func() {
		if p := a.Store.Projects[svc.ProjectID]; p != nil {
			orgID = p.OrgID
		}
		if orgID == "" {
			if n := a.Store.Nodes[svc.NodeID]; n != nil {
				orgID = n.OrgID
			}
		}
	})
	if orgID != "" {
		a.Hub.BroadcastOrg(orgID, "services_changed", struct{}{})
	}
}
