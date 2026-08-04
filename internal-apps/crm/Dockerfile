# ─────────────────────────────────────────────────────
# RackOps CRM — build from the monorepo root:
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
COPY internal-apps/crm/backend/go.mod internal-apps/crm/backend/go.sum ./
RUN go mod download

COPY internal-apps/crm/backend/ ./
# Overwrite the dist placeholder with the real dashboard build.
COPY --from=dashboard /build/internal-apps/crm/backend/internal/server/dist/ ./internal/server/dist/

# modernc.org/sqlite is pure Go — no cgo required.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /crm ./cmd/crm

# ─────────────────────────────────────────────────────
# Stage 3: Minimal runtime
# ─────────────────────────────────────────────────────
FROM alpine:3.21

LABEL maintainer="rackops"
LABEL org.opencontainers.image.title="rackops-crm"
LABEL org.opencontainers.image.description="RackOps CRM — vendor-side support & telemetry console"
LABEL org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S crm && adduser -S -G crm crm \
    && mkdir -p /data && chown crm:crm /data

COPY --from=backend /crm /usr/local/bin/crm

USER crm
VOLUME /data
EXPOSE 8091

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q --spider http://127.0.0.1:8091/healthz || exit 1

ENTRYPOINT ["crm"]
CMD ["serve", "--listen", ":8091", "--data-dir", "/data"]
