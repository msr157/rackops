package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// handleAssistant answers operator questions grounded in live fleet state.
// Providers: anthropic | gemini | openai (incl. Ollama-compatible) — or the
// built-in rule engine when no key is configured.
func (a *API) handleAssistant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}

	ctxJSON := a.fleetContext()
	var s Settings
	a.Store.View(func() { s = a.Store.Settings })

	provider := s.AIProvider
	if provider == "" || provider == "auto" {
		switch {
		case s.AnthropicKey != "":
			provider = "anthropic"
		case s.GeminiKey != "":
			provider = "gemini"
		case s.OpenAIKey != "" || s.OpenAIBase != "":
			provider = "openai"
		default:
			provider = "builtin"
		}
	}

	system := "You are the RackOps AI Co-Pilot, an infrastructure operations assistant. " +
		"You have the live fleet state below. Answer concisely and concretely; use numbers from the data. " +
		"When asked where to run a workload, reason from CPU/memory/thermal headroom.\n\nFLEET STATE:\n" + ctxJSON

	var answer string
	var err error
	switch provider {
	case "anthropic":
		answer, err = askAnthropic(s.AnthropicKey, s.AnthropicModel, system, req.Message)
	case "gemini":
		answer, err = askGemini(s.GeminiKey, s.GeminiModel, system, req.Message)
	case "openai":
		answer, err = askOpenAI(s.OpenAIKey, s.OpenAIBase, s.OpenAIModel, system, req.Message)
	default:
		answer = a.builtinAssistant(req.Message)
	}
	if err != nil {
		// Fall back to the rule engine rather than failing the chat.
		answer = a.builtinAssistant(req.Message) +
			"\n\n_(" + provider + " provider error: " + err.Error() + " — answered by the built-in engine)_"
		provider = "builtin(fallback)"
	}
	writeJSON(w, map[string]string{"answer": answer, "provider": provider})
}

// handleAssistantTest sends a trivial prompt through the configured provider
// so users can validate keys from Settings without a chat round-trip.
func (a *API) handleAssistantTest(w http.ResponseWriter, r *http.Request) {
	var s Settings
	a.Store.View(func() { s = a.Store.Settings })
	provider := s.AIProvider
	if provider == "" || provider == "auto" {
		switch {
		case s.AnthropicKey != "":
			provider = "anthropic"
		case s.GeminiKey != "":
			provider = "gemini"
		case s.OpenAIKey != "" || s.OpenAIBase != "":
			provider = "openai"
		default:
			provider = "builtin"
		}
	}
	var err error
	switch provider {
	case "anthropic":
		_, err = askAnthropic(s.AnthropicKey, s.AnthropicModel, "You are a health check.", "Reply with the single word: OK")
	case "gemini":
		_, err = askGemini(s.GeminiKey, s.GeminiModel, "You are a health check.", "Reply with the single word: OK")
	case "openai":
		_, err = askOpenAI(s.OpenAIKey, s.OpenAIBase, s.OpenAIModel, "You are a health check.", "Reply with the single word: OK")
	default:
		writeJSON(w, map[string]any{"ok": true, "provider": "builtin", "note": "rule engine active — no key needed"})
		return
	}
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "provider": provider, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "provider": provider})
}

// fleetContext renders a compact JSON snapshot for grounding.
func (a *API) fleetContext() string {
	type nodeCtx struct {
		Name   string   `json:"name"`
		Status string   `json:"status"`
		CPU    string   `json:"cpuModel"`
		CPUPct float64  `json:"cpuPct"`
		MemPct float64  `json:"memPct"`
		TempC  float64  `json:"tempC"`
		GPUs   []string `json:"gpus,omitempty"`
		Drives []string `json:"drives,omitempty"`
	}
	type svcCtx struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Node   string `json:"node"`
		Port   int    `json:"port,omitempty"`
		Domain string `json:"domain,omitempty"`
	}
	var nodes []nodeCtx
	var svcs []svcCtx
	var alerts []string
	a.Store.View(func() {
		for _, n := range a.Store.Nodes {
			nc := nodeCtx{
				Name: n.Name, Status: string(n.Status), CPU: n.Hardware.CPUModel,
				CPUPct: n.Metrics.CPUPct, MemPct: n.Metrics.MemPct, TempC: n.Metrics.TempC,
			}
			for _, g := range n.Hardware.GPUs {
				nc.GPUs = append(nc.GPUs, fmt.Sprintf("%s (%d/%d MB VRAM, %.0f%%, %.0fC)", g.Model, g.VRAMUsed, g.VRAMTotal, g.UtilPct, g.TempC))
			}
			for _, d := range n.Hardware.Drives {
				nc.Drives = append(nc.Drives, fmt.Sprintf("%s %s %.0f/%.0fGB %s", d.Device, d.Type, d.UsedGB, d.SizeGB, d.Health))
			}
			nodes = append(nodes, nc)
		}
		for _, s := range a.Store.Services {
			node := s.NodeID
			if n := a.Store.Nodes[s.NodeID]; n != nil {
				node = n.Name
			}
			svcs = append(svcs, svcCtx{Name: s.Name, Status: string(s.Status), Node: node, Port: s.Port, Domain: s.Domain})
		}
		for _, al := range a.Store.Alerts {
			if !al.Resolved {
				alerts = append(alerts, string(al.Level)+": "+al.Message)
			}
		}
	})
	out, _ := json.Marshal(map[string]any{"nodes": nodes, "services": svcs, "openAlerts": alerts})
	return string(out)
}

