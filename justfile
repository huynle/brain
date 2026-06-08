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
# Web PWA (internal/webui — embedded into the Go binary)
# =============================================================================

web_dir := "web"
web_dist := "internal/webui/dist"

# Install web dependencies (idempotent). Reinstalls when node_modules is missing
# OR incomplete (e.g. an interrupted install) — detected via the vite binary.
web-install:
    @if [ ! -x {{ web_dir }}/node_modules/.bin/vite ]; then \
        echo "Installing web dependencies..."; \
        cd {{ web_dir }} && npm install --no-audit --no-fund; \
    fi

# Build the PWA into internal/webui/dist (embedded by go:embed). Clears stale
# assets first while preserving the committed .gitkeep/.gitignore placeholders.
web-build: web-install
    @echo "Building Brain PWA..."
    @rm -rf {{ web_dist }}/assets
    @find {{ web_dist }} -maxdepth 1 -type f ! -name '.gitkeep' ! -name '.gitignore' -delete 2>/dev/null || true
    @cd {{ web_dir }} && npm run build
    @echo "PWA built into {{ web_dist }}/"

# Run the PWA dev server (proxies API to $BRAIN_API_URL or http://localhost:3333)
web-dev:
    cd {{ web_dir }} && npm run dev

# Typecheck the web app
web-check:
    cd {{ web_dir }} && npm run typecheck

# =============================================================================
# Go Development
# =============================================================================

# Build all Go binaries (Go only — embeds whatever is already in {{ web_dist }})
build:
    @mkdir -p {{ binary_dir }}
    @for cmd in {{ cmds }}; do \
        echo "Building $cmd..."; \
        go build -ldflags '{{ ldflags }}' -o {{ binary_dir }}/$cmd ./cmd/$cmd; \
    done
    @echo "Build complete: {{ binary_dir }}/"

# Build the PWA then the Go binaries (full build with the web UI embedded)
build-all: web-build build

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

# Clean build artifacts (Go + embedded web assets, preserving placeholders)
clean:
    rm -rf {{ binary_dir }} coverage.out coverage.html
    @rm -rf {{ web_dist }}/assets
    @find {{ web_dist }} -maxdepth 1 -type f ! -name '.gitkeep' ! -name '.gitignore' -delete 2>/dev/null || true
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

# Cross-compile for release (linux/darwin/windows, amd64/arm64) with the PWA embedded
release: web-build
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
# Integration Testing
# =============================================================================
#
# Isolated integration test environment using XDG env vars — never touches
# production config (~/.config/brain) or production brain dir (~/.brain).
#
# Usage:
#   just int-server          # Start isolated server on :4444
#   just int-seed            # Populate test projects (run once after int-server)
#   just int-status          # Show what's running + project list
#   just int-stop            # Kill the integration server
#   just int-clean           # Wipe all integration test state
#
#   just int-test-defaults   # Verify task_defaults applied to tasks
#   just int-test-events     # Smoke-test event ingestion + SSE stream
#   just int-test-webhooks   # Register webhook + fire event + check delivery
#   just int-test-runners    # Check runner registry endpoint
#   just int-test-claims     # Verify task_claims table in SQLite
#   just int-test-projects   # List projects (multi-project discovery)
#   just int-test-all        # Run all API-level smoke tests
#
#   just int-tui-all         # Open multi-project TUI (--include prod-*)
#   just int-tui PROJECT     # Open TUI for a specific project
#   just int-run PROJECT     # Start headless runner for a project
#
# Environment:
#   INT_DIR   = /tmp/brain-integration-test      (brain data)
#   INT_CFG   = /tmp/brain-integration-config    (XDG_CONFIG_HOME)
#   INT_STATE = /tmp/brain-integration-state     (XDG_STATE_HOME)
#   INT_PORT  = 4444
# =============================================================================

int_dir   := "/tmp/brain-integration-test"
int_cfg   := "/tmp/brain-integration-config"
int_state := "/tmp/brain-integration-state"
int_port  := "4444"
int_url   := "http://localhost:" + int_port
int_bin   := "bin/brain"
int_env   := "XDG_CONFIG_HOME=" + int_cfg + " BRAIN_DIR=" + int_dir + " XDG_STATE_HOME=" + int_state + " PORT=" + int_port + " BRAIN_API_URL=" + int_url

