# Go Rewrite — Cutover Guide

This document covers the complete transition from the TypeScript Brain API to the Go rewrite. It includes pre-cutover validation, build/release procedures, step-by-step cutover, rollback, and post-cutover cleanup.

---

## Table of Contents

1. [Pre-Cutover Checklist](#1-pre-cutover-checklist)
2. [Build & Release](#2-build--release)
3. [Cutover Procedure](#3-cutover-procedure)
4. [Rollback Procedure](#4-rollback-procedure)
5. [Post-Cutover Cleanup](#5-post-cutover-cleanup)
6. [Known Differences & Limitations](#6-known-differences--limitations)
7. [Environment Variable Reference](#7-environment-variable-reference)
8. [MCP Client Configuration](#8-mcp-client-configuration)

---

## 1. Pre-Cutover Checklist

Complete **every** item before proceeding to cutover. Each item includes the exact command to run and what success looks like.

### Tests

- [ ] **Go unit tests pass**
  ```bash
  make test
  # Expected: all tests pass, exit code 0
  ```

- [ ] **Go integration tests pass**
  ```bash
  go test ./tests/integration/ -v -count=1
  # Expected: 47+ tests pass (api_compat_test.go)
  ```

- [ ] **Performance benchmarks run**
  ```bash
  go test -bench=. -benchmem ./internal/storage/ ./internal/api/ ./internal/service/
  # Expected: 66 benchmarks complete with ns/op, B/op, allocs/op
  ```

  Or use the benchmark script for formatted output:
  ```bash
  ./scripts/benchmark-compare.sh
  ```

  To save a baseline for future comparison:
  ```bash
  ./scripts/benchmark-compare.sh --save benchmarks-baseline.txt
  ```

### Build Validation

- [ ] **GoReleaser config validates**
  ```bash
  goreleaser check
  # Expected: "config is valid"
  ```

- [ ] **All 4 binaries build**
  ```bash
  make build
  # Expected: bin/brain-api, bin/brain-runner, bin/brain, bin/brain-mcp
  ls -la bin/
  ```

- [ ] **Cross-platform binaries compile**
  ```bash
  make release
  # Expected: 20 binaries in bin/release/ (4 binaries × 5 platforms)
  ls bin/release/
  ```

- [ ] **Docker image builds**
  ```bash
  make docker
  # Expected: brain-api:<version> image created
  docker images | grep brain-api
  ```

- [ ] **GoReleaser snapshot builds**
  ```bash
  goreleaser release --snapshot --clean
  # Expected: archives + Docker image created in dist/
  ```

### Functional Validation

- [ ] **API compatibility script passes** (requires both servers running)
  ```bash
  # Terminal 1: Start Go server
  BRAIN_DIR=/path/to/brain PORT=3000 ./bin/brain-api

  # Terminal 2: Start TS server
  BRAIN_DIR=/path/to/brain PORT=3333 bun run dev

  # Terminal 3: Run comparison
  ./scripts/validate-api-compat.sh http://localhost:3000 http://localhost:3333
  # Expected: "API compatibility check PASSED" (warnings OK for known differences)
  ```

- [ ] **SSE streaming verified**
  ```bash
  # Start Go server
  BRAIN_DIR=/path/to/brain ./bin/brain-api

  # In another terminal, open SSE stream
  curl -N http://localhost:3000/api/v1/tasks/stream
  # Expected: SSE events appear when tasks change (create/update an entry to trigger)
  ```

- [ ] **Existing brain.db loads correctly in Go server**
  ```bash
  # Point Go server at an existing brain directory
  BRAIN_DIR=~/.brain ./bin/brain-api
  # Expected: "indexing complete" in logs, entries accessible via API
  curl http://localhost:3000/api/v1/entries | jq '.entries | length'
  ```

- [ ] **MCP tools work with AI agents**
  ```bash
  # Test brain-mcp starts and responds to MCP protocol
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | ./bin/brain-mcp
  # Expected: JSON-RPC response with server capabilities
  ```

---

## 2. Build & Release

### Build Binaries Locally

```bash
# Build all 4 binaries to bin/
make build

# Build a specific binary
make build-brain-api
make build-brain-runner
make build-brain-mcp
make build-brain

# Install to $GOPATH/bin
make install
```

The build embeds version info via ldflags:
- `Version` — git tag or "dev"
- `Commit` — short git SHA
- `BuildTime` — UTC build timestamp

### Create a Release with GoReleaser

```bash
# Tag the release
git tag -a v1.0.0 -m "Go rewrite: initial release"
git push origin v1.0.0

# Build release (CI does this automatically via .github/workflows/go.yml)
goreleaser release --clean

# Test locally without publishing
goreleaser release --snapshot --clean
```

GoReleaser produces:
- **Archives**: `brain-api_<version>_<os>_<arch>.tar.gz` (`.zip` for Windows)
- **Checksums**: `checksums.txt` (SHA-256)
- **Docker image**: `ghcr.io/huynle/brain-api` with tags `latest`, `v1`, `v1.0`, `v1.0.0`
- **GitHub Release**: with changelog grouped by feat/fix/perf

Platforms built:
| OS | Arch | brain-api | brain-runner | brain | brain-mcp |
|----|------|-----------|-------------|-------|-----------|
| Linux | amd64 | ✓ | ✓ | ✓ | ✓ |
| Linux | arm64 | ✓ | ✓ | ✓ | ✓ |
| macOS | amd64 | ✓ | ✓ | ✓ | ✓ |
| macOS | arm64 | ✓ | ✓ | ✓ | ✓ |
| Windows | amd64 | ✓ | ✓ | ✓ | ✓ |

### Docker Image

```bash
# Build locally
make docker
# → brain-api:<version>

# Build with custom version
docker build -t brain-api:custom --build-arg VERSION=1.0.0 --build-arg COMMIT=$(git rev-parse --short HEAD) .

# Run
docker run -d \
  -p 3000:3333 \
  -v ~/.brain:/data/brain \
  -e BRAIN_DIR=/data/brain \
  brain-api:latest
```

The Docker image:
- Base: `alpine:3.20` (~7MB)
- Includes all 4 binaries in `/usr/local/bin/`
- Runs as non-root user `brain`
- Default `BRAIN_DIR=/data/brain`
- Exposes port `3333`
- Entrypoint: `brain-api`

---

## 3. Cutover Procedure

### Step 1: Stop the TypeScript Server

```bash
# If running via bun
pkill -f "bun run.*src/index.ts" || true

# If running via systemd
sudo systemctl stop brain-api

# If running via Docker
docker stop brain-api-ts
```

Verify it's stopped:
```bash
curl -s http://localhost:3333/api/v1/health || echo "TS server stopped"
```

### Step 2: Verify Brain Data Directory

```bash
# Check the brain directory exists and has content
ls -la ~/.brain/
# Expected: markdown files, possibly .zk/ directory, possibly living-brain.db

# Note the DB file situation:
# - TS uses: $BRAIN_DIR/living-brain.db
# - Go uses: $BRAIN_DIR/.zk/brain.db
#
# The Go server creates .zk/brain.db on first start and indexes
# all markdown files from BRAIN_DIR. It does NOT read living-brain.db.
# Both can coexist safely — they are separate files.
```

### Step 3: Start the Go Server

```bash
# Option A: Direct binary
BRAIN_DIR=~/.brain PORT=3000 ./bin/brain-api

# Option B: With environment file
cp .env.example .env
# Edit .env: set BRAIN_DIR, PORT, etc.
./bin/brain-api

# Option C: Docker
docker run -d \
  --name brain-api \
  -p 3000:3333 \
  -v ~/.brain:/data/brain \
  -e BRAIN_DIR=/data/brain \
  brain-api:latest

# Option D: systemd (create a unit file)
# See example below
```

Watch the startup logs:
```bash
# Expected output:
# time=... level=INFO msg="indexing brain directory" dir=/home/user/.brain
# time=... level=INFO msg="indexing complete" added=150 updated=0 deleted=0 skipped=0 errors=0 duration=1.2s
# time=... level=INFO msg="starting brain-api" addr=0.0.0.0:3000 brain_dir=/home/user/.brain db_path=/home/user/.brain/.zk/brain.db auth_enabled=false
```

**Important**: The Go server defaults to port `3000`, not `3333`. Set `PORT=3333` if you need the same port as the TS server.

### Step 4: Verify Endpoints Respond

```bash
BASE=http://localhost:3000

# Health check
curl -s "$BASE/api/v1/health" | jq .
# Expected: {"status":"ok", ...}

# List entries
curl -s "$BASE/api/v1/entries" | jq '.entries | length'
# Expected: number matching your brain entries

# Search
curl -s -X POST "$BASE/api/v1/search" \
  -H "Content-Type: application/json" \
  -d '{"query":"test"}' | jq '.results | length'

# Stats
curl -s "$BASE/api/v1/stats" | jq .

# Tasks
curl -s "$BASE/api/v1/tasks" | jq .
```

### Step 5: Verify SSE Streaming

```bash
# Open SSE stream (should stay open)
curl -N "$BASE/api/v1/tasks/stream" &
SSE_PID=$!

# In another terminal, create an entry to trigger events
curl -s -X POST "$BASE/api/v1/entries" \
  -H "Content-Type: application/json" \
  -d '{"type":"scratch","title":"SSE Test","content":"Testing SSE streaming"}'

# Check that SSE events appeared in the first terminal
# Then clean up
kill $SSE_PID
```

### Step 6: Verify MCP Tools Connect

Update your MCP client configuration to point to the Go binary:

```json
{
  "mcpServers": {
    "brain": {
      "command": "/path/to/bin/brain-mcp",
      "env": {
        "BRAIN_API_URL": "http://localhost:3000"
      }
    }
  }
}
```

Test with your AI editor (Claude Code, OpenCode, etc.):
- Try `brain_save` — should create an entry
- Try `brain_search` — should return results
- Try `brain_list` — should list entries
- Try `brain_tasks` — should list tasks

### Step 7: Verify Task Runner (if used)

```bash
# Start the Go task runner with TUI
./bin/brain-runner my-project

# Or start for all projects
./bin/brain-runner

# Or start in background
./bin/brain-runner start my-project -b
```

### Step 8: Monitor for Errors

Watch the Go server logs for the first few hours:
```bash
# If running directly
BRAIN_DIR=~/.brain LOG_LEVEL=debug ./bin/brain-api 2>&1 | tee brain-api.log

# If running via Docker
docker logs -f brain-api

# Check for common issues:
grep -i "error\|panic\|fatal" brain-api.log
```

### Example systemd Unit File

```ini
[Unit]
Description=Brain API (Go)
After=network.target

[Service]
Type=simple
User=brain
Group=brain
ExecStart=/usr/local/bin/brain-api
Environment=BRAIN_DIR=/home/brain/.brain
Environment=PORT=3000
Environment=HOST=0.0.0.0
Environment=LOG_LEVEL=info
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

---

## 4. Rollback Procedure

If issues arise after cutover, switch back to the TypeScript server.

### Step 1: Stop the Go Server

```bash
# Direct process
pkill brain-api

# Docker
docker stop brain-api

# systemd
sudo systemctl stop brain-api
```

### Step 2: Start the TypeScript Server

```bash
cd /path/to/brain-api
bun run dev
# Or: bun run start
```

### Data Compatibility

- **SQLite schema is identical** between Go and TS — both use the same `notes`, `notes_fts`, `links`, `collections`, `entry_meta`, `generated_tasks` tables.
- **Go writes to** `$BRAIN_DIR/.zk/brain.db`
- **TS reads from** `$BRAIN_DIR/living-brain.db`
- These are **separate files**. Changes made while running the Go server are in `.zk/brain.db` and will NOT appear in the TS server's `living-brain.db`.
- **Markdown files are the source of truth**. The Go server re-indexes all markdown files on startup, so any entries created via the Go API (which writes markdown files) will be picked up by the TS server after it re-indexes.
- **Task data** (entry_meta, generated_tasks) in `.zk/brain.db` will not be visible to the TS server unless migrated.

### If You Need to Merge Data

If tasks or metadata were created in the Go server's `.zk/brain.db` that you need in the TS server's `living-brain.db`:

```bash
# Option 1: The Go server writes markdown files — TS will re-index them
# Just restart the TS server and it will pick up new/changed markdown files.

# Option 2: For task metadata, manually copy between SQLite databases
# (advanced — only if needed)
sqlite3 ~/.brain/.zk/brain.db ".dump entry_meta" | sqlite3 ~/.brain/living-brain.db
```

---

## 5. Post-Cutover Cleanup

After confirming the Go server is stable (recommended: run for at least 1 week), clean up the TypeScript codebase.

### Remove TypeScript Source & Dependencies

```bash
# Remove TS source code
rm -rf src/

# Remove TS dependencies
rm -rf node_modules/
rm -f bun.lock package.json tsconfig.json

# Remove TS-specific config
rm -f biome.json  # if exists
```

### Remove TS Test Files

```bash
# Go integration tests replace TS ones
rm -f tests/integration/*.test.ts
rm -f tests/integration/fixtures.ts
rm -f tests/integration/helpers.ts
rm -f tests/integration/helpers.test.ts
```

### Update CLAUDE.md

Replace the TypeScript-focused CLAUDE.md with Go-only instructions:

```markdown
# CLAUDE.md

## Project Overview

Brain API is a REST service for AI agent memory and knowledge management,
with an integrated task queue processor. Built with Go.

## Key Commands

\`\`\`bash
# Development
go run ./cmd/brain-api          # Start server
make build                      # Build all binaries
make test                       # Run all tests
go vet ./...                    # Static analysis

# Task Runner
./bin/brain-runner my-project   # TUI dashboard
./bin/brain-runner list all     # List all projects

# MCP Server
./bin/brain-mcp                 # Start MCP server (stdin/stdout)

# Benchmarks
./scripts/benchmark-compare.sh  # Run all benchmarks
\`\`\`

## Architecture

### Binaries (`cmd/`)
- `brain-api` — HTTP REST server (port 3000 default)
- `brain-runner` — Task queue processor with TUI
- `brain-mcp` — MCP server for AI editors
- `brain` — CLI tool (stub)

### Internal Packages (`internal/`)
- `api/` — HTTP handlers and router (chi)
- `config/` — Environment-based configuration
- `indexer/` — Markdown file indexer with fsnotify watcher
- `mcp/` — MCP protocol server and tool definitions
- `realtime/` — SSE hub for real-time events
- `runner/` — Task runner, executor, process manager
- `service/` — Business logic (brain, task, runner, monitor)
- `storage/` — SQLite storage layer
- `tui/` — Bubble Tea TUI for task runner
- `types/` — Shared type definitions

### Public Packages (`pkg/`)
- Shared utilities (if any)

## Testing

\`\`\`bash
make test                                    # All tests
go test ./internal/storage/ -v               # Storage tests
go test ./tests/integration/ -v              # Integration tests
go test -bench=. -benchmem ./internal/...    # Benchmarks
\`\`\`
```

### Update CI Workflow

The Go CI workflow (`.github/workflows/go.yml`) is already in place. Remove any TS-specific CI:

```bash
# If there's a separate TS workflow
rm -f .github/workflows/ci.yml      # or whatever the TS workflow was named
rm -f .github/workflows/test.yml
```

### Archive the TS Codebase (Optional)

Before deleting, you may want to tag the last TS version:

```bash
git tag -a ts-final -m "Last TypeScript version before Go rewrite"
git push origin ts-final
```

---

## 6. Known Differences & Limitations

### API Behavior Differences

| Behavior | TypeScript | Go | Impact |
|----------|-----------|-----|--------|
| DELETE response | `200` with JSON body | `204` No Content | MCP tools handle both; clients checking response body on DELETE need updating |
| Default port | `3000` | `3000` | Same — both default to 3000 |
| DB file path | `$BRAIN_DIR/living-brain.db` | `$BRAIN_DIR/.zk/brain.db` | Separate files; Go re-indexes markdown on startup |

### Missing Features in Go

| Feature | Status | Notes |
|---------|--------|-------|
| `brain` CLI | Stub only | Prints usage message and exits. Token management, migration, init not implemented. |
| OAuth/PKCE auth | Not implemented | `ENABLE_AUTH` + `API_KEY` work; OAuth consent flow does not. |
| Multi-tenant mode | Not implemented | `ENABLE_TENANTS` env var is not supported. |
| Built-in TLS | Not implemented | Use a reverse proxy (Caddy, nginx) for TLS termination. |
| Migration tooling | Not implemented | TS has `brain migrate` to import from `living-brain.db` / `.zk/zk.db`. Go re-indexes markdown files instead. |
| Doctor/diagnostics | Not implemented | TS has `brain doctor` for health checks. |

### Configuration Differences

| Setting | TypeScript | Go |
|---------|-----------|-----|
| `BRAIN_DIR` default | `~/.brain` | `~/.brain` |
| `PORT` default | `3000` | `3000` |
| `HOST` default | `0.0.0.0` | `0.0.0.0` |
| `LOG_LEVEL` | `debug/info/warn/error` | `debug/info/warn/error` |
| `ENABLE_AUTH` | Supported | Supported |
| `API_KEY` | Supported | Supported |
| `OAUTH_PIN` | Supported | **Not supported** |
| `ENABLE_TLS` | Supported | **Not supported** |
| `TLS_KEY`/`TLS_CERT` | Supported | **Not supported** |
| `ENABLE_TENANTS` | Supported | **Not supported** |
| `CORS_ORIGIN` | Supported | Supported |

### Task Runner Differences

| Feature | TypeScript (Bun + Ink) | Go (Bubble Tea) |
|---------|----------------------|-----------------|
| TUI framework | Ink (React for CLI) | Bubble Tea |
| Multi-project mode | ✓ | ✓ |
| Background daemon | ✓ | ✓ |
| Glob filters (`-i`/`-e`) | ✓ | ✓ |
| SSE task streaming | ✓ | ✓ |
| Tmux dashboard | ✓ | ✓ (via `--dashboard`) |

---

## 7. Environment Variable Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `BRAIN_DIR` | `~/.brain` | Brain data storage directory |
| `PORT` | `3000` | Server listen port |
| `HOST` | `0.0.0.0` | Server bind address |
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `ENABLE_AUTH` | `false` | Enable API key authentication |
| `API_KEY` | — | API key for authentication |
| `CORS_ORIGIN` | `*` | Allowed CORS origin |
| `BRAIN_API_URL` | `http://localhost:3333` | Brain API URL (for `brain-mcp` and `brain-runner`). Note: if Go server runs on port 3000, set this to `http://localhost:3000`. |
| `BRAIN_API_TOKEN` | — | API token for client authentication |

---

## 8. MCP Client Configuration

### Claude Code / OpenCode

Add to your MCP configuration. The `brain-mcp` binary defaults to `BRAIN_API_URL=http://localhost:3333`. If your Go server runs on a different port, set the env var explicitly:

```json
{
  "mcpServers": {
    "brain": {
      "command": "brain-mcp",
      "env": {
        "BRAIN_API_URL": "http://localhost:3000"
      }
    }
  }
}
```

If `brain-mcp` is not in your `$PATH`, use the full path:

```json
{
  "mcpServers": {
    "brain": {
      "command": "/path/to/bin/brain-mcp",
      "env": {
        "BRAIN_API_URL": "http://localhost:3000"
      }
    }
  }
}
```

### Available MCP Tools

The Go `brain-mcp` server exposes the same tool groups as the TypeScript version:

- **Brain tools**: `brain_save`, `brain_recall`, `brain_search`, `brain_inject`, `brain_list`, `brain_update`, `brain_delete`, `brain_stats`, `brain_link`, `brain_backlinks`, `brain_outlinks`, `brain_related`, `brain_orphans`, `brain_stale`, `brain_verify`, `brain_section`, `brain_plan_sections`
- **Task tools**: `brain_tasks`, `brain_task_next`, `brain_task_get`, `brain_task_metadata`, `brain_tasks_status`, `brain_task_trigger`
- **Planning tools**: Various planning workflow tools
