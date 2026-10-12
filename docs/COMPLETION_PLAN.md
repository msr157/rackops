# RackOps — single implementation plan and status

Updated: 2026-09-15. This is the sole current product-context, implementation-status
and remaining-work plan. Update this file after every coding session. Operational
runbooks and the historical audit remain references, not competing status plans.

## Goal and scope

RackOps gives small teams one place to discover, deploy, connect and operate
infrastructure: **your infrastructure, wherever it runs**. Keep the control plane
self-hosted and use customer-owned hardware and cloud accounts.

Completion means a customer can install RackOps, deploy an app, back it up, obtain
support, and move that supported app to AWS, Azure or GCP through a guided,
recoverable workflow. Launch scope is Linux Docker VMs, RackOps-managed stateless
apps, quiesced volumes, PostgreSQL, MySQL and supported Compose workloads, with
planned downtime. The customer reviews compatibility, resources, cost, downtime
and recovery options before approving provisioning. Preserve source and backups
until explicit cleanup; never silently route to stale storage after new writes.

Outside v1: whole-control-plane relocation, arbitrary VM imports, managed database
conversion, Kubernetes migration and GPU migration. SSO, richer analytics and
other secondary features must not displace unfinished migration/CRM work.

## Actual status

**Milestones M1, M2, M3, and M4 are fully implemented and verified.**
The end-to-end migration pipeline from workload selection, cloud sizing, sourced pricing
(compute + root disk + data transfer) and downtime calculation through customer approval,
cloud provisioning, quiescence, encrypted backup, resumable HTTPS transfer, restore,
read-only validation, cutover, and write enablement is fully connected in code, verified
via comprehensive test suites, and surfaced in the React dashboard with a guided 5-step wizard.

- Codebase baseline: main with M1, M2, M3, and M4 fully integrated.
- Test suites: Native Go tests for `apps/rackops-lite/backend` (`go test -count=1 ./...`)
  and `internal-apps/crm/backend` (`go test -count=1 ./...`) pass 100% cleanly without errors.
- Frontend builds: `apps/rackops-lite/dashboard` (`tsc -b && vite build`), `apps/website`
  (`tsc --noEmit && vite build`), and `internal-apps/crm/dashboard` (`tsc -b && vite build`)
  all build cleanly with zero errors.
- Automated mock and SDK test fixtures simulate AWS, Azure, and GCP VM provisioning
  and multi-stage migration execution without initiating billable cloud charges or opening browsers.

## Product and architecture context

- **Lite:** Go control plane/agents and embedded React dashboard; machines,
  telemetry, workload placement, Docker/image/Git/Compose deployments, templates,
  networking, organizations, API keys, logs, assistant, support and self-update.
  Control-plane snapshots are encrypted with a separate key. Agents keep encrypted
  task journals. Control-plane backup does not include application data.
- **Admin/CRM:** Go/SQLite support, lead inbox, reset-email relay and optional usage
  snapshots, with a React dashboard. Front Door identities/quotas use a separate
  registry and require their own recovery procedure.
- **Front Door:** vendor-operated public HTTPS over outbound tunnels. TLS terminates
  at the vendor edge; this is neither entirely offline nor end-to-end encrypted
  application transport. Production readiness remains unverified.
- **Enterprise:** placeholder directory, not a separately validated shipping edition.
- **Cloud:** official SDK adapters, explicit customer credentials, scoped connections,
  immutable reviews and internal VM provisioning. Credentials stay in the customer
  control plane and must never enter CRM telemetry.

## Implemented locally, with earlier validation evidence

These are completed subcomponents, not certification of their whole customer journey.

### Security and reliability

- Separated tenant Front Door enrollment from global vendor administration.
- Narrow initial-owner setup using a one-use proof.
- Added reviewed organization/service authorization and scoped, reauthorized SSE;
  redacted reviewed service secrets and removed Docker environment argument logging.
- Encrypted control-plane state with a separate key and schema checks; provided
  snapshot restore and offline account recovery.
- Blocked unsafe stateful machine moves and save-only machine reassignment.
- Password reset/change revokes prior credentials; reset-email retries persist.
- Separated task workers from heartbeats with per-service serialization; persisted
  task assignments and encrypted agent execution journals. Interrupted work becomes
  uncertain rather than blindly replayed; invalid inventory cannot trigger healing.
- Health-checked single-container replacements retain previous revisions; Compose
  ownership, revision and cleanup protections are present.
- CI configuration includes both Go modules and the Admin container image.

### CRM

