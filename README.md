# Brain

**Persistent memory and autonomous task execution for AI agents.**

Brain is a REST API + MCP server that gives AI coding agents (Claude Code, OpenCode, etc.) long-term memory, structured knowledge management, and an autonomous task runner that can execute multi-step plans while you sleep. Think of it as a second brain for your AI workflow — it remembers decisions, tracks dependencies, schedules recurring work, and orchestrates parallel execution across projects.

Built with Go and [Bubbletea](https://github.com/charmbracelet/bubbletea).

## Why Brain?

AI coding agents are powerful but stateless — they forget everything between sessions. Brain solves this by providing:

- **Persistent memory** — Save decisions, explorations, patterns, and learnings that survive across sessions
- **Structured task queues** — Break work into dependency-tracked tasks that agents execute autonomously
- **Feature orchestration** — Group related tasks into features, execute them in order, and track progress
- **Scheduled tasks** — Schedule recurring tasks with cron expressions directly in task frontmatter
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

### Scheduled Tasks
- **Cron expression scheduling** via `schedule` field in task frontmatter (standard 5-field syntax)
- **Bounded schedules** with optional `not_before` / `not_after` datetime constraints
- **Run history** tracking with trigger timestamps and outcomes via `runs` field
- **Manual triggers** — trigger a scheduled task on demand with `brain_task_trigger`
- **Automatic reset** — completed scheduled tasks reset for the next run

### Interactive TUI Dashboard
- **Real-time task tree** with dependency visualization and git-graph style lane rendering
- **Feature grouping** with collapsible headers, status indicators, and bulk operations
- **Multi-select** with Space key for batch status changes and deletions
- **Metadata popup** for editing task properties (status, priority, feature, project, schedule)
- **Settings popup** for per-project concurrency limits and runtime model overrides
- **Mouse support** with click-to-select, hover preview, and collapsible sections
- **External editor integration** — press `e` to edit a task in `$EDITOR`
- **Clipboard support** — press `y` to yank task info to system clipboard
- **Focus mode** — press `x` to execute a single feature to completion
- **Pause/resume toggles** for the active project scope; the `All` project tab toggles global execution, while a single project tab toggles only that project
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
- Tools span: entry CRUD, search, graph traversal, task management, scheduled task triggers, section extraction, verification, and link generation

### Multi-Project Mode
- **Shared execution pool** across all projects with a single `--max-parallel` limit
- **Project tabs** with per-project stats and an "All" aggregate view
- **Glob-based project filtering** with `--include` and `--exclude` patterns
- **Per-project concurrency overrides** via the settings popup

## Installation

### Quick Install (Recommended)

```bash
# Install with Go
go install github.com/huynle/brain-api/cmd/brain@latest
go install github.com/huynle/brain-api/cmd/brain-api@latest
go install github.com/huynle/brain-api/cmd/brain-runner@latest

# Or download pre-built binaries from GitHub Releases
# https://github.com/huynle/brain-api/releases
```

This provides the following CLI commands:
- `brain` - Server management and diagnostics
- `brain-api` - API server (used internally)
- `brain-runner` - Task runner with TUI

### From Source

```bash
# Clone and build
git clone https://github.com/huynle/brain-api.git
cd brain-api
make build

# Or use the justfile
just go-build

# Binaries are created in ./bin/
# Optionally, copy to your PATH:
cp ./bin/* ~/.local/bin/
```

If copying to `~/.local/bin`, make sure it's in your `PATH`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Requirements

- [Go](https://go.dev) >= 1.21

To verify your installation:

```bash
# Run diagnostics
brain doctor -v
```

## Usage

### Development Server

```bash
# Start with hot reload
make dev

# Or using justfile
just go-dev
```

### Production

```bash
# Start the server
./bin/brain-api

# Or build first
make build
./bin/brain-api
```

### Testing

```bash
# Run tests
go test ./...
make test

# Type checking (static analysis)
go vet ./...
make typecheck

# Linting
make lint
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
- `PUT /api/v1/runners/:runnerId/shutdown` - Request graceful remote runner shutdown

#### Feature Endpoints
- `GET /api/v1/features/:projectId` - List features for project
- `POST /api/v1/features/:featureId/checkout` - Trigger feature checkout

#### Scheduled Task Endpoints
- `POST /api/v1/tasks/:taskId/trigger` - Manually trigger a scheduled task

#### SSE Streaming
- `GET /api/v1/tasks/:projectId/stream` - Real-time task updates via Server-Sent Events

### Example

```bash
# Start the API server
./bin/brain-api

# Or with make
make dev

# Get ready tasks for a project
curl http://localhost:3333/api/v1/tasks/myproject/ready

# Start a task
curl -X POST http://localhost:3333/api/v1/tasks/abc123/start

# Save a note to the brain
curl -X POST http://localhost:3333/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"type": "decision", "title": "Use Go over TypeScript", "content": "..."}'

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
ENABLE_TLS=true TLS_KEY=./localhost-key.pem TLS_CERT=./localhost.pem ./bin/brain-api
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

#### Scheduled Task Tools
| Tool | Description |
|------|-------------|
| `brain_task_trigger` | Manually trigger a scheduled task run |

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

## Embedding-Based Semantic Search

Brain API supports optional embedding-based semantic search for more intelligent knowledge retrieval. When enabled, you can search by meaning rather than just keywords.

### Features

- **Multiple search strategies**: Choose between `fts` (full-text), `semantic` (embedding-based), or `hybrid` (combined)
- **Automatic fallback**: Gracefully falls back to FTS when embeddings are unavailable or failing
- **Incremental indexing**: Generate embeddings on-demand via the `backfill` command
- **Configurable providers**: Supports any OpenAI-compatible embedding API (OpenRouter, OpenAI, AI Factory, etc.)

### Configuration

Add an `embedding` block to your `config.yaml`:

```yaml
server:
  embedding:
    enabled: true                                     # Enable semantic search
    provider: "openrouter"                            # Provider name (for logging)
    base_url: "https://openrouter.ai/api/v1"          # OpenAI-compatible API endpoint
    api_key_env: "OPENROUTER_API_KEY"                 # Environment variable for API key
    model: "text-embedding-3-small"                   # Embedding model name
    dim: 1536                                         # Embedding dimension (must match model)
    batch_size: 32                                    # Batch size for embedding generation
    timeout_ms: 30000                                 # Request timeout in milliseconds
```

Generate a full default config safely:

```bash
brain config defaults       # print default YAML
brain config init --print   # print the config that would be written
brain config init           # write ~/.config/brain/config.yaml if missing
```

Runner API tokens can also be kept out of `config.yaml` by pointing at an environment variable:

```yaml
runner:
  api_token_env: "BRAIN_API_TOKEN"
```

Set `OPENROUTER_API_KEY` in the environment before running semantic search or backfill.

### Search Strategies

Brain API supports three search strategies:

#### 1. FTS (Full-Text Search) - Default

Traditional keyword-based search using SQLite's FTS5 with BM25 ranking.

```bash
# API request
curl -X POST http://localhost:3333/api/v1/search \
  -H 'Content-Type: application/json' \
  -d '{"query": "authentication JWT", "strategy": "fts"}'
```

**Best for:** Exact keyword matches, technical terms, code snippets

#### 2. Semantic Search

Embedding-based search that finds results by semantic similarity. Understands synonyms and related concepts.

```bash
# API request
curl -X POST http://localhost:3333/api/v1/search \
  -H 'Content-Type: application/json' \
  -d '{"query": "how to secure user logins", "strategy": "semantic"}'
```

**Best for:** Conceptual queries, natural language questions, finding related ideas

#### 3. Hybrid Search

Combines both FTS and semantic search, merging results by relevance score.

```bash
# API request
curl -X POST http://localhost:3333/api/v1/search \
  -H 'Content-Type: application/json' \
  -d '{"query": "JWT token validation", "strategy": "hybrid"}'
```

**Best for:** General-purpose search when you want both keyword precision and semantic understanding

### Generating Embeddings

After enabling embeddings in your config, run the backfill command to generate embeddings for existing notes:

```bash
# Generate embeddings for all notes
brain embeddings backfill

# Dry run to see what would be done
brain embeddings backfill --dry-run

# Filter by project
brain embeddings backfill --project my-project

# Filter by path prefix
brain embeddings backfill --path projects/opencode/

# Verbose output
brain embeddings backfill --verbose
```

**How it works:**
- Scans all notes in the brain
- Identifies notes missing embeddings or with stale embeddings (content changed since last indexing)
- Generates embeddings in batches (configurable via `batch_size`)
- Stores embeddings with metadata (project, type, status, feature, priority) for filtered searches
- Updates `embedding_indexed_at` timestamp to track freshness

**When to run:**
- After enabling embeddings for the first time
- Periodically to update stale embeddings (e.g., weekly)
- After bulk imports or large content updates
- When changing embedding models (requires re-indexing all notes)

### Fallback Behavior

Brain API is designed to work reliably even when embeddings are unavailable:

| Scenario | Behavior |
|----------|----------|
| `enabled: false` | All searches use FTS, semantic/hybrid strategies fall back to FTS |
| Missing API key | Logs warning, falls back to FTS |
| Embedding API unreachable | Logs error, falls back to FTS |
| No embeddings indexed yet | Falls back to FTS (run `brain embeddings backfill`) |
| Query embedding generation fails | Logs error, falls back to FTS |

**No search failures:** Embedding issues never break search — users always get results via FTS fallback.

### Storage Overhead

Embeddings are stored in separate SQLite tables (`note_embeddings`, `note_embeddings_meta`):

- **Typical size**: ~3 MB for a moderate brain with 100-200 notes (using 1536-dim embeddings)
- **Scaling**: Roughly 6-10 KB per note (depends on note length and chunking)
- **Tables**:
  - `note_embeddings`: Stores packed float32 vectors as BLOBs
  - `note_embeddings_meta`: Tracks indexing timestamps and metadata for filtering

### MCP Client Usage

When using the MCP server (Claude Code, OpenCode), the search strategy is automatically determined:

- `brain_search` tool: Uses configured default strategy (usually FTS for compatibility)
- `brain_inject` tool: Uses semantic/hybrid search when embeddings are enabled (better for context retrieval)

Configure strategy preference in client tools by setting the `strategy` parameter:

```typescript
// In MCP tool call
await callTool('brain_search', {
  query: 'authentication patterns',
  strategy: 'hybrid'  // or 'semantic', 'fts'
})
```

## Task Runner

The built-in task runner (`brain-runner`) processes tasks with dependency tracking and parallel execution.

### Basic Usage

```bash
# Start the runner in foreground mode
./bin/brain-runner start my-project -f

# Run with interactive TUI dashboard
./bin/brain-runner start my-project --tui

# List available commands
./bin/brain-runner --help
```

### TUI Dashboard

The `--tui` flag enables an interactive terminal dashboard built with [Bubbletea](https://github.com/charmbracelet/bubbletea):

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
| `p` | Pause/resume active scope; `All` toggles global execution, single-project tabs toggle only that project |
| `o` | Open settings popup |
| `O` | Open OpenCode session in tmux |
| `s` | Shutdown selected runner (runners panel) |
| `r` | Refresh task list |
| `L` | Toggle logs panel visibility |
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
| **Storage Layer** | SQLite database accessible and healthy |
| **Database Health** | Tables exist, migrations applied |
| **Attachment Storage** | Attachment storage root exists, upload limits are configured, database attachment digests match stored blobs, and orphan blobs are reported |
| **Directory Permissions** | Brain directory readable and writable |
| **Tool Versions** | Go version (optional, skippable) |
| **OpenCode Integration** | Plugin installed and configured |

#### Attachment Backup Guidance

First-class attachments are split across SQLite metadata (`brain.db`) and blob files under `server.attachments.storage_root` (default: `<brain_dir>/attachments`). Production backups and exports must include both. If `storage_root` is outside `BRAIN_DIR`, `brain doctor -v` warns so backup jobs can explicitly include that external path alongside `brain.db`.

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

## Automations

Automation entries are brain entries with `type: automation` and a `trigger` plus an `action` in frontmatter. Active automations are evaluated by the runner and create generated tasks when their trigger matches.

Automation execution can be paused globally or by project. In the TUI and PWA,
pause/resume controls are toggles scoped to the active project tab: selecting
`All` toggles global automation pause, while selecting a single project toggles
only that project's automation pause. Project-scoped automation pauses are
reported in runner status as `automationPausedProjects`; global automation pause
continues to use `automationsPaused`.

Scoped automation control endpoints:

| Endpoint | Scope |
|----------|-------|
| `POST /api/v1/tasks/runner/automations/pause` | Pause automations globally |
| `POST /api/v1/tasks/runner/automations/resume` | Resume automations globally |
| `POST /api/v1/tasks/runner/automations/pause/{projectId}` | Pause automations for one project |
| `POST /api/v1/tasks/runner/automations/resume/{projectId}` | Resume automations for one project |

Task execution pause/resume follows the same active-scope rule in the TUI and
PWA: `All` toggles global task execution and a single-project tab toggles only
that project via the existing project pause/resume endpoints.

### Supported trigger capabilities

| Capability | Field | Description |
|------------|-------|-------------|
| Cron schedules | `trigger.type: cron` + `trigger.schedule` | Runs on a standard 5-field cron expression such as `0 3 * * *`. |
| Named events | `trigger.type: event` + `trigger.event` | Matches Brain events such as `task.completed` or `feature.all_completed`. Wildcards such as `task.*` are supported. |
| Webhooks | `trigger.type: webhook` + `trigger.webhook` | Matches incoming `webhook.received` events by path, e.g. `/hooks/deploy`. |
| Runner sessions | `trigger.type: session` | Matches runner session discovery events. If `trigger.event` is omitted, the create wizard defaults to `runner.session_discovered`. |
| Filters | `trigger.filter` | Key/value filters applied to event fields. Use `project` or `project_id` for project scope; `*` matches any value. |
| Deduplication | `trigger.once_per` | Prevents duplicate generation for the same event field value, e.g. `feature_id`, `task_id`, `session`, or `day`. |
| Cooldown | `trigger.cooldown` | Minimum interval between generated runs, expressed as a Go duration such as `5m`, `1h`, or `24h`. |
| Concurrency guard | `trigger.max_concurrent` | Positive integer cap on runnable generated tasks for the automation. |

Automations default to ignoring events generated by other automations to avoid feedback loops. Set `trigger.ignore_automation_events: false` only when an automation intentionally needs to react to automation-generated events.

### Example frontmatter

```yaml
---
type: automation
title: "Feature Code Review"
status: active
trigger:
  type: event
  event: feature.all_completed
  filter:
    project: "*"
  once_per: feature_id
  cooldown: 10m
  max_concurrent: 1
action:
  type: prompt
  execution_mode: current_branch
  complete_on_idle: true
  direct_prompt: |
    Review the completed feature {{.FeatureID}} in project {{.Project}}.
enabled: true
max_runs: 0
---
```

## Web UI (PWA)

Brain ships an installable Progressive Web App that mirrors the TUI — tasks,
real-time logs, automations/goals, the knowledge base, and runners — and is
**embedded directly in the `brain` binary**. When the server runs, the app is
served at `/` from the same origin as the API, so visiting your Brain URL (e.g.
`https://brain.example.com`) loads the full dashboard with no separate deploy
and no CORS.

### Install on your phone

1. Deploy Brain behind HTTPS with auth enabled (see below) and open your domain
   in a mobile browser.
2. Use the browser's **Add to Home Screen / Install app** option. The app
   launches standalone, full-screen, with its own icon.

### Sign in

When `ENABLE_AUTH=true`, the app uses the OAuth 2.1 + PKCE flow built into the
server:

1. Tap **Sign in with PIN** — you're sent to the server's consent page.
2. Enter the `OAUTH_PIN` you configured; you're redirected back, signed in.

Tokens are stored in the browser and refreshed silently. You can also paste a
long-lived API token instead ("Use an API token").

```bash
# Minimum for a phone-installable, authenticated deployment:
ENABLE_AUTH=true
OAUTH_PIN=your-secure-pin
CORS_ORIGIN=https://brain.example.com   # optional; same-origin needs no CORS
```

HTTPS is required for PWA installation and is normally provided by your reverse
proxy (Traefik labels are stubbed in `docker-compose.yml`); the server honors
`X-Forwarded-Proto`.

### Keyboard shortcuts (desktop)

The PWA mirrors the TUI's keyboard model — press `?` for the full list. Highlights:

- `j`/`k`, `g`/`G` — move the cursor; `Enter` opens.
- `H`/`L` — switch tabs; `h`/`l`/`[`/`]`/`1–9` — switch projects.
- Tasks: `Space` select, `A`/`D` select/clear all, `c` complete, `x` run, `X`
  cancel, `d` delete, `s` metadata, `e` edit, `y` yank, `/` filter, `C`
  Tasks⇄Schedules, `n` new — selection enables batch complete/edit/delete.
- Brain: `/` search, `e` edit, `b`/`B`/`F`/`A` embed/re-embed; Automations:
  `Space` enable, `x` reconcile, `e` configure, `C` Automations⇄Dream.
- `p`/`P` toggle pause/resume for the active scope (`All` toggles global;
  single-project tabs toggle only that project), `S` settings, `w` wrap, `r`
  refresh.

### Build & develop

The web app lives in [`web/`](web/README.md) and compiles into
`internal/webui/dist`.

```bash
just web-dev      # PWA dev server (HMR) at :5179, proxying the API
just web-build    # compile the PWA into internal/webui/dist
just build-all    # web-build + build the Go binary with the UI embedded
```

`just release` and `docker build` build the web UI automatically. A plain
`just build` embeds whatever assets are already present (a placeholder page is
served if the UI hasn't been built).

> Two TUI features have no browser equivalent and are intentionally omitted:
> spawning your local `$EDITOR` (replaced by an in-app editor) and tmux/full-screen
> session reattach (logs are streamed instead).

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BRAIN_PORT` | `3333` | API server port |
| `BRAIN_HOST` | `0.0.0.0` | API server host |
| `BRAIN_DIR` | `~/.brain` | Brain data directory |
| `BRAIN_API_URL` | `http://localhost:3333` | API URL (for runner) |
| `server.attachments.storage_root` | `<BRAIN_DIR>/attachments` | Attachment blob storage root in `config.yaml`; include with `brain.db` in backups |
| `server.attachments.max_upload_size_bytes` | `104857600` | Maximum attachment upload size in bytes |
| `ENABLE_AUTH` | `false` | Require auth (API token or OAuth) on `/api/v1/*` |
| `OAUTH_PIN` | — | PIN shown on the OAuth consent page; used by the PWA "Sign in with PIN" flow |
| `CORS_ORIGIN` | `*` | Allowed CORS origin; the embedded PWA is same-origin and needs no CORS |
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
                           │    (Go standard lib)     │
                           │  OAuth 2.1 + TLS         │
                           ├─────────────────────────┤
                            │  BrainService            │
                            │  TaskService             │
                            │  FeatureService          │
                           └────────────┬────────────┘
                                        │
                    ┌───────────────────┼───────────────────┐
                    ▼                   ▼                   ▼
           ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
           │  SQLite DB   │   │  Markdown    │   │  SSE Stream  │
           │  (storage)   │   │  ~/docs/brain│   │  (real-time) │
           └──────────────┘   └──────────────┘   └──────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                      Task Runner (brain-runner)                   │
├──────────────────────────────────────────────────────────────────┤
│  Process Manager     │  Schedule Executor  │  Feature Executor   │
│  (parallel exec,     │  (cron parsing,     │  (worktree setup,   │
│   PID tracking,      │   schedule reset,   │   merge intent,     │
│   memory limits)     │   bounded sched)    │   focus mode)       │
├──────────────────────────────────────────────────────────────────┤
│                    OpenCode Executor                              │
│         (spawns AI agents in git worktrees)                      │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                    TUI Dashboard (Bubbletea)                      │
├──────────────────────────────────────────────────────────────────┤
│  StatusBar  │  TaskTree    │  LogViewer  │  TaskDetail           │
│  (stats,    │  (lanes,     │  (real-time │  (properties,         │
│   metrics)  │   features,  │   SSE logs) │   schedule info,      │
│             │   mouse)     │             │   dependencies)       │
├──────────────────────────────────────────────────────────────────┤
│  MetadataPopup  │  SettingsPopup  │  PausePopup  │  HelpOverlay │
└──────────────────────────────────────────────────────────────────┘
```

## License

MIT
