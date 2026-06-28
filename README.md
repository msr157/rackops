# RackOps

> Intelligent Infrastructure Orchestration — deploy with confidence.

[![CI Status](https://gitea.yourdomain.com/your-org/rackops/actions/workflows/ci.yml/badge.svg)](https://gitea.yourdomain.com/your-org/rackops/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## Overview

**RackOps** is a production-ready infrastructure orchestration platform delivered as a containerised static web application. It ships with a fully automated CI/CD pipeline spanning GitHub, Gitea, Docker, and Dokploy — meaning a single `git push` triggers the entire chain from source code to live production.

---

## Features

| Feature | Description |
|---|---|
| 🚀 Automated Provisioning | Zero-touch rack and resource provisioning |
| 🏥 Health Monitoring | Real-time health checks at `/health` |
| 🔒 Secrets Management | GitHub Secrets + Gitea Secrets integration |
| 🌿 Multi-Environment | `main`, `qa`, `development` branch strategy |
| 🐳 Docker First | Multi-stage build with nginx:alpine base |
| ⚡ CI/CD Pipeline | GitHub → Gitea → Docker → Dokploy |
| 🔁 Auto Mirror | GitHub auto-mirrors to Gitea on every push |

---

## Local Development

Clone and serve the static site locally:

```bash
git clone https://github.com/your-org/rackops.git
cd rackops

# Option 1: Python (zero dependencies)
python -m http.server 3000 --directory src

# Option 2: Docker
docker compose up --build
```

Open `http://localhost:3000` (Python) or `http://localhost:80` (Docker).

---

## Docker

### Build manually

```bash
docker build -t rackops:latest .
docker run -p 80:80 rackops:latest
```

### Using Docker Compose

```bash
docker compose up -d
```

The site is immediately available at `http://localhost`.

### Health check

```
GET /health  →  {"status":"ok","service":"rackops"}
```

---

## Environment Variables

Create a `.env` file based on the defaults below (never commit it):

```env
APP_ENV=production
APP_NAME=rackops
DOMAIN=rackops.yourdomain.com
DOKPLOY_DEPLOY_URL=https://dokploy.yourdomain.com/api/deploy/rackops
```

---

## Branch Strategy

| Branch | Purpose | Deploys to |
|---|---|---|
| `main` | Stable production code | Production (Dokploy) |
| `qa` | QA / acceptance testing | Staging |
| `development` | Active feature work | Dev environment |

**Rule:** Only pushes to `main` trigger deployment. PRs flow `development → qa → main`.

---

## CI/CD Architecture

```
Laptop
  │
  │  git push origin main
  ▼
GitHub (source of truth)
  │
  │  .github/workflows/mirror.yml
  │  Mirrors entire repo to Gitea
  ▼
Gitea
  │
  │  .gitea/workflows/ci.yml
  │  Self-hosted Gitea Actions Runner
  ▼
CI Pipeline
  ├─ 1. Checkout
  ├─ 2. Validate repository structure
  ├─ 3. Build Docker image
  ├─ 4. Verify Docker image
  ├─ 5. Run health check (HTTP 200 required)
  └─ 6. Trigger Dokploy webhook  ← only if all above pass
          │
          ▼
       Dokploy → Production
```

**Fail-safe:** If any CI step fails, the pipeline stops immediately and Dokploy is **never called**.

---

## Deployment — Dokploy

### Required Secrets

#### GitHub Secrets (for mirror workflow)

| Secret | Description |
|---|---|
| `GITEA_USER` | Gitea username |
| `GITEA_TOKEN` | Gitea personal access token |
| `GITEA_URL` | Gitea base URL, e.g. `https://gitea.yourdomain.com` |
| `GITEA_REPO` | Gitea repo path, e.g. `your-org/rackops` |

#### Gitea Secrets (for CI workflow)

| Secret | Description |
|---|---|
| `DOKPLOY_WEBHOOK_URL` | Full Dokploy deployment webhook URL |

### Deploy

```bash
git add .
git commit -m "Initial production bootstrap"
git push origin main
```

That's it. The full pipeline runs automatically.

---

## Project Structure

```
rackops/
│
├── .github/
│   └── workflows/
│       └── mirror.yml          # GitHub → Gitea mirror on push to main
│
├── .gitea/
│   └── workflows/
│       └── ci.yml              # Gitea CI: build → health check → deploy
│
├── src/
│   ├── index.html              # Landing page (HTML5)
│   ├── style.css               # Dark theme stylesheet (CSS3)
│   └── app.js                  # Interactivity (Vanilla JS)
│
├── docs/
│   └── branch-strategy.md      # Branching and workflow documentation
│
├── Dockerfile                  # Multi-stage production build
├── docker-compose.yml          # Dokploy-compatible compose file
├── nginx.conf                  # Production nginx configuration
├── .dockerignore               # Docker build context exclusions
├── .gitignore                  # Git exclusions
├── LICENSE                     # MIT License
└── README.md                   # This file
```

---

## License

[MIT](LICENSE) © 2026 RackOps Contributors
