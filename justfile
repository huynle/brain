# Brain API Development
# Usage: just <recipe>

default:
    @just --list

# =============================================================================
# Variables
# =============================================================================

binary_dir := "bin"
module := "github.com/huynle/brain-api"
version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_time := `date -u '+%Y-%m-%dT%H:%M:%SZ'`
ldflags := "-s -w -X " + module + "/internal/config.Version=" + version + " -X " + module + "/internal/config.Commit=" + commit + " -X " + module + "/internal/config.BuildTime=" + build_time
cmds := "brain"

# =============================================================================
# Go Development
# =============================================================================

# Build all Go binaries
build:
    @mkdir -p {{ binary_dir }}
    @for cmd in {{ cmds }}; do \
        echo "Building $cmd..."; \
        go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/$cmd ./cmd/$cmd; \
    done
    @echo "Build complete: {{ binary_dir }}/"

# Build a specific binary (e.g., just build-one brain-api)
build-one cmd:
    @mkdir -p {{ binary_dir }}
    go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/{{ cmd }} ./cmd/{{ cmd }}

# Run Go tests
test:
    go test ./... -v -count=1

# Run Go tests with coverage
test-cover:
    go test ./... -v -count=1 -coverprofile=coverage.out -covermode=atomic
    go tool cover -func=coverage.out
    @echo ""
    @echo "HTML report: go tool cover -html=coverage.out -o coverage.html"

# Run Go tests in short mode (skip long-running tests)
test-short:
    go test ./... -v -short -count=1

# Run golangci-lint
lint:
    @if command -v golangci-lint >/dev/null 2>&1; then \
        golangci-lint run ./...; \
    else \
        echo "golangci-lint not found. Install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2"; \
        exit 1; \
    fi

# Run go vet (static analysis)
vet:
    go vet ./...

# Run all checks (vet + test + lint)
check: vet test lint

# Format Go code
fmt:
    go fmt ./...
    @if command -v goimports >/dev/null 2>&1; then \
        goimports -w .; \
    fi

# Tidy Go dependencies
tidy:
    go mod tidy

# Clean build artifacts
clean:
    rm -rf {{ binary_dir }} coverage.out coverage.html
    go clean -cache -testcache

# Run brain API server (development)
dev:
    go run ./cmd/brain dev

# =============================================================================
# Installation & Release
# =============================================================================

# Build and install binaries to GOPATH/bin
install: build
    @for cmd in {{ cmds }}; do \
        go install -ldflags '{{ ldflags }}' ./cmd/$cmd; \
    done
    @echo "Installed to $(go env GOPATH)/bin"

# Cross-compile for release (linux/darwin/windows, amd64/arm64)
release:
    @mkdir -p {{ binary_dir }}/release
    @for cmd in {{ cmds }}; do \
        echo "Cross-compiling $cmd..."; \
        GOOS=linux   GOARCH=amd64 go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/release/$cmd-linux-amd64 ./cmd/$cmd; \
        GOOS=linux   GOARCH=arm64 go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/release/$cmd-linux-arm64 ./cmd/$cmd; \
        GOOS=darwin  GOARCH=amd64 go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/release/$cmd-darwin-amd64 ./cmd/$cmd; \
        GOOS=darwin  GOARCH=arm64 go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/release/$cmd-darwin-arm64 ./cmd/$cmd; \
        GOOS=windows GOARCH=amd64 go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/release/$cmd-windows-amd64.exe ./cmd/$cmd; \
    done
    @echo "Release binaries: {{ binary_dir }}/release/"

# Build Docker image
docker:
    docker build -t brain-api:{{ version }} .
    @echo "Built: brain-api:{{ version }}"

# =============================================================================
# Health & Tunnel
# =============================================================================

# Check API health
health:
    curl -s http://localhost:3333/health | jq .

# Start FRP tunnel to expose brain MCP at https://BRAIN_TUNNEL_HOST
tunnel:
    @echo "Starting FRP tunnel to https://BRAIN_TUNNEL_HOST..."
    @echo "Make sure brain-server is running (just dev)"
    frpc -c ~/.config/frp/brain-mcp.toml

# Check tunnel status
tunnel-status:
    @echo "Checking tunnel connectivity..."
    @curl -sf https://BRAIN_TUNNEL_HOST/api/v1/health && echo "Tunnel OK" || echo "Tunnel not connected"

# Show tunnel config
tunnel-config:
    @cat ~/.config/frp/brain-mcp.toml

# =============================================================================
# Docker Deployment
# =============================================================================

# Deploy brain-api via docker-compose (production with auth)
deploy:
    @if [ ! -f .env ]; then \
        echo "No .env found. Creating from .env.deploy.example..."; \
        cp .env.deploy.example .env; \
        echo ""; \
        echo "Created .env with ENABLE_AUTH=true."; \
        echo "Edit .env to configure (OAUTH_PIN, CORS_ORIGIN, etc.), then run 'just deploy' again."; \
        exit 1; \
    fi
    docker compose up -d --build
    @echo ""
    @echo "Waiting for brain-api to start..."
    @for i in 1 2 3 4 5 6; do \
        sleep 2; \
        if curl -sf http://localhost:$${PORT:-3333}/api/v1/health > /dev/null 2>&1; then \
            echo "Brain API is running at http://localhost:$${PORT:-3333}"; \
            echo ""; \
            echo "Next steps:"; \
            echo "  1. Create an API token:  just deploy-token my-token"; \
            echo "  2. Check status:         just deploy-status"; \
            echo "  3. View logs:            just deploy-logs"; \
            exit 0; \
        fi; \
        printf "."; \
    done; \
    echo ""; \
    echo "Container started but health check not responding yet."; \
    echo "Check logs: just deploy-logs"

# Create an API token in the running container
deploy-token name:
    docker compose exec brain-api bun run src/cli/brain.ts token create --name {{ name }}

# List API tokens
deploy-tokens:
    docker compose exec brain-api bun run src/cli/brain.ts token list

# Revoke an API token
deploy-revoke name:
    docker compose exec brain-api bun run src/cli/brain.ts token revoke {{ name }}

# Show deployment status and health
deploy-status:
    @docker compose ps
    @echo ""
    @curl -sf http://localhost:$${PORT:-3333}/api/v1/health | python3 -m json.tool 2>/dev/null || echo "Not responding"

# Tail deployment logs
deploy-logs:
    docker compose logs -f brain-api

# Stop deployment (data preserved)
deploy-stop:
    docker compose down

# Rebuild and restart deployment
deploy-restart:
    docker compose up -d --build

# Stop and remove all data (destructive!)
deploy-nuke:
    @echo "WARNING: This will stop containers AND delete all brain data."
    @printf "Are you sure? [y/N] " && read confirm && [ "$$confirm" = "y" ] || exit 1
    docker compose down -v
    @echo "All containers and data removed."
