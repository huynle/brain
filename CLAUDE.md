# CLAUDE.md

This file provides guidance for AI assistants working with the brain codebase.

## Project Overview

Brain API is a REST service for AI agent memory and knowledge management, with an integrated task queue processor. Built with Bun, Hono, and Ink (for TUI).

## Key Commands

```bash
# Development
bun run dev          # Start server with hot reload
bun test             # Run all tests
bun run typecheck    # TypeScript type checking

# Task Runner
bun run src/runner/index.ts start <project> --tui  # TUI dashboard
bun run src/runner/index.ts list <project>         # List tasks
```

## Architecture

### Core API (`src/api/`)
- `entries.ts` - CRUD for brain entries
- `search.ts` - Full-text search
- `graph.ts` - Graph traversal (backlinks, outlinks)
- `tasks.ts` - Task queue endpoints
- `sections.ts` - Section extraction from entries

### Core Services (`src/core/`)
- `brain-service.ts` - Main service layer
- `task-service.ts` - Task management with dependency resolution
- `task-deps.ts` - Dependency graph algorithms
- `zk-client.ts` - Zettelkasten CLI wrapper

### Task Runner (`src/runner/`)
- `index.ts` - CLI entry point with argument parsing
- `task-runner.ts` - Main runner orchestration
- `api-client.ts` - Brain API client
- `opencode-executor.ts` - OpenCode process spawning
- `process-manager.ts` - Child process lifecycle
- `state-manager.ts` - Persistent state for runner
- `signals.ts` - Graceful shutdown handling

### TUI Dashboard (`src/runner/tui/`)

The TUI uses Ink (React for CLI) with a component-based architecture:

```
App.tsx
├── StatusBar.tsx      # Top bar: project name, task stats, connection status
├── TaskTree.tsx       # Left panel: dependency tree visualization
├── LogViewer.tsx      # Right top: real-time log display
├── TaskDetail.tsx     # Right bottom: selected task details
└── HelpBar.tsx        # Bottom: keyboard shortcuts
```

#### TUI Hooks
- `useTaskPoller.ts` - Polls API for task updates, manages connection state
- `useLogStream.ts` - Manages log entry buffer with max entries limit

#### TUI State Management
- Focus state: tracks which panel (tasks/logs) is active
- Selection state: currently selected task ID
- Stats: computed from task list (ready/waiting/active/completed)

#### Key Design Decisions
1. **Ink over raw terminal** - React component model, testable with ink-testing-library
2. **Polling over WebSocket** - Simpler, works with standard REST API
3. **Dependency tree flattening** - Root tasks shown first, children indented
4. **Cycle detection** - Circular deps marked with `↺` symbol

## Testing Patterns

Tests use Bun's test runner with:
- `ink-testing-library` for TUI component tests
- Mock tasks for isolated testing
- Integration tests for API endpoints

```typescript
// TUI component test pattern
const { lastFrame, stdin, unmount } = render(<Component {...props} />);
expect(lastFrame()).toContain('expected text');
stdin.write('j'); // Send keyboard input
unmount();
```

## Common Tasks

### Adding a TUI Component
1. Create component in `src/runner/tui/components/`
2. Add types to `src/runner/tui/types.ts`
3. Create test file with `.test.tsx` suffix
4. Import in `App.tsx`

### Adding API Endpoints
1. Add route in `src/api/*.ts`
2. Add test in same file or `tests/` directory
3. Update API client in `src/runner/api-client.ts`

### Debugging TUI
```bash
# Run with verbose logging
bun run src/runner/index.ts start project --tui -v

# Check logs
bun run src/runner/index.ts logs -f
```

## File Conventions

- Tests: `*.test.ts` or `*.test.tsx` alongside source
- Types: `types.ts` in each major directory
- Entry points: `index.ts`

## Multi-Project Mode

The task runner supports monitoring multiple projects simultaneously with a shared execution pool.

### Basic Usage

```bash
# Monitor all projects
bun run src/runner/index.ts start all --tui

# Filter with glob patterns
bun run src/runner/index.ts start all --include 'prod-*' --exclude 'prod-legacy'
bun run src/runner/index.ts start all -i 'brain-*' -e 'test-*'

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
- **Parallel polling**: All projects fetched simultaneously via `useMultiProjectPoller`
- **Composite task keys**: Tasks tracked as `projectId:taskId` internally
- **Project tabs**: First tab shows "All" aggregate, then individual project tabs

### Key Components

```
Task Runner Multi-Project Architecture

TaskRunner
├── projects: string[]              # List of projects to poll
├── isMultiProject: boolean         # Enables multi-project behavior
└── Shared ProcessManager           # Single pool for all projects

TUI (App.tsx)
├── useMultiProjectPoller           # Fetches all projects in parallel
│   ├── tasksByProject: Map         # Tasks keyed by project
│   ├── statsByProject: Map         # Stats keyed by project
│   └── aggregateStats              # Combined stats for "All" view
├── StatusBar                       # Shows project tabs with task counts
└── activeProject state             # Current tab selection
```

### CLI Options for Multi-Project

```bash
-i, --include PATTERN    Include projects matching glob (repeatable)
-e, --exclude PATTERN    Exclude projects matching glob (repeatable)
-p, --max-parallel N     Max concurrent tasks across ALL projects
```

### Filter Examples

```bash
# Only production projects
bun run src/runner/index.ts start all -i 'prod-*'

# All except test projects
bun run src/runner/index.ts start all -e 'test-*' -e '*-staging'

# Brain projects except legacy
bun run src/runner/index.ts start all -i 'brain-*' -e 'brain-legacy'
```
