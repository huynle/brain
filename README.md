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

## Architecture

```
+------------------+     +------------------+     +--------------+
|   do-work        |---->|   brain-api      |---->| ~/docs/brain |
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

## License

MIT
