# Scoped agent enrollment

The new enrollment flow is available in the local implementation; existing
join commands continue to use the legacy path until the installer/release
workflow is updated. The internal cloud runner now supplies enrollment through
encrypted, immutable startup scripts; a customer-facing launch flow is not yet
available.

An authenticated organization member with machine-management permission can
POST `/api/node-enrollments`. The response contains an organization-bound token
valid for one hour. It must be handled as a secret. This endpoint currently
requires a single-server control plane.

An agent launched with that `rkenroll_` token generates its own random node
credential and writes it encrypted under its task-journal directory before
exchanging the token at `/api/agent/enroll`. Remote enrollment requires HTTPS;
HTTP is allowed only for loopback development. Redirects are refused. The
server stores hashes, binds the new node to the issuing organization, and
persists the binding before acknowledging enrollment.

One grant cannot claim another node or an existing machine. A retry proving
the exact already-enrolled node credential is accepted after token expiry,
allowing recovery of a lost exchange response. Preserve the node ID, encrypted
credential and journal key together across restarts. Missing or damaged files
fail closed rather than silently replacing the identity.

Enrolled nodes use their individual credential for telemetry and log ingestion.
The global join token cannot impersonate them. Their credential cannot act as
another node or recreate a removed node. Legacy nodes retain their existing
authentication behavior; this is not a full replacement of installation-wide
agent authentication or a complete credential-rotation workflow.

Tests cover persisted client identity, exchange retries, expiry, attempts to
claim existing nodes, organization binding, cross-node denial and deletion.
Cloud scripts verify a pinned binary's SHA-256, check Linux image prerequisites,
and run the agent as a systemd service using a root-only `--token-file` so the
proof is absent from process arguments. The script contains the short-lived
proof and must be treated as secret provider metadata. The control plane stores
that payload encrypted; it is excluded from operation progress responses.

Tests cover provider startup payloads, restart-stable encrypted scripts, shell
syntax and operation progress derived from the enrolled node. Fresh-host
installation, rotation and real cloud enrollment acceptance remain outstanding.
