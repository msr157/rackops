# RackOps Front Door: Overall Changes & Architecture

## Overview

RackOps Front Door is a built-in tunnel service providing permanent public HTTPS addresses for apps running on self-hosted machines without requiring public IPs, port forwarding, DNS records, or third-party accounts (e.g. Cloudflare Tunnels). It allows deployments behind CGNAT or restrictive firewalls to be securely reachable from the public internet.

```
Visitor (Public Internet)
    │  https://<app>-<workspace>.link.rackops.in
    ▼
┌────────────────────────────────────────────────────────┐
│  rackops-edge / rackops-admin (POP)                    │
│  • Terminates *.link.rackops.in on port 443 (TCP/UDP)   │
│  • Demuxes visitor HTTPS and tunnel sessions via ALPN  │
│  • Meters bandwidth (100 GB/month default quota)       │
│  • Immediate abuse kill switch (tenant suspension)     │
└──────────────────────────┬─────────────────────────────┘
                           │  Outbound tunnel session (dialled by agent)
                           │  QUIC/443 (UDP) primary · TLS+yamux/443 (TCP) fallback
                           ▼
┌────────────────────────────────────────────────────────┐
│  rackops agent (Ingress Node)                          │
│  • In-process tunnel worker (no container required)     │
│  • Supervised background loop with exponential backoff │
│  • Holds private Ed25519 identity key                  │
└──────────────────────────┬─────────────────────────────┘
                           │  127.0.0.1:8099 (dedicated frontdoor entrypoint)
                           ▼
┌────────────────────────────────────────────────────────┐
│  Traefik Edge (Ingress Router)                         │
│  • Standard /api/traefik/config routing pipeline       │
│  • No certResolver (TLS already terminated at POP)     │
│  • Isolated from web entrypoint to avoid 301 loop      │
└──────────────────────────┬─────────────────────────────┘
                           │  Local container port / mesh relay
                           ▼
                       Target Application
```

---

## Key Components & Changes

### 1. Wire Protocol & Transports (`backend/internal/tunnel`)
- **Protocol**: Custom binary framing supporting multiplexed bidirectional streams behind a unified `Session` interface.
- **Transports**:
  - **QUIC on UDP/443**: Primary high-performance transport with a 5-second initial probe.
  - **TLS + Yamux on TCP/443**: Automatic fallback when UDP is blocked or throttled by restrictive networks.
- **Authentication & Security**:
  - Ed25519 challenge-response handshake.
  - Handshake transcript binds a server nonce, tenant ID, and the RFC 5705 TLS channel binding exporter.
  - Prevents MITM tampering, cross-tenant replaying, and captured signature reuse.

### 2. POP Edge Server (`backend/internal/edge` & `backend/cmd/rackops-edge`)
- **Single-Port Multiplexing**:
  - Listens on `:443` for both incoming HTTPS visitor traffic and outbound tunnel client connections.
  - Sorts connections cleanly using ALPN: `rackops-fd/1` routes to tunnel sessions; `h2` and `http/1.1` route to the reverse proxy.
  - Shares UDP/443 for QUIC tunnel transport.
- **Wildcard TLS & Certificate Management**:
  - Hot reloads wildcard certificates for `*.<zone>` on renewal without dropping active connections.
  - Falls back to self-signed certificates with a console warning for local development.
- **Registry & Tenant Isolation**:
  - Persistent disk storage (`frontdoor-tenants.json`) tracking tenant ID, workspace name, public keys, and registered hostnames.
  - Hostname ownership verification prevents cross-tenant hijacking.
- **Accounting & Kill Switch**:
  - Bidirectional byte metering against monthly quota (default 100 GB/month).
  - Lazy quota reset on the first of each calendar month.
  - Instant tenant suspension drops active connections and immediately serves a reason-coded response to visitors.
- **Error Handling**: Custom error responses for `502 App is offline`, `502 Application not responding`, `404 No such app`, and `403 Suspended`.

### 3. Agent Tunnel Client (`backend/internal/agent/frontdoor.go`)
- Runs as an in-process goroutine inside `rackops agent` without requiring external binaries like `cloudflared`.
- Generates and stores machine Ed25519 keys locally at `<data-dir>/frontdoor.key` (mode 0600).
- Dials outbound to the configured POP; automatically reconnects upon network interruptions.
- Forwards decrypted HTTP streams directly to Traefik on `127.0.0.1:8099`.

### 4. Server & Ingress Integration (`backend/internal/server`)
- **Traefik Configuration**:
  - Emits a dedicated `frontdoor` entrypoint bound to `127.0.0.1:8099`.
  - Removes `certResolver` from Front Door routers to avoid burning Let's Encrypt rate limits for vendor-owned domains.
  - Bypasses Traefik's blanket HTTP-to-HTTPS redirect rule on the `web` entrypoint, eliminating infinite redirect loops.
- **Management Handlers**:
  - Enable/disable endpoints with strict organization ownership and `ManageDomains` capability checks.
  - Background reconciler loop registers and syncs active service hostnames with the POP edge.

### 5. Vendor Administration (`internal-apps/crm`)
- `rackops-admin` CLI command (`serve --frontdoor`) running CRM support, analytics, and the tunnel edge in a unified vendor process.
- Front Door dashboard tab displaying POP health, certificate status, active tenant sessions, traffic meters, and suspension controls.

### 6. Dashboard User Interface (`dashboard/src`)
- **Network Page**: Setup card to configure workspace name, enable/disable Front Door, monitor connection state (connected, connecting, offline), inspect transport (QUIC vs TCP fallback badge), and track monthly bandwidth consumption.
- **App Detail Page**: Dedicated toggle on the Networking tab to generate and display the app's live public URL (`https://<app>-<workspace>.<zone>`).

---

## File Manifest

| Directory / File | Description |
|---|---|
| `apps/rackops-lite/backend/cmd/rackops-edge/` | Standalone POP edge binary entrypoint |
| `apps/rackops-lite/backend/internal/tunnel/` | Protocol wire format, frame parsers, transport managers (QUIC/Yamux), forwarders |
| `apps/rackops-lite/backend/internal/edge/` | Edge reverse proxy, ALPN listener, cert watcher, tenant registry, admin API |
| `apps/rackops-lite/backend/internal/agent/frontdoor.go` | In-process agent client lifecycle and stream proxying |
| `apps/rackops-lite/backend/internal/server/frontdoor.go` | Server-side enablement, tenant registration, hostname reconciler |
| `apps/rackops-lite/backend/internal/server/ingress.go` | Traefik entrypoint configuration and routing rules |
| `apps/rackops-lite/dashboard/src/pages/Network.tsx` | Front Door management and status UI |
| `apps/rackops-lite/dashboard/src/pages/AppDetail.tsx` | Per-application public URL toggle |
| `docs/FRONT_DOOR.md` | Core architecture and technical specification |
| `docs/FRONT_DOOR_SETUP.md` | Production setup and operations runbook |
| `docs/OVERALL_CHANGES.md` | Summary of architecture, modifications, and components |
