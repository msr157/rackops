package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
)

// runMCP serves the Model Context Protocol over stdio, so AI tools (VS Code
// Copilot/Claude, Cursor, Claude Desktop…) can manage a RackOps fleet:
//
//	{ "mcpServers": { "rackops": {
//	    "command": "rackops", "args": ["mcp"],
//	    "env": { "RACKOPS_SERVER": "http://your-server:8080",
//	             "RACKOPS_TOKEN": "rk_…" } } } }
//
// The API key comes from the dashboard (Profile → API keys) and acts with its
// owner's role. Transport is newline-delimited JSON-RPC 2.0 (MCP 2024-11-05).
func runMCP(args []string) {
	serverURL := os.Getenv("RACKOPS_SERVER")
	token := os.Getenv("RACKOPS_TOKEN")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server":
			i++
			if i < len(args) {
				serverURL = args[i]
			}
		case "--token":
			i++
			if i < len(args) {
				token = args[i]
			}
		}
	}
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	c := &cliClient{base: strings.TrimSuffix(serverURL, "/"), token: token}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	out := json.NewEncoder(os.Stdout)

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		// Notifications (no id) need no response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		reply := func(result any) {
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
		replyErr := func(code int, msg string) {
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}})
		}

		switch req.Method {
		case "initialize":
			reply(map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "rackops", "version": version},
			})
		case "ping":
			reply(map[string]any{})
		case "tools/list":
			reply(map[string]any{"tools": mcpTools()})
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				replyErr(-32602, "invalid params: "+err.Error())
				continue
			}
			text, err := mcpCall(c, p.Name, p.Arguments)
			if err != nil {
				reply(map[string]any{
					"content": []map[string]any{{"type": "text", "text": "Error: " + err.Error()}},
					"isError": true,
				})
				continue
			}
			reply(map[string]any{"content": []map[string]any{{"type": "text", "text": text}}})
		default:
			replyErr(-32601, "method not found: "+req.Method)
		}
	}
}

// mcpTools declares the tool surface: read fleet state, deploy, lifecycle,
// logs. Destructive remove is exposed but requires an explicit confirm flag.
func mcpTools() []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	integer := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	boolean := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

	return []map[string]any{
		{
			"name":        "list_machines",
			"description": "List every machine in the RackOps fleet with status, IPs, hardware, and live CPU/memory metrics.",
			"inputSchema": obj(map[string]any{}),
		},
		{
			"name":        "list_apps",
			"description": "List all deployed apps/services: status, image, machine, ports, and public URLs.",
			"inputSchema": obj(map[string]any{}),
		},
		{
			"name":        "get_app",
			"description": "Get one app's full configuration and status by name or ID. Secret env values are redacted.",
			"inputSchema": obj(map[string]any{"app": str("App name or service ID")}, "app"),
		},
		{
			"name":        "deploy_app",
			"description": "Deploy a new app from a Docker image. RackOps picks the best machine automatically unless node is set.",
			"inputSchema": obj(map[string]any{
				"name":  str("Name for the new app"),
				"image": str("Docker image, e.g. nginx:alpine"),
				"port":  integer("Port the app listens on inside the container"),
				"node":  str("Target machine name/ID (omit for automatic placement)"),
			}, "name", "image"),
		},
		{
			"name":        "app_action",
			"description": "Start, stop, restart, or redeploy an app. Note: adopted apps (containers RackOps discovered rather than created) can't redeploy — restart them instead.",
			"inputSchema": obj(map[string]any{
				"app":    str("App name or service ID"),
				"action": str("One of: start | stop | restart | redeploy"),
			}, "app", "action"),
		},
		{
			"name":        "app_logs",
			"description": "Fetch an app's recent runtime logs from its machine.",
			"inputSchema": obj(map[string]any{"app": str("App name or service ID")}, "app"),
		},
		{
			"name":        "remove_app",
			"description": "Remove an app and delete its container (managed volume data stays on disk). Requires confirm=true.",
			"inputSchema": obj(map[string]any{
				"app":     str("App name or service ID"),
				"confirm": boolean("Must be true — this deletes the container"),
			}, "app", "confirm"),
		},
		{
			"name":        "open_alerts",
			"description": "List currently open alerts (node down, disk full, thermal, failed deploys…).",
			"inputSchema": obj(map[string]any{}),
		},
	}
}

