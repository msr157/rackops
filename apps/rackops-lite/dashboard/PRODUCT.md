# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Self-hosters and small teams who run their own hardware: homelab operators, indie
developers, and small businesses racking old PCs, Raspberry Pis, and cheap VPSes.
Linux-centric audience (the dashboard deliberately shows Ctrl+K, not ⌘K, off-Mac).
Multi-tenant: organizations with owner/admin/member roles and email invitations.
*(Inferred from code evidence; user confirmed direction but was not interviewed.)*

## Product Purpose

RackOps Lite is a self-hosted control plane for a personal or small-business
"rack": join machines with one command, deploy apps (45+ templates, Docker
images, git repos, compose files), expose them publicly (instant nip.io URL,
quick tunnel, or your own domain through the built-in edge), and watch the whole
fleet live — telemetry, logs, deployments, alerts — from one dashboard. Success
is an operator who can answer "is everything okay?" in one glance and get an app
from zero to a public URL in under a minute.

## Positioning

A single Go binary you own: works on a LAN, behind CGNAT, or fully air-gapped
(fonts and assets ship with the binary; no CDN calls). Apps keep running even
when the control plane is down — and the dashboard says so honestly instead of
pretending the rack is empty.

## Operating Context

Browser dashboard (React/Vite SPA) talking to the Go backend over REST + SSE.
Org scoping via `X-Rackops-Org` header; switching orgs remounts the data layer
(`rackops-org-changed` CustomEvent + localStorage — a contract pages rely on).
Self-update flow (server pulls release, restarts, dashboard reconnects).
Support email: support@rackops.in. Swagger UI and an MCP endpoint ship with the
server. CLI + agent run on each machine.

## Capabilities and Constraints

- Realtime: one SSE stream + 8s fallback poll in `DataProvider`; pages render
  from shared context, never their own fleet fetches.
- `loaded` / `reachable` / `lastSync` flags exist so UI never renders an outage
  as an empty rack — every surface must respect them.
- Fonts self-hosted via @fontsource (air-gap requirement — never add CDN assets).
- Templates, placement scoring, adopted (externally created) apps, managed
  volumes vs bind mounts, GPU passthrough, mesh relay networking.
- Terminology: machines (not nodes) in copy, apps (not services), the rack,
  the edge, quick tunnel, instant URL.

## Brand Commitments

- Name: **RackOps** (this tier is LITE; version shown in the sidebar fleet card).
- Taste anchor (user-pinned 2026-08-01, supersedes EnvKit/Cloudflare): the
  **NetBird admin console** — one quiet near-black surface, list/table-first
  pages, almost no boxes, played straight at reference craft level.
- Direction (user-chosen 2026-08-01): **quiet console** — pure-neutral dark,
  NetBird-minimal density (no dashboard charts/deltas), single accent =
  the marketing site's **signal green** (product and rackops.dev now share
  one brand color).
- Voice: plain-language, consequence-literate ops copy ("Data in managed
  volumes stays on disk", "your apps keep running"). Never marketing-speak
  inside the product.

## Evidence on Hand

- Real feature set as shipped in `src/` (all claims in-product are live code).
- Changelog content in `src/pages/Changelog.tsx`; CHANGELOG.md at app root.
- "45+ templates" is existing product copy served by the backend catalog.
- No testimonials/benchmarks exist — never invent any.

## Product Principles

1. **Truth over cheer** — an unreachable control plane must never look like an
   empty rack; staleness is labeled with its age.
2. **One glance answers "is everything okay?"** — attention items outrank
   healthy detail on every surface.
3. **Consequences before actions** — destructive actions state blast radius and
   what survives; routine lifecycle actions stay friction-free.
4. **Operator throughput is a feature** — keyboard paths, palette, dense-but-
   scannable lists; no forced tours or nag dialogs.
5. **Familiar before clever** — category-standard console patterns, executed at
   a high craft level; delight lives in precision, not novelty.

## Accessibility & Inclusion

WCAG AA contrast on all load-bearing text (including on raised surfaces);
keyboard path for every primary flow; status never carried by color alone
(dot + word); shared dialog behavior (Esc, focus trap, focus return);
`prefers-reduced-motion` respected globally.
