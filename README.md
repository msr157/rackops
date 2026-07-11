# RackOps Lite

**The Infrastructure Operating System** — one control plane for every machine in your fleet: hardware telemetry, intelligent workload placement, and one-command deployments. No Kubernetes. No complexity tax.

## Install

One line — installs RackOps **and everything it needs** (Docker, compose, git), then starts it. Open **http://localhost:8080** when it finishes.

```sh
curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash
```

Works on any **Linux** machine (x86-64 or ARM). Root (`sudo`) is required — the installer sets up Docker, a system service, and a binary in `/usr/local/bin`. Re-running the same command later **updates** RackOps in place. It also discovers containers already running on the machine and adopts them, so your existing services show up in the dashboard ready to manage.

**Updating:** three ways, pick any — the dashboard shows an **Update available** banner with a one-click update, `sudo rackops update` self-updates from the terminal, or re-run the install one-liner.

<a name="windows"></a>
### Windows (as a server, via WSL2)

RackOps is a Linux program, but Windows can host it through WSL2 — a real Linux kernel built into Windows. Once set up, you reach RackOps from Windows at `http://localhost:8080`.

1. **Install WSL2** — open PowerShell as Administrator and run `wsl --install`, then reboot. This gives you Ubuntu.
2. **Open Ubuntu** (Start menu → "Ubuntu") and run the one-liner above — it installs Docker inside WSL for you. (Already using Docker Desktop? That works too: enable your distro under Settings → Resources → **WSL integration**.)

RackOps now runs on your Windows machine and manages Docker Desktop. It keeps running while WSL is open; to keep it up after closing the window, leave a terminal running or start WSL at login.

## REST API & Swagger

Everything the dashboard does goes through the REST API. Interactive docs live at **`http://your-server:8080/swagger`** (spec at `/api/openapi.json`). Create an API key in **Profile → API keys**, then authenticate with `Authorization: Bearer rk_…` or the `x-api-key` header.

## Connect your editor (MCP)

The `rackops` binary doubles as a [Model Context Protocol](https://modelcontextprotocol.io) server, so AI tools — VS Code, Claude Desktop/Code, Cursor — can manage your fleet from chat ("deploy nginx", "show me plausible's logs", "restart jellyfin"). Add to your MCP config (VS Code: `.vscode/mcp.json`):

```json
{
  "mcpServers": {
    "rackops": {
      "command": "rackops",
      "args": ["mcp"],
      "env": {
        "RACKOPS_SERVER": "http://your-server:8080",
        "RACKOPS_TOKEN": "rk_your_api_key"
      }
    }
  }
}
```

Tools exposed: `list_machines`, `list_apps`, `get_app`, `deploy_app`, `app_action` (start/stop/restart/redeploy), `app_logs`, `remove_app` (requires explicit confirm), `open_alerts`. The Profile page generates this config for you with your server URL and fresh key filled in.

## Quick start (build from source)

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
- **Live dashboard** — dark console UI embedded in the binary; SSE-streamed metrics, fleet overview, per-node hardware pages, storage visualization, deploy modal with placement recommendation, alerts — with ⓘ info tips throughout so nothing needs a manual
- **Container adoption** — anything already running on a machine shows up as an adopted app: start/stop/restart, logs, and delete all work; removed-outside containers clean themselves up
- **Organisations** — Dokploy-style orgs with per-org members and roles; projects belong to an organisation
- **Accounts done right** — password show/hide, confirm-password, change password, email reset (SMTP optional — the reset link always lands in the server log too)
- **API keys + Swagger + MCP** — per-tool revocable keys in Profile, interactive API docs at `/swagger`, and a built-in MCP server for AI editors
- **Auto-update** — in-dashboard "Update available" banner with one-click update, plus `rackops update` on the CLI
- **Support tickets** — open tickets in-app; they stay local and (optionally) forward to a CRM endpoint with an anonymous usage snapshot

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
