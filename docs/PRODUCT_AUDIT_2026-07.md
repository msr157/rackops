# RackOps Product Audit — July 2026

Full-product audit run on 2026-07-18 against the `production` branch (post v0.4.0 rebuild, commit `f34fa7c`). Nine agents each read one subsystem's code end-to-end; three researched the 2025–2026 feature sets of Dokploy, Coolify, CapRover, and Portainer CE; a final critic pass synthesized cross-cutting themes and named what the audit itself did not cover. Everything below is the complete, untruncated output.

**Scope:** 9 product areas · 110 limitations · 98 improvements · 78 missing features · 27 product ideas · 3 competitor reports

## Executive summary

### Top churn risks

**1. No push-to-deploy in any form (no webhooks, no polling, no git-provider integration)**

The daily-driver loop of Dokploy, Coolify, and CapRover is git push -> live. RackOps requires a manual Redeploy click for every change, forever. Confirmed: no webhook route exists and the 'webhook' trigger in types.go:257 is dead. A developer who deploys 5x/day feels this pain 5x/day — it is the single most-cited reason to pick a PaaS, and its absence is felt within the first week. Churn trigger: user wires up their real project, realizes CI integration means curl-ing an API manually, and goes back to Coolify's GitHub App.

**2. The first template deploy visibly malfunctions (ghost duplicate services, permanent fake 'running' status, port lies)**

Spot-checked and confirmed: catalog compose stacks hardcode container_name (13 occurrences in catalog.go), deployCompose applies no rackops labels, and listExternal (agent/runtime.go:79-81) only skips labeled/prefixed containers — so deploying Immich creates 4 phantom 'external/adopted' services whose lifecycle buttons fight the stack. Meanwhile compose services are skipped by reconcile (api.go:286-289), so a crashed stack shows green forever, and the deploy form's port field is disconnected from the hardcoded compose ports. The catalog is the top-of-funnel (users arrive to one-click Immich/n8n); when the very first deploy produces obvious brokenness in hour one, users conclude the whole product is untrustworthy and leave before ever seeing the good parts.

**3. No backups anywhere — databases, volumes, or the control plane itself — plus active data-loss traps**

