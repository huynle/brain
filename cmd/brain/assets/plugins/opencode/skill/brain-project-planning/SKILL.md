---
name: brain-project-planning
description: "Use when turning an implementation plan into production-ready Brain tasks - analyzes plan gaps and risks, designs dependency-aware task graphs, and queues conflict-free work with feature_id and worktrees"
---

# Brain Project Planning

Turn a plan into an executable Brain task graph that can be safely worked by agents or brain-runner. The plan may be detailed or rough; your job is to understand it deeply, identify missing production concerns, split it into conflict-aware tasks, and queue only work that has enough context to execute.

**Announce at start:** "I'm using the brain-project-planning skill to turn this plan into production-ready Brain tasks."

## Core Principle

Do not copy plan bullets into tasks. First convert the plan into a production implementation model: risks, data changes, security boundaries, rollout requirements, dependencies, and file ownership. Then create small executable tasks with explicit `depends_on`, shared `feature_id`, and worktree-safe execution metadata.

## When to Use

- The user asks to implement, execute, queue, or bring a plan to life.
- The user references a Brain plan, project plan, PRD, architecture note, or pasted implementation outline.
- A feature needs multiple tasks, dependency ordering, or parallel agent execution.
- A plan must be made production-ready before tasking.

## When NOT to Use

- The user only wants to brainstorm or write a new plan from scratch.
- The work is a single obvious code edit that does not need Brain task orchestration.
- The user asks for immediate implementation in the current session instead of queued tasks.
- The task is a feature-completion audit; use `feature-checkout` instead.

## Required Tools

- `project_context` to resolve project and workspace context.
- `recall`, `search`, or `section` to load the plan and related context.
- `Task` with `subagent_type: "explore"` for codebase feasibility and conflict analysis.
- `save` to create draft tasks with dependencies and execution metadata.
- `update` or `bulk_update` to promote tasks after the full graph exists.
- `tasks` or `tasks_status` to verify the resulting queue.

## Workflow

### 1. Resolve Project Context

Start by resolving the current workspace:

```text
project_context()
```

Capture:

- Brain project ID.
- Current working directory.
- Latest project dream/context if present.
- Repository branch and whether worktree execution is appropriate.

If the target project is ambiguous, ask one short question before queueing tasks.

### 2. Load and Normalize the Plan

Load the plan from the strongest available source:

- A Brain path or ID with `recall(path: "...")`.
- A Brain title or search result with `search` then `recall`.
- A specific section with `section` when the user names a plan section.
- The current conversation or pasted text if no Brain entry exists.

Normalize it into:

- Plan title and objective.
- User-facing success criteria.
- Proposed phases or steps.
- Referenced files, components, endpoints, schemas, or migrations.
- Assumptions and unknowns.
- Existing docs or architecture references.

If the plan is too vague to task safely, ask for the missing decision rather than inventing it.

### 3. Production Readiness Review

Before task creation, inspect the plan for production concerns. Record the concerns in the task graph and in individual task content when relevant.

Check at minimum:

| Area | Questions to Answer |
|------|---------------------|
| Security | Authn/authz, privilege boundaries, secrets, input validation, injection risks, audit logs, dependency trust. |
| Data and DB | Schema changes, migrations, backfills, rollbacks, data retention, indexes, transactional safety, compatibility. |
| APIs and Contracts | Request/response compatibility, versioning, error formats, idempotency, pagination, rate limits. |
| Concurrency | Race conditions, retries, locking, duplicate processing, queue semantics. |
| Operations | Config, feature flags, rollout order, observability, metrics, logs, alerts, failure modes. |
| Testing | Unit, integration, migration, e2e, regression, fixtures, test isolation. |
| UX and Accessibility | Empty/loading/error states, keyboard flow, screen reader semantics, mobile behavior where applicable. |
| Performance | Query shape, caching, payload size, background work, N+1 risks. |
| Compatibility | Backward compatibility, persisted data, external consumers, cross-version deploys. |
| Documentation | Operator notes, API docs, runbooks, user-facing docs. |

A production concern becomes either:

- An acceptance criterion inside an implementation task.
- A separate task when it requires distinct files, sequencing, or expertise.
- A blocker question if it cannot be resolved from existing context.

### 4. Validate Against the Codebase

Dispatch an `explore` subagent before queueing tasks. It must not implement code.

Prompt shape:

```text
Validate this plan for production tasking. Do not edit files.

Plan:
<plan content>

Project/workdir:
<project and path>

Return:
- Existing patterns and files to follow.
- Missing or wrong file paths.
- Security concerns.
- DB migration/backfill/rollback concerns.
- API compatibility concerns.
- Testing and verification requirements.
- Hidden dependencies between steps.
- Work that can safely run in parallel.
- Files that should not be edited by parallel tasks at the same time.
- Recommended task graph with dependencies.
```

Use the validation result to adjust the plan. If validation says the plan is invalid or unsafe, stop and report the blockers.

### 5. Design the Task Graph

Create only executable tasks. Do not create parent/container tasks.

Task sizing rules:

- Each task should be small enough for one agent session.
- Split when work touches different layers with clear handoff points.
- Merge when tasks would edit the same files concurrently or cannot be validated independently.
- Separate DB migrations from code that depends on migrated schema when rollout safety matters.
- Separate security-sensitive work when it needs focused review.
- Add explicit final validation or feature-checkout task for multi-task features when appropriate.

Dependency rules:

