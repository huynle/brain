# Brain

**Persistent memory and autonomous task execution for AI agents.**

Brain is a REST API + MCP server that gives AI coding agents (Claude Code, OpenCode, etc.) long-term memory, structured knowledge management, and an autonomous task runner that can execute multi-step plans while you sleep. Think of it as a second brain for your AI workflow — it remembers decisions, tracks dependencies, schedules recurring work, and orchestrates parallel execution across projects.

Built with [Bun](https://bun.sh), [Hono](https://hono.dev), and [Ink](https://github.com/vadimdemedes/ink).

## Why Brain?

AI coding agents are powerful but stateless — they forget everything between sessions. Brain solves this by providing:

- **Persistent memory** — Save decisions, explorations, patterns, and learnings that survive across sessions
- **Structured task queues** — Break work into dependency-tracked tasks that agents execute autonomously
- **Feature orchestration** — Group related tasks into features, execute them in order, and track progress
- **Cron scheduling** — Schedule recurring task pipelines with cron expressions
- **Knowledge graph** — Link entries together, find related context, and maintain a growing knowledge base
- **Multi-project support** — Monitor and execute tasks across all your projects from a single dashboard
- **Git worktree isolation** — Each task runs in its own worktree so parallel work never conflicts

## Features

### Knowledge Management
- **Zettelkasten-style knowledge graph** with bidirectional linking between entries
- **12 entry types**: summaries, reports, walkthroughs, plans, patterns, learnings, ideas, scratch notes, decisions, explorations, executions, and tasks
- **Full-text search** across all entries with filtering by type, status, tags, and feature
- **Graph traversal**: backlinks, outlinks, related entries, and orphan detection
- **Section extraction** from plan entries for precise context injection
- **Entry verification** tracking to identify stale knowledge that needs review
- **Cross-project entry moves** with automatic dependency reference rewriting

### Task Queue & Execution
- **Dependency-tracked task queue** with automatic resolution (ready/waiting/blocked states)
- **Parallel execution** with configurable concurrency limits (per-project and global)
- **Feature grouping** — organize tasks by feature with `feature_id`, priority, and inter-feature dependencies
- **Batch task status** with long-polling/blocking wait for orchestrator agents
- **Auto-completion detection** (`complete_on_idle`) for tasks that finish when the agent goes idle
- **Git worktree isolation** — tasks with `git_branch` automatically get their own worktree
- **Merge intent tracking** — tasks carry merge policy, strategy, and target branch metadata
- **Session tracking** — each execution records OpenCode session IDs with timestamps
- **Memory monitoring** — prevents spawning when system resources are low
- **PID liveness checks** — detects and cleans up orphaned processes
- **Configurable execution**: per-task agent, model, working directory, and direct prompt overrides

### Cron Scheduling
- **Cron expression scheduling** with standard 5-field syntax
- **Bounded schedules** with optional `not_before` / `not_after` datetime constraints
- **Task pipelines** — link multiple tasks to a cron for sequential/parallel execution
- **Run history** tracking with trigger timestamps and outcomes
- **Manual triggers** — fire a cron run on demand
- **Automatic reset** — completed pipeline tasks reset for the next scheduled run

### Interactive TUI Dashboard
- **Real-time task tree** with dependency visualization and git-graph style lane rendering
- **Feature grouping** with collapsible headers, status indicators, and bulk operations
- **Multi-select** with Space key for batch status changes and deletions
- **Metadata popup** for editing task properties (status, priority, feature, project, cron links)
- **Settings popup** for per-project concurrency limits and runtime model overrides
- **Cron panel** with schedule browser, run history, and task-to-cron link editor
- **Mouse support** with click-to-select, hover preview, and collapsible sections
- **External editor integration** — press `e` to edit a task in `$EDITOR`
- **Clipboard support** — press `y` to yank task info to system clipboard
- **Focus mode** — press `x` to execute a single feature to completion
- **Pause/resume** at project, feature, or individual task level
- **Live resource metrics** (CPU, memory) in the status bar
- **Real-time SSE streaming** with automatic polling fallback
- **Keyboard-driven** with vim-style navigation (`j/k/g/G`), Tab panel cycling, and `?` help overlay
- **Text wrap toggle** — press `w` to toggle truncation vs wrapping in the task tree
- **Log panel** with togglable visibility and real-time streaming

### MCP Server (35 tools)
- **Embedded Streamable HTTP transport** — no separate process, served on the same port as the REST API
- **OAuth 2.1 with PKCE** for secure remote client authentication
- **HTTPS/TLS support** for Claude web connector integration
- **Plugin targets** for Claude Code and OpenCode with full tool parity
- Tools span: entry CRUD, search, graph traversal, task management, cron scheduling, section extraction, verification, and link generation

### Multi-Project Mode
- **Shared execution pool** across all projects with a single `--max-parallel` limit
- **Project tabs** with per-project stats and an "All" aggregate view
- **Glob-based project filtering** with `--include` and `--exclude` patterns
- **Per-project concurrency overrides** via the settings popup

## Installation

### Quick Install (Recommended)

```bash
# Install globally with bun
bun add -g @brain/api

# Or run directly with bunx (no install needed)
bunx @brain/api brain --help
```

This installs the following CLI commands:
- `brain` - Server management and diagnostics
- `brain-server` - API server (used internally)
- `brain-runner` - Task runner with TUI
- `do-work` - Quick task runner wrapper

### From Source

```bash
# Clone and install dependencies
git clone https://github.com/huynle/brain.git
cd brain
bun install

# Option 1: Link for development (updates automatically)
bun link

# Option 2: Build standalone binaries to ~/.local/bin
just install
```

If using `just install`, make sure `~/.local/bin` is in your `PATH`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Requirements

- [Bun](https://bun.sh) >= 1.0.0
- [zk](https://github.com/mickael-menu/zk) CLI (for Zettelkasten operations)

To verify your installation:

```bash
# Check zk is available
zk --version

# Run diagnostics
brain doctor -v
```

## Usage

### Development Server

```bash
# Start with hot reload
bun run dev
```

### Production

```bash
# Start the server
bun run start

# Or build first
bun run build
```

### Testing

```bash
# Run tests
bun test

# Type checking
bun run typecheck

# Linting
bun run lint
```

### API Endpoints

#### Core Endpoints
- `GET /health` - Health check
- `/api/v1/entries` - CRUD operations for brain entries
- `POST /api/v1/entries/:id/move` - Move entry to a different project
- `/api/v1/search` - Full-text search
- `/api/v1/graph` - Graph traversal (backlinks, outlinks, related, orphans)
- `/api/v1/sections` - Section extraction from plan entries

#### Task Endpoints
- `GET /api/v1/tasks/:projectId` - List all tasks for project
- `GET /api/v1/tasks/:projectId/ready` - Ready tasks (dependencies satisfied)
- `GET /api/v1/tasks/:projectId/waiting` - Waiting on dependencies
- `GET /api/v1/tasks/:projectId/blocked` - Blocked tasks
- `GET /api/v1/tasks/:projectId/next` - Next task to execute
- `POST /api/v1/tasks/:projectId/status` - Batch task status with blocking wait
- `POST /api/v1/tasks/:taskId/start` - Mark task in_progress
- `POST /api/v1/tasks/:taskId/complete` - Mark task completed
- `POST /api/v1/tasks/:taskId/block` - Mark task blocked

#### Feature Endpoints
- `GET /api/v1/features/:projectId` - List features for project
- `POST /api/v1/features/:featureId/checkout` - Trigger feature checkout

#### Cron Endpoints
- `GET /api/v1/crons/:projectId` - List cron entries
- `POST /api/v1/crons/:projectId` - Create cron entry
- `GET /api/v1/crons/:projectId/:cronId` - Get cron with pipeline tasks
- `PATCH /api/v1/crons/:projectId/:cronId` - Update cron entry
- `DELETE /api/v1/crons/:projectId/:cronId` - Delete cron entry
- `POST /api/v1/crons/:projectId/:cronId/trigger` - Manually trigger cron run
- `GET /api/v1/crons/:projectId/:cronId/runs` - Get run history
- `GET /api/v1/crons/:projectId/:cronId/tasks` - List linked tasks
- `POST /api/v1/crons/:projectId/:cronId/tasks` - Link/unlink tasks

#### SSE Streaming
- `GET /api/v1/tasks/:projectId/stream` - Real-time task updates via Server-Sent Events

### Example

```bash
# Start the API server
bun run start

# Get ready tasks for a project
curl http://localhost:3333/api/v1/tasks/myproject/ready

# Start a task
curl -X POST http://localhost:3333/api/v1/tasks/abc123/start

# Save a note to the brain
curl -X POST http://localhost:3333/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"type": "decision", "title": "Use Bun over Node", "content": "..."}'

# Search for related context
curl 'http://localhost:3333/api/v1/search?q=authentication&type=decision'

# Stream task updates in real-time
curl -N http://localhost:3333/api/v1/tasks/myproject/stream
```

## MCP Server

Brain API includes an embedded [MCP](https://modelcontextprotocol.io) (Model Context Protocol) server served over Streamable HTTP on the same port as the REST API. When `brain start` runs, the MCP endpoint is available at `POST /mcp` — no separate process needed.

### Connecting Claude Code CLI

The Claude Code CLI can use HTTP directly — no HTTPS required:

```bash
claude mcp add --transport http brain http://localhost:3333/mcp
```

Or add to `.mcp.json`:

```json
{
  "mcpServers": {
    "brain": {
      "type": "http",
      "url": "http://localhost:3333/mcp"
    }
  }
}
```

### Connecting Claude Web UI (Custom Connector)

Claude's web UI "Add Custom Connector" feature requires HTTPS with a **publicly trusted certificate**. Self-signed certificates (including mkcert) won't work because validation requests come from Anthropic's backend servers, not your browser.

#### Option 1: Use a Tunnel Service (Recommended)

Tunnel services provide publicly trusted HTTPS URLs that forward to your local server:

**ngrok:**
```bash
# Install: brew install ngrok (macOS) or https://ngrok.com/download
ngrok http 3333
# Gives you: https://xyz.ngrok-free.app
```

**Cloudflare Tunnel:**
```bash
# Install: brew install cloudflared
cloudflared tunnel --url http://localhost:3333
# Gives you: https://xyz.trycloudflare.com
```

**Tailscale Funnel:**
```bash
# Requires Tailscale account
tailscale funnel 3333
# Gives you: https://your-machine.tailnet-name.ts.net
```

Then add the tunnel URL as your custom connector in Claude's web UI.

#### Option 2: Local HTTPS (Developer Use Only)

For local development where you want HTTPS (e.g., testing TLS handling), Brain API supports TLS:

```bash
# Generate local certificates with mkcert
brew install mkcert   # macOS
mkcert -install       # One-time CA setup
mkcert localhost 127.0.0.1  # Generates localhost.pem and localhost-key.pem

# Start with TLS
ENABLE_TLS=true TLS_KEY=./localhost-key.pem TLS_CERT=./localhost.pem bun run dev
```

**Note:** Local HTTPS works for browser access but NOT for Claude's custom connector (see above).

### Available Tools (35)

#### Core Entry Tools
| Tool | Description |
|------|-------------|
| `brain_save` | Save content to the brain (summaries, plans, decisions, tasks, etc.) |
| `brain_recall` | Retrieve a specific entry by path, ID, or title |
| `brain_search` | Full-text search with type, status, tags, and feature_id filters |
| `brain_list` | List entries with filtering and sorting |
| `brain_inject` | Get relevant context for a task via fuzzy search |
| `brain_update` | Update status, title, tags, priority, feature grouping, or append content |
| `brain_delete` | Delete an entry by path (requires confirmation) |
| `brain_move` | Move an entry to a different project |
| `brain_stats` | Get brain statistics (counts by type, project, global) |
| `brain_check_connection` | Verify the brain API is running |

#### Task Management Tools
| Tool | Description |
|------|-------------|
| `brain_tasks` | List tasks with dependency status (ready/waiting/blocked) and cycle detection |
| `brain_task_next` | Get the highest-priority ready task with full content |
| `brain_task_get` | Get a task by ID with dependencies, dependents, and classification |
| `brain_task_metadata` | Get execution config (agent, model, workdir, merge intent, feature grouping) |
| `brain_tasks_status` | Batch status check with optional blocking wait for completion |

#### Cron Scheduling Tools
| Tool | Description |
|------|-------------|
| `brain_cron_list` | List cron entries for a project |
| `brain_cron_get` | Get a cron entry with pipeline tasks |
| `brain_cron_create` | Create a cron with schedule and optional task links |
| `brain_cron_update` | Update cron schedule, title, status, or tags |
| `brain_cron_delete` | Delete a cron entry (preserves linked tasks) |
| `brain_cron_trigger` | Manually fire a cron run |
| `brain_cron_runs` | Get cron run history |
| `brain_cron_linked_tasks` | List tasks linked to a cron |
| `brain_cron_linked_task_add` | Link a task to a cron |
| `brain_cron_linked_task_remove` | Unlink a task from a cron |
| `brain_cron_linked_tasks_set` | Replace all linked tasks for a cron |

#### Graph Traversal Tools
| Tool | Description |
|------|-------------|
| `brain_backlinks` | Find entries that link TO a given entry |
| `brain_outlinks` | Find entries that a given entry links TO |
| `brain_related` | Find entries sharing linked notes with a given entry |
| `brain_orphans` | Find entries with no incoming links |

#### Verification & Section Tools
| Tool | Description |
|------|-------------|
| `brain_stale` | Find entries not verified in N days |
| `brain_verify` | Mark an entry as verified (still accurate) |
| `brain_section` | Extract a specific section from a plan entry |
| `brain_plan_sections` | List section headers from a plan for orchestration |
| `brain_link` | Generate a markdown link to a brain entry |

The embedded MCP server calls the service layer directly (no HTTP round-trip), making it faster than a standalone stdio-based MCP server.

### OAuth 2.1 Authentication

Brain API supports OAuth 2.1 with PKCE for secure MCP client authentication. This enables proper access control for remote clients.

**Discovery Endpoints:**
- `GET /.well-known/oauth-authorization-server` - OAuth server metadata
- `GET /.well-known/oauth-protected-resource` - Protected resource metadata

**OAuth Endpoints:**
- `POST /register` - Dynamic client registration (RFC 7591)
- `GET /authorize` - Authorization endpoint with PKCE
- `POST /token` - Token exchange endpoint

**Supported Scopes:** `mcp`, `mcp:read`, `mcp:write`

## Task Runner

The built-in task runner (`brain-runner`) processes tasks with dependency tracking and parallel execution.

### Basic Usage

```bash
# Start the runner in foreground mode
bun run src/runner/index.ts start my-project -f

# Run with interactive TUI dashboard
bun run src/runner/index.ts start my-project --tui

# List available commands
bun run src/runner/index.ts --help
```

### TUI Dashboard

The `--tui` flag enables an interactive terminal dashboard built with [Ink](https://github.com/vadimdemedes/ink):

```
┌─ my-project ──────────────────────────────────────────────────────────────┐
│  ● 2 ready   ○ 3 waiting   ▶ 1 active   ✓ 5 done                          │
├───────────────────────────────────────────────────────────────────────────┤
│ Tasks                              │ Logs                                  │
│ ────────────────────────────────── │ ───────────────────────────────────── │
│ ● Setup base config                │ 17:30:45 INFO  Runner started         │
│ └─○ Create utils module            │ 17:30:46 INFO  Task started...        │
│   └─○ Create main entry            │ 17:30:47 DEBUG Polling...             │
├───────────────────────────────────────────────────────────────────────────┤
│ ↑↓/j/k Navigate  Tab: Switch  r: Refresh  ?: Help  q: Quit               │
└───────────────────────────────────────────────────────────────────────────┘
```

#### TUI Highlights

- **Real-time task tree** with git-graph style lane rendering and dependency path coloring
- **Feature grouping** with collapsible headers, pause indicators, and checkout actions
- **Status indicators**: `●` ready, `○` waiting, `▶` running, `✓` completed, `✗` blocked
- **Priority markers**: `!` for high priority tasks
- **Cycle detection**: `↺` marks circular dependencies
- **Multi-select operations** for batch status changes and deletions
- **Metadata popup** for editing all task properties inline
- **Settings popup** with concurrency limits, model overrides, and group visibility
- **Cron panel** for browsing schedules, run history, and managing task-cron links
- **Mouse support** with click navigation, hover preview, and header collapse
- **Live resource metrics** (CPU/memory) and connection status in the status bar
- **SSE streaming** with automatic polling fallback for reliability

#### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `j/k` | Navigate up/down |
| `g/G` | Jump to first/last task |
| `Tab` | Cycle focus (tasks → logs → details) |
| `Enter` | Select task / toggle feature collapse |
| `Space` | Toggle multi-select |
| `s` | Status change (single or bulk) |
| `e` | Edit task in `$EDITOR` |
| `y` | Yank task info to clipboard |
| `w` | Toggle text wrap/truncation |
| `x` | Focus mode (run feature to completion) |
| `p` | Pause/resume (project, feature, or task) |
| `o` | Open settings popup |
| `O` | Open OpenCode session in tmux |
| `r` | Refresh task list |
| `L` | Toggle logs panel visibility |
| `c` | Switch to cron panel |
| `Backspace` | Open metadata popup |
| `d` | Delete selected tasks |
| `?` | Toggle help overlay |
| `q` | Quit |

**Multi-project mode adds:**

| Key | Action |
|-----|--------|
| `h/[` | Previous project tab |
| `l/]` | Next project tab |
| `1-9` | Jump to project tab |

## Brain CLI

The `brain` CLI manages the API server and diagnoses brain configuration issues.

### Server Commands

```bash
# Start the API server (background)
brain start

# Stop the server
brain stop

# Restart the server
brain restart

# Check server status
brain status

# Show health check
brain health

# View logs
brain logs       # Recent logs
brain logs -f    # Follow logs

# Development mode (foreground with hot reload)
brain dev

# Show configuration
brain config
```

### Doctor Command

The `brain doctor` command diagnoses and fixes brain configuration issues:

```bash
# Run diagnostics (show failures only)
brain doctor

# Verbose output (show all checks)
brain doctor -v

# Fix fixable issues
brain doctor --fix

# Reset modified files to reference templates
brain doctor --fix --force

# Preview fixes without applying
brain doctor --fix --dry-run
```

#### What Doctor Checks

| Category | Checks |
|----------|--------|
| **ZK CLI** | CLI available, correct version |
| **ZK Notebook** | `.zk` directory exists |
| **ZK Config** | `id-length = 8`, `id-charset = alphanum` |
| **Directory Structure** | `projects/`, `global/` directories |
| **Templates** | All 12 entry type templates present and valid |

#### Entry Templates

Doctor validates these templates in `.zk/templates/`:

| Template | Entry Type |
|----------|------------|
| `summary.md` | Session summaries, key decisions |
| `report.md` | Analysis reports, code reviews |
| `walkthrough.md` | Code explanations, architecture overviews |
| `plan.md` | Implementation plans, designs |
| `pattern.md` | Reusable patterns (supports `global: true`) |
| `learning.md` | Best practices (supports `global: true`) |
| `idea.md` | Ideas for future exploration |
| `scratch.md` | Temporary working notes |
| `decision.md` | Architectural decisions, ADRs |
| `exploration.md` | Investigation notes, research |
| `execution.md` | Execution tracking |
| `task.md` | Task entries with dependencies |

### Runner Commands

```bash
# Start runner (foreground or TUI)
brain-runner start [project] [-f|--tui]

# Stop running daemon
brain-runner stop [project]

# Check status
brain-runner status [project]

# Execute single task
brain-runner run-one [project]

# List tasks by state
brain-runner list [project]    # all tasks
brain-runner ready [project]   # ready to execute
brain-runner waiting [project] # waiting on dependencies
brain-runner blocked [project] # blocked tasks

# View logs
brain-runner logs [-f]
```

### Runner Options

| Option | Description |
|--------|-------------|
| `-f, --foreground` | Run in foreground (default) |
| `-b, --background` | Run as daemon |
| `--tui` | Interactive TUI dashboard |
| `-p, --max-parallel N` | Max concurrent tasks across ALL projects |
| `--poll-interval N` | Seconds between polls (default: 30) |
| `-w, --workdir DIR` | Working directory |
| `--dry-run` | Log actions without executing |
| `-v, --verbose` | Enable verbose logging |

## do-work CLI

The `do-work` command is a convenient wrapper for quick task queue operations:

```bash
# Process tasks for a project
do-work start myproject

# With TUI dashboard
do-work start myproject --tui

# View ready tasks
do-work ready myproject

# List all tasks
do-work list myproject
```

This is an alias for `brain-runner` with commonly used defaults.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BRAIN_PORT` | `3333` | API server port |
| `BRAIN_HOST` | `0.0.0.0` | API server host |
| `BRAIN_DIR` | `~/.brain` | Brain data directory |
| `BRAIN_API_URL` | `http://localhost:3333` | API URL (for runner) |
| `ENABLE_TLS` | `false` | Enable HTTPS/TLS |
| `TLS_KEY` | — | Path to TLS private key file (PEM format) |
| `TLS_CERT` | — | Path to TLS certificate file (PEM format) |

## Architecture

```
                           ┌─────────────────────────┐
                           │     MCP Clients          │
                           │  (Claude Code, OpenCode) │
                           └────────────┬────────────┘
                                        │ MCP / HTTP
                           ┌────────────▼────────────┐
                           │     Brain API Server     │
                           │  REST + MCP (Hono/Bun)   │
                           │  OAuth 2.1 + TLS         │
                           ├─────────────────────────┤
                           │  BrainService            │
                           │  TaskService             │
                           │  CronService             │
                           │  FeatureService          │
                           └────────────┬────────────┘
                                        │
                    ┌───────────────────┼───────────────────┐
                    ▼                   ▼                   ▼
           ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
           │  zk CLI      │   │  Markdown    │   │  SSE Stream  │
           │  (CRUD)      │   │  ~/docs/brain│   │  (real-time) │
           └──────────────┘   └──────────────┘   └──────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                      Task Runner (brain-runner)                   │
├──────────────────────────────────────────────────────────────────┤
│  Process Manager     │  Cron Scheduler     │  Feature Executor   │
│  (parallel exec,     │  (cron parsing,     │  (worktree setup,   │
│   PID tracking,      │   pipeline reset,   │   merge intent,     │
│   memory limits)     │   bounded sched)    │   focus mode)       │
├──────────────────────────────────────────────────────────────────┤
│                    OpenCode Executor                              │
│         (spawns AI agents in git worktrees)                      │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                         TUI Dashboard (Ink)                       │
├──────────────────────────────────────────────────────────────────┤
│  StatusBar  │  TaskTree    │  LogViewer  │  CronPanel            │
│  (stats,    │  (lanes,     │  (real-time │  (schedules,          │
│   metrics)  │   features,  │   SSE logs) │   run history,        │
│             │   mouse)     │             │   task links)         │
├──────────────────────────────────────────────────────────────────┤
│  MetadataPopup  │  SettingsPopup  │  PausePopup  │  HelpOverlay │
└──────────────────────────────────────────────────────────────────┘
```

## License

MIT
