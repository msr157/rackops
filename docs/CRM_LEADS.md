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
