# RackOps Front Door

RackOps' own tunnel service: a permanent public HTTPS address for any app,
with no public IP, no port forwarding, no DNS records, and no third-party
account. It replaces the Cloudflare quick tunnel as the recommended way to
make an app reachable, and works behind CGNAT — where port forwarding cannot.

```
  visitor
     │  https://immich-acme.link.rackops.in
     ▼
┌──────────────────────┐
│  rackops-edge (POP)  │  vendor-run. Terminates *.link.rackops.in,
│  TLS · routing ·     │  maps hostname → tenant session, meters bytes,
│  quota · kill switch │  enforces suspension.
└──────────┬───────────┘
           │  tunnel session, dialled OUTBOUND by the customer
           │  QUIC/443 preferred · TLS+yamux/443 fallback
           ▼
┌──────────────────────┐
│  rackops agent       │  in-process client on the ingress node.
│  (tunnel client)     │  Supervised, reconnects with backoff.
└──────────┬───────────┘
           │  127.0.0.1:8099  (Traefik's `frontdoor` entrypoint)
           ▼
┌──────────────────────┐
│  Traefik edge        │  UNCHANGED. Same /api/traefik/config.
└──────────┬───────────┘
           │  local port, or mesh relay port
           ▼
       your app  (any node, any house, behind any NAT)
```

Nothing ever dials into a customer network — the same invariant the mesh and
the CRM channel already hold.

## Why it stacks on Traefik instead of replacing it

Turning Front Door on for a service sets `svc.Domain` to a vendor-issued
hostname and flags `svc.FrontDoor`. The hostname then flows into the existing
Traefik configuration as an ordinary router. Front Door replaces the *public
IP*, not the routing stack, so per-app routers, middlewares, health and the
two-hop mesh relay all keep working with no second implementation of "which
app answers this hostname".

## Two traps this design avoids

Both are load-bearing; removing either breaks the feature in a way that is
hard to diagnose from the symptom.

**1. Front Door hostnames must never reach ACME.** The customer does not
control `*.link.rackops.in`, so every HTTP-01 challenge would fail — and the
failures would burn Let's Encrypt rate limits that the customer's *real*
domains need. `handleTraefikConfig` therefore emits no `certResolver` for a
`FrontDoor` service. TLS is terminated at the POP, which holds the wildcard.

**2. Front Door traffic must not use the `web` entrypoint.** Once ACME is
enabled, `web` carries a blanket HTTP→HTTPS redirect. The tunnel delivers
cleartext, so on `web` every request would answer `301` to its own public
URL — which resolves back to the POP, back down the tunnel, and into an
infinite redirect loop. Hence the dedicated `frontdoor` entrypoint bound to
`127.0.0.1:8099` (`types.FrontDoorEntryPort`), which also keeps the port off
the network entirely.

Both are covered by tests in `internal/server/frontdoor_test.go`.

## Security model

| Concern | How it is handled |
|---|---|
| Identity | Per-install Ed25519 key in `<data-dir>/frontdoor.key` (0600). The private key never leaves the machine. |
| Replay | The POP issues a random 32-byte challenge per connection; the client signs a domain-separated transcript over it. |
| Cross-tenant replay | The transcript binds the tenant ID, so a signature for one tenant cannot be presented for another. |
| MITM | The transcript binds the TLS exporter (RFC 5705 channel binding), so a captured signature cannot be forwarded onto a different TLS session. |
| Hostname theft | The POP's registry is authoritative. A client's asserted hostnames are a logging hint only, and `Upsert` refuses a hostname already owned by another tenant. |
| Compromised POP | The agent independently refuses any stream for a hostname outside its assignment, so a malicious POP cannot aim the tunnel at an arbitrary local service. |
| Tenant enumeration | Unknown tenants and bad signatures are rejected identically. |
| Org isolation | Every handler goes through `serviceForOrg` + a `ManageDomains` check. (The product audit found the quick-tunnel handlers fetched services by raw ID with no ownership check; Front Door does not repeat that.) |
| Open relay | The POP refuses any Host outside its configured zone, so a dangling CNAME cannot make it proxy for someone else's domain. |

**TLS is terminated at the POP**, which means the vendor can see plaintext.
This is the v1 decision and must be disclosed in the ToS. Passthrough mode
(SNI-only routing, customer terminates with a delegated certificate) is the
planned P2 privacy tier.

## Metering, quota and abuse

- **Free tier: 100 GB/month per tenant** (`edge.DefaultQuotaBytes`).
- Both directions are billed — an upload costs the POP the same bandwidth as
  a download.
- Metering is on from the first byte even for uncapped tenants. Retrofitting
  usage accounting onto live workloads means guessing at history or
  surprising customers with a limit they had no way to see coming.
- Periods are calendar months in UTC and roll **lazily on access**, so a POP
  that was down at midnight on the 1st does not charge last month's usage
  against this month's allowance.
- A quota suspension lifts automatically at the period roll. **An abuse
  suspension does not** — otherwise every abuser would be un-suspended on the
  1st.
- The kill switch is immediate: suspending marks the tenant *and* drops the
  live session in the same call. One request actions a phishing report.

## Running a POP

```sh
rackops-edge \
  --pop-id blr1 \
  --zone link.rackops.in \
  --cert /etc/rackops/fullchain.pem \
  --key  /etc/rackops/privkey.pem \
  --public-addr :443 \
  --http-addr :80 \
  --admin-token-file /etc/rackops/admin.token
```