# Bootstrap: create config dir and write isolated config (idempotent)
_int-bootstrap:
    @mkdir -p {{ int_cfg }}/brain {{ int_dir }} {{ int_state }}
    @if [ ! -f {{ int_cfg }}/brain/config.yaml ]; then \
        printf 'server:\n  port: {{ int_port }}\n  brain_dir: {{ int_dir }}\n  enable_auth: false\n  log_level: info\n  task_defaults:\n    agent: "tdd-dev"\n    model: "anthropic/claude-sonnet-4-5"\n    execution_mode: "worktree"\n    complete_on_idle: true\n    merge_policy: "auto_merge"\n    merge_strategy: "squash"\n    merge_target_branch: "dev"\n    remote_branch_policy: "delete"\n' > {{ int_cfg }}/brain/config.yaml; \
        echo "Created {{ int_cfg }}/brain/config.yaml"; \
    fi

# Start isolated brain server on :4444 (foreground — run in a separate terminal)
int-server: build _int-bootstrap
    @echo "Starting integration server on {{ int_url }}"
    @echo "  Brain dir:   {{ int_dir }}"
    @echo "  Config:      {{ int_cfg }}/brain/config.yaml"
    @echo "  State:       {{ int_state }}"
    @echo ""
    {{ int_env }} {{ int_bin }} api start

# Seed all integration test projects (idempotent — safe to run multiple times)
int-seed: _int-bootstrap
    #!/usr/bin/env bash
    set -euo pipefail
    URL="{{ int_url }}/api/v1"
    mk() {
      local title="$1"; shift
      curl -sf -X POST "$URL/entries" -H "Content-Type: application/json" --data-binary "$1" \
        | python3 -c "import sys,json; d=json.load(sys.stdin); print('  +', d['path'], ' ←', d['title'])" \
        || echo "  SKIP (exists or error): $title"
    }
    echo "=== task-defaults-test ==="
    mk "Refactor auth (inherits defaults)" \
      '{"type":"task","title":"Refactor auth module (inherits all defaults)","status":"pending","priority":"high","project":"task-defaults-test","feature_id":"auth-refactor","content":"No agent/model set — should inherit server task_defaults.\n\n**Verify:** GET /tasks/task-defaults-test/next → agent=tdd-dev, execution_mode=worktree, git_branch auto-derived from feature_id."}'
    mk "Generate API docs (explicit override)" \
      '{"type":"task","title":"Generate API docs (explicit agent=explore override)","status":"pending","priority":"medium","project":"task-defaults-test","feature_id":"docs","agent":"explore","model":"anthropic/claude-opus-4-5","execution_mode":"current_branch","content":"**Verify:** agent/model/execution_mode must NOT be overwritten by server defaults."}'
    echo "=== prod-payments ==="
    mk "Fix refund idempotency" \
      '{"type":"task","title":"Fix refund idempotency bug","status":"pending","priority":"high","project":"prod-payments","feature_id":"refund-fix","content":"In prod-payments. Verify appears under prod-payments tab with: just int-tui-all"}'
    echo "=== prod-api ==="
    mk "Add rate limiting" \
      '{"type":"task","title":"Add per-endpoint rate limiting","status":"pending","priority":"high","project":"prod-api","feature_id":"security","content":"In prod-api. Should appear in its own TUI tab."}'
    echo "=== prod-workers ==="
    mk "Queue backpressure" \
      '{"type":"task","title":"Implement queue backpressure","status":"pending","priority":"high","project":"prod-workers","feature_id":"reliability","content":"In prod-workers."}'
    echo "=== test-flaky (excluded by test-*) ==="
    mk "Fix timing test" \
      '{"type":"task","title":"Fix timing-dependent test","status":"pending","priority":"low","project":"test-flaky","content":"Should NOT appear with: just int-tui-all (excluded by --exclude test-*)"}'
    echo "=== event-hooks-test ==="
    mk "Event ingestion + SSE" \
      '{"type":"task","title":"Verify POST /events ingestion and SSE broadcast","status":"pending","priority":"high","project":"event-hooks-test","feature_id":"event-stream","content":"Run: just int-test-events"}'
    mk "Webhook register + delivery" \
      '{"type":"task","title":"Register webhook and verify delivery on task.blocked","status":"pending","priority":"high","project":"event-hooks-test","feature_id":"webhooks","content":"Run: just int-test-webhooks"}'
    mk "Trigger task (fires on task.blocked)" \
      '{"type":"task","title":"Auto-investigate blocked tasks (trigger frontmatter)","status":"active","priority":"medium","project":"event-hooks-test","feature_id":"event-triggers","direct_prompt":"A task was blocked. Investigate and suggest how to unblock it.","complete_on_idle":true,"trigger":{"event":"task.blocked","filter":{"project_id":"event-hooks-test"},"cooldown":"5m","max_concurrent":1},"content":"Should activate when a task.blocked event fires for event-hooks-test."}'
    mk "Feature lifecycle probe step-1" \
      '{"type":"task","title":"Feature lifecycle probe step-1 (feature.started)","status":"pending","priority":"medium","project":"event-hooks-test","feature_id":"lifecycle-probe","content":"When claimed: feature.started fires. Watch: just int-stream"}'
    mk "Feature lifecycle probe step-2" \
      '{"type":"task","title":"Feature lifecycle probe step-2 (feature.completed)","status":"pending","priority":"medium","project":"event-hooks-test","feature_id":"lifecycle-probe","content":"When both steps complete: feature.completed fires."}'
    echo "=== horizontal-scaling-test ==="
    mk "Claim persistence probe" \
      '{"type":"task","title":"Claim persistence probe (task_claims table)","status":"pending","priority":"high","project":"horizontal-scaling-test","feature_id":"claim-persistence","content":"Run: just int-test-claims"}'
    mk "Runner registry probe" \
      '{"type":"task","title":"Runner registry probe (GET /runners)","status":"pending","priority":"high","project":"horizontal-scaling-test","feature_id":"runner-registry","content":"Run: just int-test-runners"}'
    mk "Direct dispatch probe" \
      '{"type":"task","title":"Direct dispatch probe (POST /tasks/{id}/dispatch)","status":"pending","priority":"high","project":"horizontal-scaling-test","feature_id":"dispatch","content":"Start a runner, then: curl -X POST {{ int_url }}/api/v1/tasks/horizontal-scaling-test/TASKID/dispatch -d {\"targetRunnerId\":\"RUNNERID\"}"}'
    echo ""
    echo "Done. Projects:"
    curl -sf "{{ int_url }}/api/v1/tasks" | python3 -c "import sys,json; [print('  -', p) for p in sorted(json.load(sys.stdin).get('projects',[]))]"

