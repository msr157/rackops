package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// runCLI implements `rackops <cmd>` API-client commands.
// Server and token come from flags or RACKOPS_SERVER / RACKOPS_TOKEN env.
func runCLI(cmd string, args []string) {
	server := os.Getenv("RACKOPS_SERVER")
	if server == "" {
		server = "http://localhost:8080"
	}
	token := os.Getenv("RACKOPS_TOKEN")

	// Extract --server/--token from args.
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server":
			i++
			if i < len(args) {
				server = args[i]
			}
		case "--token":
			i++
			if i < len(args) {
				token = args[i]
			}
		default:
			rest = append(rest, args[i])
		}
	}
	c := &cliClient{base: strings.TrimSuffix(server, "/"), token: token}

	switch cmd {
	case "nodes":
		var nodes []map[string]any
		c.get("/api/nodes", &nodes)
		fmt.Printf("%-14s %-8s %-16s %-10s %s\n", "NAME", "STATUS", "IP", "MESH", "CPU")
		for _, n := range nodes {
			hw, _ := n["hardware"].(map[string]any)
			fmt.Printf("%-14v %-8v %-16v %-10v %v\n", n["name"], n["status"], n["ip"], n["meshIp"], hw["cpuModel"])
		}
	case "services":
		var svcs []map[string]any
		c.get("/api/services", &svcs)
		fmt.Printf("%-18s %-10s %-14s %-6s %s\n", "NAME", "STATUS", "NODE", "PORT", "DOMAIN")
		for _, s := range svcs {
			fmt.Printf("%-18v %-10v %-14v %-6v %v\n", s["name"], s["status"], s["nodeId"], s["port"], s["domain"])
		}
	case "deploy":
		// rackops deploy --name x --image y [--port n] [--node id|auto]
		body := map[string]any{"nodeId": "auto", "kind": "app"}
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--name":
				i++
				body["name"] = rest[i]
			case "--image":
				i++
				body["image"] = rest[i]
			case "--template":
				i++
				body["templateId"] = rest[i]
			case "--port":
				i++
				var p int
				fmt.Sscanf(rest[i], "%d", &p)
				body["port"] = p
			case "--node":
				i++
				body["nodeId"] = rest[i]
			}
		}
		// Default project/env.
		var projects []map[string]any
		c.get("/api/projects", &projects)
		if len(projects) > 0 {
			pid, _ := projects[0]["id"].(string)
			body["projectId"] = pid
			var envs []map[string]any
			c.get("/api/projects/"+pid+"/envs", &envs)
			for _, e := range envs {
				if e["name"] == "production" {
					body["envId"] = e["id"]
				}
			}
		}
		var out map[string]any
		c.post("/api/services", body, &out)
		fmt.Printf("deploying %v (service %v) to node %v\n", out["name"], out["id"], out["nodeId"])
	case "alerts":
		var alerts []map[string]any
		c.get("/api/alerts", &alerts)
		if len(alerts) == 0 {
			fmt.Println("no open alerts")
			return
		}
		for _, a := range alerts {
			fmt.Printf("[%v] %v\n", a["level"], a["message"])
		}
	case "ask":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "usage: rackops ask \"question\"")
			os.Exit(1)
		}
		var out map[string]string
		c.post("/api/assistant", map[string]string{"message": strings.Join(rest, " ")}, &out)
		fmt.Println(out["answer"])
		fmt.Println("\n(provider: " + out["provider"] + ")")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		os.Exit(1)
	}
}

type cliClient struct{ base, token string }

func (c *cliClient) do(method, path string, in, out any) {
	var body io.Reader
	if in != nil {
		data, _ := json.Marshal(in)
		body = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, c.base+path, body)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "error: HTTP %d: %s\n", resp.StatusCode, strings.TrimSpace(string(data)))
		os.Exit(1)
	}
	if out != nil {
		_ = json.Unmarshal(data, out)
	}
}

func (c *cliClient) get(path string, out any)      { c.do("GET", path, nil, out) }
func (c *cliClient) post(path string, in, out any) { c.do("POST", path, in, out) }
