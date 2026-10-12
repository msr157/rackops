# Support ticket delivery and recovery

New Lite tickets persist a random 256-bit polling credential before the first
delivery request. Admin stores only its hash. If Admin receives a ticket but
the response is lost, Lite retries the same ticket, message IDs and credential
after restart. Admin merges messages by ID and checks the credential inside the
same SQL transaction as the merge, including concurrent first claims.

Creation, replies and status edits set a durable pending flag before scheduling
delivery. Acknowledgements must confirm success and the same polling credential.
An acknowledgement of an older local revision cannot clear a newer reply.
Polling does not overwrite local status while its forward is pending. The
dashboard shows **Delivery queued** while an acknowledgement is outstanding;
its normal refresh updates the badge. Ticket responses and event payloads
exclude polling credentials and snapshot message data before serialization.

## Rollout order

Deploy Admin first. `GET /ingest/v1/capabilities` advertises
`ticketClientCredentials: true`. Lite checks that capability before posting a
ticket with a new 64-character credential. When Admin is old or unavailable,
the ticket stays queued locally and no incompatible first claim is sent.
Existing tickets with older Admin-issued credentials continue using them.
Existing legacy tickets whose original credential was already lost are not
automatically recoverable; the new protocol prevents that loss for new tickets.

## Validation and remaining work

Local HTTP/SQLite tests cover repeated delivery, response failure followed by
restart, immutable credentials, message deduplication, concurrent conflicting
claims, stale acknowledgements, ordinary reply polling and older Admin versions.
No messages were sent to the production CRM or an actual mailbox.

Reply polling now requests pages of up to 100 agent messages using a persisted
insertion sequence. Existing rows are backfilled on Admin upgrade; subsequent
inserts receive durable sequence numbers. Equal timestamps, backdated replies,
and restarts do not skip messages. Internal notes are excluded. Lite saves each
page and its cursor together, deduplicates repeated IDs, and polls resolved
tickets too. Older Admin versions return the full thread without a timestamp
filter, with local deduplication. Responses exceeding the 8 MiB client read limit
are rejected without advancing the cursor; per-message limits remain pending.

Local tests also cover pagination across restart, timestamp ties, backdated
insertion, note isolation and atomic local cursor/message persistence.

Outgoing threads now send only unacknowledged customer message IDs, with each
encoded request capped at 192 KiB (below Admin's 256 KiB limit). JSON escaping
and the complete metadata/usage envelope count toward this cap. Each successful
batch saves its acknowledged IDs; remaining messages keep delivery queued for
the next retry cycle. Lost acknowledgements repeat the same IDs safely. Existing
threads start with no acknowledgement ledger and are replayed through Admin's
ID deduplication. The full conversation remains local.

New customer messages are limited to 16 KiB, subjects to 512 bytes, and reply
email addresses are validated before a ticket is queued. Oversized legacy
messages or metadata are retained with an actionable delivery error in the
dashboard; they are never truncated or marked delivered. Tests cover escaped
payload sizes, lost acknowledgements, restart between batches, invalid input,
and preservation of oversized historical messages.

Customer resolution uses a separate durable action with the last fully polled
ticket revision. Admin checks the ticket credential and revision and saves the
action outcome in one transaction. Customer/agent messages and actual status
changes advance that revision; internal notes do not. Replaying an acknowledged
action returns its original outcome without changing status again. A stale
action reports a conflict and requires reviewing current replies before retrying.
Lite shows **Resolution queued** until acknowledgement and preserves the action
across restart. A new customer reply cancels the local resolution request and
reopens the conversation; serialized delivery ensures a reply arriving during
resolution is forwarded afterwards. Older Admin versions without revision
support cannot resolve from Lite and require an update. Full reply pagination
and outgoing delivery must finish before a new resolution can be requested.

Local tests cover credential/install scope, HTTP resolution using a polled
revision, manual status conflicts, newer agent activity, immutable action IDs,
lost acknowledgements, restart, and customer replies during resolution.

Admin now caps reply/note request bodies at 128 KiB and decoded message bodies
at 16 KiB. Cursor-based reply pages also stop at a conservative 6 MiB encoded
budget, so historical escaped messages cannot collectively exceed Lite's
8 MiB read cap. The resolution revision is only included when no agent replies
remain beyond the returned cursor, including byte-limited pages. A single
historical message exceeding the page budget returns 422 without advancing;
Lite shows a sync error and disables resolution until synchronization succeeds.
The original Admin message remains intact. Updated Lite automatically retries
this case using `chunk=1&offset=...` on the credential-checked reply endpoint.
Admin returns at most 256 KiB of the complete JSON message as base64 plus its
sequence, total encoded length and SHA-256. Lite saves partial data in encrypted
state and resumes by offset after restart. Only a complete verified agent message
is added to the conversation and advances the cursor; partial data stays out of
API responses and events. Resolution stays blocked until normal polling confirms
the remaining backlog has been delivered. Automatic recovery supports encoded
messages up to 64 MiB; larger records require operator handling and are never
deleted or truncated to advance the cursor. Older Admin versions must be upgraded
to serve chunks. Poll cadence bounds transfer throughput to one chunk per sync.

Live Admin/Lite rollout, SMTP/support acceptance and operator handling above 64 MiB
still need completion. These fixes do not certify all CRM flows.

## Operator export for oversized historical replies

For replies above the 64 MiB automatic recovery limit, use the Admin host or an
access-controlled copy of its SQLite database. Stop Admin before copying crm.db,
or use SQLite's online backup facility so WAL data is included. Do not copy only
the main database file while writes are active.

Run with the exact install, ticket and agent-message IDs:

```sh
rackops-admin export-reply --data-dir /private/admin-backup --install INSTALL_ID --ticket TICKET_ID --message MESSAGE_ID --output /private/reply.txt
```

The command opens the existing database read-only, writes the body in 256 KiB
chunks and prints its SHA-256. It refuses an existing output path, does not export
internal notes, and never changes the original message, ticket or delivery cursor.
On Unix the new file is mode 0600; on Windows use a directory with restricted ACLs.
Interrupted or failed exports remove their partial output. Preserve the hash with
an operator record containing the exact IDs. No message body or credential is
printed to logs.

Review the exported material privately and agree a separate authorized delivery
channel with the customer. Do not place it in public Wiki/issues or manually skip
Lite's reply cursor: that would silently lose history. The original oversized
reply remains queued with a visible error. This export is operator recovery access,
not a bypass of automatic delivery limits or proof of successful customer delivery.
