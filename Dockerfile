# Brain - Multi-stage Docker Build
# Produces a minimal image with the unified brain binary and the embedded PWA.

# Stage 1: Build the web PWA (output → /internal/webui/dist, embedded by Go)
FROM node:22-alpine AS webbuilder

WORKDIR /web

# Cache npm deps
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund

# Build — vite's outDir resolves to /internal/webui/dist (one level up from /web)
COPY web/ ./
RUN npm run build

# Stage 2: Build the Go binary with the PWA embedded
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source, then overlay the freshly built web assets so go:embed picks them up
COPY . .
COPY --from=webbuilder /internal/webui/dist ./internal/webui/dist

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X github.com/huynle/brain-api/internal/config.Version=${VERSION} -X github.com/huynle/brain-api/internal/config.Commit=${COMMIT}" \
    -o /bin/brain ./cmd/brain

# Stage 2: Runtime
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -S brain && adduser -S brain -G brain

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/brain /usr/local/bin/brain

# Default brain directory
RUN mkdir -p /data/brain && chown -R brain:brain /data/brain
ENV BRAIN_DIR=/data/brain

USER brain

EXPOSE 3333

ENTRYPOINT ["brain", "api"]
