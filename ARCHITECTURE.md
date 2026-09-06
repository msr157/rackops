# RackOps Lite: Exhaustive Architecture & Internals Reference

This is the absolute, definitive technical reference for the RackOps Lite `production` branch. Every subsystem, data flow, internal mechanism, file, and design decision is documented here.

---

## Table of Contents

1. [Core Paradigm: The Unified Binary](#1-core-paradigm-the-unified-binary)
2. [Data Model & Type System](#2-data-model--type-system)
3. [Authentication & RBAC](#3-authentication--rbac)
4. [The Control Plane Server](#4-the-control-plane-server)
5. [Advanced Server Modules](#5-advanced-server-modules)
6. [The Node Agent](#6-the-node-agent)
7. [Bare-Metal Telemetry Engine](#7-bare-metal-telemetry-engine)
8. [WireGuard Mesh Network](#8-wireguard-mesh-network)
9. [Ingress, Domains & Tunnels](#9-ingress-domains--tunnels)
10. [High Availability Raft Clustering](#10-high-availability-raft-clustering)
11. [The Embedded Dashboard](#11-the-embedded-dashboard)
12. [Build, Deployment & Install Flow](#12-build-deployment--install-flow)
13. [CRM: Research & Feature Blueprint](#13-crm-research--feature-blueprint)

---

## 1. Core Paradigm: The Unified Binary

RackOps Lite intentionally rejects distributed complexity (Kubernetes daemonsets, separate API servers, external UI servers). Everything lives inside a single Go executable.

**Entry point: `backend/cmd/rackops/main.go`**

The binary accepts subcommands:
- `rackops server [flags]` — Starts the Control Plane + embedded local Agent + serves the Dashboard
- `rackops agent --server <url> --token <token>` — Starts a pure worker agent on a remote node
- `rackops probe` — Dumps full hardware discovery + live metrics as JSON (debug)
- `rackops nodes / services / deploy / alerts / ask` — CLI client commands (reads `RACKOPS_SERVER` / `RACKOPS_TOKEN` env vars or `--server`/`--token` flags)
- `rackops version` — Prints version string

**Auto-start of local embedded agent:**  
When `rackops server` runs, it waits 500ms (to ensure the listener is up), then spawns a goroutine that calls `agent.New(...)` and `a.Run()` using `http://127.0.0.1<port>` as its server URL and the auto-generated join token. This means the control-plane node immediately manages itself without a separate `agent` process.

**Binary Version:** `const version = "0.2.0"` (defined in `main.go`)

---

## 2. Data Model & Type System (`backend/internal/types/types.go`)

This file is the single source of truth for every data structure exchanged between agent, server, and frontend.

### 2.1 Node
```
Node {
  ID, Name, Role (server|worker), Status (online|offline|joining)
  IP (LAN/public), MeshIP (10.77.x.x), PublicIP
  OS, Arch, Kernel, Hostname
  JoinedAt, LastSeen, AgentVersion
  HasPublicIp (eligible as ingress/relay node)
  Hardware (static inventory), Metrics (live snapshot)
}
```

### 2.2 Hardware (Static Inventory per Node)
```
Hardware {
  CPUModel, CPUCores, CPUThreads
  MemTotalMB
  GPUs[]  → { Index, Vendor (nvidia|amd|intel), Model, VRAMTotal, VRAMUsed, UtilPct, TempC, PowerW }
  Drives[] → { Device (/dev/nvme0n1), Model, Type (nvme|ssd|hdd), SizeGB, UsedGB, MountPoint,
               ReadMBps, WriteMBps, TempC, Health (ok|warning|critical|unknown) }
  NICs[]   → { Name, MAC, SpeedMb, RxMBps, TxMBps }
}
```

### 2.3 Metrics (Live Snapshot, every 2s)
```
Metrics {
  Timestamp, CPUPct, Load1/5/15, MemUsedMB, MemPct, SwapUsedMB
  TempC (hottest package sensor), UptimeSec, Containers (running count), ProcessCount
}
```

### 2.4 Service & Deployment Hierarchy
```
Project → Environment → Service
```
- **Project**: Top-level folder with `SharedEnv` vars inherited by all child environments.
- **Environment**: A tier (e.g., `production`, `staging`) inside a project.
- **Service**: The actual deployable unit. Types: `app | template | compose | database`.
  - Tracks: `Image`, `GitRepo/Branch/Token`, `ComposeYML`, `Port`, `ContainerPort`, `Env[]`, `Mounts[]`, `MemLimitMB`, `CPULimit`, `RestartPol`, `GPU`, `NodeID`, `Status`, `AutoPlace`, `QuickTunnelURL`, `Adopted` (externally-started containers), `ContainerID`.

### 2.5 Agent Task System
The server-to-agent communication model uses a task queue. Task kinds:
- `deploy`, `start`, `stop`, `restart`, `remove` — container lifecycle
- `tunnel_start`, `tunnel_stop` — Cloudflare quick tunnel management
- `listdir` — File system browser (node FS API)

### 2.6 User Roles
- `owner` — First account created, full privileges
- `admin` — Can create/manage users and all resources
- `member` — Default, standard CRUD access
- `viewer` — Read-only (HTTP GET only, enforced by middleware)

---

## 3. Authentication & RBAC (`backend/internal/server/auth.go`)

### 3.1 JWT (HS256, Custom Implementation)
RackOps ships a minimal, zero-dependency JWT implementation. The JWT payload (`jwtClaims`) contains:
- `sub` — User ID
- `role` — The user's role string
- `exp` — Expiry (7 days from issue)

**JWT Secret Derivation (`DeriveJWTSecret`):**  
Instead of storing a random secret (which breaks HA failover), the secret is derived deterministically: `SHA256("rackops-jwt-v1:" + joinToken)`. This means every node in an HA cluster shares the same JWT signing key and sessions survive leader failover transparently.

### 3.2 API Tokens
Long-lived machine tokens (for CLI and automation) are issued as `rk_<48-hex-char>` strings. The raw secret is shown exactly once at creation and never stored. Only the `SHA256(secret)` hash is persisted. On each request, the middleware re-hashes the bearer token and compares against stored hashes.

### 3.3 Auth Middleware
Protects all `/api/*` routes except:
- `/api/auth/*` — Login and signup (public)
- `/api/agent/ingest` — Secured separately by the join token
- `/api/traefik/config`, `/api/edge-probe` — Open for Traefik probes
- `/api/cluster`, `/api/cluster/join` — Raft cluster joining

The middleware supports two authentication paths:
1. `Authorization: Bearer rk_...` (API token) — hashes and looks up in the token store
2. `Authorization: Bearer <jwt>` — parses and validates HMAC signature + expiry

SSE stream tokens can also be passed as `?token=<jwt>` query param (browsers cannot set headers on SSE connections).

### 3.4 Password Hashing
Passwords are hashed with `bcrypt` at `DefaultCost`. The first signup auto-assigns the `owner` role. Subsequent signups require an authenticated `admin` or `owner` to authorize them.

---

## 4. The Control Plane Server (`backend/internal/server`)

### 4.1 State Store (`store.go`)
Thread-safe in-memory state flushed to `~/.rackops/state.json`. The `Mutate(fn)` method acquires an exclusive write lock; `View(fn)` acquires a read lock. The store contains:
- `Nodes` map — All fleet nodes
- `Services` map — All service definitions
- `Deployments` map — Deployment history (keyed by deployment ID)
- `Alerts` — Active and resolved alerts
- `Projects`, `Environments` — Organizational hierarchy
- `Users`, `Tokens` — Auth state
- `Settings` — Server config (AI keys, notification webhooks, ingress config)
- `ClusterHTTP` — HA cluster HTTP address registry

### 4.2 Intelligent Placement Engine (`placement.go`)
Called when `nodeId: "auto"` is used at deploy time or when the frontend calls `POST /api/placement` for a dry-run preview.

**Algorithm:**
1. **Hard constraint filtering**: Immediately excludes nodes that are Offline, have insufficient free RAM (`MemTotalMB - MemUsedMB < req.MemMB`), insufficient free storage, or no GPU with required VRAM.
2. **Soft scoring (0–100)**:
   - CPU headroom: `35 × (100 - CPUPct) / 100`
   - Memory headroom: `30 × FreeMB / TotalMB`
   - Thermal headroom: `20 × clamp(1 - (TempC - 45) / 45, 0, 1)` — perfect at ≤45°C, zero at ≥90°C
   - Storage headroom: `15 × (FreeGB - RequiredGB) / FreeGB`
3. **Bonuses**: +5 pts if `NVMe` available and workload sets `PreferNVMe`
4. **Result**: Returns a sorted `[]PlacementScore` — the first `Viable: true` entry is picked.

### 4.3 Service & Deployment Lifecycle (`services.go`)
**Deploy flow:**
1. `POST /api/services` arrives with a service spec and optional template ID and placement request.
2. If `TemplateID` is set, the catalog entry is expanded: image, ports, mounts, and env vars are merged. Env vars tagged `Generate: true` receive auto-generated 64-hex-char secrets.
3. If `nodeId == "auto"`, `PlaceWorkload()` is called and the best node is selected.
4. Port collision avoidance: `suggestHostPort()` scans all currently occupied ports on the target node and auto-increments from the preferred port to find a free one.
5. A `Deployment` record is created (status: `building`) and added to the store.
6. An `AgentTask{Kind: TaskDeploy}` is enqueued to the target node's task queue.
7. The SSE hub broadcasts `service_update` to all connected dashboards immediately.

**Service actions:**
- `redeploy` — Re-queues a full `TaskDeploy` (blocked on adopted/external containers)
- `start`, `stop`, `restart` — Enqueues the corresponding task kind with the container ID/name
- `DELETE /api/services/:id` — Enqueues `TaskRemove`, then removes from store and broadcasts `service_removed`

### 4.4 Node Watchdog (goroutine in `main.go`)
Every 5 seconds, `store.MarkOfflineNodes(15 * time.Second)` scans all nodes. Any node whose `LastSeen` is older than 15 seconds is marked `offline`. For each newly-offline node, an event is stored and `Hub.Broadcast("node_down", n)` fires — causing the dashboard to instantly reflect the outage.

### 4.5 Full HTTP API Route Map (`api.go`)

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/agent/ingest` | Agent heartbeat — receives telemetry + task results, returns task queue |
| GET | `/api/nodes` | List all fleet nodes |
| GET | `/api/nodes/:id` | Get single node detail |
| GET | `/api/nodes/:id/fs` | Browse node filesystem |
| GET | `/api/overview` | Fleet summary (nodes online, services running, alert count) |
| GET | `/api/events` | Recent event log |
| GET | `/api/alerts` | Open alerts |
| GET | `/api/join-command` | Returns the `rackops agent --server ... --token ...` command |
| GET/POST | `/api/projects` | List / create projects |
| GET/POST | `/api/projects/:id/envs` | List / create environments |
| GET/POST | `/api/services` | List / create services |
| GET | `/api/services/:id` | Get service detail |
| PATCH | `/api/services/:id` | Update service (name, env, config) |
| POST | `/api/services/:id/:action` | Lifecycle: start/stop/restart/redeploy |
| POST | `/api/services/:id/clear-logs` | Clear stored logs |
| DELETE | `/api/services/:id` | Remove service + its container |
| GET | `/api/services/:id/logs` | Fetch stored container logs |
| GET | `/api/services/:id/deployments` | Deployment history |
| POST | `/api/placement` | Dry-run placement scoring |
| GET | `/api/suggest-port` | Port auto-suggestion |
| GET | `/api/templates` | One-click template catalog |
| GET | `/api/mesh` | Mesh network status |
| GET | `/api/mesh/dial` | Dial a node over the mesh |
| `/relay/:id/:rest...` | Relay proxy | Cross-node HTTP relay |
| GET/POST | `/api/domains` | List / create custom domains |
| DELETE | `/api/domains/:id` | Delete a domain |
| POST | `/api/domains/:id/verify` | Trigger DNS/cert verification |
| GET | `/api/edge-probe` | Traefik health probe |
| GET | `/api/network/diagnosis` | Network topology diagnosis |
| GET/POST | `/api/ingress` | Get / set ingress node config |
| GET | `/api/traefik/config` | Dynamic Traefik routing config |
| POST | `/api/services/:id/domain` | Assign domain to a service |
| POST | `/api/services/:id/expose` | Expose service via ingress |
| POST/DELETE | `/api/services/:id/quick-tunnel` | Start/stop Cloudflare quick tunnel |
| GET | `/api/stream` | SSE real-time event stream |
| GET | `/api/auth/status` | Check if setup is needed |
| POST | `/api/auth/signup` | Create user account |
| POST | `/api/auth/login` | Authenticate and get JWT |
| GET | `/api/users` | List users (admin+) |
| POST/GET | `/api/tokens` | Create / list API tokens |
| DELETE | `/api/tokens/:id` | Revoke API token |
| POST | `/api/assistant` | AI Copilot query |
| POST | `/api/assistant/test` | Test AI provider key |
| POST/GET | `/api/settings` | Save / read server settings |
| GET/POST | `/api/cluster` | Cluster status / join |

---

## 5. Advanced Server Modules

### 5.1 AI Copilot (`assistant.go`)
RackOps ships a fully integrated infrastructure AI assistant.

**Provider routing (priority order):**
1. If `Settings.AnthropicKey` is set → routes to Claude (Anthropic)
2. If `Settings.GeminiKey` is set → routes to Gemini (Google)
3. If `Settings.OpenAIKey` or `Settings.OpenAIBase` is set → routes to OpenAI (or any Ollama-compatible endpoint)
4. Fallback → deterministic built-in rule engine (`builtinAssistant`)

**Context injection:** On every request, `fleetContext()` serializes the full live state (nodes, metrics, services, alerts) into a JSON string and prepends it to the system prompt. This grounds the AI in real-time infrastructure data rather than hallucinated general knowledge.

**Rule-engine fallback:** If an API call fails (e.g., network error, quota exceeded), the server automatically falls back to the built-in rule engine and appends `_(provider error — answered by built-in engine)_` to the response. No chat session is lost.

**Key test endpoint:** `POST /api/assistant/test` sends `"Reply with the single word: OK"` to the configured provider to validate keys without starting a real conversation.

### 5.2 Alerting Engine (`notify/notify.go`)
Fires on threshold breaches (CPU >90%, temp >85°C, disk >92%, node offline). All sends are **fire-and-forget** (async goroutine, 8-second timeout per channel).

| Channel | Payload format |
|---------|---------------|
| Discord | Rich embed with title, description, and color (red=critical, yellow=warning, green=info) |
| Slack | Markdown `*title*\nmessage` formatted block |
| Telegram | Bot API `sendMessage` to configured `chat_id` |
| Generic webhook | JSON: `{ source, title, message, level, timestamp }` |

### 5.3 Template Catalog (`catalog/catalog.go`)
A statically-compiled in-binary library of 30+ one-click deployable apps across 7 categories:

| Category | Examples |
|----------|----------|
| **Infra / DevOps** | MinIO, Gitea, Jenkins, Prometheus, Grafana, Uptime Kuma, Docker Registry, RabbitMQ, SeaweedFS |
| **Homelab** | Jellyfin, Immich, Nextcloud, Home Assistant, Vaultwarden, AdGuard Home, Pi-hole, Syncthing |
| **AI / Builder** | Ollama (GPU-accelerated), Open WebUI, n8n, PocketBase, Flowise, Qdrant |
| **Web / Business** | WordPress, Ghost, Plausible CE, Umami, Listmonk |
| **Tools** | Excalidraw, IT-Tools, Stirling PDF, code-server (VS Code), File Browser, draw.io |
| **Game Servers** | Minecraft (Java & Bedrock), CS2, Valheim, Palworld, Terraria, Factorio, Rust, ARK |
| **Managed Databases** | PostgreSQL 16, MySQL 8, MariaDB 11, MongoDB 7, Redis 7, ClickHouse |

**Multi-container stacks:** Templates like Immich, WordPress, Plausible, Umami, and Listmonk ship complete Docker Compose YAMLs embedded as Go string constants. These are injected into the agent's `docker compose` execution, wiring multiple containers (app + DB + cache) onto a private Docker network automatically.

**Secret auto-generation:** Template env vars tagged `Generate: true` receive a 64-hex-character cryptographic random secret on deploy. Shared secrets (e.g., `DB_PASSWORD` used by both the app and database containers in a Compose stack) are generated once and injected consistently via `.env` substitution.

---

## 6. The Node Agent (`backend/internal/agent`)

### 6.1 The Heartbeat Loop (`agent.go`)
The agent runs a continuous 2-second ticker:
1. Collects `telemetry.NewCollector().Hardware()` and `.Metrics()`
2. Calls `runtime.List()` to snapshot all containers (RackOps-labeled + external)
3. Checks the public IP reflector (refreshed hourly) and includes it in the report
4. Serializes and `POST /api/agent/ingest` to the control plane
5. The server response contains `[]AgentTask` — the agent processes each task sequentially
6. Task results (success/failure + logs) are accumulated and sent with the next heartbeat

### 6.2 Container Runtime Abstraction (`runtime.go`)
RackOps communicates with containers entirely via CLI subprocess — no Docker daemon socket or API client libraries.

**Detection:** `DetectRuntime()` checks for `docker` then `podman` on `$PATH`. It runs a quick version probe to confirm the daemon is responsive before accepting.

**Container inventory:**
- `listLabeled("rackops.service")` — RackOps-managed services (via `docker ps --filter label=rackops.service`)
- `listLabeled("rackops.tunnel")` — Active quick-tunnel containers
- `listExternal()` — All other containers **not** managed by RackOps, reported as "adoptable" to the server

**Live resource sampling:** `docker stats --no-stream --format "{{.ID}}|{{.CPUPerc}}|{{.MemUsage}}"` — parsed into per-container CPU% and memory (MiB/GiB/KiB suffix parsing).

**Deploy flow (`Deploy()`):**
1. If `ComposeYML` is non-empty → handled by `deployCompose()` (separate path, no port assignment)
2. If `GitRepo` is set → call `buildFromGit()` to produce the image first
3. Otherwise → `docker pull <image>`
4. Run with: `docker run -d --name rackops-<serviceID> --restart <policy> -p <hostPort>:<containerPort> -e KEY=VAL ... -v ... --label rackops.service=<serviceID>`

**Port conflict resolution:** The agent attempts the exact requested host port. If it's already in use (EADDRINUSE), it scans upward to find the next free port and returns it to the server for storage.

### 6.3 CI/CD Build Pipeline (`build.go`)
No external CI required. The agent handles source builds with a 4-tier fallback:
1. **Dockerfile detected** → `docker build -t rackops/<name>:<serviceID> .`
2. **nixpacks on PATH** → `nixpacks build <workdir> --name <image>`
3. **Stack auto-detection** → Scans for `package.json` (Node), `requirements.txt` (Python), `go.mod` (Go), `index.html` (static). Generates an appropriate Alpine-based Dockerfile.
4. **Failure** → Returns error: `"no Dockerfile and unable to detect stack"`

Auth tokens are injected into the clone URL (`https://oauth2:<token>@github.com/...`) and redacted from logs before storage.

Build logs exceeding 8,000 characters are head-truncated with `"…(truncated)…"` to prevent unbounded memory usage.

### 6.4 Docker Compose Support (`compose.go`)
For multi-container templates, the agent:
1. Writes the Compose YAML to a temp directory
2. Writes a `.env` file with all the resolved env vars (so `${DB_PASSWORD}` substitutions work)
3. Runs `docker compose -p <serviceName> up -d --build`
4. Returns the first 8KB of output as the deployment log

### 6.5 Node Filesystem Browser (`fs.go`)
`GET /api/nodes/:id/fs?path=<dir>` tasks the agent to call `os.ReadDir()` on the specified path and return a sorted listing of files/directories with sizes and permissions. Used by the dashboard to let admins browse server volumes and select mount paths.

### 6.6 WireGuard Mesh on the Agent (`mesh.go`)
On startup, the agent loads or generates a WireGuard `KeyPair` (private + public keys) stored at `~/.rackops/mesh-node.key`.

When the server assigns a mesh IP in the heartbeat response (`MeshInfo`), the agent:
1. Calls `mesh.Up(Config{...})` to bring up a user-space WireGuard interface pointing at the hub (`10.77.0.1`)
2. Proxies exposed service ports onto the mesh IP (so cross-node and ingress traffic can reach them)
3. Opens relay TCP listeners for services running on *other* nodes that need to be proxied via *this* node

---

## 7. Bare-Metal Telemetry Engine (`backend/internal/telemetry`)

No external agents or exporters. RackOps reads directly from Linux kernel interfaces.

### 7.1 Host Metrics (`host.go`)
- **CPU load**: Reads `/proc/stat` lines, calculates `(total - idle) / total` delta between samples for true instantaneous CPU utilization
- **Load averages**: Reads `/proc/loadavg` for 1/5/15-minute values
- **Memory**: Reads `/proc/meminfo` for `MemTotal`, `MemAvailable`, `SwapTotal`, `SwapFree`
- **Uptime**: Reads `/proc/uptime`
- **Process count**: Reads `/proc/loadavg` (5th field = running process count)
- **Public IP**: Queries external reflector services, cached hourly (used for ingress eligibility)

### 7.2 Storage & Network (`storage_net.go`)
- **Block devices**: Iterates `/sys/block/*`, reads `size` file for capacity, calls `syscall.Statfs` for free space
- **Drive type detection**: Checks `/sys/block/<dev>/queue/rotational` (0=SSD/NVMe, 1=HDD) and device name prefix (`nvme` → NVMe)
- **Live IOPS/throughput**: Reads `/sys/block/<dev>/stat`, tracks sector delta between samples, converts using 512-byte sector size → MB/s
- **Drive temperature**: Reads NVMe temps from `/sys/class/nvme/nvme*/device/hwmon/hwmon*/temp*_input`
- **Drive health**: Cross-references SMART-adjacent status from kernel data
- **NIC throughput**: Reads `/sys/class/net/<iface>/statistics/rx_bytes` and `tx_bytes`, calculates delta per sample interval

### 7.3 GPU Engine (`gpu.go`)
- **NVIDIA**: Runs `nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits`. Parses CSV output per GPU.
- **AMD**: Reads `/sys/class/drm/card*/device/hwmon/hwmon*/` for `power1_average`, `temp1_input`, reads `mem_info_vram_used` and `mem_info_vram_total` from DRM sysfs. No external binary required.
- **Intel**: Reads `/sys/class/drm/card*/gt_cur_freq_mhz` and `/sys/class/powercap/intel-rapl/*/energy_uj`

### 7.4 Hardware Discovery (`host.go`)
On first agent registration, static hardware is inventoried:
- CPU: Reads `/proc/cpuinfo` for model name, core count, thread count
- Network: Reads `/sys/class/net/*/` for interface names, MAC addresses, link speed

---

## 8. WireGuard Mesh Network (`backend/internal/mesh`, `backend/internal/server/meshhub.go`)

RackOps ships a zero-config, fully self-managed SD-WAN using user-space WireGuard.

### 8.1 Architecture
- **Hub IP**: `10.77.0.1` (the control plane server)
- **Spoke IPs**: Agents are assigned sequential IPs from `10.77.0.2` onward
- **Implementation**: `golang.zx2c4.com/wireguard/tun/netstack` — runs entirely in user-space, requires no kernel modules, no root privileges, works even on hosts where WireGuard kernel support is absent

### 8.2 Key Exchange
1. Agent generates a WireGuard keypair on first start, stores it at `~/.rackops/mesh-node.key`
2. Agent sends its `PublicKey` to the server in every heartbeat payload
3. Server assigns a `MeshIP`, returns its own hub `HubKey` and `HubAddr` in the `MeshInfo` response
4. Agent calls `mesh.Up()` — brings up the interface, establishes the WireGuard tunnel to the hub

### 8.3 Service Port Proxying
Once connected, the agent keeps a map of `servicePort → meshIP:port` proxies. When a container exposes port 3000, the agent's mesh layer creates a local TCP listener on `10.77.0.x:3000` that transparently forwards to `127.0.0.1:3000` (the container). This allows any other node in the mesh to reach the service regardless of NAT or firewalls.

### 8.4 Relay Listeners (Ingress Integration)
For nodes designated as ingress (public IP, runs Traefik), the server sends `Relays[]` in the `MeshInfo` payload. Each relay is `{ LocalPort: 10001, TargetAddr: "10.77.0.3:3000" }`. The ingress node opens a local TCP listener on `127.0.0.1:10001` and pipes all traffic to `10.77.0.3:3000` over the WireGuard mesh. Traefik routes `service.domain.com → 127.0.0.1:10001`, giving public access to a container sitting behind CGNAT with no port forwarding.

---

## 9. Ingress, Domains & Tunnels

### 9.1 Custom Domains (`domains.go`)
- Users register domains via `POST /api/domains` with a hostname and target service ID
- RackOps auto-generates a DNS verification TXT record
- `RunDomainVerifier()` runs continuously (leader-only in HA), polling DNS to verify ownership before activating routing

### 9.2 Dynamic Traefik Configuration (`ingress.go`)
- `GET /api/traefik/config` serves a dynamically generated Traefik provider config
- Traefik is configured in `file` provider mode pointing at this endpoint
- On every poll, Traefik gets updated routing rules reflecting the current service/domain state — no Traefik restart needed

### 9.3 Cloudflare Quick Tunnels (`tunnel.go`)
For instant shareable testing URLs (zero DNS, zero account):
1. User clicks "Share" on a service in the UI
2. Server enqueues `TaskTunnelStart` to the agent on the service's node
3. Agent runs `cloudflared tunnel --no-autoupdate run --url http://localhost:<port>` (or equivalent quick tunnel command)
4. The tunnel URL (`https://<random>.trycloudflare.com`) is captured from stdout and returned to the server
5. Server stores `QuickTunnelURL` on the service and broadcasts `service_update` — dashboard shows the live link instantly

**Limitations**: Quick tunnels are testing-only — random URL per restart, bandwidth-limited by Cloudflare, not suitable for production traffic.

### 9.4 Network Diagnosis (`GET /api/network/diagnosis`)
Returns a full topology report: which nodes have public IPs, which are eligible as ingress nodes, current mesh connectivity status, and any detected routing issues.

---

## 10. High Availability Raft Clustering (`backend/internal/raftstore/raftstore.go`)

### 10.1 Single-Node Default
Without `--raft-addr`, the server uses the JSON file store. State is local and immediate. No replication overhead.

### 10.2 HA Mode
When `--raft-addr :9522` is provided:
1. The server creates a Raft node using `hashicorp/raft` with `boltdb` as the log store
2. `--join http://<leader>:8080` causes the new node to call `POST /api/cluster/join` on the leader, which admits it to the Raft group
3. All API write operations are submitted via `api.Cluster.Apply(data)` — this goes through Raft consensus before returning to the client
4. The `FSM.Apply()` method on each follower replays the log entry against their in-memory store
5. **Follower redirect**: The auth middleware detects if `Cluster.IsLeader()` is false and redirects write requests (`307 Misdirected Request`) with `{ "leader": "<http-addr>" }` — the client or agent transparently follows

### 10.3 Cluster HTTP Registry
Each server node registers its HTTP address in `store.ClusterHTTP[nodeID]`. This is replicated via Raft so every node knows how to proxy/redirect to the current leader.

### 10.4 Session Survival
Because the JWT secret is derived from the join token (not randomly generated per-node), sessions created against one server are valid against any other server in the cluster. Login survives leader failover with zero re-authentication.

---

## 11. The Embedded Dashboard (`dashboard/`)

React + TypeScript + Vite SPA, embedded into the Go binary at compile time.

### 11.1 Real-Time Data Architecture
The frontend never polls. It holds a single persistent SSE connection to `GET /api/stream?token=<jwt>`.

When any agent pushes telemetry, the server's internal `Hub.Broadcast(event, payload)` fans out JSON to all SSE listeners. The `DataProvider` context (`src/data.tsx`) processes these events and immediately updates React state — resulting in live-updating CPU graphs, LED indicators, and container statuses without any latency-introducing poll cycles.

### 11.2 Pages & Routing
| Route | Component | Purpose |
|-------|-----------|---------|
| `/` | `HomePage` | Fleet overview, alert feed, quick stats |
| `/apps` | `AppsPage` | All services across the fleet |
| `/apps/new` | `DeployFlow` | Step-by-step service deployment wizard |
| `/apps/:id/:tab?` | `AppDetail` | Service detail: logs, deployments, config, storage |
| `/machines` | `MachinesPage` | All nodes with hardware overview |
| `/machines/:id` | `MachineDetail` | Per-node detail: CPU graph, drives, GPUs, NICs |
| `/network` | `NetworkPage` | WireGuard mesh topology, domain management |
| `/settings` | `SettingsPage` | Server config: AI keys, notification webhooks, users, tokens |

### 11.3 Key Frontend Components
- **`App.tsx`**: Auth gate — calls `GET /api/auth/status` on load. If `needsSetup: true`, shows signup flow. Otherwise checks for a stored JWT and routes to the shell or login screen.
- **`data.tsx` (DataProvider)**: The global state context. Holds `overview`, `nodes`, `services`, `alerts`. Consumes the SSE stream and merges incremental updates.
- **`CommandPalette.tsx`** (`⌘K`): Keyboard-centric global search — jump to any service, node, or action instantly.
- **`CopilotDrawer.tsx`**: Slide-out AI assistant panel — sends messages to `POST /api/assistant`, displays streaming responses.
- **`AddMachineModal.tsx`**: Shows the `join-command` for adding a new node, including OS-specific install instructions.
- **`Rack.tsx`**: Visual rack/grid display of machines with status LEDs.
- **`toast.tsx`** / **`confirm.tsx`**: Ephemeral notification and destructive-action confirmation system.

### 11.4 Static Embedding (`backend/internal/server/embed.go`)
```go
//go:embed all:../../dashboard/dist
var dashboardFS embed.FS
```
At build time, every file under `dashboard/dist/` (HTML, JS, CSS, assets) is bundled into the Go binary. The HTTP server serves these assets directly from memory — no filesystem or CDN required.

---

## 12. Build, Deployment & Install Flow

### 12.1 Build from Source
```sh
# 1. Build the frontend
cd dashboard && npm install && npm run build
# 2. Build the binary (embeds the dashboard dist/)
cd ../backend && go build -o rackops ./cmd/rackops
```

### 12.2 One-Line Installer (`scripts/install.sh`)
```sh
curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash
```
The installer:
1. Installs Docker and Git if not present
2. Downloads the pre-built `rackops` binary for the host architecture (x86-64 or ARM)
3. Places it at `/usr/local/bin/rackops`
4. Creates a systemd service (`rackops.service`) for auto-start
5. Runs `rackops server` and prints the join token and dashboard URL
6. Re-running the installer performs an in-place upgrade

### 12.3 Version
Current production version: `0.2.0`

---

## 13. CRM: Research & Feature Blueprint

To elevate RackOps into a complete, world-class ecosystem, the next major evolution is a deeply integrated native CRM. Our goal is not just to clone existing tools — but to build something that no other CRM can offer by leveraging the unique fact that we own and manage the infrastructure layer.

### 13.1 Market Research: The Full Landscape

**Open-Source CRM Leaders (as of 2026):**

| CRM | Strengths | Weaknesses |
|-----|-----------|------------|
| **Twenty** | Fastest-growing, modern code-first design, gorgeous UI, developer extensibility, active GitHub community | Still maturing; some enterprise features missing |
| **SuiteCRM** | Most feature-complete OS alternative, deep invoicing/cases/leads, Salesforce-comparable depth | Outdated UI, high setup complexity, PHP-based |
| **Odoo CRM** | Full ERP suite (accounting, inventory, HR), one-stop-shop for businesses | Extreme complexity, heavy resource footprint, license model confusing |
| **EspoCRM** | Non-technical admin panel (90% no-code), fast, lightweight | Less extensible for developers, smaller ecosystem |
| **Vtiger** | Long-standing, stable, broad native tools | Dated, split open/paid features causing confusion |

**Closed-Source Enterprise CRM Leaders:**

| CRM | Strengths | Weaknesses |
|-----|-----------|------------|
| **Salesforce Sales Cloud** | Industry titan, Einstein AI analytics, massive integration ecosystem, world-class governance | Extreme cost, heavy admin overhead, vendor lock-in |
| **HubSpot Enterprise** | Best-in-class UX, high sales-team adoption, excellent marketing alignment | Cost scales aggressively with contacts, limited self-hosting |
| **Microsoft Dynamics 365** | Deep Office 365/Azure/LinkedIn integration, unified ERP+CRM | Complexity, Azure dependency, expensive for smaller teams |
| **Zoho CRM Enterprise** | Territory management, blueprint automation, value pricing | UI inconsistency, support quality variance |
| **Creatio** | Low-code/no-code workflow builder, flexible for enterprise processes | Less known, smaller partner ecosystem |

**Key Lessons from Market Analysis:**
- No existing CRM unifies customer management with infrastructure provisioning.
- Enterprises consistently cite fragmented data (CRM vs. billing vs. support) as their #1 frustration.
- AI in modern CRMs is still mostly copilot-level — agentic execution (taking action autonomously) is still a largely unmet promise.
- Self-hosted data sovereignty is the fastest-growing demand, driven by GDPR and data residency laws.

### 13.2 RackOps CRM: The Feature Blueprint

The RackOps CRM must surpass the market by unifying what no other CRM can: **customer relationships, infrastructure management, and billing — all in one place**.

---

**MODULE 1: Core Contact & Account Management**
- **360° Contact View**: Every touchpoint — emails, calls, meetings, tickets, and invoices — visible on one timeline per contact.
- **Account Hierarchy**: Organizations → Subsidiaries → Contacts, with inherited attributes and shared pipeline visibility.
- **Duplicate Detection**: Automatic merge suggestions for contacts with matching email/phone.
- **Custom Fields**: Any field type (text, number, date, dropdown, relation) without writing code.
- **Activity Log**: All interactions (emails read, links clicked, forms submitted) tracked and time-stamped.
- **Contact Enrichment**: Automatic data enrichment from LinkedIn, Clearbit, or similar APIs based on email domain.

---

**MODULE 2: Sales Pipeline & Opportunity Management**
- **Multi-Pipeline Kanban**: Different pipeline stages per product line or sales team. Drag-and-drop cards between stages.
- **Weighted Forecasting**: Revenue forecast calculated as `deal_value × close_probability`. Aggregated by rep, team, and period.
- **Deal Rotting**: Alerts when an opportunity hasn't been touched in X days (configurable per stage).
- **Win/Loss Analysis**: Mandatory "reason for loss" on close. Aggregated reporting to identify patterns.
- **Quotes & Proposals**: Generate branded PDF proposals directly from the opportunity, pulling server specs from the RackOps infrastructure layer.
- **E-Signature Integration**: Send quotes for digital signature (DocuSign / native implementation).
- **Revenue Targets**: Per-rep, per-team quota tracking with attainment % dashboards.

---

**MODULE 3: Omnichannel Communication**
- **Two-Way Email Sync**: Connect Gmail/Outlook/IMAP. All emails are associated to the correct contact automatically. Tracked opens and link clicks.
- **Email Sequences (Cadences)**: Multi-step, timed outreach sequences sent automatically when a contact enters a stage.
- **VoIP Calling**: One-click dial from contact record. Call recorded, transcribed, and auto-summarized by AI.
- **SMS & WhatsApp**: Send and receive messages, visible in the contact timeline.
- **Live Chat Widget**: Embed on the marketing site. Inbound chats routed to available sales reps with full contact card visible.
- **Meeting Booking**: Integrated calendar (Google/Outlook sync). Shareable booking links. Meetings auto-linked to the contact record.

---

**MODULE 4: Marketing Automation**
- **Segmentation**: Dynamic contact segments based on any field combination (e.g., "All enterprise contacts in APAC who opened an email in the last 30 days").
- **Email Campaigns**: Template builder (drag-and-drop blocks), A/B testing, scheduling, open/click/unsubscribe analytics.
- **Drip Campaigns**: Automated email sequences triggered by behavior (signed up, visited pricing page, trial expired).
- **Landing Pages & Forms**: No-code form builder that feeds leads directly into the pipeline.
- **Lead Scoring**: Rules-based scoring (e.g., +10 for email open, +25 for demo request, -5 for unsubscribe) + AI-predicted fit scoring.
- **UTM Tracking**: Full attribution chain from ad campaign → landing page → form → deal.

---

**MODULE 5: Customer Support & Ticketing**
- **Omnichannel Inbox**: All support emails, chat messages, and form submissions land in a single unified queue.
- **Ticket Lifecycle**: Open → In Progress → Awaiting Customer → Resolved → Closed, with SLA timers per tier.
- **Knowledge Base**: Internal and public-facing article library with search. Deflection metrics tracked.
- **CSAT / NPS**: Automated satisfaction surveys sent after ticket resolution. Scores stored per contact.
- **Escalation Rules**: Tickets breaching SLA automatically escalate to senior reps and trigger Slack/Discord/email alerts.

> **THE RACKOPS ADVANTAGE:** When a client opens a support ticket, the CRM automatically attaches **live telemetry from their specific containers** — CPU%, memory, disk I/O, error logs, and recent deployment events — directly to the ticket. The support agent sees what is happening on the client's infrastructure *before they even respond*, eliminating the back-and-forth of "can you send us logs?"

---

**MODULE 6: Infrastructure-Integrated Billing**
This is the feature that no other CRM on the planet has.

- **Usage-Based Billing**: The CRM reads directly from the RackOps Telemetry Engine. At billing cycle close, it computes each client's actual CPU-hours, RAM-GB-hours, and GB-of-network-egress consumed and generates an exact invoice.
- **Flat + Usage Hybrid Plans**: Define a base subscription fee (e.g., $99/month for 2 cores) plus overage rates (e.g., $0.05/core-hour above baseline).
- **Invoice Generation**: Auto-generate PDF invoices with itemized infrastructure usage. Sent automatically via email at billing cycle close.
- **Stripe / LemonSqueezy Integration**: Payment collection directly from the invoice. Failed payment → automatic dunning sequence.
- **Automated Provisioning on Close**: When a deal stage reaches "Closed Won," a webhook triggers the RackOps Agent to provision the contracted infrastructure for the client. Server, database, and networking are live before the client even finishes reading the confirmation email.
- **Automated Deprovisioning**: When a subscription is cancelled or payment fails after dunning, the CRM sends a deprovisioning task to the RackOps agent.
- **Multi-Currency & Tax**: Support for VAT, GST, sales tax with geo-detection.

---

**MODULE 7: Agentic AI (The Copilot Layer)**
Built on top of the existing `assistant.go` framework.

- **Lead Qualification Agent**: When a new lead comes in, the AI automatically researches the company (web scraping + enrichment APIs), scores the lead, and drafts a personalized first outreach email for rep review.
- **Meeting Prep Agent**: Before a scheduled call, the AI generates a briefing document: company news, recent interactions, open deals, last support tickets, and suggested talking points.
- **Deal Risk Agent**: Continuously monitors pipeline deals. If a deal goes quiet, key contact leaves the company (LinkedIn signal), or competitor is mentioned in email thread, the agent flags it and suggests corrective actions.
- **Auto-Summary on Calls**: VoIP recordings are transcribed and auto-summarized. Key action items are extracted and added as tasks to the deal.
- **Churn Prediction**: AI model trained on historical usage patterns predicts which clients are likely to churn in the next 30 days. Flags them for proactive outreach.
- **Natural Language CRM Queries**: `"Show me all deals above $10k that haven't moved in 2 weeks"` — typed in plain English, executed against live CRM data.

---

**MODULE 8: Analytics & Reporting**
- **Sales Dashboard**: Real-time: pipeline value by stage, win rate, average deal size, sales cycle length, rep leaderboard.
- **Marketing Dashboard**: Lead volume by source, CPL by campaign, email open/click rates, form conversion rates.
- **Support Dashboard**: Ticket volume by day, avg resolution time, CSAT trend, top issue categories.
- **Custom Reports**: Drag-and-drop report builder. Any field, any time range, any grouping. Export as CSV or PDF.
- **Goal Tracking**: Set team and individual goals (e.g., 50 calls/week, $500k ARR). Visual attainment tracking.

---

**MODULE 9: Enterprise Governance**
- **Role-Based Access Control (RBAC)**: Granular permissions. Sales reps see only their contacts. Support sees all tickets but not billing. Finance sees invoices but not pipeline.
- **Audit Log**: Every record change, login, export, and API call is logged with actor, timestamp, and before/after values. Immutable and exportable for compliance.
- **Data Retention Policies**: Configure automatic PII deletion after a period to comply with GDPR "right to be forgotten."
- **SSO / SAML 2.0**: Single sign-on with Google Workspace, Okta, Azure AD.
- **IP Allowlisting**: Restrict dashboard access to specific IP ranges.

---

**MODULE 10: Self-Hosted Data Sovereignty**
- **Zero Vendor Lock-In**: The entire CRM runs inside the RackOps binary. No SaaS dependency, no monthly per-seat fees, no data leaving your infrastructure.
- **Data Export**: One-click full data export to JSON/CSV at any time.
- **Open API**: Full REST API with API tokens (using the existing `rk_` token system) so any external tool can read/write CRM data.
- **Multi-Tenant**: A single RackOps instance can serve multiple isolated organizations (for MSPs and hosting providers).

---

### 13.3 Implementation Approach

**Phase 1 — Foundation (Weeks 1–4):**
- Extend `types.go` with `Contact`, `Account`, `Deal`, `Activity`, `Ticket` types
- Add CRM CRUD API routes under `/api/crm/*`
- Build the React frontend views for Contacts, Pipeline Kanban, and Deal Detail

**Phase 2 — Communication (Weeks 5–8):**
- Implement email sync (IMAP/SMTP or Gmail API OAuth)
- Add activity timeline with email thread association
- Meeting scheduling with calendar integration

**Phase 3 — Infra Integration (Weeks 9–12):**
- Wire telemetry data to support tickets
- Build usage-based billing engine reading from telemetry store
- Implement auto-provisioning webhooks on deal close

**Phase 4 — Agentic AI (Weeks 13–16):**
- Extend `assistant.go` with CRM-specific context injection
- Lead qualification agent
- Churn prediction ML model

**Phase 5 — Enterprise Polish (Weeks 17–20):**
- Full RBAC for CRM modules
- Audit log
- SSO / SAML
- Multi-tenant support
