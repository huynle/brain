# CLAUDE.md

This file provides guidance for AI assistants working with the brain codebase.

## Project Overview

Brain API is a REST service for AI agent memory and knowledge management, with an integrated task queue processor. Built with Go, using the standard library and Bubbletea for the TUI.

## Key Commands

```bash
# Development
just build           # Build all Go binaries
just test            # Run all tests
just vet             # Run go vet (static analysis)
just check           # Run all checks (vet + test + lint)
just dev             # Run brain-api server

# Task Runner
brain start <project>                       # TUI dashboard
brain run list <project>                    # List tasks

# API Server
./bin/brain-api      # Start API server
go run ./cmd/brain-api  # Run API server without building
```

## Architecture

### Command-Line Tools (`cmd/`)
- `brain-api/` - REST API server entry point

- `brain/` - Main CLI with subcommands (server, runner, doctor, etc.)
- `brain-mcp/` - MCP (Model Context Protocol) server

### Core API (`internal/api/`)
- `entries.go` - CRUD for brain entries
- `search.go` - Full-text search
- `graph.go` - Graph traversal (backlinks, outlinks)
- `tasks.go` - Task queue endpoints
- `sections.go` - Section extraction from entries

### Core Services (`internal/service/`)
- `brain_service.go` - Main service layer
- `task_service.go` - Task management with dependency resolution
- `task_deps.go` - Dependency graph algorithms

### Feature checkout automation

Two built-in automations trigger on `feature.completed`, discriminated by task-level `checkout_mode`:

- `brain:builtin-feature-checkout` — AI-driven (LLM prompt), default path.
- `brain:builtin-feature-checkout-simple` — deterministic squash-merge via script executor.

Fold rule across a feature's tasks: any `checkout_mode:"simple"` → simple path; else AI. Missing/empty → AI. Folded value is placed on the `feature.completed` event's `metadata["checkout_mode"]` by `CheckFeatureCompletion` and matched by the automations' `Trigger.Filter`.

The simple script honors the `git -c merge.ff=true` invariant (see `feature-checkout/SKILL.md`) so it works regardless of user `merge.ff` gitconfig. It uses `feature_id` as the source branch name and cannot recover from merge conflicts — use AI mode for anything non-trivial.

### Goal subsystem (check + steer loop)

A goal is an `automation` BrainEntry with `Goal *GoalConfig` (`generated_by: brain-goal`, tags `[goal, goal:<id>]`). Scope resolution: `task_id` → that one task; else `feature_id` → the feature's tasks; else the whole project. Core: `internal/service/goal_service.go` (+ `goal_api.go`, `goal_automation.go`, `goal_steering.go`), HTTP in `internal/api/goals.go` (CRUD incl. `DELETE /goals/{id}`, `?status=` listing), steerer wiring in `internal/apiserver/goal_steerer.go`.

- **Reconcile** is deterministic over linked-task statuses (`decideReconcile`): no tasks → `need_work` (generates one task, deduped on `goal:<id>:need_work`); all complete → `complete` (flips the goal entry to `completed`; reactivate via PATCH `status=active`); any in-progress → steer-or-noop; blocked → `block`. Per-goal mutex serializes event/ticker/manual callers.
- **Cadence**: event-driven (`task.status_changed`, `feature.completed` — the latter matches WITHOUT `to_status`, which feature events never carry) plus a periodic ticker (`goalReconcileInterval`, 5m).
- **Steering**: when linked work is in progress and `steering` is enabled (default on, cooldown 15m, persisted as `last_steered_at`), the reconcile injects a "## Goal check-in" prompt (title + criteria + validation + self-assess instruction) into each live session via the same in-process plumbing as the control API (`prompt_async`). OpenCode-only; pi instances are skipped as unsupported. Audit decision `steer` with steered/skipped counts. Nil steerer or runner-pause ⇒ silently skipped.
- **Lookups are status-agnostic** (`findGoalByID` searches all statuses) so pause (`blocked`) → resume (`active`) round-trips; only event dispatch and the ticker filter to `active`.
- **Known limitation**: `complete` is status-based, not criteria-verified — a task that completes without actually meeting the goal criteria still completes the goal. A criteria-validation task on the complete path is the designed next step. Also `opencode run` exits when its current turn ends, so a steered agent must act on the injection within that turn.

