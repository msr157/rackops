package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rackops/rackops/internal/notify"
	"github.com/rackops/rackops/internal/types"
)

// Settings are org-scoped: each organization configures its own notification
// channels and Copilot provider. Handlers resolve the active org first.

// handleGetSettings returns the active org's integrations config, secrets masked.
func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	s := a.Store.SettingsFor(org.ID)
	mask := func(v string) string {
		if len(v) > 8 {
			return v[:4] + "••••"
		}
		if v != "" {
			return "••••"
		}
		return ""
	}
	out := s
	out.AnthropicKey = mask(s.AnthropicKey)
	out.GeminiKey = mask(s.GeminiKey)
	out.OpenAIKey = mask(s.OpenAIKey)
	out.TelegramToken = mask(s.TelegramToken)
	writeJSON(w, out)
}

// handleSetSettings merges new values; masked/empty secret fields keep the old value.
func (a *API) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	if member.Role == types.OrgRoleMember {
		http.Error(w, "organization admin access required to change settings", http.StatusForbidden)
		return
	}
	var in Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	keep := func(newV, oldV string) string {
		if newV == "" || strings.Contains(newV, "••••") {
			return oldV
		}
		return newV
	}
	a.Store.Mutate(func() {
		if a.Store.OrgSettings == nil {
			a.Store.OrgSettings = map[string]*Settings{}
		}
		old := a.Store.OrgSettings[org.ID]
		if old == nil {
			old = &Settings{}
		}
		in.AnthropicKey = keep(in.AnthropicKey, old.AnthropicKey)
		in.GeminiKey = keep(in.GeminiKey, old.GeminiKey)
		in.OpenAIKey = keep(in.OpenAIKey, old.OpenAIKey)
		in.TelegramToken = keep(in.TelegramToken, old.TelegramToken)
		a.Store.OrgSettings[org.ID] = &in
	})
	a.Store.AddEvent("settings", "Integrations settings updated for "+org.Name, "", "")
	w.WriteHeader(http.StatusNoContent)
}

// notifyTargets snapshots one org's configured channels.
func (a *API) notifyTargets(orgID string) (notify.Targets, bool, bool) {
	s := a.Store.SettingsFor(orgID)
	return notify.Targets{
		DiscordWebhook: s.DiscordWebhook,
		SlackWebhook:   s.SlackWebhook,
		TelegramToken:  s.TelegramToken,
		TelegramChatID: s.TelegramChatID,
		GenericWebhook: s.GenericWebhook,
	}, s.NotifyDeploys, s.NotifyAlerts
}

// orgIDForService resolves the org a service's notifications go to.
func (a *API) orgIDForService(svc *types.Service) string {
	id := ""
	a.Store.View(func() {
		if p := a.Store.Projects[svc.ProjectID]; p != nil {
			id = p.OrgID
		} else if n := a.Store.Nodes[svc.NodeID]; n != nil {
			id = n.OrgID
		}
	})
	return id
}

// NotifyDeploy sends a deploy outcome notification to the owning org if enabled.
func (a *API) NotifyDeploy(svc *types.Service, ok bool) {
	t, deploys, _ := a.notifyTargets(a.orgIDForService(svc))
	if !deploys {
		return
	}
	if ok {
		notify.Send(t, "✅ Deployed: "+svc.Name, "Service is running on node "+svc.NodeID+".", "info")
	} else {
		notify.Send(t, "❌ Deploy failed: "+svc.Name, "Check the service logs in the RackOps dashboard.", "failed")
	}
}

// NotifyAlert sends an alert notification to the affected node's org if enabled.
func (a *API) NotifyAlert(al *types.Alert) {
	orgID := ""
	a.Store.View(func() {
		if n := a.Store.Nodes[al.NodeID]; n != nil {
			orgID = n.OrgID
		}
	})
	t, _, alerts := a.notifyTargets(orgID)
	if !alerts {
		return
	}
	notify.Send(t, "⚠ RackOps alert ("+string(al.Level)+")", al.Message, string(al.Level))
}
