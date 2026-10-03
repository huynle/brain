# Runner Eligibility and Assignment Design

## Goal

Only offer runners that can execute the selected work, allow a runner to be
chosen when a task is created, and allow that choice to be changed later from
the dashboard or Brain MCP.

The design builds on existing behavior:

- runners already advertise projects, executors, capabilities, labels,
  resources, workspace roots, and credentialed Git hosts;
- tasks already carry an executor and `requires_capability`;
- project placement already carries required capabilities, labels, resources,
  and workspace policy;
- automatic scheduling already evaluates these constraints;
- feature assignments and the MCP `feature_assign` tool already exist.

The missing pieces are a shared assignment-time eligibility service,
standalone-task assignments, candidate discovery, create-time selection, and
dashboard filtering. Today the feature picker lists every online runner and
manual feature assignment checks only that a runner exists and is online. This
allows an incompatible runner to be selected even though later scheduler and
claim checks refuse its tasks.

## Decisions

1. A runner assigned to a feature must be compatible with **all unfinished
   tasks** in that feature.
2. A runner selected for a task with a `feature_id` assigns the **whole
   feature**, not only that task.
3. A runner selected for a task without a `feature_id` creates a hard
   **standalone-task pin**.
4. Compatibility mirrors durable scheduler constraints. Temporary conditions
   such as capacity, pause, draining, or a lost heartbeat do not erase
   compatibility.
5. Candidate responses distinguish durable compatibility from current
   availability.
6. Reassignment is supported from both the dashboard and Brain MCP.
7. Server-side writes revalidate compatibility. UI or MCP filtering is never
   the authorization or correctness boundary.
8. Forced assignment may bypass temporary unavailability but may not bypass
   durable incompatibility.

## Alternatives Considered

### Server-owned eligibility endpoint (chosen)

A feature- or task-scoped API evaluates runner candidates and returns stable
reason codes. Assignment writes call the same evaluator. This keeps scheduler,
MCP, and dashboard behavior aligned and supports future diagnostics.

### Enrich `GET /runners`

Adding project, feature, and task-spec query parameters to the registry route
would save an endpoint, but it would mix fleet inventory with contextual
placement, complicate SSE caching, and make one response shape serve unrelated
purposes.

### Frontend-only filtering

React could compare projects, capabilities, and executors, but it cannot
reliably evaluate placement resources, Git credential hosts, workspace policy,
or machine affinity. It would also duplicate scheduler behavior and drift over
time.

## Eligibility Model

Introduce a shared service that evaluates a runner against a normalized work
requirement. Durable compatibility includes:

- runner project allowlist: an empty list means all projects;
- executor support for every unfinished task;
- every task's `requires_capability` values;
- project placement capabilities, labels, resources, and workspace policy;
- credential support for each task's Git remote;
- machine affinity and origin-machine constraints.

Feature evaluation folds the requirements of every unfinished member task.
Completed, validated, archived, and otherwise terminal tasks do not constrain a
new assignment. The implementation must use the repository's canonical
terminal-status helper rather than introduce another status list.

Availability is reported separately and may include online state, pause,
draining, dispatch support, and remaining capacity. These values explain
whether a compatible runner can accept work now; they do not decide whether it
may be assigned for later. Candidate ordering should prefer available runners,
then apply the scheduler's stable preference order where practical.

Each rejection uses a stable machine-readable code plus a human-readable
message. Initial codes should cover project denial, unsupported executor,
missing task capability, missing project capability, missing label/resource,
workspace-policy mismatch, unsupported Git remote, and machine-affinity
mismatch. The response should include enough task IDs to explain which feature
members caused rejection without exposing secrets.

The embedded `brain-conversation-worker` is naturally excluded from ordinary
project features because it advertises only project `assistant-jobs` and
executor `assistant`.

## Data Model

Keep `feature_assignments` as the source of truth for feature-scoped pins.

Add a tenant-scoped `task_assignments` table for standalone tasks with at least:

- `project_id`;
- `task_id`;
- `runner_id`;
- `source` (`manual`, `create`, or `auto` if later needed);
- `status`;
- `assigned_at` and `updated_at`.