# Show integration server status + project list
int-status:
    @echo "=== Server ===" && \
    curl -sf {{ int_url }}/api/v1/health | python3 -m json.tool 2>/dev/null || echo "NOT RUNNING — use: just int-server"
    @echo "" && echo "=== Projects ===" && \
    curl -sf "{{ int_url }}/api/v1/tasks" | python3 -c "import sys,json; d=json.load(sys.stdin); [print('  -', p) for p in sorted(d.get('projects',[]))]" 2>/dev/null || true
    @echo "" && echo "=== task_defaults ===" && \
    curl -sf {{ int_url }}/api/v1/config/task-defaults | python3 -m json.tool 2>/dev/null || true

# Kill the integration server
int-stop:
    @pkill -f "brain api start.*{{ int_port }}" 2>/dev/null && echo "Stopped" || \
    pkill -f "brain.*{{ int_port }}" 2>/dev/null && echo "Stopped" || \
    echo "Not running (or use Ctrl+C in the server terminal)"

# Wipe all integration test state (config + data + state)
int-clean:
    @echo "Removing integration test dirs..."
    rm -rf {{ int_dir }} {{ int_cfg }} {{ int_state }}
    @echo "Done. Run 'just int-server' + 'just int-seed' to start fresh."

# --- API smoke tests ---

