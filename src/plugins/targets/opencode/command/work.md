---
description: Manual task processing and queue management
subtask: true
---

# Work Command

Manual task processing - when you want direct control instead of the do-work monitor.

## Usage

```bash
/work                              # Show queue status (default)
/work list                         # List all pending tasks
/work next                         # Process next ready task in-session
/work <task-id>                    # Process specific task in-session
/work complete <task-id>           # Mark task complete
/work block <task-id> "reason"     # Mark task blocked
```

## Routing Decision

| Input Pattern | Action |
|---------------|--------|
| Empty or just `/work` | Show queue status summary |
| `list` | List all pending tasks sorted by priority |
| `next` | Process next ready task (highest priority) |
| 8-char alphanumeric ID | Process specific task by ID |
| `complete <id>` | Mark task as completed |
| `block <id> "reason"` | Mark task as blocked with reason |

## Execution

### Default: Show Queue Status

```
brain_list(type: "task", status: "pending", sortBy: "priority")
brain_list(type: "task", status: "in_progress")
brain_list(type: "task", status: "blocked")
```

Display:
```
TASK QUEUE STATUS
=================
Pending: 5 tasks (2 high, 2 medium, 1 low)
In Progress: 1 task
Blocked: 0 tasks

Next ready: "Fix login crash" (high)

Commands:
  /work next           Process next task
  /work list           See all tasks
  /do start <project>  Start automated processing
```

### List All Tasks

```
brain_list(type: "task", status: "pending", sortBy: "priority")
```

Display sorted by priority (high first):
```
PENDING TASKS
=============
[high]   Fix login crash (abc12def)
[high]   Update auth flow (def34567)
[medium] Add export button (ghi78901)
[medium] Refactor utils (jkl23456)
[low]    Cleanup logs (mno78901)
```

### Process Next Task

1. Find next ready task:
   ```
   brain_list(type: "task", status: "pending", sortBy: "priority", limit: 1)
   ```

2. Load the do-work skill for processing guidance

3. Follow the per-task workflow from the skill:
   - Claim task: `brain_update(path: "...", status: "in_progress")`
   - Triage complexity (Route A/B/C)
   - Execute appropriate workflow
   - Complete: `brain_update(path: "...", status: "completed")`
   - Commit changes

### Process Specific Task

Same as "next" but with explicit task ID:
```
brain_recall(path: "<task-id>")
```

Then follow the per-task workflow.

### Complete Task

```
brain_update(path: "<task-id>", status: "completed", append: "## Completion\nMarked complete manually.")
```

Report:
```
Completed: "Fix login crash" (abc12def)
```

### Block Task

```
brain_update(path: "<task-id>", status: "blocked", note: "<reason>")
```

Report:
```
Blocked: "Fix login crash" (abc12def)
Reason: <reason>
```

## Integration with do-work Skill

When processing tasks (`/work next` or `/work <id>`), this command loads the `do-work` skill which provides:

1. **Triage System** - Route A (simple), B (explore), C (complex)
2. **Per-Task Workflow** - How to process each task
3. **Brain Tools Reference** - All brain operations for queue management
4. **Script Commands** - Reference for do-work bash script

## Examples

```bash
# Check queue status
/work
# -> Shows pending/in-progress/blocked counts

# See all pending tasks
/work list
# -> Lists all pending tasks by priority

# Process next task manually
/work next
# -> Claims and processes highest priority ready task

# Process specific task
/work abc12def
# -> Claims and processes task with ID abc12def

# Mark task complete without processing
/work complete abc12def
# -> brain_update(path: "abc12def", status: "completed")

# Block a task
/work block abc12def "Waiting on API design"
# -> brain_update(path: "abc12def", status: "blocked", note: "...")
```

## When to Use /work vs /do

| Scenario | Command |
|----------|---------|
| Capture a new task | `/do <description>` |
| Start automated processing | `/do start` |
| Stop automated processing | `/do stop` |
| Check monitor status | `/do status` |
| Manual in-session processing | `/work next` |
| Process specific task manually | `/work <task-id>` |
| Quick status check | `/work` |
| Mark task done manually | `/work complete <id>` |

## Philosophy

- **Direct control** - Process tasks in current session
- **Simple interface** - Status, list, process, complete, block
- **Complements automation** - Use when you want oversight
- **Brain-backed** - All state in brain for persistence
