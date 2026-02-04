# Brain API

REST API service for AI agent memory and knowledge management, with integrated task queue processing.

Built with [Bun](https://bun.sh) and [Hono](https://hono.dev).

## Features

- Knowledge graph storage with Zettelkasten-style linking
- Full-text search across entries
- Task management with dependency tracking and resolution
- Graph traversal (backlinks, outlinks, related entries)
- Integration with `do-work` task queue processor

## Installation

```bash
# Clone the repository
git clone https://github.com/huynle/brain.git
cd brain

# Install dependencies
bun install
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
| `-p, --max-parallel N` | Max concurrent tasks (default: 3) |
| `--poll-interval N` | Seconds between polls (default: 30) |
| `-w, --workdir DIR` | Working directory |
| `--dry-run` | Log actions without executing |
| `-v, --verbose` | Enable verbose logging |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BRAIN_PORT` | `3333` | API server port |
| `BRAIN_DIR` | `~/.brain` | Brain data directory |
| `BRAIN_API_DIR` | `~/projects/brain-api` | Source directory |

## Architecture

```
+------------------+     +------------------+     +--------------+
|   brain-runner   |---->|   brain-api      |---->| ~/docs/brain |
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
