# RackOps — Project Map

> Complete codebase knowledge dump, generated from a full-repo deep read on 2026-07-15
> (branch `production` @ `c8acf51`). Covers architecture, specs, wire protocols, remotes,
> and known issues. Companion to [README.md](README.md) (commands) and [AGENTS.md](AGENTS.md)
> (conventions). Verify file:line refs against current code before relying on them.

---

## 1. What this is

**RackOps Lite** — "The Infrastructure Operating System": a homelab/fleet PaaS shipped as a
**single Go binary** (`rackops server | agent | probe | update` + API-client CLI verbs) with a
React dashboard embedded into the binary at build time. Anti-Kubernetes positioning: hardware
telemetry, intelligent workload placement, one-command Docker deploys, optional HA.

- Monorepo: **pnpm 9 workspaces + Turborepo** for TypeScript, **`go.work`** for Go (1.26.4).
- Go module: `github.com/rackops/rackops` (note: *not* the GitHub repo path).
- Current version: `0.4.0-dev` (`cmd/rackops/main.go:25`); released 0.2.0 / 0.3.0 (see CHANGELOG).

```
apps/
  rackops-lite/         The shipping product
    backend/            Go module — `rackops` binary (server / agent / probe)
                        + `rackops-edge` binary (vendor-run Front Door POP)
    dashboard/          React 19 + TS + Vite → emits into backend/internal/server/dist (go:embed)
    scripts/install.sh  Linux / WSL2 one-line installer
  rackops-enterprise/   Future product — placeholder, no code
  website/              rackops.dev marketing site (React+Vite+GSAP/three.js, nginx Docker)
internal-apps/crm/      Internal tooling — placeholder, no code
packages/
  api/                  @rackops/api — shared TS types/client (only the website consumes it)
  tsconfig/             @rackops/tsconfig — shared TS configs (base.json, react.json)
```

Key commands: see README.md. Dev loop: `pnpm --filter @rackops/dashboard-lite dev` (Vite :5173,
`/api` proxied to :8090) + `go run ./cmd/rackops server --listen :8090`.

---

## 2. Backend (Go) — `apps/rackops-lite/backend`

### 2.1 Binary & entry points

- **One executable**, subcommand dispatch at `cmd/rackops/main.go:32-48`:
  `server`, `agent`, `probe`, `update`, `version`, plus API-client CLI verbs
  `nodes | services | deploy | alerts | ask` (`cmd/rackops/cli.go:15`; server/token from
  `RACKOPS_SERVER`/`RACKOPS_TOKEN` or flags; `deploy` defaults `nodeId:"auto"`).
- `cmd/rackops-agent/`, `cmd/rackops-server/`, `internal/ingress/` are **empty leftover dirs**
  from an abandoned multi-binary layout — ingress actually lives in `internal/server/ingress.go`.
- An 18 MB compiled `rackops` binary is checked into `backend/` (should be removed/ignored).

### 2.2 Server bootstrap (`runServer`, main.go:114-244)