- Foundation before dependents: config, schema, migrations, types, interfaces.
- Data before service logic when service depends on new persisted shape.
- Service/API before UI integration when UI consumes those contracts.
- Shared utilities before feature-specific users.
- Tests can be in the same task as implementation for tight TDD loops, or separate when they verify cross-task integration.
- Docs and rollout notes depend on the behavior they describe.
- Avoid parallel tasks editing the same file unless they are intentionally ordered.

Use `depends_on` metadata, not prose, for dependencies.

### 6. Assign Feature and Execution Metadata

Every multi-task plan must use one shared `feature_id`.

Feature ID rules:

- Derive from the plan title: lowercase, hyphenated, stable, short.
- Use the same `feature_id` on every task from the plan.
- Use `feature_id` instead of parent tasks for grouping.

Execution metadata rules:

- Prefer `execution_mode: "worktree"` for multi-task or agent-executed work.
- Set `target_workdir` to the resolved project checkout when known.
- Set `git_branch` to a feature branch name when the runner should isolate work.
- Use one branch/worktree per feature unless there is a concrete reason to split features.
- Use dependencies to prevent file conflicts; do not rely on agents noticing conflicts from prose.
- Preserve project, model, executor, and merge metadata if the source plan or existing tasks specify them.

Recommended defaults for queued implementation tasks:

```text
status: "draft" first, then promote to "pending"
feature_id: "<plan-title-slug>"
execution_mode: "worktree"
merge_policy: "auto_pr" or project default
merge_strategy: "squash" or project default
tags: ["plan-to-tasks", "<feature_id>"]
```

### 7. Present the Graph Before Queueing

Show the user the graph unless they explicitly asked for fully autonomous task creation.

Include:

- Feature ID.
- Project.
- Execution mode and target workdir.
- Task list with priorities.
- `depends_on` relationships.
- Parallelization opportunities.
- File conflict notes.
- Production readiness notes, especially security and DB migration tasks.
- Open questions or blockers.

Do not queue tasks while unresolved blockers remain.

### 8. Queue Atomically

Use draft-then-promote to avoid brain-runner starting before the graph is complete.

1. Create all tasks with `status: "draft"`.
2. Include `depends_on` as Brain metadata.
3. Include shared `feature_id` as Brain metadata.
4. Include execution metadata (`execution_mode`, `target_workdir`, branch/merge fields) as Brain metadata.
5. After every draft exists, promote all created tasks to `pending`.
6. If creation fails mid-way, delete or archive the created drafts, or report the exact partial state.

Create dependent tasks only after you can refer to dependency titles or IDs reliably. If using titles in `depends_on`, keep titles stable and unique within the feature.

Task content template:

```markdown
## Objective
<single executable objective>

## Plan Context
- Plan: <title or brain link>
- Feature ID: <feature_id>
- Task: <N of M>

## Scope
- <specific included work>

## Files and Areas
- `<path>` - <expected change>

## Production Notes
- Security: <relevant notes or "No special security concerns identified">
- Data/DB: <migration/backfill/rollback notes or "No DB changes">
- Operations: <config/rollout/observability notes>

## Acceptance Criteria
- [ ] <behavioral criterion>
- [ ] <security/data/testing criterion as applicable>

## Verification
- <specific test/build/manual verification commands or checks>

## Constraints
- Do not edit files owned by parallel task <task>.
- Follow existing pattern in `<reference file>`.
```

Do not include a dependency section in task content unless it explains why; dependencies belong in `depends_on`.

### 9. Verify Queue State

After promotion, verify with Brain:

```text
tasks(project: "<project>", feature_id: "<feature_id>")
```

Confirm:

- All expected tasks exist.
- Tasks are `pending` unless intentionally blocked/draft.
- Dependency classification matches the graph.
- No cycles are present.
- Ready tasks are the intended starting points.

Final response should include the feature ID, task count, ready starting tasks, and any residual risks.

## Brain Save Pattern

Example task creation metadata:

```text
save(
  type: "task",
  title: "Add billing invoice schema migration",
  status: "draft",
  priority: "high",
  project: "<project>",
  feature_id: "billing-invoices",
  depends_on: [],
  target_workdir: "<resolved workdir>",
  execution_mode: "worktree",
  git_branch: "billing-invoices",
  merge_policy: "auto_pr",
  merge_strategy: "squash",
  tags: ["plan-to-tasks", "billing-invoices", "db-migration"],
  user_original_request: "From plan <plan title>: add invoice persistence safely",
  direct_prompt: "Implement only this task. Respect dependencies and production notes in the task body.",
  content: "<task body>"
)
```

## Anti-Patterns

- Creating a parent task to represent the feature.
- Putting dependencies only in markdown instead of `depends_on`.
- Making every plan bullet a task without production review.
- Queueing tasks as `pending` before all dependencies exist.
- Parallelizing tasks that edit the same files.
- Omitting DB rollback/backfill considerations from migration tasks.
- Omitting authz/security acceptance criteria for privileged behavior.
- Using current-branch execution for multi-agent feature work without explicit user approval.
- Leaving vague tasks like "finish integration" or "handle edge cases".

## Final Checklist

- [ ] Project context resolved.
- [ ] Plan loaded and normalized.
- [ ] Production readiness concerns reviewed.
- [ ] Codebase validation completed with an explore agent.
- [ ] Dependency graph is acyclic and conflict-aware.
- [ ] Shared `feature_id` assigned to all tasks.
- [ ] Worktree execution metadata set where appropriate.
- [ ] Tasks created as drafts first.
- [ ] Drafts promoted only after the full graph exists.
- [ ] Queue verified with `tasks`.
