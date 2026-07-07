# RackOps

> **The Infrastructure Operating System** — One Dashboard. One Control Plane. One Platform.

RackOps unifies servers, clusters, networking, GPUs, and workloads into a single intelligent
control plane. A node agent reads the hardware on every machine; the control plane shows the
fleet and decides where work should run. See [`docs/product/`](docs/product/) for the full
product vision and roadmap.

Two products, one codebase:

| Product | For | Highlights |
|---|---|---|
| **RackOps Lite** | Homelabs, developers, small teams (works behind CGNAT) | One-click deploys, hardware monitoring, storage viz, alerts, AI assist |
| **RackOps Enterprise** | Organizations running production infrastructure | Multi-server management, clusters, RBAC & SSO, GPU scheduling, audit logs |

## Monorepo layout

```
cmd/                     Go binaries: agent, rackops-lite, rackops-enterprise
internal/                Shared Go: api (wire types), hwinfo, agent, server, enterprise
apps/
  website/               Marketing site (React + Vite + Three.js) — deployed via Dokploy
  dashboard-lite/        RackOps Lite dashboard
  dashboard-enterprise/  RackOps Enterprise dashboard
packages/
  ui/                    Design tokens + shared React components
  api/                   TS wire types + API client + hooks
  tsconfig/              Shared TypeScript configs
docs/
  product/               Product vision, overview, pitch deck
  ops/                   Deployment cluster notes (Swarm, Dokploy, networking)
```

## Quickstart (MVP)

Requirements: Go ≥ 1.22, Node ≥ 22, pnpm.

```bash
pnpm install

# Terminal 1 — control plane (REST API on :8080)
pnpm dev:lite

# Terminal 2 — node agent (reports this machine's CPU/RAM/disk/network)
pnpm dev:agent

# Terminal 3 — dashboard (http://localhost:5173)
pnpm --filter @rackops/dashboard-lite dev
```

You'll see this machine appear as a live node card with real hardware stats.

Enterprise variant: `pnpm dev:enterprise` (:8081) + `pnpm --filter @rackops/dashboard-enterprise dev` (:5174).

## Development

```bash
pnpm lint        # typecheck every JS workspace (tsc --noEmit)
pnpm build       # build website + dashboards
pnpm go:test     # Go tests
pnpm go:build    # compile all Go binaries
```

Website production image (built from repo root):

```bash
docker build -f apps/website/Dockerfile -t rackops-website .
```

## Status

Early MVP (roadmap Phase 1 — Agent & Discovery). Working today: agent → control plane →
dashboard live hardware telemetry. Not yet implemented (stubbed, not faked): auth,
persistence, mesh networking, clusters, RBAC, GPU scheduling.

## CI/CD

GitHub Actions on `main` / `qa` / `development`: typecheck+build all workspaces, Go
vet/build/test, and a production Docker build of the website. Pushes to `main` are deployed
automatically by Dokploy via its GitHub webhook onto a 3-node Docker Swarm
(see [`docs/ops/`](docs/ops/)).

## License

[MIT](LICENSE) © 2026 RackOps Contributors
