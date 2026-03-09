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
# Installation
# =============================================================================

# Install Go binaries to GOPATH/bin
install:
    make install

# Check API health
health:
    curl -s http://localhost:3333/health | jq .

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
