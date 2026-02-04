# Brain API + Task Runner
# Usage: just <recipe>

# Default recipe - show help
default:
    @just --list

# =============================================================================
# API Server
# =============================================================================

# Start API server (default port 4444)
api port="4444":
    BRAIN_API_PORT={{port}} bun run src/index.ts

# Start API server with hot reload
api-dev port="4444":
    BRAIN_API_PORT={{port}} bun run --watch src/index.ts

# =============================================================================
# Task Runner TUI
# =============================================================================

# Start TUI dashboard for a project
tui project api="http://localhost:4444" parallel="3" poll="30":
    BRAIN_API_URL={{api}} \
    BRAIN_MAX_PARALLEL={{parallel}} \
    BRAIN_POLL_INTERVAL={{poll}} \
    bun run src/runner/index.ts start {{project}} --tui

# Quick start TUI with faster polling (10s)
tui-fast project api="http://localhost:4444":
    BRAIN_API_URL={{api}} \
    BRAIN_POLL_INTERVAL=10 \
    bun run src/runner/index.ts start {{project}} --tui

# List tasks for a project
tasks project api="http://localhost:4444":
    BRAIN_API_URL={{api}} bun run src/runner/index.ts list {{project}}

# Show task tree for a project
tree project api="http://localhost:4444":
    BRAIN_API_URL={{api}} bun run src/runner/index.ts tree {{project}}

# =============================================================================
# Development
# =============================================================================

# Run tests
test:
    bun test

# Run tests in watch mode
test-watch:
    bun test --watch

# Type check
typecheck:
    bun run typecheck

# Lint
lint:
    bun run lint

# Format code
format:
    bun run format

# Run all checks (typecheck + lint + test)
check: typecheck lint test

# =============================================================================
# Combined Commands
# =============================================================================

# Start API in background and launch TUI (requires tmux)
start project port="4444":
    #!/usr/bin/env bash
    set -euo pipefail
    
    # Check if API is already running
    if curl -s "http://localhost:{{port}}/health" > /dev/null 2>&1; then
        echo "API already running on port {{port}}"
    else
        echo "Starting API server on port {{port}}..."
        tmux new-window -d -n brain-api "just api {{port}}"
        sleep 2
    fi
    
    echo "Starting TUI for project: {{project}}"
    just tui {{project}} "http://localhost:{{port}}"

# Stop all brain-runner processes
stop:
    pkill -f "brain-runner" || true
    pkill -f "src/index.ts" || true
    echo "Stopped all processes"

# =============================================================================
# Utilities
# =============================================================================

# Check API health
health api="http://localhost:4444":
    curl -s {{api}}/health | jq .

# Get ready tasks for a project
ready project api="http://localhost:4444":
    curl -s "{{api}}/api/v1/tasks/{{project}}/ready" | jq '.tasks[] | {id, title, status}'

# Get all tasks for a project
all project api="http://localhost:4444":
    curl -s "{{api}}/api/v1/tasks/{{project}}" | jq '.tasks[] | {id, title, status}'

# Clean up test task files
clean-tasks project:
    rm -f ~/docs/brain/projects/{{project}}/task/test-*.md
    rm -f ~/docs/brain/projects/{{project}}/task/cleanup-*.md
    rm -f ~/docs/brain/projects/{{project}}/task/depth-*.md
    rm -f ~/docs/brain/projects/{{project}}/task/base-*.md
    rm -f ~/docs/brain/projects/{{project}}/task/mid-*.md
    rm -f ~/docs/brain/projects/{{project}}/task/upper-*.md
    rm -f ~/docs/brain/projects/{{project}}/task/final-*.md
    echo "Cleaned up test tasks for {{project}}"