Store → join token load-or-create (16 random hex bytes at `<dataDir>/join-token`, 0600, printed
to logs with a copy-paste join command) → `MeshHub` (if `--mesh-port` > 0, default 51820) →
`server.API{Store, Hub, Mesh, JoinToken, JWTSecret, DashFS}` → node-down watchdog (15s deadline)
→ **embedded local agent** pointed at `127.0.0.1` unless `--no-local-agent` (server's own machine
is fleet node #1) → optional Raft HA → domain verifier loop → `http.ListenAndServe`.
Flags: `--listen :8080 --data-dir ~/.rackops --name --advertise --mesh-port --raft-addr
--raft-advertise --node-id --join`.

### 2.3 HTTP API surface (`internal/server/api.go:35-121`)

stdlib `http.ServeMux` with method+path patterns, wrapped `clusterGuard(authMiddleware(mux))`:

| Group | Routes |
|---|---|
| Agent protocol | `POST /api/agent/ingest` |
| Fleet | nodes, overview, events, alerts, join-command |
| Workloads | projects/envs/services CRUD, `{action}` (start/stop/restart/redeploy), clear-logs, node FS browser |
| Placement | `POST /api/placement`, `GET /api/suggest-port` |
| Templates/logs | catalog, service logs, deployments |
| Mesh/relay | status, dial, `/relay/{id}/{rest...}` (reverse proxy over mesh) |
| Domains/ingress | domains CRUD+verify, edge-probe, network diagnosis, ingress get/set, `GET /api/traefik/config`, per-service domain/expose/quick-tunnel |
| Live | `GET /api/stream` → SSE Hub (`sse.go`) |
| Auth/platform | auth status/signup/login, users, tokens, settings, assistant |
| Updates/cluster | version-check, update apply; cluster status, join |

SPA served at `/` with index.html fallback; `index.html` no-cache, `assets/` immutable
(`api.go:764-782`). Embed: `//go:embed all:dist` (`embed.go:8`), nil if only placeholder present.

### 2.4 Auth model (`internal/server/auth.go`)

- Hand-rolled **HS256 JWT** sessions, 7-day expiry, bcrypt passwords. First signup → Owner;
  later signups require admin/owner bearer. Roles: owner/admin/member/viewer (viewer = GET-only).
- **JWT secret = `sha256("rackops-jwt-v1:" + joinToken)`** (`auth.go:70-83`) so HA peers agree.
  ⚠ Anyone holding the agent join token can forge owner sessions; the token is also printed to
  startup logs. Falls back to persisted random secret only when no join token.
- **API keys**: `rk_` + 24 random bytes hex, stored SHA-256-hashed, secret shown once. No
  per-token RBAC; API tokens are rejected for self-update (owner/admin JWT only).
- Open paths (`auth.go:260-269`): non-`/api/`, `/api/auth/*`, `/api/agent/ingest`,
  `/api/traefik/config`, `/api/edge-probe`, `/api/cluster`, `/api/cluster/join`.
  **Setup mode**: with zero users the entire API is open. SSE accepts `?token=` query.
- Agent auth: shared join token via `X-Rackops-Token` header. Cluster join uses the same token.
- **No password-reset flow exists in this backend** (the support@rackops.in reset flow belongs
  to another lineage/product, not Lite).

### 2.5 State (`internal/server/store.go`)

All state (Nodes/Projects/Envs/Services/Deployments/Users/Tokens/Alerts/Domains/Ingress/
Settings/Events) in maps under one RWMutex; JSON snapshot to `<dataDir>/state.json` every 2s when
dirty (tmp+rename). Events capped at 500. **Task queue (nodeID→[]AgentTask) is in-memory only** —
queued tasks die with the process; reconcile/auto-heal partially compensates.

### 2.6 Agent protocol (pull-based, CGNAT-friendly)

- No server→agent connections. Agent loop (`internal/agent/agent.go:102-171`): every 2s POST
  `types.AgentReport` (telemetry; hardware re-inventoried every 60s; host info + hourly public-IP
  reflector; mesh pubkey; container list; buffered TaskResults) → response `types.AgentResponse`
  carries drained `Tasks`, optional `Mesh` assignment, `LeaderAddr`.
- **Registration is implicit**: server creates the Node on first report (`api.go:125-148`), role
  `worker`. Agent identity: 12-hex node ID persisted at `<dataDir>/node-id`.
- Task kinds (`types/types.go:502-516`): `deploy | stop | start | restart | remove | logs |
  listdir | tunnel-start | tunnel-stop` (+ declared-but-unimplemented `build`, `backup`).
- Ingest side-effects (`api.go:125-508`): container→service status sync, **adoption of external
  containers** as manageable services, reconcile/auto-heal missing containers (5-attempt budget,
  2-min spacing), sustained-alert evaluation (≥3 reports), SSE broadcast.
- Synchronous ops (logs, dir listing) block an HTTP handler on a channel keyed by task ID,
  waiting ≤10s for the next agent report (`templates_logs.go:23-58`).

### 2.7 Deploys & container runtime (`internal/agent/runtime.go`, `build.go`, `compose.go`)

- Runtime detect: docker preferred, then podman; degrades to telemetry-only if neither.
- `List()` reports three container classes: `rackops.service`-labeled, `rackops.tunnel`-labeled
  (don't drive service status), and **external** (adoption candidates). Stats via one
  `docker stats --no-stream`.
- **Deploy paths**: compose → `~/.rackops/stacks/<name>-<id>/compose.yml` + `.env`,
  `compose up -d --remove-orphans`; git → shallow clone (token embedded as `oauth2:<token>@`,
  redacted in logs), then **Dockerfile > nixpacks > auto-generated Dockerfile** from stack
  detection (Node/Go/Python/static); image → pull.
- Run args: labels, restart policy (default `unless-stopped`), host/container port split with
  `firstFreePort` fallback (scans up to 2000 ports), env, named volumes `rackops-<svcID>-<path>`,
  bind mounts, mem/CPU limits, `--gpus all`, host networking. Post-start: capture 40 log lines
  after 2s, fail deploy if not `running`. Actual host port returned via `TaskResult.HostPort`.
- Container naming: `rackops-<sanitized-name>-<serviceID>`; adopted services use `ExternalName`.
- `listDir` (`fs.go`): host folder browser; absolute paths only, blocks `/proc,/sys,/dev,/run`.

### 2.8 Templates (`internal/catalog/catalog.go`)

~50 one-click templates across `infra|homelab|ai|web|tools|games|database`. Schema: image or
embedded `ComposeYML` (compose wins → service becomes `KindCompose`), primary port + extras,
`EnvDef` form fields with `Secret/Required/Generate` (server generates 64-hex secrets),
volumes, `MemMB` (used by placement), GPU flag. Five full compose stacks embedded as consts:
Immich, WordPress+MariaDB, Umami, Listmonk, Plausible (+Postgres+ClickHouse). Compose services
are excluded from label-based reconcile.

### 2.9 Placement & alerts (`internal/server/placement.go`)

Scoring: **35% CPU + 30% mem + 20% thermal + 15% storage headroom**, hard constraints on
memory/GPU-VRAM/storage; `nodeId:"auto"` deploys to the winner. Alert engine: fixed thresholds
(CPU>90%, mem>90%, package temp>85°C, disk>92%, node down), sustained ≥3 reports.

### 2.10 Mesh networking (`internal/mesh` + `server/meshhub.go` + `agent/mesh.go`)

- **Userspace WireGuard** (wireguard-go + gvisor netstack): no root, no TUN, no port forwarding
  on members. Hub-and-spoke; server hub fixed at `10.77.0.1`, agents dial out
  (persistent-keepalive 25s). MTU 1420, UAPI text config.
- Hub: keys persisted at `<dataDir>/mesh-hub.key`; `HandleExchange` (called from ingest)
  allocates sequential IPs `10.77.x.y` from .0.2. ⚠ Peer map and `nextIP` are in-memory only —
  hub restarts reallocate IPs while `Service.RelayPort` persists → transient routing skew.
- Primitives: `Dial` (TCP-over-mesh), `ProxyPort` (netstack listener → host 127.0.0.1),
  `ProxyMesh` (hub-side spoke→spoke relay; needed because netstack doesn't IP-forward).
- Agent side reconciles mesh-exposed service ports + (ingress node only) **relay listeners**:
  plain TCP on `127.0.0.1:<LocalPort>` piping into the mesh — what Traefik routes to.

### 2.11 Ingress / domains / TLS (`internal/server/ingress.go`, `domains.go`)

- Edge = **Traefik v3.1 container** deployed as pseudo-service `ingress-traefik` to a chosen node
  (host networking), configured entirely by CLI flags incl. **HTTP provider** polling
  `http://<server>/api/traefik/config` every 5s. ACME (Let's Encrypt HTTP-01) + HTTP→HTTPS
  redirect when `ACMEEmail` set. Missing edge auto-redeploys.
- Dynamic config: one router+service per domain-assigned *running* service. Backend:
  `127.0.0.1:<Port>` if on the ingress node, else `127.0.0.1:<RelayPort>` (mesh relay).
  `X-Rackops-Edge: 1` middleware + synthetic `edge.rackops.internal` probe route distinguish
  "our Traefik" from port-squatters.
- **Relay path (two hops)**: Traefik → ingress-agent listener `127.0.0.1:<relayPort>` → hub
  `10.77.0.1:<relayPort>` (`ProxyMesh`) → `<node meshIP>:<svcPort>`. Ports allocated from 42000.
- **Domain verifier** (`domains.go:26-169`): every 30s until verified, hourly after; leader-only
  under HA. Pipeline: DNS lookup (wildcards probed via random label) → edge probe at resolved IPs
  with synthetic Host header → TLS cert probe via SNI (rejects Traefik placeholder cert).
  Nuanced verdicts: hairpin-NAT hints, LAN-only nip.io acceptance, misconfigured-A-record advice.
- **Zero-DNS expose**: `POST /api/services/{id}/expose` → `<name>.<ip-dashed>.nip.io`.

### 2.12 Cloudflare quick tunnels (`internal/server/tunnel.go`, `agent/runtime.go:417-458`)

`POST /api/services/{id}/quick-tunnel` queues `tunnel-start`; agent runs
`cloudflare/cloudflared:latest` (host net, named `rackops-qt-<svcID>`), scrapes the
`https://*.trycloudflare.com` URL from logs (≤60s). Result correlated via `pendingTunnels`
sync.Map into `Service.QuickTunnelURL`. Vanished tunnels auto-restart (heal budget; URL changes).
Positioned as testing-only — kept as a **fallback** behind a disclosure in the app's Networking
tab now that Front Door is the primary path.

### 2.12b RackOps Front Door (`internal/tunnel`, `internal/edge`, `cmd/rackops-edge`,
`agent/frontdoor.go`, `server/frontdoor.go`) — full detail in `docs/FRONT_DOOR.md`

First-party tunnel: permanent public HTTPS with no public IP, no DNS, no third party, CGNAT-safe.

- **Vendor POP** (`rackops-edge`, separate binary, same module): terminates a `*.link.rackops.in`
  wildcard (files on disk + 1-min hot reload; DNS-01 handled out of band by any ACME client),
  maps SNI/Host → tenant session, `httputil.ReverseProxy` with a `DialContext` that opens a
  tunnel stream instead of a TCP conn. Meters both directions, enforces a 100 GB/month free tier,
  instant kill switch. JSON registry, atomic flush, lazy calendar-month roll.
- **Transport** (`internal/tunnel`): QUIC/443 with a **5s probe** falling back to TLS+yamux/443
  (ALPN `rackops-fd/1`). One `Session` interface so nothing above branches on transport. TCP
  listener binds first, UDP pinned to its port (else `:0` splits the two).
- **Auth**: per-install Ed25519 (`<data-dir>/frontdoor.key`, 0600). POP sends a 32-byte challenge;
  client signs a domain-separated transcript binding nonce + tenant + **TLS exporter** (RFC 5705
  channel binding). Registry is authoritative for hostnames; the agent independently refuses
  streams outside its assignment.
- **Client** (in-process in the agent, no container — unlike cloudflared): supervised, exponential
  backoff + jitter, stops on permanent rejection (bad key / suspended / version).
- **Delivery**: `FrontDoorSpec` rides `AgentResponse` on every report (desired state, not a task —
  a task would be lost across an agent restart). State returns on `AgentReport.FrontDoor`.
- **Stacks on Traefik**: enabling sets `svc.Domain` + `svc.FrontDoor`, so the hostname becomes an
  ordinary router. ⚠ Two load-bearing details: Front Door routers get **no certResolver** (the
  customer can't validate a vendor name; failures would burn LE rate limits) and use a dedicated
  **`frontdoor` entrypoint on `127.0.0.1:8099`** (`types.FrontDoorEntryPort`) because `web`'s
  blanket HTTP→HTTPS redirect would loop the tunnel infinitely. Both are tested.
- Handlers are `serviceForOrg` + `ManageDomains` scoped — deliberately not repeating the
  quick-tunnel org hole from the audit. `RunFrontDoorReconciler` (leader-only) retries enrollment.

### 2.13 HA / Raft (`internal/raftstore`, `server/cluster.go`)

- **Snapshot-granular replication**: leader proposes the *entire store state as one JSON blob*
  on every mutation ("small at Lite scale"). hashicorp/raft + raft-boltdb at
  `<dataDir>/raft/raft.db`, file snapshots (retain 2), 30s snapshot interval.
- Enable with `--raft-addr`; `Bootstrap` when no `--join`. Node ID defaults `srv-<raft-advertise>`.
  Server records its HTTP advertise addr into replicated `Store.ClusterHTTP[id]`.
- **Join flow**: joiner retries `POST /api/cluster/join` (same join token) up to 30×2s, following
  421 redirects carrying the real leader's address.
- `clusterGuard`: followers reject non-GET with a leader pointer, **except**
  `/api/agent/ingest`, `/api/cluster/join`, `/api/auth/login` (login works during failover).
  FSM applies imports only on followers (leader's store is authoritative). Sessions survive
  failover because the JWT secret derives from the shared join token.

### 2.14 Telemetry (`internal/telemetry`) — Linux-only by design

Direct `/proc` + `/sys` reads, no deps, no root; fails soft to zeros elsewhere (macOS dev host
reports empty hardware). CPU% from `/proc/stat` deltas; mem from `MemTotal-MemAvailable`;
hottest package temp via hwmon scan (prefers `k10temp|coretemp|zenpower|cpu_thermal`);
drives from `/sys/block` (nvme/ssd/hdd typing, live MB/s from sector deltas, NVMe temps,
usage health >85% warn / >95% crit); NICs (skips lo/veth/br-/docker); GPUs: NVIDIA via
`nvidia-smi` CSV, AMD/Intel via `/sys/class/drm` (+hwmon, lspci fallback). Public IP via
ipify → checkip.amazonaws → icanhazip.

### 2.15 Notifications (`internal/notify`) & AI assistant (`server/assistant.go`)

Notify: stateless fan-out to Discord (embeds w/ severity color), Slack, Telegram, generic
webhook; fired on deploy success/failure + alert raise; targets from replicated settings.
Assistant ("AI Co-Pilot"): Anthropic/Gemini/OpenAI-compatible/Ollama providers + builtin rule
engine, grounded on fleet state; CLI verb `rackops ask`.

### 2.16 Self-update (`internal/server/update.go`)

`GET /api/version-check`: latest tag scraped from GitHub `/releases/latest` redirect (no API
rate limit), cached 6h; repo `msr157/rackops`. `POST /api/update` (owner/admin JWT only):
downloads `rackops-linux-<arch>`, verifies it runs `version`, atomic-rename over the executable,
`syscall.Exec` restart (Linux only). CLI `rackops update` shares the same path.

### 2.17 Shared types (`internal/types/types.go`)

`Node` (roles server/worker; status `online|offline|joining`; LAN+mesh+public IPs; `HasPublic`
marks relay/ingress eligibility) · `Hardware/GPU/Drive/NIC/Metrics` · `Project/Environment/EnvVar`
· `Service` (kind app/database/compose/template; source git/image/compose/template; adoption
fields `Adopted`/`ExternalName`; placement `NodeID`/`AutoPlace`/`RelayPort`; host `Port` vs
`ContainerPort`; `IngressType` none/static/relay; `QuickTunnelURL`; limits) · `Deployment` ·
`Alert/Event` · `Domain` (full verification state: `EdgeOK`, `CertOK`, issuer/expiry) ·
`IngressConfig` · `User`/`APIToken` (hashes replicate via raft; `Sanitized()` for API) ·
wire protocol `AgentReport`/`AgentResponse{Tasks, Mesh, LeaderAddr}`/`MeshInfo`/`RelaySpec`/
`ContainerStatus`/`TaskKind`.

### 2.18 End-to-end data flow

1. Deploy request (UI/CLI) → server creates Service (template expansion, secret generation,
   auto-placement, port dedup) + queues `AgentTask{deploy}`.
2. Agent's next 2s ingest delivers telemetry + container truth; response drains its task queue
   + mesh assignment.
3. Agent deploys via docker/podman; reports `TaskResult{ContainerID, HostPort}` next push;
   server reconciles status, auto-heals, adopts externals.
4. Domain routing: service port → node agent `ProxyPort` onto mesh → hub `ProxyMesh` relay →
   ingress agent's local listener → Traefik router (from `/api/traefik/config`).
5. Under HA, every store mutation on the leader re-proposes full state JSON to followers.

---

## 3. Dashboard — `apps/rackops-lite/dashboard` (`@rackops/dashboard-lite`)

- **Stack**: React 19 + TS + Vite 8; deps are only `react-router-dom@7` + `lucide-react`.
  No UI kit, no CSS framework, no state library. Lint = oxlint; `build` = `tsc -b && vite build`.
- **Build handoff**: `vite.config.ts` → `outDir: '../backend/internal/server/dist'`
  (`emptyOutDir`), consumed by `go:embed`. Dev proxy `/api` → `:8090`. Treat dist as generated.
- **Routing** (`App.tsx:148-158`): `/` Home · `/apps` · `/apps/new` (DeployFlow) ·
  `/apps/:id/:tab?` (overview|networking|logs|deployments|settings) · `/machines[/:id]` ·
  `/network` · `/settings` · `*`→`/`. Auth gate via `api.authStatus()` + localStorage token;
  global `rackops-logout` window event on 401.
- **Pages**: Auth (first-run → owner signup) · Home ("Is everything okay?" hero, setup checklist,
  needs-attention list, live event terminal, `<Rack/>`) · Apps (failing-first sort, inline
  actions) · AppDetail (draft/PATCH editing with dirty detection via JSON baseline; 4s status
  poll that never clobbers the draft; networking tab = nip.io / quick tunnel / own domain;
  settings = env, mounts w/ remote FS browser, limits, migration, danger zone) · DeployFlow
  (3-step wizard, placement card w/ score+reasons, live build log to "make it public" payoff) ·
  Machines (CPU sparkline from module-level rolling Map) · Network (edge config, domains w/
  registrar record cards, mesh/HA status, public-access wizard) · Settings (Copilot provider,
  webhooks, API tokens).
- **Design system** — all in `src/index.css` ("an operator's console"; EnvKit/Cloudflare taste):
  tokens at `:root:11-49` — near-black chassis `--bg #0f1013`→`--bg-3`, hairline white-alpha
  borders ("borders over shadows"), single phosphor-amber accent `--amber #ffb02e`, LED colors
  `--ok/--crit/--warn/--gpu`, 3-step ink `--text/--dim/--faint`, Space Grotesk display / Inter
  body / IBM Plex Mono telemetry. Semantic classes: `.card .kpi-* .badge .led .rack .rack-unit
  .terminal .btn .input .tabs .modal .drawer .palette .toast .stepper .update-banner`.
  Status language centralized in `ledClass()` (`components/ui.tsx:8-19`);
  `publicUrls()` (`ui.tsx:125-135`) is the single source of truth for "where is this reachable".
- **State**: four nested providers — Toast → Confirm (promise-based dialog) → **DataProvider**
  (one fetch + one SSE stream for all fleet state; upserts on `node_metrics`/`service_update`,
  full refresh on `node_joined`/`node_down`; 8s overview poll as SSE fallback) → Copilot drawer.
  Page-local `useState` + targeted polling. localStorage: session token, checklist dismissal.
- **API client**: hand-rolled `src/api.ts` (~45 endpoints; types hand-mirrored from Go —
  **does NOT use `@rackops/api`**). SSE via `EventSource('/api/stream?token=…')`.
- **Dead code**: `pages/Changelog.tsx` is not routed and links to `/organizations`, `/profile`,
  `/swagger` — routes that don't exist in this app. Hardcoded releases through v0.3.0.

---

## 4. Website — `apps/website` (`@rackops/website`)

- Single-page React 19 + Vite 6 marketing site: 9 scroll sections composed in `App.tsx`,
  GSAP/ScrollTrigger + Lenis smooth-scroll + one react-three-fiber WebGL hero.
- **Theme** (`styles/theme.css:4-36`): terminal-industrial — `--bg-0 #050706`, single signal
  green `--signal #3df582`, `--warn #ffb224`; Space Grotesk + JetBrains Mono. `@` alias →
  `src/website`; manualChunks split `three` / `r3f` / `motion`.
- **Deploy**: two-stage Dockerfile, **must build from repo root** (pnpm workspace):
  node:22-alpine build (`pnpm install --frozen-lockfile --filter @rackops/website...`) →
  nginx:1.27-alpine serving `dist/` with the repo's full `nginx.conf` (gzip, security headers,
  1-year immutable asset cache, `/health` JSON, SPA fallback, dotfile deny).
  ⚠ HEALTHCHECK uses `curl`, which nginx:alpine doesn't ship — use busybox `wget -q --spider`.
- CTA form POSTs an early-access request with its own fetch + hardcoded URL (`CTA.tsx`),
  bypassing `@rackops/api`'s client (only its `EarlyAccessRequest` type is imported).

## 5. Shared packages

- **`packages/api` (`@rackops/api`)**: source-only (`exports: ./src/index.ts`, no build);
  exports `types.ts` (Snapshot/Node/Meta/EarlyAccessRequest…), `client.ts`
  (`fetchNodes`, `fetchMeta`), `useNodes.ts` polling hook. **Only consumer: the website**, and
  only the `EarlyAccessRequest` type. ⚠ Drift: header says "KEEP IN SYNC with
  internal/api/types.go" (path doesn't exist — real file is
  `apps/rackops-lite/backend/internal/types/types.go`); TS `NodeStatus 'online'|'stale'` vs Go
  `online|offline|joining`; TS `Node` shape matches neither the Go model nor any consumer.
- **`packages/tsconfig`**: `base.json` (ES2022, bundler resolution, strict, noEmit) +
  `react.json` (DOM libs, react-jsx). Extended by website + packages/api.
- Workspace globs (`pnpm-workspace.yaml`): website, dashboard-lite, `packages/*`; enterprise
  dashboard + `internal-apps/*` commented out as future workspaces.

---

## 6. Installer — `apps/rackops-lite/scripts/install.sh`

One-liner: `curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash`

Flow: `set -eu`; `RACKOPS_PORT` env override (default 8080); amber-on-black ANSI UI with braille
spinner (`step()`), gated on tty/`NO_COLOR` → **root required** (uid 0 or die with sudo
instructions); Linux + macOS (Windows → WSL2 README anchor); asset map `rackops-<os>-<arch>`
(linux/darwin × amd64/arm64) → download from GitHub releases-latest, `install -m 0755` to
`/usr/local/bin/rackops` → prereqs via package-manager autodetect (apt/dnf/yum/apk/pacman/zypper):
**git** (warn-only on failure), **container runtime** (accepts running docker *or podman*;
starts stopped docker via systemd or `nohup dockerd` on WSL2; installs via get.docker.com if
absent; adds `$SUDO_USER` to docker group; non-fatal — telemetry works without deploys),
**compose plugin** (warn-only) → launch: systemd unit `/etc/systemd/system/rackops.service`
(`ExecStart=… server --listen :$PORT`, `Restart=always`, `After=docker.service`) or `setsid`
detached with logs at `/root/.rackops/server.log` → health-poll `/api/auth/status` 30×1s
(warn on timeout) → payoff card (LAN URLs, "create your owner account", "re-run to update").

Notes: container **adoption is server/agent behavior, not an installer step**; no port-conflict
detection (systemd would loop-restart on a taken :8080).

---

## 7. Versions & changelog (`apps/rackops-lite/CHANGELOG.md`)

- **Unreleased**: port-forwarded-router public path recognition; DNS-copy buttons on plain-HTTP
  LAN dashboards; in-dashboard changelog; owner/admin server-restart control.
- **v0.3.0 (2026-07-12)**: orgs/member roles, API keys + Swagger + MCP server, one-click + CLI
  updates, account recovery, support tickets, Needs Attention view. ⚠ Several of these have
  **no Lite backend code** — see §9.
- **v0.2.0 (2026-07-12)**: installer + prereqs, container adoption, multi-service templates,
  app-first dashboard rebuild, self-healing, correct port mapping.
- Both dated the same day — consistent with `production`'s history landing as milestone commits.

---

## 8. Git remotes — TWO DISJOINT LINEAGES

Remote: `origin = https://github.com/msr157/rackops.git`. **`origin/main` and `production`
share no common ancestor** (independent roots; even authors are disjoint — prototype: msr157,
production: Nikish).

| Branch | Tip | Commits | Dates | What it is |
|---|---|---|---|---|
| `origin/production` (= local HEAD) | `c8acf51` | 26 | Jul 9–15 | **The real product.** Milestone commits M1–M6, installer/orgs work, turborepo restructure |
| `origin/main` | `21f7aeb` | 40 | Jun 27–Jul 12 | **Old prototype** → live early-access marketing site (React/Three.js website redesign, Go early-access API `821c43b` + Postgres, Dokploy deploys) |
| `origin/development` = `origin/qa` | `45f2a7d` (identical) | 16 | Jun 27–29 | Pre-monorepo static site + Gitea CI experiments; strict ancestors of main; abandoned |

Operational gotchas:

1. **`production` has ZERO CI/deploy.** No `.github/`, no `.gitea/`, no root Dockerfiles.
   All CI lives on the prototype lineage: `origin/main` has `.github/workflows/ci.yml`
   (pnpm/turbo lint+build, go vet/build/test, website-image gate) with Dokploy deploying `main`
   via its GitHub webhook. dev/qa carry the older `.gitea/workflows/ci.yml` (Dokploy webhook via
   `DOKPLOY_WEBHOOK_URL`) + `mirror.yml` (mirror to `gitea.rackops.in`). Pushing `production`
   builds and deploys **nothing**.
2. **Local `main` (`1b8ead8`) is NOT `origin/main`** — it's a stale unconfigured pointer into
   *production's* history (16 behind origin/production). Fetch/check refs before trusting it.
3. `apps/website` on production is byte-identical to main's except production adds
   `apps/website/Dockerfile` — the website was lifted from main at `c8acf51`.
4. `origin/main` is what serves the live early-access site today.

---

## 9. Known issues / risks (as of c8acf51)

**Security**
- **JWT secret derived from the join token** (`server/auth.go:70`) — the agent-enrollment
  credential can mint owner sessions; token printed to startup logs. Highest-priority item.
- Setup mode (zero users) leaves the **entire API open** until first signup.
- `/api/traefik/config` unauthenticated → leaks service names/domains/backend ports.
- One shared join token authenticates agents, ingest, and cluster join; no per-node credentials
  or revocation.
- `handleDeleteToken`/`handleListUsers` lack per-handler role checks beyond viewer-GET —
  any member can delete API tokens or list users.

**Truth-in-docs**
- v0.3.0 changelog claims orgs, API-key UI, Swagger, MCP server, support tickets — **no Lite
  backend code exists for these**; the strings appear only in the dashboard's dead
  `Changelog.tsx`. No password-reset flow exists either.
- README lists M6 HA as "roadmap" yet documents it as shipping below; code sides with shipping —
  the roadmap line is stale. "What works today (M1)" also lags two releases.

**Consistency / drift**
- Three hand-maintained type surfaces for one backend: Go `internal/types`, dashboard `api.ts`,
  and `packages/api` (already diverged; stale sync-comment path).
- `Changelog.tsx` unreachable + broken links; either route it with fixed links or delete.

**Durability / housekeeping**
- Task queue volatile → queued-never-sent deploys stuck `building` until a heal cycle notices.
- Mesh hub IP allocations not persisted → transient routing skew after server restart.
- `agent.Run` has no context/shutdown path (mesh/relays close only on process exit).
- 18 MB compiled `rackops` binary checked into `backend/`; every go.mod dep marked `// indirect`
  (`go mod tidy` never run); empty `cmd/rackops-agent`, `cmd/rackops-server`, `internal/ingress`
  dirs mislead newcomers.
- Website Dockerfile HEALTHCHECK uses `curl` (absent in nginx:alpine).

---

## 10. Conventions (summary — see AGENTS.md for the full text)

Go: gofmt, tabs, platform code in `*_linux.go`, table-driven `*_test.go` beside the package.
TS: 2-space, PascalCase components, explicit types at API boundaries. Commits: short imperative
sentence-case subjects. Never commit API keys / join tokens / state dirs. Dashboard `dist` is
generated output. Project instruction (CLAUDE.md): **no patchwork — production-ready code only.**
