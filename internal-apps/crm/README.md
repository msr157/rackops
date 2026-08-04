# RackOps CRM

The vendor-side operations console. Customers run self-hosted RackOps Lite
instances behind NAT; this app is the piece of RackOps-the-company they talk
to. It does three things:

1. **Support tickets** — receives tickets forwarded by customer instances,
   lets the support team answer them, and hands replies back when instances
   poll (all traffic is instance → CRM; nothing dials into customer networks).
2. **Password-reset relay** — sends reset emails from
   `support@rackops.in` on behalf of instances that have no SMTP of their own.
3. **Usage analytics** — stores opt-in usage snapshots and turns them into
   version distribution and "hero feature" rankings.

```
internal-apps/crm/
  backend/     Go module github.com/rackops/crm — single `crm` binary
  dashboard/   React 19 + Vite operator console (embedded into the binary)
  Dockerfile   Multi-stage image, built from the repo root
```

## Environment

| Variable             | Required | Default                                   | Purpose                          |
| -------------------- | -------- | ----------------------------------------- | -------------------------------- |
| `CRM_ADMIN_EMAIL`    | yes      | —                                         | Admin login email                |
| `CRM_ADMIN_PASSWORD` | yes      | —                                         | Admin login password (bcrypt-compared at login) |
| `SMTP_HOST`          | no       | —                                         | Reset-email relay host; relay returns 503 when unset |
| `SMTP_PORT`          | no       | `587`                                     | SMTP port (PLAIN auth + STARTTLS) |
| `SMTP_USERNAME`      | no       | —                                         | SMTP username                    |
| `SMTP_PASSWORD`      | no       | —                                         | SMTP password                    |
| `SMTP_FROM`          | no       | `RackOps Support <support@rackops.in>`    | From header; Reply-To is always `support@rackops.in` |

State lives in `--data-dir` (default `~/.rackops-crm`): a WAL-mode SQLite
database (`crm.db`, pure-Go driver — no cgo) and the JWT session secret
(`session.secret`, random 32 bytes created on first start).

## Run / develop / build

```sh
# Backend (API on :8091)
cd internal-apps/crm/backend
CRM_ADMIN_EMAIL=ops@rackops.in CRM_ADMIN_PASSWORD=... go run ./cmd/crm serve

# Dashboard with HMR (proxies /api and /ingest to localhost:8091)
pnpm --filter @rackops/crm-dashboard dev

# Production build: dashboard → Go embed dir → single binary
pnpm --filter @rackops/crm-dashboard build
cd internal-apps/crm/backend && go build -o crm ./cmd/crm

# Tests
cd internal-apps/crm/backend && go vet ./... && go test ./...
```

## Deploy

```sh
# From the monorepo root:
docker build -f internal-apps/crm/Dockerfile -t rackops-crm .
docker run -d --name rackops-crm \
  -p 8091:8091 -v crm-data:/data \
  -e CRM_ADMIN_EMAIL=ops@rackops.in \
  -e CRM_ADMIN_PASSWORD=... \
  -e SMTP_HOST=smtp.example.com -e SMTP_USERNAME=... -e SMTP_PASSWORD=... \
  rackops-crm
```

`/healthz` is the liveness endpoint (used by the image's `HEALTHCHECK`).
When running behind a reverse proxy, the per-IP rate limits honor
`X-Forwarded-For`.

## Ingest API (public — called by customer instances)

All endpoints are `instance → CRM` HTTPS; instances behind NAT poll for
replies. Hardening on every `/ingest/*` route: per-IP token bucket
(60 req/min, burst 20), 256 KB JSON body cap, strict enum/format validation,
opaque error bodies.

### `POST /ingest/v1/tickets` — upsert-forward a ticket

```jsonc
{
  "installId": "uuid",           // instance's stable UUID (required, validated)
  "installName": "acme-lab",
  "version": "0.4.0",
  "pollSecret": "",              // empty on first create; required afterwards
  "ticket": {
    "id": "tk_abc",              // instance-minted; key is (id, installId)
    "subject": "Deploys hang",
    "category": "general|deploys|networking|updates|billing|bug",
    "severity": "low|normal|high|urgent",
    "status": "open|pending|resolved",
    "email": "customer@example.com",
    "createdAt": "RFC3339",
    "messages": [                // only author "user" is accepted
      { "id": "m1", "author": "user", "name": "Casey", "body": "…", "at": "RFC3339" }
    ]
  },
  "usage": { /* UsageSnapshot, optional */ }
}
```

`200 {"ok":true,"pollSecret":"<40-hex>"}` — the first forward of a
`(ticket.id, installId)` pair creates the ticket and mints the poll secret
(store it; only its SHA-256 is kept server-side). Later forwards must present
it or get `403`. Messages merge idempotently by id; a *new* customer message
re-opens the ticket. The install record (+ usage snapshot when given) is
refreshed on every forward.

### `GET /ingest/v1/tickets/{ticketId}/replies?installId=X&since=RFC3339`

Header: `X-Poll-Secret: <secret>`. Response:

```json
{ "status": "open|pending|resolved",
  "replies": [ { "id": "…", "name": "RackOps Support", "body": "…", "at": "RFC3339" } ] }
```

Only **agent** replies strictly after `since` (omit `since` for the full
thread); internal notes are never included. Bad secret or unknown ticket →
`403` (indistinguishable on purpose).

### `POST /ingest/v1/reset-email`

```json
{ "installId": "uuid", "instanceName": "acme-lab",
  "email": "user@example.com", "link": "https://…/reset?token=…", "version": "0.4.0" }
```

`200 {"ok":true}` after relaying "Reset your RackOps password" (plain text +
HTML, link valid 1 hour, "ignore if not requested"). `503
{"error":"email relay not configured"}` when SMTP env is missing. Stricter
limits: 5/hour per IP **and** per target email. Every attempt is logged to
the `reset_emails` table (`sent` / `failed` / `unconfigured`).

### `POST /ingest/v1/snapshots`

```json
{ "installId": "uuid", "installName": "acme-lab", "version": "0.4.0", "usage": { … } }
```

`200 {"ok":true}`. UsageSnapshot shape:

```jsonc
{ "version": "0.4.0", "os": "linux", "arch": "amd64",
  "nodes": 3, "nodesOnline": 3, "services": 7,
  "servicesByKind": { "app": 4, "database": 2, "compose": 1, "template": 0 },
  "adopted": 1, "edgeEnabled": true, "meshEnabled": false, "ha": false,
  "orgs": 1, "users": 4, "domains": 2,
  "featuresUsed": ["deploys", "domains", "edge"] }
```

## Admin API (JWT bearer)

`POST /api/auth/login {email,password}` → `{token}` (7-day HS256 JWT).
Everything else requires `Authorization: Bearer <token>`:

- `GET /api/overview` — installs, activeWeek, openTickets, versionDist,
  featureUsage, byKindTotals, recentTickets
- `GET /api/installs` · `GET /api/installs/{id}` (detail + snapshot history + tickets)
- `GET /api/tickets?status=` · `GET /api/tickets/{id}?installId=` (full thread incl. notes)
- `POST /api/tickets/{id}/reply {body}` — author `agent`, name "RackOps
  Support", sets status `pending` (waiting on customer)
- `POST /api/tickets/{id}/note {body}` — internal only, never polled out
- `POST /api/tickets/{id}/status {status}` — manual open/pending/resolved

Status semantics: customer message → `open` (needs agent) · agent reply →
`pending` (waiting on customer) · `resolved` is manual.
