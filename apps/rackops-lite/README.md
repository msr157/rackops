# RackOps Lite

**The Infrastructure Operating System** — one control plane for every machine in your fleet: hardware telemetry, intelligent workload placement, and one-command deployments. No Kubernetes. No complexity tax.

## Install

One line — installs RackOps **and everything it needs** (Docker, compose, git), then starts it. Open **http://localhost:8080** when it finishes.

```sh
curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash
```

Works on any **Linux** machine (x86-64 or ARM) and on **macOS** (Apple Silicon or Intel). Root (`sudo`) is required — the installer sets up Docker, a system service, and a binary in `/usr/local/bin`. Re-running the same command later **updates** RackOps in place. It also discovers containers already running on the machine and adopts them, so your existing services show up in the dashboard ready to manage.

### Add more machines

Open **Machines → Add machine** in the dashboard — it hands you a ready-made one-liner (the same installer in `agent` mode, join token included) to paste on the new machine:

```sh
curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | \
  sudo bash -s -- agent --server http://<control-plane>:8080 --token <join-token>
```

The machine installs Docker if needed, joins the fleet, and reports in every couple of seconds. Agents **dial out** to the control plane — machines behind NAT/CGNAT join without any port forwarding.

### Firewall / cloud notes

- **UDP 51820 inbound on the control-plane machine** — the WireGuard mesh listens there. Without it, cross-machine routing (edge → apps on other machines) can't establish; agents still report fine over HTTP. Change it with `--mesh-port`, or set `--mesh-port 0` to disable the mesh.
- **`--advertise <ip-or-host>`** — on clouds where the machine's primary interface holds a private IP (AWS, GCP, Oracle…), auto-detection picks that private address. Pass the public IP (or DNS name) so agents, Traefik, and the mesh reach the server on an address that routes.

### macOS

The same one-liner installs the binary and a launchd service (starts on boot, restarts on crash, logs to `/var/log/rackops.log`). Two macOS differences:

- **Docker isn't auto-installed** — there is no headless install path on macOS. Have [Docker Desktop](https://www.docker.com/products/docker-desktop/) running, or `brew install colima docker && colima start`. RackOps starts either way; deploys need the runtime.
- **Hardware telemetry is partial** — Linux-only sources (`/proc`, `/sys`, hwmon thermals) aren't available, so those tiles stay empty. Fleet control, deploys, domains, and the mesh all work.

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

## What works today

- **Hardware telemetry** — CPU, memory, load, thermals (hwmon), NVMe temps, drive usage & live R/W throughput, NIC throughput, NVIDIA GPUs (nvidia-smi) and AMD GPUs (sysfs), all read directly from /proc and /sys
- **Fleet control plane** — agents push reports every 2s over outbound HTTP (CGNAT-friendly), node-down watchdog, JSON-persisted state
- **Organizations** — Dokploy-style multi-tenancy: an org switcher, invitation links, per-member roles and granular permissions (down to per-project access), with machines, apps, domains, and settings scoped per organization
- **Intelligent placement** — multi-constraint scorer (CPU/memory/storage/thermal headroom + NVMe/GPU capability) ranks the org's nodes; `nodeId: "auto"` deploys to the winner
- **Deployments** — image, git (Dockerfile/nixpacks/auto), and compose-stack deploys via Docker/podman with env vars, port mapping, restart policy; start/stop/restart/redeploy/remove; adoption of containers that already exist on a machine
- **Projects → Environments → Services** hierarchy (Dokploy-style)
- **Networking** — Traefik edge with Let's Encrypt, domain verification wizard, WireGuard mesh with relays for CGNAT nodes, zero-DNS nip.io URLs, Cloudflare quick tunnels (for apps and for the dashboard itself)
- **Auth & API** — owner/admin/member/viewer roles, password reset (emailed + always in the server log), API keys that act with their owner's role, interactive Swagger docs at `/swagger`, and `rackops mcp` for AI tools (VS Code/Claude/Cursor)
- **Support** — in-dashboard tickets answered by RackOps support, with replies landing back in the thread
- **Alert engine** — sustained-condition alerts (CPU/memory/thermal/disk/node-down) that auto-resolve the moment the condition clears
- **Live dashboard** — dark console UI embedded in the binary; SSE-streamed metrics, org-scoped fleet views, deploy wizard with placement recommendation, one-click self-update

## Roadmap (in flight)

- Web terminal into containers and machines
- Backups (volumes + databases) with schedules
- Managed database lifecycle (upgrades, credentials rotation)
- Prometheus-compatible metrics endpoint

## Layout

```
backend/   Go module — single `rackops` binary (server / agent / probe)
dashboard/ React + TypeScript + Vite (embedded into the binary at build time)
scripts/   install.sh — Linux / macOS / WSL2 one-line installer
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
