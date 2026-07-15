# RackOps Lite

**The Infrastructure Operating System** — one control plane for every machine in your fleet: hardware telemetry, intelligent workload placement, and one-command deployments. No Kubernetes. No complexity tax.

## Install

One line — installs RackOps **and everything it needs** (Docker, compose, git), then starts it. Open **http://localhost:8080** when it finishes.

```sh
curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash
```

Works on any **Linux** machine (x86-64 or ARM). Root (`sudo`) is required — the installer sets up Docker, a system service, and a binary in `/usr/local/bin`. Re-running the same command later **updates** RackOps in place. It also discovers containers already running on the machine and adopts them, so your existing services show up in the dashboard ready to manage.

<a name="windows"></a>
### Windows (as a server, via WSL2)

RackOps is a Linux program, but Windows can host it through WSL2 — a real Linux kernel built into Windows. Once set up, you reach RackOps from Windows at `http://localhost:8080`.

1. **Install WSL2** — open PowerShell as Administrator and run `wsl --install`, then reboot. This gives you Ubuntu.
2. **Open Ubuntu** (Start menu → "Ubuntu") and run the one-liner above — it installs Docker inside WSL for you. (Already using Docker Desktop? That works too: enable your distro under Settings → Resources → **WSL integration**.)

RackOps now runs on your Windows machine and manages Docker Desktop. It keeps running while WSL is open; to keep it up after closing the window, leave a terminal running or start WSL at login.

## Quick start (build from source)

```sh
# From the monorepo root: build the dashboard, then the single binary
pnpm install && pnpm --filter @rackops/dashboard-lite build
cd apps/rackops-lite/backend && go build -o rackops ./cmd/rackops

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
scripts/   install.sh — Linux / WSL2 one-line installer
```

This app lives in the RackOps monorepo — see the [repo root README](../../README.md) for workspace-wide commands.

## High Availability (Milestone 6)

RackOps Lite ships an optional Raft-replicated control plane — the dashboard and API survive the loss of any single server node.

```sh
# Node 1 (bootstrap)
rackops server --raft-addr :9522 --node-id n1 --advertise <ip1>

# Nodes 2 & 3 join the cluster (share the same join token)
rackops server --raft-addr :9522 --node-id n2 --advertise <ip2> --join http://<ip1>:8080
rackops server --raft-addr :9522 --node-id n3 --advertise <ip3> --join http://<ip1>:8080
```

- Full control-plane state (nodes, services, users, tokens, settings) replicates via `hashicorp/raft`
- Automatic leader election on failure; followers redirect writes to the leader
- Sessions and API tokens survive failover (JWT secret derived from the shared join token)
- Workloads never depend on control-plane uptime — agents keep containers and routing alive regardless

Without `--raft-addr`, RackOps runs as a single node (JSON-persisted state) — ideal for a home lab.
