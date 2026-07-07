# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

RackOps is a **single-page marketing/landing website** for an infrastructure-orchestration product. Despite the infra-heavy documentation, the source code (`src/`) is purely a client-side React site — there is no backend, API, or database in this repo. It is built into static files and served by nginx.

> Note: `README.md` is **stale**. It describes an older vanilla HTML/CSS/JS site (`src/index.html`, `style.css`, `app.js`) and a GitHub→Gitea mirror pipeline. Both are gone. The repo is now React 19 + TypeScript + Vite + Three.js, and CI runs directly on GitHub Actions (see commits `05fc604`, `f26b454`). Trust the code over the README.

## Commands

```bash
npm run dev       # Vite dev server (hot reload)
npm run build     # tsc --noEmit (typecheck) then vite build → dist/
npm run lint      # tsc --noEmit — this is the ONLY lint; there is no ESLint
npm run preview   # serve the production build locally
```

There is **no test runner** configured. CI calls `npm run test` but tolerates its absence — do not assume tests exist.

The single source of truth for "does this compile" is `tsc --noEmit` (strict TypeScript). Run `npm run lint` after changes.

## Architecture

Entry: `index.html` → `src/main.tsx` → `src/App.tsx`. `App.tsx` renders `Navbar`, a fixed sequence of section components inside `<main>`, and `Footer`.

All real code lives under **`src/website/`**, organized by role:
- `components/layout/` — Navbar, Footer
- `components/sections/` — the page's vertical sections (Hero, Problem, Solution, Architecture, Features, AI, CTA). Each section is a `.tsx` + co-located `.css`. Adding a section = new folder pair + import into `App.tsx`.
- `three/` — the WebGL background (`SceneCanvas.tsx`), a react-three-fiber scene of animated "server rack" meshes used behind the Hero.
- `hooks/` — `useScrollReveal` (IntersectionObserver → toggles `.visible` on `.reveal` elements), `useReducedMotion`.
- `lib/` — singleton wrappers for third-party animation libs (see below).
- `styles/` — `tokens.css` (design tokens / CSS custom properties) and `global.css`. Section CSS relies on these tokens.

**Import alias:** `@` → `src/` (configured in both `vite.config.ts` and `tsconfig.json`). Imports look like `@/components/sections/Hero`, `@/lib/gsap`. Keep both files in sync if you change the alias.

### Animation system (the non-obvious part)

Three libraries cooperate and must be initialized through the shared singletons in `src/website/lib/`, never imported raw in components:

- **`lib/gsap.ts`** — imports gsap, registers the `ScrollTrigger` plugin once, and re-exports both. Always import gsap/ScrollTrigger from here so the plugin is guaranteed registered.
- **`lib/lenis.ts`** — smooth-scroll singleton. `initLenis()` wires Lenis's scroll event into `ScrollTrigger.update` so scroll-triggered animations stay in sync. `App.tsx` calls `initLenis()` on mount and `destroyLenis()` + kills all ScrollTriggers on unmount.
- Section components use `gsap.context(...)` inside `useEffect` and `return () => ctx.revert()` for cleanup (see `Hero.tsx`).

**Reduced-motion is a first-class path, not an afterthought.** `initLenis()` returns `null` when `prefers-reduced-motion: reduce`, `SceneCanvas` lowers DPR and stops the scene rotation, and animations should degrade gracefully. Preserve this when touching animation code.

## Build & deployment

Production is a two-stage Docker build (`Dockerfile`): `node:22-alpine` runs `npm run build`, then the `dist/` output is copied into `nginx:1.27-alpine`. **Any file the build needs must be explicitly `COPY`'d in the builder stage** — currently `index.html vite.config.ts tsconfig.json tsconfig.node.json` and `src/`. If you add a root-level config the build depends on, add it to the Dockerfile too.

`nginx.conf` provides: SPA fallback (`try_files ... /index.html`), 1-year immutable caching for hashed assets, security headers, and a `/health` endpoint returning `{"status":"ok","service":"rackops"}` — used by the Docker `HEALTHCHECK` and CI.

CI/CD (`.github/workflows/ci.yml`): runs on push to `main`, `qa`, `development`. Steps: verify runtimes → lint → test → **`docker build`** (the real gate) → deploy. Deployment to production happens **only on `main`**, and is handled automatically by Dokploy's own GitHub webhook — the workflow does not push or call a deploy endpoint itself.

## Deployment infrastructure context

Not needed for code changes, but explains the ops docs in the repo root. The site deploys via **Dokploy onto a 3-node Docker Swarm** (`.context.md`, `CLUSTER_ARCHITECTURE_NOTES.md`, `NETWORKING_DEEP_DIVE.md`):
- 3 manager nodes are required for Swarm quorum / HA of the control plane.
- Images go through a registry (`127.0.0.1:5000`) because only the primary manager builds; other nodes pull the finished image.
- Ingress: Cloudflare Tunnel → Traefik → Swarm service. TLS terminates at Cloudflare; internal endpoints are plain `http://`. Nodes communicate over a Netbird overlay network.