The key is `(tenant_id, project_id, task_id)` in tenant mode and follows the
existing normalized schema conventions. Runner references must obey the same
tenant guards as feature assignments. Assignment is runtime scheduling state,
not task authoring metadata, so it remains in SQLite rather than being copied
into every task frontmatter file.

Resolved task responses should expose the effective assignment and scope so UI
and MCP clients do not need to join tables themselves. A feature assignment
takes precedence for feature tasks; standalone task assignment applies only
when `feature_id` is empty.

## HTTP API

Add contextual candidate routes:

- `GET /api/v1/tasks/{projectId}/features/{featureId}/runner-candidates`
- `POST /api/v1/tasks/{projectId}/runner-candidates` for a proposed task spec
- `GET /api/v1/tasks/{projectId}/{taskId}/runner-candidates` for an existing
  standalone task

The proposed-task request accepts only placement-relevant fields: optional
feature ID, executor, required capabilities, Git remote, machine affinity,
origin machine, execution mode, and target workdir. If a feature ID is present,
the service evaluates the union of existing unfinished tasks and the proposed
task.

Candidate responses contain runner identity, compatibility, availability,
reasons, and relevant advertised capabilities. The default dashboard and MCP
formatters show compatible candidates; callers may request rejected candidates
for diagnostics.

Keep the existing feature assignment routes, but make them use the shared
eligibility service. Add equivalent standalone-task assign, reassign, and clear
routes. Assignment responses include previous runner, new runner, scope, and
timestamps.

Creation accepts optional `runner_id` and explicit assignment intent. For a
feature task it creates or changes the feature assignment. For a standalone
task it creates the task assignment. The service validates before writing and
uses compensation if assignment persistence loses a race after the file write:
remove the newly created task and index entry, then return a conflict. It must
never silently leave a requested pin unapplied.

## Brain MCP

Extend `save(type: "task")` with:

- `requires_capability: string[]`;
- `runner_id?: string`;
- `assignment_intent?: "assign" | "reassign"`.

Add read tools:

- `runner_candidates` for a proposed or existing standalone task;
- `feature_runner_candidates` for an existing feature.

Add write tools:

- `task_assign`;
- `task_clear_assignment`.

Retain `feature_assign` and `feature_clear_assignment`, but update their
descriptions and output to state that the assignment covers all unfinished
tasks in the feature. `feature_assign` and `task_assign` both use server-side
eligibility validation.

Keep `runners` as the generic fleet inventory tool. Correct its project filter
so a runner with an empty project allowlist is included for every project,
matching scheduler semantics.

MCP candidate output should show runner name/ID, compatibility, current
availability, executors, capabilities, and concise rejection reasons. This lets
an agent discover candidates before creating a task instead of guessing a
runner ID.

## Dashboard UX

The feature detail assignment section loads the feature candidate endpoint and
shows compatible online runners. It continues to show the currently assigned
runner even if that runner later becomes offline or incompatible, with a clear
warning and the reason. Selecting another compatible runner performs an
explicit reassignment.

Standalone task detail receives the same assignment control using the task
candidate endpoint. Task-creation UI, where present, loads proposed-task
candidates after enough placement fields are known and includes the optional
runner selection in the create request.

The UI must not locally reconstruct compatibility. Shared TypeScript types can
format availability and reason codes, but the server response is authoritative.
Loading and failure states should not fall back to displaying all runners,
because that recreates the current correctness bug.

Successful assignment changes update optimistic local state and invalidate the
candidate, runner, task, and feature queries. The server emits the existing
assignment/runner update event where possible, or adds a scoped assignment
event if the existing event cannot represent standalone tasks.

## Consistency and Safety

- Assignment endpoints re-read the runner and work immediately before writing.
- Adding or editing a feature task can make its assigned runner incompatible.
  Such a write should be rejected when it introduces an unsatisfied hard
  requirement, unless the request also supplies a compatible reassignment.
- Claim, pull, push-dispatch, and run-now paths enforce task or feature pins.
- Deleted runners do not delete assignment history automatically; the UI shows
  the unresolved assignment and offers reassignment or clear.
- In-progress tasks are not migrated by reassignment. The new assignment
  governs unclaimed unfinished work; a live claim remains with its owner until
  completion, release, or abandonment recovery.
