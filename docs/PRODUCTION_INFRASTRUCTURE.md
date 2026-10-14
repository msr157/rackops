# Production Infrastructure & Cutover Architecture: Dokploy Sunset -> RackOps Native

## 1. Executive Summary & Fleet Architecture
All dependency on expired Azure VMs (`BetterRoad-VM`, `rackops-vm`, `rackops-demo`) and Dokploy / Docker Swarm has been terminated. The production cluster is deployed natively on **RackOps Lite** (`apps/rackops-lite`), utilizing a 3-server topology interconnected via NetBird WireGuard mesh:

```
                      Internet Traffic
                             │
            ┌────────────────┴────────────────┐
            │  Cloudflare Edge / Tunnel Pool  │
            │  Tunnel ID: 564e4c31-a321...    │
            └────────────────┬────────────────┘
                             │
              ┌──────────────┴──────────────┐
              ▼                             ▼
   ┌──────────────────────┐      ┌──────────────────────┐
   │ Cloud VPS (Static IP)│      │ mayank-mainframe     │
   │  eb2ebaa51935        │      │ 100.82.217.249       │
   │  100.82.107.228      │      │ Core i7 · 15GB RAM   │
   │  109.122.56.185      │      │                      │
   │ - Stalwart Mail (25) │      │ - RackOps Hub (:8090)│
   │ - Roundcube Webmail  │      │ - Edge Proxy (:80)   │
   │ - Redundant Edge (:80)      │ - Backup Vault(/data)│
   └──────────┬───────────┘      └──────────┬───────────┘
              │                             │
              │     NetBird WireGuard Mesh  │
              └──────────────┬──────────────┘
                             │
                             ▼
              ┌────────────────────────┐
              │ rogue-server-precision │
              │ 100.82.188.57 (LAN .12)│
              │ Dual Xeon 72T · 60GB   │
              │ Quadro GPU · 468G NVMe │
              │                        │
              │ - PostgreSQL 16 (NVMe) │
              │ - BetterRoads Backend  │
              │ - BetterRoads Frontend │
              │ - BetterRoads Admin    │
              │ - RogX Backend (:3005) │
              │ - RogX Frontend (:8005)│
              └────────────────────────┘
```

---

## 2. Live Server Inventory & Node Registry

| Node Name | NetBird IP / Host | Hardware Profile | RackOps Agent Status | Active Workloads |
| :--- | :--- | :--- | :--- | :--- |
| **`eb2ebaa51935`** (`mail.betterroads.org`) | `100.82.107.228`<br>`109.122.56.185` (Static) | 2 vCPU, 1.9 GB RAM, 30 GB SSD | **Node ID**: `de3f00360ed9`<br>**Status**: `online` | Stalwart Mail (`25, 465, 587, 993, 8080`), Roundcube (`8088`), `edge-proxy` (Nginx port 80), `cloudflared-vps`. |
| **`mayank-mainframe-server`** | `100.82.217.249`<br>`192.168.1.7` (LAN) | Intel Core i7-4785T (8T), 15 GB RAM, 232 GB SSD + 458 GB HDD (`/data`) | **Node ID**: `be4e755d24bb`<br>**Status**: `online` | `rackops server` v0.7.2 control plane (`:8090`), `edge-proxy` (Nginx port 80), `cloudflared-mainframe`, backup storage vault (`/data/backups/postgres`). |
| **`rogue-server-precision-tower-7810`** | `100.82.188.57`<br>`192.168.1.12` (LAN) | Dual Intel Xeon E5-2696 v3 (72T), 60 GB ECC RAM, 468 GB NVMe SSD, Quadro M2000 | **Node ID**: `1febff435a35`<br>**Status**: `online` | `postgres-production` (:5432, 8GB shared buffers), `betterroads-backend` (:3000), `betterroads-frontend` (:8000), `betterroads-dashboard` (:8001), `rogx-backend` (:3005), `rogx-frontend` (:8005). |

---

## 3. Database Architecture & Disaster Recovery

### PostgreSQL 16 on Precision Tower NVMe
- **Container**: `postgres-production`
- **Tuning**:
  - `shared_buffers = 8GB`
  - `effective_cache_size = 32GB`
  - `maintenance_work_mem = 2GB`
  - `max_worker_processes = 32`
- **Hosted Schemas / Databases**:
  - `betterroads_db`: 15 tables (Drizzle ORM, device-identity-v2 schema)
  - `rogx_db`: 5 tables
  - `rackops_db`: Production telemetry & fleet state
  - `appdb`: Internal applications

### Automated Synchronized Backups
- **Script**: `/home/rogue-server/services/backup-postgres.sh`
- **Crontab**: `0 */6 * * *` (Runs every 6 hours)
- **Destination**: Compressed gzip archives generated locally on Precision Tower, then synced over passwordless SSH to `mayank@100.82.217.249:/data/backups/postgres/`.

---

## 4. Decommissioning & Sunset Log
1. **Docker Swarm Demolition**:
   - `pg-ha` and `redis-ha` swarm stacks removed from all nodes.
   - `BetterRoad-VM` and `rackops-vm` forced out of Docker Swarm (`docker swarm leave --force`).
   - `mayank-mainframe-server` decoupled from Swarm.
2. **Container Retirement on Azure**:
   - All legacy app containers, Dokploy services, and swarm registries stopped.
   - Azure VMs are now completely dormant with zero traffic.
3. **Cloudflare Tunnel Routing**:
   - Scaled legacy Swarm tunnel connector to 0 (`docker service scale cloudflare-tunnel=0`).
   - Tunnel version 10 routes directly to `edge-proxy` on Mainframe and VPS, proxying traffic seamlessly to Precision Tower.

---

## 5. Verified Live Endpoints (100% Operational)

| Endpoint | Ingress Hostname | Upstream Destination | HTTP Status |
| :--- | :--- | :--- | :--- |
| **RackOps Marketing Site** | `rackops.in`, `www.rackops.in` | Mainframe & VPS `/var/www/rackops` | **200 OK** |
| **RackOps Control Console** | `dash.rackops.in` | Mainframe `100.82.217.249:8090` | **200 OK** |
| **BetterRoads Public App** | `betterroads.org`, `www.betterroads.org` | Precision Tower `100.82.188.57:8000` | **200 OK** |
| **BetterRoads Admin Dashboard** | `admin.betterroads.org` | Precision Tower `100.82.188.57:8001` | **200 OK** |
| **BetterRoads API** | `betterroads.org/api/health` | Precision Tower `100.82.188.57:3000` | **200 OK** (`device-identity-v2`) |
| **RogX Platform** | `rogx.in` | Precision Tower `100.82.188.57:8005` & `:3005` | **200 OK** |
| **Roundcube Webmail** | `webmail.betterroads.org`, `webmail.rackops.in` | VPS `100.82.107.228:8088` | **200 OK** |
| **Stalwart Mail Core** | `mail.betterroads.org` | VPS `100.82.107.228:8080` | **200 OK** |
