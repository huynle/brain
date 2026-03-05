# Brain API Development
# Usage: just <recipe>

default:
    @just --list

# =============================================================================
# Go Development
# =============================================================================

# Build all Go binaries
go-build:
    make build

# Run Go tests
go-test:
    make test

# Run Go tests with coverage
go-cover:
    make test-cover

# Run Go linter (golangci-lint)
go-lint:
    make lint

# Run go vet (static analysis)
go-vet:
    make typecheck

# Run all Go checks (vet + test + lint)
go-check:
    make check

# Format Go code
go-fmt:
    make fmt

# Tidy Go dependencies
go-tidy:
    make tidy

# Clean Go build artifacts
go-clean:
    make clean

# Run brain-api server (Go)
go-dev:
    go run ./cmd/brain-api

# =============================================================================
# Development
# =============================================================================

# Run API server (dev mode with hot reload)
dev:
    bun run --watch src/index.ts

# Run API server (production mode)
serve:
    bun run src/index.ts

# Run tests
test:
    bun test

# Run tests in watch mode
test-watch:
    bun test --watch

# Type check
typecheck:
    bun run typecheck

# Run all checks
check: typecheck test

# =============================================================================
# Ad-hoc Testing
# =============================================================================

# Test brain CLI
brain *args:
    bun run src/cli/brain.ts {{args}}

# Test brain-runner CLI
runner *args:
    bun run src/runner/index.ts {{args}}

# Check API health
health:
    curl -s http://localhost:3333/health | jq .

# =============================================================================
# Installation
# =============================================================================

# Build and install CLI tools to ~/.local/bin (replaces existing)
install: build-cli
    mkdir -p ~/.local/bin
    cp dist/brain ~/.local/bin/brain
    cp dist/brain-server ~/.local/bin/brain-server
    cp dist/brain-runner ~/.local/bin/brain-runner
    chmod +x ~/.local/bin/brain ~/.local/bin/brain-server ~/.local/bin/brain-runner
    @echo "Installed brain, brain-server, and brain-runner to ~/.local/bin"

# Build standalone CLI executables
build-cli:
    bun build src/cli/brain.ts --compile --outfile dist/brain
    bun build src/index.ts --compile --outfile dist/brain-server
    bun build src/runner/index.ts --compile --outfile dist/brain-runner

# Uninstall CLI tools
uninstall:
    rm -f ~/.local/bin/brain ~/.local/bin/brain-server ~/.local/bin/brain-runner
    @echo "Removed brain, brain-server, and brain-runner from ~/.local/bin"

# =============================================================================
# Tunnel (FRP)
# =============================================================================

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
