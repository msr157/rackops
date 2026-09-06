# Password recovery status

Reset links are emailed through the Admin relay. They are no longer written
to logs, API responses or the fleet event feed. The public request response
is identical for existing and unknown accounts and does not promise successful
email delivery. Relay failures log an operational message without the reset
credential or recipient.

The control plane persists the reset-token hash and delivery request in its
encrypted state before requesting delivery. Failed relay requests retry in the
support synchronization loop, using a persisted schedule and at most six
attempts. Retries retain the same link; a lost relay response can cause duplicate
email. Expired, superseded, consumed and deleted-account requests are discarded.
Delivery waits when state cannot be persisted; repeated disk failures can exhaust
the bounded attempt budget. The user can request another reset after service is
restored. Relay acceptance does not prove inbox delivery.
Reset credentials expire after one hour. Consumption is rechecked under the
store write lock, so concurrent submissions cannot both succeed. A completed
reset increments the account's session version, removes its API keys and
persists the result before returning a new signed-in session. Old sessions
are rejected on normal API routes, account routes and open event streams.

Existing accounts and sessions begin at version zero; they remain compatible
until that account completes a reset. Ordinary password changes do not yet
implement the same session-revocation behavior.

## Offline operator recovery

The `rackops recover-account` command accepts `--snapshot`, `--key`,
`--data-dir`, `--email` and `--password-stdin`. Supply the replacement password
through standard input from a secure prompt or secret manager, never as a
command-line argument. It requires the original encryption key and an existing
account email; the destination directory must not exist.

Recovery creates a separate encrypted state copy, changes that account's
password, removes its API keys and queued reset email, and rotates the global
session signing secret. All previous browser sessions must sign in again.
The original snapshot is unchanged. Stop the original control plane before
starting the recovered copy, and restore loose identity files separately as
described in [state recovery](STATE_RECOVERY.md). This command does not restore
application volumes or databases.

## Outstanding recovery work

- Inbox delivery status and full SMTP integration remain unverified. An
  unavailable CRM/SMTP service can still prevent email recovery.
- Full SMTP failure/expiry integration, persistence-failure handling and
  account-wide session invalidation for ordinary password changes remain
  release gates.
- Tests cover concurrent single-use enforcement, old-session/API-key rejection,
  relay failures, persisted retry schedules after restart, expiry, superseded
  credentials, storage failures and offline recovery into a new directory.
