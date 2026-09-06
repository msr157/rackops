# Front Door: start-to-finish setup

Two machines are involved:

| | What it is | Who runs it |
|---|---|---|
| **RackOps Admin** | One VPS with a public IP running `rackops-admin`: support inbox, install analytics, **and** the Front Door tunnel edge, in one process. Holds the wildcard certificate. | You (RackOps, the vendor) — once |
| **Install** | The customer's RackOps server + agents, behind NAT. | Each customer |

You set the admin box up once. Every install after that is two clicks.

> Running a POP in a second region later? `rackops-edge` is the same edge as
> a standalone binary — see [Extra POPs](#extra-pops). For one machine, you
> only need `rackops-admin`.

---

## Part 1 — DNS (do this first, it needs to propagate)

At your DNS provider for `rackops.in`, create **two** records pointing at the
POP's public IP:

| Type | Name | Value |
|---|---|---|
| `A` | `pop1` | `<POP public IP>` |
| `A` | `*.link` | `<POP public IP>` |

- `pop1.rackops.in` is what tunnel clients dial and what the certificate is
  validated against.
- `*.link.rackops.in` is where every customer app lands
  (`immich-acme.link.rackops.in`, and so on).

Add `AAAA` records too if the POP has IPv6.

Verify before continuing:

```sh
dig +short pop1.rackops.in
dig +short anything.link.rackops.in     # must return the same IP
```

---

## Part 2 — The wildcard certificate

A wildcard requires **DNS-01** validation, so this needs an API token from your
DNS provider. RackOps deliberately does not embed one provider's API — use
[lego](https://go-acme.github.io/lego/), certbot, or acme.sh with whatever
provider you have.

Example with lego and Cloudflare DNS:

```sh
export CLOUDFLARE_DNS_API_TOKEN=<token with DNS:Edit on rackops.in>

lego --email ops@rackops.in \
     --dns cloudflare \
     --domains '*.link.rackops.in' \
     --domains 'pop1.rackops.in' \
     --path /etc/rackops/lego \
     run
```

That writes:

```
/etc/rackops/lego/certificates/_.link.rackops.in.crt   # chain
/etc/rackops/lego/certificates/_.link.rackops.in.key   # private key
```

Renew from cron or a systemd timer — **no restart needed**, the POP watches
the files and hot-reloads within a minute:

```sh
0 3 * * * lego --email ops@rackops.in --dns cloudflare \
  --domains '*.link.rackops.in' --domains 'pop1.rackops.in' \
  --path /etc/rackops/lego renew --days 30
```

> Skipping this? RackOps Admin generates a self-signed certificate, logs a
> loud warning, and shows a banner on the Front Door page. Fine for a local
> trial, useless in production — browsers will refuse it.

---

## Part 3 — Run RackOps Admin

The easy path is Docker Compose — two values in a file, one command:

```sh
cd internal-apps/crm
cp .env.example .env      # set RACKOPS_ADMIN_EMAIL + RACKOPS_ADMIN_PASSWORD,
                          # and FRONTDOOR_CERT_DIR to where Part 2 wrote certs
docker compose up -d
```

That is the whole deployment: console on `127.0.0.1:8091`, tunnel edge on
443 (TCP **and** UDP) and 80, one Docker volume holding the database, the
session secret and the tunnel's tenant registry.

Prefer systemd? Build and run it directly:

```sh
cd internal-apps/crm/backend
go build -o /usr/local/bin/rackops-admin ./cmd/rackops-admin
sudo mkdir -p /var/lib/rackops-admin
```

`/etc/systemd/system/rackops-admin.service`:

```ini
[Unit]
Description=RackOps Admin — support, CRM and the Front Door tunnel
After=network-online.target
Wants=network-online.target

[Service]
Environment=RACKOPS_ADMIN_EMAIL=ops@rackops.in
EnvironmentFile=/etc/rackops/admin.env
ExecStart=/usr/local/bin/rackops-admin serve \
  --listen 127.0.0.1:8091 \
  --data-dir /var/lib/rackops-admin \
  --frontdoor \
  --pop-id pop1 \
  --zone link.rackops.in \
  --public-addr :443 \
  --http-addr :80 \
  --cert /etc/rackops/lego/certificates/_.link.rackops.in.crt \
  --key  /etc/rackops/lego/certificates/_.link.rackops.in.key \
  --support-email support@rackops.in
Restart=always
RestartSec=2
# Binding 443/80 without running as root.
AmbientCapabilities=CAP_NET_BIND_SERVICE
User=rackops
Group=rackops

[Install]
WantedBy=multi-user.target
```

`/etc/rackops/admin.env` holds the password, so it never appears in a unit
file or in `ps`:

```sh
sudo useradd --system --no-create-home rackops || true
printf 'RACKOPS_ADMIN_PASSWORD=%s\n' "$(openssl rand -base64 24)" \
  | sudo tee /etc/rackops/admin.env >/dev/null
sudo chmod 600 /etc/rackops/admin.env
sudo chown -R rackops:rackops /var/lib/rackops-admin /etc/rackops
sudo systemctl daemon-reload && sudo systemctl enable --now rackops-admin
```

**Firewall — open exactly these:**

```sh
sudo ufw allow 443/tcp     # visitor HTTPS + tunnel fallback
sudo ufw allow 443/udp     # tunnel over QUIC  ← easy to forget
sudo ufw allow 80/tcp      # redirect to HTTPS
# 8091 stays on 127.0.0.1. Do NOT expose the console: anyone who reaches it
# can suspend any customer.
```

Check it:

```sh
curl -s http://127.0.0.1:8091/healthz          # → ok
```

Then open the console at `http://127.0.0.1:8091` (tunnel it over SSH from
your laptop) and go to **Front Door**. It shows the POP, the certificate's
remaining days, a warning if it is self-signed, connected tenants, traffic
against each one's quota, and a **Suspend** button.

443 carries visitor HTTPS *and* tunnel sessions on one socket, sorted by ALPN
after the TLS handshake — QUIC takes the same port on UDP. Both roles need
exactly 443 (a URL without a port means 443; 443 is also the one outbound port
restrictive networks allow), so they share it.

### Reaching the console

It listens on loopback. Do **not** move it to `0.0.0.0` — tunnel it:

```sh
ssh -N -L 8091:127.0.0.1:8091 rackops@pop1.rackops.in
# then browse http://127.0.0.1:8091
```

---

## Part 4 — Point an install at the POP

On the customer's RackOps server:

```sh
# A tenant-bound grant issued from RackOps Admin for this install only.
# Never copy the vendor signing/admin token to a customer machine.
sudo install -m 600 /dev/stdin /etc/rackops/fd.token <<<'<tenant enrollment grant>'

rackops server \
  --listen :8080 \
  --frontdoor-pop   pop1.rackops.in:443 \
  --frontdoor-zone  link.rackops.in \
  --frontdoor-admin https://admin.rackops.in \
  --frontdoor-token-file /etc/rackops/fd.token
```

The **Front Door** page in RackOps Admin registers the install's tenant ID,
public key and unique workspace, then issues a tenant-bound grant. See
[the enrollment procedure](FRONT_DOOR_ENROLLMENT.md). The grant expires after
24 hours; later hostname changes require a fresh grant. Existing established
tunnel identity does not expire with the grant.

The Admin file `<data-dir>/frontdoor-enroll.token` is a vendor-only signing
secret despite its legacy name. Never copy it to a customer or use it as a
customer enrollment grant. Automated grant renewal is not implemented yet.

Existing systemd unit? Add the four flags to `ExecStart` and
`sudo systemctl restart rackops`.

On startup you should see:

```
server: RackOps Front Door available via pop1.rackops.in:443 (zone link.rackops.in)
```

Without `--frontdoor-pop` the console says the install isn't set up for Front
Door instead of showing a switch that cannot work.

---

## Part 5 — Turn it on (the part customers do)

1. **Network** page → **Front Door**.
2. Type a workspace name (`acme`) → flip the switch.
   The Traefik edge starts automatically if it isn't already running.
3. Open any app → **Networking** → flip **Front Door** on.
4. The URL appears immediately:

```
https://immich-acme.link.rackops.in
```

The Network page then shows the live truth: connected/connecting/rejected, which
POP answered, whether it fell back to TCP, and traffic used against the 100 GB
monthly free tier.

---

## Try it locally first (no VPS, no DNS, 2 minutes)

```sh
cd internal-apps/crm/backend
go build -o /tmp/rackops-admin ./cmd/rackops-admin

RACKOPS_ADMIN_EMAIL=ops@rackops.in RACKOPS_ADMIN_PASSWORD=secret123 \
/tmp/rackops-admin serve \
  --listen 127.0.0.1:8091 --data-dir /tmp/rackops-admin-dev \
  --frontdoor --pop-id dev --zone link.rackops.in \
  --public-addr 127.0.0.1:8443 --http-addr ""
```

Open <http://127.0.0.1:8091>, sign in, and click **Front Door**. Or drive it
from a second shell:

```sh
A=http://127.0.0.1:8091
T=$(cat /tmp/rackops-admin-dev/frontdoor-enroll.token)

# Enroll a fake tenant the way a customer's control plane would
curl -s -X PUT -H "Authorization: Bearer $T" \
  -d '{"publicKey":"'"$(openssl rand -hex 32)"'","workspace":"dev",
       "hostnames":["app-dev.link.rackops.in"]}' \
  $A/admin/tenants/inst-dev | jq

# Enrolled but nobody connected → a clear 502, not a hang
curl -sk --resolve app-dev.link.rackops.in:8443:127.0.0.1 \
  https://app-dev.link.rackops.in:8443/ | grep -o '<h1>.*</h1>'

# Not our zone → 404, never an open relay
curl -sk -o /dev/null -w '%{http_code}\n' \
  --resolve evil.example.com:8443:127.0.0.1 https://evil.example.com:8443/

# Visitor HTTPS and tunnels share one port, sorted by ALPN
openssl s_client -connect 127.0.0.1:8443 -alpn rackops-fd/1 </dev/null 2>/dev/null \
  | grep -i 'ALPN protocol'

# Kill switch, with the reason shown to visitors
curl -s -X POST -H "Authorization: Bearer $T" \
  -d '{"suspended":true,"reason":"phishing report #7"}' \
  $A/admin/tenants/inst-dev/suspend
```

The full path — real POP, real tunnel client, real backend — is exercised by
`go test ./internal/edge/ -run TestEndToEnd -v`.

---

## Operating it

Everything below is in the console under **Front Door**: connected tenants
and their transport, traffic against each quota, and a Suspend button that
takes effect immediately. Since abuse reports arrive as support tickets in
the same app, acting on one never means SSHing anywhere.

The following legacy `/admin/*` examples apply only to the standalone edge's
private vendor-admin listener, using its vendor administrator credential.
They are not customer enrollment calls and are not mounted by integrated
RackOps Admin. Use the authenticated Admin dashboard for integrated operation:

```sh
# Who is connected, over what
curl -s -H "Authorization: Bearer $T" https://admin.rackops.in/admin/sessions | jq

# One tenant's usage
curl -s -H "Authorization: Bearer $T" \
  https://admin.rackops.in/admin/tenants/inst-1/usage | jq

# Abuse report → offline immediately, session dropped in the same call
curl -s -X POST -H "Authorization: Bearer $T" \
  -d '{"suspended":true,"reason":"phishing — ticket 412"}' \
  https://admin.rackops.in/admin/tenants/inst-1/suspend

# Undo
curl -s -X POST -H "Authorization: Bearer $T" \
  -d '{"suspended":false}' \
  https://admin.rackops.in/admin/tenants/inst-1/suspend
```

A quota suspension lifts by itself on the 1st. An abuse suspension does not —
otherwise every abuser would be un-suspended monthly.

---

## When it doesn't work

| Symptom | Cause | Fix |
|---|---|---|
| Console stuck on **connecting** | POP unreachable, or 443 blocked outbound | `curl -v https://pop1.rackops.in` from the install |
| **not connected: …key is not recognised** | Install never enrolled, or its key rotated | Check `/admin/tenants`; the reconciler retries each minute |
| Console shows **TCP fallback** | UDP/443 blocked — often the POP firewall | `sudo ufw allow 443/udp` |
| Visitor gets **502 App is offline** | Tunnel down | Network page status; `journalctl -u rackops` |
| Visitor gets **502 not responding** | Tunnel up, app isn't | Is the app running? Does it listen on its declared port? |
| Visitor gets **404 No such app** | Hostname not registered | Re-flip Front Door on the app |
| Visitor gets **404 Unknown address** | Host outside `--zone` | DNS points somewhere it shouldn't |
| Browser cert warning | Self-signed (no `--cert`) | Part 2 |
| Redirect loop | Only possible if the `frontdoor` entrypoint was removed from Traefik | See the two traps in `FRONT_DOOR.md` |

Logs: `journalctl -u rackops-admin -f` (or `docker compose logs -f`) on the
admin box, `journalctl -u rackops -f` (look for `frontdoor:`) on the install.

---

## Before you let real customers on

Not code — process, and genuinely blocking:

- [ ] **`abuse@rackops.in` that a human reads.** You are now the hosting
      provider of record. The first phishing report will not wait.
- [ ] **ToS saying the POP terminates TLS** and can therefore see traffic.
      Disclose it now; passthrough mode is a later privacy tier and lands far
      better as "and now we can't" than as a correction.
- [ ] **Bandwidth alerting on the POP.** The 100 GB free tier is per tenant;
      your VPS bill is not.
- [ ] **Back up the admin data directory** (`/var/lib/rackops-admin`, or the
      `rackops-admin-data` volume). It holds the ticket database, the session
      secret and the tunnel registry; losing it disconnects every customer
      until they re-enroll.
- [ ] **A second POP.** One VPS is a single point of failure for every
      customer's public URL — see below.

## Extra POPs

`rackops-admin --frontdoor` runs one edge on the admin box, which is all a
small deployment needs. For a POP in another region, `rackops-edge` is the
same edge as a standalone binary:

```sh
rackops-edge --pop-id fra1 --zone link.rackops.in \
  --public-addr :443 --http-addr :80 \
  --cert … --key … \
  --admin-token-file /etc/rackops/enroll.token   # the same token
```

Point that POP's `--admin-token-file` at the same enrollment token, and add
its IP to the `*.link` DNS record. Note the current limits: usage is counted
per POP rather than aggregated, and an install holds a session to one POP at
a time, so a POP restart is a brief outage rather than a seamless failover.