# Verify task_defaults are applied to tasks (and explicit overrides preserved)
int-test-defaults:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== task_defaults endpoint ==="
    curl -sf {{ int_url }}/api/v1/config/task-defaults | python3 -c "
    import sys,json; d=json.load(sys.stdin)
    assert d['agent']          == 'tdd-dev',                        f'agent={d[\"agent\"]!r}'
    assert d['model']          == 'anthropic/claude-sonnet-4-5',    f'model={d[\"model\"]!r}'
    assert d['execution_mode'] == 'worktree',                       f'execution_mode={d[\"execution_mode\"]!r}'
    assert d['complete_on_idle'] == True,                           f'complete_on_idle={d[\"complete_on_idle\"]!r}'
    print('  PASS: task_defaults endpoint returns correct values')
    "
    echo ""
    echo "=== defaults applied to task without agent/model ==="
    curl -sf "{{ int_url }}/api/v1/tasks/task-defaults-test/next" | python3 -c "
    import sys,json; d=json.load(sys.stdin)
    assert d.get('agent') == 'tdd-dev',                      f'agent={d.get(\"agent\")!r} (expected tdd-dev from defaults)'
    assert d.get('model') == 'anthropic/claude-sonnet-4-5',  f'model={d.get(\"model\")!r}'
    assert d.get('execution_mode') == 'worktree',            f'execution_mode={d.get(\"execution_mode\")!r}'
    print(f'  PASS: \"{d[\"title\"]}\" inherited defaults correctly')
    "
    echo ""
    echo "=== explicit overrides NOT overwritten by defaults ==="
    curl -sf "{{ int_url }}/api/v1/tasks/task-defaults-test" | python3 -c "
    import sys,json
    tasks = json.load(sys.stdin).get('tasks',[])
    override = next((t for t in tasks if 'override' in t.get('title','').lower()), None)
    assert override, 'Could not find explicit-override task'
    assert override.get('agent') == 'explore',                f'agent={override.get(\"agent\")!r} (expected explore)'
    assert override.get('model') == 'anthropic/claude-opus-4-5', f'model={override.get(\"model\")!r}'
    assert override.get('execution_mode') == 'current_branch',f'execution_mode={override.get(\"execution_mode\")!r}'
    print(f'  PASS: explicit overrides preserved for \"{override[\"title\"]}\"')
    "

# Smoke-test event ingestion + SSE
int-test-events:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== POST /events (ingest synthetic event) ==="
    STATUS=$(curl -sf -o /dev/null -w "%{http_code}" -X POST {{ int_url }}/api/v1/events \
      -H "Content-Type: application/json" \
      -d '[{"type":"task.started","source":"runner","project_id":"event-hooks-test","task_id":"test-123","task_title":"Integration test event"}]')
    [[ "$STATUS" =~ ^2 ]] \
      && echo "  PASS: POST /events returned $STATUS" \
      || { echo "  FAIL: POST /events returned $STATUS"; exit 1; }
    echo ""
    echo "=== GET /events/recent ==="
    curl -sf "{{ int_url }}/api/v1/events/recent?limit=5" | python3 -c "
    import sys,json; d=json.load(sys.stdin)
    events = d if isinstance(d,list) else d.get('events',[])
    print(f'  PASS: {len(events)} recent event(s) returned')
    for e in events[:3]: print(f'    - {e.get(\"type\",\"?\")} [{e.get(\"project_id\",\"?\")}]')
    "
    echo ""
    echo "=== GET /events/stream (5s subscription) ==="
    echo "  Subscribing for 5s, posting 1 event mid-stream..."
    curl -sN "{{ int_url }}/api/v1/events/stream" > /tmp/int-sse-capture.txt 2>/dev/null &
    SSE_PID=$!
    sleep 1
    curl -sf -X POST {{ int_url }}/api/v1/events \
      -H "Content-Type: application/json" \
      -d '[{"type":"task.blocked","source":"runner","project_id":"event-hooks-test","task_id":"test-456","task_title":"SSE test"}]' > /dev/null
    sleep 2; kill $SSE_PID 2>/dev/null; wait $SSE_PID 2>/dev/null || true
    if grep -q "task\." /tmp/int-sse-capture.txt 2>/dev/null; then
      echo "  PASS: SSE stream received events"
      grep "data:" /tmp/int-sse-capture.txt | head -3 | sed 's/^/    /'
    else
      echo "  WARN: SSE stream — no events captured (may need longer window)"
      cat /tmp/int-sse-capture.txt | head -5 | sed 's/^/    /'
    fi

