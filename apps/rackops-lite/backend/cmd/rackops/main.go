// Command rackops is the single RackOps binary: control-plane server, node
// agent, and CLI in one executable.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/rackops/rackops/internal/agent"
	"github.com/rackops/rackops/internal/raftstore"
	"github.com/rackops/rackops/internal/server"
	"github.com/rackops/rackops/internal/telemetry"
)

const version = "0.5.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "server":
		runServer(os.Args[2:])
	case "agent":
		runAgent(os.Args[2:])
	case "probe":
		runProbe()
	case "update":
		runUpdate()
	case "mcp":
		runMCP(os.Args[2:])
	case "nodes", "services", "deploy", "alerts", "ask":
		runCLI(os.Args[1], os.Args[2:])
	case "version":
		fmt.Println("rackops", version)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `RackOps — The Infrastructure Operating System

Usage:
  rackops server [flags]     Run the control plane (+ embedded local agent)
  rackops agent  [flags]     Run the node agent
  rackops probe              Print discovered hardware + live metrics (debug)
  rackops update             Self-update to the latest release
  rackops mcp                Serve the Model Context Protocol over stdio (AI tools)
  rackops version            Print version

CLI (uses RACKOPS_SERVER / RACKOPS_TOKEN env or --server/--token flags):
  rackops nodes              List fleet nodes
  rackops services           List services
  rackops deploy --name x --image y [--port n] [--node auto]
  rackops alerts             Show open alerts
  rackops ask "question"     Ask the AI Co-Pilot

Server flags:
  --listen :8080             Address to listen on
  --data-dir ~/.rackops      State directory
  --no-local-agent           Don't manage this machine itself
  --name <hostname>          Display name of this node

Agent flags:
  --server http://host:8080  Control plane URL (required)
  --token <token>            Join token (required)
  --name <hostname>          Display name of this node`)
}

// runUpdate self-updates the binary in place to the latest GitHub release —
// re-running the install one-liner is never required.
func runUpdate() {
	fmt.Printf("rackops %s — checking for updates…\n", version)
	tag, _, err := server.LatestRelease()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: couldn't reach GitHub releases:", err)
		os.Exit(1)
	}
	cur := "v" + version
	if tag == cur {
		fmt.Printf("already up to date (%s)\n", tag)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("updating %s → %s …\n", cur, tag)
	if err := server.SelfUpdate(exe, tag); err != nil {
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			fmt.Fprintf(os.Stderr, "error: %v\nre-run with sudo: sudo rackops update\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
	fmt.Printf("✓ updated to %s (%s)\n", tag, exe)
	if runtime.GOOS == "darwin" {
		fmt.Println("restart the server to pick it up: sudo launchctl kickstart -k system/in.rackops.server")
	} else {
		fmt.Println("restart the server to pick it up: sudo systemctl restart rackops")
	}
}

// runServer starts the control plane and, by default, an embedded local
// agent so the server's own machine is part of the fleet immediately.
func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "listen address")
	dataDir := fs.String("data-dir", defaultDataDir(), "state directory")
	noLocal := fs.Bool("no-local-agent", false, "disable the embedded local agent")
	name := fs.String("name", "", "node display name")
	meshPort := fs.Int("mesh-port", 51820, "WireGuard UDP port (0 disables mesh)")
	advertise := fs.String("advertise", "", "address agents reach this server on (default: auto-detect)")
	raftAddr := fs.String("raft-addr", "", "enable HA: raft bind address, e.g. :9522")
	raftAdvertise := fs.String("raft-advertise", "", "raft address peers reach us on (default: advertise host + raft port)")
	join := fs.String("join", "", "join an existing cluster via a leader's HTTP address")
	nodeID := fs.String("node-id", "", "stable server node ID (default: derived from advertise addr)")
	_ = fs.Parse(args)

	server.ServerVersion = version

	store, err := server.NewStore(*dataDir)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	// Migrate pre-org state and guarantee a default organization exists.
	store.EnsureDefaultOrg()

	// Join token: load or create.
	token := loadOrCreateToken(*dataDir)

	// Mesh hub.
	var meshHub *server.MeshHub
	if *meshPort > 0 {
		adv := *advertise
		if adv == "" {
			adv = telemetry.Host().IP
		}
		meshHub, err = server.NewMeshHub(*dataDir, adv, *meshPort)
		if err != nil {
			log.Printf("mesh: disabled (%v)", err)
		}
	}

	api := &server.API{
		Store:     store,
		Hub:       server.NewHub(),
		Mesh:      meshHub,
		JoinToken: token,
		InstallID: server.LoadOrCreateInstallID(*dataDir),
		DashFS:    server.DashboardFS(),
		History:   server.NewHistory(*dataDir),
	}

	// Node-down watchdog.
	go func() {
		for range time.Tick(5 * time.Second) {
			for _, n := range store.MarkOfflineNodes(15 * time.Second) {
				store.AddEvent("node_down", "Node went offline: "+n.Name, n.ID, "")
				api.Hub.Broadcast("node_down", n)
			}
		}
	}()

	// Embedded local agent — the server manages its own machine.
	if !*noLocal {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			time.Sleep(500 * time.Millisecond) // let the listener come up
			a, err := agent.New(agent.Config{
				ServerURL: "http://127.0.0.1" + normalizeListen(*listen),
				Token:     token,
				Name:      *name,
				DataDir:   *dataDir,
				Version:   version,
			})
			if err != nil {
				log.Printf("local agent: %v", err)
				return
			}
			a.Run(ctx)
		}()
	}

	store.AddEvent("server", "RackOps control plane started (v"+version+")", "", "")
	server.SetListenPort(strings.TrimPrefix(normalizeListen(*listen), ":"))

	// HA clustering (optional).
	if *raftAddr != "" {
		host := *advertise
		if host == "" {
			host = telemetry.Host().IP
		}
		radv := *raftAdvertise
		if radv == "" {
			radv = host + ":" + strings.TrimPrefix(normalizeListen(*raftAddr), ":")
		}
		id := *nodeID
		if id == "" {
			id = "srv-" + radv
		}
		cl, err := raftstore.New(raftstore.Options{
			NodeID:    id,
			BindAddr:  *raftAddr,
			Advertise: radv,
			DataDir:   *dataDir,
			Bootstrap: *join == "",
			OnApply:   api.ApplyReplicated, // follower-only import (leader is authoritative)
		})
		if err != nil {
			log.Fatalf("raft: %v", err)
		}
		api.StartReplication(cl)

		httpAdv := host + normalizeListen(*listen)
		store.Mutate(func() {
			if store.ClusterHTTP == nil {
				store.ClusterHTTP = map[string]string{}
			}
			store.ClusterHTTP[id] = httpAdv
		})
		log.Printf("raft: node %s bind %s advertise %s", id, *raftAddr, radv)

		if *join != "" {
			go joinCluster(*join, token, id, radv, httpAdv)
		}
	}

	// Domain DNS/edge/cert verification loop (leader-only under HA; started
	// after cluster wiring so it sees the raft handle).
	go api.RunDomainVerifier()
	// Support sync: retries CRM forwards, polls for support-agent replies.
	go api.RunSupportSync()

	log.Printf("rackops server v%s listening on %s", version, *listen)
	log.Printf("join token: %s", token)
	log.Printf("add a node:  rackops agent --server http://<this-host>%s --token %s", normalizeListen(*listen), token)
	if err := http.ListenAndServe(*listen, api.Router()); err != nil {
		log.Fatal(err)
	}
}

