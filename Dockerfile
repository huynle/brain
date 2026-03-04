# Brain API - Docker Image
# Multi-stage build using official Bun image

# --- Stage 1: Install dependencies ---
FROM oven/bun:1 AS deps
WORKDIR /app

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# --- Stage 2: Download zk CLI ---
FROM debian:bookworm-slim AS zk
ARG ZK_VERSION=0.15.2
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
RUN ARCH=$(dpkg --print-architecture) && \
    curl -fsSL "https://github.com/zk-org/zk/releases/download/v${ZK_VERSION}/zk-v${ZK_VERSION}-linux-${ARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin zk && \
    chmod +x /usr/local/bin/zk

# --- Stage 3: Production runtime ---
FROM oven/bun:1 AS runtime
WORKDIR /app

# Install curl for healthcheck
RUN apt-get update && apt-get install -y --no-install-recommends curl && rm -rf /var/lib/apt/lists/*

# Copy zk binary from builder
COPY --from=zk /usr/local/bin/zk /usr/local/bin/zk

COPY --from=deps /app/node_modules ./node_modules
COPY package.json ./
COPY tsconfig.json ./
COPY src/ ./src/

# Brain data volume mount point
RUN mkdir -p /data/brain

ENV BRAIN_DIR=/data/brain
ENV PORT=3333
ENV HOST=0.0.0.0

EXPOSE 3333

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -sf http://localhost:3333/api/v1/health || exit 1

CMD ["bun", "run", "src/index.ts"]
