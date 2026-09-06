package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rackops/rackops/internal/types"
)

func (a *API) eventVisible(r *http.Request, orgID string, message []byte) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.Header.Get("X-Api-Key")
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	var claims *jwtClaims
	if strings.HasPrefix(token, "rk_") {
		claims = a.claimsForAPIKey(token)
	} else {
		claims, _ = parseJWT(a.Store.SessionSecret(), token)
	}
	if claims == nil {
		return false
	}
	parts := bytes.SplitN(message, []byte("\ndata: "), 2)
	if len(parts) != 2 {
		return false
	}
	event := strings.TrimPrefix(string(parts[0]), "event: ")
	var resource struct {
		ID        string `json:"id"`
		ServiceID string `json:"serviceId"`
		OrgID     string `json:"orgId"`
	}
	if json.Unmarshal(bytes.TrimSpace(parts[1]), &resource) != nil {
		return false
	}
	allowed := false
	a.Store.View(func() {
		user, org := a.Store.Users[claims.Sub], a.Store.Orgs[orgID]
		if user == nil || org == nil || user.SessionVersion != claims.Version {
			return
		}
		admin := user.Role == types.RoleOwner || user.Role == types.RoleAdmin
		member := org.Member(user.ID)
		if member == nil && admin {
			member = &types.OrgMember{Role: types.OrgRoleAdmin}
		}
		if member == nil {
			return
		}
		switch event {
		case "log_chunk":
			svc := a.Store.Services[resource.ServiceID]
			allowed = svc != nil && a.serviceInOrg(svc, orgID) && projectVisible(member, svc.ProjectID)
		case "ticket_update":
			ticket := a.Store.Tickets[resource.ID]
			allowed = ticket != nil && (admin || ticket.CreatedBy == user.ID)
		case "domain_update":
			domain := a.Store.Domains[resource.ID]
			allowed = domain != nil && domain.OrgID == orgID
		case "alert", "alert_resolved":
			alert := a.Store.Alerts[resource.ID]
			if alert != nil && a.alertInOrg(alert, orgID) {
				allowed = true
				if alert.ServiceID != "" {
					svc := a.Store.Services[alert.ServiceID]
					allowed = svc != nil && projectVisible(member, svc.ProjectID)
				}
			}
		case "node_joined", "node_metrics":
			node := a.Store.Nodes[resource.ID]
			allowed = node != nil && node.OrgID == orgID
		case "services_changed":
			allowed = true // contains no resource data
		case "ingress_update", "frontdoor_update":
			allowed = admin
		}
	})
	return allowed
}
