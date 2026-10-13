# Changelog

Notable user-visible changes to RackOps Lite are recorded here.

## v0.7.0 - 2026-10-13

### Multi-Cloud Migration Engine

- **Launch workloads on AWS, Azure, and Google Cloud**: Choose target VM sizes (e.g. AWS c6i, Azure Standard_D4s_v5, GCP c2-standard-4) and disk types with live pricing calculations across compute, storage, and outbound transfer egress.
- **9-stage zero-loss migration workflow**: Deterministic state machine orchestrating preflight inspection, source database quiescence, authenticated AES-GCM 256 encryption, snapshot extraction, encrypted transport, destination restore, boot verification, health checks, and zero-stale traffic cutover.
- **Downtime & egress estimation**: Real-time migration timing estimates based on volume size and active dirty-page rates, ensuring cutovers happen within agreed maintenance windows.
- **Guided 5-step React migration wizard**: Interactive in-dashboard wizard with real-time SSE progress streaming and automated rollback safeguards if target health verification fails.

### RackOps Front Door (Self-Hosted Edge Tunnels)

- **Dedicated edge ingress tunnels**: Expose internal workloads and dashboards across NAT and CGNAT without public IP addresses or router port forwarding.
- **Multiplexed TCP/QUIC transport**: Low-latency, high-throughput encrypted edge tunnels with automatic Let's Encrypt TLS certificate issuance.

### Hardened Security & Tenant Isolation

- **AI Co-Pilot organization scoping**: Strict tenant memory and context isolation ensuring AI assistant diagnostics never cross organization boundaries.
- **Offline recovery & command sandboxing**: Resilient fallback operational modes when remote LLM APIs are unreachable, with strict sandboxing preventing runaway executions.
- **Encrypted volume snapshots**: Authenticated AES-256-GCM backups with direct offload support to AWS S3, Azure Blob, Google Cloud Storage, and MinIO.

## v0.6.0 - 2026-08-06

### Adding machines is one paste now

- **The installer grew an `agent` mode.** "Add machine" hands you a single command that installs RackOps *and* everything it needs (Docker, compose, git) on the new machine, joins it to your rack, and registers a boot-time service — Linux, macOS, and WSL2. No more copying binaries around. It preflights that the control plane is actually reachable before installing anything.
- **Join commands always point somewhere reachable.** A dashboard opened on `localhost` used to mint a join command telling the new machine to join itself; loopback now falls back to the server's advertised address.

### Reach your dashboard from anywhere

