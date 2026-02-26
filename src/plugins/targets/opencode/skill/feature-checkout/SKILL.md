---
name: feature-checkout
description: Use when tasks for a feature are completed and need review against original user requests - gathers completed tasks via dependency graph, audits implementation against user_original_request fields, identifies gaps, and automatically creates follow-up tasks with matching feature_id, project, model, and workspace to cover missing work, then schedules another checkout to repeat until feature is fully complete. Runs fully autonomously with NO user interaction required.
---

# Feature Checkout

Audit completed feature work against original user intent. Find gaps. Create follow-up tasks. Repeat until done. **Fully autonomous — no user interaction required.**

**Announce at start:** "I'm using the feature-checkout skill to autonomously audit completed tasks against the original requests."

## Overview

This skill runs **as a task in the work queue** via `direct_prompt`. It:

1. Discovers its sibling tasks from its own `depends_on`
2. Gathers all `user_original_request` fields to reconstruct original intent
3. Dispatches an explore agent to verify implementation against intent
4. **Automatically decides** based on coverage results (no user input)
5. If gaps exist: creates new tasks + a new checkout task (recursive loop)
6. If no gaps: marks the feature as validated

The checkout task's `direct_prompt` should be:
```
Load the feature-checkout skill and process the checkout task at brain path: <task-path>. Validate implementation coverage against dependency tasks' user_original_request intent. Start now.
```

## Checklist

- [ ] Step 1: Read own task with `brain_recall(path: "<task-path>")`
- [ ] Step 2: Read own metadata with `brain_task_metadata(taskId: "<own-id>")`
- [ ] Step 3: Mark as in_progress with `brain_update(path: "...", status: "in_progress")`
- [ ] Step 4: For each dependency ID, call `brain_task_get(taskId)` to get content + user_original_request
- [ ] Step 5: Synthesize acceptance criteria from all user_original_request fields
- [ ] Step 5b: Split criteria into phases if > 7 criteria (≤7 per phase)
- [ ] Step 6: For each phase, dispatch explore agent to verify criteria
- [ ] Step 7: Merge phase results into unified coverage report
- [ ] Step 8: Auto-decide: all covered → Step 9a, any gaps → Step 9b
- [ ] Step 9a: If all covered — mark tasks validated, save summary, complete checkout task
- [ ] Step 9b: If gaps — create gap tasks + new checkout task, complete this checkout task

---

## Step 1: Read Own Task

```
brain_recall(path: "<task-path-from-prompt>")
```

Extract from the response:
- Your own **ID** (8-char)
- Your own **path**
- The **depends_on** list (these are the completed task IDs to audit)

If `depends_on` is empty, STOP and report: "Checkout task has no dependencies — nothing to audit."

## Step 2: Read Own Metadata

```
brain_task_metadata(taskId: "<own-id>")
```

Extract and save — these fields will be copied to any gap tasks:
- `feature.id` → the `feature_id`
- `feature.priority` → the `feature_priority`
- `feature.depends_on` → the `feature_depends_on`
- `execution.model` → the `model`
- `execution.agent` → the `agent` (will be null for checkout tasks; gap tasks may override)
- `execution.target_workdir` → the `target_workdir`
- `execution.git_branch` → the `git_branch`
- `tags` → base tags to propagate
- `dependencies.depends_on` → raw dependency IDs (the tasks to audit)

Also extract the **project** from the path: `projects/<project>/task/<id>.md` → `<project>`.

## Step 3: Claim Task

```
brain_update(path: "<task-path>", status: "in_progress")
```

## Step 4: Gather Dependency Tasks

For each task ID in `dependencies.depends_on`:

```
brain_task_get(taskId: "<dep-id>")
```

From each response, collect:
- **Title**
- **ID**
- **Status** (should be completed or validated; warn if not)
- **User Original Request** (the verbatim user intent)
- **Content** (implementation summary, triage notes, etc.)

### Guard: All Dependencies Must Be Complete

If any dependency is NOT `completed` or `validated`, STOP and report:

```
Cannot checkout: <N> dependency task(s) are not yet completed:
- <title> (<id>) — status: <status>

All tasks must be completed before checkout. Mark this task as blocked.
```

Then: `brain_update(path: "<task-path>", status: "blocked", note: "Dependency tasks not yet completed")`

## Step 5: Synthesize Acceptance Criteria

Combine all `user_original_request` fields into a unified picture:

1. List each unique original request (deduplicate if multiple tasks share the same request)
2. Extract discrete, testable acceptance criteria from the requests
3. Number each criterion for reference in the coverage matrix

Append to the task:

```
brain_update(
  path: "<task-path>",
  append: "## Acceptance Criteria\n\n<numbered list of criteria extracted from user_original_request fields>"
)
```

## Step 5b: Split Into Phases

A single explore agent cannot effectively verify a large number of criteria at once. Split the criteria into phases small enough for one agent to handle thoroughly.

### Sizing Rules

- **≤ 7 criteria total** → single phase (no splitting needed, skip to Step 6)
- **8–20 criteria** → split into phases of **5–7 criteria** each
- **20+ criteria** → split into phases of **5 criteria** each (more headroom per criterion)