- Capacity, pause, draining, and heartbeat state remain scheduling concerns.
  They may affect `available` and ordering but do not invalidate the pin.

## Implementation Plan

### Phase 1: Shared eligibility domain

1. Extract durable checks from `internal/service/scheduler.go` into reusable,
   side-effect-free helpers while preserving scheduler behavior.
2. Add normalized requirement, candidate, availability, and reason types in
   `internal/types`.
3. Implement proposed-task, existing-task, and all-unfinished-feature
   evaluators in `internal/service`.
4. Add table-driven tests proving parity with project, executor, capability,
   placement, Git, workspace, and affinity scheduler checks.

### Phase 2: Standalone assignment storage and enforcement

1. Add the normalized `task_assignments` schema and tenant coverage.
2. Implement assign-if-empty, force-reassign, get, list, and clear storage
   operations following `feature_assignments` patterns.
3. Expose effective assignment on resolved tasks.
4. Enforce standalone pins in ready-task filtering, claim validation,
   push scheduling, direct dispatch, and run-now.
5. Add race, tenant-isolation, stale-runner, and conflicting-claim tests.

### Phase 3: Candidate and assignment HTTP APIs

1. Add service interfaces, handlers, and routes for proposed-task, task, and
   feature candidates.
2. Refactor feature assignment to reject durable incompatibility through the
   shared evaluator.
3. Add standalone task assign/reassign/clear routes.
4. Return structured conflict details suitable for UI and MCP display.
5. Add handler and service tests for compatible, rejected, reassigned, and
   unavailable-but-compatible runners.

### Phase 4: Create-time assignment

1. Add `requires_capability`, `runner_id`, and assignment intent to task-create
   request handling.
2. Validate a proposed task and its selected runner before persistence.
3. Persist feature or standalone assignment after task creation, with tested
   compensation on conflicts or storage failures.
4. Reject feature edits that invalidate the current assignment unless a
   compatible reassignment accompanies the edit.
5. Verify old clients that omit assignment fields retain current behavior.

### Phase 5: Brain MCP

1. Extend the `save` tool schema and request body.
2. Add `runner_candidates` and `feature_runner_candidates` tools and formatters.
3. Add `task_assign` and `task_clear_assignment`; update feature assignment
   descriptions and output.
4. Fix generic runner project filtering for empty allowlists.
5. Add schema, request-capture, formatting, validation, and error tests.

### Phase 6: Dashboard

1. Add API client methods and TypeScript types for candidate and assignment
   responses.
2. Replace the feature detail's all-online-runner list with server candidates.
3. Add standalone task assignment/reassignment controls.
4. Preserve and warn on an unavailable or incompatible current assignment.
5. Wire optimistic updates, query invalidation, and SSE reconciliation.
6. Add component/helper tests covering the conversation worker exclusion,
   mixed-capability features, current-runner visibility, reassignment, and
   candidate-load failure.

### Phase 7: Verification and rollout

1. Run focused Go service/API/storage/MCP tests and frontend tests.
2. Run `just vet`, `just test`, the web typecheck/tests, and `just build`.
3. Verify an ordinary project excludes `brain-conversation-worker` while
   `assistant-jobs` still recognizes it for its private executor path.
4. Verify create-time feature assignment, standalone assignment, dashboard
   reassignment, MCP reassignment, and claim enforcement end to end.
5. Ship schema migration before enabling task assignment controls; no data
   backfill is required because missing assignments preserve current behavior.

## Acceptance Criteria

- The feature assignment picker never offers `brain-conversation-worker` for
  an unrelated project.
- A runner appears only if it satisfies every unfinished feature task's durable
  requirements.
- Compatible but temporarily full, paused, draining, or offline runners remain
  identifiable as compatible; only online candidates are offered for a new UI
  selection.
- Brain MCP can discover eligible runners before task creation.
- Task creation can atomically request a runner assignment or fail without
  leaving an unassigned task.
- A feature task's runner selection assigns the whole feature.
- A standalone task can be pinned, reassigned, and cleared independently.
- UI and MCP can reassign existing work to a more capable compatible runner.
- Manual assignment cannot bypass project, executor, capability, placement,
  Git, workspace, or machine-affinity constraints.
- Existing clients and unassigned tasks continue to schedule as before.