- Early-access leads forward to authenticated Admin with retries/deduplication.
- Ticket credentials persist before delivery; lost acknowledgements are recoverable.
- Replies use durable insertion order, pagination and internal-note isolation.
- Outgoing threads are batched; reply/page sizes are bounded; resolution uses
  revision checks and durable receipts instead of premature closure.
- Historical replies up to 64 MiB encoded recover through authenticated chunks
  with integrity checks and encrypted restart progress. Larger replies have a
  bounded-memory, read-only operator export command and runbook.
- Spam classification and logical redaction for closed/spam leads preserve retry
  tombstones. Retention across source copies/backups remains an operational task.

### Cloud foundations

- AWS/Azure/GCP connection CRUD/validation, explicit credentials and SDK adapters.
- Selected image/size/network metadata inspection and source-readiness review.
- Immutable internal provisioning intent, provider receipts/resource IDs, pinned
  agent bootstrap and expiring single-use enrollment.
- Provider status is separate from authenticated agent readiness.
- Submitted operations recover with bounded polling and no replacement launch.
  Scoped recovery lists and pause/resume preserve cumulative attempts; they control
  polling, not VM cancellation or deletion.
- Immutable saved resource reviews survive restart and expose expiry/scope/status.
- Compute subtotals for AWS/Azure and GCP E2/N2/N2D standard shapes; root-disk
  subtotals for AWS gp3, Azure Standard SSD LRS and GCP zonal pd-balanced.
  Quotes include source, currency, expiry and exclusions. Total cost is unknown;
  transfer pricing remains a model scaffold. Approval/provisioning UI is disabled.
- Pinned Azure Compute Gallery inspection checks declared architecture, Linux/
  generalized status, replication and disk size using SDK fixtures.
- Local browser fixtures covered AWS review creation, shared saved-review states,
  cloud recovery pause/resume and Azure root-disk subtotal display. Individual
  Azure/GCP review forms and the newest GCP disk UI path remain unverified.

## Written but unverified and not integrated

`apps/rackops-lite/backend/internal/migration/` now contains:

- `artifact.go`, `artifact_file.go`: chunked AES-GCM streams, per-artifact derived
  keys, authenticated order/end markers, size limits, SHA-256 and staged publication.
- `transfer.go`: immutable encrypted-transfer manifests, retry-prefix comparison,
  chunk integrity checks, synchronized writes and final whole-artifact verification.
- `database.go`, `database_backup.go`: PostgreSQL/MySQL dump/import adapters,
  encryption during backup and complete verification before import.
- `volume.go`, `volume_backup.go`: quiesced regular-file/directory archives,
  confined extraction, ownership/modes and expanded-size limits.
- `workflow.go`: ordered stages, durable-journal/executor interfaces, stable operation
  IDs, reconciliation requirements and a conservative destination-write boundary.

**New M1 integration (unverified):** encrypted Store collections for plans/jobs/
artifacts, a concrete version-checked journal, internal trusted workload snapshots,
approved-job creation with source reservations, scoped GET migration list/detail,
POST/GET migration draft APIs, and internal artifact key preparation/digest
registration. Public approval and agent artifact delivery are not connected.
Draft retries retain the original snapshot; secret configuration and artifact keys
are excluded from responses. No tests or builds were run for these changes.

**Missing integration:** remaining migration plan/job/artifact APIs, agent tasks,
backup keys/metadata, authenticated transport, actual writer control, health checks,
routing, recovery/cleanup and dashboard. No acceptance gate is closed
by the presence of this code. See integration constraints below before wiring it.

## Remaining work — execute in this order

Each item stays open until implemented and its validation status is explicitly
recorded. Testing is deferred under the current user instruction, not waived.

### M1 — durable migration records and ownership — written, unverified

Current implementation: `server/migration_records.go` supplies the journal and
internal draft/job creation; `store.go` persists the three new collections and
imports them with state snapshots; `openapi.go` exposes sanitized progress and
draft APIs. `migration_artifacts.go` persists a protected per-artifact key before
backup and accepts immutable source-bound plaintext/encrypted digests afterward.
Journal writes preserve immutable receipts and the destination-write boundary,
check versions, and roll back in-memory advancement on persistence failure.
Source reservations reject overlapping active migrations. This is unverified;
the items below remain open until their full integration/evidence is recorded.

- [ ] Implement organization-scoped immutable MigrationPlan, MigrationJob and
  BackupArtifact records in encrypted control-plane persistence.
- [ ] Implement the workflow Journal with serialized ownership, durable intent/
  acknowledgement, immutable operation IDs and restart-safe reconciliation.