### Dispatch delivery + lease states

A dispatch is a durable lease row (`task_dispatch_leases`) plus an SSE publish
to `realtime.RunnerTopic(runnerID)`. The row outlives the publish, so the two
have to be reconciled at publish time or the lease describes work nobody has.

- **Delivery is checked, not assumed.** `SchedulerService.publishDispatch` uses
  `Hub.PublishRunnerCommandTracked` and treats `delivered == 0` as a lost
  command: the lease is cleared immediately (`undoUndeliveredLease`) and the
  skip is reported as `runner_unreachable`. All three dispatch sites
  (`ScheduleProject`, `RunTaskNow`, `RunFeatureNow`) do this. A publisher that
  cannot report delivery counts as delivered — "cannot measure" must never
  read as "did not arrive". Publisher and deliverer are wired from the same
  object in `NewSchedulerService`.
- **`pushed` ≠ in flight.** An `acked` lease means a runner took the work.
  A `pushed` lease means the dispatch went out and nothing came back. The
  runner acks LATE — after `ResolveWorkdir`, which can create a worktree or
  clone a remote — so `pushed` covers both "the command evaporated" and "the
  runner is mid-setup". That ambiguity is why nothing auto-reclaims a pushed
  lease, and why a reconnect-driven lease sweep would be wrong: on an API
  restart the runner's stream drops while its in-flight setup continues, and
  wiping the lease would abort a spawn that had already paid for a clone.
- **`force` on `RunFeatureNow` reclaims only unacknowledged leases**
  (`reclaimableLease`). It never displaces an acked one, matching the resume
  path's refusal to release a claim held by an online runner. Note
  `RunTaskNow`'s force is blunter and does release acked leases.
- **Feature-level reasons distinguish the two**: `feature_in_progress` (a
  runner acknowledged the work) vs `feature_dispatch_pending` (every blocking
  lease is unacked — nothing is running, the holds expire on their own).
  `no_ready_tasks` names the gating feature via
  `waitingOnFeatures`/`blockedByFeatures` folded from the tasks.

### Abandonment + Resume model

When a runner dies mid-task, or when a task's claim lease expires without renewal, the task's `status` stays stuck at `in_progress` while nothing is actually running it. The abandonment surface makes that recoverable without introducing new sweepers.

**Detection (read-only, derived at API-response time):**
- `enrichAbandonmentState` in `internal/service/task.go` runs alongside `enrichDispatchDiagnostics` for every task in a `GET /tasks` response. It cross-references `task_claims` (from `StartClaimCleanup`), the owning runner's `runners.status` (from `RunLifecycleSweep`), and the reaper marker note text / metadata (from `reapOrphanedTasks`) to derive two fields on `ResolvedTask`:
  - `is_abandoned bool`
  - `abandon_reason string` — one of `no_claim | claim_expired | runner_offline | orphan_reaped`
- No new sweeper is introduced. Every signal is produced by an existing background job; enrichment just reads them together.

