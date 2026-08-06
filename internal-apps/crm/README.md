# RackOps Admin

Everything that runs on RackOps' own machines, in **one binary, one console,
one data directory**. Customers run self-hosted RackOps Lite behind NAT; this
is the piece of RackOps-the-company they talk to.

1. **Support tickets** — receives tickets forwarded by customer instances,
   lets the support team answer them, and hands replies back when instances
   poll (all traffic is instance → here; nothing dials into customer networks).
2. **Password-reset relay** — sends reset emails from
   `support@rackops.in` on behalf of instances that have no SMTP of their own.
3. **Usage analytics** — stores opt-in usage snapshots and turns them into
   version distribution and "hero feature" rankings.
4. **Front Door** — the tunnel edge customers dial for public HTTPS. Runs in
   this same process, administered from the same console behind the same
   login. See [FRONT_DOOR.md](../../docs/FRONT_DOOR.md).

Merging the tunnel in removes the parts most likely to break a small vendor
deployment: two daemons to supervise, two data directories to back up, two
sets of credentials, and an admin API that had to be exposed over the network
so the two could talk.

```
internal-apps/crm/
  backend/            Go module github.com/rackops/crm — `rackops-admin` binary
  dashboard/          React 19 + Vite console (embedded into the binary)
  Dockerfile          Multi-stage image, built from the repo root
  docker-compose.yml  One-command deployment
  .env.example        Copy to .env and fill in two values
```

## Quick start

```sh
cd internal-apps/crm
cp .env.example .env          # set RACKOPS_ADMIN_EMAIL and RACKOPS_ADMIN_PASSWORD
docker compose up -d
```

Console on <http://127.0.0.1:8091>. The tunnel edge takes 443 (TCP **and**
UDP) and 80. Without a wildcard certificate it starts with a self-signed one
and says so on every page — fine to look around, useless for customers.

Running it directly instead:

```sh
RACKOPS_ADMIN_EMAIL=ops@rackops.in RACKOPS_ADMIN_PASSWORD=… \
  go run ./cmd/rackops-admin serve --frontdoor \
    --zone link.rackops.in \
    --cert /etc/rackops/fullchain.pem --key /etc/rackops/privkey.pem
```

Omit `--frontdoor` to run support and CRM only.

## Environment

| Variable             | Required | Default                                   | Purpose                          |
| -------------------- | -------- | ----------------------------------------- | -------------------------------- |
| `RACKOPS_ADMIN_EMAIL` | yes     | —                                         | Console login email (`CRM_ADMIN_EMAIL` still accepted) |
| `RACKOPS_ADMIN_PASSWORD` | yes  | —                                         | Console login password, bcrypt-compared (`CRM_ADMIN_PASSWORD` still accepted) |
| `SMTP_HOST`          | no       | —                                         | Reset-email relay host; relay returns 503 when unset |
| `SMTP_PORT`          | no       | `587`                                     | SMTP port (PLAIN auth + STARTTLS) |
| `SMTP_USERNAME`      | no       | —                                         | SMTP username                    |
| `SMTP_PASSWORD`      | no       | —                                         | SMTP password                    |
| `SMTP_FROM`          | no       | `RackOps Support <support@rackops.in>`    | From header; Reply-To is always `support@rackops.in` |

State lives in `--data-dir` (default `~/.rackops-admin`): a WAL-mode SQLite
database (`crm.db`, pure-Go driver — no cgo), the JWT session secret
(`session.secret`, random 32 bytes created on first start), and the Front
Door tenant registry (`frontdoor-tenants.json`). One path to back up.

## Run / develop / build

```sh
# Backend (console on :8091). Add --frontdoor to run the tunnel edge too;
# without --cert/--key it uses a development certificate.
cd internal-apps/crm/backend
RACKOPS_ADMIN_EMAIL=ops@rackops.in RACKOPS_ADMIN_PASSWORD=... \
  go run ./cmd/rackops-admin serve

# Dashboard with HMR (proxies /api and /ingest to localhost:8091)
pnpm --filter @rackops/crm-dashboard dev

# Production build: dashboard → Go embed dir → single binary
pnpm --filter @rackops/crm-dashboard build
cd internal-apps/crm/backend && go build -o rackops-admin ./cmd/rackops-admin

# Tests. GOWORK=off proves the module still builds standalone, which is how
# the Docker image builds it — go.work otherwise hides missing go.sum entries.
cd internal-apps/crm/backend && go vet ./... && go test ./...
GOWORK=off go build ./...
```

This module depends on `github.com/rackops/rackops` for the tunnel
(`pkg/tunnel`, `pkg/edge`) via a relative `replace` directive, so both module
trees must be present — which is why the Dockerfile copies each of them.

## Deploy

Prefer `docker compose up -d` (see Quick start). The equivalent by hand:

```sh
# From the monorepo root:
docker build -f internal-apps/crm/Dockerfile -t rackops-admin .
docker run -d --name rackops-admin \
  -p 127.0.0.1:8091:8091 -p 443:443/tcp -p 443:443/udp -p 80:80 \
  -v rackops-admin-data:/data \
  -v /etc/rackops/certs:/etc/rackops/certs:ro \
  -e RACKOPS_ADMIN_EMAIL=ops@rackops.in \
  -e RACKOPS_ADMIN_PASSWORD=... \
  -e SMTP_HOST=smtp.example.com -e SMTP_USERNAME=... -e SMTP_PASSWORD=... \
  rackops-admin serve --listen :8091 --data-dir /data \
    --frontdoor --zone link.rackops.in \
    --cert /etc/rackops/certs/fullchain.pem \
    --key /etc/rackops/certs/privkey.pem
```

Bind **443/udp** as well as 443/tcp: without it every customer silently falls
back to the slower TCP transport. Keep the console port on loopback or behind
your own authenticating proxy — anyone who reaches it can suspend any
customer.

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