- **Dashboard quick tunnel** — one switch on the Network page's new *Remote access* section gives the dashboard itself a public `trycloudflare.com` URL: no port forwarding, works behind CGNAT, auto-restarts if the tunnel container dies. Made for home-lab installs the internet can't reach.
- **Dashboard domain** — installs with a public edge can serve the dashboard at a real domain through Traefik (HTTPS included once Let's Encrypt is on). Set it in the edge form.

### Honest guidance

- The **VPS anchor wizard** now tells the truth: install a fresh control plane on the VPS, create your account there, and join this machine from the VPS dashboard — instead of handing out a join command with a token the new VPS would reject. It also warns that apps don't migrate.
- **"Recommended" moved to the option that works.** On a fleet with no public IP, an instant nip.io URL only opens inside your network — the badge now sits on the quick tunnel there, with a plain warning on the instant URL.
- README documents the two things NAT'd and cloud installs trip on: inbound **UDP 51820** for the mesh, and `--advertise` for machines whose primary interface holds a private IP.

### Fixes and polish

- Every **Copy button works on plain-HTTP dashboards** now (LAN/VPS IPs): clipboard writes fall back to the legacy selection path on insecure origins.
- The **real RackOps logo** replaced the leftover template icon in the browser tab, the top nav, the boot screen, and the sign-in card.
- The **activity feed moved off Home** to a dedicated Activity page (sidebar, ⌘K, notification bell) with area filters; Home stays a one-glance status answer.
- **Cross-node domain routing no longer flaps** on multi-machine fleets: relay listeners were being torn down and rebuilt every couple of seconds by non-edge machines' reports.
- The installer labels its printed address "(network)" instead of "(LAN)" — on a VPS that's the public IP and the only one that works from your browser.

## v0.5.0 - 2026-08-06

- RackOps now runs on macOS. The same install one-liner works on Apple Silicon and Intel Macs: it installs the binary, sets up a launchd service (starts on boot, restarts on crash, logs to `/var/log/rackops.log`), and `rackops update` self-updates in place. Docker Desktop or colima provides the container runtime — the installer detects them and explains what to do if neither is present. Linux-only hardware telemetry (thermals, per-device throughput) stays empty on a Mac; everything else works.
- Fixed the app detail page crashing with "Couldn't load this app — TypeError: crypto.randomUUID is not a function" on dashboards served over plain HTTP (any non-localhost LAN address). The API had returned the app fine; generating React row IDs from `crypto.randomUUID`, which browsers only expose on secure origins, crashed the page and the built-in retry could never succeed. All client-side IDs now come from one shared helper that falls back to a non-cryptographic generator on insecure origins.
- Stream runtime and deployment logs in real time. The Logs tab is now a live `docker logs -f` tail that appends as output happens, and build/deploy output (git clone, image build, container start) streams line-by-line as it runs instead of appearing all at once when the deploy finishes.
- Harden session security: sign login tokens with a dedicated per-cluster secret instead of one derived from the agent join token, so a leaked enrollment token can no longer forge dashboard sessions. Existing users are signed out once on upgrade and simply log in again.
- Keep queued deploys and actions across a control-plane restart — pending work is no longer lost if the server restarts before an agent picks it up.
- Preserve each machine's mesh IP across control-plane restarts, so domain routing stays stable instead of briefly breaking after a restart.
- Shut the node agent down cleanly on stop/restart, releasing the mesh interface and relay ports instead of leaking them.
- Recognize a running edge behind a port-forwarding router as a configured public path.
- Make domain DNS copy buttons work on plain-HTTP LAN dashboards as well as secure contexts.
- Add an in-dashboard changelog and feature discovery links.
- Add an owner/admin server restart control to the dashboard with automatic reconnection.

## v0.4.0 - 2026-07-17

- Rebuilt organizations Dokploy-style: an org switcher in the sidebar, 48-hour invitation links that let teammates create their own account straight into the org, per-member roles (owner/admin/member) with granular permissions and per-project access, and real scoping — machines, apps, domains, notification channels, and Copilot settings all belong to one organization and stay invisible to the others. Existing installs migrate automatically into a default organization.
- Added a Profile page: account details, change password, and API keys with optional expiry and last-used tracking. Keys are shown once, act with exactly their owner's role (a viewer's key is read-only), and revoking one is instant. The old Settings tokens tab points there.
- Added self-hosted password reset: "Forgot password?" on the sign-in screen emails a single-use, one-hour reset link from RackOps support — and always prints the same link in the server log (`journalctl -u rackops`), so recovery works even fully offline. Signup and reset now confirm the password, and every password field has a show/hide toggle.
- Added support tickets: open a ticket from the dashboard, and replies from the RackOps support team appear right back in the thread. An anonymous usage summary (version, counts, features used — never names or credentials) travels along so support sees your setup at a glance.
- Added interactive API docs at `/swagger`, generated from the same route table the server registers handlers from — the docs cannot drift from the code — and served entirely from the binary (works airgapped). Authorize with an API key from Profile.
- Added `rackops mcp`: a built-in Model Context Protocol server over stdio so VS Code, Claude, and Cursor can list machines, deploy apps, read logs, and check alerts. Secret env values are redacted before anything reaches an AI context. Profile shows a ready-to-paste config.
- Made Needs Attention truthful: resolved alerts now disappear from the dashboard the moment the condition clears (SSE + fallback polling), instead of lingering until a page reload.
- Locked action buttons for the whole deploy window: start/stop/restart/redeploy/save stay disabled while an app is actually building — not just during the click — and the edge proxy's Disable button waits for an in-flight edge deploy.
- Added plain-language info tips (ⓘ) across every page of the dashboard.
- The dashboard changelog is now reachable at `/changelog` (click the version in the sidebar).
- Restricted the user list to admins and made API-key deletion ownership-checked.

## v0.3.0 - 2026-07-12

- Added organizations, member roles, and organization-owned projects.
- Added revocable API keys, Swagger documentation, and a built-in MCP server.
- Added one-click and CLI updates, account recovery improvements, and support tickets.
- Improved the Needs Attention view, contextual help, and action button states.

## v0.2.0 - 2026-07-12

- Added the one-line Linux/WSL installer and automatic runtime prerequisites.
- Added adoption of existing containers and multi-service template deployments.
- Rebuilt the dashboard around app-first workflows.
- Added self-healing services, complete logs, and correct port mapping.
