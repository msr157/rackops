# Stabilization and migration delivery ledger

This is an implementation ledger, not a release announcement. Updated 2026-09-05.

## Baseline and branch reconciliation

`main` already imported the exact `production` tip tree (`5c2803c`) in
`1b7306a`. Merge `9f14a6f` links both histories without discarding subsequent
main work. The working tree's untracked ARCHITECTURE.md is user-owned and
must not be overwritten or published. The GitHub default branch and live
Dokploy configuration have not been changed.

## Implemented locally (not released)

- Reject machine changes atomically in service edits, including save-only
  requests. Disable the selector until recoverable migration exists.
- Enforce existing deployment capability for app edits, lifecycle actions and
  log clearing; enforce machine management for filesystem browsing.
- Scope quick tunnels and domain/expose controls to visible projects and the
  domain-management capability.
- Replace full service event payloads with org-scoped refresh notifications.
- Stop echoing Docker run arguments, which contain app configuration.
- Mask stored environment values and Git tokens in service responses. Preserve
  unchanged environment values through an explicit redacted-value marker.
- Separate Front Door tenant enrollment from vendor administration. A signed
  grant binds an existing tenant, public key, workspace and 24-hour expiry;
  it cannot create tenants, change keys/quotas, suspend or enumerate customers.
- Add platform-specific filesystem telemetry helpers so native Windows test
  builds can compile; Windows product support still means WSL2.
- Require a host-generated, single-use setup proof for the first owner.
  Block unrelated API access before setup completes.
- Encrypt control-plane state with AES-GCM, migrate legacy state on startup,
  and reject missing keys, corrupt snapshots and newer schemas. Add an
  installation-owner snapshot download and a restore-to-new-directory CLI.
  This is a state backup, not a database/volume or complete identity backup.
- Separate task execution from reporting with two bounded workers and
  per-service serialization. Persist delivery assignments before dispatch;
  retain the same task ID across lost responses and control-plane restarts.
- Journal agent task intent and results locally with encryption. An interrupted
  execution reports uncertainty instead of blindly repeating side effects.
- Pause automatic healing for outstanding or uncertain tasks. Add an app
  inspection acknowledgement before resuming recovery. These latest changes
  still require final verification; see the validation checkpoint below.
- Reserve Front Door workspace names uniquely and retry failed registry saves.
- Include both Go modules and an Admin image smoke job in CI. This does not
  itself change or enforce external Dokploy deployment ordering.
- Poll container inventory independently with a bounded deadline. Failed,
  malformed, consumed or stale samples cannot trigger missing-container healing.
  Reject container status updates for services assigned to another node.
- Replicate organizations, invitations, tickets, leads, per-org settings and
  install metadata alongside existing state. Reject unsupported/incomplete
  snapshots, preserve local task ownership and suppress import echoing.
- Authorize live-log start/stop through service/project access. Scope log
  ingestion to the reported node/service and deployment, and broadcast within
  its organization. Isolate agent log follower IDs by service.
- Reauthorize SSE payloads at delivery using current account membership,
  project access and resource ownership. Deleted accounts and demoted roles
  are rechecked for ordinary authenticated API requests. This does not close
  the remaining audit of separately authenticated account endpoints.
- Single-container replacement now verifies ownership, pins the pulled/built
  image ID, retains the stopped prior container with automatic restart disabled,
  and checks runtime/image health before reporting success. Failures after
  interruption require inspection. See DEPLOYMENT_RECOVERY.md for the explicit
  Compose, shared-data, retention and live-runtime acceptance limitations.
- Compose now uses service-specific project names for new stacks and checks
  container ownership for legacy stacks. Lifecycle commands target the stack.
  Deployment validates a separate configuration revision before selecting it,
  replaces empty environments correctly, waits for health, and avoids automatic
  orphan removal. See COMPOSE_SAFETY.md for unresolved compatibility boundaries.
- Remove password-reset credentials from server logs and fleet events. Persist
  token hashes before relay, atomically consume reset credentials, and invalidate
  old sessions/API keys after a completed reset. Update UI guidance to reflect
  possible email-delivery failure. PASSWORD_RECOVERY.md records the pending
  durable relay and offline operator recovery work.

## Remaining release gates

1. Finish route-by-route organization/project authorization and event review,
   including log streams, account revocation and install-wide networking.
2. Complete secret redaction on all APIs, application/build output, assistant
   and diagnostics; complete recovery key lifecycle, rotation and HA storage.
3. Verify bootstrap installation and upgrade journeys across supported hosts.
4. Finish durable operations: cancellation, bounded transport retry policy,
   HA ownership, protocol upgrade tests and failure-injection integration tests.
   Inventory now runs separately; hardware/mesh work and end-to-end heartbeat
   timing still require verification.