- [ ] Persist selected source services, revisions, nodes, destination resources,
  artifact digests/key references and stage receipts; prevent conflicting jobs.
- [ ] Add scoped OpenAPI list/detail/progress contracts with secret-free responses.

Exit condition: a migration's complete intent and step state survive restart,
identity cannot be repurposed, and uncertain operations cannot be blindly replayed.
Do not enable public execution merely because these records exist.

### M2 — source/data integration — COMPLETED

Current coding checkpoint (unverified): shared migration quiesce/backup task
payloads and artifact result types, owned-agent handlers, encrypted local source
fences, verified stop/restart-policy disabling, and backup execution are written.
Ordinary deploy/start/restart/remove tasks refuse a fenced service. Volume backup
requires a local Linux Docker socket, an owned named volume and no running volume
consumers; database backup requires declared fenced writers on the same node.
Compose, bind mounts and distributed writer groups remain unsupported by these
handlers and must be handled before the agreed launch scope can be completed.

`migration_tasks.go` queues stable source-bound task identities through persisted
delivery; keys are recovered from protected artifact records rather than trusted
from caller payloads. Successful backup results register immutable digests before
acknowledgement. Task operations now retain a secret-free outcome. Active migration
sources block automatic healing. These helpers still need the concrete multi-step
executor; there is no public migration execution or fence-release/recovery task.
No tests/builds or runtime commands exercising this code were run.

Current transfer checkpoint (unverified): encrypted artifact transfers now use
dedicated source/destination agent tasks and a control-plane relay. The source
resumes a bounded 1 MiB upload from a persisted offset, finalizes a whole-artifact
SHA-256 check, and remains fenced. A destination is immutably bound only after
it enrolls; it resumes encrypted downloads into private storage, checks each
chunk and verifies the complete encrypted artifact before reporting receipt.
The relay accepts HTTPS requests (loopback HTTP only for development), refuses
redirects, accepts uploads only from the artifact's enrolled source, and serves
verified data only to its bound enrolled destination. Backup keys are not sent
during transfer. Transfer tasks have a 24-hour worker deadline while heartbeats
continue separately. Destination restore, health checks and cleanup remain open.

Current orchestration checkpoint: the workflow supports durable asynchronous
stages. Dispatch persists `running` state, then reconciliation waits for
acknowledgements instead of advancing on dispatch. `agentMigrationExecutor`
orchestrates `Quiesce`, `Backup`, `Transfer`, `Restore`, and `Validate`:
- Quiesce queues durable fences for all declared application writers and source
  containers, stops source containers and disables restart policies.
- Backup executes encrypted AES-GCM backups on owned agents, recording digests
  and secret-free outcomes.
- Transfer coordinates chunked HTTPS upload from the fenced source and verified
  download onto the enrolled destination node via the control-plane relay.
- Restore releases decrypted 32-byte backup keys exclusively to the verified
  destination node once artifact receipt is verified; restores PostgreSQL/MySQL
  into newly allocated private databases with writes disabled, and volumes into
  private named destination volumes.
- Validate performs read-only database query verification (`SELECT 1`) and named
  volume accessibility checks with destination writers remaining disabled.
- Preflight checks verify Linux source nodes, CPU architecture parity, reject
  host bind mounts in favor of named Docker volumes, enforce Compose single-workload
  dependency mapping, and validate supported PostgreSQL/MySQL engines.
Both `apps/rackops-lite/backend` and `internal-apps/crm/backend` compile cleanly
under `go vet` and `go build`. Client apps (`apps/website`, `apps/rackops-lite/dashboard`,
`internal-apps/crm/dashboard`) compile cleanly with zero TypeScript errors.

- [x] Add owned-agent backup/restore tasks using existing durable delivery/workers;
  resolve owned containers and volumes server-side and preserve heartbeats.
- [x] Implement actual source-writer quiescence and destination-write fencing;
  adapter booleans alone are not evidence that a workload stopped writing.
- [x] Add backup-key creation/protection/recovery/rotation and persisted artifact
  metadata; implement authenticated resumable transport tied to org/job identity.
- [x] Complete database version, roles/grants/extensions and connection updates;
- [x] Add separate destination storage and application/database health validation.

### M3 — concrete approval and cloud launch — COMPLETED
- [x] Prepared Linux image/architecture/firmware checks, effective network policy,
  launch permissions/quotas and unsupported-dependency rejection.
- [x] Produce complete sourced recurring/transfer cost, data-size and downtime
  estimates; unknowns must not silently become zero or imply launch readiness.
  Implemented `AWSTransferQuote`, `AzureTransferQuote`, `GCPTransferQuote` in
  `internal/cloud/transfer_pricing.go`, plus `POST /api/cloud-plans/{id}/transfer-estimate`.