// findService resolves an app by exact ID, exact name, then unique prefix.
func findService(c *cliClient, ref string) (map[string]any, error) {
	var svcs []map[string]any
	if err := c.getE("/api/services", &svcs); err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	lower := strings.ToLower(ref)
	var byName, byPrefix []map[string]any
	for _, s := range svcs {
		id, _ := s["id"].(string)
		name, _ := s["name"].(string)
		if id == ref {
			return s, nil
		}
		if strings.ToLower(name) == lower {
			byName = append(byName, s)
		} else if strings.HasPrefix(strings.ToLower(name), lower) {
			byPrefix = append(byPrefix, s)
		}
	}
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) == 0 && len(byPrefix) == 1 {
		return byPrefix[0], nil
	}
	if len(byName)+len(byPrefix) > 1 {
		var names []string
		for _, s := range append(byName, byPrefix...) {
			names = append(names, fmt.Sprintf("%v (%v)", s["name"], s["id"]))
		}
		return nil, fmt.Errorf("%q matches several apps: %s — use the ID", ref, strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("no app found matching %q", ref)
}

// sanitizeService strips secret material before a service lands in an AI
// context window: env values marked secret are redacted, git tokens dropped.
func sanitizeService(svc map[string]any) map[string]any {
	out := make(map[string]any, len(svc))
	maps.Copy(out, svc)
	delete(out, "gitToken")
	if envs, ok := out["env"].([]any); ok {
		cleaned := make([]any, 0, len(envs))
		for _, e := range envs {
			ev, ok := e.(map[string]any)
			if !ok {
				cleaned = append(cleaned, e)
				continue
			}
			if secret, _ := ev["secret"].(bool); secret {
				ev = map[string]any{"key": ev["key"], "value": "[redacted]", "secret": true}
			}
			cleaned = append(cleaned, ev)
		}
		out["env"] = cleaned
	}
	return out
}

func mcpCall(c *cliClient, name string, rawArgs json.RawMessage) (string, error) {
	var args map[string]any
	if len(rawArgs) > 0 {
		_ = json.Unmarshal(rawArgs, &args)
	}
	argStr := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }

	pretty := func(v any) string {
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}

	switch name {
	case "list_machines":
		var nodes []map[string]any
		if err := c.getE("/api/nodes", &nodes); err != nil {
			return "", err
		}
		out := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			hw, _ := n["hardware"].(map[string]any)
			m, _ := n["metrics"].(map[string]any)
			out = append(out, map[string]any{
				"id": n["id"], "name": n["name"], "status": n["status"],
				"ip": n["ip"], "publicIp": n["publicIp"],
				"cpu": hw["cpuModel"], "cores": hw["cpuCores"], "memTotalMb": hw["memTotalMb"],
				"cpuPct": m["cpuPct"], "memPct": m["memPct"], "containers": m["containers"],
			})
		}
		return pretty(out), nil

	case "list_apps":
		var svcs []map[string]any
		if err := c.getE("/api/services", &svcs); err != nil {
			return "", err
		}
		out := make([]map[string]any, 0, len(svcs))
		for _, s := range svcs {
			out = append(out, map[string]any{
				"id": s["id"], "name": s["name"], "status": s["status"],
				"image": s["image"], "node": s["nodeId"], "port": s["port"],
				"domain": s["domain"], "adopted": s["adopted"],
			})
		}
		return pretty(out), nil

	case "get_app":
		svc, err := findService(c, argStr("app"))
		if err != nil {
			return "", err
		}
		return pretty(sanitizeService(svc)), nil

	case "deploy_app":
		if argStr("name") == "" || argStr("image") == "" {
			return "", fmt.Errorf("name and image are required")
		}
		body := map[string]any{
			"name": argStr("name"), "image": argStr("image"), "kind": "app", "nodeId": "auto",
		}
		if node := argStr("node"); node != "" {
			body["nodeId"] = node
		}
		if p, ok := args["port"].(float64); ok && p > 0 {
			body["port"] = int(p)
		}
		var out map[string]any
		if err := c.postE("/api/services", body, &out); err != nil {
			return "", err
		}
		return fmt.Sprintf("Deploying %v (service %v) to machine %v. Check status with get_app.", out["name"], out["id"], out["nodeId"]), nil

	case "app_action":
		action := argStr("action")
		switch action {
		case "start", "stop", "restart", "redeploy":
		default:
			return "", fmt.Errorf("action must be start, stop, restart, or redeploy")
		}
		svc, err := findService(c, argStr("app"))
		if err != nil {
			return "", err
		}
		var out map[string]any
		if err := c.postE(fmt.Sprintf("/api/services/%v/%s", svc["id"], action), map[string]any{}, &out); err != nil {
			return "", err
		}
		return fmt.Sprintf("%v: %s requested (status now %v).", svc["name"], action, out["status"]), nil

	case "app_logs":
		svc, err := findService(c, argStr("app"))
		if err != nil {
			return "", err
		}
		var out map[string]string
		if err := c.getE(fmt.Sprintf("/api/services/%v/logs", svc["id"]), &out); err != nil {
			return "", err
		}
		logs := out["logs"]
		if strings.TrimSpace(logs) == "" {
			logs = "(no output)"
		}
		return logs, nil

	case "remove_app":
		if ok, _ := args["confirm"].(bool); !ok {
			return "", fmt.Errorf("refusing to remove without confirm=true")
		}
		svc, err := findService(c, argStr("app"))
		if err != nil {
			return "", err
		}
		if err := c.deleteE(fmt.Sprintf("/api/services/%v", svc["id"])); err != nil {
			return "", err
		}
		return fmt.Sprintf("%v removed.", svc["name"]), nil

	case "open_alerts":
		var alerts []map[string]any
		if err := c.getE("/api/alerts", &alerts); err != nil {
			return "", err
		}
		if len(alerts) == 0 {
			return "No open alerts — everything looks healthy.", nil
		}
		return pretty(alerts), nil
	}
	return "", fmt.Errorf("unknown tool %q", name)
}
