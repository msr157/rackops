# ─────────────────────────────────────────────
# Stage 1: Build (nothing to compile for static)
# ─────────────────────────────────────────────
FROM alpine:3.20 AS builder

WORKDIR /build

# Copy source files
COPY src/ ./

# ─────────────────────────────────────────────
# Stage 2: Production nginx image
# ─────────────────────────────────────────────
FROM nginx:1.27-alpine

LABEL maintainer="rackops"
LABEL org.opencontainers.image.title="rackops"
LABEL org.opencontainers.image.description="RackOps — Intelligent Infrastructure Orchestration"
LABEL org.opencontainers.image.licenses="MIT"

# Remove default nginx content
RUN rm -rf /usr/share/nginx/html/*

# Copy built static files
COPY --from=builder /build/ /usr/share/nginx/html/

# Copy custom nginx configuration
COPY nginx.conf /etc/nginx/nginx.conf

# Expose HTTP port
EXPOSE 80

# nginx started by default CMD in base image
# Healthcheck via nginx's own status
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost/health || exit 1
