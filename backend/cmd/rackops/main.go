// Command rackops is the single RackOps binary: control-plane server, node
// agent, and CLI in one executable.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rackops/rackops/internal/agent"
	"github.com/rackops/rackops/internal/server"
	"github.com/rackops/rackops/internal/telemetry"
)

const version = "0.1.0-dev"

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
  rackops version            Print version

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

// runServer starts the control plane and, by default, an embedded local
// agent so the server's own machine is part of the fleet immediately.
func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "listen address")
	dataDir := fs.String("data-dir", defaultDataDir(), "state directory")
	noLocal := fs.Bool("no-local-agent", false, "disable the embedded local agent")
	name := fs.String("name", "", "node display name")
	_ = fs.Parse(args)

	store, err := server.NewStore(*dataDir)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	// Join token: load or create.
	token := loadOrCreateToken(*dataDir)

	api := &server.API{
		Store:     store,
		Hub:       server.NewHub(),
		JoinToken: token,
		DashFS:    server.DashboardFS(),
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
			a.Run()
		}()
	}

	store.AddEvent("server", "RackOps control plane started (v"+version+")", "", "")
	log.Printf("rackops server v%s listening on %s", version, *listen)
	log.Printf("join token: %s", token)
	log.Printf("add a node:  rackops agent --server http://<this-host>%s --token %s", normalizeListen(*listen), token)
	if err := http.ListenAndServe(*listen, api.Router()); err != nil {
		log.Fatal(err)
	}
}

func runAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := fs.String("server", "", "control plane URL (required)")
	token := fs.String("token", "", "join token (required)")
	name := fs.String("name", "", "node display name")
	dataDir := fs.String("data-dir", defaultDataDir(), "state directory")
	_ = fs.Parse(args)

	if *serverURL == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "agent: --server and --token are required")
		os.Exit(1)
	}
	a, err := agent.New(agent.Config{
		ServerURL: *serverURL, Token: *token, Name: *name,
		DataDir: *dataDir, Version: version,
	})
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	a.Run()
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
