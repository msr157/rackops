# Agent task delivery and recovery

Implementation status: local development, not release-certified. See
[the delivery ledger](STABILIZATION.md) for outstanding verification.

## Delivery protocol

Updated agents advertise durable task delivery and their available queue
capacity. The control plane persists each assignment before sending it.
Unacknowledged assignments are resent under the same task ID after a
15-second delivery lease. This is transport retry, not permission to execute
the command again. At most 64 assignments per node are outstanding.

The agent runs two workers and serializes tasks for the same service. Before
executing a task, it writes an encrypted intent record under its data directory
in `task-journal`. It then saves the result before reporting completion. A
retransmitted task returns the saved result. Reusing an ID with different
content is rejected. The server validates the reporting node and service
against its retained assignment before accepting the result.

## Interrupted execution

A receipt with an intent but no completed result means the external command
may have changed the machine. RackOps reports an uncertain outcome instead
of running it again. Task deadlines and agent shutdown can also produce an
uncertain outcome. Automatic healing pauses for that service.

Inspect the actual containers, running processes and application data first.
The app detail page offers **Review and resume recovery**, with a separate
acknowledgement that inspection is complete. Resuming permits automatic
healing, which may redeploy a missing container. It does not restore data or
prove application consistency. Do not acknowledge while the old command is
still running or when the data needs reconciliation.

## Upgrade and backup requirements

Upgrade the control plane before agents. Drain existing work before the
upgrade: legacy agents retain the old delivery behavior, and newer agents
refuse to execute against a server that does not advertise the durable
protocol. A rolling upgrade with active legacy work is not yet certified.

Back up the agent's entire task journal and its key alongside its node
identity. Never delete receipts to force a retry: doing so removes replay
protection. Missing keys fail closed. Receipts and server completion
tombstones currently have no automatic retention cleanup; account for their
growth. Control-plane snapshots do not contain remote agent journals.

## Remaining limits

This implementation does not make arbitrary Docker commands transactional.
It prevents blind replay after uncertain execution; immutable deployment
revisions and data-safe replacement remain separate work.

Transport retries currently have no terminal retry limit. User cancellation,
HA task ownership, complete hardware/mesh/report isolation and supported journal
retention/compaction remain unfinished. Container inventory now runs separately;
failed or stale samples cannot trigger missing-container recovery. The recovery
UI production build and backend regression tests pass. Full process-level
interruption tests and interactive workflow acceptance remain release gates.
