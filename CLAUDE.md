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
