# ─────────────────────────────────────────────────────
# Stage 1: Build static site with Vite
# ─────────────────────────────────────────────────────
FROM node:22-alpine AS builder

WORKDIR /build

COPY package.json package-lock.json* ./
RUN npm install

COPY index.html vite.config.ts tsconfig.json tsconfig.node.json ./
COPY src/ ./src/

RUN npm run build

# ─────────────────────────────────────────────────────
# Stage 2: Production runtime (nginx on Alpine)
# ─────────────────────────────────────────────────────
FROM nginx:1.27-alpine

LABEL maintainer="rackops"
LABEL org.opencontainers.image.title="rackops"
LABEL org.opencontainers.image.description="RackOps — The Infrastructure Operating System"
LABEL org.opencontainers.image.licenses="MIT"

RUN rm -rf /usr/share/nginx/html/*

COPY --from=builder /build/dist/ /usr/share/nginx/html/
COPY nginx.conf /etc/nginx/nginx.conf

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fs http://127.0.0.1/health || exit 1
