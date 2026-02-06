# Brain

REST API service for AI agent memory and knowledge management, with integrated task queue processing.

Built with [Bun](https://bun.sh) and [Hono](https://hono.dev).

## Features

- Knowledge graph storage with Zettelkasten-style linking
- Full-text search across entries
- Task management with dependency tracking and resolution
- Graph traversal (backlinks, outlinks, related entries)
- Integration with `do-work` task queue processor
- TUI dashboard for task monitoring
- Embedded MCP server via Streamable HTTP transport
- OAuth 2.1 authentication with PKCE for secure client connections
- Multi-project support with shared execution pools

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
- `/api/v1/search` - Full-text search
- `/api/v1/graph` - Graph traversal operations

#### Task Endpoints
- `GET /api/v1/tasks/:projectId` - List all tasks for project
- `GET /api/v1/tasks/:projectId/ready` - Ready tasks (dependencies satisfied)
- `GET /api/v1/tasks/:projectId/waiting` - Waiting on dependencies
- `GET /api/v1/tasks/:projectId/blocked` - Blocked tasks
- `GET /api/v1/tasks/:projectId/next` - Next task to execute
- `POST /api/v1/tasks/:taskId/start` - Mark task in_progress
- `POST /api/v1/tasks/:taskId/complete` - Mark task completed
- `POST /api/v1/tasks/:taskId/block` - Mark task blocked

### Example

```bash
# Start the API server
bun run start

# Get ready tasks for a project
curl http://localhost:3333/api/v1/tasks/myproject/ready

# Start a task
curl -X POST http://localhost:3333/api/v1/tasks/abc123/start
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

### Available Tools

| Tool | Description |
|------|-------------|
| `brain_save` | Save content to the brain (notes, plans, decisions, etc.) |
| `brain_recall` | Retrieve a specific entry by path, ID, or title |
| `brain_search` | Full-text search across entries |
| `brain_list` | List entries with filtering by type, status, etc. |
| `brain_inject` | Get relevant context for a task query |
| `brain_update` | Update an entry's status, title, or append content |
| `brain_stats` | Get brain statistics |
| `brain_check_connection` | Verify the brain API is available |

The embedded MCP server calls the service layer directly (no HTTP round-trip), making it faster than the standalone stdio-based MCP server.

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

#### Features

- **Real-time task tree** with dependency visualization
- **Status indicators**: `●` ready, `○` waiting, `▶` running, `✓` completed, `✗` blocked
- **Priority markers**: `!` for high priority tasks
- **Cycle detection**: `↺` marks circular dependencies
- **Live logs** with timestamps and log levels
- **Connection status** indicator

#### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑/k` | Navigate up |
| `↓/j` | Navigate down |
| `Tab` | Switch focus (tasks/logs) |
| `Enter` | Select task |
| `r` | Refresh task list |
| `?` | Toggle help |
| `q` | Quit |

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
+------------------+     +------------------+     +--------------+
|   brain-runner   |---->|   brain          |---->| ~/docs/brain |
|   (task runner)  |     |   TaskService    |     | (markdown)   |
+------------------+     +------------------+     +--------------+
        |                        |
        | (spawns)               | (uses)
        v                        v
+------------------+     +------------------+
|   OpenCode       |     |   zk CLI         |
|   (task exec)    |     |                  |
+------------------+     +------------------+
```

### TUI Architecture

```
src/runner/tui/
├── App.tsx              # Main app component with layout
├── index.tsx            # Entry point and configuration
├── types.ts             # TypeScript interfaces
├── components/
│   ├── StatusBar.tsx    # Project status and stats
│   ├── TaskTree.tsx     # Dependency tree visualization
│   ├── TaskDetail.tsx   # Selected task details
│   ├── LogViewer.tsx    # Real-time log display
│   └── HelpBar.tsx      # Keyboard shortcuts bar
└── hooks/
    ├── useTaskPoller.ts # API polling for tasks
    └── useLogStream.ts  # Log entry management
```

## License

MIT
