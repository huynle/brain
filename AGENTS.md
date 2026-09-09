# AGENTS.md

This file provides guidance for AI assistants working with the brain codebase.

## Project Overview

Brain API is a REST service for AI agent memory and knowledge management, with an integrated task queue processor. Built with Go and the standard library. The dashboard is the embedded PWA (`web/`), not a terminal UI.

## Key Commands

```bash
# Development
just build           # Build all Go binaries
just test            # Run all tests
just vet             # Run go vet (static analysis)
just check           # Run all checks (vet + test + lint)
just dev             # Run brain-api server

# Task Runner
brain run list <project>                    # List tasks

# API Server
./bin/brain-api      # Start API server
go run ./cmd/brain-api  # Run API server without building
```

## Architecture

### Command-Line Tools (`cmd/`)
- `brain-api/` - REST API server entry point

- `brain/` - Main CLI with subcommands (server, runner, doctor, etc.)
- `brain-mcp/` - MCP (Model Context Protocol) server

### Core API (`internal/api/`)
- `entries.go` - CRUD for brain entries
- `search.go` - Full-text search
- `graph.go` - Graph traversal (backlinks, outlinks)
- `tasks.go` - Task queue endpoints
- `sections.go` - Section extraction from entries

### Core Services (`internal/service/`)
- `brain_service.go` - Main service layer
- `task_service.go` - Task management with dependency resolution
- `task_deps.go` - Dependency graph algorithms

### Feature checkout automation

Two built-in automations trigger on `feature.completed`, discriminated by task-level `checkout_mode`:

- `brain:builtin-feature-checkout` — AI-driven (LLM prompt), default path.
- `brain:builtin-feature-checkout-simple` — deterministic squash-merge via script executor.

Fold rule across a feature's tasks: any `checkout_mode:"simple"` → simple path; else AI. Missing/empty → AI. Folded value is placed on the `feature.completed` event's `metadata["checkout_mode"]` by `CheckFeatureCompletion` and matched by the automations' `Trigger.Filter`.

The simple script honors the `git -c merge.ff=true` invariant (see `feature-checkout/SKILL.md`) so it works regardless of user `merge.ff` gitconfig. It uses `feature_id` as the source branch name and cannot recover from merge conflicts — use AI mode for anything non-trivial.

### Feature git delivery automation

A separate, opt-in path that actually delivers a completed feature's branch via git — distinct from the review-oriented checkout automations above. Controlled by the task-level `delivery_mode` field: `none` (default; nothing is pushed or merged), `mr` (push the feature branch + open a merge request), or `local_merge` (squash-merge into the target branch locally + push). A `merge_policy → delivery_mode` bridge lets legacy tasks opt in without a new field: `auto_pr → mr`, `auto_merge → local_merge`.

Fold rule: `foldDeliveryMode` reduces the per-task `delivery_mode` across a feature's tasks and `CheckFeatureCompletion` places the folded value on the `feature.completed` event's `metadata["delivery_mode"]`, matched by the automation's `Trigger.Filter` (`delivery_mode:"in:mr,local_merge"`, `project:"*"`). `ResolveFeatureDelivery` (`internal/service/feature_delivery.go`) is the authoritative resolver: conflicting per-task modes across one feature → error, never a silent pick.

The built-in `brain:builtin-feature-delivery` automation (registered by `EnsureBuiltInFeatureDeliveryAutomation`) is gated by `cfg.FeatureDelivery.Enabled` (default **OFF**, also `BRAIN_FEATURE_DELIVERY_ENABLED`); while off nothing is registered and no feature is pushed or merged by this path. Its action is a deterministic `script` (no LLM) rendered by `buildFeatureDeliveryScript` with two runtime modes:

- **mr**: `git push -u origin <feature>` then `glab mr create --squash-before-merge` (GitLab; GitHub not yet implemented, unknown provider errors) — never merges, never deletes the source branch. NOTE: the flag is `--squash-before-merge` (sets the MR to squash on accept), NOT `--squash`; `glab` has no `--squash` flag and the script fails loudly if it is used. On success it PATCHes `mr_url` + `status:"mr_open"` onto the feature's Brain-native `merge_request` entry (best-effort, non-fatal).
- **local_merge**: a FAIL-CLOSED protected-branch guard runs first — it queries `glab api projects/:id/protected_branches/<target>` and distinguishes three outcomes: a JSON body naming the branch → protected → REFUSE (`local_merge refused — set delivery_mode: mr`); a `404` → not protected → proceed; ANY other result (glab error, missing glab, non-gitlab remote with a real origin) → could-not-determine → REFUSE. The design invariant "never direct-push a protected target" means an indeterminate check fails closed, never "best-effort proceed". A purely local repo with no remote is the one allowed-through case. Only after the guard passes: `git -c merge.ff=true merge --squash <feature>` (Finding-7 invariant), push target, worktree + local/remote source-branch cleanup, then PATCHes `status:"merged"` onto the `merge_request` entry (only if the target actually pushed).

The write-back reuses one shared `brain_patch_merge_request()` bash function that resolves the entry path via `GET /api/v1/entries?type=merge_request&project=..&feature_id=..` and PATCHes `/api/v1/entries/{path}/metadata` using `BRAIN_API_URL`/`BRAIN_API_TOKEN`; `mr_url` is an audit-only DB-mirror field (in `AllowedMetadataUpdateFields` + `runtimeKeys`, never frontmatter) and merge_request statuses are free-form so `mr_open`/`merged` are accepted with no enum change. All git ops run runner-side in the feature's worktree; the `merge_request` status lifecycle is `pending → mr_open` (mr) or `pending → merged` (local_merge).

The write-back's `curl` calls expand an optional `-H Authorization` array with the bash-3.2-safe idiom `${auth_args[@]+"${auth_args[@]}"}` — a bare `"${auth_args[@]}"` under `set -u` is an unbound-variable error on macOS's bash 3.2 when no `BRAIN_API_TOKEN` is set, which would otherwise abort the write-back. The protected-branch guard likewise captures glab's exit code with `PB_OUT="$(...)" && PB_RC=0 || PB_RC=$?` so `set -e` does not kill the script at the command-substitution assignment when glab errors.

Verified end-to-end against `orion/ai/canis` on `gitlab.us.lmco.com` (brain task `akzdsp8n`): mr mode pushed the source branch and opened an MR into the protected `dev` target (opened, not merged, squash-on-accept, source branch preserved) — glab CLI resolution can be tripped by a local SSH-config `HostName` rewrite, in which case pass an https origin or a resolvable remote; local_merge squash-merged + pushed + cleaned up against an unprotected sandbox target and was correctly REFUSED against protected `dev`; opt-in gating (opted-in fires, `none`/missing does not, master-switch-off registers nothing) confirmed via the automation matcher; the `mr_url` + `status` metadata write-back was confirmed accepted by `PATCH /entries/{path}/metadata`. The GitHub PR path remains an unexercised stub (out of scope; GitLab-only).

### Goal subsystem (check + steer loop)

A goal is an `automation` BrainEntry with `Goal *GoalConfig` (`generated_by: brain-goal`, tags `[goal, goal:<id>]`). Scope resolution: `task_id` → that one task; else `feature_id` → the feature's tasks; else the whole project. Core: `internal/service/goal_service.go` (+ `goal_api.go`, `goal_automation.go`, `goal_steering.go`), HTTP in `internal/api/goals.go` (CRUD incl. `DELETE /goals/{id}`, `?status=` listing), steerer wiring in `internal/apiserver/goal_steerer.go`.

