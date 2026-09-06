# Single-container deployment recovery

Status: implemented locally; command-sequence unit tests pass, but a live
Docker/Podman acceptance run remains required before release.

Image and Git deployments finish pulling/building, resolve the image ID and
validate mounts before interrupting an existing container. RackOps verifies
that the existing container belongs to the service. It disables that
container's automatic restart policy, stops it and retains it under a name
ending in `-rackops-retained-<identifier>`. It does not delete the container or
its volumes. Retained containers are excluded from active-service inventory.

The replacement uses the resolved image ID. After startup, RackOps checks its
state and, when the image defines a health check, waits up to 60 seconds for
healthy status. Without an image health check, running state is the only
available check; it is not proof of application or database correctness.
Unavailable or malformed inspection results are failures, never success.

If replacement fails after the original container was interrupted, automatic
healing pauses and the operation requires inspection. The deployment log
identifies the retained container. Inspect both versions, stop any unexpected
writers, verify the data and determine which application version can safely
use it before resuming recovery. Never start a retained container alongside
another writer or assume that its shared volumes contain an older snapshot.

## Current boundaries

- Dashboard redeploy actions explicitly confirm immediate maintenance and
  explain the backup requirement. Scheduled windows and an API-enforced
  maintenance plan are not implemented.
- Retention applies to single-container image/Git deployments. Compose
  deployments retain their existing behavior and are not certified by this
  safeguard.
- Containers are retained indefinitely until operator cleanup. Removing the
  app currently does not clean up its retained revisions. Account for disk
  use and inspect recovery needs before removing any retained container.
- Retained containers have automatic restart disabled. Restoring an original
  restart policy is an explicit operator step after data reconciliation.
- Retained container configuration and pinned image IDs are useful recovery
  material, but full immutable revision records, a rollback UI, isolated data
  snapshots and application-level health validation remain unfinished.
