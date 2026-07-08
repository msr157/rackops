# RackOps Lite

**The Infrastructure Operating System** — one control plane for every machine in your fleet: hardware telemetry, intelligent workload placement, and one-command deployments. No Kubernetes. No complexity tax.

## Quick start

```sh
# Build the dashboard, then the single binary
cd dashboard && npm install && npm run build
cd ../backend && go build -o rackops ./cmd/rackops

# Run the control plane (manages its own machine too)
./rackops server                     # dashboard at http://localhost:8080

# Add another machine to the fleet
./rackops agent --server http://<server>:8080 --token <printed-at-startup>

# Inspect what RackOps sees on this machine
./rackops probe
```

## What works today (Milestone 1)

- **Hardware telemetry** — CPU, memory, load, thermals (hwmon), NVMe temps, drive usage & live R/W throughput, NIC throughput, NVIDIA GPUs (nvidia-smi) and AMD GPUs (sysfs), all read directly from /proc and /sys
- **Fleet control plane** — agents push reports every 2s over outbound HTTP (CGNAT-friendly), node-down watchdog, JSON-persisted state
- **Intelligent placement** — multi-constraint scorer (CPU/memory/storage/thermal headroom + NVMe/GPU capability) ranks nodes; `nodeId: "auto"` deploys to the winner
- **Deployments** — image-based deploys via Docker/podman with env vars, port mapping, restart policy, and per-service container labels; start/stop/restart/redeploy/remove
- **Projects → Environments → Services** hierarchy (Dokploy-style)
- **Alert engine** — CPU >90%, memory >90%, package temp >85°C, disk >92%, node down
- **Live dashboard** — dark console UI embedded in the binary; SSE-streamed metrics, fleet overview, per-node hardware pages, storage visualization, deploy modal with placement recommendation, alerts

## Roadmap (in flight)

- M3: Git + Nixpacks builds, Docker Compose, managed databases, web terminal, backups, template catalog (infra / homelab / AI / web / game servers)
- M4: WireGuard mesh, Traefik + Let's Encrypt, self-hosted relay ingress for CGNAT users
- M5: Auth + RBAC, API tokens, CLI, notifications (Discord/Telegram/Slack/webhook), AI assistant (pluggable providers)
- M6: HA control plane — embedded Raft, 3-node clusters, automatic leader failover

## Layout

```
backend/   Go module — single `rackops` binary (server / agent / probe)
dashboard/ React + TypeScript + Vite (embedded into the binary at build time)
```
