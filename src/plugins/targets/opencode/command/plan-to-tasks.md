---
description: Convert implementation plans into dependency-aware task queues for autonomous execution by do-work
---

# /plan-to-tasks - Plan to Task Queue

Convert a structured implementation plan into executable tasks with proper dependency ordering for do-work to process autonomously.

**Project:** `$1` (optional - defaults to brain's auto-generated project ID)

<critical_rules>
  <rule id="require_plan">REQUIRE a structured implementation plan - do NOT extract from vague discussion</rule>
  <rule id="validate_first">ALWAYS dispatch explore agent to validate feasibility before queuing</rule>
  <rule id="show_graph">ALWAYS show task graph and get confirmation before creating tasks</rule>
  <rule id="proper_dependencies">Create tasks with correct depends_on - dependent tasks created BEFORE independent ones</rule>
  <rule id="atomic_queue">Either queue ALL tasks successfully or NONE (rollback on failure)</rule>
  <rule id="no_implementation">This command ONLY queues tasks - it does NOT implement anything</rule>
  <rule id="no_parent_task">Do NOT create parent/container tasks - only create executable tasks</rule>
  <rule id="depends_on_field">Use the `depends_on` field in brain_save - do NOT put dependencies in content</rule>
  <rule id="draft_then_promote">Create ALL tasks as "draft" first, then batch-promote to "pending" after the LAST task is created</rule>
</critical_rules>

## What This Command Does

1. **Locates implementation plan** - From brain or current conversation
2. **Validates plan exists** - Exits if no structured plan found
3. **Dispatches validation agent** - Checks feasibility against codebase
4. **Extracts task structure** - Phases, steps, dependencies
5. **Presents task graph** - Interactive editing before commit
6. **Creates tasks as drafts** - All tasks created with `status: "draft"` first
7. **Batch promotes to pending** - After ALL tasks created, promotes them to `status: "pending"`
8. **Hands off to do-work** - Ready for autonomous execution

**Why draft-then-promote?**
- Prevents race condition where do-work executes tasks before all dependencies are filed
- Ensures complete dependency graph exists before any task becomes visible to do-work
- Atomic: either all tasks become pending together, or none do (on failure)

**This command does NOT:**
- Implement any code (do-work handles that)
- Extract plans from vague discussions (requires structured plan)
- Skip validation (always checks feasibility first)
- Create parent/container tasks (only creates executable tasks)

---

## Startup Sequence

### Step 1: Resolve Project

```
if $1 is provided:
  PROJECT = "$1"
  PROJECT_DISPLAY = "project: $1"
else:
  PROJECT = null  # brain uses default based on working directory
  PROJECT_DISPLAY = "project: (default)"
```

### Step 2: Locate Implementation Plan

Prompt user for plan source:

```
I'll convert an implementation plan into executable tasks.

Where is the plan?
  1. Load from brain (provide path or title)
  2. Use plan from current conversation
  3. Paste a plan now

> 
```

**Option 1: Brain reference**

```
Brain plan path or title: 
```

Then:
```
brain_recall(path: "<provided>") OR brain_recall(title: "<provided>")
```

Validate it's type: "plan" or contains structured implementation steps.

**Option 2: Current conversation**

Scan conversation for a structured implementation plan. Look for:
- Numbered phases/steps
- File paths mentioned
- Clear implementation sequence
- Headers like "Implementation Plan", "Tasks", "Phases"

**Option 3: Paste now**

```
Paste your implementation plan (end with a line containing only "END"):
```

Accept multi-line input until "END" marker.

### Step 3: Validate Plan Exists

A valid implementation plan MUST have:
- Clear title/objective
- Numbered or bulleted steps
- Some indication of files or components involved

**If no valid plan found:**

```
No valid implementation plan found.

I need a structured implementation plan with:
  - Clear phases or steps (numbered/bulleted)
  - Files or components involved
  - Logical implementation sequence

Example structure:
  ## Implementation Plan: Feature Name
  
  ### Phase 1: Setup
  1. Create schema in `prisma/schema.prisma`
  2. Generate types in `src/types/`
  
  ### Phase 2: Backend
  3. Implement service in `src/services/`
  4. Add API routes in `src/routes/`
  
  ### Phase 3: Frontend
  5. Build UI components in `src/components/`

Tip: Use the brainstorming skill or /plan command to create one first.
```

Then EXIT. Do not proceed without a valid plan.

---

## Validation Phase

### Step 4: Dispatch Validation Agent

```
Validating plan against codebase...
```

```
Task(
  subagent_type: "explore",
  description: "Validate plan: <plan_title>",
  prompt: @validation_prompt
)
```

<validation_prompt>
PLAN FEASIBILITY VALIDATION

**Plan Title:** $PLAN_TITLE

**Plan Content:**
```
$PLAN_CONTENT
```

**Working Directory:** $PWD

---

## Your Task

Validate this implementation plan is feasible against the current codebase. Do NOT implement anything.

### Validation Checks

1. **File Paths**
   - Do referenced files exist? Are paths correct?
   - For new files, is the directory structure valid?
   - Use glob/grep to verify paths

2. **Pattern Consistency**
   - Does the plan follow existing codebase conventions?
   - Find similar implementations to compare against
   - Check naming conventions, file organization

3. **Dependencies**
   - Are required imports/packages available?
   - Any missing dependencies not mentioned in plan?

4. **Ordering Logic**
   - Is the proposed sequence logical?
   - Any hidden dependencies between steps?
   - Can any steps be parallelized?

5. **Scope Completeness**
   - Are there implicit requirements not captured?
   - Missing error handling, tests, types?

### Return Format

```markdown
## Validation: $PLAN_TITLE

### Status: <VALID | NEEDS_ADJUSTMENT | INVALID>

### File Check
- [x] `path/to/file` - exists
- [ ] `path/to/file` - not found, suggest: `actual/path`
- [~] `path/to/new/file` - will be created (directory exists)

### Pattern Check
- [x] Follows existing pattern in `similar/file.ts`
- [!] Deviates from convention: <explanation>

### Dependency Check
- [x] All imports available
- [!] Missing package: `package-name`

### Ordering Check
- [x] Logical sequence
- [!] Reorder: Step N should come before Step M because <reason>

### Parallelization Opportunities
- Steps X and Y can run concurrently (no shared dependencies)

### Scope Check
- [!] Plan doesn't mention: <implicit requirement>

### Recommendations
1. <actionable suggestion>
2. <actionable suggestion>

### Adjusted Plan (only if NEEDS_ADJUSTMENT)
<corrected plan structure with fixes applied>
```
</validation_prompt>

### Step 5: Handle Validation Result

**If VALID:**
```
Validation passed.
  - All file paths verified
  - Follows existing patterns
  - Logical ordering confirmed

Proceeding to task breakdown...
```

**If NEEDS_ADJUSTMENT:**
```
Validation found issues:

  [!] File path: `src/auth.ts` should be `src/lib/auth.ts`
  [!] Missing step: Need to update types before API changes
  [!] Reorder: Schema migration should come before service impl

Adjusted plan:
  1. Create schema migration
  2. Update types  
  3. Implement service
  ...

Accept adjusted plan? [y/n/edit]
```

- `y` - Use adjusted plan
- `n` - Exit, user will revise manually
- `edit` - Let user modify the adjustments

**If INVALID:**
```
Validation failed:

  [x] Core assumption incorrect: <reason>
  [x] Conflicting requirements: <details>
  [x] Missing critical context: <what's needed>

Cannot proceed. Please revise the plan and try again.

Specific issues to address:
  1. <issue>
  2. <issue>
```

Then EXIT.

---

## Task Breakdown Phase

### Step 6: Determine Granularity

```
Plan structure:
  - Phases: N
  - Total steps: M

How granular should tasks be?
  A. One task per phase (N tasks) - faster execution, less parallelism
  B. One task per step (M tasks) - balanced [recommended]
  C. Custom - specify groupings

Choice [A/B/C]: 
```

**If C (Custom):**
```
Steps in plan:
  1. Create schema
  2. Generate types
  3. Implement service
  4. Add API routes
  5. Build form component
  6. Build list component
  7. Write tests
  8. Update documentation

Group steps into tasks (comma-separated step numbers per task):
  Example: "1,2" "3,4" "5,6" "7" "8" creates 5 tasks

Your groupings: 
```

Parse input like `"1,2" "3,4" "5,6,7" "8"` into task groups.

### Step 7: Build Dependency Graph

**Auto-detect dependencies from:**

1. **Explicit markers in plan:**
   - "after X", "once Y is done", "requires Z"
   - "depends on", "following", "then"

2. **Implicit ordering patterns:**
   - Schema/migration → Types → Service → API → UI
   - Config → Implementation → Tests → Docs
   - Parent component → Child components

3. **File dependencies:**
   - If step B imports from files step A creates
   - If step B modifies files step A creates

**Assign priorities:**

| Position | Default Priority |
|----------|------------------|
| Foundation tasks (schema, config) | high |
| Core implementation | high |
| UI/Integration | medium |
| Tests, docs, polish | low |

User can adjust in edit phase.

### Step 8: Present Task Graph

```
Task Graph: "$PLAN_TITLE"
Project: $PROJECT_DISPLAY

  1. [high] $TASK_1_TITLE
     Files: $FILES
     depends_on: []
     
  2. [high] $TASK_2_TITLE
     Files: $FILES
     depends_on: [#1]
     
  3. [high] $TASK_3_TITLE
     Files: $FILES
     depends_on: [#2]
     
  4. [medium] $TASK_4_TITLE
     Files: $FILES
     depends_on: [#3]
     
  5. [medium] $TASK_5_TITLE
     Files: $FILES
     depends_on: [#3]
     
  6. [low] $TASK_6_TITLE
     Files: $FILES
     depends_on: [#4, #5]

Total: 6 tasks
Parallelizable: #4 and #5 can run concurrently after #3

Queue these tasks? [y/n/edit]
```

---

## Interactive Editing

### Step 9: Edit Mode

If user types `edit`:

```
Edit commands:
  priority <n> <high|medium|low>  - Change task priority
  merge <n> <m>                   - Combine two tasks into one
  split <n>                       - Split task (will prompt for details)
  deps <n> add <m>                - Add dependency (#n depends on #m)
  deps <n> remove <m>             - Remove dependency
  rename <n> <new title>          - Rename task
  remove <n>                      - Remove task from queue
  files <n> add <path>            - Add file to task
  files <n> remove <path>         - Remove file from task
  reorder                         - Re-detect all dependencies
  show                            - Show current graph
  done                            - Finish editing

edit> 
```

**Command Examples:**

```
edit> priority 6 medium
Updated: #6 "Write documentation" priority: low -> medium

edit> merge 4 5
Merging #4 "Build form component" + #5 "Build list component"
New title [Build UI components]: 
Merged into #4 "Build UI components"
  Files: src/components/Form.tsx, src/components/List.tsx
  Dependencies: #3

edit> deps 4 add 2
Added: #4 now depends on #2

edit> split 3
Current task #3: "Implement service layer"
  Files: src/services/auth.ts, src/services/user.ts

Split into how many tasks? 2
Task 3a title: Implement auth service
Task 3a files: src/services/auth.ts
Task 3b title: Implement user service  
Task 3b files: src/services/user.ts
Dependencies: 3b depends on 3a? [y/n]: n

Split complete. Tasks renumbered.

edit> show
[displays updated graph]

edit> done
```

**Circular Dependency Detection:**

```
edit> deps 2 add 5

Error: This would create a circular dependency:
  #2 -> #5 -> #4 -> #3 -> #2

Cannot add this dependency.

edit> 
```

---

## Queue Creation Phase

### Step 10: Create Tasks in Brain

**CRITICAL: Draft-then-promote pattern!**

To prevent race conditions where do-work picks up tasks before the full dependency graph is established:
1. Create ALL tasks as `status: "draft"` first
2. After the LAST task is created, batch-promote ALL to `status: "pending"`

```
Queueing tasks as drafts...
```

**Algorithm:**

1. Topological sort tasks by dependency depth (most dependent first)
2. Create each task in sorted order with `status: "draft"`
3. Use `depends_on` field with task IDs/titles of dependencies
4. After ALL tasks created, batch-promote to `status: "pending"`

**Creation Order Example:**

For graph: 1 -> 2 -> 3 -> 4, 5 (4 and 5 depend on 3)
           6 depends on 4 and 5

Create order (all as draft): 6, 5, 4, 3, 2, 1
- Task 6 created first as draft (will depend on 4, 5)
- Task 5 created as draft (will depend on 3)
- Task 4 created as draft (will depend on 3)
- Task 3 created as draft (will depend on 2)
- Task 2 created as draft (will depend on 1)
- Task 1 created LAST as draft (no dependencies)

Then promote all to pending in one batch.

**Task Creation (NO parent task - only executable tasks):**

```
# Step 1: Create all tasks as DRAFT
task_paths = []

for each task in sorted_tasks:
  path = brain_save(
    type: "task",
    title: "$TASK_TITLE",
    status: "draft",  # DRAFT first - not visible to do-work yet
    priority: "$PRIORITY",
    project: PROJECT,  # omit if null
    depends_on: ["$DEP_TASK_TITLE_1", "$DEP_TASK_TITLE_2"],  # USE THIS FIELD - omit if empty
    tags: ["do-work", slugify("$PLAN_TITLE")],
    user_original_request: "$PLAN_TITLE: $TASK_DESCRIPTION",
    content: @task_content
  )
  task_paths.append(path)

# Step 2: BATCH PROMOTE - All tasks created, now make them visible to do-work
for each path in task_paths:
  brain_update(path: path, status: "pending")
```

**Why draft-then-promote?**
- Prevents race condition where do-work executes tasks before all dependencies are filed
- Ensures the complete dependency graph exists before any task becomes visible
- Atomic: either all tasks become pending together, or none do (on failure)

**IMPORTANT:** 
- Do NOT create a parent/container task
- Do NOT put dependencies in the content - use the `depends_on` field
- The `depends_on` field accepts task titles or IDs
- Each task should be independently executable
- ALL tasks start as draft, ALL promoted to pending at the end

### Step 11: Batch Promote to Pending

After all tasks are created as drafts, promote them all to pending:

```
All N tasks created as drafts. Promoting to pending...
```

```
# Promote all tasks to pending (makes them visible to do-work)
for each path in task_paths:
  brain_update(path: path, status: "pending")
```

```
All N tasks promoted to pending.
```

### Step 12: Confirmation

```
Queued: "$PLAN_TITLE" (N tasks)

  1. $TASK_1 [pending] (ready)
  2. $TASK_2 [pending] (blocked -> #1)
  3. $TASK_3 [pending] (blocked -> #2)
  4. $TASK_4 [pending] (blocked -> #3)
  5. $TASK_5 [pending] (blocked -> #3)
  6. $TASK_6 [pending] (blocked -> #4, #5)

All N tasks promoted to pending (dependency graph complete)

Execution order:
  1. #1 runs immediately
  2. #2 runs after #1 completes
  3. #3 runs after #2 completes
  4. #4 and #5 run in parallel after #3
  5. #6 runs after both #4 and #5 complete

Next steps:
  - Run `do-work start $PROJECT` to begin autonomous execution
  - Run `/do status` to monitor progress

Brain paths:
  | Task | Path |
  |------|------|
  | #1 $TASK_1 | $PATH_1 |
  | #2 $TASK_2 | $PATH_2 |
  ...
```

---

## Content Templates

### Task Content

**IMPORTANT:** Dependencies are tracked via the `depends_on` field in brain_save, NOT in the content.

```markdown
## Task
$TASK_DESCRIPTION_FROM_PLAN

## Context
Plan: $PLAN_TITLE
Task $N of $TOTAL

## Files Involved
- `$FILE_PATH` - $WHAT_CHANGES

## Acceptance Criteria
- [ ] $CRITERIA_1
- [ ] $CRITERIA_2

## Validation Notes
$RELEVANT_NOTES_FROM_VALIDATION

## Implementation Notes
_To be filled by do-work during execution_
```

**Do NOT include a "Dependencies" section in the content.** The `depends_on` field handles this.

---

## Edge Cases

### Plan from brain is stale

```
This plan was last modified 14 days ago.

The codebase may have changed since then. Options:
  1. Proceed anyway (validation will catch issues)
  2. Cancel and update the plan first

Choice [1/2]: 
```

### Circular dependencies detected

```
Circular dependency detected in task graph:

  #3 "Implement service" 
    -> depends on #5 "Add API routes"
    -> depends on #3 "Implement service"

This must be resolved before queuing.

Entering edit mode to fix...

edit> deps 5 remove 3
Removed: #5 no longer depends on #3
Circular dependency resolved.

edit> done
```

### Too many tasks

```
This plan would create 25+ tasks.

Large task counts can be harder to manage and track.

Options:
  1. Proceed with 25 tasks
  2. Re-group into fewer tasks (recommended: ~10-15)
  3. Cancel and split into multiple plans

Choice [1/2/3]: 
```

If choice 2:
```
Suggested groupings to reduce to ~12 tasks:
  - Merge schema + types tasks
  - Merge related UI components
  - Combine test tasks

Accept suggested groupings? [y/n/edit]
```

### Validation timeout

```
Validation is taking longer than expected...

The codebase may be large or the plan complex.

Options:
  1. Continue waiting
  2. Skip validation and proceed (not recommended)
  3. Cancel

Choice [1/2/3]: 
```

### Brain save fails during draft creation

```
Error saving task #3 to brain: <error>

Rolling back... (deleting draft tasks #1, #2)

No tasks were queued. Please try again or check brain connection.
```

### Brain update fails during promotion

```
Error promoting task #5 to pending: <error>

Partial promotion occurred. Current state:
  - Tasks #1-#4: pending (promoted)
  - Tasks #5-#9: draft (not promoted)

Options:
  1. Retry promotion for remaining tasks
  2. Delete all tasks and start over
  3. Leave as-is (manually fix later)

Choice [1/2/3]:
```

---

## Example Session

```
$ /plan-to-tasks myproject

I'll convert an implementation plan into executable tasks.

Where is the plan?
  1. Load from brain (provide path or title)
  2. Use plan from current conversation
  3. Paste a plan now

> 1

Brain plan path or title: OAuth Implementation

Loading: projects/myproject/plan/oauth-implementation.md

Found plan: "OAuth Implementation"
  Created: 2 days ago
  Phases: 4
  Steps: 9

Validating plan against codebase...

Validation passed.
  - All file paths verified
  - Follows existing auth patterns in `src/lib/auth.ts`
  - Parallelization opportunity: Google + GitHub providers

How granular should tasks be?
  A. One task per phase (4 tasks)
  B. One task per step (9 tasks) [recommended]
  C. Custom groupings

> B

Task Graph: "OAuth Implementation"
Project: myproject

  1. [high] Add OAuth config schema
     Files: src/config/oauth.ts
     depends_on: []
     
  2. [high] Create OAuth provider interface
     Files: src/lib/oauth/provider.ts
     depends_on: ["Add OAuth config schema"]
     
  3. [high] Implement Google OAuth provider
     Files: src/lib/oauth/google.ts
     depends_on: ["Create OAuth provider interface"]
     
  4. [high] Implement GitHub OAuth provider
     Files: src/lib/oauth/github.ts
     depends_on: ["Create OAuth provider interface"]
     
  5. [medium] Add OAuth callback routes
     Files: src/routes/oauth.ts
     depends_on: ["Create OAuth provider interface"]
     
  6. [medium] Create OAuth button components
     Files: src/components/OAuthButtons.tsx
     depends_on: ["Implement Google OAuth provider", "Implement GitHub OAuth provider"]
     
  7. [medium] Integrate OAuth into login page
     Files: src/pages/Login.tsx
     depends_on: ["Add OAuth callback routes", "Create OAuth button components"]
     
  8. [low] Add OAuth to user settings
     Files: src/pages/Settings.tsx
     depends_on: ["Integrate OAuth into login page"]
     
  9. [medium] Write OAuth documentation
     Files: docs/oauth.md
     depends_on: ["Integrate OAuth into login page"]

Total: 9 tasks
Parallelizable: #3, #4, #5 can run concurrently after #2

Queue these tasks? [y/n/edit]

> edit

edit> priority 9 medium
Updated: #9 "Write OAuth documentation" priority: low -> medium

edit> show

[updated graph displayed]

edit> done

Queue these tasks? [y/n]

> y

Queueing tasks as drafts...

Creating 9 tasks as drafts...
  ✓ #9 Write OAuth documentation [draft]
  ✓ #8 Add OAuth to user settings [draft]
  ✓ #7 Integrate OAuth into login page [draft]
  ✓ #6 Create OAuth button components [draft]
  ✓ #5 Add OAuth callback routes [draft]
  ✓ #4 Implement GitHub OAuth provider [draft]
  ✓ #3 Implement Google OAuth provider [draft]
  ✓ #2 Create OAuth provider interface [draft]
  ✓ #1 Add OAuth config schema [draft]

All 9 tasks created as drafts. Promoting to pending...
  ✓ All 9 tasks promoted to pending

Queued: "OAuth Implementation" (9 tasks)

  1. Add OAuth config schema [pending] (ready)
  2. Create OAuth provider interface [pending] (blocked -> #1)
  3. Implement Google OAuth provider [pending] (blocked -> #2)
  4. Implement GitHub OAuth provider [pending] (blocked -> #2)
  5. Add OAuth callback routes [pending] (blocked -> #2)
  6. Create OAuth button components [pending] (blocked -> #3, #4)
  7. Integrate OAuth into login page [pending] (blocked -> #5, #6)
  8. Add OAuth to user settings [pending] (blocked -> #7)
  9. Write OAuth documentation [pending] (blocked -> #7)

All 9 tasks promoted to pending (dependency graph complete)

Execution order:
  1. #1 runs immediately
  2. #2 runs after #1
  3. #3, #4, #5 run in parallel after #2
  4. #6 runs after #3 and #4 both complete
  5. #7 runs after #5 and #6 both complete
  6. #8 and #9 run in parallel after #7

Next steps:
  - Run `do-work start myproject` to begin autonomous execution
  - Run `/do status` to monitor progress

Brain paths:
  | Task | Path |
  |------|------|
  | #1 Add OAuth config schema | projects/myproject/task/abc123.md |
  | #2 Create OAuth provider interface | projects/myproject/task/def456.md |
  | ... | ... |
```

---

## Integration Points

| Component | How It Integrates |
|-----------|-------------------|
| `brain_recall` | Load existing plans from brain |
| `brain_save` | Create task entries with `depends_on` field |
| `do-work` skill | Processes the queued tasks |
| `do-work` script | Handles dependency resolution at runtime |
| `/do status` | Monitor queue progress |
| `explore` agent | Validates plan feasibility |

---

**Arguments:** $ARGUMENTS
