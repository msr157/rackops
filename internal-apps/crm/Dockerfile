# ─────────────────────────────────────────────────────
# RackOps Admin — the vendor console: support, CRM and the Front Door
# tunnel edge in one image. Build from the monorepo root:
#   docker build -f internal-apps/crm/Dockerfile .
# ─────────────────────────────────────────────────────

# Stage 1: Build the dashboard with Vite (pnpm workspace)
FROM node:22-alpine AS dashboard

RUN corepack enable
WORKDIR /build

# Manifests first so the install layer caches across source changes.
COPY pnpm-workspace.yaml pnpm-lock.yaml package.json turbo.json ./
COPY internal-apps/crm/dashboard/package.json internal-apps/crm/dashboard/
COPY packages/api/package.json packages/api/
COPY packages/tsconfig/package.json packages/tsconfig/

RUN pnpm install --frozen-lockfile --filter @rackops/crm-dashboard...

COPY packages/ ./packages/
COPY internal-apps/crm/dashboard/ ./internal-apps/crm/dashboard/
# The build emits into ../backend/internal/server/dist — create the target.
COPY internal-apps/crm/backend/internal/server/dist/ ./internal-apps/crm/backend/internal/server/dist/

RUN pnpm --filter @rackops/crm-dashboard build

# ─────────────────────────────────────────────────────
# Stage 2: Build the Go binary with the embedded dashboard
# ─────────────────────────────────────────────────────
FROM golang:1.26-alpine AS backend

WORKDIR /build

# Module files first for dependency-layer caching.
# The admin module depends on the rackops module for the Front Door tunnel
# (pkg/tunnel, pkg/edge), resolved by a relative replace directive — so both
# module trees must be present, in the same relative layout as the repo.
COPY internal-apps/crm/backend/go.mod internal-apps/crm/backend/go.sum ./internal-apps/crm/backend/
COPY apps/rackops-lite/backend/go.mod apps/rackops-lite/backend/go.sum ./apps/rackops-lite/backend/
WORKDIR /build/internal-apps/crm/backend
RUN go mod download

COPY apps/rackops-lite/backend/ /build/apps/rackops-lite/backend/
COPY internal-apps/crm/backend/ /build/internal-apps/crm/backend/
# Overwrite the dist placeholder with the real dashboard build.
COPY --from=dashboard /build/internal-apps/crm/backend/internal/server/dist/ ./internal/server/dist/

# GOWORK=off: the image has no go.work, so the replace directive resolves
# the sibling module. modernc.org/sqlite is pure Go — no cgo required.
RUN GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /rackops-admin ./cmd/rackops-admin

# ─────────────────────────────────────────────────────
# Stage 3: Minimal runtime
# ─────────────────────────────────────────────────────
FROM alpine:3.21

LABEL maintainer="rackops"
LABEL org.opencontainers.image.title="rackops-admin"
LABEL org.opencontainers.image.description="RackOps Admin — vendor console: support, CRM and the Front Door tunnel edge"
LABEL org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S rackops && adduser -S -G rackops rackops \
    && mkdir -p /data && chown rackops:rackops /data

COPY --from=backend /rackops-admin /usr/local/bin/rackops-admin

# Bind 80/443 for the tunnel edge without running as root.
RUN apk add --no-cache libcap \
    && setcap cap_net_bind_service=+ep /usr/local/bin/rackops-admin \
    && apk del libcap

USER rackops
VOLUME /data
# 8091 console · 443 visitor HTTPS + tunnels (tcp+udp) · 80 redirect
EXPOSE 8091 443 443/udp 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q --spider http://127.0.0.1:8091/healthz || exit 1

ENTRYPOINT ["rackops-admin"]
CMD ["serve", "--listen", ":8091", "--data-dir", "/data"]
