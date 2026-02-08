---
name: brain-planning
description: "Use when creating, resuming, or updating implementation plans - ensures plans are stored in and retrieved from brain for persistence across sessions, enabling dynamic querying and plan evolution"
---

# Brain-First Planning

All planning work should use the brain as the source of truth. This ensures:
- Plans persist across sessions
- Related plans are discoverable
- Subagents can query plan content dynamically
- Plan changes are tracked with memory strength

**Announce at start:** "I'm using the brain-planning skill to manage this plan with persistent storage."

## Checklist

### Phase 1: Context Discovery (BEFORE any planning)
- [ ] Step 1: Announce skill usage: "Using brain-planning skill for persistent plan management"
- [ ] Step 2: Extract key terms from user's request (objective, domain, technology)
- [ ] Step 3: Search brain for existing plans:
  ```
  brain_inject(query: "{objective} {key_terms}", type: "plan")
  ```
- [ ] Step 4: Search for related patterns that might help:
  ```
  brain_search(query: "{key_terms}", type: "pattern")
  ```
- [ ] Step 5: Present findings to user:
  - If matching plans found: offer to resume/view/create new
  - If patterns found: note they'll inform the new plan
  - If nothing found: proceed with fresh planning

### Phase 2: Plan Retrieval (if resuming existing plan)
- [ ] Step 1: Load full plan content:
  ```
  brain_recall(path: "{plan_id}")
  ```
- [ ] Step 2: Extract current structure:
  ```
  brain_plan_sections(id: "{plan_id}")
  ```
- [ ] Step 3: Present plan summary with options:
  - "Continue where we left off"
  - "Update plan based on new requirements"
  - "View full plan and discuss changes"
- [ ] Step 4: Track plan ID for later updates

### Phase 3: New Plan Creation
- [ ] Step 1: Gather requirements through conversation
- [ ] Step 2: Structure plan with clear headers (## sections)
- [ ] Step 3: Include these sections:
  - **Overview**: What and why
  - **Prerequisites**: Dependencies and setup
  - **Tasks**: Numbered implementation steps
  - **Verification**: How to validate success
  - **Related**: Wiki-links to patterns/learnings
- [ ] Step 4: Draft plan and present for review
- [ ] Step 5: Iterate based on feedback

### Phase 4: Create Tasks with Dependencies
- [ ] Step 1: For each task in the plan, create a brain task entry:
  ```
  brain_save(
    type: "task",
    title: "{Task Name}",
    content: "{task_details_and_acceptance_criteria}",
    status: "pending",
    priority: "high|medium|low",
    project: "{projectId}",
    depends_on: ["{previous_task_id}"],  // IDs from earlier brain_save calls
    tags: ["task", "{feature}"]
  )
  ```
- [ ] Step 2: Capture the returned ID for each task (for dependency linking)
- [ ] Step 3: Build dependency chain - later tasks reference earlier task IDs
- [ ] Step 4: Present task summary table to user

### Phase 5: Plan Updates
- [ ] Step 1: List existing tasks:
  ```
  brain_list(type: "task", project: "{projectId}", status: "pending")
  ```
- [ ] Step 2: Show user what will change
- [ ] Step 3: Update tasks as needed:
  ```
  brain_update(path: "{task_id}", append: "## Updated Requirements\n...")
  ```
- [ ] Step 4: Add new tasks if scope expanded

### Phase 6: Handoff to Execution
- [ ] Step 1: Confirm all tasks are created with dependencies
- [ ] Step 2: Provide execution instructions:
  ```
  Ready for execution:
  - Project: {projectId}
  - Tasks: {N} with dependencies configured
  
  To execute:
    do-work graph {projectId}    # Review dependencies
    do-work start {projectId} --foreground --tui
  ```
- [ ] Step 3: Explain that do-work handles dependency resolution
- [ ] Step 4: User runs do-work separately to start execution

## Plan Structure Template

```markdown
# {Plan Title}

## Overview
Brief description of what this plan achieves and why.

## Prerequisites
- Required setup or dependencies
- Environment requirements
- Prior knowledge assumed

## Tasks

### 1. {Task Name}
**Files:** `path/to/file.ts`
**Details:** What to implement
**Acceptance:** How to verify this task is complete

### 2. {Task Name}
...

## Verification
- [ ] All tests pass
- [ ] Build succeeds
- [ ] Feature works as specified

## Related
- [[Pattern Name]] - Referenced pattern
- [[Previous Plan]] - If building on prior work

## Notes
Any additional context, decisions made, or caveats.
```

## Brain Tool Quick Reference

| Tool | When to Use |
|------|-------------|
| `brain_inject(query, type)` | Quick context at session start |
| `brain_search(query, type)` | Find specific plans/patterns |
| `brain_recall(id)` | Load full content by ID |
| `brain_save(type: "task", ...)` | Create tasks with dependencies |
| `brain_save(type: "plan", ...)` | Save plan overview (optional) |
| `brain_update(path, status)` | Update task status |
| `brain_list(type: "task")` | Browse project tasks |

## Anti-Patterns (Do NOT)

- **Create plans without checking brain first** - May miss existing work
- **Leave tasks only in conversation** - Lost when session ends
- **Forget to set depends_on** - Tasks may execute out of order
- **Skip the checklist** - Leads to inconsistent plan management
- **Forget to capture task IDs** - Can't build dependency chain

## Integration with do-work

When handing off to execution, the brain-based workflow enables:

1. **Dependency Resolution**: do-work resolves task dependencies automatically
2. **Parallel Execution**: Tasks with no dependencies run concurrently
3. **Progress Tracking**: do-work monitors task status via Brain API
4. **Interruption Recovery**: do-work resumes interrupted tasks gracefully

## Tips

1. **Use descriptive task titles** - "Setup database schema" not "Task 1"
2. **Add project tags** - Enables filtering by project
3. **Include acceptance criteria** - Clear definition of done
4. **Capture task IDs** - Essential for building dependency chains
5. **Set priorities correctly** - 🔴 high for blocking tasks
6. **Link related tasks** - Use depends_on for execution order
7. **Run do-work graph** - Verify dependencies before execution
