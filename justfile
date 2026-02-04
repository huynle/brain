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

# Install CLI tools globally (brain, do-work)
install:
    bun link

# Uninstall CLI tools
uninstall:
    bun unlink