// joinCluster asks a leader to admit this server, following redirects to the
// actual leader if the contacted node is a follower.
func joinCluster(leaderHTTP, token, nodeID, raftAddr, httpAddr string) {
	body, _ := json.Marshal(map[string]string{"nodeId": nodeID, "raftAddr": raftAddr, "httpAddr": httpAddr})
	target := strings.TrimSuffix(leaderHTTP, "/")
	for range 30 {
		time.Sleep(2 * time.Second)
		req, _ := http.NewRequest("POST", target+"/api/cluster/join", bytes.NewReader(body))
		req.Header.Set("X-Rackops-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("cluster: join attempt failed: %v (retrying)", err)
			continue
		}
		data, _ := readAll(resp)
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			log.Printf("cluster: joined via %s", target)
			return
		case http.StatusMisdirectedRequest:
			// Follow to the real leader.
			var e struct {
				Leader string `json:"leader"`
				Error  string `json:"error"`
			}
			_ = json.Unmarshal(data, &e)
			if e.Leader != "" {
				log.Printf("cluster: redirected to leader %s", e.Leader)
				target = "http://" + strings.TrimPrefix(strings.TrimPrefix(e.Leader, "http://"), "https://")
			}
		default:
			log.Printf("cluster: join rejected (%d): %s", resp.StatusCode, string(data))
		}
	}
	log.Printf("cluster: giving up joining %s after retries", leaderHTTP)
}

func readAll(resp *http.Response) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}

func runAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := fs.String("server", "", "control plane URL (required)")
	token := fs.String("token", "", "join token (required)")
	name := fs.String("name", "", "node display name")
	org := fs.String("org", "", "organization ID to join (default: the server's default org)")
	dataDir := fs.String("data-dir", defaultDataDir(), "state directory")
	_ = fs.Parse(args)

	if *serverURL == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "agent: --server and --token are required")
		os.Exit(1)
	}
	a, err := agent.New(agent.Config{
		ServerURL: *serverURL, Token: *token, Name: *name, OrgID: *org,
		DataDir: *dataDir, Version: version,
	})
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a.Run(ctx)
}

// runProbe prints full hardware discovery and a metric sample as JSON.
func runProbe() {
	c := telemetry.NewCollector()
	time.Sleep(1 * time.Second) // let rate counters accumulate
	out := map[string]any{
		"host":     telemetry.Host(),
		"hardware": c.Hardware(),
		"metrics":  c.Metrics(),
		"sensors":  telemetry.AllTemps(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".rackops"
	}
	return filepath.Join(home, ".rackops")
}

func loadOrCreateToken(dataDir string) string {
	path := filepath.Join(dataDir, "join-token")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return string(data)
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	_ = os.MkdirAll(dataDir, 0o700)
	_ = os.WriteFile(path, []byte(token), 0o600)
	return token
}

// normalizeListen turns ":8080" into ":8080" and "0.0.0.0:8080" into ":8080".
func normalizeListen(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i:]
		}
	}
	return ":" + addr
}
