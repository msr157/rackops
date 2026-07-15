package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rackops/rackops/internal/notify"
	"github.com/rackops/rackops/internal/types"
)

// handleGetSettings returns integrations config with secrets masked.
func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	a.Store.View(func() { s = a.Store.Settings })
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
		old := a.Store.Settings
		in.AnthropicKey = keep(in.AnthropicKey, old.AnthropicKey)
		in.GeminiKey = keep(in.GeminiKey, old.GeminiKey)
		in.OpenAIKey = keep(in.OpenAIKey, old.OpenAIKey)
		in.TelegramToken = keep(in.TelegramToken, old.TelegramToken)
		a.Store.Settings = in
	})
	a.Store.AddEvent("settings", "Integrations settings updated", "", "")
	w.WriteHeader(http.StatusNoContent)
}

// notifyTargets snapshots the configured channels.
func (a *API) notifyTargets() (notify.Targets, bool, bool) {
	var s Settings
	a.Store.View(func() { s = a.Store.Settings })
	return notify.Targets{
		DiscordWebhook: s.DiscordWebhook,
		SlackWebhook:   s.SlackWebhook,
		TelegramToken:  s.TelegramToken,
		TelegramChatID: s.TelegramChatID,
		GenericWebhook: s.GenericWebhook,
	}, s.NotifyDeploys, s.NotifyAlerts
}

// NotifyDeploy sends a deploy outcome notification if enabled.
func (a *API) NotifyDeploy(svc *types.Service, ok bool) {
	t, deploys, _ := a.notifyTargets()
	if !deploys {
		return
	}
	if ok {
		notify.Send(t, "✅ Deployed: "+svc.Name, "Service is running on node "+svc.NodeID+".", "info")
	} else {
		notify.Send(t, "❌ Deploy failed: "+svc.Name, "Check the service logs in the RackOps dashboard.", "failed")
	}
}

// NotifyAlert sends an alert notification if enabled.
func (a *API) NotifyAlert(al *types.Alert) {
	t, _, alerts := a.notifyTargets()
	if !alerts {
		return
	}
	notify.Send(t, "⚠ RackOps alert ("+string(al.Level)+")", al.Message, string(al.Level))
}
