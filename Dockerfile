# Brain API - Multi-stage Docker Build
# Produces a minimal image with just the brain-api binary.

# Stage 1: Build
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X github.com/huynle/brain-api/internal/config.Version=${VERSION} -X github.com/huynle/brain-api/internal/config.Commit=${COMMIT}" \
    -o /bin/brain-api ./cmd/brain-api

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X github.com/huynle/brain-api/internal/config.Version=${VERSION} -X github.com/huynle/brain-api/internal/config.Commit=${COMMIT}" \
    -o /bin/brain-runner ./cmd/brain-runner

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X github.com/huynle/brain-api/internal/config.Version=${VERSION} -X github.com/huynle/brain-api/internal/config.Commit=${COMMIT}" \
    -o /bin/brain ./cmd/brain

# Stage 2: Runtime
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -S brain && adduser -S brain -G brain

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /bin/brain-api /usr/local/bin/brain-api
COPY --from=builder /bin/brain-runner /usr/local/bin/brain-runner
COPY --from=builder /bin/brain /usr/local/bin/brain

# Default brain directory
RUN mkdir -p /data/brain && chown -R brain:brain /data/brain
ENV BRAIN_DIR=/data/brain

USER brain

EXPOSE 3333

ENTRYPOINT ["brain-api"]