**Recovery (`POST /api/v1/tasks/{project}/{taskId}/resume`):**
- Validates `is_abandoned` (unless `force: true`), refuses to release a claim held by an online runner (live-claim safety — force does NOT override this), stamps `metadata.resume_requested=true`, flips `status` to `pending`. Emits `EventTaskResumeRequested` + `EventTaskStatusChanged`.
- The runner reads `resume_requested` in `claimAndSpawnWithWorkdir` and passes `IsResume=true` into the executor's prompt builder (`CommonBuildPrompt`). The flag is cleared after a *successful* `Spawn`; on Spawn-error rollback it is best-effort re-stamped so the intent survives a retry.
- `POST /api/v1/tasks/{project}/features/{featureId}/resume` fans out across every task in a feature; per-task outcomes come back in `ResumeFeatureResult.results` (skipped entries include a `reason` so partial failures don't fail the batch).
- Idempotent: a resume on a task already `pending+resume_requested=true` returns `Resumed=false` with an explanatory `Reason` and skips cleanup work.
- The orphan reaper (`tryReapOrphan`) skips tasks with `resume_requested=true` and re-reads the task immediately before its status flip, so a Resume that races with a reaper doesn't get silently reverted.

### Task origin + machine affinity

A task records where it was created, so it can be run back there. Three
provenance fields (`origin_machine_id`, `origin_client_id`, `origin_path`) are
stamped by the **stdio** MCP server from its `ExecutionContext`, plus a
caller-chosen `machine_affinity` (`local` | `preferred` | `none`).

- **`origin_path` is the caller's ACTUAL cwd** — the linked worktree, absolute.
  That is what distinguishes it from `workdir`, which is home-relative and gets
  re-resolved against whichever host runs the task; on a machine with a
  different layout that guess silently lands on another checkout, or falls
  through to `ensureCachedRemoteRepo` and clones a fresh copy.
- **The default is `preferred`, not `local`**, whenever an origin machine is
  known (`types.ResolveMachineAffinity`). Preferred is a +75 score in
  `scoreMachine` — outranking the project-level preferred-machine bonus, since
  it is the only signal distinguishing two tasks in the same project created on
  different machines. Only `local` is a hard filter. A strict default would
  turn "the origin machine's runner is offline" into a task that never runs and
  names no reason.
- **Enforced on BOTH dispatch paths** via `machineAffinitySatisfied`: push in
  `runnerEligibleForTask`, pull in `filterByRunnerEligibility`. Enforcing one
  only would let a polling runner elsewhere quietly undo the pin. Rejections
  surface as `machine_affinity_mismatch` / `machine_affinity_unresolved` in the
  same reasons slice as every other check.
- **Everything fails closed.** `local` with no origin is refused rather than
  widened to "anywhere". An unregistered runner (no row, so every other filter
  is skipped) is served no pinned tasks. The runner honors `origin_path` only
  when `RunnerConfig.MachineID` equals the task's origin — an absolute path
  from another host would otherwise open an unrelated directory. `MachineID` is
  derived in `NewExecutorRegistry`, the one chokepoint both runnercli entry
  points share; empty means "unknown machine", which never matches.
- **Stamping is gated on the transport** (`Server.ambientContextDescribesCaller`,
  sharing the `WithLocalFilesystem` flag). `GetCachedContext` is a
  process-global from `os.Getwd()`; under the in-process HTTP transport that is
  the API host, shared by every client, so stamping it would brand every task
  with amos's machine id and — at `local` — pin them all there. Over HTTP,
  `machine_affinity=local` is refused at creation rather than queued unrunnable.
  Note the pre-existing `workdir`/`git_remote`/`git_branch` stamping at the same
  call site is NOT gated this way.
- **Generated tasks carry no origin** by design — automation and goal
  `createTask` deliberately omit it, since server-generated work has no human
  caller and stamping would pin it to the API box.
- **Known limitation**: `ClaimTask` does not validate affinity, so a runner that
  claims a task directly by id bypasses the pin. Both discovery paths (push
  dispatch and `/next`) are filtered, so this is not reachable by accident;
  `requires_capability` has the identical gap.

Adding another frontmatter task field means SEVEN registration points, and
missing any one is silent — unknown YAML keys land in `Frontmatter.Extra`,
which nothing reads: the `Frontmatter` struct (both `yaml` AND `json` tags —
the indexer marshals the struct into `notes.metadata`), `rawFrontmatter`,
`knownFields`, the raw→Frontmatter copy in `Parse`, the `Generate` serializer,
`GenerateOptions` + its literal, then `parseMetadataIntoEntry`
(`internal/service/task.go`) and `brainEntryToResolvedTask`
(`internal/service/taskdeps.go`). The last two are the hops that left
`checkout_mode` write-only. Use `emit` (escaped) for anything caller-settable;
`emitPlain` is only safe for closed enums, since `SanitizeSimpleValue` strips
NULs and newlines but not YAML metacharacters.

### MCP transports + local paths

The MCP server ships in two transports, and only one of them shares a
filesystem with its client:

- **stdio** (`brain mcp`, `internal/mcpserver`) is a child process of the
  client, so a path the client names is a path this process can open.
- **HTTP** (`internal/mcp/http_transport.go`, mounted in-process by
  `apiserver/server.go` at `/mcp` and `/`) runs inside brain-api. A path from
  the client resolves on the API *host* — which fails outright, or silently
  reads/writes a different file that happens to exist there. It is also a
  remote file-read/write primitive for anyone who can reach the endpoint.

`NewServer()` therefore defaults to **no local filesystem**; only the stdio
path passes `WithLocalFilesystem()`. Any tool argument naming a caller-side
path must be gated on `Server.requireLocalFilesystem`, as `attachment_upload`
(`file_path`) and `attachment_download` (`output_path`) are. Both tools have a
transport-independent form that carries bytes over the wire instead —
`content`/`filename` base64 in, base64 out (capped at
`maxInlineAttachmentBytes`, 5 MiB, with the REST content endpoint as the
fallback for anything larger). Note this does *not* apply to arguments that
intentionally name paths on the server/runner host, such as the control tools'
`workdir`.

### Index freshness (who writes to the brain dir)

SQLite is a derived view of the markdown files. Everything the API serves —
search, the link graph, orphan detection — reads the index, not the disk, so a
file that lands without an `IndexFile` call is invisible until something
re-indexes it.

- **Every writer indexes.** `BrainServiceImpl.Save`/`Update`/`Move` and
  `TaskServiceImpl.CheckoutFeature` all call `indexer.IndexFile` immediately
  after the write. `NewTaskService` takes the indexer as a required argument
  for exactly this reason — CheckoutFeature is the one task-service path that
  writes a file, and it silently skipped indexing until 2026-08-26.
- **Boot indexes once.** `internal/apiserver/server.go` runs `IndexChanged` in
  a background goroutine at startup, then never scans again.
- **Out-of-band writes need the watcher.** A git pull into the brain dir, a
  manual edit, or another process bypasses both of the above. `indexer.FileWatcher`
  covers that gap, enabled with `server.index_watch.enabled` in config.yaml or
  `BRAIN_INDEX_WATCH=true`. **It is off by default**: the watcher registers one
  fsnotify watch per directory, and a large brain dir can exhaust the
  platform's watch limit (inotify `max_user_watches`). With it off, out-of-band
  writes appear only after a server restart.
- The watcher starts after the boot scan finishes so the two never race on the
  same path, and is stopped before the store closes so no debounced flush hits
  a closed database.
- fsnotify is not recursive. A directory created after startup arrives as a
  single Create event naming only that directory, while the OS has usually
  already built the rest of the chain and dropped files into it — so
  `addDirRecursive` walks each new directory, watches every level, and queues
  the markdown already inside. Without that walk a pulled
  `projects/foo/note/` subtree is never watched at all.
- `RebuildAll` exists but has no caller outside tests — there is no CLI or API
  route to force a full reindex. Content already on disk with a matching
  checksum is skipped by `IndexChanged`, so extraction fixes reach it only via
  a migration that nulls the affected checksums.

### Storage Layer (`internal/storage/`)
- `entries.go` - Entry storage operations
- `search.go` - Full-text search indexing
- `tasks.go` - Task persistence
- `graph.go` - Graph relationship storage

### Task Runner (`internal/runner/`)
- `runner.go` - Main runner orchestration (poll loop, claim/spawn, completion)
- `client.go` - Brain API HTTP client
- `executor.go` - OpenCode executor (`TaskExecutor` interface impl)
- `pi_executor.go` - Pi executor (`TaskExecutor` interface impl)
- `executor_factory.go` - `ExecutorRegistry` and `ResolveExecutorForTask` precedence chain
- `executor_common.go` - Shared logic: workdir resolution, prompt building, env exports, cleanup
- `idle_detection.go` - Idle detection: OpenCode HTTP polling, Pi process-exit detection
- `process_manager.go` - Child process lifecycle and tracking
- `state_manager.go` - Persistent state for runner
- `types.go` - All config, execution, state, and event types
- `signals.go` - Graceful shutdown handling
- `execute.go` - Manual TUI execution and feature batch execution
- `schedule.go` - Cron scheduling for recurring tasks
- `logging.go` - slog-based event handler for headless mode
- `sse_listener.go` - SSE stream watcher for task changes

#### Multi-Executor Architecture

The runner supports multiple executor backends via the `TaskExecutor` interface and `ExecutorRegistry`:

```
ExecutorRegistry
├── "opencode" → OpenCodeExecutor  (HTTP API-based, port polling for idle detection)
└── "pi"       → PiExecutor        (RPC mode via stdin, process-exit for completion)
```

**TaskExecutor interface:**
```go
type TaskExecutor interface {
    BuildPrompt(task *types.ResolvedTask, isResume bool) string
    ResolveWorkdir(task *types.ResolvedTask) (string, error)
    Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error)
    Cleanup(taskID, projectID string) error
}
```

**Executor resolution precedence:**
```
task.Executor > task_defaults.executor > config.DefaultExecutor > "opencode"
```

#### Pi Executor

The Pi executor spawns [Pi](https://github.com/anthropics/pi) processes in RPC mode (`--mode rpc`). Key features:

- **Agent bundles**: Resolved from `~/.pi/brain-agents/<agentName>/config.json`
  - `system_prompt_file`: Path to system prompt markdown
  - `extension`: Agent-bundled TypeScript extension
  - `thinking`: Thinking level (off/minimal/low/medium/high/xhigh)
  - `tools`: Tool restriction list
- **Extension composition** (3 layers, all additive):
  - Layer 1: Agent-bundled extension (from agent bundle config.json)
  - Layer 2: Config always-on extensions (`config.Pi.Extensions`)
  - Layer 3: Per-task extensions (`task.Extensions`)
- **Short name resolution**: `"code-review"` resolves to `~/.pi/extensions/brain-code-review.ts`
- **Model precedence**: `task.Model > runtime default > config.Pi.Model`
- **Idle detection**: Pi RPC processes exit when done (process exit = completion); no HTTP polling needed
- **Graceful fallback**: Missing agent bundle falls back to `--append-system-prompt`

#### Configuration

**Config types** (`types.go`):
```yaml
# Runner config (config.yaml or env vars)
pi:
  bin: "pi"                           # PI_BIN env var
  model: "anthropic/claude-sonnet-4-20250514"    # PI_MODEL
  thinking: "high"                    # PI_THINKING (off/minimal/low/medium/high/xhigh)
  agents_dir: "~/.pi/brain-agents"    # Agent bundle directory
  extensions_dir: "~/.pi/extensions"  # Extension resolution base
  extensions:                         # Always-on extensions (Layer 2)
    - "/path/to/ext.ts"
  no_session: true                    # --no-session flag

default_executor: "opencode"          # DEFAULT_EXECUTOR (opencode/pi)

task_defaults:                        # Defaults for all tasks
  agent: "tdd-dev"
  model: "anthropic/claude-sonnet-4-20250514"
  executor: "pi"
  execution_mode: "worktree"
  merge_policy: "auto_pr"
  merge_strategy: "squash"
  merge_target_branch: "main"
  remote_branch_policy: "delete"
  target_workdir: "/path/to/work"
```

**CLI flags:**
- `--executor <name>` - Override default executor (opencode/pi)
- `--pi-bin <path>` - Path to pi binary
- `--pi-model <model>` - Model for Pi executor
- `--pi-thinking <level>` - Thinking level for Pi

#### Idle Detection

Two mechanisms based on executor type:
- **OpenCode**: HTTP polling via `/session/status` endpoint. Empty response = idle.
- **Pi**: Process exit detection. A running Pi RPC process is always "busy". Completion is detected when the process exits (handled by `checkRunningTasks` → `CheckCompletion`).

Mixed workloads (OpenCode + Pi tasks running simultaneously) are supported with correct per-task routing.

### TUI Dashboard (`internal/tui/`)

The TUI uses Bubbletea (Elm-inspired architecture) with a component-based model:

```
Model (tui.go)
├── StatusBar       # Top bar: project name, task stats, connection status
├── TaskTree        # Left panel: dependency tree visualization
├── LogViewer       # Right top: real-time log display
├── TaskDetail      # Right bottom: selected task details
└── HelpBar         # Bottom: keyboard shortcuts
```

#### TUI Architecture (Bubbletea Pattern)

Bubbletea follows the Elm architecture with three core functions:

1. **Model** - Application state (struct)
2. **Update** - Handle messages and update state
3. **View** - Render the current state to terminal

```go
// Core pattern
type Model struct {
    // State fields
}

func (m Model) Init() tea.Cmd {
    // Initialize and return initial commands
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle messages, update state, return new commands
}

func (m Model) View() string {
    // Render state to string for display
}
```

#### TUI Components
- `sse.go` - Single-project SSE task stream for real-time updates
- `multi_project_sse.go` - Multi-project SSE streams with per-project update channels
- `logviewer.go` - Manages log entry buffer with max entries limit
- `tasktree.go` - Task list rendering with dependency visualization
- `statusbar.go` - Top status bar with stats
- `helpbar.go` - Bottom keyboard shortcuts

#### TUI State Management
- Focus state: tracks which panel (tasks/logs) is active
- Selection state: currently selected task ID
- Stats: computed from task list (ready/waiting/active/completed)
- Modal system: for confirmations, settings, help screens

#### Key Design Decisions
1. **Bubbletea over raw terminal** - Elm architecture, functional approach, type-safe
2. **SSE over polling** - Real-time updates via Server-Sent Events, simpler than WebSocket
3. **Dependency tree flattening** - Root tasks shown first, children indented
4. **Cycle detection** - Circular deps marked with `↺` symbol

### Shared Utilities (`pkg/`)
- `frontmatter/` - YAML frontmatter parsing
- `markdown/` - Markdown processing utilities

## Testing Patterns

Tests use Go's built-in testing framework:

```go
// Example test pattern
func TestTaskService_GetNext(t *testing.T) {
    // Arrange
    service := setupTestService(t)
    
    // Act
    task, err := service.GetNext("project-id")
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if task == nil {
        t.Fatal("expected task, got nil")
    }
}

// Table-driven tests
func TestDependencyResolution(t *testing.T) {
    tests := []struct {
        name     string
        input    []Task
        expected []string
    }{
        // test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

### TUI Testing

TUI components are tested using Bubbletea's testing patterns:

```go
// Component test pattern
func TestStatusBar_Render(t *testing.T) {
    model := StatusBar{
        ProjectName: "test-project",
        Stats: TaskStats{Ready: 5, Active: 2},
    }
    
    view := model.View()
    
    if !strings.Contains(view, "test-project") {
        t.Error("expected project name in view")
    }
}

// Update function test
func TestTaskTree_Update(t *testing.T) {
    model := TaskTree{}
    
    // Send key message
    newModel, cmd := model.Update(tea.KeyMsg{
        Type:  tea.KeyRunes,
        Runes: []rune("j"),
    })
    
    // Verify state change
    tree := newModel.(TaskTree)
    // assertions...
}
```

## Common Tasks

### Adding a TUI Component
1. Create component file in `internal/tui/` (e.g., `mycomponent.go`)
2. Implement Model struct and Init/Update/View methods
3. Create test file with `_test.go` suffix
4. Add component to main Model in `tui.go`

### Adding API Endpoints
1. Add handler in `internal/api/*.go`
2. Add test in same package or `_test.go` file
3. Update API client in `internal/runner/api_client.go`
4. Register route in `cmd/brain-api/main.go` or route initialization

### Debugging TUI
```bash
# Run with verbose logging
brain start project -v

# Check logs
brain api logs -f

# Run without TUI for direct output
brain start project
```

## File Conventions

- Tests: `*_test.go` alongside source files
- Internal packages: `internal/` (not importable by external projects)
- Exported packages: `pkg/` (can be imported)
- Commands: `cmd/<binary-name>/main.go`
- Entry points: `main.go` in each `cmd/` subdirectory

## Multi-Project Mode

The task runner supports monitoring multiple projects simultaneously with a shared execution pool.

### Basic Usage

```bash
# Monitor all projects
brain start all

# Filter with glob patterns
brain start all --include 'prod-*' --exclude 'prod-legacy'
brain start all -i 'brain-*' -e 'test-*'

# List all available projects
curl http://localhost:3333/api/v1/tasks | jq '.projects'
```

### TUI Keyboard Shortcuts (Multi-Project Mode)

| Key | Action |
|-----|--------|
| `h` / `[` | Previous project tab |
| `l` / `]` | Next project tab |
| `1-9` | Jump to project tab 1-9 |
| `j/k` | Navigate tasks |
| `Tab` | Switch panel focus |
| `r` | Refresh all projects |
| `q` | Quit |

### Architecture

- **Shared execution pool**: `--max-parallel` applies across ALL projects
- **Real-time updates**: All projects stream task updates via SSE
- **Composite task keys**: Tasks tracked as `projectId:taskId` internally
- **Project tabs**: First tab shows "All" aggregate, then individual project tabs

### Key Components

```
Task Runner Multi-Project Architecture

TaskRunner
├── projects: []string              # List of projects to poll
├── isMultiProject: bool            # Enables multi-project behavior
└── Shared ProcessManager           # Single pool for all projects

TUI (Model)
├── sseClients: map[string]*SSEClient  # SSE streams per project
│   ├── tasksByProject: map            # Tasks keyed by project
│   └── projectTabs: ProjectTabs       # Tab state management
├── StatusBar                          # Shows project tabs with task counts
└── activeProjectID: string            # Current tab selection
```

### Filter Examples

```bash
# Only production projects
brain start all -i 'prod-*'

# All except test projects
./bin/brain-runner start all -e 'test-*' -e '*-staging'

# Brain projects except legacy
brain start all -i 'brain-*' -e 'brain-legacy'
```

## Build System

The project uses `just` (justfile) for all task automation:

```bash
just              # List all recipes
just build        # Build all binaries
just test         # Run tests
just test-cover   # Run tests with coverage
just lint         # Run golangci-lint
just vet          # Run go vet
just check        # Run all checks (vet + test + lint)
just clean        # Clean build artifacts
just dev          # Run brain-api server
just install      # Install binaries to GOPATH/bin
just release      # Cross-compile for release
just docker       # Build Docker image
```

## Go Module

Module path: `github.com/huynle/brain-api`

Use standard Go commands:
```bash
go mod tidy       # Clean up dependencies
go mod download   # Download dependencies
go build ./...    # Build all packages
go test ./...     # Test all packages
```