5. Immutable deploy revisions, health checks, safe replacement and recovery.
6. Encrypted state/database/volume backups, restoration and schema upgrades.
7. CRM lead ingestion and full support/reset/consent delivery verification.
8. Enforced CI-triggered deployment, Admin image tests, release signing and
   Linux/macOS/WSL installation, update and compatibility checks.
9. Cloud connections, three SDK adapters, sizing/cost preflight, provisioning,
   migration job APIs/UI, data transfer, cutover, cleanup and real cloud tests.
10. Sanitize and publish GitHub Wiki, reconcile product claims, and obtain
    provider acceptance evidence before announcing a migration release.

## Historical audit interpretation

PROJECT_MAP.md and PRODUCT_AUDIT_2026-07.md describe historical snapshots.
Every finding must be checked against current code before closure. Known
updates: session-secret separation is present; pending queue persistence is
present, with acknowledged delivery now implemented locally for upgraded
agents but awaiting full integration validation; stable mesh
allocations and cross-node relay fixes are present. Unsafe machine moves are
blocked by this change set, not replaced by a migration implementation.
The remaining July findings are unverified until individually reproduced or
disproved. No blanket closure of that audit is claimed.

## Acceptance evidence required

Record exact revision, environment, command and outcome. Test denial as well
as successful authorized operations. Cloud acceptance must exercise the same
stateless, PostgreSQL, MySQL and supported Compose fixtures on all providers,
including interrupted transfer, denied credentials, quota/disk failures,
restart/resume, unhealthy destination and post-write recovery behavior.
Never record credentials, reset links or customer data in this ledger.

## Validation checkpoint — 2026-09-05

- Full native Windows Lite tests passed before the latest uncertainty/recovery
  UI changes. Focused agent, server and edge tests passed after initial
  persistent delivery, journal and workspace-reservation implementation.
- Admin backend tests passed after the shared registry changes.
- All three frontend builds passed before the latest operation-recovery UI.
  Two unchanged builds were served from cache. Workspace lint passed with
  16 Lite warnings and no errors; the warnings remain to be reviewed.
- Earlier Linux/WSL Lite tests passed before the newest worker/journal changes.
  Ubuntu has no C compiler available for the race detector. Docker daemon
  access is unavailable, so container smoke tests have not run locally.
- The final formatting/test invocation after recovery changes was blocked by
  automatic approval review because the account usage limit was reached.
  That invocation did not run. Final Go formatting, updated frontend builds,
  backend tests and integration checks remain outstanding.
- No implementation release, production deployment, cloud provisioning,
  GitHub push or Wiki publication has occurred.

### Subsequent verification in the same implementation session

The earlier usage-limit block cleared. Go formatting and the full native Lite
suite now pass, including recovery, inventory, replication and event-access
regressions. All three frontend production builds pass with the recovery UI;
one unchanged website build was cached. Lint passes with 16 Lite warnings and
no errors. Linux/WSL full tests passed after inventory and replication changes,
before the newest SSE/log authorization edits; those newest edits passed the
full native Lite suite. The tracked diff whitespace check passed before the
latest event edits. Race, live container, real provider and full installation
acceptance remain outstanding.

### Single-container replacement checkpoint

Full native Windows and Linux/WSL Lite tests pass after the replacement and
SSE authorization changes. New unit tests cover ownership denial, command
failure before/after interruption, disabling restart before retention, and
healthy/starting/unhealthy/malformed inspection responses. All frontend builds
pass with the maintenance confirmation (two unchanged builds cached); lint
passes with 16 warnings and no errors. The tracked whitespace check passes.
These are unit/build results, not live Docker/Podman recovery certification.

### Compose checkpoint

Full native Lite tests pass after Compose identity/revision changes. The
installed Windows Docker Compose parser executed the environment fixtures
without starting containers; quoting, dollar escaping and rejected ambiguous
values were checked. Linux/WSL full tests also pass before the final lifecycle
status adjustment, which passed targeted native server tests. Acknowledged
start/stop/restart results now update stack status. No frontend changed in
this step. Live stack health, interruption, volume ownership and restore
acceptance remain outstanding; application-data backups are not implemented.

### Password recovery checkpoint

Full native and Linux/WSL Lite tests pass with concurrent reset consumption,
session/API-key revocation and relay-only credential tests. All frontend builds
pass (two unchanged builds cached). Lint passes with the existing 16 warnings
and no errors, and the tracked whitespace check passes. This does not certify
live SMTP delivery, retries or offline operator recovery.

### Recovery continuation — 2026-09-06

