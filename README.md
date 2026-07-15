# RackOps Monorepo

**The Infrastructure Operating System** — one control plane for every machine in your fleet.

## Repository layout

```
apps/
  rackops-lite/         RackOps Lite — the shipping product
    backend/            Go module — single `rackops` binary (server / agent / probe)
    dashboard/          React + TypeScript + Vite (embedded into the binary at build time)
    scripts/            install.sh — Linux / WSL2 one-line installer
  rackops-enterprise/   RackOps Enterprise — future product (placeholder)
  website/              rackops.dev marketing site (React + Vite, deployed via Docker/nginx)
internal-apps/          Internal tooling (CRM, etc.) — placeholders until built
packages/
  api/                  @rackops/api — shared TS API types + client hooks
  tsconfig/             @rackops/tsconfig — shared TypeScript configs
```

- **JavaScript/TypeScript** is managed as a [pnpm workspace](https://pnpm.io/workspaces) with [Turborepo](https://turbo.build) for task running.
- **Go** is managed via `go.work` at the root, pointing at each app's Go module.

## Prerequisites

- Node 22+ and pnpm 9 (`corepack enable`)
- Go 1.26+

## Common tasks

```sh
pnpm install        # install every JS workspace
pnpm build          # build all JS apps (dashboard build lands in the Go embed dir)
pnpm lint           # lint/typecheck all JS workspaces

pnpm go:build       # go build ./... (all Go modules via go.work)
pnpm go:test        # go test ./...
```

### RackOps Lite

```sh
# Dev loop
pnpm --filter @rackops/dashboard-lite dev   # Vite on :5173, /api proxied to :8090
cd apps/rackops-lite/backend && go run ./cmd/rackops server --listen :8090

# Production build — dashboard first (it emits into the Go embed dir), then the binary
pnpm --filter @rackops/dashboard-lite build
cd apps/rackops-lite/backend && go build -o rackops ./cmd/rackops
```

See [apps/rackops-lite/README.md](apps/rackops-lite/README.md) for the full product docs (install one-liner, HA, roadmap).

### Website

```sh
pnpm --filter @rackops/website dev      # local dev
pnpm --filter @rackops/website build    # production build

# Production image (build from the repo root — the pnpm workspace needs it)
docker build -f apps/website/Dockerfile -t rackops-website .
```