- **Reconcile** is deterministic over linked-task statuses (`decideReconcile`): no tasks → `need_work` (generates one task, deduped on `goal:<id>:need_work`); all complete → `complete` (flips the goal entry to `completed`; reactivate via PATCH `status=active`); any in-progress → steer-or-noop; blocked → `block`. Per-goal mutex serializes event/ticker/manual callers.
- **Cadence**: event-driven (`task.status_changed`, `feature.completed` — the latter matches WITHOUT `to_status`, which feature events never carry) plus a periodic ticker (`goalReconcileInterval`, 5m).
- **Steering**: when linked work is in progress and `steering` is enabled (default on, cooldown 15m, persisted as `last_steered_at`), the reconcile injects a "## Goal check-in" prompt (title + criteria + validation + self-assess instruction) into each live session via the same in-process plumbing as the control API (`prompt_async`). OpenCode-only; pi instances are skipped as unsupported. Audit decision `steer` with steered/skipped counts. Nil steerer or runner-pause ⇒ silently skipped.
- **Lookups are status-agnostic** (`findGoalByID` searches all statuses) so pause (`blocked`) → resume (`active`) round-trips; only event dispatch and the ticker filter to `active`.
- **Known limitation**: `complete` is status-based, not criteria-verified — a task that completes without actually meeting the goal criteria still completes the goal. A criteria-validation task on the complete path is the designed next step. Also `opencode run` exits when its current turn ends, so a steered agent must act on the injection within that turn.

### Abandonment + Resume model

When a runner dies mid-task, or when a task's claim lease expires without renewal, the task's `status` stays stuck at `in_progress` while nothing is actually running it. The abandonment surface makes that recoverable without introducing new sweepers.

**Detection (read-only, derived at API-response time):**
- `enrichAbandonmentState` in `internal/service/task.go` runs alongside `enrichDispatchDiagnostics` for every task in a `GET /tasks` response. It cross-references `task_claims` (from `StartClaimCleanup`), the owning runner's `runners.status` (from `RunLifecycleSweep`), and the reaper marker note text / metadata (from `reapOrphanedTasks`) to derive two fields on `ResolvedTask`:
  - `is_abandoned bool`
  - `abandon_reason string` — one of `no_claim | claim_expired | runner_offline | orphan_reaped`
- No new sweeper is introduced. Every signal is produced by an existing background job; enrichment just reads them together.