Offline account recovery now writes a separate encrypted state directory from
an existing snapshot and key, invalidating old sessions and the recovered
account's API keys. Reset email delivery now has an encrypted persistent queue,
bounded backoff, restart recovery and stale-credential rejection. Relay retries
are at least once: a lost response can duplicate an email containing the same
link. No inbox-delivery guarantee is made. Storage failures prevent sending;
repeated failures can consume the bounded attempt budget.

Native Windows and Linux/WSL full Lite suites passed with the restart, expiry
and offline recovery tests. Live SMTP, race checks, application-data restore,
ordinary password-change session revocation and the three-cloud migration
workflow remain outstanding. These changes are local and have not been deployed.

### CRM and migration checkpoint — 2026-09-06

Enterprise enquiries now have persistent Lite-to-Admin delivery and an
authenticated, paginated Admin inbox with status tracking. Idempotent ingestion
preserves existing submissions and operator updates. See [lead delivery](CRM_LEADS.md)
for rollout order and limitations. Remaining CRM work includes live website,
SMTP and support-flow acceptance, spam handling and retention.

The app page now exposes an initial migration readiness review for all three
target clouds. Source compatibility, authorization and secret exclusion are
tested. It cannot provision or migrate; see [migration status](MIGRATION_STATUS.md)
for explicit implementation boundaries and next milestones.

### AWS implementation checkpoint — 2026-09-06

The earlier approximate overall percentage was not a reliable measure of the
remaining migration work and should not be used as a release-readiness claim.
AWS connection management now uses the official SDK and encrypted credentials.
Internal EC2 provisioning records bind requests to an account, region and stable
operation ID; restart tests reconcile uncertain launches instead of repeating
them. No launch API is enabled. Azure/GCP adapters, pricing/confirmation,
enrollment, application-data backup, transfers, cutover and cleanup are pending.

Full native Lite tests and frontend builds pass; lint has 16 existing warnings.
Linux Lite tests passed before the final permission and organization-removal
guards, which passed native tests. Provider tests use local fakes; live cloud,
browser acceptance and race testing are not certified. Nothing is deployed.

### Three-provider and enrollment continuation — 2026-09-06

Azure/Google SDK adapters and account management now complement AWS. Shared
provisioning records preserve immutable specifications and reconcile uncertain
submissions after restart. Local SDK transport tests check Azure overwrite
protection and Google request IDs/private networking/retained disk settings.
All providers' saved credentials are covered by encryption/revalidation tests.

New single-use organization-bound enrollment issues individual node credentials.
The agent persists its credential encrypted before exchange, and enrolled nodes
cannot be impersonated with the legacy global token. Tests cover failed exchange
retries, expiry, existing-node denial and cross-node report/log rejection.

Native Lite and CRM tests pass. Frontend builds pass (two unchanged builds
cached); lint retains 16 existing warnings. This is not a migration release:
resource/cost review, cloud bootstrap, app-data backup and transfer, cutover,
cleanup and real provider acceptance remain pending. No cloud launch API is
enabled and nothing has been deployed.

### Cloud bootstrap and progress continuation — 2026-09-06

The internal three-provider runner now attaches an immutable encrypted bootstrap
payload with a single-use enrollment grant. Scripts verify a pinned binary,
check prepared-image prerequisites, and use a protected token file for the
systemd agent. AWS dry-run and launch use matching startup payloads.

Organization-scoped operation progress separates VM allocation from node
enrollment and fresh authenticated heartbeats. Expiry, absent identity and
incompatible machines produce actionable states. Expired grants block new
launches without blocking reconciliation of already-created resources. Source
apps are unaffected; agent connection does not authorize migration or cutover.

Resource/cost approval, image/network inspection, fresh-VM acceptance, app-data
backup/restore and transfer, traffic cutover and cleanup remain unfinished. No
public launch endpoint is enabled. Nothing has been deployed.

Validation for this checkpoint: full Lite tests pass on native Windows and
Linux/WSL, including generated-script shell syntax on Linux. Frontend builds
and lint pass from cache with 16 existing warnings. The CLI help path confirms
the token-file option. `git diff --check` passes. These checks do not exercise
a real provider, Docker daemon, fresh VM or application migration.

### AWS resource inspection continuation — 2026-09-06

AWS connections now offer a selected-resource inspection form backed by EC2
read APIs. Checks cover image and size architecture, Linux image metadata,
root disk requirements, accelerator exclusions, subnet availability, VPC
matching and zone offerings. Identity is revalidated before inventory reads;
organization/capability checks and secret exclusion are covered by tests.

Native Windows and Linux Lite suites pass. Frontend build/lint pass with the
same 16 warnings. A local browser smoke fixture exercised compatible results,
disk blockers, stale-review clearing and provider failure messages. This does
not certify live cloud behavior. Azure/GCP inventory, egress/firmware checks,
cost approval, application-data movement, cutover and cleanup remain open.
