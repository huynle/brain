---
description: Convert a plan into production-ready, dependency-aware Brain tasks
---

# /plan-to-tasks

Turn an implementation plan into an executable Brain task graph for the current project or the project named in `$1`.

**Arguments:** `$1` optional project ID/name. If omitted, resolve the project from the current workspace.

## Required Behavior

Load the `brain-project-planning` skill and follow it exactly:

```text
skill(name: "brain-project-planning")
```

This command only performs plan-to-task orchestration. It does not implement code.

## Input Handling

Accept the plan from the best available source:

- Brain path, ID, or title supplied by the user.
- Current conversation context when it contains a structured plan.
- Pasted plan text.

If no plan source is obvious, ask:

```text
What plan should I convert to tasks? Provide a Brain path/ID/title, paste the plan, or say "current conversation".
```

## Non-Negotiables

- Resolve project context before tasking.
- Validate the plan against the codebase with an `explore` subagent before queueing.
- Identify production-readiness work: security, DB migrations/backfills/rollbacks, API compatibility, operations, tests, and docs.
- Create executable tasks only; never create parent/container tasks.
- Use one shared `feature_id` for all tasks from the plan.
- Use `depends_on` metadata for dependency ordering.
- Prefer `execution_mode: "worktree"` and set `target_workdir` for multi-task work.
- Prevent parallel file conflicts through dependencies or task grouping.
- Create tasks as `draft` first, then promote to `pending` only after the full graph exists.
- Verify the queue with `brain_tasks` before reporting success.

## Output

After queueing, report:

- Project and feature ID.
- Number of tasks created.
- Ready starting tasks.
- Dependency/parallelization summary.
- Security, DB, or production risks that remain.
- How to start or monitor execution.

Examples:

```text
/plan-to-tasks
/plan-to-tasks brain-api
/plan-to-tasks my-project projects/my-project/plan/abc12345.md
```

Arguments: $ARGUMENTS