### How to Split

1. **Group by related area** — criteria touching the same module/feature area go in the same phase (the agent can verify them together efficiently since they share codebase context)
2. **Keep related tasks together** — if multiple criteria came from the same `user_original_request`, prefer keeping them in the same phase
3. **Number phases** — Phase 1, Phase 2, ... Phase N

Example with 15 criteria:
```
Phase 1 (criteria 1-5):  Auth module — JWT, session, OAuth endpoints
Phase 2 (criteria 6-10): Data layer — schema, migrations, queries
Phase 3 (criteria 11-15): API routes — endpoints, validation, error handling
```

Store the phases as a list — you will iterate through them in Step 6.

## Step 6: Verify Implementation (Per Phase)

For **each phase**, dispatch a separate explore agent. If there is only one phase, dispatch once.

```
Task(
  subagent_type: "explore",
  description: "Verify feature phase <P>/<N>",
  prompt: """
I need you to verify a feature implementation against acceptance criteria.
This is phase <P> of <N> — focus ONLY on the criteria listed below.

## Acceptance Criteria (phase <P>/<N>)
<numbered criteria for THIS phase only>

## Completed Tasks (relevant to this phase)
<for each task relevant to these criteria: title, summary/content>

## Instructions
Examine the codebase to verify each criterion:
1. Find the implementation for each criterion
2. Check if tests exist for the implemented functionality
3. Check for integration completeness (no dangling references, incomplete wiring)

For EACH criterion, report:
- COVERED: Fully implemented with file paths as evidence
- PARTIAL: Some implementation exists but incomplete (explain what's missing)
- GAP: No implementation found

Return your findings as a structured list matching the criterion numbers.
Be thorough but factual — only mark as GAP if you genuinely cannot find the implementation.
"""
)
```

**Process phases sequentially** (one agent at a time). Collect results from each phase before dispatching the next.

After all phases complete, combine results into a single findings list covering all criteria.

## Step 7: Build Coverage Report

From the explore agent's findings **across all phases**, build a unified report:

```
## Feature Checkout Report: <feature_id>

### Original Intent
<synthesized from user_original_request fields>

### Phases
- Phase 1: <area> — criteria 1-5
- Phase 2: <area> — criteria 6-10
- ...

### Coverage Matrix
| # | Criterion | Phase | Status | Evidence |
|---|-----------|-------|--------|----------|
| 1 | ... | 1 | COVERED | src/auth/jwt.ts |
| 2 | ... | 1 | GAP | No implementation found |
| 3 | ... | 2 | PARTIAL | Google OAuth done, GitHub missing |

### Summary
- <N> / <total> fully covered
- <N> gaps
- <N> partial
- Verified in <P> phases
```

Append to the task:

```
brain_update(
  path: "<task-path>",
  append: "## Coverage Report\n\n<the report above>"
)
```

## Step 8: Auto-Decide

**This step is fully automatic. Do NOT ask the user for input.**

Evaluate the coverage report:

**If all criteria are COVERED** → proceed to **Step 9a** (validate feature)

**If ANY gaps or partial items exist** → proceed to **Step 9b** (create follow-up tasks)

The decision is mechanical: zero gaps = validated, any gaps = follow-up tasks. No judgment call needed.

## Step 9a: Feature Validated (All Criteria Covered)

1. **Mark dependency tasks as validated:**

```
brain_update(path: "<dep-task-path>", status: "validated")
```

For each dependency task.

2. **Save checkout summary to brain:**

```
brain_save(
  type: "summary",
  title: "Feature Checkout: <feature_id>",
  content: "<the coverage report>",
  tags: ["checkout", "feature-complete", "<feature_id>"],
  project: "<project>"
)
```

3. **Complete checkout task:**

```
brain_update(
  path: "<task-path>",
  status: "completed",
  append: "## Result\n\nFeature approved. All <N> criteria covered. <N> tasks validated."
)
```

## Step 9b: Gaps Found — Create Follow-Up Tasks

**IMPORTANT: Order of operations matters here.** You must:
1. Create all gap tasks and the next checkout task FIRST (they depend on this checkout task)
2. Complete this checkout task LAST (which unblocks the gap tasks in the work queue)

Gap tasks **depend on this checkout task's ID** so they stay blocked until checkout completes.

For each gap or partial item, create a new task. **Copy the task configuration from the dependency tasks:**

```
brain_save(
  type: "task",
  title: "Cover gap: <short description of what's missing>",
  content: """
## Gap from Feature Checkout

**Criterion:** <the specific criterion that wasn't met>
**Status:** <GAP or PARTIAL>
**What's Missing:** <specific missing pieces from explore agent>

## Original Context

**User Original Request:** <the specific user_original_request this gap relates to>

## Existing Implementation

<what was already done, from the completed task summaries>
""",
  status: "pending",
  priority: <derive from gap severity — GAP="high", PARTIAL="medium">,
  project: "<same project as checkout task>",
  feature_id: "<same feature_id>",
  feature_priority: "<same feature_priority>",
  feature_depends_on: <same feature_depends_on>,
  depends_on: ["<THIS CHECKOUT TASK'S ID>"],
  user_original_request: "<the specific part of the original request that wasn't covered>",
  target_workdir: "<same target_workdir>",
  generated: true,
  generated_kind: "gap_task",
  generated_key: "feature-checkout:gap:<feature_id>:criterion-<N>",
  generated_by: "feature-checkout",
  tags: ["gap", "follow-up", "<feature_id>"]
)
```

