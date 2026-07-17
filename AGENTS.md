# Repository Guidelines

## Project Structure & Module Organization

This is the **RackOps monorepo**: pnpm workspaces + Turborepo for TypeScript, `go.work` for Go.

- `apps/rackops-lite/` — the shipping product. Go CLI entry points are in `backend/cmd/rackops`; domain packages such as `server`, `agent`, and `telemetry` are under `backend/internal`. The React/TypeScript dashboard is in `dashboard/src`, with reusable UI in `components`, route views in `pages`, and static files in `dashboard/public` or `dashboard/src/assets`. `scripts/install.sh` is the Linux/WSL installer. Dashboard builds go to `backend/internal/server/dist`; treat it as generated output.
- `apps/website/` — rackops.dev marketing site (React + Vite + GSAP/three.js), deployed as an nginx Docker image built from the repo root.
- `apps/rackops-enterprise/` — future product; placeholder only, no code yet.
- `internal-apps/crm/` — the vendor-side CRM (Go + SQLite backend in `backend/`, React dashboard in `dashboard/`). Receives support tickets, usage snapshots, and password-reset relays from customer installs over its `/ingest/v1/*` API; support agents answer tickets from its admin dashboard. Module `github.com/rackops/crm`; dashboard workspace `@rackops/crm-dashboard`.
- `packages/api` (`@rackops/api`) — shared TS API types/client; `packages/tsconfig` (`@rackops/tsconfig`) — shared TS configs.

## Build, Test, and Development Commands

- `pnpm install` at the repo root installs every JS workspace (pnpm 9, `corepack enable`).
- `pnpm build` / `pnpm lint` run the corresponding Turborepo task across all workspaces.
- `pnpm --filter @rackops/dashboard-lite dev` starts Vite with HMR; `/api` is proxied to `localhost:8090`.
- `pnpm --filter @rackops/dashboard-lite build` type-checks and emits into the Go embed dir.
- `pnpm --filter @rackops/website dev|build` for the marketing site.
- `cd apps/rackops-lite/backend && go test ./...` compiles and tests all Go packages.
- `cd apps/rackops-lite/backend && go build -o rackops ./cmd/rackops` builds the executable; build the dashboard first for UI changes.
- `cd apps/rackops-lite/backend && ./rackops server --listen :8090` runs a backend compatible with the Vite proxy.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs, concise package names, exported `PascalCase`, and unexported `camelCase` identifiers. Keep platform-specific implementations in files such as `exec_linux.go`. For TypeScript, use two-space indentation, `PascalCase` component files, `camelCase` functions, and explicit types at API boundaries. Follow existing component and CSS patterns.

## Testing Guidelines

Place Go tests beside their package as `*_test.go` and favor table-driven cases. There is no committed frontend test framework; if adding one, colocate `*.test.tsx` files and add its command to the workspace's `package.json`. At minimum, run Go tests, dashboard lint, and the production build. Manually exercise flows spanning server, agent, or browser processes.

## Commit & Pull Request Guidelines

Recent commits use short, imperative, sentence-case subjects, for example `Add one-line installer for Linux / Windows-WSL2`. Keep commits focused. Pull requests should describe user-visible effects, link issues, list validation, and call out configuration or migration impacts. Include screenshots for dashboard changes and sample CLI/API output for interface changes.

## Security & Configuration

Never commit API keys, join tokens, local state directories, or deployment credentials. Use placeholders such as `rk_your_api_key` in documentation and keep development state outside the repository.
