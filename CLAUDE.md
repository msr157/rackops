# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

The **RackOps monorepo**. RackOps is "The Infrastructure Operating System" — a control plane + node-agent platform (see `docs/product/` for vision, pitch deck, and roadmap). Two products share one codebase — **RackOps Lite** (homelabs/CGNAT) and **RackOps Enterprise** (orgs, RBAC/clusters) — plus the marketing **website**.

Three languages of structure:
- **Go** (single module `github.com/msr157/rackops` at repo root): `cmd/` binaries + `internal/` shared code.
- **TypeScript** (pnpm workspaces + Turborepo): `apps/*` and `packages/*`.
- **Docs**: `docs/product/` (product vision — treat as source of truth for what RackOps *is*), `docs/ops/` (deployment cluster notes).

## Commands

```bash
pnpm install               # install all JS workspaces
pnpm lint                  # turbo run lint → tsc --noEmit in every workspace (the ONLY lint; no ESLint)
pnpm build                 # turbo run build → website + both dashboards
pnpm go:build / go:test    # go build ./... / go test ./...

# Run the MVP stack (3 terminals):
pnpm dev:lite              # control plane on :8080  (go run ./cmd/rackops-lite)
pnpm dev:agent             # agent reporting this machine to :8080
pnpm --filter @rackops/dashboard-lite dev    # dashboard on :5173, /api proxied to :8080

pnpm dev:enterprise        # enterprise control plane on :8081
pnpm --filter @rackops/dashboard-enterprise dev   # :5174, /api → :8081
pnpm --filter @rackops/website dev                # marketing site
```

There is no JS test runner. Go tests exist (`internal/hwinfo`). Typecheck (`pnpm lint`) is the compile gate for TS.

## Architecture

### Data flow (the core of the product MVP)

`cmd/agent` samples hardware every N seconds via `internal/hwinfo` (gopsutil: CPU/RAM/disks/network/OS) → `POST /api/v1/report` to a control plane → `internal/server` stores nodes in memory (stale after 15s without a report) → dashboards poll `GET /api/v1/nodes` every 5s via the `useNodes` hook and render `NodeCard`s.

**`internal/api/types.go` is the single source of truth for the wire format.** `packages/api/src/types.ts` hand-mirrors it — when you change one, change the other (both files carry a reminder comment).

### Lite vs Enterprise divergence

Both control planes are thin `cmd/` mains over `internal/server.New(Options{Edition, DashboardDir, Extra})`. Enterprise passes `internal/enterprise.Routes` which registers **501 stubs** (`/api/v1/clusters`, `/api/v1/rbac/roles`) — that package is where enterprise features (clusters, RBAC/SSO, GPU scheduling…) will actually live. The dashboards mirror this: `dashboard-enterprise` adds a nav rail of "coming soon" entries. There is deliberately **no auth, no DB, no mesh networking yet** — don't fake them; extend the stubs.

Agent identity is a UUID persisted at `os.UserConfigDir()/rackops-agent/node-id`.

### JS workspace layout

- `packages/tsconfig` — shared tsconfig bases (`base.json`, `react.json`); every workspace extends these.
- `packages/api` — wire types + fetch client + `useNodes` polling hook.
- `packages/ui` — design tokens (`styles/tokens.css`, CSS custom properties used by ALL apps) + shared components (`NodeCard`, `MetricBar`, `StatusDot`). Consumed **as source** (`exports` → `./src/index.ts`, no build step); Vite compiles it inside each app.
- Dashboards import from `@rackops/ui` / `@rackops/api`; keep shared UI there, not in the apps.

### Website (`apps/website`)

Single-page marketing site (React 19 + Vite + Three.js). Alias `@` → `src/website` (both `vite.config.ts` and `tsconfig.json` — keep in sync). Animation system: gsap/ScrollTrigger and Lenis must be imported via the singletons in `src/website/lib/` (never raw), sections use `gsap.context` + `ctx.revert()` cleanup, and **reduced-motion is a first-class path** (`initLenis()` returns null, `SceneCanvas` lowers DPR). In r3f scenes, `<line>` JSX collides with the SVG type — build `THREE.Line` in `useMemo` and render `<primitive>`.

## Build & deployment

Only the **website** deploys today: `apps/website/Dockerfile`, **built from the repo root** (`docker build -f apps/website/Dockerfile .`) because the pnpm workspace needs root manifests. Manifest-COPY-then-install keeps layer caching; if you add a workspace package the website depends on, add its `package.json` COPY to the Dockerfile. Runtime is nginx:1.27-alpine with SPA fallback and `/health` (used by HEALTHCHECK).

CI (`.github/workflows/ci.yml`, push to main/qa/development): three parallel jobs — `web` (turbo lint+build), `go` (vet/build/test), `docker-website` (image build = deploy gate). **Dokploy deploys `main` automatically via its GitHub webhook**; its dashboard is configured with Docker File `apps/website/Dockerfile` and Docker Context Path `.`.

Deploy infra (context for `docs/ops/`): Dokploy on a 3-node Docker Swarm, images via local registry `127.0.0.1:5000`, ingress Cloudflare Tunnel → Traefik, nodes on a Netbird overlay.
