// The RackOps node agent: samples local hardware and reports it to a
// control plane on an interval.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/msr157/rackops/internal/agent"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "control plane base URL")
	interval := flag.Duration("interval", 5*time.Second, "reporting interval")
	flag.Parse()

	nodeID, err := agent.NodeID()
	if err != nil {
		log.Fatalf("node identity: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := agent.Config{
		ServerURL: strings.TrimRight(*server, "/"),
		Interval:  *interval,
		NodeID:    nodeID,
	}
	if err := agent.Run(ctx, cfg); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
	log.Println("agent stopped")
}
