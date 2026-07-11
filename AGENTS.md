# Repository Guidelines

## Project Structure & Module Organization

RackOps Lite ships as one Go binary with an embedded web dashboard. Go CLI entry points are in `backend/cmd/rackops`; domain packages such as `server`, `agent`, and `telemetry` are under `backend/internal`. The React/TypeScript dashboard is in `dashboard/src`, with reusable UI in `components`, route views in `pages`, and static files in `dashboard/public` or `dashboard/src/assets`. `scripts/install.sh` is the Linux/WSL installer. Dashboard builds go to `backend/internal/server/dist`; treat it as generated output.

## Build, Test, and Development Commands

- `cd dashboard && npm ci` installs the locked frontend dependencies.
- `cd dashboard && npm run dev` starts Vite with HMR; `/api` is proxied to `localhost:8090`.
- `cd dashboard && npm run lint` runs Oxlint over the dashboard.
- `cd dashboard && npm run build` type-checks and builds embedded assets.
- `cd backend && go test ./...` compiles and tests all Go packages.
- `cd backend && go build -o rackops ./cmd/rackops` builds the executable; build the dashboard first for UI changes.
- `cd backend && ./rackops server --listen :8090` runs a backend compatible with the Vite proxy.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs, concise package names, exported `PascalCase`, and unexported `camelCase` identifiers. Keep platform-specific implementations in files such as `exec_linux.go`. For TypeScript, use two-space indentation, `PascalCase` component files, `camelCase` functions, and explicit types at API boundaries. Follow existing component and CSS patterns.

## Testing Guidelines

Place Go tests beside their package as `*_test.go` and favor table-driven cases. There is no committed frontend test framework; if adding one, colocate `*.test.tsx` files and add its command to `dashboard/package.json`. At minimum, run Go tests, dashboard lint, and the production build. Manually exercise flows spanning server, agent, or browser processes.

## Commit & Pull Request Guidelines

Recent commits use short, imperative, sentence-case subjects, for example `Add one-line installer for Linux / Windows-WSL2`. Keep commits focused. Pull requests should describe user-visible effects, link issues, list validation, and call out configuration or migration impacts. Include screenshots for dashboard changes and sample CLI/API output for interface changes.

## Security & Configuration

Never commit API keys, join tokens, local state directories, or deployment credentials. Use placeholders such as `rk_your_api_key` in documentation and keep development state outside the repository.