**Save each returned task ID.** You will need them for the next checkout task.

### Create Next Checkout Task

After creating all gap tasks, create a new checkout task that depends on the gap tasks:

```
brain_save(
  type: "task",
  title: "Feature checkout: <feature_id> (round <N+1>)",
  content: "Automated feature checkout — verify gap tasks cover remaining acceptance criteria.",
  status: "pending",
  priority: "medium",
  project: "<same project>",
  feature_id: "<same feature_id>",
  feature_priority: "<same feature_priority>",
  feature_depends_on: <same feature_depends_on>,
  depends_on: [<all new gap task IDs>],
  direct_prompt: "Load the feature-checkout skill and process the checkout task at brain path: <NEW_TASK_PATH>. Validate implementation coverage against dependency tasks' user_original_request intent. Start now.",
  target_workdir: "<same target_workdir>",
  generated: true,
  generated_kind: "feature_checkout",
  generated_key: "feature-checkout:<feature_id>:round-<N+1>",
  generated_by: "feature-checkout",
  tags: ["checkout", "<feature_id>"]
)
```

**Important:** The `direct_prompt` must reference the NEW task's path (returned from `brain_save`). Use the path from the response.

### Complete Current Checkout Task (LAST)

This is the final action — completing this task unblocks the gap tasks in the work queue:

```
brain_update(
  path: "<task-path>",
  status: "completed",
  append: "## Result\n\nFound <N> gaps. Created <N> follow-up tasks and scheduled next checkout.\n\nGap tasks: <list IDs>\nNext checkout: <checkout task ID>"
)
```

---

## The Recursive Loop

This skill drives a feature to full completion by iterating as many rounds as needed:

```
Round 1:  [task-A, task-B, task-C] all complete
            → checkout-1 runs, finds 2 gaps
            → creates gap-1, gap-2 (depend on checkout-1)
            → creates checkout-2 (depends on gap-1, gap-2)
            → checkout-1 completes → unblocks gap-1, gap-2

Round 2:  gap-1, gap-2 complete
            → checkout-2 runs, finds 1 remaining gap
            → creates gap-3 (depends on checkout-2)
            → creates checkout-3 (depends on gap-3)
            → checkout-2 completes → unblocks gap-3

Round 3:  gap-3 completes
            → checkout-3 runs, finds 0 gaps
            → feature validated! Loop ends.

...repeat as many rounds as needed until all criteria are covered.
```

**There is no round limit.** Each checkout either:
- Finds gaps → creates tasks + next checkout → loop continues
- Finds no gaps → validates the feature → loop ends

The dependency chain ensures correct ordering:
- Gap tasks are blocked until the checkout that created them completes
- The next checkout is blocked until all gap tasks complete
- The loop runs until the original user request is fully satisfied

---

## How to Create the Initial Checkout Task

When creating a feature's task set, add the checkout as the final task:

```
# After creating all implementation tasks and collecting their IDs:

brain_save(
  type: "task",
  title: "Feature checkout: <feature_id>",
  content: "Automated feature checkout — verify all acceptance criteria are met.",
  status: "pending",
  priority: "medium",
  project: "<project>",
  feature_id: "<feature_id>",
  feature_priority: "<feature_priority>",
  feature_depends_on: <feature_depends_on>,
  depends_on: [<all implementation task IDs>],
  direct_prompt: "Load the feature-checkout skill and process the checkout task at brain path: <TASK_PATH>. Validate implementation coverage against dependency tasks' user_original_request intent. Start now.",
  target_workdir: "<target_workdir>",
  generated: true,
  generated_kind: "feature_checkout",
  generated_key: "feature-checkout:<feature_id>:round-1",
  generated_by: "feature-checkout",
  tags: ["checkout", "<feature_id>"]
)
```

The checkout task will become `ready` only after all implementation tasks complete.

---

## Error Handling

### Explore Agent Returns Inconclusive Results

If the explore agent cannot determine coverage for a criterion, mark it as `PARTIAL` with a note. Err on the side of creating a follow-up task — false positives (unnecessary gap tasks) are cheaper than missed gaps.

### Task Metadata Unavailable

If `brain_task_metadata` fails for any task, fall back to `brain_recall` and extract what you can. The minimum required fields to propagate are `feature_id` and `project`.

---

## Anti-Patterns

**Never:**
- **Ask the user for input or approval** (this skill is fully autonomous — asking breaks the iteration loop)
- Skip the explore agent verification (don't trust task summaries alone)
- Create gap tasks without copying feature_id, project, and target_workdir
- Forget the next checkout task (breaks the recursive loop)
- Mark dependency tasks as validated when gaps exist
- Create a checkout task without depends_on (nothing to audit)
- Modify source code (this skill only reads and creates tasks)
