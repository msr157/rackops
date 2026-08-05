# Front Door: start-to-finish setup

Two machines are involved:

| | What it is | Who runs it |
|---|---|---|
| **POP** | A cheap VPS with a public IP running `rackops-edge`. Holds the wildcard certificate. | You (RackOps, the vendor) — once |
| **Install** | The customer's RackOps server + agents, behind NAT. | Each customer |

You set the POP up once. Every install after that is two clicks.

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

> Skipping this? `rackops-edge` will generate a self-signed certificate and log
> a loud warning. Fine for a local trial, useless in production — browsers will
> refuse it.

---

## Part 3 — Run the POP

On the VPS:

```sh
# Build (or copy the binary over)
cd apps/rackops-lite/backend
go build -o /usr/local/bin/rackops-edge ./cmd/rackops-edge

# An admin token the control plane will use. Keep it in a file, not a flag:
# flag values are world-readable via /proc/*/cmdline.
sudo mkdir -p /etc/rackops /var/lib/rackops-edge
openssl rand -hex 32 | sudo tee /etc/rackops/admin.token >/dev/null
sudo chmod 600 /etc/rackops/admin.token
```

`/etc/systemd/system/rackops-edge.service`:

```ini
[Unit]
Description=RackOps Front Door edge
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/rackops-edge \
  --pop-id pop1 \
  --zone link.rackops.in \
  --public-addr :443 \
  --http-addr :80 \
  --admin-addr 127.0.0.1:9443 \
  --cert /etc/rackops/lego/certificates/_.link.rackops.in.crt \
  --key  /etc/rackops/lego/certificates/_.link.rackops.in.key \
  --data-dir /var/lib/rackops-edge \
  --admin-token-file /etc/rackops/admin.token \
  --support-email support@rackops.in
Restart=always
RestartSec=2
# Binding 443 without running as root.
AmbientCapabilities=CAP_NET_BIND_SERVICE
User=rackops
Group=rackops

[Install]
WantedBy=multi-user.target
```

```sh
sudo useradd --system --no-create-home rackops || true
sudo chown -R rackops:rackops /var/lib/rackops-edge /etc/rackops
sudo systemctl daemon-reload && sudo systemctl enable --now rackops-edge
```

**Firewall — open exactly these:**

```sh
sudo ufw allow 443/tcp     # visitor HTTPS + tunnel fallback
sudo ufw allow 443/udp     # tunnel over QUIC  ← easy to forget
sudo ufw allow 80/tcp      # redirect to HTTPS
# 9443 stays on 127.0.0.1. Do NOT expose the admin API.
```

Check it:

```sh
curl -s http://127.0.0.1:9443/healthz | jq
# {"status":"ok","pop":"pop1","zone":"link.rackops.in","sessions":0,"certDaysLeft":89,...}
```

443 carries visitor HTTPS *and* tunnel sessions on one socket, sorted by ALPN
after the TLS handshake — QUIC takes the same port on UDP. Both roles need
exactly 443 (a URL without a port means 443; 443 is also the one outbound port
restrictive networks allow), so they share it.

### Reaching the admin API from the control plane

The admin API listens on loopback. If your control plane runs elsewhere, do
**not** move it to `0.0.0.0` — tunnel it:

```sh
# On the control plane machine
ssh -N -L 9443:127.0.0.1:9443 rackops@pop1.rackops.in
```

…and point `--frontdoor-admin` at `http://127.0.0.1:9443`. Or put the admin API
behind your own authenticated proxy. Anyone who reaches it can suspend any
customer.

---

## Part 4 — Point an install at the POP

On the customer's RackOps server:

```sh
# The admin token from Part 3, copied over
sudo install -m 600 /dev/stdin /etc/rackops/fd.token <<<'<admin token>'

rackops server \
  --listen :8080 \
  --frontdoor-pop   pop1.rackops.in:443 \
  --frontdoor-zone  link.rackops.in \
  --frontdoor-admin http://127.0.0.1:9443 \
  --frontdoor-token-file /etc/rackops/fd.token
```

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
cd apps/rackops-lite/backend
go build -o /tmp/rackops-edge ./cmd/rackops-edge

/tmp/rackops-edge \
  --pop-id dev --zone link.rackops.in \
  --public-addr 127.0.0.1:8443 --http-addr "" \
  --admin-addr 127.0.0.1:9443 \
  --data-dir /tmp/fd-dev \
  --admin-token dev-token-0123456789
```

In another shell:

```sh
T=dev-token-0123456789

# Enroll a fake tenant
curl -s -X PUT -H "Authorization: Bearer $T" \
  -d '{"publicKey":"'"$(openssl rand -hex 32)"'","workspace":"dev",
       "hostnames":["app-dev.link.rackops.in"]}' \
  http://127.0.0.1:9443/admin/tenants/inst-dev | jq

# Enrolled but nobody connected → a clear 502, not a hang
curl -sk --resolve app-dev.link.rackops.in:8443:127.0.0.1 \
  https://app-dev.link.rackops.in:8443/ | grep -o '<h1>.*</h1>'

# Not our zone → 404, never an open relay
curl -sk -o /dev/null -w '%{http_code}\n' \
  --resolve evil.example.com:8443:127.0.0.1 https://evil.example.com:8443/

# Kill switch, with the reason shown to visitors
curl -s -X POST -H "Authorization: Bearer $T" \
  -d '{"suspended":true,"reason":"phishing report #7"}' \
  http://127.0.0.1:9443/admin/tenants/inst-dev/suspend
```

The full path — real POP, real tunnel client, real backend — is exercised by
`go test ./internal/edge/ -run TestEndToEnd -v`.

---

## Operating it

```sh
# Who is connected, over what
curl -s -H "Authorization: Bearer $T" http://127.0.0.1:9443/admin/sessions | jq

# One tenant's usage
curl -s -H "Authorization: Bearer $T" \
  http://127.0.0.1:9443/admin/tenants/inst-1/usage | jq

# Abuse report → offline immediately, session dropped in the same call
curl -s -X POST -H "Authorization: Bearer $T" \
  -d '{"suspended":true,"reason":"phishing — ticket 412"}' \
  http://127.0.0.1:9443/admin/tenants/inst-1/suspend

# Undo
curl -s -X POST -H "Authorization: Bearer $T" \
  -d '{"suspended":false}' \
  http://127.0.0.1:9443/admin/tenants/inst-1/suspend
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

Logs: `journalctl -u rackops-edge -f` on the POP, `journalctl -u rackops -f`
(look for `frontdoor:`) on the install.

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
- [ ] **Back up `/var/lib/rackops-edge/tenants.json`.** Losing it disconnects
      every customer until they re-enroll.
- [ ] **A second POP.** One VPS is a single point of failure for every
      customer's public URL.