// builtinAssistant is the zero-config rule engine.
func (a *API) builtinAssistant(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "where") && (strings.Contains(m, "deploy") || strings.Contains(m, "run") || strings.Contains(m, "place")):
		scores := a.Store.PlaceWorkload(types.PlacementRequest{})
		if len(scores) == 0 || !scores[0].Viable {
			return "No viable node right now — check the Alerts page."
		}
		var b strings.Builder
		b.WriteString("Placement ranking (best first):\n")
		for _, sc := range scores {
			name := sc.NodeID
			a.Store.View(func() {
				if n := a.Store.Nodes[sc.NodeID]; n != nil {
					name = n.Name
				}
			})
			b.WriteString(fmt.Sprintf("- **%s** — score %.0f (%s)\n", name, sc.Score, strings.Join(sc.Reasons, ", ")))
		}
		return b.String()
	case strings.Contains(m, "hot") || strings.Contains(m, "temp") || strings.Contains(m, "thermal"):
		var b strings.Builder
		b.WriteString("Thermal state:\n")
		a.Store.View(func() {
			var ns []*types.Node
			for _, n := range a.Store.Nodes {
				ns = append(ns, n)
			}
			sort.Slice(ns, func(i, j int) bool { return ns[i].Metrics.TempC > ns[j].Metrics.TempC })
			for _, n := range ns {
				verdict := "healthy"
				if n.Metrics.TempC > 85 {
					verdict = "CRITICAL — throttling likely; check airflow/dust and consider migrating workloads"
				} else if n.Metrics.TempC > 70 {
					verdict = "warm — fine under load, watch the trend"
				}
				b.WriteString(fmt.Sprintf("- **%s**: %.0f°C package (%s)\n", n.Name, n.Metrics.TempC, verdict))
				for _, g := range n.Hardware.GPUs {
					b.WriteString(fmt.Sprintf("  - GPU %s: %.0f°C\n", g.Model, g.TempC))
				}
			}
		})
		return b.String()
	case strings.Contains(m, "why") && strings.Contains(m, "fail"):
		var failed []string
		a.Store.View(func() {
			for _, s := range a.Store.Services {
				if s.Status == types.StatusFailed {
					failed = append(failed, s.Name+" (id "+s.ID+")")
				}
			}
		})
		if len(failed) == 0 {
			return "No failed services right now. If a deploy just failed, open its logs (📜) — the build/run output shows the exact error."
		}
		return "Failed services: " + strings.Join(failed, ", ") + ". Open each service's logs (📜 button) for the failing command output; common causes are port conflicts, missing env vars, and image architecture mismatches."
	case strings.Contains(m, "disk") || strings.Contains(m, "storage") || strings.Contains(m, "full"):
		var b strings.Builder
		b.WriteString("Storage state:\n")
		a.Store.View(func() {
			for _, n := range a.Store.Nodes {
				for _, d := range n.Hardware.Drives {
					pct := 0.0
					if d.SizeGB > 0 {
						pct = d.UsedGB / d.SizeGB * 100
					}
					b.WriteString(fmt.Sprintf("- %s %s: %.0f%% used (%.0f/%.0f GB, health %s)\n", n.Name, d.Device, pct, d.UsedGB, d.SizeGB, d.Health))
				}
			}
		})
		return b.String()
	default:
		o := ""
		a.Store.View(func() {
			online, total := 0, 0
			for _, n := range a.Store.Nodes {
				total++
				if n.Status == types.NodeOnline {
					online++
				}
			}
			up := 0
			for _, s := range a.Store.Services {
				if s.Status == types.StatusRunning {
					up++
				}
			}
			openAlerts := 0
			for _, al := range a.Store.Alerts {
				if !al.Resolved {
					openAlerts++
				}
			}
			o = fmt.Sprintf("Fleet: %d/%d nodes online, %d services running, %d open alerts.", online, total, up, openAlerts)
		})
		return o + "\n\nI can answer: *where should I deploy X* · *why is a node hot* · *what's failing* · *storage state*. Add an AI provider key in Settings for full natural-language operations."
	}
}

// ---- Providers ----

func askAnthropic(key, model, system, msg string) (string, error) {
	if model == "" {
		model = "claude-opus-4-8"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": 1024, "system": system,
		"messages": []map[string]string{{"role": "user", "content": msg}},
	})
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := doJSON(req, &out); err != nil {
		return "", err
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return out.Content[0].Text, nil
}

func askGemini(key, model, system, msg string) (string, error) {
	if model == "" {
		model = "gemini-3.5-flash"
	}
	body, _ := json.Marshal(map[string]any{
		"system_instruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":           []map[string]any{{"parts": []map[string]string{{"text": msg}}}},
	})
	u := "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent?key=" + key
	req, _ := http.NewRequest("POST", u, bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := doJSON(req, &out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

func askOpenAI(key, base, model, system, msg string) (string, error) {
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-5.5"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": msg},
		},
	})
	req, _ := http.NewRequest("POST", strings.TrimSuffix(base, "/")+"/chat/completions", bytes.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("content-type", "application/json")
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := doJSON(req, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return out.Choices[0].Message.Content, nil
}

func doJSON(req *http.Request, out any) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		s := string(data)
		if len(s) > 300 {
			s = s[:300]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, s)
	}
	return json.Unmarshal(data, out)
}