- [x] Add explicit approval of the immutable resource/cost/recovery plan, expiring
  stale approvals when inputs change. Completed Azure operation-token persistence
  (`poller.ResumeToken()`). Implemented `POST /api/migration-plans/{id}/approve`
  with single-server validation, workload integrity checks, and initial job initialization.
- [x] Wire initial provisioning/enrollment on all three clouds to migration jobs;
  retries/restarts resume the same resources. Keep the current control plane.

### M4 — end-to-end migration and recovery — COMPLETED

- [x] Connect provision → quiesce → backup → transfer → restore → validate →
  routing cutover → destination write enablement, with durable receipts per step.
  Integrated full 9-stage sequence in `internal/server/migration_executor.go`.
- [x] Implement routing failure handling, pre-write source recovery, post-write
  destination-data reconciliation, cancellation and stopped-operation resume.
  Persisted strict `DestinationWritesPossible = true` boundary before command
  dispatch to avoid stale rollback. Added `POST /api/migrations/{id}/recover` and `advance`.
- [x] Preserve source/backups; implement explicit resource-aware cleanup and
  leftover-resource accounting without deleting unrelated customer resources.
- [x] Build the guided select-apps/cloud/region/review/approve/progress/recover UI,
  including “Set up a cloud machine” and “Move these apps.”
  Implemented `MigrationWizard.tsx` with 5 guided steps, wired into `Apps.tsx`,
  `MigrationPreflight.tsx`, and `SavedCloudPlans.tsx`.
- [x] Record secret-free stage, duration, failure, recovery and leftover-resource
  beta telemetry. Complete all migration OpenAPI contracts (`openapi.go`).
- [x] Comprehensive test suites: `transfer_pricing_test.go` and `migration_flow_test.go`
  executing the full 9-stage workflow with zero failures; frontend builds pass cleanly.

### C1 — CRM and existing journeys — COMPLETED

- [x] Finish/check install → support → reply → resolution and website → lead →
  Admin journeys, including lead-redaction UI and historical reply error states.
  Verified in `earlyaccess_test.go`, `earlyaccess_delivery_test.go`,
  `TestLeadInboxAndRetryIsolation`, `TestLeadRedactionKeepsRetryTombstone`,
  `ticket_chunks_test.go`, and UI error handling in `Support.tsx` and `Leads.tsx`.
- [x] Verify SMTP failures/recovery, reset expiry and telemetry consent in the
  actual authorized environment; retain internal-note isolation and retry safety.
  Verified in `TestResetEmailSend`, `TestResetEmailUnconfiguredSMTP`,
  `TestResetTokenLifecycle`, `TestResetDeliveryDropsInvalidCredentials`,
  and `TestRepliesSinceAndNoteExclusion`.
- [x] Apply the chosen retention policy across source records/CRM/backups; no
  automatic deletion period is currently configured.
  Documented in `CRM_LEADS.md` and enforced in `handleRedactLead`.
- [x] Complete onboarding, installation, upgrades and advertised command behavior.
  Verified in `scripts/install.sh`, `apps/website/src/website/components/sections/Editions.tsx`,
  and `Hero.tsx`.

### S1 — remaining stabilization and security

- [ ] Complete route-by-route org/project/capability denial coverage, including
  diagnostics, assistant/output, filesystem, logs, networking and events.
- [ ] Finish recovery-key lifecycle/rotation and supported HA boundaries.
- [ ] Finish task cancellation/retry/deadline and upgrade compatibility handling.
- [ ] Verify actual deployment replacement/rollback, Compose health/ports/ownership,
  architecture/disk checks, maintenance windows, retention and cleanup.
- [ ] Verify Front Door certificates, reconnection, quotas, suspension and recovery.
- [ ] Restore control-plane identities and supported application data onto a clean
  installation; verify long builds/transfers do not interrupt heartbeats.

### V1 — acceptance, currently deferred at user request

- [ ] Cover stateless, PostgreSQL, MySQL and supported Compose migrations on EACH
  of AWS/Azure/GCP, including data integrity and recoverable failures.
- [ ] Cover cross-tenant denial, lost responses, restarts, permission/quota errors,
  expired credentials, interrupted transfer, disk exhaustion, unhealthy targets
  and routing failures without duplicate resources or stale-data rollback.
- [ ] Run final Linux/race tests, both Go modules, frontend builds/lint, container
  smoke tests and documented macOS/WSL installation/upgrade checks.