Both competitors headline scheduled S3 database/volume backups with retention and restore; RackOps ships DB templates with zero backup story (TaskBackup constant at types.go:663 is unused, verified). Worse, RackOps actively destroys data: node migration (service_manage.go:119-137, verified) removes the old container and redeploys fresh with no volume transfer — a Postgres migrated between machines is silently emptied — and state.json (the entire platform's brain: services, domains, users, secrets) has no export, snapshot rotation, or restore path. Users will not put real data on a platform that cannot dump it offsite; the first person who loses a database to a migration will churn loudly and publicly.

**4. Deploys are unsafe: guaranteed downtime, failed deploys strand you with nothing running, no rollback, and a hung build bricks the UI**

Every deploy does docker rm -f before docker run (runtime.go:211) — downtime on every config change, and if the new run fails the old container is already gone with auto-heal looping the broken config. Every build overwrites the only image tag (build.go:42), so even manual rollback is impossible. A lost TaskResult leaves the service in 'building' forever with all buttons disabled (AppDetail.tsx:65-66) and no server-side timeout. CapRover has done health-checked traffic switching with one-click rollback for years; a single bad Friday deploy that becomes an unrecoverable outage with no revert button is a definitive churn event for anyone hosting something that matters.

**5. The multi-machine story — RackOps's core differentiator — is unreliable in practice**

The one reason to choose RackOps over single-server Dokploy/Coolify is 'your whole fleet, one edge.' But in any fleet >1 machine: hub relay listeners are torn down and rebuilt every ~2s (ingress.go:288/342 called per-node from api.go:212), causing random 502s on cross-node apps; long builds freeze the agent's synchronous report loop (agent.go:189-196) so machines falsely go 'offline' with critical alerts on every deploy; node_down alerts never reach Slack/Discord (only dashboard broadcast, main.go:171-174); dead machines can never be removed from the fleet (no DELETE /api/nodes); and disk fills from build images with no prune anywhere (grep verified). The accumulated experience — random 502s, cry-wolf alerts, zombie nodes, eventual disk-full outage — teaches users the platform can't be trusted with a fleet, which removes the only reason not to go back to Coolify.

### Cross-cutting themes

**1. Backups and disaster recovery as a product pillar (control-plane state, volumes, databases, mesh keys)**

Three audits each mention a fragment (DB dumps in the deployment audit, competitor S3 backups, mesh key files in networking) but nobody owns DR end-to-end. Verified: state.json is the single source of truth for the entire platform — services, users, password hashes, plaintext env secrets, domains — rewritten every 2s (store.go:154-178) with no export endpoint, no snapshot rotation, no restore-onto-new-server flow; TaskBackup (types.go:663) exists but is referenced nowhere else; mesh hub keys/allocations are loose local files (meshhub.go:42-92). One corrupted SSD loses the whole install and every secret in it. DR needs a single owner: state export/import + scheduled encrypted snapshots to S3/local + volume and DB backup built on the same plumbing.

**2. Secrets management does not exist as a system — every area leaks its own way**

Each audit found a distinct leak: env values broadcast install-wide over SSE (services.go:275 via Hub.Broadcast), secrets echoed into permanent deploy logs (runtime.go:285), GitToken serialized raw on the list endpoint (api.go:822-844), the shared join token letting anyone drain task payloads full of secrets (api.go:199), and — verified — everything at rest in plaintext pretty-printed JSON (only API keys and reset tokens are hashed, auth_account.go:149-152; Env and GitToken are not). These are symptoms of one missing design: a secret type with encryption at rest, masking on every read path, redaction in logs, and scoped delivery to exactly the agent that needs it. Fixing the five leaks individually without the system guarantees leak number six.

**3. Org multi-tenancy is a facade — scoping was retrofitted and every subsystem forgets it differently**

The deployment audit found install-wide SSE broadcasts of full service objects; the machines audit found the shared join token joins any org and node_down broadcasts leak cross-org; the networking audit found ingress/domain/expose/tunnel endpoints with zero org checks (ingress.go:145-195, 360, 402; tunnel.go:20,43) and first-come domain squatting. This is one systemic failure, not twelve bugs: there is no enforced pattern (middleware) that makes org scoping the default. Either do a requireOrg + BroadcastOrg sweep with a test that walks every route asserting cross-org denial, or stop marketing orgs as an isolation boundary — right now it is security theater.

**4. Unbounded growth of replicated state (deployments, alerts, events) with no retention policy anywhere**

Deployment records append forever with up to 20KB of log each (services.go:294), resolved alerts are never pruned (placement.go:186-207), and events accumulate similarly — all serialized pretty-printed into state.json every 2s and shipped as full raft snapshots under HA. Two audits flag their own slice; nobody owns 'retention and compaction' as a platform concern. Without caps, every long-lived install degrades: slower persists, larger snapshots, growing SSE replay payloads. One M-sized change (per-collection caps + age-based pruning + moving deploy logs out of replicated state onto disk) fixes all three.

**5. The agent's synchronous single-threaded task loop is the shared root cause behind at least four audited symptoms**

False node-offline alerts during builds, frozen telemetry, inability to stop/restart other apps on a node mid-build, and stale-task replay storms on reconnect all trace to agent.go:189-196 running tasks inline between reports, plus tasks having no TTL (store.go:252-271) and no server-side execution watchdog. The audits propose per-symptom mitigations (raise offline deadline, add task TTL, add build timeout); the correct fix is architectural and singular: run tasks on worker goroutines with heartbeat progress reports, give tasks deadlines, and have the server expire abandoned ones. Fix it once and four limitation entries across two audits disappear.

**6. 'Destroy-then-create' is the universal state-transition pattern; 'old serves until new is verified' appears nowhere**

Deploys rm -f the old container before running the new (runtime.go:211); node migration removes the old container with no volume data move (verified, service_manage.go:119-137); moving the edge leaves the orphan Traefik but deploys the new one without health confirmation (ingress.go:172-178); compose redeploys use --remove-orphans against name-colliding projects. No subsystem does health-gated cutover or keeps a fallback. This is a design principle to adopt platform-wide (deploy-new -> health check -> swap traffic/name -> remove old), not a per-feature fix — and it is the foundation rollback, zero-downtime deploys, and safe migration all sit on.

**7. Heterogeneous hardware (CPU arch, GPU vendor) is invisible to every layer despite being the defining trait of the target fleet**

A homelab fleet is definitionally mixed: Pi (arm64) + old desktop (amd64) + maybe an NVIDIA or AMD card. Yet placement has no arch constraint (an amd64 image can be auto-placed on ARM to die with exec-format-error), GPU placement counts AMD VRAM then runs --gpus all which requires the NVIDIA runtime (placement.go:42-54, runtime.go:278), and git builds produce images tagged only on the node that built them — a service migrated to another node can't pull its own image at all (no registry story). Arch/runtime awareness needs to thread through node telemetry, placement, build, and migration together; no single-area fix covers it.

**8. No resource governance beyond a single per-service memory limit**

Placement double-books instantaneous free RAM (placement.go:37-41) and ignores existing MemLimitMB commitments and CPUMilli entirely; there are no per-org quotas (relevant if orgs are real tenants), no disk budgets for builds/logs/volumes, no reserved headroom for the agent/system itself, and no prune anywhere (grep verified). The failure mode spans areas: a build fills the disk, which kills the node, which strands services, which floods alerts. Governance (reservations-based scheduling + disk thresholds with auto-cleanup + headroom) is one coherent feature nobody audited as such.

### What this audit did not cover

- **Authentication, RBAC, and security hardening as a first-class area** — The audits cite auth incidentally (IDOR, open /relay paths) but nobody audited auth.go / auth_account.go / orgs.go themselves: session lifetime and revocation, CSRF posture, brute-force/lockout on login and password reset, password policy, API key scoping and rotation, the actual permission matrix (does ManageMachines gate everything it should?), and invitation flow abuse. Verified absent: 2FA/TOTP (no matches in backend) and any audit log (grep 'audit' finds only a comment at ingress.go:287) — a team/RBAC product where no one can answer 'who deleted the production app' or 'who read the secrets.' For a product that holds root-equivalent power over a user's whole fleet, this is the most important unaudited surface.
- **Upgrade path and state-schema migration (version skew, downgrade, compatibility)** — Self-update exists (update.go) but nobody audited what happens next: state.json has no schema version field and the only migration in the codebase is the pre-org backfill (orgs.go:27, main.go:140). Unanswered: does 0.4 reading 0.3 state silently drop unknown fields on the next 2s persist? What happens with a 0.4 server and six 0.3 agents (report/task payload skew)? Is downgrade possible after a bad update? Under raft HA, can mixed-version servers exchange snapshots? For a single-binary product whose pitch includes self-update, the upgrade journey is a headline day-2 flow with zero coverage.
- **Volume and persistent-data lifecycle (the stateful half of the product)** — Mounts/volumes exist in the model (MountVolume, types.go:166) but no audit covered the data lifecycle: what happens to volume data when a service is deleted (orphaned or destroyed?), how users see volume sizes and locations, cleanup of orphans, and — verified while spot-checking — that node migration (service_manage.go:119-137) redeploys on the new node with no data transfer whatsoever, silently emptying every database template that gets migrated. The deployment audit covers containers; the machines audit covers disks as hardware; the data ON those disks that users actually care about is owned by nobody.
- **Runtime logs, events, and app-level observability experience** — templates_logs.go and agent/logstream.go exist but no audit examined the runtime logging journey: log retention and rotation (or unbounded container logs filling disks — no json-file log-opts appear in runtime.go's docker run), search/filter across restarts, multi-container/compose log aggregation, event feed growth and usefulness, and log-based alerting (crash-loop detection from logs). Competitors' users live in the log view when things break; the audits covered deploy logs and metrics but not the day-2 'why is my app returning 500s' journey.
- **Programmatic surface: OpenAPI/MCP/API keys, the AI assistant, and automation workflows** — openapi.go, swagger, MCP integration, and assistant.go were audited by nobody. Unanswered: does the API cover all UI actions (can you drive RackOps from CI or Terraform, which matters directly to the missing push-to-deploy story)? What can the AI assistant read and do — can it see env secrets, is it prompt-injectable via service names or log content it summarizes? Are API keys org-scoped or install-scoped, and what are their permissions? This surface is both a differentiator (MCP is ahead of competitors) and an unassessed attack surface.
- **Vendor telemetry, CRM relay, and support-ticket privacy** — internal-apps/crm plus crm.go/tickets.go form a phone-home channel from a self-hosted product to the vendor — the single most trust-sensitive subsystem for the privacy-motivated homelab audience — and no audit touched it. Unanswered: exactly what data leaves the install (metrics? hostnames? emails?), is it opt-in, is it disclosed, what does a ticket attach automatically, and how is the vendor-side console secured? One HN comment discovering undisclosed telemetry in a self-hosted PaaS is an extinction-level reputation event; this needed auditing more than several areas that got it.
- **First-run journey and HA/raft cluster operations end-to-end** — Two adjacent day-2 gaps: (1) Nobody walked signup -> add first machine -> first deploy -> first domain as a continuous journey (the machines audit found the missing install one-liner, but not whether the sequence's empty states, defaults, and error recovery hang together — e.g. Machines.tsx:104 tells users to check a systemd unit nothing creates). (2) cluster.go/raftstore.go: the networking audit noted mesh doesn't fail over with raft leadership, but nobody audited raft itself — join/leave a server, quorum-loss recovery, split-brain behavior, what is and isn't replicated (task queue is deliberately local, store.go:180-184 comment), and whether HA actually works or is a checkbox. Shipping 'HA' that loses the mesh, the task queue, and the mesh keys on failover may be worse than not shipping it.

## Priority matrix (all improvements & missing features)

| Impact | Effort | Type | Item | Area |
|---|---|---|---|---|
| critical | L | missing | Per-service HTTP/TCP health checks with response-time and uptime tracking | Observability |
| critical | L | missing | User documentation site (and a docs link anywhere) | Support pipeline, CRM app, marketing website, docs & changelog |
| critical | M | missing | Push-to-deploy webhook receiver (GitHub/GitLab/Gitea) | AI assistant, API ecosystem, extensibility |
| critical | M | improve | Wire the early-access endpoint into the CRM | Support pipeline, CRM app, marketing website, docs & changelog |
| critical | M | improve | Wire agent failover to the raft leader | Platform |
| critical | M | missing | Server state backup/restore (one-click export + restore-on-install) | Platform |
| critical | M | improve | Run agent tasks off the report loop | Machines, agents, telemetry, placement, alerts |
| critical | M | missing | curl-able install one-liner + systemd unit for agents (and server) | Machines, agents, telemetry, placement, alerts |
| critical | M | missing | Deploy on git push (webhooks + polling fallback) | Application deployment & lifecycle |
| critical | S | improve | Connection-aware data layer + outage banner | Dashboard UX & onboarding |
| critical | S | improve | Org-scope log streams: BroadcastOrg for log_chunk + serviceForOrg on stream start/stop | Observability |
| critical | S | improve | Org-scope the assistant context (fix the cross-tenant leak) | AI assistant, API ecosystem, extensibility |
| critical | S | improve | Email the operator on new/reopened tickets | Support pipeline, CRM app, marketing website, docs & changelog |
| critical | S | improve | Fix ImportJSON to replicate ALL tenant state (orgs, org settings, invitations, tickets) | Platform |
| critical | S | improve | Add node remove + rename endpoints and UI | Machines, agents, telemetry, placement, alerts |
| critical | S | improve | Stop the secret leaks: org-scope service SSE events, strip GitToken from list responses, redact -e values from deploy logs | Application deployment & lifecycle |
| critical | S | improve | Fix invitation-accept takeover of existing accounts | Auth, organizations, RBAC, API keys, profile |
| critical | S | improve | Fix hub relay teardown flapping | Networking |
| critical | XL | missing | Scheduled volume + database backups with S3/local/SFTP targets and retention | Platform |
| high | L | missing | Historical/persistent logs that survive redeploys | Observability |
| high | L | improve | Copilot actions with in-chat confirmation | AI assistant, API ecosystem, extensibility |
| high | L | improve | Named agent accounts with assignment in the CRM | Support pipeline, CRM app, marketing website, docs & changelog |
| high | L | improve | Fleet-wide coordinated updates | Platform |
| high | L | missing | Dokploy/Coolify migration importer | Platform |
| high | L | improve | Server-side metrics history + real charts | Machines, agents, telemetry, placement, alerts |
| high | L | missing | Web terminal onto machines and containers | Machines, agents, telemetry, placement, alerts |
| high | L | improve | Keep the old container until the new one is healthy | Application deployment & lifecycle |
| high | L | improve | Move volume data during node migration | Application deployment & lifecycle |
| high | L | missing | Database backups (scheduled dumps + restore) | Application deployment & lifecycle |
| high | L | missing | OIDC/SSO login | Auth, organizations, RBAC, API keys, profile |
| high | L | missing | TCP (and UDP) ingress through the edge | Networking |
| high | L | missing | DNS-01 ACME (provider API keys) and true wildcard certificates | Networking |
| high | M | improve | Palette v2: fuzzy match, full index, recents, entity actions | Dashboard UX & onboarding |
| high | M | improve | Mobile pass: overlay nav with org switcher, fixed footer | Dashboard UX & onboarding |
| high | M | missing | Notification center with alert/event history | Dashboard UX & onboarding |
| high | M | improve | Server-side metrics ring buffer + real charts | Observability |
| high | M | improve | Log download, search, and timestamps | Observability |
| high | M | missing | Docker healthcheck status, restart counts, and OOM-kill detection | Observability |
| high | M | improve | Add request/response schemas to the OpenAPI document | AI assistant, API ecosystem, extensibility |
| high | M | improve | Scoped API keys | AI assistant, API ecosystem, extensibility |
| high | M | improve | Round out the MCP tool surface | AI assistant, API ecosystem, extensibility |
| high | M | missing | First-class CI/CD recipe: official GitHub Action + rackops deploy --wait | AI assistant, API ecosystem, extensibility |
| high | M | missing | Custom templates (per-org catalog extensions) | AI assistant, API ecosystem, extensibility |
| high | M | missing | Remote MCP endpoint (streamable HTTP on the server) | AI assistant, API ecosystem, extensibility |
| high | M | improve | Add search, filters, and severity-aware ordering to the CRM inbox | Support pipeline, CRM app, marketing website, docs & changelog |
| high | M | missing | Ticket attachments / diagnostic uploads | Support pipeline, CRM app, marketing website, docs & changelog |
| high | M | missing | Website pages for docs, changelog, and pricing | Support pipeline, CRM app, marketing website, docs & changelog |
| high | M | improve | Stop persisting/replicating volatile metrics every 2 seconds | Platform |
| high | M | improve | Self-update hardening: checksums, keep-old rollback, version pinning | Platform |
| high | M | improve | Agent install one-liner + systemd unit from the Add Machine modal | Platform |
| high | M | improve | Make placement honest: CPU request, reservations, arch, GPU vendor | Machines, agents, telemetry, placement, alerts |
| high | M | improve | Configurable alert rules + ack/snooze | Machines, agents, telemetry, placement, alerts |
| high | M | improve | Per-node join secrets and token rotation | Machines, agents, telemetry, placement, alerts |
| high | M | missing | Fleet-wide agent auto-update from the control plane | Machines, agents, telemetry, placement, alerts |
| high | M | missing | Node maintenance mode / drain (cordon) | Machines, agents, telemetry, placement, alerts |
| high | M | missing | Service-level alert rules (crash loop, OOM, restart storm, deploy failed) | Machines, agents, telemetry, placement, alerts |
| high | M | improve | Label compose stack containers and wire them into status sync / reconcile | Application deployment & lifecycle |
| high | M | improve | Per-version image tags + one-click rollback from the Deployments tab | Application deployment & lifecycle |
| high | M | improve | Run agent tasks off the report loop | Application deployment & lifecycle |
| high | M | improve | Parameterize compose template ports and include the service ID in the project name | Application deployment & lifecycle |
| high | M | improve | User-definable health checks that gate status and deploys | Application deployment & lifecycle |
| high | M | missing | Container shell (exec) from the dashboard | Application deployment & lifecycle |
| high | M | missing | Pre/post-deploy hooks (release commands) | Application deployment & lifecycle |
| high | M | improve | Add token versioning + logout-everywhere | Auth, organizations, RBAC, API keys, profile |
| high | M | improve | Ship platform user management (API + Settings UI) | Auth, organizations, RBAC, API keys, profile |
| high | M | missing | 2FA (TOTP + recovery codes) | Auth, organizations, RBAC, API keys, profile |
| high | M | missing | Scoped API keys | Auth, organizations, RBAC, API keys, profile |
| high | M | improve | Add org/permission checks to all networking mutations and close /relay | Networking |
| high | M | missing | Route-level protection: basic-auth, IP allowlist, rate limiting | Networking |
| high | M | missing | Multiple domains per service, redirects, and path routing | Networking |
| high | S | improve | Real loading states: splash + skeletons, kill the onboarding flash | Dashboard UX & onboarding |
| high | S | improve | Send node_down (and recovery) through notification channels | Observability |
| high | S | improve | Multi-turn conversations | AI assistant, API ecosystem, extensibility |
| high | S | improve | Propagate customer resolution to the CRM and briefly keep polling resolved tickets | Support pipeline, CRM app, marketing website, docs & changelog |
| high | S | improve | Surface ForwardPending in the Support UI | Support pipeline, CRM app, marketing website, docs & changelog |
| high | S | improve | Scope ticket_update SSE events to the ticket's author and admins | Support pipeline, CRM app, marketing website, docs & changelog |
| high | S | missing | Periodic opt-in telemetry sender | Support pipeline, CRM app, marketing website, docs & changelog |
| high | S | improve | Make state persistence crash-safe: fsync + rotated last-known-good + corruption fallback | Platform |
| high | S | improve | Route node_down (and resolutions) through notification channels | Machines, agents, telemetry, placement, alerts |
| high | S | improve | Deploy watchdog: time out stuck 'building' services and never lock the user out | Application deployment & lifecycle |
| high | S | improve | Rate-limit login and audit auth attempts | Auth, organizations, RBAC, API keys, profile |
| high | S | improve | Refresh relay targets on change (ProxyMesh/ProxyPort upsert) | Networking |
| high | XL | missing | GitHub App integration (repo picker, commit statuses, PR preview envs) | AI assistant, API ecosystem, extensibility |
| high | XL | missing | Automatic workload failover off dead nodes | Machines, agents, telemetry, placement, alerts |
| medium | L | missing | Global search across all entities | Dashboard UX & onboarding |
| medium | L | missing | Guided product tour / demo data mode | Dashboard UX & onboarding |
| medium | L | missing | Public status page | Observability |
| medium | L | missing | Licensing/entitlement plumbing | Support pipeline, CRM app, marketing website, docs & changelog |
| medium | L | missing | Real audit log | Auth, organizations, RBAC, API keys, profile |
| medium | L | missing | DNS provider integration (auto-create records) | Networking |
| medium | M | improve | Multiplex all SSE consumers over one EventSource | Dashboard UX & onboarding |
| medium | M | improve | Richer, per-user setup checklist | Dashboard UX & onboarding |
| medium | M | missing | Light theme + system preference | Dashboard UX & onboarding |
| medium | M | missing | Bulk / multi-select actions on apps | Dashboard UX & onboarding |
| medium | M | missing | Installable PWA with offline shell | Dashboard UX & onboarding |
| medium | M | improve | Prune alerts and deployment history; move logs out of the hot state path | Observability |
| medium | M | improve | Events API pagination + filters + user attribution | Observability |
| medium | M | improve | Configurable alert thresholds per org | Observability |
| medium | M | missing | Email (SMTP) notification channel | Observability |
| medium | M | missing | Notification routing rules | Observability |
| medium | M | missing | Prometheus /metrics endpoint (or OTLP export) | Observability |
| medium | M | improve | Harden outbound webhooks into a real events system | AI assistant, API ecosystem, extensibility |
| medium | M | improve | Render markdown + stream Copilot answers | AI assistant, API ecosystem, extensibility |
| medium | M | missing | Audit log API | AI assistant, API ecosystem, extensibility |
| medium | M | missing | Canned replies and a lightweight knowledge base in the CRM | Support pipeline, CRM app, marketing website, docs & changelog |
| medium | M | missing | SLA / responsiveness metrics in the CRM | Support pipeline, CRM app, marketing website, docs & changelog |
| medium | M | improve | CLI parity with the MCP surface + --json | Platform |
| medium | M | improve | Cluster membership management: remove peers, warn on even quorums, show health | Platform |
| medium | M | missing | Node decommission + uninstall flow | Platform |
| medium | M | missing | Release channels + changelog-gated updates | Platform |
| medium | M | missing | `rackops doctor` diagnostics + support bundle | Platform |
| medium | M | improve | Real drive health via smartctl when available | Machines, agents, telemetry, placement, alerts |
| medium | M | missing | Node labels/tags with placement affinity | Machines, agents, telemetry, placement, alerts |
| medium | M | missing | Per-process/per-container top view on the machine page | Machines, agents, telemetry, placement, alerts |
| medium | M | missing | Scheduled/cron jobs | Application deployment & lifecycle |
| medium | M | missing | Multiple domains and per-port routes per app | Application deployment & lifecycle |
| medium | M | missing | Update notifications for template/image apps | Application deployment & lifecycle |
| medium | M | improve | Move machines and projects between orgs | Auth, organizations, RBAC, API keys, profile |
| medium | M | missing | Active session management | Auth, organizations, RBAC, API keys, profile |
| medium | M | improve | Surface mesh peer health (handshake age, RTT) in the Network page | Networking |
| medium | M | improve | Make ACME resilient: port-80 preflight, LE staging toggle, cert-expiry alerts, exempt nip.io from redirect | Networking |
| medium | M | improve | Expose extra Ports on the mesh and relay them | Networking |
| medium | M | improve | Keep routes up during redeploys | Networking |
| medium | M | improve | Revoke mesh peers when a machine is removed | Networking |
| medium | M | missing | Persistent named Cloudflare tunnels (account token) | Networking |
| medium | M | missing | Custom Traefik configuration escape hatch | Networking |
| medium | M | missing | Certificate management surface | Networking |
| medium | S | improve | Fix the nested-NavLink brand and give the changelog a real entry point | Dashboard UX & onboarding |
| medium | S | improve | Focus trap + Escape + focus-return for Modal, confirm, and drawer | Dashboard UX & onboarding |
| medium | S | improve | Bundle fonts locally | Dashboard UX & onboarding |
| medium | S | improve | Honest freshness: bind LIVE to stream health, surface lastSeen | Dashboard UX & onboarding |
| medium | S | improve | Toast dismissal + persistent error detail | Dashboard UX & onboarding |
| medium | S | improve | Type-to-confirm for high-blast-radius deletions | Dashboard UX & onboarding |
| medium | S | improve | Single shared EventSource with a handler registry | Observability |
| medium | S | improve | 'Send test notification' button per channel | Observability |
| medium | S | missing | Cert-expiry alerting | Observability |
| medium | S | improve | Let viewers use the Copilot and rate-limit it | AI assistant, API ecosystem, extensibility |
| medium | S | missing | CLI depth: logs, restart, status --watch, JSON output | AI assistant, API ecosystem, extensibility |
| medium | S | improve | Generate the in-app changelog from CHANGELOG.md at build time | Support pipeline, CRM app, marketing website, docs & changelog |
| medium | S | improve | Forward only new messages after the first ticket sync | Support pipeline, CRM app, marketing website, docs & changelog |
| medium | S | improve | Add a telemetry section to Settings with an opt-out and payload preview | Support pipeline, CRM app, marketing website, docs & changelog |
| medium | S | improve | Prune deployment history | Platform |
| medium | S | improve | Fix hottestTemp CPU-sensor preference | Machines, agents, telemetry, placement, alerts |
| medium | S | improve | Guard deploys against offline nodes and expire stale tasks | Machines, agents, telemetry, placement, alerts |
| medium | S | improve | Surface agent version and fleet skew | Machines, agents, telemetry, placement, alerts |
| medium | S | improve | De-duplicate Needs attention and mark services on dead nodes | Machines, agents, telemetry, placement, alerts |
| medium | S | missing | Node reboot / shutdown / docker prune actions | Machines, agents, telemetry, placement, alerts |
| medium | S | missing | Email notification channel | Machines, agents, telemetry, placement, alerts |
| medium | S | improve | Bulk .env paste + project-level shared env inheritance | Application deployment & lifecycle |
| medium | S | improve | Fail loudly on missing git branch; record and show the built commit | Application deployment & lifecycle |
| medium | S | improve | Cap deployment history per service | Application deployment & lifecycle |
| medium | S | missing | Private registry credentials | Application deployment & lifecycle |
| medium | S | improve | Enforce role hierarchy in org member management | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Stop cross-org leakage of auth/org events | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Audit key + membership mutations, with actor identity | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Honor X-Forwarded-For behind a trusted proxy | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Restrict query-param auth to the SSE stream | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Make 'no project access' representable | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Editable profile identity + name resolution for members | Auth, organizations, RBAC, API keys, profile |
| medium | S | missing | Invitation + security email delivery via CRM relay | Auth, organizations, RBAC, API keys, profile |
| medium | S | improve | Tear down the old edge when the ingress node changes | Networking |
| medium | S | improve | Validate domain routing: verified-domain check, uniqueness, org ownership | Networking |
| medium | XL | missing | Terraform/Pulumi provider | AI assistant, API ecosystem, extensibility |
| medium | XL | missing | Preview environments (deploy a branch/PR to a temporary URL) | Application deployment & lifecycle |
| medium | XL | missing | P2P mesh paths with hub fallback | Networking |
| medium | XL | missing | Multi-edge / edge failover | Networking |
| low | L | missing | i18n framework | Dashboard UX & onboarding |
| low | M | missing | Keyboard shortcut system + '?' cheat sheet | Dashboard UX & onboarding |
| low | M | missing | Webhook signing, retries, and delivery log | Observability |
| low | S | missing | Per-page document titles and favicon status | Dashboard UX & onboarding |
| low | S | improve | Indicate dropped chunks in live tails | Observability |
| low | S | improve | Move Gemini key out of the URL | AI assistant, API ecosystem, extensibility |
| low | S | missing | CSAT + resolution feedback | Support pipeline, CRM app, marketing website, docs & changelog |
| low | S | missing | Alert history / audit page | Machines, agents, telemetry, placement, alerts |
| low | S | missing | Configurable password policy + breach hygiene | Auth, organizations, RBAC, API keys, profile |
| low | XL | missing | Replicas / multi-instance scaling | Application deployment & lifecycle |

## Area audits

### Dashboard UX & onboarding (apps/rackops-lite/dashboard)

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| Information architecture / nav shell | solid | Compact, opinionated IA: 4 primary nav items (Home/Apps/Machines/Network) with live count badges, org switcher, sidebar footer icon row (Profile/Support/Settings/Logout), topbar with search trigger + Copilot + primary Deploy CTA (App.tsx:104-174). 11 routes total, unknown routes redirect home. Weak points: Settings/Profile/Support are unlabeled 13px icon buttons buried in the footer, the changelog's only entry point is the tiny 'LITE · v0.4.0' text (App.tsx:118), and nav badge semantics are inconsistent (Home badge = problems, Apps badge = total count). |
| Design token system (EnvKit/Cloudflare-inspired) | solid | index.css is a genuinely coherent 634-line system: near-black chassis with white-alpha hairlines, one amber accent, LED status language with glow/blink animations, rack-unit signature component, 3-tier ink hierarchy, Space Grotesk/Inter/IBM Plex Mono. Includes :focus-visible outlines, prefers-reduced-motion, custom scrollbars, selection color. This genuinely hits the bar visually — the gaps are behavioral (loading, mobile, theming), not aesthetic. |
| First-run auth & account flows | solid | Auth.tsx detects needsSetup and opens straight into owner-account creation with an explanatory badge; forgot/reset flows with the server-log fallback tip (journalctl) for self-hosters; invitation deep-link handled before the auth gate (App.tsx:52); live 'passwords don't match yet' hinting; password eye toggles; correct autoComplete attributes. |
| Setup checklist (Home) | basic | 3 steps (add machine → deploy app → make it public) that tick off live via SSE and highlight the next action (Home.tsx:183-242). Good arc ending on the public-URL payoff. But it's shallow vs Coolify/Dokploy onboarding: no domain/HTTPS step, no alerts/notifications step, no invite-team step; dismissal is a localStorage flag so it reappears on every new browser and is shared across orgs. |
| Command palette (⌘K) | basic | CommandPalette.tsx: static pages/actions plus live apps and machines with status LEDs, keyboard navigation, mouse hover sync. But it's substring matching only (no fuzzy), no recents/frecency, missing Profile/Support/Changelog and Settings tabs from its index (CommandPalette.tsx:33-42), no domains/tickets/deployments, and no verbs on entities (can navigate to an app but not restart it or jump to its logs). |
| Empty states | solid | Every page has a crafted empty state with a next-step CTA: Apps ('45+ templates' + deploy button, Apps.tsx:107-119), Machines (join-command pitch, Machines.tsx:35-45), Rack, Network public routes, Support, Home's dependency-aware 'Apps run on machines' variant (Home.tsx:123-129). Consistently good copy in product voice. |
| Error surfacing (toasts + confirm) | basic | Promise-based confirm dialog for every destructive action with danger styling (confirm.tsx); capped toast stack with aria-live, error prefix stripping via cleanError (toast.tsx:23-25); inline failure cards for edge deploy (Network.tsx:182-185) and failed deploys with full build logs. But toasts can't be dismissed or copied and errors vanish after 7s; and the data layer swallows every fetch error silently (see limitations). |
| Real-time data layer | basic | data.tsx: one DataProvider with initial fetch + SSE upserts per event type + 8s convergence poll for overview/alerts, and a clean org-remount strategy (App.tsx:32-34). Live log tailing with re-arm before the 15-min agent cap and scroll pinning (AppDetail.tsx:469-522) is excellent. But every consumer of subscribe() opens its own EventSource (api.ts:257-269) so DataProvider + LogsTab + DeploymentsTab + Support can hold 4+ concurrent SSE connections, and there is zero connection-state tracking. |
| Update banner + one-click self-update | solid | App.tsx:205-263: server-cached GitHub check (one call per 6h fleet-wide), confirm dialog explaining apps keep running, poll-until-new-version then hard reload, per-version dismissal. This is better than Dokploy's update UX. |
| Copilot drawer | basic | CopilotDrawer.tsx: persistent-across-routes drawer, starter prompts, provider attribution, live fleet summary in the empty state. But responses render as pre-wrap plain text so LLM markdown shows as raw asterisks (line 94), no streaming (single-shot api.assistant), no conversation persistence across reload, no 'ask about this app' context entry points from pages. |
| InfoTip contextual help | solid | ui.tsx:9-24 — hover/focus/click tooltip used ~20 times across pages with genuinely good plain-language copy (ports explainer AppDetail.tsx:418-423, edge explainer Network.tsx:176-181, telemetry disclosure Support.tsx:56-60). This substitutes surprisingly well for docs. |
| Responsive / mobile behavior | skeletal | One real breakpoint (≤900px) collapses the sidebar to a 60px icon rail (index.css:585-593). The org switcher is display:none on mobile (index.css:162-164) so phone users cannot switch orgs at all; the 4-button footer row (App.tsx:143-156) doesn't fit a 60px rail; wide tables, the domain-record card row, and terminals overflow. No hamburger, no bottom nav, no touch-target sizing. |
| Accessibility basics | basic | ~27 aria attributes, aria-modal on dialogs, aria-label on icon buttons, role=status on toasts, :focus-visible outlines, reduced-motion support, StatusBadge pairs LED color with text. But: no focus trap in any modal/drawer, Escape doesn't close Modal or confirm (only the palette handles it), focus isn't returned on close, palette lacks combobox/listbox semantics, and the nested-anchor brand (App.tsx:114-122) is invalid HTML. |
| Dark/light themes | skeletal | Dark-only: html{color-scheme:dark} (index.css:53), tokens hardcoded in one :root block. No light theme, no prefers-color-scheme handling. The token architecture would make a light theme cheap, but the Cloudflare/EnvKit dashboards this emulates all ship both. |
| Loading / skeleton states | skeletal | authed===null renders literally nothing (App.tsx:56) — a blank screen until /api/auth/status answers. Elsewhere it's spinner-plus-'loading…' rows (AppDetail.tsx:153) or nothing. No skeletons anywhere, and empty states are not distinguished from not-yet-loaded (see limitations). |
| Real-time freshness indicators | skeletal | The Home activity feed has a hardcoded 'LIVE' badge (Home.tsx:138) that stays lit even if the SSE stream is dead; nothing anywhere shows connection health or data age. Machines page metrics update every 2s via SSE but nothing indicates staleness when an agent or the stream stalls — lastSeen exists in the API (api.ts:24) and is never rendered. |
| Search | basic | Palette covers apps + machines by name; Apps page has a name/image/domain filter that only appears when >5 services (Apps.tsx:48); DeployFlow has template search. No global search across domains, env keys, deployments, events, or tickets. |
| Keyboard shortcuts | skeletal | ⌘K is the only shortcut in the codebase (App.tsx:87-96). No g-style navigation, no j/k list movement, no '?' cheat-sheet, no Esc-consistency outside the palette. |
| Changelog page | solid | Changelog.tsx: release rail with live-dot, per-release quick links into the shipped features. Content is hardcoded in the bundle (fine for a versioned binary) — but its only nav entry point is the tiny brand-ver link, which is also broken (see limitations). |
| i18n | skeletal | None — every string is inline English JSX. No framework, no extraction. Acceptable for the current audience but a total rewrite cost later. |

#### Limitations & weaknesses

**1. Server outage renders as a fresh empty rack — the worst possible lie**

Every fetch in DataProvider ends in .catch(() => {}) (data.tsx:39-45,97-100) and state stays at []. If the control plane is unreachable or the session breaks mid-flight, Home shows 'Your rack is waiting for its first machine' (Home.tsx:30) with 0/0 counts and the setup checklist reappears — an operator whose production fleet is down sees onboarding UI instead of an outage banner. There is no connection indicator, no retry surface, no error state anywhere in the shell.

**2. Loading is indistinguishable from empty**

nodes/services initialize to [] with no 'loaded' flag, so on every cold load the SetupChecklist and empty states flash before data arrives (Home.tsx:186-193 renders checklist whenever nodes.length===0, including the first ~300ms of every session). Combined with App.tsx:56 returning null pre-auth-check, first paint is: blank screen → onboarding flash → real dashboard.

**3. Nested NavLink makes the changelog effectively unreachable and is invalid HTML**

App.tsx:114-122 nests the brand-ver NavLink (to /changelog) inside the brand NavLink (to /). Nested anchors are invalid; the click bubbles from inner to outer so both navigate() calls fire and the outer (to '/') runs last — clicking 'What's new' lands on Home. The only other changelog entry is nothing: it's absent from the palette (CommandPalette.tsx:33-42) and the nav.

**4. Mobile users can't switch orgs and the footer breaks**

At ≤900px the org switcher is display:none (index.css:162-164) with no alternate entry point, so multi-org users on a phone are stuck in whichever org was last active. The 4-button flex row in the sidebar footer (App.tsx:143-156) is crushed inside the 60px rail, and .release-links/tables/domain-record cards overflow. No hamburger or bottom navigation exists.

**5. No focus management in any overlay**

Modal (ui.tsx:142-157), confirm (confirm.tsx:40-56), the Copilot drawer, and AddMachineModal have no focus trap, don't handle Escape, and don't restore focus on close. Tab walks straight out of an open dialog into the page behind it. Only the command palette handles Escape (CommandPalette.tsx:65).

**6. 'LIVE' badge and activity feed overpromise freshness**

The badge is hardcoded (Home.tsx:138). Events only refetch on service_update/node_joined/node_down (data.tsx:65-78) — alert broadcasts don't refresh the feed, and if the EventSource dies mid-session nothing detects it; the dashboard keeps showing stale metrics under a lit LIVE badge. Node lastSeen (api.ts:24) is never surfaced anywhere.

**7. Every SSE consumer opens its own EventSource connection**

subscribe() constructs a new EventSource per call (api.ts:257-269) and subscribeLogChunks piggybacks on it (api.ts:284-291). DataProvider + an open Logs tab + Deployments tab + Support page = 4 concurrent streams; with a second browser tab that's 8, brushing the browser's 6-connections-per-host HTTP/1.1 limit and silently starving requests on plain-HTTP homelab deployments.

**8. Palette is a navigator, not a command surface**

Substring-only matching (CommandPalette.tsx:54), no recents, no fuzzy tolerance ('jellyfn' finds nothing), and the index omits Profile, Support, Changelog, Settings tabs, domains, and tickets. Cannot act on results — 'restart jellyfin' or 'logs postgres' aren't expressible, which undercuts the operator-console identity.

**9. Google Fonts CDN import contradicts the self-hosted promise**

index.css:1 @imports three families from fonts.googleapis.com. On an air-gapped or LAN-only rack (a core persona — the Network page even has a LAN-only banner) the entire typographic identity silently falls back to system fonts, and every dashboard load pings Google from a product whose login screen says 'your hardware, your data' (Auth.tsx:174).

**10. Error toasts evaporate; destructive confirms are single-click**

Errors auto-dismiss after 7s with no dismiss/copy affordance (toast.tsx:34) — a failed deploy reason can vanish while you're reading it. Deleting an app, a domain with routed apps, or an org is a single confirm click with no type-the-name friction and no undo anywhere (AppDetail.tsx:121-135, Network.tsx:95-110).

**11. Static document.title and no per-page titles**

index.html sets 'RackOps' once; no route updates it. Browser history, tab bars, and bookmarks are indistinguishable — noticeably below the Cloudflare bar for a multi-page console.

**12. Copilot renders raw markdown and forgets everything**

Answers render as pre-wrap plain text (CopilotDrawer.tsx:91-97) so any **bold** or bullet list from a configured LLM displays as literal syntax; no streaming so long answers hang on a spinner; conversation is lost on reload; and there's no contextual entry ('ask about this failing app') from the places users actually get stuck.

#### Improvements

**1. Connection-aware data layer + outage banner** · impact: critical · effort: S

*Why:* An unreachable server currently masquerades as a healthy empty rack — the single most dangerous UX defect in the product (data.tsx:39-45, Home.tsx:30).

*How:* Track fetch failures and EventSource onerror in DataProvider; expose {loaded, connected, lastSync}. When disconnected, render a persistent amber 'Reconnecting to control plane… last data Xs ago' banner in the shell, dim stale metrics, and gate all empty states + SetupChecklist behind loaded===true.

**2. Palette v2: fuzzy match, full index, recents, entity actions** · impact: high · effort: M

*Why:* ⌘K is the flagship power-user surface but misses half the app and can only navigate (CommandPalette.tsx:33-54).

*How:* Add subsequence/fuzzy scoring; index Profile, Support, Changelog, Settings tabs, domains, and open tickets; store last-5 selections; add per-app sub-actions (Restart, Logs, Copy URL, Networking) revealed on Tab or 'app: ' prefix; reuse the existing serviceAction API.

**3. Mobile pass: overlay nav with org switcher, fixed footer** · impact: high · effort: M

*Why:* Homelab operators check their rack from the couch; today they can't switch orgs (index.css:162-164) and the footer is crushed at 60px.

*How:* ≤900px: replace the icon rail with a hamburger-triggered full-height overlay reusing the existing sidebar markup (labels + org switcher + counts intact); stack footer buttons in a 2×2 grid; add overflow-x handling for tables and the domain-record row; bump touch targets to 40px.

**4. Real loading states: splash + skeletons, kill the onboarding flash** · impact: high · effort: S

*Why:* Blank screen at boot (App.tsx:56) and empty-state flashes on every load make the app feel broken on slow links; the design system already has everything needed for handsome skeletons.

*How:* Branded splash (rack-unit + pulsing LED) while auth resolves; skeleton cards for KPI row, rack units, and app list using shimmer on --bg-2; render nothing checklist-related until first fetch resolves.

**5. Multiplex all SSE consumers over one EventSource** · impact: medium · effort: M

*Why:* 4+ parallel /api/stream connections per tab (api.ts:257-269) can hit the 6-per-host HTTP/1.1 limit on plain-HTTP homelab installs and silently break live features.

*How:* Module-level singleton connection with a subscriber registry and refcounted lifecycle; subscribe()/subscribeLogChunks keep their signatures so no call sites change; reconnect with backoff and re-emit connection state to DataProvider.

**6. Richer, per-user setup checklist** · impact: medium · effort: M

*Why:* The 3-step checklist ends before the features that retain users (domains/HTTPS, notifications, team), and its localStorage dismissal resurfaces it on every device (Home.tsx:186).

*How:* Persist checklist state server-side per user+org (one small settings key); add optional steps: add a domain + HTTPS, connect a notification channel, invite a teammate, create an API key/MCP — each deep-linking to the existing page; collapse to a progress pill after the core 3.

**7. Fix the nested-NavLink brand and give the changelog a real entry point** · impact: medium · effort: S

*Why:* Clicking 'What's new' navigates to Home due to anchor nesting (App.tsx:114-122); the changelog and its release-spotlight styling (index.css:190) are dead assets.

*How:* Split brand into a div with two sibling links; add Changelog to the palette; show a one-time sparkle dot on the brand-ver after an update to a version the user hasn't seen (persist seen-version in localStorage).

**8. Focus trap + Escape + focus-return for Modal, confirm, and drawer** · impact: medium · effort: S

*Why:* Baseline dialog accessibility is missing (ui.tsx:142-157, confirm.tsx:40-56); Escape working in the palette but nowhere else feels broken to keyboard users.

*How:* One useDialog hook: trap Tab within the container, close on Escape, restore focus to the invoker, set initial focus. Wire into Modal, ConfirmProvider, CopilotDrawer, and the FS-browser modal in AppDetail.

**9. Bundle fonts locally** · impact: medium · effort: S

*Why:* Air-gapped/LAN racks lose the entire visual identity and every load pings Google (index.css:1) — directly contradicts the 'your data' promise printed on the login screen.

*How:* Vendor woff2 subsets of Space Grotesk 500-700, Inter 400-600, IBM Plex Mono 400-600 via @fontsource into the Vite bundle; they embed in the Go binary like every other asset.

**10. Honest freshness: bind LIVE to stream health, surface lastSeen** · impact: medium · effort: S

*Why:* A status console that shows stale data under a lit LIVE badge (Home.tsx:138) loses operator trust the first time they catch it.

*How:* Drive the badge from DataProvider's connected flag ('LIVE' / 'RECONNECTING' with warn LED); refetch events on alert events too (data.tsx:83-91); show 'seen Xs ago' from node.lastSeen on rack units and machine detail when >10s.

**11. Toast dismissal + persistent error detail** · impact: medium · effort: S

*Why:* Deploy-failure reasons vanish after 7s with no way to copy them (toast.tsx:34), forcing a hunt through the logs tab for text the UI already had.

*How:* Add an X button per toast; errors persist until dismissed; add optional action link ('View logs') on failure toasts from deploy/action call sites; add 'Copy details' for errors >120 chars.

**12. Type-to-confirm for high-blast-radius deletions** · impact: medium · effort: S

*Why:* Removing a wildcard domain routing N apps or deleting an app is one click past a generic confirm (Network.tsx:95-110); competitors gate these with name confirmation for good reason.

*How:* Extend ConfirmOpts with requireText; apply to app removal (when volumes exist), domain removal (when routed apps > 0), org deletion, and member removal of the last admin.

#### Missing features

**1. Notification center with alert/event history** · impact: high · effort: M

Alerts vanish from the UI the moment they resolve and the activity feed caps at 80 in-memory events (data.tsx:42) — there is no way to answer 'what happened overnight?'. Kubernetes/Portainer users expect a bell with unread state and a filterable history; the events API already exists.

**2. Global search across all entities** · impact: medium · effort: L

The palette finds apps and machines only; users coming from bigger platforms expect one search hitting domains, env var keys, deployment logs, events, and tickets — especially valuable once a fleet has dozens of services.

**3. Guided product tour / demo data mode** · impact: medium · effort: L

The empty dashboard before the first machine joins is the highest-drop-off moment; Coolify-style seeded demo entities or a 4-stop spotlight tour (rack, deploy, networking, copilot) would let the marketing-site visitor feel the product before owning hardware.

**4. Light theme + system preference** · impact: medium · effort: M

Every reference product (Cloudflare, EnvKit, Dokploy, Portainer) ships both modes; dark-only excludes daylight/office use and some visual-accessibility needs. The token architecture (index.css:11-49) makes this mostly a second :root block plus a toggle persisted per user.

**5. Bulk / multi-select actions on apps** · impact: medium · effort: M

Restarting five apps after a machine reboot is five click-wait cycles on Apps.tsx; Portainer and every container UI offer checkbox multi-select with stop/restart/start batch actions.

**6. Installable PWA with offline shell** · impact: medium · effort: M

Homelab operators live on their phones; a manifest + service worker caching the shell (assets are already versioned with the binary) gives a home-screen app for near-free and pairs with the existing notification webhooks story.

**7. i18n framework** · impact: low · effort: L

Zero extraction today means the cost grows with every string added; low urgency for the current English-speaking homelab audience but worth deciding deliberately before the string count doubles.

**8. Keyboard shortcut system + '?' cheat sheet** · impact: low · effort: M

An 'operator's console' with exactly one shortcut undersells its own identity; g+a/g+m navigation, j/k list movement, and Esc consistency are table stakes in Linear-class tools this audience uses daily.

**9. Per-page document titles and favicon status** · impact: low · effort: S

All tabs read 'RackOps' (index.html); no title updates per route and no favicon change when something is failing — Cloudflare-bar polish that costs an afternoon (a useTitle hook + a red-dot favicon swap driven by the existing attention count in App.tsx:100).

#### Product ideas

**1. Rack Doctor — one-click fleet diagnosis wired into support**

A single 'Diagnose my rack' action (Home, palette, and the failed-state cards) that runs the checks the backend already exposes — networkDiagnosis, domain DNS/edge/cert probes, agent lastSeen, disk pressure, failed deploys — and renders an ordered fix-it list in the LED language with copy-paste commands per finding ('forward port 443', 'systemctl restart rackops-agent'). One button attaches the (redacted) report to a support ticket, so the existing CRM relay receives structured diagnostics instead of 'it doesn't work'. Zero new agents, zero config — it composes APIs the dashboard already calls, and it converts the product's telemetry depth into its support moat.

**2. Wall mode — the rack as a physical status screen**

A read-only fullscreen route (/wall, token-authenticated, no chrome) showing the signature rack view, oversized LEDs, temps, and the live activity ticker — auto-cycling, burn-in-safe, designed for the spare monitor or wall tablet every homelabber owns. The LED/rack design language was practically built for this; it's one route reusing Rack.tsx + DataProvider, and every screenshot of it on r/homelab is free marketing for the 'operator's console' identity.

**3. One grammar everywhere: verb-aware palette sharing the MCP vocabulary**

Promote ⌘K into a command line that parses the same verbs the MCP server and CLI expose: 'restart jellyfin', 'logs postgres', 'deploy nginx on node-2', 'expose grafana'. Because rackops mcp already defines the tool surface (Profile.tsx MCP card), the palette becomes a third client of one grammar — teachable ('type restart '), autocompleted from live entities, and perfectly on-brand: the operator console gets an actual console, in the browser, with no new backend.

### Observability: logs, events, alerts, metrics history, uptime monitoring, notifications

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| Live runtime log tail (docker/compose logs -f) | solid | Genuinely well-engineered pipeline: agent runs `logs -f` into a logSink (backend/internal/agent/logstream.go) that accumulates + forwards over a chunked POST to /api/agent/logstream, which fans out as `log_chunk` SSE events (server/templates_logs.go:84-118). Careful backpressure (non-blocking sends, drop-from-wire-not-from-buffer), 15-min follow cap with client re-arm at 10 min (AppDetail.tsx:469-503), follower replacement by stream ID, compose-stack aggregation supported. Seeds with a 200-line tail. |
| One-shot log fetch (snapshot) | basic | Queues a TaskLogs for the agent and blocks the HTTP request up to 10s waiting for the next agent report round-trip (templates_logs.go:23-55). Fixed 500-line tail (agent/agent.go fetchLogs), handles compose/container-id fallback. No tail/since/grep params, no download, 504 if node offline. |
| UI-level log clearing | basic | 'Clear old' sets Service.LogsClearedAt; subsequent fetches/follows pass --since (types.go:243, templates_logs.go:33-36). Purely presentational — nothing deleted, honest UI copy about it. |
| Deploy/build logs | solid | Build output streams live during deploys (stream ID = deployment ID), appends to Deployment.Log capped at 200k live (templates_logs.go:123-134), final log trimmed to last 20k (api.go:504-508). Deployments tab shows per-version history with trigger, duration, status, expandable log, live badge while building (AppDetail.tsx:565-624). Late-joining viewers get the accumulated record. |
| Events feed ('Live activity') | basic | Append-only feed capped at 500 entries (store.go:238-250), org-filtered read via eventInOrg (api.go:660-692), rendered as a terminal widget on Home limited to 80 (data.tsx:42, Home.tsx:140-149). No pagination, no kind/service/date filter, no user attribution (Event has no userID), refreshed via poll piggybacking on service_update rather than pushed. |
| Alert engine (node thresholds + node_down) | basic | Four hardcoded rules — CPU>90% warn, mem>90% warn, temp>85C crit, disk>92% crit (placement.go:136-154) — with a 3-consecutive-report sustain window and automatic resolution (placement.go:156-222). node_down watchdog marks offline after 15s of silence (main.go:169-176, placement.go:224-252), auto-resolves on next report. No service-level rules, no configurable thresholds, no ack/mute; 'deploy_failed' kind documented in types.go:304 but never raised anywhere. |
| Alerts UI ('Needs attention') | basic | Home shows open alerts, failing apps, and offline machines as cards with one-click paths to logs/deployments (Home.tsx:48-93). alert/alert_resolved SSE upserts plus an 8s convergence poll (data.tsx:83-100) — resolution UX is genuinely good. But there is no alerts page, no history view (API supports ?all=true, api.go:702, with zero UI), no acknowledge/silence. |
| Metrics (live gauges only) | basic | Rich 2s-cadence snapshot per node — CPU/load/mem/swap/temp/uptime/containers/processes plus GPU VRAM/util/temp/power, per-drive throughput+health, per-NIC rates (types.go:96-110, telemetry pkg) — and per-container CPU/mem synced onto services (api.go:151-161). But each report overwrites the last: zero server-side history. The only 'chart' is a client-side 40-sample CPU sparkline on MachineDetail, accumulated in a module Map, lost on page reload (Machines.tsx:50-66,114-118). |
| Notification channels | basic | Org-scoped Discord embed, Slack text, Telegram bot, and generic JSON webhook (notify/notify.go), with two global toggles: NotifyDeploys and NotifyAlerts (settings.go:78-129, SettingsPage notify tab). Fire-and-forget with 8s timeout, errors only logged; no test button, no retry, no delivery history, no per-event routing, no email/SMTP. |
| SSE hub | solid | Org-scoped server-side filtering (sse.go:15-49), slow-client drop policy so a stalled browser never blocks telemetry, 512-message client buffers sized for log bursts. Clean design — but see the log_chunk scoping hole in limitations. |
| External checks: domains/edge only | basic | A 30s background verifier does DNS-vs-expected-IP, an edge HTTP probe, and a TLS cert probe capturing issuer/expiry (domains.go:26-46,139,175-238). This is the only outside-in checking in the product; it monitors the edge, not the apps. Cert expiry is captured but never alerts. Per-service HTTP/uptime monitoring, external status checks, and a status page do not exist — the catalog even ships Uptime Kuma as a template (catalog.go:55), effectively outsourcing the product's own gap. |

#### Limitations & weaknesses

**1. log_chunk SSE events broadcast install-wide, leaking logs across orgs**

handleLogStream fans every chunk out with a.Hub.Broadcast (org "") — templates_logs.go:105 and :116 — so every connected dashboard in EVERY organization receives every org's runtime and build log text, directly violating the hub's own contract ('a browser must never receive another org's data just to discard it', sse.go:13-14). Compounding it, handleStartLogStream and handleStopLogStream skip serviceForOrg entirely and look the service up raw (templates_logs.go:149-154, :179-183), so any authenticated user in any org can start a live tail of any service by ID. In the multi-tenant product story this is a data-exposure bug, not a polish item.

**2. No metrics history anywhere on the server**

Node.Metrics is a single snapshot overwritten on every 2s report (api.go:116); Deployments/Services likewise hold only current CPU/mem. A user asking the most basic homelab questions — 'was the CPU pegged last night?', 'is memory creeping?', 'when did disk start filling?' — gets nothing. The lone sparkline is 40 client-accumulated CPU points on MachineDetail, wiped on reload (Machines.tsx:50-66). Portainer, Coolify, and even `docker stats`+Grafana beat this.

**3. node_down — the most important alert — never reaches notification channels**

The watchdog loop only calls store.AddEvent and Hub.Broadcast (main.go:169-176); NotifyAlert is invoked solely from handleIngest for threshold alerts raised during a report (api.go:186-191). A machine that dies silently produces a critical alert visible only in a dashboard nobody has open. For a homelab PaaS whose pitch is 'machines behind CGNAT in your closet', missing offline-machine push notifications is the #1 user betrayal.

**4. No recovery notifications and no runtime-crash notifications**

Resolved alerts skip NotifyAlert (api.go:192-195) so channels get the alarm but never the all-clear. A container that crashes at runtime generates only feed events (reconcileNode, api.go:328-349); if auto-heal succeeds the org may receive a confusing '✅ Deployed' message, and if the heal budget exhausts (5 attempts) the app stays down with zero outbound notification.

**5. Observability data is never pruned and lives inside state.json**

Resolved alerts accumulate forever in Store.Alerts (placement.go:202-205 only flips a bool; nothing deletes), and every Deployment record with up to 20k of log text is kept for life (api.go:504-508). The whole store — logs included — is re-serialized with MarshalIndent and rewritten to disk every 2s while dirty, and replicated to raft followers (store.go:155-177). An active install degrades its own control plane with its build-log history.

**6. Events feed is too small and too thin to serve as an audit trail**

500-entry cap (store.go:244-247) means a busy fleet's history evaporates in hours; the API has no pagination, kind/service/time filters, or search (api.go:674-692); the UI shows only 80 (data.tsx:42); events record no acting user, so 'who redeployed prod?' is unanswerable.

**7. Each log subscription opens its own SSE connection**

subscribeLogChunks calls subscribe(), which constructs a fresh EventSource per call (api.ts:257-269, 284-291). AppDetail with DataProvider + LogsTab + DeploymentsTab holds 3 concurrent SSE connections; over HTTP/1.1 browsers cap ~6 per host, so a second dashboard tab can wedge all live streaming with no error surfaced.

**8. Live tails can silently drop lines**

Both the hub (slow client drop, sse.go:44-47) and the agent sink (wire backpressure drop, logstream.go:44-46) discard chunks by design, but the UI never indicates a gap — a user watching a live tail during a log burst can miss the exact error line they were waiting for and never know.

**9. Alert thresholds are hardcoded and sustain state is process-local**

90/90/85/92 thresholds are compile-time constants (placement.go:136-154) — no per-node overrides (a Pi that idles at 80C vs a Xeon), no service-level rules (restart loops, OOM kills, unhealthy healthchecks are invisible). alertSustain is a process sync.Map (placement.go:161) so restarts/HA failover reset counters; the comment claims 15-20s sustain but at the agent's 2s interval (agent.go:67) three reports is ~6s.

**10. Logs UI lacks table-stakes tooling**

No download button, no search/filter, no timestamps (docker logs run without -t: runtime.go:369-397), no line-wrap toggle, no per-compose-service selection, and the buffer truncates at 200k chars (AppDetail.tsx:478). Historical logs die with the container: any redeploy replaces the container and its `docker logs` history is gone.

#### Improvements

**1. Org-scope log streams: BroadcastOrg for log_chunk + serviceForOrg on stream start/stop** · impact: critical · effort: S

*Why:* Cross-tenant log exposure contradicts the product's own multi-tenancy contract and would be a disclosure-worthy vulnerability the moment a second org exists.

*How:* In handleLogStream resolve the service's org (serviceInOrg path already exists) and use a.Hub.BroadcastOrg(orgID, "log_chunk", ...) at templates_logs.go:105/116; replace the raw store lookups in handleStartLogStream/handleStopLogStream (templates_logs.go:149,179) with the serviceForOrg helper used by every other service endpoint.

**2. Server-side metrics ring buffer + real charts** · impact: high · effort: M

*Why:* Turning the already-flowing 2s telemetry into 24h of visible history is the biggest perceived-quality jump available; it's what makes MachineDetail feel like Cloudflare's dashboards (the user's explicit design north star) instead of a static spec sheet.

*How:* Keep an in-memory ring per node (downsample to one point/15s ≈ 5.7k points/day for CPU, mem, temp, net, per-drive) outside state.json, optionally snapshotted to a compact sidecar file; expose GET /api/nodes/{id}/metrics/history?window=24h; render area charts on MachineDetail and a per-service CPU/mem chart on AppDetail Overview. No new deps server-side.

**3. Log download, search, and timestamps** · impact: high · effort: M

*Why:* Every incident ends with 'let me grep the logs' — today users must SSH to the box, defeating the product's purpose.

*How:* Add ?tail=&since= to /api/services/{id}/logs and pass -t for timestamps (runtime.go:375-397); add a Download button that requests a large tail and saves as a file; add a client-side find-in-buffer input with match highlighting in LogsTab (AppDetail.tsx:524-560).

**4. Send node_down (and recovery) through notification channels** · impact: high · effort: S

*Why:* 'My server went offline and RackOps told my phone' is the single highest-value alert this product can deliver; today it's dashboard-only.

*How:* Have the watchdog loop call api.NotifyAlert for the alert MarkOfflineNodes creates (return it alongside the node, main.go:169-176), and add a NotifyResolved send for the resolved list in handleIngest (api.go:192-195) plus node_down resolution in EvaluateAlerts.

**5. Prune alerts and deployment history; move logs out of the hot state path** · impact: medium · effort: M

*Why:* Unbounded resolved alerts + per-deployment 20k logs inside a JSON blob rewritten every 2s and raft-replicated will visibly degrade active installs within months.

*How:* Cap resolved alerts (keep last ~200), cap deployments per service (~30, keep failed ones longer), and store deployment logs in per-deployment files under dataDir referenced by ID rather than inline in state.json (store.go:155-177).

**6. Events API pagination + filters + user attribution** · impact: medium · effort: M

*Why:* The feed is the product's flight log, but it can't answer 'what happened to app X last week' or 'who did this'.

*How:* Add userID to Event and stamp it in handlers that act on behalf of a session; add ?kind=&service=&node=&before=&limit= to /api/events; raise the cap to a few thousand with the pruning work; add a filterable Activity view (per-service timeline on AppDetail reusing Event.ServiceID which is already recorded).

**7. Configurable alert thresholds per org** · impact: medium · effort: M

*Why:* Hardcoded 85C/90% thresholds misfire on heterogeneous homelab hardware (Pis, old laptops, fanless boxes) — false alarms train users to ignore alerts.

*How:* Add threshold overrides to the org Settings blob (cpuPct, memPct, tempC, diskPct, node-down grace) with the current values as defaults; EvaluateAlerts reads them via SettingsFor (placement.go:136-154).

**8. Single shared EventSource with a handler registry** · impact: medium · effort: S

*Why:* 3+ SSE connections per tab hits the browser's 6-per-host HTTP/1.1 limit with just two tabs open, silently killing live features.

*How:* Make subscribe() in api.ts lazily open one EventSource and multiplex handlers (add/remove per subscription, close when the last unsubscribes); subscribeLogChunks rides the same connection.

**9. 'Send test notification' button per channel** · impact: medium · effort: S

*Why:* Webhook setup is fail-silent today (errors only reach the server's stdout, notify.go:67); users paste a Discord URL and wait for an outage to learn it was wrong.

*How:* POST /api/settings/test-notification sends a synchronous test to each configured target and returns per-channel status; surface results as badges in the Notifications tab, mirroring the existing AI-provider test pattern (SettingsPage.tsx:157-163).

**10. Indicate dropped chunks in live tails** · impact: low · effort: S

*Why:* Silent gaps in a log tail during bursts are worse than no tail — users trust what they see.

*How:* Sequence-number chunks per stream in logSink; the client detects a gap and renders a one-line '… some output skipped (burst) — use Download for the full log' marker.

#### Missing features

**1. Per-service HTTP/TCP health checks with response-time and uptime tracking** · impact: critical · effort: L

This is the defining observability feature of the competitive set (Coolify healthchecks, Kubernetes probes, Uptime Kuma — which RackOps ships as a catalog template, tacitly admitting the gap). Today 'running' means 'container process exists'; an app returning 500s or hanging shows green. The control plane already knows every service's URL/port and has agents on every node to probe from.

**2. Historical/persistent logs that survive redeploys** · impact: high · effort: L

Every redeploy replaces the container and destroys its `docker logs` history — the logs of the crashed version, the exact thing you need, are gone. Dokploy/Coolify users hit this too, but Kubernetes users expect previous-container logs (kubectl logs --previous) at minimum. A bounded per-service ring file on the node (agent-side) would fix it without a log stack.

**3. Docker healthcheck status, restart counts, and OOM-kill detection** · impact: high · effort: M

ContainerStatus reports only running/exited (types.go:631-649) — no health=unhealthy, no RestartCount, no OOMKilled flag. A crash-looping or OOM-killed app under `restart: always` shows healthy forever. One `docker inspect` field away, and it feeds the missing service-level alerts (restart-loop, unhealthy).

**4. Public status page** · impact: medium · effort: L

'Self-host your apps AND show your users they're up' — Uptime Kuma's flagship feature and a natural fit once HTTP checks exist; servable through the existing Traefik edge on a user domain.

**5. Email (SMTP) notification channel** · impact: medium · effort: M

Every competitor has it; homelab users without Discord/Slack (and for paging reliability) expect email. The product already sends email via the vendor CRM relay for password resets but offers no direct SMTP for alerts.

**6. Notification routing rules** · impact: medium · effort: M

Two global booleans (deploys, alerts — settings.go:87) is far below expectations: no per-severity routing (critical→Telegram, info→Discord), no per-project/service scoping, no quiet hours. Alert fatigue kills channel adoption.

**7. Prometheus /metrics endpoint (or OTLP export)** · impact: medium · effort: M

Users who already run Grafana (a large fraction of the homelab audience — Grafana is in the catalog) get zero integration; a /metrics endpoint exposing node+service gauges makes RackOps a citizen of existing stacks instead of a silo.

**8. Cert-expiry alerting** · impact: medium · effort: S

Domain.CertExpiry is already probed and stored every 30s (domains.go:139) but nothing alerts as expiry approaches — the classic silent-failure a PaaS exists to prevent. Trivial rule on existing data.

**9. Webhook signing, retries, and delivery log** · impact: low · effort: M

The generic webhook is unsigned fire-and-forget with no retry (notify.go:26-71); anything mission-critical built on it (PagerDuty bridge, ntfy) is unreliable and unverifiable.

#### Product ideas

**1. Flight Recorder: zero-config embedded metrics+logs history on every agent**

Each agent keeps a 7-day local ring of downsampled metrics and per-service log files (bounded, rotating, no Prometheus/Loki, no config — it's just there, matching the single-binary identity). The control plane queries agents on demand, so history costs the server nothing and survives control-plane restarts. The payoff UI: click any alert or failed deploy and get an 'incident view' — synced CPU/mem/temp charts and the service's logs from that exact window, including logs of the container that no longer exists. No competitor in this class has post-mortem debugging built in.

**2. Mesh-powered uptime monitoring with a one-click public status page**

Every service with a public URL gets automatic HTTP checks — not just from the control plane but from OTHER machines in the WireGuard mesh, giving multi-vantage verdicts for free from hardware the user already enrolled ('app down' vs 'that node's ISP down' vs 'edge cert broken'). Response-time history feeds per-app uptime badges, and one toggle publishes a status page at status.<your-domain> through the existing Traefik edge with the domain/ACME machinery already built. Turns the awkward 'deploy Uptime Kuma to watch the platform that runs it' into a native superpower only a mesh-based PaaS can offer.

**3. Ops digest: the fleet writes you a weekly report**

A scheduled digest to the org's configured channels (and the dashboard) that turns raw events+history into narrative: 'This week: 14 deploys (1 failed), jellyfin auto-healed twice on Tuesday (OOM — consider a memory limit), disk on node-2 grows 1.1 GB/day and fills in ~23 days, uptime 99.7%.' Trend math is simple once the flight recorder exists, and the already-integrated Copilot provider (Anthropic/Gemini/OpenAI/Ollama settings are built) writes the prose. Homelab operators don't watch dashboards — this makes the platform proactively tell them what needs attention before it pages them.

### AI assistant, API ecosystem, extensibility

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| AI Copilot chat (POST /api/assistant + CopilotDrawer) | basic | Multi-provider: Anthropic, Gemini, OpenAI-compatible incl. Ollama (assistant.go:269-357), org-scoped keys with auto-pick order, graceful fallback to a builtin rule engine on provider error (assistant.go:65-70). Grounded in a compact live snapshot: nodes+CPU/mem/temp/GPU/drives, services, open alerts (fleetContext, assistant.go:115-165). But strictly single-turn (request body is one message, assistant.go:20-22), no streaming, no tool use/actions, Anthropic capped at 1024 tokens, blocking 60s HTTP call (assistant.go:360). |
| Builtin zero-config rule engine | basic | Four keyword intents — placement ranking via PlaceWorkload, thermal report, failed-services triage, storage report — plus a fleet-summary default (assistant.go:168-265). Genuinely useful answers with no key configured; on-brand for 'zero config'. Not org-scoped and returns markdown the drawer renders as raw text. |
| AI provider settings + test (Settings → Copilot AI) | solid | Per-org provider pin or auto, model pickers with sensible datalist presets incl. Ollama models, secret masking on GET and keep-on-masked-write (settings.go:16-76), /api/assistant/test validates the saved key end-to-end (assistant.go:76-112). Test requires saving first — cannot validate a key before persisting it. |
| MCP server (rackops mcp, stdio) | basic | Clean hand-rolled JSON-RPC 2.0 / MCP 2024-11-05 stdio server (mcp.go:22-106). 8 tools: list_machines, list_apps, get_app, deploy_app, app_action, app_logs, remove_app (confirm-gated), open_alerts. Thoughtful touches: name/prefix service resolution with ambiguity errors (mcp.go:178-212), secret env redaction before LLM context (sanitizeService, mcp.go:216-236). Gaps: no env/volume/domain args on deploy_app, no templates/placement/domains/env-edit/deployment-history tools, no org selection, stdio-only (no remote HTTP transport), no MCP resources/prompts. |
| CLI client (rackops nodes/services/deploy/alerts/ask) | skeletal | Five read/deploy commands over the REST API with RACKOPS_SERVER/RACKOPS_TOKEN (cli.go:15-122). No logs, no restart/stop, no domain ops, no --wait on deploy, no JSON output flag for scripting, no org selection. |
| OpenAPI 3 document + embedded Swagger UI | basic | Genuinely nice architecture: docs generated from the same route table the router registers, with a test proving zero drift both directions (openapi.go:26-125, openapi_test.go:16-68). ~70 endpoints tagged and summarized; Swagger UI vendored into the binary for airgapped installs with an operator-console dark skin (swagger.html). But operations carry no requestBody, no response schemas, no query params, no error codes (openapi.go:142-165) — it is a route list, not a contract. |
| API keys (rk_ tokens) | basic | Hashed at rest, act with owner's role (never a super-credential), optional expiry, throttled last-used stamping, instant kill on account deletion (auth.go:388-419), X-Api-Key or Bearer accepted (auth.go:347-353). No scopes: any key has its user's full write power — a CI key can delete every service. |
| Outbound notifications (Discord/Slack/Telegram/generic webhook) | basic | Per-org targets, deploy-outcome and alert events only, fire-and-forget with 8s timeout (notify.go:26-71, settings.go:104-129). No HMAC signing, no retries, no delivery log, no test-fire button in Settings, no other event types (service crash, node offline are alerts, but domain verified, member added, etc. are not). |
| SSE event stream (/api/stream) | basic | Org-scoped server-side filtering, token-via-query for EventSource, slow-client drop protection (sse.go:15-89). Built for the dashboard; usable by integrators but event names/payloads are undocumented and unversioned. |
| Git deploy (repo+branch+token clone-and-build) | skeletal | As a CI/CD integration surface: agent does depth-1 clone + Dockerfile build (agent/build.go:22-30), token stored and never sent to UI/MCP (types.go:201). No webhook receiver, no GitHub App, no commit statuses, no auto-deploy on push — redeploys are manual or raw API calls. |
| Plugin/extension story | skeletal | None. The 49-template catalog is hardcoded in Go (catalog/catalog.go:47) with no user-defined templates; arbitrary compose YAML on service create is the only escape hatch. No hook points, no custom commands, no terraform provider. |

#### Limitations & weaknesses

**1. Assistant context leaks all organizations' fleet data (multi-tenant break)**

handleAssistant builds the LLM context via a.fleetContext() at assistant.go:28 — before requireOrg even runs — and fleetContext iterates the global a.Store.Nodes/Services/Alerts maps with zero org filtering (assistant.go:136-162). Every other handler filters by org (e.g. handleListNodes, api.go:557-568). Result: org B's node names, service names, domains, and alerts are serialized into org A's prompt and shipped to org A's configured third-party AI provider. The builtin engine has the same flaw (assistant.go:168-265, PlaceWorkload over all nodes). On any install actually using orgs as tenant boundaries this is a confidentiality bug, not a polish issue.

**2. Copilot has no memory — every question starts from zero**

The API accepts only {message} (assistant.go:20-26) and CopilotDrawer keeps the visible history but never sends it (CopilotDrawer.tsx:44-46). 'Why is node2 hot?' followed by 'and what should I move off it?' fails — the second turn has no idea what 'it' is. Every competitor chat UX (and every LLM API being called, which natively takes a messages array) supports this trivially.

**3. Copilot is read-only while MCP exposes actions — backwards priority**

An external Claude/Cursor via rackops mcp can deploy, restart, and remove apps (mcp.go:110-174), but the in-product Copilot — the surface every user actually sees — can only talk (assistant.go:49-71). 'Restart the failing app' answers with instructions to click a button instead of a confirm-and-do flow. The tool surface already exists server-side; it is simply not wired to the chat.

**4. OpenAPI document has no schemas, making it decorative as an integration surface**

Operations get tags/summary/security and inferred path params only (openapi.go:142-165). POST /api/services — the single most important endpoint — documents no request body at all, so Swagger 'Try it out' presents an empty editor and openapi-generator/terraform-provider-openapi emit clients full of interface{}. Query params (?all=true, ?project=, ?node=) and error responses are also absent. The no-drift test (openapi_test.go) guarantees the route list is complete but nothing about usability.

**5. No push-to-deploy: git services cannot auto-redeploy on commit**

The route table (openapi.go:26-125) contains no webhook receiver — no /api/hooks/github, no per-service deploy-hook URL. Users deploying from a repo (agent/build.go) must click Redeploy or script a curl with a full-power API key from CI. Dokploy, Coolify, and even Watchtower-class tools treat push-to-deploy as table stakes; this is the single most-hit wall for the git-deploy user.

**6. API keys are all-or-nothing**

APIToken has no scopes field (types.go:422-432) and claimsForAPIKey returns the owner's full role (auth.go:418). The key you paste into a GitHub Action to redeploy one app can delete every service, read settings, and remove machines. Combined with the missing webhook receiver, CI integration currently requires handing out owner-power credentials.

**7. MCP and CLI are locked to the user's default org**

cliClient sends only Authorization (cli.go:134-143); the X-Rackops-Org header the server resolves (orgs.go:25) is never settable via flag or env. A user in two orgs can only ever manage their first org from MCP/CLI, silently — tools operate on the wrong fleet with no error.

**8. MCP deploy_app cannot express a real deployment**

Inputs are name/image/port/node only (mcp.go:139-147): no env vars, no volumes, no domain, no template ID, no project/env. Deploying Postgres via MCP yields a container with no password and no persistence — the AI then needs the human to finish the job in the dashboard, defeating the purpose.

**9. Viewers get a hard 403 from the Copilot**

authMiddleware blocks every non-GET for RoleViewer except /api/tokens (auth.go:379-383), and /api/assistant is a POST. The most read-only feature in the product — asking questions — is denied to the read-only role. The drawer just surfaces the raw error.

**10. Copilot answers render markdown as literal asterisks**

The drawer prints message text with whiteSpace: pre-wrap and no markdown rendering (CopilotDrawer.tsx:91-97), while the builtin engine deliberately emits **bold** (assistant.go:185, 204) and every LLM defaults to markdown. Day-one visual jank in the flagship AI surface.

**11. Outbound webhooks are unsigned, unretried, and cover two event types**

notify.Send is a single fire-and-forget POST with no HMAC signature, no retry/backoff, no delivery history (notify.go:26-71), and only deploy-outcome + alert triggers (settings.go:104-129, called from api.go:190,538). Receivers cannot verify authenticity; a 30-second receiver outage silently loses the 'deploy failed' ping.

**12. No cost/rate guard on the assistant, and Gemini key rides in the URL**

/api/assistant has no rate limiting or usage accounting — any org member can loop requests against the org's paid Anthropic key. The Gemini call puts the API key in the query string (assistant.go:303), where proxies and server logs capture it; Google supports the x-goog-api-key header for exactly this reason.

#### Improvements

**1. Org-scope the assistant context (fix the cross-tenant leak)** · impact: critical · effort: S

*Why:* Fleet data from every org is currently serialized into any org's LLM prompt (assistant.go:28,136-162) — a confidentiality bug that undermines the entire orgs feature and could not ship in a security review.

*How:* Move requireOrg above fleetContext in handleAssistant, pass org.ID into fleetContext/builtinAssistant, and filter Nodes/Services/Alerts by OrgID exactly as handleListNodes does (api.go:562-567). Add a regression test with two orgs asserting org B's node name never appears in org A's context string.

**2. Copilot actions with in-chat confirmation** · impact: high · effort: L

*Why:* The highest-leverage AI feature: 'restart the failing app', 'deploy uptime-kuma on node2' executed from chat. The action surface already exists (MCP tools, service handlers); only the wiring is missing. This is the demo that sells the product.

*How:* Add provider tool-calling (all three APIs support it) with a server-side tool registry mirroring mcpTools() but calling handlers in-process with the caller's claims. Mutating tools return a pending-action payload; the drawer renders an Approve/Cancel card and POSTs approval. Builtin engine keeps read-only intents.

**3. Add request/response schemas to the OpenAPI document** · impact: high · effort: M

*Why:* Unlocks the whole ecosystem story: usable Swagger try-it-out, generated Go/TS/Python clients, and the prerequisite for any terraform provider. Currently integrators reverse-engineer payloads from the dashboard's network tab.

*How:* Extend the route struct (openapi.go:16-23) with optional reqBody/respBody referencing hand-written components/schemas for the ~15 core types (Service, Node, CreateServiceRequest, Domain, ...), plus query-param declarations. Extend the drift test to require a schema on every POST/PATCH.

**4. Scoped API keys** · impact: high · effort: M

*Why:* CI and MCP integrations currently demand owner-power tokens; least-privilege keys are expected by anyone coming from Portainer (RBAC tokens) or cloud PaaS.

*How:* Add Scopes []string to APIToken (read, deploy, services:write, or per-project IDs), enforce in authMiddleware after claimsForAPIKey, add a scope picker to the profile key-creation modal. Default existing keys to full scope for compat.

**5. Round out the MCP tool surface** · impact: high · effort: M

*Why:* An operator's AI should be able to do what the dashboard does; today it stalls at 'now set the env vars yourself'. Cheap wins, big perceived depth.

*How:* Add env/volumes/domain/template_id to deploy_app; new tools: list_templates, placement_scores, set_env (secret-aware), expose_app (nip.io), deployment_history, node_metrics; add an org argument plumbed as X-Rackops-Org (also fix cliClient to send it, cli.go:134-143).

**6. Multi-turn conversations** · impact: high · effort: S

*Why:* Follow-up questions are the natural mode of a chat drawer; today each message amnesia-resets, making the Copilot feel broken next to any 2026 chat product.

*How:* Accept messages: [{role, text}] in /api/assistant (keep message for back-compat), pass the array through to all three providers (each natively supports it), cap at ~20 turns server-side. CopilotDrawer already holds msgs — send them.

**7. Harden outbound webhooks into a real events system** · impact: medium · effort: M

*Why:* Automation consumers need authenticity and reliability; today's unsigned fire-and-forget generic webhook (notify.go:48-53) cannot drive anything that matters.

*How:* Add X-Rackops-Signature HMAC (per-org secret), 3x retry with backoff, an in-store ring buffer of last 100 deliveries surfaced in Settings, a 'send test event' button, and event-type expansion (service.deployed/failed, node.online/offline, domain.verified, alert.opened/resolved) with a typed JSON payload.

**8. Render markdown + stream Copilot answers** · impact: medium · effort: M

*Why:* Raw asterisks and a 20-second frozen spinner are the two visible quality gaps in the drawer; streaming makes even slow models feel fast.

*How:* Add a tiny markdown renderer (marked + DOMPurify or a 100-line subset), switch /api/assistant to SSE or chunked streaming for the anthropic/openai paths (both stream natively), drawer appends tokens.

**9. Let viewers use the Copilot and rate-limit it** · impact: medium · effort: S

*Why:* Read-only users are exactly who asks questions instead of clicking; and unmetered use of the org's paid key is an obvious abuse hole.

*How:* Whitelist /api/assistant for viewers in authMiddleware (auth.go:379-383) — it is read-only once context is org-scoped; add a simple per-user token bucket (e.g. 20 req/5min) and a monthly request counter shown in Settings → Copilot AI.

**10. Move Gemini key out of the URL** · impact: low · effort: S

*Why:* API keys in query strings end up in reverse-proxy and server logs.

*How:* Send x-goog-api-key header instead of ?key= in askGemini (assistant.go:303).

#### Missing features

**1. Push-to-deploy webhook receiver (GitHub/GitLab/Gitea)** · impact: critical · effort: M

Table stakes in Dokploy/Coolify: per-service deploy-hook URL (/api/hooks/deploy/{token}) plus native GitHub/GitLab signature verification, branch matching, and auto-redeploy of git services on push. The git-deploy feature is half-built without it — every git user hits this wall within a day.

**2. First-class CI/CD recipe: official GitHub Action + rackops deploy --wait** · impact: high · effort: M

Deploy-from-Actions is the second-most-common integration ask. A marketplace action (rackops/deploy-action) wrapping the existing API with a scoped key, image-tag update via PATCH, and --wait polling deployment status to fail the pipeline on failed deploys turns 'possible with curl' into 'documented and reliable'.

**3. Custom templates (per-org catalog extensions)** · impact: high · effort: M

The hardcoded 49-template catalog (catalog/catalog.go) cannot be extended without recompiling. Portainer app templates and Coolify services both support user-supplied definitions; homelabbers live on this. Accept the existing Template JSON shape pasted in Settings or synced from a git URL, merged into /api/templates per org.

**4. Remote MCP endpoint (streamable HTTP on the server)** · impact: high · effort: M

Stdio MCP requires the rackops binary installed on every operator laptop. Serving MCP over streamable HTTP at /api/mcp (authenticated by the same rk_ keys) lets claude.ai, hosted agents, and teammates connect with a URL — the direction the MCP ecosystem moved in 2025-2026, and near-free since tools already call local handlers.

**5. GitHub App integration (repo picker, commit statuses, PR preview envs)** · impact: high · effort: XL

The deep version of git integration users know from Vercel/Coolify Cloud: OAuth-less install, repo/branch dropdowns instead of pasted deploy tokens, deployment status checks on commits, and ephemeral preview environments per PR mapped to nip.io domains. Big lift but the headline feature of a 1.0.

**6. Audit log API** · impact: medium · effort: M

Orgs/RBAC without an immutable who-did-what trail is incomplete for the on-prem SME buyer; the Events feed (api.go:674-692) is close but lacks actor attribution, is trimmed, and is not queryable. Add actor to AddEvent call sites and a filterable /api/audit endpoint.

**7. CLI depth: logs, restart, status --watch, JSON output** · impact: medium · effort: S

The 5-command CLI (cli.go:42-121) cannot tail logs or restart a service — the two things you want at 2am over SSH. Add logs [-f] (reuse the SSE log stream), restart/stop/redeploy, get -o json for scripting.

**8. Terraform/Pulumi provider** · impact: medium · effort: XL

On-prem platform teams codify infrastructure; rackops_service/rackops_domain/rackops_org resources would make RackOps composable with the rest of their stack. Realistically gated on OpenAPI schemas landing first; generate the provider from the spec rather than hand-writing.

#### Product ideas

**1. Fleet Night Watch: the Copilot becomes a proactive on-call agent**

Flip the assistant from pull to push: an in-binary agent loop that runs on a schedule or on alert events, uses the org's configured LLM plus the (expanded) internal tool registry in read-only mode to correlate telemetry — 'app X OOM-looped 3 times, node2's SSD hit 91%, and the thermal trend on node1 predicts throttling by Friday' — and delivers a morning digest to Discord/Slack with one-click approve links for its proposed remediations (restart, migrate, prune). Zero new infrastructure: reuses notify targets, the alert engine, and the MCP tool surface. No competitor in the Dokploy/Coolify class has credible AIOps; RackOps already has the three ingredients wired.

**2. GitOps mode: rackops.yaml reconciliation from a single binary**

Point an org at a git repo containing a declarative fleet manifest (services, images, env refs, domains, target nodes or 'auto'). The server polls or receives the push webhook, diffs desired vs. store state, and converges — creating, redeploying, and pruning services with the existing task queue. This is 'Argo CD for people who will never run Kubernetes': the entire homelab reproducible from one YAML file in version control, disaster recovery included ('new box, rackops server, paste repo URL, fleet reappears'). It also composes with the missing push-to-deploy feature — one webhook receiver serves both.

**3. Automations: event-condition-action rules as the extensibility layer**

Instead of a plugin SDK (wrong-sized for a single-binary product), ship a small rules engine: ON service.crashed IF restarts > 3 IN 10m THEN migrate + notify; ON node.temp > 85 THEN stop lowest-priority app; ON image.updated (registry poll, Watchtower-style) THEN redeploy. A dozen curated triggers/actions exposed in a dashboard builder UI and as YAML, executed against the same internal handlers MCP uses. This converts the webhook/event work into a user-facing feature, covers the top 20 'I wish it would just...' requests without arbitrary code execution, and stays true to zero-config: rules are data, not plugins.

### Support pipeline, CRM app, marketing website, docs & changelog

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| In-dashboard support tickets (product side) | solid | Support.tsx + server/tickets.go: compose with 6 categories/4 severities, threaded replies, resolve/reopen, per-user visibility (admins see whole install queue), SSE ticket_update push + 30s poll fallback, toast UX, disclosure InfoTip. Handler bodies validate enums, trim input, reopen-on-reply. Genuinely usable end-to-end. |
| Ticket relay protocol (instance ⇄ CRM) | solid | Outbound-only forward with per-ticket poll secret (SHA-256 stored server-side, constant-time compare), idempotent message merge by ID, retry loop with ForwardPending flag, leader-only sync under HA, 403-indistinguishable unknown-ticket/bad-secret. Tested (tickets_test.go, ingest_test.go). Well-engineered for the CGNAT constraint. |
| CRM ticket console (operator side) | basic | Inbox grouped by status with 10s polling, full thread view, agent replies (always named 'RackOps Support'), internal notes, manual status changes, status semantics (customer msg→open, agent reply→pending). No assignment, search, filters beyond status, pagination, canned replies, or SLA awareness. |
| CRM installs roster + usage analytics | basic | Installs list with online LED/version/features/open tickets, install detail with capability badges + snapshot history (50), overview with version distribution, 'hero features' ranking, services-by-kind. But data only arrives piggybacked on ticket forwards — the /ingest/v1/snapshots endpoint has no sender in the Lite binary, so analytics reflect only installs that filed tickets or reset emails. |
| Password-reset email relay | solid | crm.go relayResetEmail → CRM mailer.go: dual per-IP AND per-address 5/hour limits, MIME multipart plain+HTML template, audit log of every attempt (sent/failed/unconfigured), server-log link as universal fallback. Degrades gracefully when SMTP unset. |
| CRM admin auth & hardening | basic | Single env-configured admin (CRM_ADMIN_EMAIL/PASSWORD), bcrypt with timing equalizer, 7-day HS256 JWT, login/ingest rate limiters with bucket eviction, 256KB body cap, opaque errors. Solid for one person; structurally cannot support a team. |
| Marketing website | basic | 9 polished sections (GSAP/Lenis/three.js RackField), editions comparison (Lite vs Enterprise), early-access form with per-field state and 409 dedup handling. No docs, pricing, changelog, or blog pages; footer claims MIT license; contact email early@rackops.dev vs product domain rackops.in. |
| Early-access capture | skeletal | CTA.tsx posts EarlyAccessRequest to https://api.rackops.in/api/v1/early-access — no implementation of that endpoint exists anywhere in the monorepo (grep confirms). Every signup from the live site fails; the funnel's terminal step is vapor. |
| Changelog surfacing | basic | CHANGELOG.md (well-written, user-facing voice) + in-app /changelog page linked from sidebar version + update banner polling GitHub releases 6-hourly. But Changelog.tsx hardcodes its own release array — already drifted (Unreleased items absent, only 3 releases duplicated by hand). |
| Documentation | skeletal | READMEs (good for build/install), /swagger generated from live route table (genuinely nice, airgap-friendly), InfoTips across dashboard pages. Zero user documentation: no guides for mesh, edge, domains, orgs, HA troubleshooting — nothing a Coolify/Dokploy user would call docs. Website has no docs link at all. |
| Telemetry disclosure & ethics | basic | buildUsageSnapshot sends only counts + feature flags (no names/domains/env), InfoTip discloses it plainly, snapshot travels only with user-initiated ticket forwards — ethically clean posture. But there is no opt-out toggle, no settings surface listing what's sent, and the CRM README claims 'opt-in periodic telemetry' that doesn't exist. |

#### Limitations & weaknesses

**1. Customer 'Mark resolved' never reaches the CRM**

tickets.go:185 comments 'tell the CRM it's resolved' and forwards, but the CRM's UpsertTicket update path (internal-apps/crm/backend/internal/server/store_tickets.go:128-133) only refreshes subject/category/severity/email — status is never taken from instance forwards. Result: every ticket a customer resolves stays open/pending in the operator inbox forever, silently inflating the queue with zombies.

**2. Agent replies to resolved tickets are never delivered**

RunSupportSync only polls tickets where Status != resolved (tickets.go:344). If support replies to a ticket the customer resolved locally (CRM AddMessage flips it to pending, store_tickets.go:243), the instance never polls again and the reply is lost until the customer happens to reopen by writing first.

**3. Failed forwards are invisible to the user**

When crmPost fails, ForwardPending is set (tickets.go:307-313) and the field even survives Sanitized(), but Support.tsx never renders it. A ticket that never reached the vendor shows the identical 'waiting for support' state as one in the queue — a user with a broken outbound path waits days for an answer that was never requested.

**4. ticket_update SSE broadcasts full ticket content install-wide**

pollTicketReplies calls a.Hub.Broadcast (tickets.go:440), which sse.go:26 sends to every connected client — the payload is the entire sanitized ticket including message bodies. In a multi-org install, any authenticated viewer in any org receives other users' support conversations over SSE, undermining the per-user visibility enforced in handleListTickets (tickets.go:43).

**5. CRM is single-operator by construction**

One admin account from env vars (auth.go:63-79), every reply hardcoded as name 'RackOps Support' (admin.go:113-115), notes as 'Internal note'. No agent identity, no assignment, no way two people can work an inbox without stepping on each other. This caps the support org at exactly one human.

**6. CRM inbox has no search, filters, or pagination**

handleListTickets supports only a status filter (admin.go:74-86) and ListTickets has no LIMIT in the dashboard path (store_tickets.go:271); Installs list is similarly unbounded and unsearchable. Finding 'that ticket about WireGuard from acme-lab last month' means scrolling. Degrades linearly with success of the product.

**7. No notification when a ticket arrives**

handleIngestTicket (ingest.go:54) stores and returns — the Mailer sits right there but is never used to alert the operator. Support responsiveness depends entirely on someone keeping the CRM tab open (10s poll); an 'urgent — production is down' ticket at night is indistinguishable from silence.

**8. Early-access funnel is unimplemented — every lead is lost**

CTA.tsx:7 posts to https://api.rackops.in/api/v1/early-access; no such endpoint exists in the CRM, Lite, or anywhere in the repo. The pre-launch site's only conversion action always errors. The CRM (which already has SQLite + admin dashboard) is the obvious home but has no early_access table or page.

**9. Usage analytics have no data source of their own**

The CRM's /ingest/v1/snapshots endpoint, snapshot history UI, and README-documented 'opt-in periodic telemetry' are all dormant: the Lite binary contains zero calls to /ingest/v1/snapshots (only tickets + reset-email, tickets.go:306, crm.go:75). Version distribution and hero-features charts describe only the biased subset of installs that filed tickets.

**10. Whole-thread re-forward will eventually hit the 256KB cap**

forwardTicket re-sends every user message on every reply (tickets.go:275-295) into a 256KB body limit (crm api.go:13). A long-running thread (pasted logs are explicitly encouraged by the compose placeholder) will one day 403/413 permanently — the retry loop then re-fails forever and new replies never sync.

**11. No user documentation exists anywhere**

No docs directory, no docs site, no docs link on website or dashboard. A user who hits a WireGuard/CGNAT relay issue, a Let's Encrypt failure, or an HA question has only InfoTips and the support ticket button — meaning docs gaps convert directly into support load, which a one-person CRM cannot absorb.

**12. Changelog duplicated by hand between CHANGELOG.md and Changelog.tsx**

dashboard/src/pages/Changelog.tsx:4-35 re-transcribes releases; it already lacks the Unreleased entries and only covers 3 versions. Every release requires editing two prose sources; drift is guaranteed. The GitHub update banner links out but no release notes are shown at update time.

**13. No telemetry opt-out and no monetization hooks at all**

The usage snapshot rides every forward unconditionally (tickets.go:300) with disclosure only in a hover InfoTip — fine ethically today, but there's no settings toggle or 'what we send' viewer to point skeptics at. Meanwhile 'billing' exists only as a ticket category; no license keys, entitlements, or Enterprise upgrade path despite the website marketing an Enterprise edition.

#### Improvements

**1. Wire the early-access endpoint into the CRM** · impact: critical · effort: M

*Why:* The website's only CTA fails 100% of the time (CTA.tsx:7 → nonexistent api.rackops.in endpoint). Pre-launch, lost leads are the most expensive bug in the repo.

*How:* Add POST /ingest/v1/early-access to the CRM (email unique → 409, rate-limited like reset-email), an early_access table, a simple Leads page in the dashboard, and point the website at crm.rackops.in (or proxy api.rackops.in→CRM). Optionally send a confirmation email via the existing Mailer.

**2. Email the operator on new/reopened tickets** · impact: critical · effort: S

*Why:* The CRM has a configured Mailer and zero proactive alerting; response time is bounded by tab-watching. For a one-person support org this is the single highest-leverage change in the area.

*How:* In handleIngestTicket after UpsertTicket (created or newUserMsg), send a templated email to CRM_ADMIN_EMAIL with subject/severity/install and a deep link; batch per-minute to respect the SMTP relay. Escalate subject prefix for 'urgent'.

**3. Named agent accounts with assignment in the CRM** · impact: high · effort: L

*Why:* The moment a second person answers tickets, the single env-var admin and hardcoded 'RackOps Support' name (admin.go:114) collapse — no ownership, no accountability, duplicate replies.

*How:* agents table (email, name, bcrypt hash, role), JWT sub per agent, replies signed with agent name (instances already display m.name), assignee column + 'mine/unassigned' inbox views, assignment events as internal notes.

**4. Add search, filters, and severity-aware ordering to the CRM inbox** · impact: high · effort: M

*Why:* Status-only grouping (Tickets.tsx:40) with updatedAt sort buries urgent tickets under chatty normal ones and makes history retrieval impossible as volume grows.

*How:* SQL LIKE search over subject/body/email/install name, category/severity filter chips, urgent/high floated to top within the open group, LIMIT+cursor on ListTickets. All server-side; SQLite is fine at this scale.

**5. Propagate customer resolution to the CRM and briefly keep polling resolved tickets** · impact: high · effort: S

*Why:* Two halves of the same protocol hole: operator inbox fills with zombie open tickets (store_tickets.go:128 ignores forwarded status) and agent replies to resolved tickets vanish (tickets.go:344 stops polling). Both erode trust in the only support channel the product has.

*How:* Accept a status field on the update path in UpsertTicket when it's 'resolved' (customer-initiated resolution is legitimate); in RunSupportSync, keep polling resolved tickets for ~7 days after resolution so late agent replies (which flip status to pending) reopen the thread locally.

**6. Surface ForwardPending in the Support UI** · impact: high · effort: S

*Why:* A ticket that never left the box looks identical to one in the vendor queue (tickets.go:307-313; Support.tsx renders nothing). Users behind broken egress wait indefinitely.

*How:* The field already survives Sanitized() — render a badge on the ticket card and thread header: 'Not delivered yet — retrying' with the InfoTip explaining outbound HTTPS is needed; clear on successful forward.

**7. Scope ticket_update SSE events to the ticket's author and admins** · impact: high · effort: S

*Why:* tickets.go:440 broadcasts full ticket threads to every SSE client install-wide — support conversations leak across users and orgs in shared installs.

*How:* Either broadcast only {ticketId, updatedAt} and let the Support page refetch (it already calls load() on the event), or add a per-user hub channel. The payload-free option is a 5-line change.

**8. Generate the in-app changelog from CHANGELOG.md at build time** · impact: medium · effort: S

*Why:* Two hand-maintained prose copies of every release (Changelog.tsx:4-35 vs CHANGELOG.md) have already drifted; this tax recurs every release forever.

*How:* Small Vite plugin or prebuild script parsing CHANGELOG.md into JSON consumed by Changelog.tsx; keep the curated feature-link cards as a static header. Also show the relevant section in the update banner when an update is available.

**9. Forward only new messages after the first ticket sync** · impact: medium · effort: S

*Why:* Re-sending the whole thread every time (tickets.go:275-295) guarantees an eventual permanent failure against the 256KB ingest cap — worst for exactly the users pasting the logs support asked for.

*How:* Track lastForwardedAt per ticket; send messages after it (the CRM merge-by-ID already makes this safe). Bump the CRM cap to 1MB as a belt-and-suspenders.

**10. Add a telemetry section to Settings with an opt-out and payload preview** · impact: medium · effort: S

*Why:* The ethics are already good — make them legible. A hover InfoTip is not where skeptical self-hosters look, and there's no way to decline; that's a recurring HN-thread landmine for this product category.

*How:* Settings card: 'What travels with support tickets' rendering the live buildUsageSnapshot() JSON, plus a toggle that nils out payload['usage'] in forwardTicket. Persist per-install in Store settings.

#### Missing features

**1. User documentation site (and a docs link anywhere)** · impact: critical · effort: L

Coolify, Dokploy, Portainer, and Kamal users all expect real docs — installation, networking/CGNAT, domains/TLS, mesh, HA, troubleshooting. RackOps has literally none beyond READMEs and InfoTips; every docs gap becomes a ticket in a one-person support pipeline. This is the loudest absence in the whole area.

**2. Ticket attachments / diagnostic uploads** · impact: high · effort: M

The compose form begs for 'error messages and logs' but only offers a textarea; support then plays 20 questions. Every competitor's support flow accepts files. Needs multipart ingest + CRM storage + thread rendering.

**3. Website pages for docs, changelog, and pricing** · impact: high · effort: M

The site is a single scroll with an (unwired) email form. Evaluators comparing against Coolify/Dokploy look for docs, a living changelog (CHANGELOG.md content already exists and is good), and at least a pricing/licensing statement for Enterprise; their absence reads pre-product.

**4. Periodic opt-in telemetry sender** · impact: high · effort: S

The entire CRM analytics surface (snapshots endpoint, history table, version/feature charts, 'active this week') is built and documented but starves — no scheduled sender exists in the Lite binary. An explicit opt-in daily snapshot (default off, set during onboarding) would make version-distribution and churn data real without changing the ethics posture.

**5. Licensing/entitlement plumbing** · impact: medium · effort: L

The website markets a flagship Enterprise edition and tickets have a 'billing' category, but there is no license key, entitlement check, or upgrade path anywhere. The InstallID + CRM pairing is the natural anchor for issuing and validating licenses when monetization starts; having zero hooks means retrofitting later under commercial pressure.

**6. Canned replies and a lightweight knowledge base in the CRM** · impact: medium · effort: M

Support teams live on snippets; the operator currently retypes WireGuard/CGNAT explanations per ticket. A snippets table with slash-insert in the reply box is table stakes for any helpdesk.

**7. SLA / responsiveness metrics in the CRM** · impact: medium · effort: M

'Urgent — production is down' is collected from customers but drives nothing: no first-response timers, no age indicators, no breach highlighting, no response-time stats on Overview. The product promises 'a human answers' — the console should measure whether that's true.

**8. CSAT + resolution feedback** · impact: low · effort: S

After resolve, silence. A one-tap 'did this help?' on resolved threads (instance → forward → CRM) is cheap signal a support-led product should collect from day one.

#### Product ideas

**1. One-click diagnostic bundle attached to tickets**

When a user opens a ticket, offer 'Attach diagnostics': the binary assembles a redacted bundle it already knows how to produce — recent server log tail, alert history, node statuses, service states, failed-deploy output, mesh/edge health — secrets stripped by the same redaction used for MCP. It forwards with the ticket and renders as a structured panel in the CRM thread. Support sees the actual failing state on message one instead of after three round-trips through a 60-second polling channel. Perfectly on-brand: the single binary is the diagnostician.

**2. Docs embedded in the binary, one source of truth**

Ship docs as markdown in the repo, rendered three ways: docs.rackops.in (website), /docs inside every install (embedded like the dashboard — works airgapped, always version-matched to the running binary), and deep-linked from InfoTips ('Learn more →'). The swagger-from-live-routes trick already proves the philosophy: docs that cannot drift because they ship with the code. No competitor gives you offline, version-exact docs on your own LAN.

**3. Proactive support from opt-in health pings**

Extend the dormant snapshot channel into an opt-in daily health ping (same counts-only ethics, explicit toggle). The CRM then flags trouble before the customer does: an install stuck three versions behind, nodes offline for days, repeated failed forwards, an HA cluster down to one voter. Operator sees a 'needs outreach' queue and can open a proactive ticket that lands in the customer's Support page. Turns the support pipeline from reactive inbox into fleet care — a story neither Coolify nor Dokploy tells, and a natural bridge to paid support tiers.

### Platform: CLI, installer, updates, cluster/HA, data/backup

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| API-client CLI (nodes / services / deploy / alerts / ask) | basic | cmd/rackops/cli.go:15-121. Five subcommands over the HTTP API using RACKOPS_SERVER/RACKOPS_TOKEN or --server/--token. `nodes` and `services` print fixed-width tables, `deploy` supports --name/--image/--port/--node and an undocumented --template (cli.go:69, absent from usage in main.go:70), silently files the service under the first project's production env (cli.go:83-95). No lifecycle commands (logs/restart/stop/remove), no JSON output, hand-rolled flag parsing that silently drops unknown flags, no login flow — token must be copied from the dashboard. |
| MCP stdio server for AI tools | solid | cmd/rackops/mcp.go. Real newline-delimited JSON-RPC 2.0 (MCP 2024-11-05) with 8 tools: list_machines, list_apps, get_app, deploy_app, app_action, app_logs, remove_app, open_alerts. Thoughtful details: secret env values redacted before hitting an AI context (sanitizeService, mcp.go:216-236), destructive remove gated on confirm=true (mcp.go:343-345), name/ID/prefix resolution with ambiguity errors (findService, mcp.go:178-212). Notably richer than the human CLI. |
| One-line server installer (install.sh) | solid | scripts/install.sh. Genuinely polished: arch detection, curl/wget fallback, installs git + Docker (via get.docker.com) + compose plugin across 6 package managers, systemd unit with Restart=always, WSL2/no-systemd fallback via setsid + nohup dockerd, health-wait on /api/auth/status, docker group for SUDO_USER, branded TTY output with CI fallback. Re-running upgrades in place. Gaps: no checksum verification of the downloaded binary, no version pinning, no uninstall, and it only installs the SERVER role — there is no agent install mode. |
| Self-update (CLI `rackops update` + dashboard one-click) | basic | internal/server/update.go + main.go:88-116 + dashboard App.tsx UpdateBanner. Version check resolves the GitHub /releases/latest redirect (no API rate limits), server-side cached 6h so N dashboards = 1 request; POST /api/update is owner/admin-gated, downloads the asset, sanity-checks it by executing `version`, atomically renames over the executable, then execs into the new binary (exec_linux.go) with systemd as backstop. Dashboard polls until the new version answers and reloads. No release channel, no rollback, no checksum/signature, no version pinning, Linux-only, and it updates exactly one machine. |
| Raft HA control plane | basic | internal/raftstore/raftstore.go + server/cluster.go + main.go:203-241. Real hashicorp/raft over BoltDB with TCP transport, single-node bootstrap, join-token-authed /api/cluster/join with follow-the-leader redirects (421 + leader address), clusterGuard rejecting writes on followers, leader-only domain-verifier loop, peer list in the Network page. But replication is whole-state-JSON-per-change proposed on every dirty tick, ImportJSON drops org state entirely, agents cannot fail over (dead LeaderAddr field), and there is no peer removal or health. It demos; it would not survive a real failover intact. |
| JSON snapshot state persistence | basic | internal/server/store.go. Single ~/.rackops/state.json holding everything (nodes, services, users, tokens, orgs, tickets, JWT secret, AI keys). persistLoop marshals the whole store with MarshalIndent every 2s when dirty and writes tmp-then-rename (atomic, good). No fsync, no rotated copies, no corruption fallback, no size management. TaskQueue is persisted so queued deploys survive a restart (store.go:252-272) — a nice durability touch. |
| Backups (server state, volumes, databases) | skeletal | Nonexistent. types.go:663 declares `TaskBackup TaskKind = "backup"` but the agent's execute() switch (agent/agent.go:245-330) has no case for it and no server code ever enqueues it — a vestigial constant. README.md:59 lists 'Backups (volumes + databases) with schedules' as roadmap. No state export/import endpoints, no volume snapshot, no pg_dump/mysqldump for catalog databases, no S3/offsite anything, no dashboard surface. |
| Migration / import-export | skeletal | No importers, no export. The only migration aid is incidental: adoptExternal (server/api.go:409) adopts containers RackOps didn't create, so a machine previously run by Coolify/plain Docker shows its workloads as manageable adopted apps. Nothing maps env vars, compose files, domains, or proxy config from a competitor install. |
| Cluster status UI | skeletal | dashboard/pages/Network.tsx:251-261 renders a read-only peer list with leader/follower badges inside the Network page when /api/cluster reports enabled. No peer health, no lag, no remove/add actions, no follower-redirect handling anywhere in api.ts (a 421 from a follower surfaces as a raw error to the user). |

#### Limitations & weaknesses

**1. Backups do not exist in any form**

The flagship gap. types.go:663 declares TaskBackup but agent/agent.go:245-330 never handles it and nothing enqueues it. App data lives in rackops-managed volume dirs (agent/runtime.go:254-260, :440 'rackops-volumes/<serviceID>/<vol>') with no snapshot, schedule, restore, or offsite path. Catalog templates deploy Postgres/MySQL/Redis-backed stacks (catalog.go) with zero database dump capability. One `rm -rf`, disk death, or bad redeploy and a user's Immich photo library or WordPress DB is simply gone. Dokploy and Coolify both ship scheduled S3 database/volume backups; this is the first question any migrating user asks.

**2. HA replication silently drops orgs, org settings, invitations, and tickets**

store.go:183-212 ImportJSON copies Nodes/Projects/Envs/Services/Deployments/Users/Tokens/Alerts/Domains/Ingress/Settings/Events/ClusterHTTP/JWTSecret — but not Orgs, OrgSettings, Invitations, or Tickets. Each follower boots its own default org with different IDs (EnsureDefaultOrg, orgs.go:27). After failover, every replicated node/service references org IDs that don't exist on the new leader: org scoping breaks, machines vanish from users' views, and per-org AI keys/notification webhooks (store.go:39-41) are lost. README.md:86 claims 'Full control-plane state ... replicates' — it does not.

**3. Agents cannot fail over to a new leader — HA doesn't cover the fleet**

AgentResponse.LeaderAddr (types.go:697, 'set when this server is not the Raft leader') is never assigned anywhere in internal/server and never read by the agent (agent.go:168-197 uses only resp.Tasks and resp.Mesh). Agents pin to the one URL in cfg.ServerURL (agent.go:222) forever. If the server an agent enrolled against dies, that machine drops off the control plane even though two healthy raft peers remain — the exact scenario HA exists for.

**4. Agents reporting to a follower get their state clobbered and never receive tasks**

clusterGuard exempts /api/agent/ingest from the follower write-block (cluster.go:97-99), so a follower accepts the report and mutates its local store (api.go:86) — but follower mutations never replicate (OnDirty proposes only on the leader, cluster.go:123-127) and the next leader snapshot overwrites them. Worse, tasks are enqueued in the LEADER's TaskQueue, which is deliberately never imported (store.go:46-48, :183), and DrainTasks runs on whichever server receives the ingest (api.go:199) — so a deploy queued on the leader for an agent pointed at a follower silently never executes.

**5. state.json is rewritten every 2 seconds forever, without fsync**

Every agent report (default interval 2s, agent.go:67) calls Store.Mutate to stamp metrics (api.go:86-124), marking the store dirty; persistLoop (store.go:155-178) then MarshalIndents the ENTIRE state and rewrites state.json every 2s for the lifetime of the install. os.WriteFile + rename with no fsync of file or directory means a power loss can leave a truncated/empty state.json on common filesystems. This also grinds SD cards on the Pi-class hardware homelabbers run, and under HA the same tick proposes the full state JSON into the raft BoltDB log every 2s (raftstore.go:151-156), which retains up to 256 full-state entries between snapshots (raftstore.go:97-98).

**6. A corrupt state.json bricks the control plane with no recovery path**

NewStore (store.go:120-123) returns the json.Unmarshal error, and main.go:136-139 log.Fatalf's on it. There is no .bak, no last-known-good rotation, no --recover flag, no partial-load. Combined with the no-fsync 2-second rewrite loop above, a single ill-timed power cut can take out the entire platform — users, services, domains, mesh config — permanently.

**7. Unbounded state growth: deployments are never pruned**

Every deploy appends a Deployment carrying up to 20KB of build log (services.go:294; truncation at api.go:504-508) and nothing ever deletes them — grep shows no delete/prune of Store.Deployments anywhere. Events are capped at 500 (store.go:246) but deployments grow forever, and since the whole state is marshaled every 2s and shipped through raft as full snapshots, an active install gets slower and chattier every single deploy.

**8. Self-update has no rollback, no channel, no integrity check — and 'verification' executes the unverified download as root**

SelfUpdate (update.go:165-202) downloads the GitHub asset and its only check is RUNNING the downloaded file (`tmp version`, update.go:196) — as root, before any checksum — then renames over the live binary, destroying the old version. No sha256/signature verification against a checksums asset, no rackops.old kept, no `rackops update --rollback`, no version pinning (runUpdate, main.go:88-116, is latest-only), no stable/edge channel. A bad release strands every install with no one-command way back.

**9. No fleet update story: worker agents and HA peers never get updated**

POST /api/update (update.go:110-161) updates only the machine it runs on. Worker machines — where the binary was hand-copied (AddMachineModal.tsx: 'Copy the rackops binary to the new machine') — have no self-update task kind, no server-pushed upgrade, nothing; AgentVer is merely displayed (api.go:124). HA followers likewise stay on the old version until someone SSHes in. Version skew across the fleet is guaranteed after the first release.

**10. Cluster membership is add-only: dead peers can never be removed**

raftstore.go exposes AddVoter (159-164) but no RemoveVoter, and there is no /api/cluster/leave or remove endpoint (openapi.go:119-123 lists only status + join). A permanently dead server stays in the quorum config forever — in a 3-node cluster you're then one failure from total write outage with no API-level fix. Nothing warns against 2-node clusters (strictly worse availability than one node), and the peer list (cluster.go:167-182) carries no health/lag info.

**11. Adding a machine is manual: copy the binary yourself, and the agent dies with your SSH session**

handleJoinCommand (api.go:710-725) returns a raw `rackops agent --server http://<host> --token ... --org ...` command, and AddMachineModal.tsx:17-20 literally instructs 'Copy the rackops binary to the new machine, then run this command.' install.sh only writes a SERVER systemd unit (install.sh:248-272) — there is no agent install one-liner, no agent systemd unit, so the pasted command runs in the foreground and the node goes offline on logout or reboot. Dokploy/Coolify add nodes over SSH automatically; this is the worst moment of an otherwise excellent first-run experience.

**12. No state export/import, no DR runbook, no competitor migration**

No endpoint or CLI verb exports/restores server state (openapi.go route table has nothing); disaster recovery is 'know to tar ~/.rackops yourself while the server is mid-rewrite of state.json', undocumented anywhere. Nothing imports from Dokploy/Coolify/Portainer. Secrets (AI keys store.go:88-95, git tokens, webhook URLs, JWT secret) sit plaintext in state.json, so even a hand-rolled copy of the data dir is a secrets-handling hazard.

#### Improvements

**1. Wire agent failover to the raft leader** · impact: critical · effort: M

*Why:* HA that doesn't include the fleet is theater: agents pinned to a dead server take their machines (and all queued deploys) offline while healthy peers idle. The LeaderAddr plumbing was started (types.go:697) and abandoned.

*How:* On follower ingest, set resp.LeaderAddr from ClusterHTTP (cluster.go leaderHTTP); in agent.Run, on LeaderAddr or repeated push failures, retarget cfg.ServerURL to the advertised leader (persist last-known server list in the data dir). Also make followers proxy or redirect enqueued-task delivery so DrainTasks (api.go:199) happens against leader state.

**2. Fix ImportJSON to replicate ALL tenant state (orgs, org settings, invitations, tickets)** · impact: critical · effort: S

*Why:* HA is advertised ('full control-plane state replicates', README.md:86) but failover today breaks org scoping, hides machines from users, and loses AI/notification config. Anyone who actually runs 3 servers hits this on their first leader kill.

*How:* In store.go ImportJSON (183-212), copy Orgs, OrgSettings, Invitations, Tickets (and DashboardURL/InstanceName). Add a failover test: boot two stores, replicate, kill leader, assert org-scoped node listing is identical.

**3. Fleet-wide coordinated updates** · impact: high · effort: L

*Why:* After any release, worker agents and HA followers silently stay on old versions (no update path at all for hand-copied agent binaries); version skew becomes every support ticket's first question.

*How:* Add TaskKind 'self-update' handled by the agent (reusing SelfUpdate + systemd restart); server's post-update hook enqueues it for all online nodes and updates HA followers first (via their /api/update), leader last. Show per-node version drift in Machines.

**4. Stop persisting/replicating volatile metrics every 2 seconds** · impact: high · effort: M

*Why:* Heartbeat metrics dirty the store on every agent report (api.go:86-124), causing perpetual 2s full-state writes, SD-card wear, and — under HA — a full cluster snapshot through raft every 2s (cluster.go:123-127), which is the real scale ceiling of the HA design.

*How:* Split Node.Metrics/LastSeen into an in-memory (json:"-") side map; only mark dirty on durable changes (new node, status transition, hardware change). Alternatively debounce: persist metrics at most every 60s. Raft proposals then happen only on real state changes.

**5. Self-update hardening: checksums, keep-old rollback, version pinning** · impact: high · effort: M

*Why:* Executing an unverified root download (update.go:196) and overwriting the only good binary means one bad or tampered release strands users with no way back. Trust in one-click update is existential for a self-hosted platform.

*How:* Publish SHA256SUMS with releases; verify before the exec sanity check. Rename current binary to rackops.old before installing; add `rackops update --rollback` and `rackops update v0.3.2` (download by tag instead of latest). Surface rollback in the dashboard update banner error path.

**6. Agent install one-liner + systemd unit from the Add Machine modal** · impact: high · effort: M

*Why:* The current flow ('copy the binary yourself, run a foreground command that dies on logout') is the single worst step of onboarding and directly precedes the 'does this product work?' judgment. Competitors add nodes automatically.

*How:* Teach install.sh an agent mode (env RACKOPS_JOIN_URL/RACKOPS_TOKEN or flags) that downloads the binary and writes a rackops-agent systemd unit; have handleJoinCommand return `curl -fsSL <server>/install.sh | sudo RACKOPS_JOIN=... bash` (serve the script from the binary so no GitHub dependency). Update AddMachineModal copy.

**7. Make state persistence crash-safe: fsync + rotated last-known-good + corruption fallback** · impact: high · effort: S

*Why:* The 2s no-fsync full rewrite (store.go:155-178) plus fatal-on-parse-error load (store.go:120-123, main.go:136-139) means one power cut can permanently brick the install. This is the cheapest catastrophic-risk fix in the codebase.

*How:* In persistLoop: f.Sync() before rename, fsync the dir; keep state.json.1..3 rotated on a slower cadence (e.g. hourly + on shutdown). In NewStore: on unmarshal failure, log loudly, move the corrupt file aside, and load the newest valid rotation instead of dying.

**8. CLI parity with the MCP surface + --json** · impact: medium · effort: M

*Why:* The AI gets logs, restart, and remove (mcp.go) while the human CLI can only look and deploy (cli.go:42-121); scripting users expect `rackops logs app -f`, `rackops restart app`, and JSON output for pipelines.

*How:* Reuse the existing cliClient + findService helpers to add logs/start/stop/restart/redeploy/remove/status subcommands, a global --json flag, migrate to the flag package (fixes silent typo-swallowing), and document --template in usage (main.go:70).

**9. Cluster membership management: remove peers, warn on even quorums, show health** · impact: medium · effort: M

*Why:* Add-only membership (raftstore.go:159-164) means the first permanently dead server leaves the cluster one failure from write outage, unfixable via API.

*How:* Add Cluster.RemoveVoter + DELETE /api/cluster/peers/{id} (leader-only, admin session), last-contact/lag per peer in handleClusterStatus via raft.Stats(), a red badge for unreachable peers in Network.tsx, and a startup/UI warning for 2-node clusters.

**10. Prune deployment history** · impact: medium · effort: S

*Why:* Store.Deployments grows without bound (services.go:294, never deleted) inside a file that is re-marshaled every 2s and raft-replicated in full — a slow-motion performance death for active users.

*How:* On deployment finish (applyTaskResult, api.go:474+), keep the last N=20 per service and drop older ones (also on service delete). One Mutate block.

#### Missing features

**1. Server state backup/restore (one-click export + restore-on-install)** · impact: critical · effort: M

Today the entire platform's memory is one un-fsynced JSON file with no supported copy procedure. A GET /api/backup tarball (state.json + mesh keys + join token, secrets optionally passphrase-encrypted), a `rackops restore <file>` verb, and an install.sh prompt to restore turn total-loss disaster recovery from 'impossible' to 'ten minutes'. Cheap because state is already one directory.

**2. Scheduled volume + database backups with S3/local/SFTP targets and retention** · impact: critical · effort: XL

The #1 table-stakes feature of Dokploy and Coolify that RackOps lists on its own roadmap (README.md:59). Users are trusting irreplaceable data (photos in Immich, WordPress DBs) to rackops-volumes directories with zero protection. The TaskBackup stub (types.go:663) and per-service volume layout (runtime.go:440) mean the plumbing is half-imagined already: agent-side tar/dump of volume dirs, database-aware pg_dump/mysqldump for catalog templates, cron schedule in service config, restore flow in AppDetail.

**3. Dokploy/Coolify migration importer** · impact: high · effort: L

The exact users this product hunts already run a competitor. adoptExternal (api.go:409) already claims their containers; the missing 20% — importing env vars, domains, and compose definitions from the competitor's config/DB so apps become fully managed (redeployable) rather than merely adopted — converts 'trial next to Coolify' into 'replaced Coolify this weekend'.

**4. Node decommission + uninstall flow** · impact: medium · effort: M

Every fleet eventually retires a machine. There's no `rackops uninstall`, no dashboard 'remove machine' that cleans the remote agent, containers, and volumes, and no way to revoke a join token (it's static, loadOrCreateToken main.go:350-361). Portainer/Dokploy both handle node removal cleanly.

**5. Release channels + changelog-gated updates** · impact: medium · effort: M

Self-hosters running production workloads expect a stable channel while enthusiasts ride edge; today everyone gets latest immediately (update.go:46-64) with no way to hold a version. A channel field in settings + tag filtering in LatestRelease is most of it.

**6. `rackops doctor` diagnostics + support bundle** · impact: medium · effort: M

A self-hosted platform lives or dies on debuggability. A single command that checks docker/compose/ports/mesh/DNS/leader-status and emits a redacted support bundle would deflect most of the support tickets the product already has infrastructure to receive (tickets.go), and mirrors what Kamal/kubectl users expect from a CLI-first tool.

#### Product ideas

**1. Time Machine for your rack**

Zero-config continuous protection as a product pillar: the agent snapshots every managed volume and database (content-addressed, incremental, encrypted) to a local drive by default and any S3/SFTP/rsync target optionally, on a schedule you never had to configure. The payoff command is `rackops restore --from <target>` on a fresh machine: it rebuilds the entire rack — apps, env vars, domains, mesh, users — from the last snapshot. 'Your homelab can burn down and you're back in ten minutes' is a marketing line no competitor can say, and it composes naturally from the existing pieces: TaskBackup stub, per-service volume dirs (runtime.go:440), catalog knowledge of which template runs which database, and single-file server state.

**2. Buddy backup over the mesh**

Two RackOps installs — yours and a friend's, or home and office — pair with one code and become each other's encrypted offsite backup target over the WireGuard mesh that already exists (meshhub.go). No cloud account, no S3 bill, no config: each side donates a disk quota and stores the other's snapshots, unreadable without the owner's key. This is peak homelab culture (people already swap backup drives with friends), it exploits infrastructure RackOps uniquely already ships (CGNAT-piercing mesh), and it makes every install more valuable when a second one exists — a built-in growth loop.

**3. rackops takeover: one command to defect from your current PaaS**

`rackops takeover` pointed at a machine running Coolify, Dokploy, or hand-rolled docker-compose: it inventories every container (adoptExternal already does this), reads compose files, Traefik/labels, and env vars, then presents a migration plan in the dashboard — 'these 14 apps become managed services, these domains re-route through RackOps edge, data stays exactly where it is.' Accept, and the competitor is decommissioned in place with a full rollback snapshot taken first (which conveniently requires the backup engine). It weaponizes the single-binary identity: the migration tool IS the product, no separate installer, and the safety-net snapshot doubles as the user's first backup.

### Machines, agents, telemetry, placement, alerts

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| Agent join flow | basic | One-line join command with shared token + org routing (backend/internal/server/api.go:710-725, dashboard/src/components/AddMachineModal.tsx). Agent is the same binary, dials out (works behind CGNAT), persists a node ID, embedded local agent on the server machine (cmd/rackops/main.go:178-197). But the user must manually copy the binary to each machine — no install script, no systemd unit creation, no arch-appropriate download. |
| Telemetry collection (agent) | solid | Dependency-free /proc + /sys sampling every 2s (internal/telemetry/telemetry.go): CPU%, load 1/5/15, mem, swap, hottest package temp, uptime, process count. Per-drive live R/W MB/s, NVMe temp, fill-based health (storage_net.go); per-NIC rx/tx + link speed; NVIDIA GPUs full stats via nvidia-smi, AMD via sysfs (VRAM/util/temp/power), Intel detected name-only (gpu.go). Public IP via reflectors hourly (host.go:56-78). No per-process, no SMART, no history. |
| Rack visualization | basic | Rack.tsx renders nodes as rack units with LED, CPU/MEM/temp meters, app up-count, click-through — a distinctive signature element. U-numbers are just reverse index (Rack.tsx:16); no physical arrangement, grouping, or GPU/drive indicators on the slat. |
| Machine detail page | solid | Machines.tsx: KPI cards, CPU sparkline (client-side only, 40 samples, accumulates only while the page is open, lost on refresh — lines 52-66), GPU cards with VRAM/util/temp/power, drives with throughput + health badges, NICs, apps-on-machine list, offline guidance banner. Zero actions: no rename, remove, reboot, drain, prune, or terminal. |
| Placement scoring | basic | PlaceWorkload (server/placement.go:19-125): hard constraints (free mem, free VRAM, free storage), weighted soft score 35% CPU headroom / 30% mem / 20% thermal / 15% storage + NVMe bonus, org-scoped, human-readable reasons. Used for auto-place on create (services.go:218-237) and surfaced with scores in DeployFlow. PlacementRequest.CPUMilli is defined but never used; no arch, GPU-vendor, affinity, or reservation accounting. |
| Alert engine | basic | 4 hardcoded node rules — CPU>90, mem>90, temp>85, any drive>92% (placement.go:136-154) — with sustain-for-3-consecutive-reports debounce and auto-resolve on one clean report (placement.go:161-222). node_down via 5s watchdog with 15s deadline (main.go:169-176, placement.go:225-252). Broadcast raise AND resolve over SSE; UI converges via 8s poll fallback (data.tsx:96-101). No user-configurable thresholds, no ack/silence, no service-level rules (deploy_failed mentioned in types.go:304 but never implemented). |
| Notification delivery | basic | Per-org Discord/Slack/Telegram/generic-webhook fan-out with deploy and alert toggles (internal/notify/notify.go, server/settings.go:79-128). Fire-and-forget with 8s timeout, no retry, no email channel — and node_down (the most important alert) never reaches it (see limitations). |
| Auto-heal & reconcile | solid | Agent report is ground truth: vanished containers flip services to failed and queue redeploys with a heal budget (5 attempts, 2min spacing, reset when seen alive) (api.go:224-372); quick tunnels restarted, Traefik edge redeployed, external containers adopted with delete-suppression (api.go:374-471). Genuinely good self-healing story. |
| Offline handling | basic | 15s deadline → offline status + critical node_down alert + event + SSE; reporting again auto-resolves (placement.go:210-252). Tasks queue in persisted TaskQueue while a node is offline and drain on return (store.go:252-271) — no TTL, so stale actions replay. Services on an offline node keep their last status (still shown 'running'). Machines page shows a helpful offline banner (Machines.tsx:99-107). |
| Needs-attention / overview | solid | Home.tsx: one-glance status banner, needs-attention list (failing services + offline machines + open alerts) with one-click paths to logs/deployments, live event terminal, setup checklist, fleet KPIs. Org-scoped overview aggregation (api.go:586-641). Events capped at 500 (store.go:238-248). |
| GPU support | basic | Discovery is strong (NVIDIA full, AMD good, Intel name-only); placement checks free VRAM (placement.go:42-54); deploy passes --gpus all (agent/runtime.go:278) which is NVIDIA-runtime-only and grabs all GPUs; UI renders GPU cards nicely. No per-GPU assignment, no vendor matching, no AMD/Intel device passthrough. |
| ARM/architecture awareness | skeletal | Arch collected (runtime.GOARCH, host.go:17-20), stored on the node, and shown as a tag (Machines.tsx:92). Self-update downloads per-arch binaries (update.go:169). Nothing else: placement, image deploys, and migration ignore arch entirely. |
| Agent auto-update | skeletal | Server has one-click self-update + restart (server/update.go:110-211) and a `rackops update` CLI, but there is no mechanism to update worker agents from the control plane — no task kind for it, and AgentVer is reported (api.go:124) but never compared or surfaced in the UI. Fleet version skew is invisible and fixing it is one SSH per machine. |
| Node removal / decommission | skeletal | Does not exist. Route table has only GET /api/nodes, GET /api/nodes/{id}, GET /api/nodes/{id}/fs, and the join command (openapi.go:33-39). The ManageMachines permission promises join/rename/remove (types.go:461) but no rename or remove handler exists anywhere. |

#### Limitations & weaknesses

**1. A dead machine can never be removed from the fleet**

There is no DELETE or PATCH /api/nodes endpoint (openapi.go:33-39) and no UI affordance. A decommissioned Pi stays in the rack forever with a permanent unresolvable node_down critical alert, drags down 'nodes online' KPIs, and its stale hardware inflates pooled memory/storage totals (api.go:610-620). types.go:461 documents ManageMachines as 'join/rename/remove nodes' — two-thirds of that is unimplemented. This is the first wall every real user hits.

**2. Long builds make machines falsely go 'offline' with critical alerts**

The agent executes tasks synchronously inside its report loop (agent/agent.go:189-196), so a git build or large image pull (minutes) stops telemetry pushes entirely. The watchdog marks any node silent for >15s offline and raises a critical node_down alert (main.go:171, placement.go:225-252). Net effect: every non-trivial deploy triggers 'machine stopped reporting' panic in the UI, then auto-resolves — training users to ignore the most serious alert the system has.

**3. node_down alerts never reach Slack/Discord/Telegram**

NotifyAlert is only called for alerts raised by EvaluateAlerts during ingest (api.go:186-191). node_down alerts are created in MarkOfflineNodes (placement.go:236-246) and broadcast to the dashboard only (main.go:171-174). A user who configured alert notifications will get pinged about 91% CPU but not about their server dying — the single most important notification silently doesn't exist.

**4. Shared static join token allows full fleet impersonation**

Ingest auth is one install-wide token (api.go:72-75); NodeID is client-chosen (agent.go:81-90). Anyone holding the token — printed in server logs (main.go:250) and shown to any member with ManageMachines — can forge reports as any node and, worse, drain that node's queued tasks via DrainTasks (api.go:199), which contain deploy payloads with plaintext env secrets and GitToken. The token also joins any org (api.go:100-111), piercing the multi-tenancy boundary. No per-node credentials, no rotation endpoint.

**5. No metrics history anywhere**

The server keeps only the latest Metrics snapshot per node (types.go:97-110); the only 'history' is a client-side Map feeding a 40-sample sparkline that accumulates solely while the machine detail page is open and dies on refresh (Machines.tsx:52-66). Users of Portainer/Netdata/Coolify expect at least a 1h/24h CPU-mem-disk chart; 'was the box swapping at 3am?' is unanswerable.

**6. Placement ignores CPU request, reservations, arch, and GPU vendor**

PlacementRequest.CPUMilli is dead (never referenced in placement.go). Memory/storage checks use instantaneous free values (placement.go:37-41), so two concurrent deploys both 'fit' the same RAM, and a node's existing MemLimitMB commitments are ignored. No arch constraint: an amd64-only image can be auto-placed on an ARM node and fail at runtime with exec-format-error. NeedsGPU is satisfied by an AMD GPU's free VRAM (placement.go:42-54), then the deploy runs --gpus all (runtime.go:278) which only works with the NVIDIA container runtime — guaranteed failure.

**7. Deploying to an offline node silently hangs forever**

handleCreateService validates an explicit node only for org membership, not liveness (services.go:124-134); queueDeploy enqueues into the persisted TaskQueue regardless. The service sits in 'building' with no feedback until the node returns — and queued stale actions (multiple restarts, old deploys) all replay at once on reconnect since tasks have no TTL (store.go:252-271).

**8. Alerts accumulate in state forever**

Resolved alerts are never pruned — Store.Alerts only grows (store.go:30, placement.go:186-207) and each raise creates a new record. Every one is serialized into state.json (pretty-printed!) on the 2s persist loop (store.go:154-178) and replicated under HA. A months-old homelab with flappy wifi will have thousands of dead alert objects rewritten to disk every few seconds.

**9. Package-temp detection can report the wrong sensor**

hottestTemp (telemetry.go:228-258) intends to prefer CPU sensors, but the fallback branch ('else if best == 0') only fires when nothing has been recorded yet — if a non-CPU chip (NVMe at 60C, wifi module) is enumerated before coretemp, its reading sticks and the actual 45C CPU sensor can never override it. Inflated TempC feeds both the thermal alert (>85C critical) and the 20%-weighted placement thermal score.

**10. Offline machine appears twice in Needs attention**

Home renders a row per offline node AND the node_down alert for the same machine (Home.tsx:72-90), so one dead box produces two nearly identical cards. Also, services on that node still show green 'running' since reconcile only runs on agent reports — the dashboard says 'All systems' lies by omission about every app on a dead machine.

**11. Alert thresholds, sustain window, and offline deadline are all hardcoded**

nodeRules thresholds (90/90/85/92) are compile-time constants (placement.go:136-154); sustainReports=3 at the 2s report interval is ~6s, not the '15-20s' the comment claims (placement.go:156-163); the 15s offline deadline (main.go:171) is aggressive for wifi/powersave homelab nodes and none of it is configurable. No ack/snooze exists — GET /api/alerts is the entire alert API surface (openapi.go:38).

**12. Drive 'health' is just fill percentage, not health**

Health is derived purely from used/size ratio (storage_net.go:98-107) yet the UI presents it as a health badge next to temperature. A drive at 40% full that's throwing SMART reallocated-sector errors shows 'ok'. No smartctl integration despite it being the standard homelab expectation.

**13. Misc accuracy and scoping nits**

(a) The containers KPI counts exited and external containers because ingest uses len(rep.Containers) which includes `ps -a` output (api.go:125, runtime.go:62). (b) Overview pools unmounted drives into total storage (api.go:617-620). (c) node_down and service_update SSE events broadcast install-wide, leaking node/service names across orgs (main.go:173, api.go:338 use Broadcast, not BroadcastOrg). (d) The join command hardcodes http:// from r.Host (api.go:722) — wrong behind a TLS proxy. (e) The offline banner tells users to check `systemctl status rackops-agent` (Machines.tsx:104) but nothing in the product ever creates that unit.

#### Improvements

**1. Run agent tasks off the report loop** · impact: critical · effort: M

*Why:* Every git build/large pull currently blanks telemetry and fires a false critical node_down (agent.go:189-196 + 15s deadline in main.go:171). This poisons trust in the alert system and makes machines look flaky during the product's core action — deploying.

*How:* Execute tasks in a worker goroutine (one queue per node is fine; serialize deploys per service), keep pushing 2s reports concurrently, and attach in-flight task IDs to reports so the server doesn't re-queue. Alternatively/mildly: also bump LastSeen on /api/agent/logstream chunks as a stopgap.

**2. Add node remove + rename endpoints and UI** · impact: critical · effort: S

*Why:* Decommissioning is impossible today; dead nodes pollute the rack, KPIs, and alerts forever (openapi.go:33-39). The permission model already promises it (types.go:461).

*How:* DELETE /api/nodes/{id} (require offline or ?force, cascade: resolve its alerts, clear TaskQueue, block or reassign its services, release mesh IP) and PATCH /api/nodes/{id} for rename/org-move. Gate on ManageMachines; add a danger-zone card on MachineDetail plus a 'Remove machine' action on the offline banner.

**3. Server-side metrics history + real charts** · impact: high · effort: L

*Why:* No time dimension exists (Machines.tsx:52-66 is the entire story). Every competitor and every 'is my box healthy' question needs at least a day of history.

*How:* In-memory ring buffer per node (e.g. 30s resolution x 24h ≈ 2880 points of cpu/mem/temp/disk/net — a few hundred KB), persisted separately from state.json, exposed as GET /api/nodes/{id}/history?window=; replace the sparkline with proper area charts and add per-drive/NIC series toggles.

**4. Make placement honest: CPU request, reservations, arch, GPU vendor** · impact: high · effort: M

*Why:* Auto-place currently can pick a node that will fail (wrong arch, AMD GPU for --gpus all) or overcommit (instantaneous free mem, CPUMilli ignored) — placement.go:37-54.

*How:* Hard-constrain on node.Arch vs a service arch hint (default: control-plane arch for images, native for git builds), require GPU vendor==nvidia when the deploy path uses --gpus, subtract sum of MemLimitMB of services already assigned to the node from free memory, and use CPUMilli against cores*1000*(1-cpuPct). Add 'insufficient CPU' / 'wrong architecture' reasons to the score card users already see.

**5. Configurable alert rules + ack/snooze** · impact: high · effort: M

*Why:* Hardcoded 90/90/85/92 thresholds and a 15s offline deadline (placement.go:136-154, main.go:171) generate noise on homelab hardware (a NAS at 93% disk is normal); there is no way to silence a known condition.

*How:* Per-org AlertConfig in Settings (threshold + sustain per rule, offline grace), optional per-node overrides; POST /api/alerts/{id}/ack and /snooze?until=; suppress acked alerts from Needs attention and notifications. Prune resolved alerts to last 200 while in there.

**6. Per-node join secrets and token rotation** · impact: high · effort: M

*Why:* One static token = fleet-wide impersonation and cross-org secret exfiltration through DrainTasks (api.go:72-79,199).

*How:* Keep the shared token for first contact only; on node_joined mint a per-node secret returned in AgentResponse and required (paired with NodeID) for subsequent ingest/logstream. Add POST /api/join-token/rotate. Stop honoring client OrgID unless the join token was org-scoped.

**7. Route node_down (and resolutions) through notification channels** · impact: high · effort: S

*Why:* The machine-died alert is the one users configure Slack/Discord for, and it's the only alert class that never notifies (placement.go:236-246 vs api.go:186-191).

*How:* In the watchdog loop after MarkOfflineNodes, call NotifyAlert for each new alert; also notify on resolve ('back online after 4m12s'). Debounce with a per-node cooldown so flappy links don't spam.

**8. Real drive health via smartctl when available** · impact: medium · effort: M

*Why:* The 'health' badge is fill-% only (storage_net.go:98-107); presenting it as health is actively misleading for failing disks.

*How:* If smartctl exists (and agent has permission), sample SMART overall-health + key attributes (reallocated, pending, wear level, NVMe percentage_used) on the 60s hardware refresh; feed a real disk_failing critical alert rule.

**9. Fix hottestTemp CPU-sensor preference** · impact: medium · effort: S

*Why:* Enumeration-order dependent bug can report an NVMe/wifi sensor as package temp (telemetry.go:238-255), corrupting thermal alerts and 20% of the placement score.

*How:* Two-pass: collect all readings, take max over CPU-named chips if any exist, else max overall. Consider reporting cpuTempC and hottestOtherC separately.

**10. Guard deploys against offline nodes and expire stale tasks** · impact: medium · effort: S

*Why:* Explicit deploys to an offline node hang in 'building' forever with zero feedback, and queued actions replay en masse on reconnect (services.go:124-134, store.go:252-271).

*How:* Reject (or require confirm) create/redeploy when the target node is offline; stamp tasks with a TTL and drop expired ones at drain time with an event; surface 'waiting for machine X to come back' on the service card instead of 'building'.

**11. Surface agent version and fleet skew** · impact: medium · effort: S

*Why:* AgentVer is collected (api.go:124) but shown nowhere; after a server update the operator can't tell which machines run old agents.

*How:* Show agent version on the machine detail header and a subtle 'agent outdated' tag on rack slats when node.AgentVer != ServerVersion; count skew in overview.

**12. De-duplicate Needs attention and mark services on dead nodes** · impact: medium · effort: S

*Why:* One offline machine renders two cards (Home.tsx:72-90) and its apps stay green, undermining the 'is everything okay?' promise.

*How:* Skip node_down alerts in the openAlerts render when the node also appears in nodesOffline; in the UI (or ingest watchdog) derive an 'unknown' display status for services whose node is offline.

#### Missing features

**1. curl-able install one-liner + systemd unit for agents (and server)** · impact: critical · effort: M

Onboarding currently requires manually copying the binary and keeping the process alive yourself; the agent dies on reboot. The UI even references a rackops-agent systemd unit that nothing creates (Machines.tsx:104, AddMachineModal.tsx). Dokploy/Coolify are literally 'curl | sh' — this is the top-of-funnel moment and RackOps fumbles it.

**2. Web terminal onto machines and containers** · impact: high · effort: L

Portainer and Dokploy both ship a browser console; for a product whose pitch is 'never SSH again', debugging still requires SSH the moment anything is weird. The agent task channel + SSE/logstream plumbing already exists to carry it.

**3. Fleet-wide agent auto-update from the control plane** · impact: high · effort: M

SelfUpdate already exists (update.go:163-202) and agents are the same binary — but updating a 6-machine fleet is 6 SSH sessions. Competitors update workers centrally; a single-binary product should make this its showcase: one click updates server then rolls agents.

**4. Node maintenance mode / drain (cordon)** · impact: high · effort: M

Kubernetes cordon/drain, Docker Swarm availability=drain, even Proxmox migration — there is no way to say 'stop placing here' before rebooting a box for kernel updates. Placement will happily target a machine mid-maintenance.

**5. Service-level alert rules (crash loop, OOM, restart storm, deploy failed)** · impact: high · effort: M

The alert engine only watches node vitals; a service OOM-crash-looping within its heal budget produces events but no alert and no notification. types.go:304 even names deploy_failed as an intended kind. Users of any competitor expect 'my app is down' to page them.

**6. Automatic workload failover off dead nodes** · impact: high · effort: XL

The product self-heals containers on a live node but does nothing when a node dies — auto-placed stateless services could be rescheduled to a healthy machine after a grace period. This is the headline capability that separates 'dashboard' from 'platform', and placement + deploy plumbing already exist.

**7. Node labels/tags with placement affinity** · impact: medium · effort: M

'ssd-only', 'living-room', 'gpu-rig', 'do-not-schedule-media' — every scheduler from Nomad to Swarm has constraints; RackOps has only pin-to-node or full auto with no middle ground.

**8. Per-process/per-container top view on the machine page** · impact: medium · effort: M

When CPU is at 95% the page shows the number but not the culprit; ProcessCnt is collected (telemetry.go:299-313) but nothing more. A 'top 10 processes / containers by cpu+mem' table answers the immediate question competitors' host views answer.

**9. Node reboot / shutdown / docker prune actions** · impact: medium · effort: S

Basic fleet hygiene (reclaiming disk from dangling images is the #1 disk_full cause) requires SSH today. Coolify ships cleanup buttons; an agent task for `docker system prune` and `systemctl reboot` is cheap.

**10. Email notification channel** · impact: medium · effort: S

Discord/Slack/Telegram/webhook exist but not email — the lowest-common-denominator channel every alerting product has, and the CRM mailer patterns already exist in the codebase (internal-apps/crm mailer).

**11. Alert history / audit page** · impact: low · effort: S

?all=true exists on the API (api.go:702) but no UI shows resolved alerts, so 'what happened last night' is unanswerable; there is no dedicated alerts page at all (App.tsx routes 179-190).

#### Product ideas

**1. Fleet flight recorder**

Embed a zero-config time-series store in the single binary (compressed per-node ring files, ~24h at 10s + 30d at 5m, a few MB per machine) and add a timeline scrubber that overlays metrics with deploys, heals, alerts, and node events. When something breaks at 3am, you drag a cursor and watch the incident replay — memory climbing, OOM, auto-heal firing — like a dashcam for your rack. No Prometheus, no Grafana, no config, on-brand for 'single binary, your machines'.

**2. Evacuate & self-heal at the machine level**

One-click 'Evacuate' drains a node: placement re-scores every service, volumes stream to the target over the already-built WireGuard mesh, DNS/relay routing flips automatically. Pair it with an opt-in failover policy ('if a machine is down 5 minutes, move its auto-placed apps') and RackOps becomes the only homelab PaaS where a dead Raspberry Pi means nothing — Kubernetes-grade resilience with literally zero YAML.

**3. Physical rack twin with power awareness**

Let users arrange machines into their real rack/shelf layout (the Rack view becomes a true digital twin), then add watts: CPU RAPL + GPU PowerW (already collected) + smart-plug integrations give per-machine and per-app energy cost in currency. Close the loop with Wake-on-LAN: idle machines suspend to save power and the control plane wakes them when placement needs capacity — a 'green homelab autoscaler' no competitor has and exactly what the audience obsesses over.

### Application deployment & lifecycle

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| Four deploy sources via a 3-step wizard (template / plain image / git repo / pasted compose YAML) | solid | DeployFlow.tsx implements source pick → configure (name, ports, node, env) → live launch progress with SSE build-log streaming and a 'Reach it / Make it public' payoff. Backend handleCreateService (services.go:90-277) validates org/project/node, expands templates, auto-places, and queues the deploy. Genuinely polished happy path. |
| Template catalog | solid | 49 templates across 7 categories (infra/homelab/AI/web/tools/games/databases) in catalog.go, with typed env definitions (label/default/required/secret/auto-generate), volume declarations, memory hints for placement, GPU flags, and 5 real multi-container compose stacks (Immich, WordPress, Plausible, Umami, Listmonk) with generated shared secrets via ${VAR} .env substitution. Comparable per-template quality to Coolify, far smaller breadth (Coolify ships 300+). |
| Git build pipeline | basic | build.go: shallow clone (token auth for https, redacted from logs), priority Dockerfile > nixpacks (only if already installed on the node — not shipped) > auto-generated Dockerfile from stack detection (Node/Go/Python/pyproject/static only). No build args, no context subdirectory/monorepo support, no custom Dockerfile path, no commit SHA capture (Deployment.CommitSHA exists but is never set), no registry push — image lives only on the build node, tagged rackops/<name>:<serviceID> so every build overwrites the previous image. |
| Env vars & secrets | basic | Per-service KV list with a secret flag (UI masking, password inputs), template defaults + genSecret() 64-hex auto-generation, compose stacks get a .env file (0600). But: plaintext at rest in the replicated store JSON, passed as -e KEY=VALUE (visible in docker inspect AND echoed into the stored deploy log at runtime.go:285), no bulk .env paste, and Project.SharedEnv (types.go:123) is defined but never inherited anywhere. |
| Volumes & mounts | solid | Template volumes auto-become managed named volumes; user mounts support bind (with a real host folder browser round-tripped through the agent, service_manage.go:186-221) and managed volumes optionally placed on a specific physical drive (mountSpec, runtime.go:419-450), read-only flag, absolute-path validation on PATCH. Data survives redeploys and delete (documented in the UI). |
| Resource limits, GPU, restart policies | basic | --memory, --cpus, --gpus all, and the four docker restart policies, editable in Settings and applied on redeploy (runtime.go:271-279). No swap/pids/ulimits/io limits, GPU is all-or-nothing (no per-device selection), no per-service resource usage history. |
| Auto-heal / reconcile loop | basic | reconcileNode (api.go:258+) treats the agent's 2s container report as ground truth: vanished containers flip services to failed and requeue deploys with a budget (5 attempts, 2-min cooldown, reset on recovery); immediate-crash detection captures the first 40 log lines and container state right in the deploy log (runtime.go:311-325). But it is existence-based only — no user-definable health checks — and compose stacks and adopted services are entirely exempt. |
| Deployment history with live logs | basic | Versioned Deployment records per service (trigger: manual/config-change/auto-heal), full build output streamed live over SSE and stored (trimmed to 20KB), expandable rows in the Deployments tab. No rollback action, no config/image snapshot per version, no commit metadata, records are never pruned. |
| Lifecycle actions & config editing | solid | start/stop/restart/redeploy/delete with org RBAC checks, pointer-field PATCH distinguishing unset from zero, 'Save only' vs 'Save & redeploy', dirty-state banner, action locking through the whole deploy window driven by live status. Adopted (externally created) containers are correctly restricted to lifecycle-only with clear UI messaging. |
| Port model | solid | Clean container-port vs host-port split with server-side free-port suggestion (suggestHostPort), agent-side actual-bindability check with bump-and-report-back (firstFreePort + res.HostPort feedback into svc.Port, api.go:522), extra-ports list with per-port conflict fallback. Better than Dokploy's default behavior here. |
| App migration between machines | basic | Changing the node in Settings + Save & redeploy enqueues remove on the old node and deploy on the new (service_manage.go:129-141), relay port re-derived. Volume data does NOT move — admitted in the UI ('managed volumes don't move automatically', AppDetail.tsx:779) — which makes migration useless for stateful apps, i.e. most of the catalog. |
| External container adoption | basic | Containers created outside RackOps are surfaced and adopted as manageable services (adoptExternal api.go:409-470): lifecycle + logs work against the real container name, rebuild correctly blocked. A differentiator vs Dokploy/Coolify (closer to Portainer). Undermined by the compose ghost-adoption bug below. |
| Projects & environments | skeletal | Project/Environment/EnvID exist in the data model and API filters, but DeployFlow hardcodes projectId: projects[0] and the 'production' env (DeployFlow.tsx:108-109); the Apps page is a flat list with no project grouping; SharedEnv is dead code. Effectively a single implicit project today. |
| Backups / build task kinds | skeletal | TaskBuild and TaskBackup constants are declared in types.go:663-664 and referenced nowhere else — zero backup capability exists. |

#### Limitations & weaknesses

**1. Compose template containers get re-adopted as ghost duplicate services**

Catalog compose stacks hardcode container_name (immich_server, wordpress_app, umami_db, plausible_app... catalog.go:151-345). deployCompose applies no rackops labels (compose.go:44-47), and listExternal only skips containers with a rackops.service/rackops.tunnel label or a rackops- name prefix (runtime.go:79-81). So every stack member is reported External and adoptExternal (api.go:419-461) creates a phantom adopted service per container: deploy Immich once and the Apps list gains 4 extra 'adopted' entries whose lifecycle buttons fight the parent stack. Users hit this on their first template stack deploy.

**2. Compose stacks have zero liveness tracking after deploy**

The container→service status sync matches only the rackops.service label (api.go:150-163), and reconcileNode explicitly skips services with ComposeYML (api.go:286-289). A crashed/OOM-killed stack keeps showing 'running' forever, gets no CPU/mem stats on the parent service, and is never auto-healed — the exact workloads (Immich, WordPress, Plausible) most people run.

**3. service_update SSE broadcasts are install-wide, leaking config across organizations**

Every service change is sent via Hub.Broadcast (services.go:275, api.go:338/537, service_manage.go:145), which is BroadcastOrg("") — delivered to every connected client of every org (sse.go:26-42). The payload is the full Service including Env values (secrets in cleartext) and GitToken. In a multi-tenant install this is a cross-org secret leak; GET /api/services also returns GitToken unstripped (api.go:822-844 serializes raw pointers; only handleGetService strips it at service_manage.go:156 despite the 'never sent to UI' comment on types.go:201).

**4. Secrets echoed into stored deploy logs**

runtime.go:285 logs the full docker run command line — including every -e KEY=VALUE pair — into the log sink that becomes the persistent Deployment.Log and the live SSE deploy stream. Generated DB passwords and user secrets are readable by anyone who can open the Deployments tab, forever.

**5. No rollback, and every build destroys the previous image**

Deployment.Trigger documents 'rollback' (types.go:257) but no handler exists; CommitSHA is never populated. Git builds always tag rackops/<name>:<serviceID> (build.go:42) so each deploy overwrites the only tag — even a hand-rolled rollback is impossible without a rebuild of the old commit, which the system doesn't record. Dokploy and Coolify both ship one-click rollback.

**6. A lost deploy task bricks the app page in 'building' forever**

If the agent restarts mid-build (or the build hangs), the TaskResult never arrives, nothing times out — no watchdog exists server-side — and the service stays StatusBuilding. The UI locks Start/Stop/Restart/Redeploy AND Save for the entire 'working' window (AppDetail.tsx:65-66, busy gating on every button), so the user cannot even retry from the dashboard. reconcileNode's switch only handles Running/Failed (api.go:304-311).

**7. The agent executes tasks synchronously inside its report loop**

agent.go:189-196 runs each task inline between telemetry pushes. A multi-GB image pull or long git build blocks all reporting and all other tasks on that node for minutes: metrics freeze, the node can be marked offline (placement.go:229), and you cannot stop/restart any other app on that machine until the build finishes.

**8. Redeploys take the app down, and a failed run leaves nothing running**

Deploy does `docker rm -f <name>` before `docker run` (runtime.go:211). Build/pull happen first (good), but the stop→start gap is downtime on every config change, and if the new `run` fails (bad mount, port, GPU flag) the old container is already gone — auto-heal then loops on the same broken config. No health gating, no keep-old-until-new-is-ready.

**9. User-chosen host port is a lie for compose templates**

Catalog compose files hardcode published ports ("8081:80" WordPress catalog.go:218, "2283:2283" Immich, "8000:8000" Plausible), but the deploy form still suggests/accepts a 'Web port' whose value is stored as svc.Port and used for routing (services.go:246 skips port logic for compose). If the hardcoded port is busy the stack fails to start; if the user edits the port, routing points somewhere nothing listens. Two instances of the same template on one node can never coexist (fixed ports + fixed container_name), and the compose project name omits the service ID (compose.go:34: rackops-+sanitize(name)) so two same-named stacks clobber each other with --remove-orphans.

**10. No deploy-on-push in any form**

No webhook endpoint exists (openapi.go route table has none), no git polling, no provider integration — the 'webhook' trigger in types.go:257 is aspirational. Every git deploy is a manual Redeploy click. This is the single largest workflow gap vs Dokploy/Coolify, whose core loop is push-to-deploy.

**11. Wrong-branch silent fallback in git clone**

build.go:34-39: if cloning the requested branch fails (typo, deleted branch), it silently retries the default branch and deploys that — shipping code the user did not ask for instead of failing loudly.

**12. Deployment records grow without bound in replicated state**

Store.Deployments (store.go:27) is only ever appended to (services.go:294); nothing prunes old records. Each carries up to 20KB of log and the whole store replicates as full raft snapshots — a busy install with auto-heal retries accumulates thousands of records that slow every replication and persist forever.

**13. Stack auto-detection is too naive to build most real repos**

detectStack (build.go:87-137) assumes npm start + port 3000, `python main.py` + port 8000, static via index.html; `npm install --omit=dev` breaks any TypeScript build that needs devDependencies, and `npm run build || true` swallows build failures into broken runtime images. Rust/Java/PHP/Ruby/Deno/Bun repos are unsupported. Nixpacks would fix this but is only used if the user separately installed it (build.go:51) — undercutting the zero-config promise. Command override parsing also uses strings.Fields (agent runtime.go:282) so quoted arguments break.

#### Improvements

**1. Stop the secret leaks: org-scope service SSE events, strip GitToken from list responses, redact -e values from deploy logs** · impact: critical · effort: S

*Why:* Three concrete leaks today: cross-org SSE broadcast of full service objects with env secrets and git tokens (services.go:275 + sse.go:26-42), GitToken in GET /api/services (api.go:822), and secrets in stored deployment logs (runtime.go:285). Breaks the multi-tenant/orgs story completely.

*How:* Resolve the service's org (serviceInOrg already exists) and use BroadcastOrg for every service_update/service_removed; add a Sanitized() for Service used by list + SSE paths; in Runtime.Deploy log the run command with env values replaced by KEY=*** (secrets are already flagged in the payload).

**2. Keep the old container until the new one is healthy** · impact: high · effort: L

*Why:* Every redeploy is downtime, and a failed redeploy is a total outage because `rm -f` runs before `run` (runtime.go:211). Coolify does rolling container replacement for exactly this reason.

*How:* Start the new container as rackops-<name>-<id>-next on the bumped free port, wait for the existing 2s crash check (plus optional health check below), then swap: retarget ingress/relay to the new host port, rename/remove the old container. Falls back to current behavior for host-network apps.

**3. Move volume data during node migration** · impact: high · effort: L

*Why:* Migration is already a headline feature ('changing = migrate on Save & redeploy') but silently leaves all data behind (AppDetail.tsx:779) — for the stateful apps that dominate the catalog it's a foot-gun that looks like data loss.

*How:* Before the deploy task on the new node, run a copy task: old-node agent tars each managed volume/drive-placed dir and streams it to the new node over the existing WireGuard mesh (agents already relay TCP); block the new deploy until copy completes, show progress in the deploy log; offer 'move data' vs 'start fresh' choice in the UI.

**4. Label compose stack containers and wire them into status sync / reconcile** · impact: high · effort: M

*Why:* Fixes both the ghost-adoption bug (duplicate phantom services per stack member) and the frozen-status problem (crashed stacks show running forever) — the two worst surprises for template-stack users.

*How:* In deployCompose, inject labels via a compose override file (`docker compose -f compose.yml -f labels.yml`) adding `labels: [rackops.service=<id>, rackops.compose=member]` to every service, or filter by the com.docker.compose.project=rackops-* label in listExternal (runtime.go:79) as a minimal fix; teach the status sync to aggregate member states into the parent service (any exited member => degraded/failed) and let reconcileNode requeue `compose up -d` on absence.

**5. Per-version image tags + one-click rollback from the Deployments tab** · impact: high · effort: M

*Why:* Rollback is table stakes in Dokploy/Coolify; RackOps already has the version counter, history UI, and deploy plumbing — it just overwrites the only image tag every build (build.go:42).

*How:* Tag builds rackops/<name>:v<version> (and pulls by digest for image deploys), store the resolved image + commit SHA on the Deployment record (populate the existing CommitSHA field from `git rev-parse HEAD` after clone), add POST /api/services/{id}/rollback/{deploymentId} that redeploys with the recorded image (trigger: 'rollback' — the enum value already documented), keep last N images and prune older ones on the node.

**6. Run agent tasks off the report loop** · impact: high · effort: M

*Why:* A 5-minute image pull today freezes telemetry and blocks stop/restart of every other app on that node (agent.go:189-196), and can flip the node to offline — users see the whole machine 'hang' whenever anything deploys.

*How:* Execute deploy/remove tasks in per-service goroutines with results queued into the existing a.results slice (mutex-guard it); keep quick lifecycle ops inline; serialize per service ID so two deploys of one app can't race.

**7. Parameterize compose template ports and include the service ID in the project name** · impact: high · effort: M

*Why:* The 'Web port' field is currently cosmetic for stacks, port conflicts kill deploys, and two same-named/same-template stacks can't coexist on a node (compose.go:34, catalog.go hardcoded ports + container_name).

*How:* Change catalog compose files to `ports: ["${RACKOPS_PORT}:80"]` and drop hardcoded container_name (compose's project-prefixed names already carry the rackops- prefix, which also fixes ghost adoption for free); write RACKOPS_PORT into the stack's .env from p.Port; make project name rackops-<name>-<serviceID>.

**8. User-definable health checks that gate status and deploys** · impact: high · effort: M

*Why:* 'Running' currently means 'docker says running', not 'app answers' — a crash-looping or deadlocked app looks green. Every competitor (and half the catalog's own compose files) has health checks.

*How:* Add HealthCheck{type: http|tcp|cmd, path, port, interval, retries} to Service + template defaults; agent passes --health-cmd or probes the published port and reports health in ContainerStatus; server maps unhealthy → degraded status, feeds auto-heal, and gates the new-container swap during deploys.

**9. Deploy watchdog: time out stuck 'building' services and never lock the user out** · impact: high · effort: S

*Why:* An agent restart mid-build leaves a service permanently 'building' with every button disabled (AppDetail.tsx:65-66) — the user's only escape is the raw API. Silent, infuriating, and easy to hit on flaky homelab hardware.

*How:* Record deploy start on the Deployment; a server ticker (the offline-marking loop in placement.go already runs) fails deployments older than a configurable window (e.g. 20 min) with a clear message, resets svc.Status to failed, and lets auto-heal/redeploy proceed; additionally allow Redeploy while building (cancel-and-replace: agent kills the in-flight build when it sees a newer deploy task for the same service).

**10. Bulk .env paste + project-level shared env inheritance** · impact: medium · effort: S

*Why:* Entering 15 variables row-by-row in the deploy form is painful; Project.SharedEnv exists in the model (types.go:123) but is dead code, so there is no way to share DATABASE_URL across an app and its workers.

*How:* Add a 'paste .env' toggle on the env editors (parse KEY=VALUE lines, mark obvious secrets); merge project.SharedEnv into the DeployPayload env in queueDeploy (service-level keys win); UI on the project page to edit shared vars.

**11. Fail loudly on missing git branch; record and show the built commit** · impact: medium · effort: S

*Why:* Silently building the default branch when the requested branch doesn't exist (build.go:34-39) ships the wrong code with no signal; deploy history has no commit identity at all.

*How:* Remove the fallback clone (or gate it behind branch==''), surface the clone error; after clone, `git rev-parse --short HEAD` into the result payload → Deployment.CommitSHA, render it in the Deployments tab rows.

**12. Cap deployment history per service** · impact: medium · effort: S

*Why:* Unbounded Deployments map with 20KB logs each is replicated in full raft snapshots on every mutation (store.go:27, services.go:294) — installs with auto-heal churn degrade over months.

*How:* In queueDeploy, after insert, delete records beyond the newest 30 per service (keep any referenced by a future rollback feature); one-time prune on store load.

#### Missing features

**1. Deploy on git push (webhooks + polling fallback)** · impact: critical · effort: M

The core loop of every competitor (Dokploy, Coolify, even Kamal via CI) — without it RackOps git deploys are a manual chore. A GitHub/GitLab/Gitea webhook endpoint (per-service secret URL) covers publicly reachable servers; a server-side poller (compare remote HEAD via `git ls-remote` every N minutes) covers the CGNAT/homelab installs the product explicitly targets. The Deployment.Trigger enum already reserves 'webhook'.

**2. Database backups (scheduled dumps + restore)** · impact: high · effort: L

RackOps has a 'Managed databases' catalog category and a KindDatabase, plus an unused TaskBackup constant (types.go:664) — but no backup at all. Dokploy ships scheduled Postgres/MySQL/Mongo dumps to S3 as a headline feature; for homelab users this is the difference between a toy and a system of record. Dump via `docker exec pg_dump` on the agent, store locally or to any S3 endpoint (MinIO/SeaweedFS from their own catalog), with retention and one-click restore.

**3. Container shell (exec) from the dashboard** · impact: high · effort: M

Portainer, Dokploy, and Coolify all offer a web terminal into a running container; debugging a failing app in RackOps today means SSHing to the node yourself, which defeats the product's premise. The agent task channel + SSE log streaming infrastructure is 90% of the plumbing needed for an interactive `docker exec -it` bridge.

**4. Pre/post-deploy hooks (release commands)** · impact: high · effort: M

No way to run migrations (`rails db:migrate`, `prisma migrate deploy`) on deploy — every framework app needs this and both Coolify (pre/post scripts) and Kamal (hooks) have it. One command field executed via `docker run --rm <image> <cmd>` between build and swap, with output in the deploy log and failure aborting the release.

**5. Scheduled/cron jobs** · impact: medium · effort: M

Coolify's 'Scheduled Tasks' run commands in an app's container on cron; homelab users want nightly jobs (cleanups, imports, certbot-ish scripts) without deploying a second always-on container. Fits the agent task model trivially: server-side cron ticker enqueues exec tasks.

**6. Multiple domains and per-port routes per app** · impact: medium · effort: M

Service.Domain is a single string (types.go:230); apps with an API + admin UI on different ports, or www + apex domains, can't be modeled. Dokploy/Coolify support N domains per app with per-domain target port.

**7. Update notifications for template/image apps** · impact: medium · effort: M

Homelab users run Watchtower for this: surface 'image has a newer tag/digest' on the Apps list with a one-click pull-and-redeploy. RackOps controls deploys already, so it can do this safely (and per its own rollback history once that exists).

**8. Private registry credentials** · impact: medium · effort: S

Any team with images in GHCR-private/ECR/self-hosted registry cannot deploy them — `docker pull` relies on whatever login happens to exist on the node (runtime.go:202). Org-scoped registry credentials passed to the agent for `docker login` before pull; the catalog even ships a Docker Registry template it can't authenticate against.

**9. Preview environments (deploy a branch/PR to a temporary URL)** · impact: medium · effort: XL

Coolify and Dokploy both do PR preview deploys. RackOps already has environments in the model, instant nip.io URLs, and auto-placement — cloning a service for a branch with an auto-generated subdomain and TTL cleanup is a natural composition. Depends on webhook support landing first.

**10. Replicas / multi-instance scaling** · impact: low · effort: XL

One container per service, period. Even a modest 'run N replicas on this node behind the edge' (Traefik already load-balances) or 'one per node' mode would differentiate for game servers and stateless APIs; full multi-node scheduling is what pushes people to k8s and could stay out of scope.

#### Product ideas

**1. Built-in git remote: `git push rackops production` deploys — no webhooks, no CI, no GitHub required**

The product's whole identity is 'agents dial out, works behind CGNAT' — but webhook-based deploy-on-push assumes the internet can reach your server. Flip it: embed a smart-HTTP git receiver in the single binary (go-git can serve receive-pack). Each app gets a remote URL authenticated by the existing API tokens; a push stores the commit, streams the build live back over the push connection (like Heroku's classic UX), and deploys. Combined with a `git ls-remote` poller for GitHub-hosted repos, RackOps gets push-to-deploy everywhere — including air-gapped racks — with zero external configuration. No competitor in this class does receiver-side push deploys well; it would be the demo moment for the product.

**2. One-click app teleport: move an app AND its data between machines over the mesh**

The WireGuard mesh already connects every node. Make 'Runs on' changes actually complete: snapshot the container's managed volumes on the old node, stream them encrypted over the mesh to the target drive (with live progress in the deploy log), start the new container, verify health, swap ingress routing, then tear down the old one — total downtime measured in seconds, data intact. Homelab hardware churn (new mini-PC, dying disk, rebalancing a hot node) is constant; today migration silently orphans data. 'Drag an app to another machine and it just moves' is a story Dokploy, Coolify, and Portainer cannot tell, and every building block (mesh, agent tasks, drive inventory, deploy log streaming) already exists.

**3. Rack time machine: whole-app snapshots (config + volumes) with restore-anywhere**

A snapshot = the service definition (image/env/mounts/ports) + tarred volumes, written to any S3 endpoint — including a MinIO or SeaweedFS deployed from RackOps' own catalog, keeping the 'your machines' promise. Scheduled per app or fleet-wide, with a restore flow that recreates the app on any node (or a brand-new install, via the join token). This collapses three competitor features — database backups, disaster recovery, and clone/duplicate app — into one mental model users already trust from their laptops, and turns the scariest homelab moment ('the SSD died') into a five-minute recovery.

### Auth, organizations, RBAC, API keys, profile

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| Session auth (hand-rolled HS256 JWT) | basic | auth.go:29-62 — correct HMAC w/ constant-time compare, 7-day fixed expiry (auth.go:163,190). Claims = {sub, role, exp} only: no iat/jti, no refresh, no server-side revocation. Secret is a random ≥32-byte per-cluster value adopted from raft leader on failover (store.go:129-134,204-208). Logout is purely client-side localStorage clear (App.tsx:64, api.ts:127). |
| First-run setup mode | basic | Zero users → entire API open (auth.go:339-345) and org guard grants synthetic admin (orgs.go:186-193); first signup becomes platform Owner (auth.go:127-128). Works, but the window is unauthenticated on a public VPS. |
| Signup / login | basic | auth.go:75-195. Email lowercased, password ≥8 (only policy), bcrypt DefaultCost. Non-first signup requires admin bearer (auth.go:106-119) — API-only, no dashboard UI for it. No login rate limit, no lockout, no failed-attempt logging. |
| Forgot/reset password | solid | auth_account.go:161-259 — genuinely thoughtful: uniform anti-enumeration responses, per-IP (5/h) + per-email (3/h) limiters, SHA-256-hashed single-use 1h token, reset link always printed to server log as offline fallback, CRM email relay best-effort (crm.go:72-85), Host-header-poisoning defense via recorded login URL (auth_account.go:266-287). |
| Change password + profile page | basic | auth_account.go:66-102 verifies current password; event logged. Profile.tsx is read-only identity display + password + API keys + MCP config — no endpoint exists to edit fullName or email. Password change does not invalidate existing sessions. |
| API keys | basic | auth.go:216-302,391-419 — rk_ prefix, SHA-256 at rest, shown once, expiry presets (never/7d/30d/90d/1y), throttled last-used stamp, act-as-creating-user (never anonymous), instant kill if user deleted, admins see/revoke all keys, X-Api-Key header compat. No scopes: every key carries the user's full role install-wide. |
| Platform RBAC (owner/admin/member/viewer) | skeletal | types.go:383-391. Role fixed at account creation — no endpoint to change platform role, delete, or deactivate a user; /api/users is list-only (auth.go:199-212). Viewer enforcement is one coarse method-based gate in middleware (auth.go:379-383). Role checks trust the JWT claim, not the store. |
| Organizations + membership resolution | solid | orgs.go:31-198 — default-org migration is idempotent and adopts orphans; active org resolved per request via X-Rackops-Org header (switch = header change, not new session); membership validated per request; platform owner/admin get synthetic org-admin everywhere; last-owner demote/remove guards (orgs.go:410-421,446-459); delete guarded to empty non-last orgs. |
| Org granular permissions (Dokploy-style) | basic | types.go:455-466 — 7 capability switches + AccessedProjects checkbox tree. orgCan/projectVisible (orgs.go:212-233) are enforced at CreateProjects/CreateServices/DeleteServices/ManageDomains/ManageMachines call sites and project list/get filtering (api.go:737-837, services.go, ingress.go). Editor UI in OrgPanel.tsx:224-298 is complete. |
| Invitations | basic | orgs.go:477-666 — 48h single-use copy-link, SHA-256-hashed token, constant-time lookup, owner-role invites downgraded to admin, already-member check, open preview endpoint, clean landing page (Invitation.tsx). No email delivery, no resend; critical flaw when the invited email belongs to an existing account (see limitations). |
| Org switcher / org panel UI | solid | OrgSwitcher.tsx (Dokploy-style dropdown, active org in localStorage → header) and OrgPanel.tsx (rename/delete, member roles, permission drawer, pending invites) cover the full backend surface for this area. |
| Auth audit events | skeletal | Only 5 event types (user created, password changed/reset-requested/reset-completed, invitation created/accepted) via AddEvent into a 500-entry ring (store.go:238-250). No login success/failure, no IP/UA, no events for API-key create/revoke or member role/permission changes. |

#### Limitations & weaknesses

**1. Invitation accept = account takeover of any existing user**

handleAcceptInvitation (orgs.go:616-665): when the invited email matches an EXISTING account, the password-creation branch is skipped and a session JWT for that account is minted from the link alone (orgs.go:662-665). Any org owner/admin can invite e.g. the platform owner's email, open the link themselves, and receive the owner's session — org admin → platform owner escalation, or plain takeover if an invite link for a current member leaks. This is the single worst bug in the area.

**2. No session revocation of any kind**

Pure stateless 7-day JWTs: logout only clears localStorage (App.tsx:64); password change (auth_account.go:99) and even password reset after a compromise (auth_account.go:247-251) leave every previously issued token valid for up to 7 days. No logout-everywhere, no session list, no JWT-secret rotation endpoint. A stolen token is unstoppable short of editing raft state on disk.

**3. Platform role is frozen into the JWT for 7 days**

All platform-RBAC checks read claims.Role from the token (auth.go:201,264,379; update.go:112) and never re-read the user from the store (only rk_ keys re-resolve, auth.go:418). Demoting or compromising an admin has no effect until their token expires. Combined with no revocation, offboarding an admin is impossible without rotating the cluster secret by hand.

**4. No brute-force protection or audit on login**

handleLogin (auth.go:168-195) accepts unlimited attempts — the reset limiter exists (auth_account.go:135-139) but nothing guards login. Also a timing oracle: unknown emails skip the bcrypt compare (auth.go:186), enabling account enumeration. Failed logins are recorded nowhere.

**5. No platform user lifecycle management at all**

Route table (openapi.go:90) has GET /api/users only: no role change, no delete, no deactivate, no admin-initiated password reset. Admin user creation exists solely as a raw API call (auth.go:106-119) with zero dashboard UI. An ex-employee's account (and its never-expiring API keys, auth.go:230-231) lives forever; the only lever is removing org memberships one by one.

**6. Org admins can self-promote to owner and purge other owners**

orgForManage admits org admins (orgs.go:368), and handleSetOrgMember/handleRemoveOrgMember (orgs.go:380-473) apply no role hierarchy — an admin can PATCH their own membership to owner, or demote/remove any owner as long as one remains. The 'ownership is granted by promotion, never by invite' rule (orgs.go:498-501) is undermined by the promotion path itself.

**7. Auth/org events leak across org boundaries**

eventInOrg (api.go:660-672) returns true for events with no node/service — which is every auth and org event. Members of any org see 'User created: alice@acme.com (admin)', password-reset activity, and invitation events from every other tenant via /api/events. Undermines the org-as-tenant-boundary story beyond the known SSE issue.

**8. Security-critical operations produce no audit events**

API-key creation/revocation (auth.go:216-302), member role/permission changes and removals (orgs.go:380-473), and org rename have no AddEvent calls; the feed itself is a 500-entry ring (store.go:245-247) with no actor IP/UA and no filtering — useless for 'who gave X delete rights?' forensics.

**9. Reset rate limiter breaks behind a reverse proxy**

clientIP (auth_account.go:141-147) uses RemoteAddr and ignores X-Forwarded-For. Behind Traefik/nginx every visitor shares the proxy's IP, so one attacker (or 5 legit requests/hour install-wide) exhausts the per-IP budget (auth_account.go:137) and silently blackholes all password resets. The limiter maps also never evict keys (auth_account.go:117-133) — unbounded growth on an open endpoint.

**10. ?token= bearer accepted on every endpoint, not just SSE**

authMiddleware falls back to the token query param for all paths (auth.go:351-353), though the stated purpose is SSE-only. Session JWTs and rk_ keys end up in access logs, browser history, and Referer headers for any request written that way. Session also lives in localStorage (api.ts:125-127) — one XSS exfiltrates it with no httpOnly-cookie option.

**11. Permissions drawer inverts to full access at zero projects**

Empty AccessedProjects means 'all projects' (orgs.go:224, types.go:464-465). In the drawer (OrgPanel.tsx:231,273-291), an admin restricting a member who unchecks the last project box silently flips them to all-projects access. No distinct 'no projects' state exists.

**12. Resources are permanently stuck in their org**

Node org membership is fixed at first ingest — the comment says moving is 'an explicit admin action' (api.go:98-110) but no such endpoint exists; nothing moves projects or domains either. Org deletion demands the org be emptied first (orgs.go:336-339), so the only path is destroying and recreating everything. Members also see raw user IDs instead of names in OrgPanel since /api/users is admin-only (OrgPanel.tsx:36-47).

#### Improvements

**1. Fix invitation-accept takeover of existing accounts** · impact: critical · effort: S

*Why:* Org admin → platform owner escalation via a copy-link is a shipped privilege-escalation vulnerability (orgs.go:616-665); any leaked invite for an existing member's email is a full account takeover.

*How:* In handleAcceptInvitation, when a user with the invited email already exists, do NOT mint a JWT: require the requester to be authenticated as that user (or verify their password in the request) before enrolling, and return 409 'sign in to accept' otherwise. Update Invitation.tsx to route existing-account invitees through login first.

**2. Add token versioning + logout-everywhere** · impact: high · effort: M

*Why:* Password change/reset after compromise currently leaves stolen sessions valid 7 days with no recourse (auth_account.go:99,247-251).

*How:* Add User.TokenVersion (raft-replicated); embed iat+ver in jwtClaims; middleware rejects tokens with stale ver (one map lookup — the store is in-memory). Bump ver on password change/reset, add POST /api/auth/logout-all, and a 'sign out other sessions' button on Profile. Re-read role from the user record at the same lookup, fixing the 7-day stale-role hole for free.

**3. Ship platform user management (API + Settings UI)** · impact: high · effort: M

*Why:* Cannot promote/demote/delete/deactivate any account after creation (openapi.go:90 — list only); offboarding is impossible and admin user-creation has no UI at all.

*How:* Add PATCH /api/users/{id} (role, disabled flag — checked in middleware/claimsForAPIKey) and DELETE /api/users/{id} (last-owner guard; cascades org memberships + API keys, which claimsForAPIKey already honors, auth.go:407). Add a Users card in Settings with create/role/disable/delete plus an admin 'generate reset link' action reusing the existing reset-token machinery.

**4. Rate-limit login and audit auth attempts** · impact: high · effort: S

*Why:* Unlimited password guessing against an internet-exposed control plane (auth.go:168-195); zero forensic trail of failures.

*How:* Reuse resetLimiter (per-IP ~10/min, per-email ~5/min) on handleLogin; run bcrypt against a dummy hash when the email is unknown to kill the timing oracle; AddEvent on failures (with IP) and successes.

**5. Move machines and projects between orgs** · impact: medium · effort: M

*Why:* Org boundaries are one-way doors: a mis-joined machine or a project that outgrows its org means destroy-and-recreate (api.go:98-110, orgs.go:336-339), and org deletion is practically unreachable.

*How:* POST /api/nodes/{id}/move and /api/projects/{id}/move {orgId} (platform admin or owner of both orgs); reassign OrgID, cascade project services/domains, emit events; add a 'Move to organization…' action in Machines and project menus.

**6. Enforce role hierarchy in org member management** · impact: medium · effort: S

*Why:* Org admins can self-promote to owner and demote/remove other owners (orgs.go:368,380-473), collapsing the owner/admin distinction.

*How:* In handleSetOrgMember/handleRemoveOrgMember: only org owners (or platform owner) may grant/revoke the owner role or touch owner memberships; block self-role-change. ~30 lines plus disabling the owner option for admins in OrgPanel.tsx:163-168.

**7. Stop cross-org leakage of auth/org events** · impact: medium · effort: S

*Why:* Every tenant's member list changes, emails, and reset activity broadcast to all orgs via /api/events (api.go:660-672 default-true).

*How:* Add OrgID (and actor UserID) to types.Event; AddEvent callers in auth.go/orgs.go pass it; eventInOrg matches OrgID and shows org-less platform events (updates, cluster) to platform admins only.

**8. Audit key + membership mutations, with actor identity** · impact: medium · effort: S

*Why:* API-key creation, revocation, and permission grants are invisible (auth.go:216-302, orgs.go:380-473) — the exact operations a post-incident review needs.

*How:* AddEvent in handleCreateToken/handleDeleteToken/handleSetOrgMember/handleRemoveOrgMember including actor email and client IP; raise the event cap for kind=auth or persist auth events in a separate capped log.

**9. Honor X-Forwarded-For behind a trusted proxy** · impact: medium · effort: S

*Why:* The reset limiter collapses to a single shared bucket behind any reverse proxy — trivially DoS-able password recovery (auth_account.go:137,141-147).

*How:* clientIP takes the last hop from X-Forwarded-For only when RemoteAddr is a private/loopback address (or a configured trusted-proxy CIDR in Settings); evict empty limiter keys on the same pass.

**10. Restrict query-param auth to the SSE stream** · impact: medium · effort: S

*Why:* Credentials in URLs land in proxy logs and browser history for every endpoint today (auth.go:351-353).

*How:* Only consult r.URL.Query().Get("token")/("org") when the path is /api/stream (the one place headers are impossible); everything else must use headers.

**11. Make 'no project access' representable** · impact: medium · effort: S

*Why:* Unchecking every project in the permissions drawer silently grants access to ALL projects (OrgPanel.tsx:273-291 → empty slice = wildcard, orgs.go:224).

*How:* Add RestrictProjects bool alongside AccessedProjects (empty+restricted = none); update projectVisible and the drawer's all-projects toggle; migrate nothing (false = today's wildcard behavior).

**12. Editable profile identity + name resolution for members** · impact: medium · effort: S

*Why:* fullName/email are immutable after signup (Profile.tsx:30-53, no endpoint), and non-admin org members see raw user IDs in the members list (OrgPanel.tsx:36-47).

*How:* PATCH /api/auth/me for fullName (email change gated on password re-entry); add a members-directory endpoint returning id+name+email for co-members of the caller's orgs only, and use it in OrgPanel instead of admin-only /api/users.

#### Missing features

**1. OIDC/SSO login** · impact: high · effort: L

Table stakes for team/on-prem adoption (Portainer OAuth, Coolify OAuth, Dokploy planned). Homelabbers want to plug in Authentik/Authelia/Keycloak; small companies want Google/Entra. A single generic OIDC client (authorize-code flow, email-matched account linking, optional auto-provision into an org) covers all of them.

**2. 2FA (TOTP + recovery codes)** · impact: high · effort: M

Dokploy, Portainer, and Coolify all ship it; RackOps controls root-equivalent access to every machine in the fleet, and a single phished password is currently game over. Self-hosted operators specifically expect TOTP since there's no upstream IdP by default.

**3. Scoped API keys** · impact: high · effort: M

Every rk_ key is the user's full power across all their orgs forever (types.go:422-431). CI that only needs 'redeploy service X' holds domain-deletion rights. Org-scoped and read-only flags on key creation (checked where claims are minted, auth.go:391-419) cover 90% of the need; per-capability scopes can follow.

**4. Real audit log** · impact: medium · effort: L

Compliance-minded on-prem buyers ask first: persistent (not a 500-ring), actor+IP+target on every mutation, filterable by user/kind/date, CSV export. The event system is the seed but is currently a toy for this purpose (store.go:238-250).

**5. Active session management** · impact: medium · effort: M

Users of every competitor expect Profile → 'Active sessions' with device/IP/last-seen and per-session revoke. Requires the session registry that logout-everywhere needs anyway — without it, 'is someone else in my account?' is unanswerable.

**6. Invitation + security email delivery via CRM relay** · impact: medium · effort: S

Reset emails already relay through the vendor CRM (crm.go:72-85) but invitations are copy-link only (orgs.go:529-533) and password-change/new-login notifications don't exist. Wiring invites and security notices through the same relay (with copy-link fallback) matches what every SaaS-grade competitor does.

**7. Configurable password policy + breach hygiene** · impact: low · effort: S

Hardcoded 8-char minimum everywhere (auth.go:87, auth_account.go:80,221, orgs.go:625). On-prem deployments have to satisfy internal policy: min length, optional complexity, and a max-length guard before bcrypt's silent 72-byte truncation.

#### Product ideas

**1. Mesh-native zero-config SSO: org RBAC as the fleet's access plane**

Because agents dial out and the server IS the mesh hub, RackOps can do what no competitor does: `rackops login` on a workstation gets a device-code prompt in the dashboard, mints a short-lived scoped key, and — the killer part — grants SSH-over-WireGuard to exactly the machines your org role allows, with every session logged to the audit trail. Your org RBAC becomes your fleet's SSH CA. Zero config, one binary, replaces teleport-lite use cases for homelabs and small teams.

**2. Device-approval login instead of passwords-only (offline-first 2FA)**

First login on a new device shows a 6-digit code; any already-signed-in session (or the server console via `rackops approve`) confirms it. TOTP-level protection with no authenticator app, no email dependency, works fully offline — perfectly matching the 'works on an air-gapped rack' identity. Falls back to the log-printed approval link exactly like password reset already does, so the recovery story stays one consistent mechanism.

**3. Built-in access review: 'who can touch what' report card**

A Settings toggle that snapshots current auth posture (stale never-expiring API keys, members with delete rights they never used, accounts that haven't logged in for 90 days, orgs with one owner) and renders a Cloudflare-style security score card with one-click remediations. Turns the audit data this review says is missing into a product surface — self-hosted operators have no security team, so the binary becomes one.

### Networking: ingress, domains, mesh, tunnels

#### What exists today

| Feature | Depth | Notes |
|---|---|---|
| Traefik edge deploy & lifecycle (pseudo-service) | solid | handleSetIngress (ingress.go:152) deploys traefik:v3.1 with host networking on a chosen node, tracks it as pseudo-service 'ingress-traefik', correlates container state into ingress health (api.go:132-146), and auto-heals a vanished edge container with rate-limited retries (api.go:365-371). Status/failure log tail surfaced in the UI (Network.tsx:182-186). |
| Traefik dynamic config generation (HTTP provider) | basic | handleTraefikConfig (ingress.go:477) renders one Host-rule router + loadBalancer per domain-routed running service, polling every 5s. HTTP routers only — no TCP/UDP routers, no path rules, no middleware beyond an X-Rackops-Edge marker header, single backend URL per service (no replicas/health checks). Only Status==running services get routers (ingress.go:494), so routes 404 during redeploys. |
| Automatic HTTPS (ACME) | basic | HTTP-01 challenge only, single 'le' resolver, acme.json in a container volume (ingress.go:240-249). Turning it on force-redirects ALL plain HTTP to HTTPS globally. No DNS-01 (so no true wildcard certs — wildcards get per-subdomain certs), no staging CA option, no custom cert upload, breaks silently if the HTTP port isn't 80. |
| Domain management + continuous verification | solid | Genuinely deep pipeline (domains.go:48-169): DNS lookup vs expected edge IP, an edge-identity probe using a synthetic Host header + response marker to prove OUR Traefik answers (probeEdge, domains.go:175), TLS SNI cert probe with issuer/expiry extraction and Traefik-default-cert detection (probeCert, domains.go:212), wildcard checked via random probe labels, honest verdicts for hairpin-NAT and LAN-only cases. Auto re-check every 30s until verified, hourly after; HA leader-only. |
| Registrar instructions UX | solid | Network.tsx:306-362 shows the exact record (Type A / Host with '@', '*' computed by recordHost / Value = edge IP / TTL) with copy buttons, plus per-domain DNS/Edge/Certificate LED checklist and last-checked timestamps. Best-in-class vs Dokploy/Coolify's bare domain fields. |
| WireGuard mesh (userspace, dial-out, CGNAT-safe) | solid | internal/mesh/mesh.go: wireguard-go + netstack, no root/TUN/port-forwarding on members; hub at 10.77.0.1 (meshhub.go), persisted keys + IP allocations so mesh IPs survive restarts, persistent-keepalive 25s for NAT. Agents proxy service ports onto their mesh IP (agent/mesh.go:72-87). Strictly hub-and-spoke: agents only ever peer with the hub (agent/mesh.go:52-59) — the peer list sent to agents is never used, so no p2p paths exist. |
| Hub relay for cross-node ingress routing | basic | For domain-routed apps on non-edge nodes: Traefik → 127.0.0.1:4200x on the edge node → agent relay listener → over WG to hub 10.77.0.1:4200x → hub ProxyMesh forwards to target node's meshIP:port (ingress.go:288-357, mesh.go:196-229, agent/mesh.go:90-140). Works but every cross-node byte transits the control-plane server twice, and it carries two real bugs (see limitations). |
| Instant nip.io URLs | basic | handleExposeService (ingress.go:402) mints <app>.<edge-ip-dashes>.nip.io through the user's own edge — nice zero-DNS onboarding. HTTP URL only; interacts badly with the global HTTPS redirect when ACME is on (LE issuance for nip.io names generally fails on shared rate limits). |
| Cloudflare quick tunnels | basic | tunnel.go + runtime.go:456-498: runs cloudflared quick-tunnel container with host networking, scrapes the trycloudflare URL from logs, correlates via pendingTunnels, restarts vanished tunnels via auto-heal (api.go:351-363) with an honest 'URL will change' event. Ephemeral/testing only — no persistent named tunnels with an account token. |
| Network diagnosis + public-access wizard | solid | handleNetworkDiagnosis (domains.go:260) classifies each node direct/nat/unknown from detected public IP; Network.tsx wizard presents port-forwarding vs VPS-anchor (with copyable join command) vs quick-tunnel paths, with a LAN-only warning banner. Excellent homelab onboarding no competitor has. |
| Port management | basic | host port / container port / extra ports model (types.go:224-226), auto-bump to next free port with a log note (runtime.go:220-248), /api/suggest-port. TCP only — '-p host:container' never appends /udp, so UDP workloads (game servers, DNS) cannot be published. Extra ports are never exposed on the mesh or relayed (api.go:203-207 exposes only svc.Port), so only the primary port of an app is reachable cross-node. |
| Mesh status UI/API | skeletal | GET /api/mesh lists hub IP/key/UDP port and peer meshIPs; the Reachable field (mesh_api.go:22) is declared but never populated, and the dashboard shows only name+IP rows (Network.tsx:241-247). No last-handshake, no RTT, no transfer stats, no health. handleMeshDial exists as a debug endpoint but has no UI. |
| /relay/{id} HTTP proxy path | skeletal | mesh_api.go:65-105 reverse-proxies to any service over the mesh — but nothing in the dashboard or the current relay design uses it; the RelaySpec/ProxyMesh path replaced it. It is dead code that is also mounted UNAUTHENTICATED (auth.go:321 opens everything not under /api/). |

#### Limitations & weaknesses

**1. Hub relay listeners flap on every non-ingress agent report (intermittent 502s)**

relaySpecsFor (ingress.go:288) is called for EVERY reporting node (api.go:212). For any node that is not the ingress node, the Mutate closure returns early leaving `desired` empty — then reconcileRelays(desired) (ingress.go:342) closes ALL hub relay listeners (ports >= 42000). The next edge-node report (up to 2s later) recreates them. In any fleet with >1 machine, hub relays are torn down and rebuilt every couple of seconds, so new connections to remote-node apps randomly get 'connection refused' → Traefik 502. This is the flagship cross-node routing path silently degrading itself.

**2. Relay target goes stale when a service migrates nodes**

ProxyMesh (mesh.go:196-200) returns early if a listener already exists on the port, without checking whether targetMeshAddr changed. A service that keeps its RelayPort but moves to another node keeps being forwarded to the OLD node's meshIP:port. Only the flapping bug above accidentally refreshes it. Same early-return pattern in ProxyPort (mesh.go:149-153).

**3. Several networking endpoints skip org/permission checks entirely (cross-org IDOR)**

handleSetIngress/handleGetIngress (ingress.go:145-195) have no requireOrg or permission check — any authenticated user in any org can reconfigure or DISABLE the shared edge. handleSetServiceDomain (ingress.go:360), handleExposeService (ingress.go:402), and both quick-tunnel handlers (tunnel.go:20,43) fetch services by raw ID with no org ownership check, unlike services.go which scopes everything. Any logged-in user can re-route, expose, or tunnel another org's app.

**4. /relay/{id}/ and /api/traefik/config are unauthenticated information/access leaks**

The auth middleware opens any path not under /api/ (auth.go:321-333), so /relay/{serviceId}/... (api.go:56) lets ANYONE who can reach the control-plane port proxy HTTP straight to any service over the mesh — guarded only by 12-hex-char service IDs that appear in URLs and events. /api/traefik/config is intentionally open for Traefik but leaks every domain, service ID, backend port, and relay port to unauthenticated callers.

**5. Moving the edge to a different node leaves the old Traefik running**

handleSetIngress's enabled path (ingress.go:172-178) only deploys to the new node; no TaskRemove is queued for the previous ingress node (teardown happens only on disable, ingress.go:179-192). The orphaned Traefik keeps holding :80/:443 on the old machine with a stale config, still restart=unless-stopped.

**6. HTTP-only edge: no TCP or UDP ingress at all**

The dynamic config renders only http.routers (ingress.go:552-558) — no tcp/udp router sections, no extra entrypoints. Databases, MQTT, SSH, game servers can only be reached by raw host port on the node they run on; cross-node they are unreachable because only svc.Port is mesh-exposed (api.go:203-207) and the mesh proxy is TCP-only (mesh.go:142-144 dials 'tcp'). Dokploy/Coolify/Portainer users expect at least TCP proxying for databases.

**7. No route-level access control, rate limiting, or custom middleware**

The only middleware ever generated is the X-Rackops-Edge marker header (ingress.go:480-488). No basic-auth on routes, no IP allowlists, no rate limiting, no redirects (www→apex), no path prefixes, no custom headers, no compression. Coolify and Dokploy both ship basic-auth and custom Traefik label passthrough; RackOps has no escape hatch at all.

**8. Enabling ACME force-redirects everything to HTTPS, breaking instant URLs**

ACME adds a global entrypoint-level HTTP→HTTPS redirect (ingress.go:246-249). nip.io instant URLs then redirect to HTTPS where issuance typically fails (shared nip.io LE rate limits), yet publicUrls (ui.tsx:168-178) happily builds https:// links for them. Also HTTP-01 silently cannot work when the edge's HTTP port isn't 80 — the UI hint (Network.tsx:207-209) mentions it but nothing validates or warns.

**9. No mesh peer revocation or key rotation — removed machines keep mesh access forever**

internal/mesh/mesh.go has AddPeer but no RemovePeer; MeshHub never deletes from its peers map or the WG device (meshhub.go:129-166), and allocations persist to disk. A decommissioned or compromised node's key remains a valid mesh peer able to dial every exposed service port until the hub key file is manually rotated.

**10. Domain assignment has no conflict or ownership validation**

handleSetServiceDomain (ingress.go:375-391) accepts any hostname string: no check that the domain exists in the org's Domains, is verified, or is not already routed to another service. Two services with the same domain produce two Traefik routers with identical Host rules (nondeterministic routing). Domain creation is first-come-first-served install-wide (ingress.go:79-92) with no TXT ownership proof, so in a shared install one org can squat another org's hostname.

**11. All cross-node traffic transits the control-plane server; mesh has no HA story**

Spokes only peer with the hub (agent/mesh.go:52-59), so edge→app traffic crosses the WG hub twice through userspace netstack — the control plane is a throughput bottleneck and SPOF for data traffic. Under raft HA, mesh-hub keys/allocations are local files on one server (meshhub.go:42-92) and agents pin HubAddr to one advertise address — mesh does not fail over with leadership.

**12. Mesh health is invisible**

MeshStatus.Reachable is never populated (mesh_api.go:22), no last-handshake or RTT is surfaced, and a spoke whose WG session died still shows as a green 'active' mesh row. Debugging 'my app 502s via relay' has zero supporting UI; handleMeshDial exists but is unexposed.

**13. No IPv6 anywhere**

Verification expects a single IPv4 A record (domains.go:106-128 message text says 'A:'), the mesh is 10.77.0.0/16 only, nip.io URLs are IPv4-based, and no AAAA guidance exists — an IPv6-only or dual-stack homelab (increasingly common behind CGNAT, DS-Lite) gets misleading 'misconfigured' verdicts.

#### Improvements

**1. Fix hub relay teardown flapping** · impact: critical · effort: S

*Why:* The core cross-node routing path randomly refuses connections every ~2s in any multi-node fleet — users see unexplained 502s on exactly the 'apps stay home, VPS is the edge' topology the product is built around.

*How:* In relaySpecsFor (ingress.go:288), return before reconcileRelays when nodeID != ingress node (or make reconcileRelays a no-op unless invoked for the ingress node's report). Add a regression test that a worker report leaves hub ProxiedPorts untouched.

**2. Add org/permission checks to all networking mutations and close /relay** · impact: high · effort: M

*Why:* Any authenticated user can disable the fleet's edge or re-route/expose/tunnel another org's app; unauthenticated visitors can proxy to internal services via /relay/{id}. In a product selling orgs/RBAC this is a headline security hole.

*How:* Apply requireOrg + a ManageDomains/ManageIngress-style permission to handleSetIngress, handleSetServiceDomain, handleExposeService, and quick-tunnel handlers, verifying svc's node belongs to the caller's org (pattern already in service_manage.go:50). Delete the unused /relay route (mesh_api.go:65, api.go:56) or gate it behind auth. Optionally add a shared-secret header to /api/traefik/config passed via Traefik's provider headers.

**3. Refresh relay targets on change (ProxyMesh/ProxyPort upsert)** · impact: high · effort: S

*Why:* Service migration to another node silently leaves the hub forwarding to the old machine — traffic reaches a dead or, worse, wrong backend.

*How:* Track target per port in Net.proxies (map[int]struct{ln, target}); in ProxyMesh (mesh.go:196), if the port exists with a different target, close and re-listen (or store target atomically and read it per-Accept). Mirror in reconcileRelays.

**4. Surface mesh peer health (handshake age, RTT) in the Network page** · impact: medium · effort: M

*Why:* When relay routing breaks, users currently have nothing — a green 'active' badge over dead WG sessions. Tailscale-grade visibility is the bar.

*How:* Populate MeshStatus.Reachable by running the existing handleMeshDial probe (or dev.IpcGet last_handshake_time) per peer on request; render handshake age + RTT chips per machine in Network.tsx and a 'via mesh relay' health hint on routed apps.

**5. Make ACME resilient: port-80 preflight, LE staging toggle, cert-expiry alerts, exempt nip.io from redirect** · impact: medium · effort: M

*Why:* The three most common homelab TLS failures (port 80 closed, LE rate-limit burn during experimentation, silently expiring certs) are all unguarded; instant URLs break the moment TLS is enabled.

*How:* Warn/deny ACME when HTTPPort != 80 (or the probe shows 80 unreachable); add a staging-CA checkbox; feed probeCert's CertExpiry into the existing alerts pipeline (<14 days); skip TLS/redirect for *.nip.io routers by rendering them web-entrypoint-only in handleTraefikConfig.

**6. Expose extra Ports on the mesh and relay them** · impact: medium · effort: M

*Why:* Multi-port apps (web+metrics, app+db in one template) are only reachable cross-node on their primary port; users hit dead connections with no explanation.

*How:* In the ingest mesh exchange (api.go:203-207), append svc.Ports to ExposePorts; extend relaySpecsFor to allocate relay ports per (service, port) pair and render additional Traefik services when a secondary port is domain-routed.

**7. Keep routes up during redeploys** · impact: medium · effort: M

*Why:* handleTraefikConfig drops the router the instant a service leaves 'running' (ingress.go:494), so every deploy is a hard 404 window instead of the zero-downtime story users expect from a PaaS.

*How:* Render routers for services in building/deploying states too (backend keeps pointing at the old container until the new one is up — pairs with a start-before-stop deploy), or at minimum keep the router and let Traefik return 502 briefly rather than 404.

**8. Revoke mesh peers when a machine is removed** · impact: medium · effort: M

*Why:* Decommissioned machines keeping fleet-wide network access indefinitely is a real security liability for the on-prem audience.

*How:* Add Net.RemovePeer (IpcSet 'public_key=...\nremove=true\n'), call it from node deletion, delete from MeshHub.peers + saveAllocations, and free the mesh IP for reuse guarded against RelayPort pinning.

**9. Tear down the old edge when the ingress node changes** · impact: medium · effort: S

*Why:* An orphaned Traefik keeps squatting :80/:443 on the previous machine with stale routes — confusing 'why does the old IP still answer' and blocks reusing the ports.

*How:* In handleSetIngress's enabled path (ingress.go:172), read the previous NodeID under the store lock and, if different, enqueue the same TaskRemove used by the disable path before deploying to the new node.

**10. Validate domain routing: verified-domain check, uniqueness, org ownership** · impact: medium · effort: S

*Why:* Typos and duplicate assignments produce nondeterministic Traefik routing with zero feedback; cross-org squatting is possible on shared installs.

*How:* In handleSetServiceDomain, reject a domain already routed to another service (offer takeover), warn/deny when the backing Domain record is missing or unverified, and require the matching Domain (exact or wildcard base) to belong to the caller's org. Add TXT-record ownership verification as an optional stronger check on domain creation.

#### Missing features

**1. TCP (and UDP) ingress through the edge** · impact: high · effort: L

Every competitor (Dokploy, Coolify, Portainer+Traefik, k8s) can expose a Postgres/MySQL/Redis/MQTT port through the proxy; RackOps caps its own headline 'apps on any machine, one edge' story at HTTP. Cross-node TCP is already 90% built (mesh relays are raw TCP) — only the Traefik tcp.routers rendering and per-service entrypoint allocation are missing. UDP additionally needs '/udp' publishing in runtime.go and a UDP-capable relay.

**2. DNS-01 ACME (provider API keys) and true wildcard certificates** · impact: high · effort: L

The wildcard-domain feature currently issues per-subdomain HTTP-01 certs — slow first hits, LE rate-limit exposure, and completely broken when port 80 can't be opened (CGNAT users, exactly this product's audience). DNS-01 via Cloudflare/deSEC/RFC2136 credentials fixes all three and enables HTTPS without any inbound port.

**3. Route-level protection: basic-auth, IP allowlist, rate limiting** · impact: high · effort: M

The most-requested Traefik middlewares for self-hosters (protect an admin panel, LAN-only route, brute-force damping). Coolify/Dokploy both offer basic-auth per route. Trivial to render as Traefik middlewares in the existing dynamic config; needs a small per-service UI block and htpasswd-style secret storage.

**4. Multiple domains per service, redirects, and path routing** · impact: high · effort: M

svc.Domain is a single string (types.go:231); www+apex, legacy hostnames, or /api path splits — table stakes in every competitor — are impossible. Needs Domain → []DomainRoute with per-route options and multi-router rendering.

**5. DNS provider integration (auto-create records)** · impact: medium · effort: L

The registrar-instructions UX is great, but users with Cloudflare/Hetzner/deSEC-managed zones expect 'connect provider, records appear' — removes the single biggest drop-off step (manual DNS) and enables DNS-01 with the same credentials.

**6. Persistent named Cloudflare tunnels (account token)** · impact: medium · effort: M

The quick-tunnel UX proves demand, but 'URL changes on restart' disqualifies it for real use. Accepting a Cloudflare tunnel token + hostname (like Coolify's integration) turns the same cloudflared plumbing into a production-grade CGNAT bypass with the user's own domain.

**7. Custom Traefik configuration escape hatch** · impact: medium · effort: M

Power users hitting any gap (sticky sessions, compression, custom headers, forward-auth to Authelia/authentik) currently have zero recourse — the config endpoint is fully generated. A per-service raw middleware/labels JSON blob merged into handleTraefikConfig is the classic pressure valve Dokploy/Coolify ship.

**8. Certificate management surface** · impact: medium · effort: M

No page lists issued certs, expiry, or allows uploading a custom/wildcard cert bought elsewhere; acme.json lives invisibly inside a container volume and is lost if the edge moves nodes (re-issuance against LE rate limits). Kubernetes/Coolify users expect at least visibility plus custom-cert upload.

**9. P2P mesh paths with hub fallback** · impact: medium · effort: XL

All app traffic funnels through the control plane twice — a $4 VPS anchor becomes the bandwidth ceiling for a whole homelab. The hub already distributes the full peer list (meshhub.go:153-157) that agents ignore; adding endpoint discovery + UDP hole punching (fall back to hub relay) is the WireGuard-native fix Netbird/Tailscale users expect.

**10. Multi-edge / edge failover** · impact: medium · effort: XL

One ingress node is a hard SPOF for all public traffic even though the product ships raft HA for the control plane. Running the edge on 2+ nodes from the same dynamic config, with health-checked DNS (multiple A records) or keepalived-style failover guidance, is what 'production PaaS on your machines' implies.

#### Product ideas

**1. Built-in internal DNS + service discovery over the mesh (.rack names)**

Every service gets api.myproject.rack resolvable from every machine and container, backed by a tiny DNS server on the hub's netstack and per-node resolver injection. Apps on different machines talk by name over encrypted WireGuard with zero ports, zero IPs, zero config — Fly.io's .internal magic delivered by the same single binary that already runs the mesh. It converts the mesh from an ingress implementation detail into a visible product feature (private east-west networking) no Dokploy/Coolify install can match.

**2. RackOps Front Door: vendor-hosted anchor with instant *.yourname.rackops.app HTTPS**

One click gives any CGNAT'd homelab a permanent public hostname with real TLS: the agent dials out to a vendor-run relay fleet (same WireGuard/dial-out machinery, same CRM/telemetry channel that already exists), which terminates *.rackops.app with a wildcard cert and tunnels to the user's edge. It's the quick-tunnel UX without the URL roulette — a monetizable managed service that perfectly matches 'runs on YOUR machines, reachable from anywhere, zero config', and a clean upgrade path: outgrow it, add your own domain, nothing else changes.

**3. Follow-the-sun edge: self-healing public entry that migrates automatically**

Instead of pinning ingress to one node, the fleet continuously scores every machine's public reachability (the edge-probe machinery already exists) and automatically redeploys the edge — and, with a DNS provider connected, repoints A records — when the current edge machine dies or loses its public IP (ISP renumbering, VPS outage). Domains keep working through hardware failure with zero human action: 'your rack heals its own front door', a genuinely differentiated reliability story for on-prem PaaS.

## Competitive landscape

### CapRover & Portainer CE (combined, 2025–2026 feature sets)

#### Feature comparison

| Feature | Theirs | RackOps |
|---|---|---|
| One-click app catalog | CapRover: marketplace of one-click apps (Postgres, MongoDB, WordPress, Ghost, Nextcloud, Gitea, Vaultwarden, etc.). Portainer CE: app templates + custom template lists. | has |
| Automatic HTTPS (Let's Encrypt) + wildcard certs | CapRover: auto-HTTPS with force-redirect, wildcard via DNS-01, multiple custom domains per app. Portainer: N/A (no ingress management). | has |
| Customizable reverse-proxy config | CapRover: fully customizable per-app nginx templates + Certbot overrides for advanced routing. | missing |
| Infrastructure monitoring | CapRover: one-click Netdata (leader node only) with SMTP/Slack/Telegram/PushBullet alerts. Portainer CE: basic container stats/log streaming. | has |
| Multi-node clustering + load balancing | CapRover: Docker Swarm cluster — join nodes in seconds, nginx auto-configured to load-balance across nodes (needs open ports + a registry). | partial |
| Horizontal replica scaling with self-healing | CapRover: set Instance Count, crashed instances auto-respawn, requests spread by built-in LB. | missing |
| Zero-downtime deploys + one-click rollback | CapRover: deploy waits for health check before traffic switch; revert icon in Deployment tab rebuilds any previous version. | missing |
| Git push-to-deploy webhooks + deploy CLI | CapRover: GitHub/GitLab/Bitbucket webhook builds from branch on push (HTTPS creds or SSH deploy key), plus `caprover deploy` CLI with build-failure reporting, tarball and captain-definition paste. | partial |
| Private/self-hosted Docker registry management | CapRover: enable self-hosted registry from Cluster tab or attach external registry. Portainer CE: add private registries for deploys (browsing/tag mgmt is BE). | missing |
| Backup & restore + disk cleanup | CapRover: built-in backup & restore of server config plus unused-image disk cleanup tooling. | missing |
| No lock-in / plain-Docker escape hatch | CapRover: remove CapRover and apps keep running as standard Docker containers. | missing |
| Deep container-level operations UI | Portainer CE: full CRUD on containers/images/volumes/networks, exec console into containers, live log streaming, image builds — across every connected environment. | partial |
| Compose/stack management depth | Portainer CE: deploy and edit docker-compose stacks from UI or git (manual pull); BE adds full GitOps auto-update via webhook/polling with change windows. | partial |
| Multi-orchestrator support | Portainer: manages Docker standalone, Swarm, Kubernetes (incl. Helm charts, manifests, namespaces), Podman, Azure ACI from one pane. | missing |
| Teams + access control | Portainer CE: Admin/Standard/Read-Only roles, teams, limited LDAP/OAuth; full 7-role RBAC, SSO, audit logs are BE-only. | has |
| Edge agents for remote/NAT nodes | Portainer: Edge Agent polls out to server for firewalled sites — but most edge capability (edge stacks with private registries, edge groups) is BE-gated. | has |
| API access | Portainer: documented REST API + webhooks. CapRover: experimental API via `caprover api` CLI. | has |
| GitOps auto-redeploy (webhook/scheduled polling) | Portainer BE: git-driven stack auto-update with polling intervals, change windows, stored credentials (CE = manual re-pull). CapRover: webhook-triggered rebuilds. | partial |

#### Standout features worth studying

- **Zero-downtime deploys with health-checked traffic switch + one-click rollback (CapRover)** — Deploys are the riskiest moment for a self-hoster; keeping the old container serving until the new one passes health checks, with instant revert to any prior version, turns a bad deploy from an outage into a non-event. RackOps lists no rollback or blue/green story.
- **Replica scaling with built-in load balancing and self-healing (CapRover/Swarm)** — Setting Instance Count = 3 gives horizontal scale, crash auto-respawn, and request spreading for free. RackOps places an app on one machine via placement scoring but has no stated multi-replica or LB-across-replicas capability.
- **Deep container operations console — exec shell, live logs, volume/network/image CRUD (Portainer CE)** — Day-2 debugging is where PaaS abstractions leak; Portainer lets users drop into a container shell and inspect volumes/networks from the browser instead of SSH-ing. RackOps telemetry shows the machine, not the container internals.
- **Per-app customizable reverse-proxy templates (CapRover nginx)** — Real deployments always hit an edge case — custom headers, websocket tweaks, body-size limits, redirects. CapRover exposes the full nginx template as an escape hatch; RackOps' Traefik ingress has no stated user-editable config surface.
- **Private registry integration incl. one-click self-hosted registry (both)** — Private images are table stakes for any team past hobby stage, and CapRover's cluster mode requires a registry so images reach all nodes. RackOps deploys from images/git but lists no registry credential or hosting story.
- **Built-in backup & restore + disk cleanup (CapRover)** — Self-hosters own their own disaster recovery; one-click config backup and pruning of stale build images prevents the classic 'disk full, server dead' failure. RackOps has no stated backup story.
- **Kubernetes / multi-orchestrator management incl. Helm (Portainer)** — Teams that outgrow a fleet tool standardize on k8s; Portainer rides that growth path (manifests, Helm, namespaces, Podman, ACI) while RackOps' proprietary agent model is a ceiling for that audience.
- **Push-to-deploy build pipeline on the server (CapRover)** — CapRover builds the image from source on git push (webhook or CLI) with build-failure feedback — a true Heroku-style workflow with no external CI. RackOps deploys from a git repo but has no stated on-push auto-build/redeploy loop.

#### Where RackOps is ahead

- NAT/CGNAT-native fleet: agents dial OUT with zero port forwarding — CapRover requires ~7 open inbound ports per node and a public IP, and Portainer gates most Edge Agent capability behind Business Edition; RackOps ships this free and by default.
- WireGuard mesh between machines: encrypted cross-machine networking out of the box, vs CapRover's default-unencrypted Swarm overlay and Portainer's total absence of a networking layer.
- Free, granular RBAC: orgs, roles, granular permissions, invitation links, and API keys with expiry — Portainer locks full RBAC/audit behind BE licensing and CapRover is single-admin only.
- Single static Go binary with embedded React dashboard — no bootstrap container, no Docker dependency for the control plane, trivial install/upgrade vs both competitors' container-based servers.
- Hardware-level telemetry (CPU/mem/temp/disk/GPU) with rack visualization and a sustained-threshold alert engine with auto-resolve — richer and more integrated than CapRover's leader-node-only Netdata or Portainer CE's basic stats, with no SMTP setup needed (Discord/webhook notifications built in).
- First-class API story: OpenAPI spec + embedded Swagger UI + MCP server subcommand for AI agents — vs CapRover's 'experimental' CLI-tunneled API; the MCP integration has no equivalent in either competitor.
- Zero-config account recovery and support: hosted password-reset relay (no SMTP) and support tickets relayed to vendor CRM — both competitors require users to wire up SMTP or rely on community forums.
- Control-plane HA (raft-based, experimental) and one-click self-update from the dashboard — CapRover's captain and Portainer's server are both single points of failure with manual upgrade paths.
- Onboarding polish: DNS-record verification UX, instant public URLs, setup checklist, SSE live updates, and an AI copilot drawer — a materially smoother first-hour experience than either competitor.

### Dokploy (v0.29.x, 2025–2026 — OSS self-hosted + Cloud + Enterprise editions; ~28k GitHub stars, 5M+ Docker pulls)

#### Feature comparison

| Feature | Theirs | RackOps |
|---|---|---|
| Git provider integrations | Native GitHub App, GitLab, Bitbucket, Gitea providers + generic private Git via SSH keys; per-provider permission management (v0.24) | partial |
| Build types | Dockerfile, Nixpacks, Heroku Buildpacks, Paketo Buildpacks, Railpack — deploy Node/PHP/Python/Go/Ruby with zero Dockerfile | partial |
| Docker Compose deployments | Native Compose support incl. raw compose, per-service logs/monitoring/terminal, custom build command | has |
| Docker image deployments | Deploy prebuilt images from any registry (Docker provider) | has |
| Docker Swarm multi-node scaling | Scale services to replicas across Swarm cluster nodes; cluster settings per app | partial |
| Managed databases | First-class MySQL, PostgreSQL, MongoDB, MariaDB, Redis (+libsql) with lifecycle UI, custom images, volumes, resource limits, terminal | partial |
| Database backups | Manual + scheduled backups to S3-compatible destinations with retention limits and auto-cleanup (v0.20) | missing |
| Docker volume backups | Backup/restore of arbitrary volumes for apps and Compose stacks (e.g. n8n SQLite) to external storage (v0.24) | missing |
| Platform backup/restore | Backup and restore of the Dokploy instance itself (v0.21); env vars encrypted at rest AES-256-GCM with exportable keyring (v0.29.12) | missing |
| Preview deployments | Auto per-PR environments with traefik.me or wildcard custom domains, PR comment with link, label filters, per-app limits, ${{DOKPLOY_DEPLOY_URL}} env injection | missing |
| Rollbacks | One-click rollbacks, re-engineered as registry-based so rollback images live in an external registry, not on-server (v0.24→v0.26) | missing |
| Scheduled tasks / cron jobs | Cron with IANA timezones at app, Compose-service, remote-server, and Dokploy-server level; manual run; API endpoints | missing |
| Watch paths (monorepo) | Trigger deploys only when specific directories change (v0.20) | missing |
| Auto-deploy on push | Webhook-driven auto deploy from all git providers | partial |
| Custom build servers & build queues | Dedicated build servers separate from deploy servers (v0.26); per-server build queues with 2 concurrent builds OSS, unlimited Enterprise (v0.29) | missing |
| Docker registry management | Configure external/self-hosted registries for Swarm distribution, build servers, and rollback image storage | partial |
| Multi-server management | Control plane manages remote servers over SSH + remote Docker API; requires root/passwordless-sudo SSH and inbound reachability; remote-server monitoring unsupported | has |
| Monitoring | CPU/mem/disk/network per server and per container (20s refresh, 2-day retention); advanced monitoring is Cloud-only; no GPU/temp metrics (Prometheus extension template for more) | has |
| Notifications | Deploy success/failure + threshold alerts via Slack, Discord, Telegram, Email, Gotify, generic webhook (v0.26) | partial |
| Template catalog | 388+ one-click open-source templates in a community repo (templates.dokploy.com) independent of core releases | partial |
| REST API + OpenAPI/Swagger | Full API (tRPC + REST) with Swagger docs and redesigned API keys (v0.19) | has |
| Standalone CLI | Separate Dokploy CLI for managing apps/databases from the terminal | partial |
| MCP server for AI agents | Official Dokploy/mcp package (separate install) covering apps, servers, Traefik, GPU setup, cleanup | has |
| AI assistant | BYOK AI (OpenAI, Anthropic, Ollama, Azure, Gemini, any OpenAI-compatible) generating Docker Compose from natural language; multiple providers switchable | has |
| Organizations / multi-tenancy | Organization → Project → Environment → Service hierarchy; resource isolation; multi-admin (v0.26) | has |
| Environments per project | Multiple named environments (prod pre-configured) within each project (v0.25) | missing |
| Auth: 2FA / SSO / audit logs / custom roles / whitelabel | 2FA + email/password OSS; SSO, custom roles, audit logs, whitelabeling, air-gapped license gated to Enterprise | partial |
| Web terminal | In-browser terminal into app containers, Compose services, databases, and remote servers | missing |
| Traefik ingress + auto-HTTPS | Traefik standalone container with HTTP3 default, Let's Encrypt auto-SSL, custom cert resolvers, editable Traefik config, redirects/security headers | has |
| Instant free domains | Auto-generated traefik.me domains, zero DNS config | has |
| Real-time logs & build logs | Live logs per app/service/DB, build logs, cancel queued deployments | has |
| Server maintenance tooling | Docker cleanup, Traefik file editor, GPU setup endpoint, patches system, setup validation checks | partial |

#### Standout features worth studying

- **Preview deployments (per-PR environments)** — Auto-spun ephemeral envs with PR-comment links, label filtering, and free traefik.me domains are the single most-loved 'Vercel-at-home' workflow; it's a headline reason teams pick a PaaS and RackOps has nothing equivalent.
- **Registry-based rollbacks** — One-click revert to a previous image — stored in an external registry so it survives server loss — is a production-safety must-have; without it a bad RackOps deploy means manual re-deploy of an older ref under pressure.
- **Database + Docker volume backups to S3 with retention** — Scheduled backups with auto-cleanup and restore for both databases and arbitrary volumes is core stateful-workload insurance; RackOps deploying DB templates without backup UX makes it hard to trust with production data.
- **Buildpack variety (Nixpacks / Heroku / Paketo / Railpack)** — Zero-Dockerfile deploys straight from source dramatically widen the audience — hobbyists and app devs who don't write Dockerfiles can still ship; RackOps' git deploys presumably require a Dockerfile.
- **Scheduled tasks / cron jobs (container + host level)** — First-class cron with timezones at app, Compose-service, and server level removes the #1 reason users SSH back into boxes; it's table stakes for real workloads (cleanup jobs, report generation, cert scripts).
- **Template catalog scale (388+ community templates)** — A separate community-maintained repo with per-PR template previews compounds network effects; RackOps' catalog competes with an ecosystem, not just a feature.
- **Environments per project (dev/staging/prod)** — Named environments with isolated services and variables inside one project map to how teams actually promote software; RackOps orgs/roles don't substitute for env-level separation.
- **Custom build servers + per-server concurrent build queues** — Offloading builds to dedicated machines and parallelizing per-server keeps the control plane and production hosts responsive during heavy CI — an architectural maturity signal RackOps lacks.

#### Where RackOps is ahead

- Agent dial-OUT fleet architecture: RackOps agents connect outbound to the server, working behind CGNAT/NAT with zero port forwarding; Dokploy requires inbound SSH with root or passwordless-sudo access to every remote server — a non-starter for home labs, edge sites, and locked-down networks.
- Built-in WireGuard mesh between machines; Dokploy's docs merely recommend you bring your own VPN/private network (Tailscale/Cloudflare Tunnel guides) for inter-server security.
- Hardware-level telemetry — CPU/mem/temp/disk/GPU with rack visualization — in the free product; Dokploy has no GPU or temperature metrics at all, gates advanced monitoring to Cloud, and doesn't support monitoring on remote servers.
- Single static Go binary with embedded React dashboard vs Dokploy's Docker Swarm + Traefik + PostgreSQL + Redis service stack — far simpler install, upgrade, and disaster-recovery story.
- Sustained-threshold alert engine with auto-resolve is a real alerting system; Dokploy only offers simple CPU-threshold notifications, pushing users to an external Prometheus/Grafana stack.
- Placement scoring for choosing which machine runs a workload; Dokploy has no scheduler intelligence beyond manual server selection.
- Raft-based HA control plane (experimental); Dokploy's control plane is a single instance with no HA story in any edition.
- Granular permissions, API keys with RBAC + expiry, DNS-record verification UX, and MCP server all in the OSS core binary — Dokploy gates custom roles, SSO, and audit logs behind Enterprise and ships MCP as a separate package.
- Zero-config operability extras: hosted password-reset relay (no SMTP setup), one-click dashboard self-update, and support tickets relayed to vendor CRM — onboarding friction Dokploy leaves to the operator.

### Coolify (v4.0 stable Apr 2026, v4.1.x current; open-source Laravel/PHP PaaS, Apache 2.0, ~55k GitHub stars, paid cloud tier $5/mo)

#### Feature comparison

| Feature | Theirs | RackOps |
|---|---|---|
| One-click service catalog | 280+ templates (n8n, WordPress, Ghost, Grafana, Ollama, etc.), community-contributed, synced to self-hosted instances | partial |
| Git integrations | GitHub App (private repos, granular perms), GitLab, Bitbucket, Gitea — hosted AND self-hosted instances; OAuth/PAT/deploy keys; auto-deploy on push via webhooks; [skip ci] commit markers | partial |
| PR preview deployments | Auto-deploys each pull request to an isolated environment with its own URL (requires GitHub App integration) | missing |
| Managed databases | First-class one-click DBs: PostgreSQL, MySQL, MariaDB, MongoDB, Redis, KeyDB, DragonFly, ClickHouse with lifecycle management | partial |
| Scheduled S3 database backups | Per-database cron-scheduled dumps (pg_dump/mongodump/RDB) with retention counts, schema include/exclude, to any S3-compatible destination (AWS, MinIO, R2, B2, Wasabi); backup status notifications | missing |
| Automated disk cleanup | Threshold-based and scheduled Docker pruning (stopped containers, images, build cache, optionally volumes/networks) to prevent disk-full outages | missing |
| Browser terminal | Real-time web terminal into servers and containers from the dashboard | missing |
| Scheduled tasks / cron | Container-level scheduled tasks with full cron syntax, execution logs, and failure notifications (no host-level cron UI — known gap, issue #8500) | missing |
| Teams & permissions | Teams with shared projects and role management, but only two roles (admin/member) — coarse-grained | has |
| Notification channels | Email, Telegram, Discord, Slack, Mattermost, Pushover, generic webhooks — for deploys, backups, and server alerts | partial |
| REST API + tokens | Full API with Bearer tokens (Security > API Tokens), OpenAPI spec, CI/CD deploy triggers; structured audit logging for API mutations added in v4.1 | has |
| MCP server for AI agents | Instance-level MCP with read-only tools and enablement controls (added v4.1, #9862) | has |
| Auto SSL (Let's Encrypt) | Automatic issuance and renewal via Traefik/Caddy proxy; SSL cert expiry tracked and renewed by scheduled jobs; custom certs via /data/coolify/proxy/certs | has |
| Wildcard certificates | Supported but manual: DNS-01 challenge requires hand-editing Traefik labels + DNS provider API env vars, then proxy restart | has |
| Firewall management | Not built-in — hardening guides direct users to manual UFW/iptables (with the Docker-bypasses-UFW gotcha); Cloudflare Tunnel support as an alternative exposure model | missing |
| Service health checks | Per-app healthchecks feeding status (running:healthy), container status monitoring, SSL expiry checks; combined Sentinel push agent + SSH-polling fallback | partial |
| Server monitoring / metrics | Sentinel push agent for container status + system metrics (CPU/mem/disk) with retention settings; no temperature or GPU telemetry | has |
| Zero-downtime rolling deployments | Rolling updates so users see no interruption during deploys | missing |
| Buildpack auto-detection | Nixpacks auto-detects language/build for Node, Python, PHP, Ruby, Go, Rust etc.; Railpack in beta (v4.1); plus Dockerfile, Compose, static builds | partial |
| Remote build servers | Dedicated build-server support to offload builds from production machines | missing |
| Multi-server management | Manage many servers from one dashboard via inbound SSH; Docker Swarm deprecated ahead of v5; no built-in cross-server private network | has |
| Instant public URLs | Auto-generated sslip.io magic domains per app | has |
| Project environments | Prod/staging/preview environments per project with per-environment and shared variables; config diff tracking before redeploy (v4.1) | missing |
| Self-update | Auto-update on configurable cron schedule or manual, incl. service template sync | has |
| NAT/CGNAT traversal | Via Cloudflare Tunnels only — cloudflared must be pre-installed manually; core model still requires inbound SSH to servers | has |

#### Standout features worth studying

- **Scheduled database backups to S3 with retention and restore** — Data safety is table stakes for a production PaaS — users will not host databases on a platform that cannot dump them offsite on a cron with retention; this is the single most-cited must-have in Coolify vs alternatives comparisons.
- **PR preview deployments** — Auto-deploying every pull request to an isolated URL is the core Vercel/Netlify workflow teams expect; it turns the platform from an ops tool into part of the development loop and is a major reason teams pick Coolify.
- **Deep git-provider integration with push-to-deploy** — GitHub App plus self-hosted GitLab/Gitea/Bitbucket support means private repos, webhook auto-deploy on push, and granular repo permissions with zero CI config — without push-to-deploy, RackOps' git deploys remain manual redeploys rather than continuous delivery.
- **Buildpack auto-detection (Nixpacks/Railpack)** — Deploying source code without writing a Dockerfile removes the biggest onboarding hurdle for non-Docker users; Coolify auto-detects Node/Python/PHP/Ruby/Go/Rust builds, dramatically widening the audience.
- **Automated threshold-based Docker disk cleanup** — Full disks are the number-one killer of self-hosted deploy boxes; Coolify prunes images/containers/build cache automatically when disk usage crosses a threshold, preventing the most common production outage class.
- **Browser web terminal into servers and containers** — Debugging without leaving the dashboard (exec into a container, tail a process) eliminates SSH round-trips and is now expected in every modern hosting panel; RackOps has live telemetry but no interactive shell.
- **In-platform scheduled tasks (cron) with logs and failure notifications** — Real apps need cron jobs (queue sweeps, report generation, cache warmup); running them inside the platform with execution history and alerting keeps operational logic visible instead of hidden in crontabs.
- **280+ one-click service catalog** — Catalog breadth is Coolify's top acquisition funnel — self-hosters arrive to deploy n8n or Ghost in one click and stay for app hosting; a small RackOps catalog concedes that funnel entirely.

#### Where RackOps is ahead

- Agent dial-out architecture: agents connect outbound to the server, so fleet machines work behind CGNAT/NAT with zero port forwarding — Coolify requires inbound SSH to every server, and its NAT workaround (Cloudflare Tunnels) needs manual cloudflared installation and a third-party dependency.
- Native WireGuard mesh between machines: Coolify has no built-in cross-server private networking (Swarm is deprecated for v5); cross-server service-to-service traffic must be wired up manually.
- Single static Go binary with embedded dashboard vs Coolify's Laravel/PHP stack requiring Docker, Postgres, Redis, and Horizon with a 2-core/2GB minimum — far lighter footprint and simpler install, upgrade, and backup story.
- Granular RBAC: orgs with fine-grained permissions, invitation links, and API keys carrying user RBAC plus expiry — Coolify ships only admin/member roles, a gap its own users cite as a limitation for larger teams.
- Richer hardware telemetry and fleet visualization: CPU/mem/disk plus temperature and GPU metrics with rack visualization — Coolify's Sentinel covers basic system and container metrics only, with no GPU or thermal awareness.
- Built-in sustained-threshold alert engine with auto-resolve: Coolify's monitoring provides baseline visibility but guides recommend pairing it with external Uptime Kuma/Grafana for real alerting.
- Placement scoring for choosing which machine gets a workload — Coolify deployments require manual server selection with no scheduling intelligence.
- Experimental raft-based HA for the control plane, plus wildcard-domain DNS-verification UX in the dashboard (Coolify wildcard certs require hand-editing Traefik labels), an AI copilot drawer beyond Coolify's read-only MCP, and SMTP-free password reset/support-ticket relay out of the box.