**Everything public shares `--public-addr` (443).** On TCP, one TLS listener
sorts connections by negotiated ALPN: `rackops-fd/1` is a tunnel, `h2` and
`http/1.1` are visitors. On UDP, the same port carries QUIC for tunnels.

This is not a space saving — it is the only arrangement where both roles work.
A URL without a port means 443, so visitors need exactly that; and 443 is the
one outbound port restrictive networks reliably allow, which is the entire
reason the TCP fallback exists. Splitting them would put one of the two on a
port that cannot do its job.

The wildcard certificate needs DNS-01 and is obtained **out of band** by any
ACME client (certbot, lego, acme.sh) with whatever DNS provider you prefer.
The POP watches the files and hot-reloads on renewal, so rotation is not an
outage. Embedding one provider's API would have locked the deployment to it.

Omitting `--cert`/`--key` generates a self-signed certificate and logs a loud
warning — development only; browsers will refuse it, which is the intended
outcome.

The TCP listener binds first and the UDP socket is pinned to whatever port it
landed on, so `:0` (tests, ephemeral POPs) cannot hand the two transports
different ports.

### Admin API

Bound to a private address, bearer-token guarded.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Unauthenticated. Sessions, bytes, cert days remaining. |
| `GET` | `/admin/tenants` | List tenants. |
| `PUT` | `/admin/tenants/{id}` | Enroll or update. Idempotent; rotating a key drops the live session immediately. |
| `DELETE` | `/admin/tenants/{id}` | Remove and disconnect. |
| `GET` | `/admin/tenants/{id}/usage` | Usage, quota, reset date, connected. |
| `POST` | `/admin/tenants/{id}/suspend` | Kill switch. A reason is **required** when suspending — it is shown to the customer and to visitors. |
| `GET` | `/admin/sessions` | Live sessions with transport and stream counts. |

## Enabling it on an install

```sh
rackops server \
  --frontdoor-pop pop1.rackops.in:443 \
  --frontdoor-zone link.rackops.in \
  --frontdoor-admin https://pop1.rackops.in:9443 \
  --frontdoor-token-file /etc/rackops/fd.token
```

With no `--frontdoor-pop`, the console says the install is not set up for
Front Door rather than showing a switch that cannot work.

Then, in the console: **Network → Front Door**, pick a workspace name, and
switch it on. Enabling starts the Traefik edge automatically if one is not
already running — requiring the user to go set that up first is exactly the
friction this feature removes. Per app: **Networking → Front Door**.

Addresses are `<app>-<workspace>.<zone>`. A single DNS label keeps every name
inside one wildcard certificate; `<app>.<workspace>.<zone>` would need a
certificate per workspace, with per-tenant ACME, renewal and rate limits — real
machinery buying only cosmetics, and a one-way door once customers have
bookmarks.

## Transports

QUIC is tried first and falls back to TLS+yamux over TCP. The QUIC probe gets
a **5-second** leash, not the full 20-second handshake budget: on a network
that silently drops UDP there is nothing to wait for and the user is watching
a spinner. Hotels, campuses, corporate guest wifi and some mobile carriers all
block UDP, so a product promising "it just works" cannot ship QUIC-only.

The console shows a **TCP fallback** badge when this happens — it is the first
thing worth knowing if the connection feels slow.

Authentication failures are never retried across transports: a rejected
identity would be rejected identically over TCP, and retrying only doubles
POP load and buries the real error.

## Files

| Path | What |
|---|---|
| `internal/tunnel/` | Wire protocol, handshake, transports, `Forwarder`. Shared by both ends so a one-sided change to the stream handshake is visible in review. |
| `internal/edge/` | POP: registry, sessions, metered reverse proxy, certs, admin API. |
| `cmd/rackops-edge/` | The vendor POP binary. |
| `internal/agent/frontdoor.go` | Supervised client: key, reconnect policy, state reporting. |
| `internal/server/frontdoor.go` | Control plane: enable/disable, enrollment, spec delivery, reconciler. |

## Tests

- `internal/tunnel/tunnel_test.go` — signing, transcript tampering, framing
  limits, handshake over both transports, half-close (the bug that truncates
  large downloads).
- `internal/edge/edge_test.go` — registry, quota, period roll, hostname theft,
  routing refusals.
- `internal/edge/e2e_test.go` — **the headline test**: a real POP, a real
  client and a real backend in one process, proving an HTTPS request reaches
  an app that never accepted an inbound connection. Covers both transports,
  metering in both directions, SSE streaming without buffering, websocket
  upgrades, immediate suspension, and 25 concurrent requests.
- `internal/server/frontdoor_test.go` — handlers, org isolation, the two
  Traefik traps above.

## Known gaps

- **Single POP per install.** The client supports reconnecting but does not
  yet hold sessions to two POPs simultaneously; a POP restart is a brief
  outage rather than a seamless failover. (P1.)
- **Usage is POP-local.** With multiple POPs, quota would need aggregating in
  the vendor control plane rather than each POP counting independently.
- **No `abuse@` intake or ToS copy.** The kill switch exists and works; the
  process around it is a launch blocker that is not code.
- **TLS terminates at the POP.** Passthrough is P2.
