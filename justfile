# Brain API Development
# Usage: just <recipe>

default:
    @just --list

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
    docker compose exec brain-api bun run src/cli/brain.ts token create --name {{name}}

# List API tokens
deploy-tokens:
    docker compose exec brain-api bun run src/cli/brain.ts token list

# Revoke an API token
deploy-revoke name:
    docker compose exec brain-api bun run src/cli/brain.ts token revoke {{name}}

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
