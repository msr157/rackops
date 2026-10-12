# Enterprise lead delivery

Lite retains the existing public website submission endpoint and persists
accepted enquiries in encrypted state. Its support synchronization loop forwards
up to 20 pending enquiries per cycle after ticket synchronization, using the
installation ID and original lead ID. Delivery is independent of optional usage
telemetry because it fulfils an explicit enterprise enquiry. Only name, email,
industry, notes and the submission identifiers are forwarded.

Deploy Admin before Lite: Admin must expose `POST /ingest/v1/leads`. Its SQLite
migration creates the lead inbox automatically. Missing/unavailable Admin leaves
enquiries queued in Lite; restarts preserve them. Retries cannot overwrite a
previously received lead or its operator-assigned status. A lost response can
repeat delivery safely. Old pending enquiries are forwarded too; oversized or
invalid legacy records may need operator correction before delivery succeeds.

Admin's **Enterprise leads** page supports new, contacted, qualified and closed
statuses, refresh and pagination. Inbox and status endpoints require Admin
authentication. Public ingestion is rate-limited and body-limited; identities
and contact details remain unverified. Status changes do not send messages.

Tests exercise unauthenticated denial, installation-specific updates, repeated
delivery, preservation of operator changes, and Lite retry after restart using
a local relay. Production routing, retention policies, spam handling and a live
website-to-Admin acceptance run remain outstanding. CRM support and mail delivery
still have separate release gates; a lead inbox does not complete all CRM work.

## Spam and retention operations

Admin can mark an enquiry as spam without contacting the sender. All leads still
have the existing ingest body/field limits and per-IP throttling; contact details
are unverified. Spam classification is operator-driven, not an automatic filter.

When the company's retention policy permits, an administrator can remove contact
details from a closed or spam record. The dashboard confirms the exact enquiry.
Removal clears name, email, industry and notes in the active record and retains
only the install/submission IDs, received date and redacted status. Retries of that
same submission are acknowledged without recreating its private fields. Redacted
records cannot be reopened through the status API. A changed lead requires refresh.

No retention duration or automatic deletion schedule is enabled by default: the
operator must apply the company's chosen policy. This is logical redaction of the
active CRM record, not secure erasure of SQLite free pages, WAL, snapshots, source
website/Lite records or backups. Apply matching access and expiry policies to those
copies. Restoring an older backup may restore private fields; reapply the recorded
redactions before reopening ingestion. Keep retention decisions in a restricted
operator record keyed by submission IDs, without duplicating removed contact data.
