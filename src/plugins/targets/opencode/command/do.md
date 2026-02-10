---
description: Issue ticketing - file issues/features, dispatch general agent for root cause analysis, queue to brain
---

# /do - Issue Ticketing

File issues and feature requests. Dispatches a general subagent for root cause analysis, then queues findings to brain for `do-work` to process.

**Project:** `$1` (optional - defaults to brain's auto-generated project ID based on working directory)

<critical_rules>
  <rule id="no_work_inline">NEVER investigate or implement inline - ALWAYS dispatch to general subagent</rule>
  <rule id="orchestrator_only">This command is a DISPATCHER - it routes to subagents and queues results</rule>
  <rule id="simple_tasks_pending">Simple standalone tasks go directly to status: "pending"</rule>
  <rule id="dependent_tasks_draft">For task chains with dependencies: create ALL as "draft" first, then batch-promote to "pending" after the LAST task is created</rule>
  <rule id="continuous_intake">Keep accepting new issues until user says stop</rule>
  <rule id="clarify_then_continue">If clarification needed, ask user, then dispatch NEW subagent with full context</rule>
  <rule id="clarify_complex_tasks">For complex multi-step features, ALWAYS ask clarifying questions before splitting into tasks</rule>
  <rule id="include_original_request">ALWAYS include exact user input in "## Original Request" section - used for validation</rule>
  <rule id="split_large_tasks">For complex tasks with multiple discrete steps, create multiple tasks with depends_on relationships and ALWAYS use feature_id to group them</rule>
  <rule id="use_project_arg">Use $1 as project name if provided, otherwise omit project parameter to use brain's default</rule>
  <rule id="reset_status_on_update">When updating existing task with new requirements, ALWAYS set status: "pending" to re-queue</rule>
  <rule id="keep_tasks_small">Tasks should be SMALL and focused - completable in 15-30 minutes. Split larger work into multiple tasks.</rule>
  <rule id="no_parent_tasks">NEVER create "parent" tasks that sit at in_progress as containers. Use feature_id to group related tasks instead.</rule>
  <rule id="use_feature_id">MANDATORY: Any feature requiring 2+ tasks MUST use feature_id to group them (e.g., "auth-system", "dark-mode"). ALL tasks in a feature share the same feature_id. Single standalone tasks don't need feature_id.</rule>
  <rule id="checkout_task_optional">Only create a "final checkout" task if user EXPLICITLY requests verification OR the feature requires integration testing. The checkout task depends on ALL other tasks in the feature.</rule>
  <rule id="use_feature_depends_on">Use feature_depends_on when entire features must complete before another feature starts.</rule>
</critical_rules>

## What This Command Does

1. **Parses project argument** - Uses `$1` if provided, otherwise uses brain's default project ID
2. **Checks environment** - Verifies project is ready (runs health checks if defined)
3. **Accepts issue/feature descriptions** from user
4. **Dispatches general subagent** for root cause analysis
5. **Auto-queues** findings to brain:
   - **Simple tasks:** Created directly as `pending` (immediately visible to do-work)
   - **Dependent task chains:** Created as `draft`, then batch-promoted to `pending` after last task
6. **Repeats** until user says `stop`

**Why draft-then-promote for dependent tasks?**
- Prevents race condition where do-work executes tasks before all dependencies are filed
- Ensures complex multi-task features have complete dependency graph before execution
- Simple standalone tasks still go to pending immediately (no delay)

**Feature ID Decision Tree:**
```
Is this a single standalone task?
├── YES → No feature_id needed, status: "pending" immediately
└── NO (2+ tasks required)
    └── MUST use feature_id (MANDATORY)
        ├── Generate slug from feature name: "Add dark mode" → "dark-mode"
        ├── ALL tasks share the same feature_id
        ├── Create all as "draft", then batch-promote to "pending"
        └── Benefits: TUI grouping, pause/resume, focus mode ('x' key)
```

**This command does NOT:**
- Investigate issues itself (dispatches subagent)
- Implement fixes (do-work handles that)
- Read code or logs directly (subagent does)

## Usage

```bash
/do                    # Start ticketing mode (uses brain's default project)
/do myproject          # Start ticketing mode, queue all tasks to "myproject"
/do status             # Show queue status
/do stop               # Exit ticketing mode

# In ticketing mode:
> scanner not working   # -> Simple task, queued as PENDING immediately
> add dark mode         # -> Complex feature, creates task chain, all promoted to PENDING when complete
> status                # -> Shows queue for current project
> stop                  # -> Exits
```

**Task Lifecycle:**
1. `/do` creates tasks:
   - Simple tasks → `pending` immediately
   - Dependent task chains → `draft` during creation, then batch-promote to `pending`
2. `do-work` processes **pending** tasks → **in_progress** → **completed**
3. `/validate` verifies **completed** tasks → **validated**

## Startup Sequence

### Step 0: Resolve Project

```
# $1 is the first argument passed to /do
# If provided, use it as the project name
# If not provided, omit project parameter - brain will use its default (based on working directory)

if $1 is provided:
  PROJECT = "$1"
  PROJECT_DISPLAY = "project: $1"
else:
  PROJECT = null  # omit from brain_save calls
  PROJECT_DISPLAY = "project: (default)"
```

### Step 1: Check Environment Health

Run basic environment checks to ensure the project is ready for development:

```bash
echo "=== Checking Environment ==="

# Check if we're in a git repo
if git rev-parse --git-dir > /dev/null 2>&1; then
  echo "Git repo: $(basename $(git rev-parse --show-toplevel)) ✓"
else
  echo "WARNING: Not in a git repository"
fi

# Check for common project indicators
if [ -f "package.json" ]; then
  echo "Node project: package.json ✓"
  # Check if node_modules exists
  if [ ! -d "node_modules" ]; then
    echo "WARNING: node_modules missing - run npm install"
  fi
fi

if [ -f "pyproject.toml" ] || [ -f "setup.py" ] || [ -f "requirements.txt" ]; then
  echo "Python project ✓"
fi

if [ -f "go.mod" ]; then
  echo "Go project ✓"
fi

if [ -f "Cargo.toml" ]; then
  echo "Rust project ✓"
fi

# Check for Justfile or Makefile
if [ -f "Justfile" ] || [ -f "justfile" ]; then
  echo "Task runner: just ✓"
elif [ -f "Makefile" ]; then
  echo "Task runner: make ✓"
fi

# Check for docker-compose
if [ -f "docker-compose.yml" ] || [ -f "docker-compose.yaml" ] || [ -f "compose.yml" ]; then
  echo "Docker Compose ✓"
fi

echo "Environment check complete."
```

### Step 2: Enter Ticketing Mode

```
Issue Ticketing Ready

  Directory: $(pwd)
  Project:   $1 (or "(default)" if not provided)

Tip: Run `do-work start` in another terminal to process the queue.

Describe issues or features to file. I'll dispatch an agent to investigate
and queue the findings.

What's the issue?
> 
```

---

## Ticketing Loop

### Input Routing

| Input | Action |
|-------|--------|
| `stop`, `quit`, `exit` | Promote all drafts to pending, then exit ticketing mode |
| `status` | Show queue via `brain_list(type: "task", project: PROJECT)` (omit project if using default) |
| Anything else | Dispatch general subagent |

### Filing an Issue

When user describes an issue or feature:

#### 1. Acknowledge

```
Filing: "scanner not working"
Dispatching investigation...
```

#### 2. Dispatch General Subagent

```
Task(
  subagent_type: "general",
  description: "RCA: <short_desc>",
  prompt: @rca_prompt
)
```

#### 3. Wait for Findings

Subagent returns structured findings with:
- Summary
- Evidence found
- Root cause / approach
- Files involved
- Complexity estimate

#### 4. Handle Clarification

If subagent returns `CLARIFICATION_NEEDED:`:
- Present question to user
- Get answer
- Dispatch NEW general subagent with full context (original + clarification)

#### 4b. Handle Updates to Existing Tasks

If the subagent identifies that the issue relates to an **existing task** in the brain (e.g., user feedback changes requirements for a task already filed):

```
brain_update(
  path: "<existing_task_path>",
  status: "pending",  # CRITICAL: Reset to pending so it gets re-queued!
  append: @updated_requirements
)
```

**Updated requirements format:**
```markdown
## Updated Requirements (from user feedback)

User clarification: <what the user said>

Updated approach:
1. <revised step 1>
2. <revised step 2>
...
```

**Why reset to pending?**
- The task may have been `completed` or `in_progress`
- New requirements mean the previous work may be invalid
- Resetting to `pending` ensures `do-work` picks it up again
- The appended content documents why requirements changed

**Example:**
```
# User says: "actually remove the scroll indicators entirely"
# Agent finds existing task sdw6d94q about scroll indicator rendering

brain_update(
  path: "projects/brain/task/sdw6d94q.md",
  status: "pending",
  append: """
## Updated Requirements (from user feedback)

User clarification: Remove the "more above" and "more below" scroll indicators entirely from the LogViewer panel.

Updated approach:
1. Remove the scroll indicator rendering code entirely
2. Keep j/k scrolling functionality but without visual indicators
3. This simplifies the fix - no need to account for indicator lines
"""
)
```

#### 5. Auto-Queue to Brain

**For simple/medium tasks (single discrete unit - no dependencies):**
```
brain_save(
  type: "task",
  title: "<from investigation>",
  status: "pending",  # Simple tasks go directly to pending
  priority: "<simple=low, medium=medium, complex=high>",
  project: PROJECT,  # Include only if $1 was provided, otherwise omit
  tags: ["do-work"],
  user_original_request: "<exact user input>",
  content: @task_content
)
```

**For complex tasks with multiple discrete steps (dependent task chain):**

When the agent identifies that a task has multiple independent or sequential parts, **ALWAYS** create tasks grouped by `feature_id`. This is **MANDATORY** for any feature requiring 2+ tasks. **Do NOT create a "parent" task that sits at in_progress as a container.**

**Why feature_id is required for multi-task features:**
- Groups related tasks in the TUI dashboard for easy tracking
- Enables pause/resume at the feature level
- Allows "focus mode" (press 'x' on feature to run only that feature)
- Provides clear visual organization of work
- Makes it easy to see feature progress at a glance

**Task structure using feature_id:**
```
<project> (PROJECT or brain's default)
├── Simple bug fix (single task - pending immediately)
└── Feature: "dark-mode" (grouped by feature_id)
    ├── Create theme context (task 1 - no dependencies)
    ├── Update components (task 2, depends_on: task 1)
    └── Add toggle to header (task 3, depends_on: task 2)
```

**CRITICAL: Use draft-then-promote for dependent task chains!**

To prevent race conditions where do-work picks up tasks before the full dependency graph is established:
1. Create ALL tasks in the chain as `status: "draft"` with shared `feature_id`
2. After the LAST task is created, batch-promote ALL to `status: "pending"`

```
# Generate feature_id from the feature name (slug format)
# "Add dark mode" -> "dark-mode"
# "TUI Task Detail Improvements" -> "tui-task-detail-improvements"
FEATURE_ID = slugify("<feature name>")

# Step 1: Create task 1 as draft (small, focused - ~15-20 min)
task1_path = brain_save(
  type: "task",
  title: "Create theme context",
  status: "draft",  # Draft until chain complete
  priority: "high",
  project: PROJECT,  # Include only if $1 was provided, otherwise omit
  feature_id: FEATURE_ID,  # Groups all related tasks
  feature_priority: "high",  # Priority relative to other features
  tags: ["do-work", FEATURE_ID],
  user_original_request: "<exact user input>",
  content: @task1_content
)

# Step 2: Create task 2 as draft (depends on task 1, ~20-30 min)
task2_path = brain_save(
  type: "task",
  title: "Update components to use theme",
  status: "draft",  # Draft until chain complete
  priority: "high",
  project: PROJECT,
  feature_id: FEATURE_ID,
  depends_on: [task1_path],
  tags: ["do-work", FEATURE_ID],
  user_original_request: "<exact user input>",
  content: @task2_content
)

# Step 3: Create task 3 as draft (depends on task 2, ~15 min)
task3_path = brain_save(
  type: "task",
  title: "Add toggle to header",
  status: "draft",  # Draft until chain complete
  priority: "high",
  project: PROJECT,
  feature_id: FEATURE_ID,
  depends_on: [task2_path],
  tags: ["do-work", FEATURE_ID],
  user_original_request: "<exact user input>",
  content: @task3_content
)

# Step 4 (OPTIONAL): Create checkout task ONLY if user explicitly requested verification
# OR if the feature requires integration testing
IF user_requested_verification OR feature_needs_integration_test:
  checkout_path = brain_save(
    type: "task",
    title: "Final checkout: Dark mode integration",
    status: "draft",
    priority: "high",
    project: PROJECT,
    feature_id: FEATURE_ID,
    depends_on: [task1_path, task2_path, task3_path],  # Depends on ALL tasks
    tags: ["do-work", FEATURE_ID, "checkout"],
    user_original_request: "<exact user input>",
    content: @checkout_content  # Verification checklist
  )

# Step 5: BATCH PROMOTE - All tasks created, now make them visible to do-work
brain_update(path: task1_path, status: "pending")
brain_update(path: task2_path, status: "pending")
brain_update(path: task3_path, status: "pending")
IF checkout_path:
  brain_update(path: checkout_path, status: "pending")
```

**Result in do-work dashboard:**
```
<project>
└── Feature: dark-mode
    ├── Create theme context (pending - ready to run)
    ├── Update components (pending - waiting on #1)
    └── Add toggle to header (pending - waiting on #2)
```

**With optional checkout task:**
```
<project>
└── Feature: dark-mode
    ├── Create theme context (pending - ready to run)
    ├── Update components (pending - waiting on #1)
    ├── Add toggle to header (pending - waiting on #2)
    └── Final checkout: Dark mode integration (pending - waiting on #1, #2, #3)
```

---

## Task Sizing and Feature Grouping

### Keep Tasks Small

**Target: 15-30 minutes of focused work per task.**

Small tasks are better because:
- Easier to verify and validate
- Faster feedback loops
- Less context switching for the worker agent
- Clearer success criteria
- Easier to re-queue if something goes wrong

**Signs a task is too large:**
- Touches more than 3-5 files
- Has multiple distinct "phases" (schema, API, UI)
- Description uses words like "and then" or "also"
- Estimated time > 30 minutes

### Using `depends_on` for Task Chains

Use `depends_on` when tasks must execute in sequence:

```
Task A: Create database schema
Task B: Build API endpoints (depends_on: [Task A])
Task C: Create UI components (depends_on: [Task B])
```

The brain's task queue will only mark Task B as "ready" after Task A completes.

### Using `feature_id` for Logical Grouping (MANDATORY for 2+ tasks)

**RULE: Any feature requiring 2 or more tasks MUST use `feature_id`.**

Use `feature_id` to group related tasks that belong to the same feature:

```
brain_save(
  type: "task",
  title: "Create theme context",
  feature_id: "dark-mode",
  feature_priority: "high",
  ...
)

brain_save(
  type: "task",
  title: "Update button styles for theme",
  feature_id: "dark-mode",
  depends_on: ["<theme-context-task-id>"],
  ...
)
```

**Benefits of feature_id:**
- Tasks grouped together in TUI dashboard under collapsible feature headers
- Enables feature-level actions: pause, resume, focus mode ('x' to run feature to completion)
- Can query all tasks for a feature: `brain_list(type: "task", tags: ["dark-mode"])`
- Provides logical organization beyond just dependencies
- Shows feature completion progress (e.g., "2/5 tasks complete")

**When to use feature_id:**
- ✅ Feature requires 2+ tasks → MUST use feature_id
- ✅ Related bug fixes that should be grouped → use feature_id
- ❌ Single standalone task → no feature_id needed

### Using `feature_depends_on` for Feature-Level Dependencies

When entire features must complete before another feature starts:

```
Feature: "auth-system" (login, registration, password reset)
Feature: "user-dashboard" (depends on auth-system being complete)

brain_save(
  type: "task",
  title: "Build dashboard layout",
  feature_id: "user-dashboard",
  feature_depends_on: ["auth-system"],
  ...
)
```

This ensures ALL tasks in "auth-system" complete before ANY task in "user-dashboard" becomes ready.

---

**When to split tasks:**
- Task involves changes to multiple layers (backend + frontend, API + UI)
- Task has clear sequential phases (schema -> API -> UI)
- Estimated implementation time > 30 minutes
- Agent identifies 3+ distinct file groups with different concerns

**When NOT to split:**
- Simple bug fixes
- Single-file changes
- Tightly coupled changes that must deploy together

**IMPORTANT: Clarify before splitting!**

For complex tasks that will be split, the subagent should return `CLARIFICATION_NEEDED:` to gather requirements BEFORE proposing the task split. This ensures we understand the full scope and don't create incorrect task chains.

Examples of clarifying questions for complex features:
- "Should this persist across sessions or be session-only?"
- "Do you want a system preference option in addition to manual toggle?"
- "Should existing settings be preserved or replaced?"

#### 6. Confirm & Continue

**Single task (no dependencies):**
```
✅ Queued: "Fix scanner execution error" 🟡 medium
  Files: src/scanner.ts, src/components/Scanner.tsx
  Status: pending (visible to do-work)

What else?
> 
```

**Multi-task with dependencies (batch promoted after last task):**
```
✅ Queued: Feature "dark-mode" (3 tasks)
  ├── 1. Create theme context 🔴 high [pending]
  │      Files: src/contexts/ThemeContext.tsx
  ├── 2. Update components 🔴 high (after #1) [pending]
  │      Files: src/components/*.tsx
  └── 3. Add toggle to header 🔴 high (after #2) [pending]
         Files: src/layouts/AppLayout.tsx
  
  All 3 tasks promoted to pending (dependency graph complete)

What else?
> 
```

**Multi-task with checkout (when user requested verification):**
```
✅ Queued: Feature "dark-mode" (3 tasks + checkout)
  ├── 1. Create theme context 🔴 high [pending]
  │      Files: src/contexts/ThemeContext.tsx
  ├── 2. Update components 🔴 high (after #1) [pending]
  │      Files: src/components/*.tsx
  ├── 3. Add toggle to header 🔴 high (after #2) [pending]
  │      Files: src/layouts/AppLayout.tsx
  └── ✓ Final checkout: Dark mode integration (after all) [pending]
  
  All 4 tasks promoted to pending (dependency graph complete)

What else?
> 
```

---

## RCA Subagent Prompt

<rca_prompt>
ROOT CAUSE ANALYSIS

**Issue/Feature:** $USER_INPUT

**Working Directory:** $PWD

**Your Task:**
Investigate this issue to gather context for later implementation. Do NOT implement anything.

**If you need clarification:** Return ONLY:
```
CLARIFICATION_NEEDED: <your question>
```
The orchestrator will get the answer and dispatch a new investigation.

**IMPORTANT:** For complex features that will require multiple tasks, ALWAYS ask clarifying questions first! Don't assume scope - gather requirements before proposing a task split. Ask about:
- User preferences (persistence, defaults, behavior)
- Edge cases and error handling expectations
- Integration points with existing features
- Priority of sub-features if the request is broad
- Whether user wants a final verification/checkout step

**TASK SIZING:** Keep tasks SMALL - each task should be completable in 15-30 minutes. If a task would take longer, split it into smaller pieces. Use `depends_on` for sequential tasks and `feature_id` to group related tasks together.

**FEATURE GROUPING (MANDATORY for 2+ tasks):** When splitting into multiple tasks:
- **ALWAYS** generate a `feature_id` slug from the feature name (e.g., "Add dark mode" -> "dark-mode")
- ALL tasks in the feature MUST share the same `feature_id` - this is NOT optional
- Do NOT create a "parent" task - feature_id handles grouping in the TUI
- Only suggest a "checkout" task if user explicitly wants verification
- Feature_id enables: TUI grouping, pause/resume, focus mode ('x' key)

---

## Investigation Steps

### For Bugs:

1. **Search for error patterns in codebase:**
   - Use grep to find related error messages
   - Check log files if they exist

2. **Trace the code path:**
   - Find entry points related to the issue
   - Follow the execution flow
   - Identify where the bug likely occurs

3. **Check recent changes:**
   - `git log --oneline -20` for recent commits
   - `git diff HEAD~5` if issue is recent

4. **Look for similar patterns:**
   - Search for how similar functionality works elsewhere
   - Check tests for expected behavior

### For Features:

1. **Search codebase for similar patterns:**
   - Find existing implementations to follow
   - Identify conventions used in the project

2. **Identify files that need changes**

3. **Check for existing conventions to follow**

4. **Map dependencies:**
   - What existing code will this interact with?
   - Are there shared utilities to use?

---

## Return Format

**For simple/medium tasks:**
```markdown
## Issue: <title>

### Summary
<1-2 sentences>

### Evidence
- <what you found>
- <errors/logs>

### Root Cause / Approach
<why this happens OR how to implement>

### Files Involved
- `path/to/file` - <what changes>

### Complexity
<simple|medium|complex> - <why>
```

**For complex tasks that should be split (2+ tasks REQUIRE feature_id):**
```markdown
## Issue: <title>

### Summary
<1-2 sentences>

### Evidence
- <what you found>
- <errors/logs>

### Root Cause / Approach
<why this happens OR how to implement>

### Recommended Task Split
This task should be split into multiple dependent tasks.

**Feature ID:** `<feature-slug>` (REQUIRED - e.g., "dark-mode", "auth-system", "worktree-bugfix")
**Feature Priority:** <high|medium|low>
**Needs Checkout Task:** <yes|no> (only "yes" if user explicitly requested verification)

NOTE: feature_id is MANDATORY when creating 2+ tasks. It groups tasks in the TUI and enables feature-level controls.

#### Task 1: <title>
- **Files:** `path/to/file1`
- **Description:** <what this task does>
- **Estimated Time:** ~15-20 min
- **Dependencies:** none

#### Task 2: <title>
- **Files:** `path/to/file2`
- **Description:** <what this task does>
- **Estimated Time:** ~20-30 min
- **Dependencies:** Task 1

#### Task 3: <title>
- **Files:** `path/to/file3`
- **Description:** <what this task does>
- **Estimated Time:** ~15 min
- **Dependencies:** Task 2

#### (OPTIONAL) Checkout Task: Final verification - <feature name>
- **Description:** Verify all components work together
- **Verification Checklist:**
  - [ ] <check 1>
  - [ ] <check 2>
- **Dependencies:** Task 1, Task 2, Task 3 (all tasks)
- **Note:** Only include if user explicitly requested verification

### Task Sizing Notes
- Each task should be completable in 15-30 minutes
- If a task would take longer, split it further
- **MANDATORY:** All tasks in a multi-task feature share the same feature_id
- NO parent task needed - feature_id handles grouping in TUI dashboard
- feature_id enables: visual grouping, pause/resume, focus mode

### Complexity
complex - <why it needs splitting>
```
</rca_prompt>

---

## Task Content Structure

When saving to brain, the content MUST follow this structure:

```markdown
## Original Request
<exact user input - this is used by /validate>

## Root Cause Analysis

### Summary
<from agent>

### Evidence
<from agent>

### Root Cause / Approach
<from agent>

## Files Involved
- `path/to/file` - <what changes>

## Complexity
<simple|medium|complex>
```

---

## Subcommands

### `/do status`

```
brain_list(type: "task", project: PROJECT, limit: 20)  # Omit project if using default
```

Display:
```
Task Queue (PROJECT_DISPLAY):

  ⏳ Standalone Pending (3) - ready for do-work:
    🔴 high   - Fix authentication crash
    🟡 medium - Scanner execution error
    🟢 low    - Add clear button

  📦 Feature: dark-mode (3 tasks)
    ├── ✅ Create theme context (completed)
    ├── 🔵 Update components (running)
    └── ⏳ Add toggle to header (waiting on #2)

  📦 Feature: auth-system (4 tasks + checkout)
    ├── ⏳ Create auth context (ready)
    ├── ⏳ Build login/register forms (waiting)
    ├── ⏳ Add password reset flow (waiting)
    ├── ⏳ Protect routes (waiting)
    └── ✓ Final checkout (waiting on all)

  ✅ Completed (awaiting validation) (2):
    🟢 low    - Fix typo in header
    🟢 low    - Update footer links

Tip: Run `do-work start` to process pending tasks.
     Run `/validate` to verify completed tasks.
```

### `/do stop`

```
Exiting ticketing mode.

Queue summary (PROJECT_DISPLAY):
  Pending: 3
  In Progress: 1
  Completed: 2

Next steps:
  - Run `do-work start` to process pending tasks
  - Run `/validate` to verify completed tasks
```

---

## Example Session

```
$ /do myproject

Issue Ticketing Ready

  Directory: /Users/dev/myproject
  Project:   myproject

Tip: Run `do-work start` in another terminal to process the queue.

Describe issues or features to file. I'll dispatch an agent to investigate
and queue the findings.

What's the issue?
> scanner shows error when I click run

Filing: "scanner shows error when I click run"
Dispatching investigation...

[Agent investigates codebase, logs, error patterns]

✅ Queued: "Fix scanner execution error" 🟡 medium
  Files: src/scanner.ts, src/components/Scanner.tsx
  Status: pending (visible to do-work)

What else?
> add dark mode to the app

Filing: "add dark mode to the app"
Dispatching investigation...

[Agent investigates codebase, identifies this is a complex multi-step feature]

Clarification needed:
  This is a complex feature. A few questions to scope it correctly:
  1. Should the theme preference persist across sessions (localStorage)?
  2. Do you want a "system" option that follows OS preference?
  3. Where should the toggle live - header, settings page, or both?
  4. Do you want a final verification step after all tasks complete?

> yes persist it, yes follow system, just put it in the header for now, no verification needed

Filing: "add dark mode - persist, system option, header toggle"
Dispatching investigation...

[Agent now has full context to properly split the task]
[Creates all 3 tasks as draft with feature_id: "dark-mode", then batch-promotes to pending]

✅ Queued: Feature "dark-mode" (3 tasks)
  ├── 1. Create theme context with system detection 🔴 high [pending]
  │      Files: src/contexts/ThemeContext.tsx, src/index.css
  ├── 2. Update components to use theme tokens 🔴 high (after #1) [pending]
  │      Files: src/components/*.tsx
  └── 3. Add theme toggle to header 🔴 high (after #2) [pending]
         Files: src/layouts/AppLayout.tsx

  All 3 tasks promoted to pending (dependency graph complete)

What else?
> fix the header

Filing: "fix the header"
Dispatching investigation...

Clarification needed:
  What's wrong with the header? I see several elements:
  - Logo/title
  - Navigation tabs  
  - User menu

> the navigation tabs don't highlight the current page

Filing: "navigation tabs don't highlight current page"
Dispatching investigation...

✅ Queued: "Fix navigation tab highlighting" 🟢 low
  Files: src/layouts/AppLayout.tsx
  Status: pending (visible to do-work)

What else?
> add user authentication and make sure it all works together

Filing: "add user authentication"
Dispatching investigation...

[Agent identifies this is complex AND user wants verification]

Clarification needed:
  This is a complex feature. A few questions:
  1. OAuth providers (Google, GitHub) or email/password only?
  2. Need password reset flow?
  3. Session persistence (JWT, cookies)?

> just email/password, yes password reset, use JWT

Filing: "add auth - email/password, password reset, JWT"
Dispatching investigation...

[Agent creates 4 tasks + checkout task with feature_id: "auth-system"]

✅ Queued: Feature "auth-system" (4 tasks + checkout)
  ├── 1. Create auth context and JWT utilities 🔴 high [pending]
  │      Files: src/contexts/AuthContext.tsx, src/utils/jwt.ts
  ├── 2. Build login/register forms 🔴 high (after #1) [pending]
  │      Files: src/components/auth/*.tsx
  ├── 3. Add password reset flow 🔴 high (after #1) [pending]
  │      Files: src/components/auth/PasswordReset.tsx, src/api/auth.ts
  ├── 4. Protect routes and add logout 🔴 high (after #2, #3) [pending]
  │      Files: src/routes/*.tsx, src/layouts/AppLayout.tsx
  └── ✓ Final checkout: Auth system integration (after all) [pending]

  All 5 tasks promoted to pending (dependency graph complete)

What else?
> status

Task Queue (project: myproject):

  ⏳ Pending (3 standalone + 2 features):
    🟡 medium - Fix scanner execution error
    🟢 low    - Fix navigation tab highlighting

  📦 Feature: dark-mode (3 tasks)
    ├── 🔵 Create theme context (running)
    ├── ⏳ Update components (waiting on #1)
    └── ⏳ Add toggle to header (waiting on #2)

  📦 Feature: auth-system (4 tasks + checkout)
    ├── ⏳ Create auth context (ready)
    ├── ⏳ Build login/register forms (waiting on #1)
    ├── ⏳ Add password reset flow (waiting on #1)
    ├── ⏳ Protect routes (waiting on #2, #3)
    └── ✓ Final checkout (waiting on all)

Tip: Run `do-work start` to process pending tasks.

What else?
> stop

Exiting ticketing mode.

Queue summary (project: myproject):
  Standalone tasks: 2
  Features: 2 (dark-mode: 3 tasks, auth-system: 5 tasks)

Next steps:
  - Run `do-work start` to process pending tasks
  - Run `/validate` after tasks complete to verify
```

**Argument passed:** $1
