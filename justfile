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

# Test do-work CLI
do-work *args:
    bun run src/cli/do-work.ts {{args}}

# Test runner directly
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
    cp dist/do-work ~/.local/bin/do-work
    chmod +x ~/.local/bin/brain ~/.local/bin/brain-server ~/.local/bin/do-work
    @echo "Installed brain, brain-server, and do-work to ~/.local/bin"

# Build standalone CLI executables
build-cli:
    bun build src/cli/brain.ts --compile --outfile dist/brain
    bun build src/index.ts --compile --outfile dist/brain-server
    bun build src/cli/do-work.ts --compile --outfile dist/do-work

# Uninstall CLI tools
uninstall:
    rm -f ~/.local/bin/brain ~/.local/bin/brain-server ~/.local/bin/do-work
    @echo "Removed brain, brain-server, and do-work from ~/.local/bin"