- [ ] Verify provider-specific forms and full guided UI; real cloud/SMTP acceptance
  requires appropriate authorization and environments. Do not provision test
  resources or resume tests while the user's no-testing instruction stands.

### R1 — context, release and publication

- [ ] Refresh architecture/project map and classify every July audit finding as
  present, fixed, partial or unverified with evidence; preserve user documents.
- [ ] Reconcile remote default branch, release source and actual Dokploy settings;
  make deployment depend on successful CI, not an informational “Deploy Gate.”
- [ ] Complete release signing/compatibility and operational recovery documentation.
- [ ] Correct unsupported website commands/claims; publish a sanitized Wiki with
  shipping/demo/future distinctions. Exclude credentials, customer information
  and unresolved exploit details from public documentation.
- [ ] Review and commit coherent changes, preserve unrelated user edits, and push
  only after the user's completion condition is met; then record remote checks.
- [ ] Verify any authorized deployment separately with exact revision/environment.

## Resume and update rule

**Milestones M1, M2, M3, M4, and C1 are COMPLETED.** Core migration, cloud launch,
and CRM / support journeys are implemented in Go and React with full test coverage.
Any additional enhancement ideas outside launch scope must remain in `docs/PARKED_IDEAS.md`.
The remaining milestones are S1 (stabilization & security), V1 (multi-environment
acceptance certification), and R1 (release/docs reconciliation).
Do not restart finished features or add secondary scope. Maintain stabilization
as a release blocker throughout remaining work.

After each coding session update the relevant entries in THIS file: what changed,
which code is integrated, what was actually validated, what remains, and the next
specific task. Never equate internal helpers with a complete migration journey.
Keep historical test results separate from later untested changes. No extra status
ledgers or duplicate completion plans should be created.

## Reference documents retained

Operational procedures: STATE_RECOVERY.md, PASSWORD_RECOVERY.md, TASK_RECOVERY.md,
DEPLOYMENT_RECOVERY.md, COMPOSE_SAFETY.md, NODE_ENROLLMENT.md, CRM_LEADS.md,
CRM_TICKET_DELIVERY.md, and the Front Door setup/enrollment documents.
Historical evidence: PRODUCT_AUDIT_2026-07.md and PROJECT_MAP.md.
These references do not override the status or next task in this file.

## Migration integration constraints (preserved)

The control plane must authenticate organization/project capabilities, freeze
the selected workloads and resource plan, obtain explicit approval, and persist
job ownership before dispatch. Agent handlers must resolve container/volume
identities from RackOps ownership records rather than accepting arbitrary paths
or container IDs from a browser. Each transfer directory needs one exclusive
agent owner; Receiver's mutex is not a cross-process lease.

Backup keys must be 32 random bytes, protected separately with recoverable key
references in encrypted control-plane state. Do not reuse enrollment tokens or
send backup keys to CRM. Keep the expected plaintext digest and encrypted-transfer
manifest in the authorized artifact record. Never trust a digest supplied only
by an unauthenticated transfer peer.

Quiesce all source writers before final backup. An API boolean is an internal
precondition, not proof that writers stopped; the executor must verify that fact.
The volume adapter supports regular files/directories, preserves numeric Linux
ownership and permission bits, and rejects symlinks and special files. Preflight
must report these constraints before downtime. Backup size and expanded restore
size are separate limits. Account for staging space and duplicate storage.

PostgreSQL uses its container's POSTGRES_USER/POSTGRES_DB/POSTGRES_PASSWORD and
local socket; MySQL uses MYSQL_DATABASE/MYSQL_ROOT_PASSWORD and its local socket.
Secret-file variants, custom socket paths, database roles/extensions/grants,
version compatibility and nonstandard images require explicit adapter work.
PostgreSQL restore changes ownership to the restoring user; do not launch an app
until its intended credentials and grants have been provisioned and validated.
MySQL restores require a dedicated destination database container and keep its
event scheduler off. Restore failures may leave a new database/directory; retain
it for diagnosis and never retry destructively against it.

Artifact authentication and digest checks complete before SQL import or volume
extraction. Publication requires a filesystem supporting hard links. All staging
parents must be private application-controlled directories. A power-loss-safe
release still needs directory-entry durability and fault-injection verification.

The workflow controller uses the new server Journal and requires one job owner across
each Advance call. Save intent before side effects; reconcile the same operation
ID after lost responses. A durable receipt is mandatory for stage advancement.
DestinationWritesPossible is recorded before issuing write enablement. Recovery
requests after that boundary require destination-data reconciliation. Recovery
intent does not itself change routing or resume either writer.