**Recovery (`POST /api/v1/tasks/{project}/{taskId}/resume`):**
- Validates `is_abandoned` (unless `force: true`), refuses to release a claim held by an online runner (live-claim safety — force does NOT override this), stamps `metadata.resume_requested=true`, flips `status` to `pending`. Emits `EventTaskResumeRequested` + `EventTaskStatusChanged`.
- The runner reads `resume_requested` in `claimAndSpawnWithWorkdir` and passes `IsResume=true` into the executor's prompt builder (`CommonBuildPrompt`). The flag is cleared after a *successful* `Spawn`; on Spawn-error rollback it is best-effort re-stamped so the intent survives a retry.
- `POST /api/v1/tasks/{project}/features/{featureId}/resume` fans out across every task in a feature; per-task outcomes come back in `ResumeFeatureResult.results` (skipped entries include a `reason` so partial failures don't fail the batch).
- Idempotent: a resume on a task already `pending+resume_requested=true` returns `Resumed=false` with an explanatory `Reason` and skips cleanup work.
- The orphan reaper (`tryReapOrphan`) skips tasks with `resume_requested=true` and re-reads the task immediately before its status flip, so a Resume that races with a reaper doesn't get silently reverted.

### Supervisor session-resume with context

A richer resume path (`POST /api/v1/tasks/{project}/{taskId}/resume-with-context`, service `ResumeTaskWithContext`, feature fan-out `POST /features/{featureId}/resume-with-context`) hands the relaunched agent supervisor-authored context. It is distinct from plain `/resume`: the request body (`ResumeWithContextOptions`) carries `injected_context` (required), `prefer_same_session` (default true), `executor_override` ("pi"/"opencode"), and `force`.

- **Live short-circuit.** If the task's session is still running, the context is injected into that session with no relaunch and no status flip — reported as `resume_mode=live_injected`, `injected_live=true`.
- **Relaunch modes.** Otherwise the endpoint stamps extended runtime metadata and flips `status` to `pending`, letting the runner reclaim it. The three modes: `same_session` (reattach the prior OpenCode session id), `rehydrate` (fresh session seeded with a bounded prior transcript + injected context), `live_injected` (above).
- **Extended metadata keys.** Alongside `resume_requested=true` / `resume_requested_at`, the endpoint stamps `resume_mode`, `resume_injected_context`, `resume_prefer_same_session`, `resume_executor_override`. These are runtime-only (never on-disk frontmatter) and parsed onto `ResolvedTask`.
- **Runner is authoritative.** The API's `resume_mode` is advisory. In `claimAndSpawnWithWorkdir` (`applyResumeWithContext`) the runner picks the of-record stored session — most-recent by `SessionInfo.Timestamp`, tie-broken by id, never "newest live" — and calls `taskExecutor.CanResumeSession(storedID)`. It finalizes `same_session` only when `prefer_same_session` holds, `executor_override` does not change the executor, a stored id exists, and the probe reports `SameSession=true`; otherwise it rehydrates, best-effort pre-fetching the prior transcript (`readSessionHistorySQLite` then `readSessionHistory`) into `SpawnOptions.PriorTranscript`. `InjectedContext` is always carried.
- **OpenCode true-resumes dead sessions** because its history is durable in the on-disk session store (SQLite or legacy files), so `CanResumeSession` can succeed even after the live instance is gone. **Pi always rehydrates** — it has no session continuation and coerces `same_session`→`rehydrate` in `Spawn`.
- **Metadata lifecycle mirrors plain resume.** On a successful `Spawn` the runner clears all extended keys (`resume_mode`/`resume_injected_context`/`resume_prefer_same_session`/`resume_executor_override` to zero-values, plus `resume_requested=false`); on Spawn-error rollback it re-stamps them so a retry keeps the injected context + mode intent. The legacy path (`resume_requested` set, `resume_mode` empty) is untouched — `IsResume=true` only.
- The MCP tool `resume_task_with_context` (in `internal/mcp/task_tools.go`) wraps the endpoint.
- **Verification status (honest).** Every path is proven at the build + test + command-capture level, not by a live end-to-end run against real `opencode`/`pi` processes with a running runner (impractical in a sandbox with no live executors). `just vet` is clean, `just build` succeeds, and `just test` passes (32/32 packages). Per-path evidence: same-session feeds the STORED id into `--session` and never creates a fresh session (`TestSpawn_SameSession_UsesStoredSessionNoCreate`, command-capture; confirmed via a proof-of-negative — forcing the fresh-create branch makes it fail with `ses_freshly_created`); rehydrate creates a fresh session and bounds the transcript (`TestSpawn_Rehydrate_CreatesFreshSession`, `TestBuildRehydratePrompt_*`); Pi coerces `same_session`→`rehydrate` and injects context+transcript at its `Spawn` boundary without leaking the stored id (`TestPiExecutor_Spawn_ResumeWithContext_RehydratesAndInjects`, `TestPiExecutor_CanResumeSession`); live-inject skips relaunch via a mocked bridge (`TestResumeTaskWithContext_LiveInject`, `TestHandleResumeWithContext_200_LiveInjected`); live-claim safety and idempotency hold (`TestResumeTaskWithContext_LiveClaimSafety` / `_Idempotent`). The `OpenCode true-resumes dead sessions` claim rests on `CanResumeSession` finding durable on-disk history (SQLite/legacy); if a given deployment's history is absent, the same probe returns `SameSession=false` and the runner falls back to rehydrate — the fallback is the tested, guaranteed behavior. See [[projects/brain-api/report/shhk3jt5.md]] for the full verification log.

### Index freshness (who writes to the brain dir)

SQLite is a derived view of the markdown files. Everything the API serves —
search, the link graph, orphan detection — reads the index, not the disk, so a
file that lands without an `IndexFile` call is invisible until something
re-indexes it.

- **Every writer indexes.** `BrainServiceImpl.Save`/`Update`/`Move` and
  `TaskServiceImpl.CheckoutFeature` all call `indexer.IndexFile` immediately
  after the write. `NewTaskService` takes the indexer as a required argument
  for exactly this reason — CheckoutFeature is the one task-service path that
  writes a file, and it silently skipped indexing until 2026-08-26.
- **Boot indexes once.** `internal/apiserver/server.go` runs `IndexChanged` in
  a background goroutine at startup, then never scans again.
- **Content-root policy:** discovery (`IndexChanged`, `RebuildAll`, `GetHealth`)
  and watcher startup/new-directory walks use exact first-component roots
  `projects/` and `global/` under the supplied `brainDir`. All siblings, including
  `attachments/`, `.git/`, `.brain-data/`, and root-level Markdown, are excluded;
  excluded directories are pruned before reading their children. The watcher
  keeps `brainDir` itself watched as an anchor for initially absent content roots,
  sweeps populated new content subtrees and watches them for later writes.
  Custom/default watcher ignores still apply within allowed roots. Directory
  symlinks are not recursively followed; parser containment remains authoritative
  for file reads, and direct `IndexFile` is not layout-restricted.
- **P3 integration:** indexer and watcher constructors are unchanged and must
  receive the same supplied `brainDir` root. P3 root ownership must preserve
  `projects/` and `global/` directly beneath that root, not pass either subtree
  as the root or flatten the layout.
- **Out-of-band writes need the watcher.** A git pull into the brain dir, a
  manual edit, or another process bypasses both of the above. `indexer.FileWatcher`
  covers that gap, enabled with `server.index_watch.enabled` in config.yaml or
  `BRAIN_INDEX_WATCH=true`. **It is off by default**: the watcher registers one
  fsnotify watch per allowed, non-ignored content directory (plus the root
  anchor), and a large content tree can still exhaust the
  platform's watch limit (inotify `max_user_watches`). With it off, out-of-band
  writes appear only after a server restart.
- The watcher starts after the boot scan finishes so the two never race on the
  same path, and is stopped before the store closes so no debounced flush hits
  a closed database.
- fsnotify is not recursive. A directory created after startup arrives as a
  single Create event naming only that directory, while the OS has usually
  already built the rest of the chain and dropped files into it — so
  `addDirRecursive` walks each new directory, watches every level, and queues
  the markdown already inside. Without that walk a pulled
  `projects/foo/note/` subtree is never watched at all.
- `RebuildAll` exists but has no caller outside tests — there is no CLI or API
  route to force a full reindex. Content already on disk with a matching
  checksum is skipped by `IndexChanged`, so extraction fixes reach it only via
  a migration that nulls the affected checksums.

### Subagent session drill-down (recursive child-session viewing)

The dashboard session view lets a user drill from a subagent invocation into
that subagent's OWN full transcript (its messages, tool-calls, reasoning) —
recursively for subagents-of-subagents — both live (streaming) and in history
(dead-instance safe). This is a *linkage* layer over the existing
session-id-keyed read/render machinery: once brain knows a child session id,
the unchanged single-id readers and the `<Transcript>` component render it.

**parentID capture (backend).** A subagent shows up as an OpenCode tool part
with `tool === "task"`; invoking it spawns a *child* OpenCode session whose
`parentID` is the hosting session. The runner captures that link in two places
that were previously dropping it:
- `opencodeSession` (`internal/runner/runner.go`) decodes the OpenCode
  `parentID` field (Go: `ParentID string` with tag `json:"parentID,omitempty"`)
  from `/session` — it used to decode only `id`+`time`. Removing that field
  build-fails
  `internal/runner/session_parent_decode_test.go` — the regression guard.
- `SessionInfo.ParentID` (`internal/types/types.go`, `json:"parent_id"`)
  persists the parent link so the parent→child tree survives instance death.

**Child-discovery API.** The child *tree* is discovered from the persisted
`parent_id` linkage, sourced live (bridge) or from history (SQLite/on-disk),
so it works after the instance exits:
- `session_children.go`: `readSessionChildrenSQLite` (indexed
  `SELECT ... FROM session WHERE parent_id = ?`, read-only DSN) with a
  filesystem fallback (`storage/session/global/*.json`); `childrenOf`
  (SQLite-first, FS fallback, empty ≠ error); pure `buildChildrenTree`
  (depth cap — 1 flat / 5 recursive — plus a mandatory ancestor-path cycle
  guard, always a non-nil slice).
- Bridge: `FrameChildren` frame (`Recursive`/`Depth`) + `Hub.FetchChildren`
  + runner `handleChildren` → `fetchSessionChildren`, mirroring the existing
  `FetchHistory` plumbing.
- HTTP: `GET /control/runners/{runnerId}/sessions/{sessionId}/children[?recursive=true&depth=N]`
  (`HandleControlSessionChildren`), sibling of the history route. Child
  *transcripts* themselves reuse the existing history route unchanged.

**Correlation (a tool-call → its child session id).** OpenCode writes the
child id into the tool part's `state.output` inside a `<task_metadata>` block:
`session_id: ses_…`. That regex is the primary source (see
`web/src/lib/subagent.ts childSessionIdFromPart`); `state.metadata.sessionId`
is a secondary fallback. (The original ADR's `metadata.sessionId`-primary and
`task_id:`-prefix guesses were both wrong — verified against a real DB.)

**Frontend drill-down.** `web/src/components/Session/Transcript.tsx`:
- `subagentDrilldownState(part, sessionRef, ancestors, depth, maxDepth)` in
  `web/src/lib/subagent.ts` is the single, unit-tested source of truth for the
  gating decision (`childId`/`canDrill`/`capped`/`cappedReason`). `PartView`
  calls it — the drill-down affordance renders only when `canDrill`.
- `SubagentDrilldown` is a lazy, expandable `<details>` (fetch/subscribe starts
  only on expand) that renders the child via the same `useSessionTranscript` +
  `<Transcript>`; it recurses by passing `depth+1` and
  `ancestors=[...ancestors, childId]`, so a subagent of a subagent drills down
  further.
- `childSessionRef` maps a live parent → live child (same runner+instance,
  child session id ⇒ streams) and a history parent → history child by id
  (dead-instance safe). `applyEvent`'s per-hook single-session filter is left
  intact: nesting is compositional (each nested pane pins to its own session
  id), never a global unfilter.
- Non-subagent tool parts are unchanged (input/output/error `<details>` as
  before). A subagent with no resolvable child id (Pi tasks, or output without
  the metadata block) renders normally with no affordance and no error — the
  cap note only appears for a resolvable child blocked by cycle/max-depth.

**Pi.** Pi does not spawn OpenCode-style nested child sessions, so a Pi task
part resolves no child id and cleanly renders without a drill-down (no error),
which falls out of the same gating logic — no Pi-specific code path.

### Storage Layer (`internal/storage/`)
- `entries.go` - Entry storage operations
- `search.go` - Full-text search indexing
- `tasks.go` - Task persistence
- `graph.go` - Graph relationship storage

### Task Runner (`internal/runner/`)
- `runner.go` - Main runner orchestration (poll loop, claim/spawn, completion)
- `client.go` - Brain API HTTP client
- `executor.go` - OpenCode executor (`TaskExecutor` interface impl)
- `pi_executor.go` - Pi executor (`TaskExecutor` interface impl)
- `executor_factory.go` - `ExecutorRegistry` and `ResolveExecutorForTask` precedence chain
- `executor_common.go` - Shared logic: workdir resolution, prompt building, env exports, cleanup
- `idle_detection.go` - Idle detection: OpenCode HTTP polling, Pi process-exit detection
- `process_manager.go` - Child process lifecycle and tracking
- `state_manager.go` - Persistent state for runner
- `types.go` - All config, execution, state, and event types
- `signals.go` - Graceful shutdown handling
- `execute.go` - Manual execution and feature batch execution
- `schedule.go` - Cron scheduling for recurring tasks
- `logging.go` - slog-based event handler for headless mode
- `sse_listener.go` - SSE stream watcher for task changes

#### Multi-Executor Architecture

The runner supports multiple executor backends via the `TaskExecutor` interface and `ExecutorRegistry`:

```
ExecutorRegistry
├── "opencode" → OpenCodeExecutor  (HTTP API-based, port polling for idle detection)
└── "pi"       → PiExecutor        (RPC mode via stdin, process-exit for completion)
```

**TaskExecutor interface:**
```go
type TaskExecutor interface {
    BuildPrompt(task *types.ResolvedTask, isResume bool) string
    ResolveWorkdir(task *types.ResolvedTask) (string, error)
    Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error)
    Cleanup(taskID, projectID string) error
    // CanResumeSession is a read-only probe: does this runner hold the given
    // session's history so a true same-session resume is possible? OpenCode
    // probes its on-disk session store (SQLite, then the legacy file layout);
    // Pi always returns false (no session continuation).
    CanResumeSession(sessionID string) SessionResumeCapability
}
```

**Executor resolution precedence:**
```
task.Executor > task_defaults.executor > config.DefaultExecutor > "opencode"
```

#### Pi Executor

The Pi executor spawns [Pi](https://github.com/anthropics/pi) processes in RPC mode (`--mode rpc`). Key features:

- **Agent bundles**: Resolved from `~/.pi/brain-agents/<agentName>/config.json`
  - `system_prompt_file`: Path to system prompt markdown
  - `extension`: Agent-bundled TypeScript extension
  - `thinking`: Thinking level (off/minimal/low/medium/high/xhigh)
  - `tools`: Tool restriction list
- **Extension composition** (3 layers, all additive):
  - Layer 1: Agent-bundled extension (from agent bundle config.json)
  - Layer 2: Config always-on extensions (`config.Pi.Extensions`)
  - Layer 3: Per-task extensions (`task.Extensions`)
- **Short name resolution**: `"code-review"` resolves to `~/.pi/extensions/brain-code-review.ts`
- **Model precedence**: `task.Model > runtime default > config.Pi.Model`
- **Idle detection**: Pi RPC processes exit when done (process exit = completion); no HTTP polling needed
- **Graceful fallback**: Missing agent bundle falls back to `--append-system-prompt`

#### Configuration

#### Task Git remote policy

Task `git_remote` admission is HTTPS-only and requires a registered runner's
credentialed-host advertisement. Registration/heartbeat derive reserved
`git-credential-host:<authority>` capabilities; the service gates Save/Update,
metadata writes and generated checkouts before persistence, and dispatch/pull/
direct claims require compatible live runners. See
[Git remote policy](docs/git-remote-policy.md) for nested/flat configuration,
legacy GitHub-only binding, explicit-empty deny, anonymous-transport limits,
redirect/CA behavior and cache migration costs.

#### Shared runner workdir policy

`runner.control.allowed_workdir_roots` applies to **both task executors and
ad-hoc spawning**, even when `control.disabled` is true. In flat/legacy config,
use `control.allowed_workdir_roots` without the `runner` wrapper:

```yaml
runner:
  control:
    allowed_workdir_roots:
      - /srv/projects
      - /srv/brain-repo-cache
```

- Empty/omitted roots mean **HOME ONLY**; unavailable home fails closed.
  Explicit roots replace, rather than extend, the home default. Use existing
  absolute directories (no shell `~` expansion).
- Both candidates and roots are symlink-canonicalized, including macOS
  `/var` → `/private/var`. Sibling prefixes and symlink escapes are refused.
- Execution requires an existing directory. A supplied invalid/forbidden
  `target_workdir` is an error, not a fallback hint. Successful origin, workdir,
  resolved-workdir, config-default and worktree resolutions all use the same
  validator; executor `SpawnOptions.Workdir` overrides are checked again.
- Before clone/fetch or worktree creation, repo/destination paths are checked;
  missing destinations are checked through their canonical existing parent.
  Allow the main repo as well when creation from a linked worktree needs it.
- This is cwd authorization, **not a filesystem sandbox**: child code and Git
  metadata are not confined, and concurrent symlink replacement is not made
  race-free. `script.workdir_restrict` remains a separate, unchanged policy.
- Local script-child integration proves linked-worktree execution, not actual
  LLM compatibility. Real LLM compatibility remains a release gate.

**Config types** (`types.go`):
```yaml
# Runner config (config.yaml or env vars)
pi:
  bin: "pi"                           # PI_BIN env var
  model: "anthropic/claude-sonnet-4-20250514"    # PI_MODEL
  thinking: "high"                    # PI_THINKING (off/minimal/low/medium/high/xhigh)
  agents_dir: "~/.pi/brain-agents"    # Agent bundle directory
  extensions_dir: "~/.pi/extensions"  # Extension resolution base
  extensions:                         # Always-on extensions (Layer 2)
    - "/path/to/ext.ts"
  no_session: true                    # --no-session flag

default_executor: "opencode"          # DEFAULT_EXECUTOR (opencode/pi)

task_defaults:                        # Defaults for all tasks
  agent: "tdd-dev"
  model: "anthropic/claude-sonnet-4-20250514"
  executor: "pi"
  execution_mode: "worktree"
  merge_policy: "auto_pr"
  merge_strategy: "squash"
  merge_target_branch: "main"
  remote_branch_policy: "delete"
  target_workdir: "/path/to/work"
```

**CLI flags:**
- `--executor <name>` - Override default executor (opencode/pi)
- `--pi-bin <path>` - Path to pi binary
- `--pi-model <model>` - Model for Pi executor
- `--pi-thinking <level>` - Thinking level for Pi

#### Idle Detection

Two mechanisms based on executor type:
- **OpenCode**: HTTP polling via `/session/status` endpoint. Empty response = idle.
- **Pi**: Process exit detection. A running Pi RPC process is always "busy". Completion is detected when the process exits (handled by `checkRunningTasks` → `CheckCompletion`).

Mixed workloads (OpenCode + Pi tasks running simultaneously) are supported with correct per-task routing.

### Shared Utilities (`pkg/`)
- `frontmatter/` - YAML frontmatter parsing
- `markdown/` - Markdown processing utilities

## Testing Patterns

Tests use Go's built-in testing framework:

```go
// Example test pattern
func TestTaskService_GetNext(t *testing.T) {
    // Arrange
    service := setupTestService(t)
    
    // Act
    task, err := service.GetNext("project-id")
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if task == nil {
        t.Fatal("expected task, got nil")
    }
}

// Table-driven tests
func TestDependencyResolution(t *testing.T) {
    tests := []struct {
        name     string
        input    []Task
        expected []string
    }{
        // test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

## Common Tasks

### Adding API Endpoints
1. Add handler in `internal/api/*.go`
2. Add test in same package or `_test.go` file
3. Update API client in `internal/runner/api_client.go`
4. Register route in `cmd/brain-api/main.go` or route initialization

## File Conventions

- Tests: `*_test.go` alongside source files
- Internal packages: `internal/` (not importable by external projects)
- Exported packages: `pkg/` (can be imported)
- Commands: `cmd/<binary-name>/main.go`
- Entry points: `main.go` in each `cmd/` subdirectory

## Multi-Project Mode

The task runner supports monitoring multiple projects simultaneously with a shared execution pool.

### Basic Usage

```bash
# Monitor all projects
brain run start all

# Filter with glob patterns
brain run start all --include 'prod-*' --exclude 'prod-legacy'
brain run start all -i 'brain-*' -e 'test-*'

# List all available projects
curl http://localhost:3333/api/v1/tasks | jq '.projects'
```

### Architecture

- **Shared execution pool**: `--max-parallel` applies across ALL projects
- **Real-time updates**: All projects stream task updates via SSE
- **Composite task keys**: Tasks tracked as `projectId:taskId` internally

### Key Components

```
Task Runner Multi-Project Architecture

TaskRunner
├── projects: []string              # List of projects to poll
├── isMultiProject: bool            # Enables multi-project behavior
└── Shared ProcessManager           # Single pool for all projects
```

### Filter Examples

```bash
# Only production projects
brain run start all -i 'prod-*'

# All except test projects
./bin/brain-runner start all -e 'test-*' -e '*-staging'

# Brain projects except legacy
brain run start all -i 'brain-*' -e 'brain-legacy'
```

## Build System

The project uses `just` (justfile) for all task automation:

```bash
just              # List all recipes
just build        # Build all binaries
just test         # Run tests
just test-cover   # Run tests with coverage
just lint         # Run golangci-lint
just vet          # Run go vet
just check        # Run all checks (vet + test + lint)
just clean        # Clean build artifacts
just dev          # Run brain-api server
just install      # Install binaries to GOPATH/bin
just release      # Cross-compile for release
just docker       # Build Docker image
```

## Go Module

Module path: `github.com/huynle/brain-api`

Use standard Go commands:
```bash
go mod tidy       # Clean up dependencies
go mod download   # Download dependencies
go build ./...    # Build all packages
go test ./...     # Test all packages
```
