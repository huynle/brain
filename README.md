# brain-api-tasks Experiment

## Objective

Integrate the `do-work` task queue processor with `brain-api` by adding a dedicated TaskService that handles:

1. **Task Query Endpoints** - List tasks with dependency resolution
2. **Task Classification** - Ready/waiting/blocked status based on dependencies
3. **Task Lifecycle** - Start/complete/block operations
4. **Workdir Resolution** - Resolve $HOME-relative paths for task execution
5. **Project Discovery** - Find all projects with task directories

## Current State

### Files Copied

- `src/` - Full brain-api source (TypeScript/Hono)
- `scripts/do-work-original` - Original bash script (~1600 lines)

### What do-work Does (that brain-api doesn't)

| Feature | do-work (bash) | brain-api |
|---------|---------------|-----------|
| List tasks by project | zk CLI + jq | GET /entries (no project filter) |
| Dependency resolution | Complex jq logic | Not implemented |
| Cycle detection | In jq | Not implemented |
| Task classification | ready/waiting/blocked | Not implemented |
| Parent hierarchy | jq traversal | Not implemented |
| Workdir resolution | bash function | Stores fields only |
| Running task state | JSON files | Not implemented |

## Implementation Plan

### Phase 1: Task Query Endpoints

Add to brain-api:
- `GET /api/v1/tasks/:projectId` - List all tasks for project
- `GET /api/v1/tasks/:projectId/ready` - Ready tasks (deps satisfied)
- `GET /api/v1/tasks/:projectId/waiting` - Waiting on dependencies
- `GET /api/v1/tasks/:projectId/blocked` - Blocked tasks
- `GET /api/v1/tasks/:projectId/next` - Next task to execute

### Phase 2: Dependency Resolution

Port jq logic to TypeScript:
- Build task lookup maps
- Resolve dependency references (by ID or title)
- Detect cycles (iterative reachability)
- Classify tasks based on dependency status
- Handle parent hierarchy

### Phase 3: Task Lifecycle

Add endpoints:
- `POST /api/v1/tasks/:taskId/start` - Mark in_progress
- `POST /api/v1/tasks/:taskId/complete` - Mark completed
- `POST /api/v1/tasks/:taskId/block` - Mark blocked with reason

### Phase 4: Workdir Resolution

Add to TaskService:
- Resolve $HOME-relative workdir/worktree paths
- Fall back to git remote search
- Return absolute path for task execution

### Phase 5: Simplified do-work

Rewrite do-work to use brain-api:
- Replace zk calls with HTTP calls
- Remove jq dependency resolution
- Keep only OpenCode spawning logic
- Target: ~100 lines instead of ~1600

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌──────────────┐
│   do-work       │────▶│   brain-api     │────▶│  ~/docs/brain│
│   (simplified)  │     │   TaskService   │     │  (markdown)  │
└─────────────────┘     └─────────────────┘     └──────────────┘
        │                       │
        │ (spawns)              │ (uses)
        ▼                       ▼
┌─────────────────┐     ┌─────────────────┐
│   OpenCode      │     │   zk CLI        │
│   (task exec)   │     │                 │
└─────────────────┘     └─────────────────┘
```

## Running

```bash
# Start brain-api (in this experiment)
bun run src/index.ts

# Test endpoints
curl http://localhost:3333/api/v1/tasks/test/ready
```

## Testing

```bash
bun test
```