# Register a webhook and verify delivery
int-test-webhooks:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== POST /webhooks (register) ==="
    WH=$(curl -sf -X POST {{ int_url }}/api/v1/webhooks \
      -H "Content-Type: application/json" \
      -d '{"name":"int-test-hook","url":"http://localhost:19999","events":["task.*"],"enabled":true}')
    WH_ID=$(echo "$WH" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))")
    [ -n "$WH_ID" ] && echo "  PASS: webhook registered id=$WH_ID" || { echo "  FAIL: no id returned: $WH"; exit 1; }
    echo ""
    echo "=== GET /webhooks ==="
    curl -sf {{ int_url }}/api/v1/webhooks | python3 -c "
    import sys,json; d=json.load(sys.stdin)
    whs = d if isinstance(d,list) else d.get('webhooks',[])
    print(f'  PASS: {len(whs)} webhook(s) registered')
    "
    echo ""
    echo "=== PATCH /webhooks/{id} (update) ==="
    PATCH_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" \
      -X PATCH {{ int_url }}/api/v1/webhooks/$WH_ID \
      -H "Content-Type: application/json" \
      -d '{"enabled":false}')
    [ "$PATCH_STATUS" = "200" ] && echo "  PASS: PATCH returned 200" || echo "  WARN: PATCH returned $PATCH_STATUS"
    echo ""
    echo "=== DELETE /webhooks/{id} (cleanup) ==="
    DEL_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" \
      -X DELETE {{ int_url }}/api/v1/webhooks/$WH_ID)
    [ "$DEL_STATUS" = "200" ] || [ "$DEL_STATUS" = "204" ] \
      && echo "  PASS: DELETE returned $DEL_STATUS" \
      || echo "  WARN: DELETE returned $DEL_STATUS"

# Verify runner registry endpoint
int-test-runners:
    #!/usr/bin/env bash
    echo "=== GET /runners ==="
    curl -sf {{ int_url }}/api/v1/runners | python3 -c "
    import sys,json; d=json.load(sys.stdin)
    runners = d if isinstance(d,list) else d.get('runners',[])
    print(f'  PASS: /runners returned {len(runners)} runner(s)')
    [print(f'    - {r.get(\"runnerId\",\"?\")} [{r.get(\"status\",\"?\")}] {r.get(\"hostname\",\"?\")}') for r in runners]
    "
    echo "  (Start a runner with: just int-run horizontal-scaling-test)"

# Verify task_claims table in SQLite (persistent claim storage)
int-test-claims:
    @echo "=== task_claims table ===" && \
    sqlite3 {{ int_dir }}/.brain-data/brain.db \
      "SELECT COUNT(*) || ' rows in task_claims' FROM task_claims;" && \
    sqlite3 {{ int_dir }}/.brain-data/brain.db \
      ".schema task_claims" | head -8 && \
    echo "  PASS: task_claims table exists (schema v5+)" && \
    echo "  (Start a runner to populate claims: just int-run horizontal-scaling-test)"

# List all projects visible to the integration server
int-test-projects:
    #!/usr/bin/env bash
    echo "=== All projects ==="
    curl -sf "{{ int_url }}/api/v1/tasks" | python3 -c "
    import sys,json; d=json.load(sys.stdin)
    projects = sorted(d.get('projects',[]))
    print(f'  {len(projects)} project(s):', *[f'\n    - {p}' for p in projects], sep='')
    "

# Run all API-level smoke tests
int-test-all: int-test-defaults int-test-events int-test-webhooks int-test-runners int-test-claims int-test-projects
    @echo "" && echo "=== All smoke tests complete ==="

# --- TUI / runner targets ---

# Open multi-project TUI watching prod-* projects (excludes test-* and staging-*)
int-tui-all: build
    {{ int_env }} {{ int_bin }} start all --include 'prod-*' --exclude 'test-*' --exclude 'staging-*'

# Open TUI for a specific project (e.g.: just int-tui event-hooks-test)
int-tui project: build
    {{ int_env }} {{ int_bin }} start {{ project }}

# Start a headless runner for a project (e.g.: just int-run horizontal-scaling-test)
int-run project: build
    {{ int_env }} {{ int_bin }} run start {{ project }} --headless

# Subscribe to the event stream (Ctrl+C to stop)
int-stream:
    @echo "Subscribing to {{ int_url }}/api/v1/events/stream (Ctrl+C to stop)..."
    curl -N "{{ int_url }}/api/v1/events/stream"

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
