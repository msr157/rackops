package server

import (
	"net/http"

	"github.com/rackops/rackops/internal/types"
)

// CreateServices is the existing deploy capability. Editing configuration or
// executing a lifecycle action carries the same privilege as deploying code.
func requireServiceControl(w http.ResponseWriter, member *types.OrgMember) bool {
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.CreateServices }) {
		http.Error(w, "app deployment permission is required to control this app", http.StatusForbidden)
		return false
	}
	return true
}

func (a *API) serviceForNetworking(w http.ResponseWriter, r *http.Request) *types.Service {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil {
		return nil
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageDomains }) {
		http.Error(w, "domain management permission is required to change public access", http.StatusForbidden)
		return nil
	}
	return svc
}
