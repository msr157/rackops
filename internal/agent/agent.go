// Package agent implements the RackOps node agent: a loop that samples local
// hardware via hwinfo and reports it to a control plane.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/msr157/rackops/internal/api"
	"github.com/msr157/rackops/internal/hwinfo"
)

type Config struct {
	ServerURL string
	Interval  time.Duration
	NodeID    string
}

// Run reports snapshots until ctx is cancelled. Report failures are logged
// and retried on the next tick — the control plane being down must never
// kill the agent.
func Run(ctx context.Context, cfg Config) error {
	client := &http.Client{Timeout: 5 * time.Second}
	log.Printf("rackops-agent %s → %s every %s", cfg.NodeID, cfg.ServerURL, cfg.Interval)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		if err := reportOnce(ctx, client, cfg); err != nil {
			log.Printf("report failed: %v (retrying in %s)", err, cfg.Interval)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func reportOnce(ctx context.Context, client *http.Client, cfg Config) error {
	snap, err := hwinfo.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}

	body, err := json.Marshal(api.ReportRequest{NodeID: cfg.NodeID, Snapshot: snap})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ServerURL+"/api/v1/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("control plane returned %s", resp.Status)
	}
	return nil
}
