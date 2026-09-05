/**
 * lib/features — pure feature-lifecycle derivation.
 *
 * A "feature" is an emergent grouping of tasks that share the same
 * `feature_id`. The Brain API does not have a first-class features
 * endpoint — everything is derived from the task list. Phase 5
 * formalizes the rollup so the UI can render lifecycle chips, sort
 * features consistently, and link to open MRs.
 *
 * This module is intentionally pure: no react, no hooks, no fetch.
 * Trivially unit-tested from `features.test.ts`.
 *
 * ─── Rollup rules ────────────────────────────────────────────────
 *
 *   validated      — every task is done AND every non-generated task
 *                    carries status "validated" — the checkout agent's
 *                    verdict that the work is verified. See "Why the
 *                    denominator excludes generated tasks" below.
 *   ready-to-merge — the feature is otherwise `finished` AND an open
 *                    Brain-native `merge_request` ENTRY names it. The
 *                    checkout agent validated the work and parked a merge
 *                    intent for deterministic merge execution. See
 *                    `lib/mergeRequests`.
 *   finished       — 100% of tasks are in {completed, validated}.
 *   blocked        — any task is status "blocked" AND no task is
 *                    "pending" or "in_progress". A pending sibling means
 *                    work is still active → in-progress wins.
 *   in-progress    — default. Any task in {pending, in_progress} lands
 *                    here.
 *
 * Priority (highest wins):
 *   validated > finished > blocked > in-progress
 *
 * ...with `ready-to-merge` layered on top: the fold in `deriveFeatures`
 * promotes EITHER terminal work state (`finished` or `validated`) when an
 * open merge_request entry names the feature, because an outstanding merge
 * is more actionable than the verdict on the work. When that entry reaches
 * a terminal status it drops out of `openMRs` on its own and the feature
 * falls back to the state its tasks earned.
 *
 * ─── Why this is `validated` and not `merged` ──────────────────────
 *
 * It was called `merged` until 2026-09-05, and it never meant that. The
 * predicate is a task-status check, and `validated` is the status enum's
 * own word for "Implementation verified working" (internal/types/types.go).
 * Whether a branch actually landed is a DIFFERENT fact, and one nothing in
 * this system observes: the only code that performs a real merge is the
 * simple feature-checkout script, which is the non-default checkout mode
 * and runs under the script executor (off unless `script.enabled`). Naming
 * a chip after a fact no signal supports is what produced the `mr-open`
 * bug above; a real MERGED chip has to wait for a real merge receipt.
 *
 * ─── Why the denominator excludes generated tasks ──────────────────
 *
 * A feature's checkout task is a member of its own feature — the automation
 * stamps the event's feature id onto it — but the checkout skill validates
 * only the tasks it DEPENDS ON and then sets ITSELF to `completed`.
 * Counting it made `validated === total` structurally false for every
 * AI-checked-out feature, so the terminal state was unreachable and its
 * lane sat permanently empty. Worse, the skill spawns a fresh checkout task
 * per round, so the shortfall grew.
 *
 * Excluding `generated` tasks matches what the server already believes:
 * `extractUniqueNonGeneratedTaskIds` builds the checkout's own
 * `depends_on` from exactly the non-generated set.
 *
 * `allDone` deliberately keeps the FULL denominator. A feature whose
 * checkout task is still pending is not finished, and must not jump
 * straight to `validated` on the strength of its work tasks alone.
 *
 * ─── Why there is no `mr-open` lifecycle ──────────────────────────
 *
 * There was one until 2026-09-05, and it was the largest single source of
 * wrong chips in the PWA. A forge URL is an ATTACHMENT, not a state:
 *
 *   1. It was tested BEFORE `allDone` and before `blocked`, so a feature
 *      with three blocked tasks and a GitHub link sitting anywhere in any
 *      task's markdown body rendered "MR OPEN". The URL painted over the
 *      work state instead of describing it.
 *   2. Nothing in this system ever contacts a git server — no forge SDK,
 *      no `gh`/`glab` call, no webhook reading merge state. The URL is
 *      rendered as an href and never fetched, so the state could be
 *      ENTERED but never EXITED: a long-merged PR kept the chip lit
 *      forever, and the tooltip claimed a live merge request either way.
 *   3. `task.mr_url` and `task.merge_request_url` are never populated by
 *      the server (zero occurrences across the Go tree), so in practice
 *      the state was driven entirely by `extractPrUrl`'s regex scan of
 *      task prose — a weaker signal than every lifecycle it outranked.
 *
 * `prUrl` is still extracted and still exposed on DerivedFeature. It is
 * now rendered as a separate `MergeRequestLink` chip riding alongside
 * whatever lifecycle is actually true, and it claims only what the system
 * knows: "a task in this feature mentions this URL".
 *
 * ─── PR URL extraction ─────────────────────────────────────────────
 *
 *   1. `task.mr_url` if set
 *   2. `task.merge_request_url` (compat alias) if set
 *   3. Regex scan of `task.content` for the first GitLab or GitHub
 *      merge-request URL
 *   4. undefined
 *
 * Tasks without a `feature_id` (undefined or empty string) are
 * skipped entirely — the caller's project-level views handle those.
 *
 * Archived tasks are likewise skipped, mirroring the server's stats
 * rule: they count toward nothing — not totals, not progress, not
 * lifecycle, not `ownerTaskIds`. A feature whose tasks are ALL
 * archived therefore derives no feature at all and leaves the lanes.
 */
import { buildDepForest, type DepNode } from "./depTree";
import type { Task } from "./types";

export type FeatureLifecycle =
  | "in-progress"
  | "blocked"
  | "finished"
  | "ready-to-merge"
  | "validated";

export interface DerivedFeature {
  /** The shared `feature_id` value. */
  id: string;
  /** The project this feature lives under. Injected by the caller so
   *  downstream views (drag-drop, modals) can address the tasks. */
  projectId: string;
  /** Human-visible name. Currently identical to `id`; a future task
   *  may source this from a feature entry's title. */
  name: string;
  /** Fraction 0..1 of tasks in {completed, validated}. */
  progress: number;
  lifecycle: FeatureLifecycle;
  taskCount: {
    total: number;
    completed: number; // completed + validated
    blocked: number;
    active: number; // pending + in_progress
  };
  /** Copied from the first task with a non-empty `merge_policy`.
   *  Features whose tasks disagree pick the first — call sites treat
   *  this as an advisory hint, not a source of truth. */
  mergePolicy?: string;
  prUrl?: string;
  /** Reserved for a real merge receipt, and deliberately still unassigned.
   *  Nothing in Brain observes a branch landing: the only code that performs
   *  a merge is the simple feature-checkout script, which is the non-default
   *  checkout mode and runs under the script executor (off unless
   *  `script.enabled`). Until one of those emits a receipt this stays
   *  undefined, and `validated` makes the weaker, checkable claim instead. */
  mergedAt?: string;
  finishedAt?: string;
  /** Every task id belonging to this feature. Preserved in input
   *  order so drag-drop assignment in a later phase can address a
   *  stable list. */
  ownerTaskIds: string[];
  /** Number of tasks in this feature currently flagged is_abandoned
   *  by the server (offline-runner claim, expired lease, orphan
   *  reaped). Read by lib/actions/featureActions + FeatureActionsModal
   *  to surface the Resume affordance. Zero when nothing is resumable. */
  resumableCount: number;
  /** Feature ids this feature depends on, from the first task in the
   *  group carrying a non-empty `feature_depends_on`.
   *
   *  `feature_depends_on` is a feature-level field replicated onto
   *  every task's frontmatter, so in a well-formed feature all tasks
   *  agree and "first non-empty" is exact. When they disagree (a task
   *  edited in isolation) we take the first rather than unioning: the
   *  server reads the feature's first task for this field, so unioning
   *  would draw a tree the backend does not agree with.
   *  Empty when the feature has no declared dependencies. */
  dependsOn: string[];
}

/**
 * Group tasks by `feature_id` and roll each group up into a
 * DerivedFeature. See module docstring for the rollup rules.
 *
 * Callers that want the canonical display order should pipe the
 * result through {@link sortFeatures}; deriveFeatures itself does
 * not sort so tests can assert grouping and ordering independently.
 */
export function deriveFeatures(
  tasks: readonly Task[],
  projectId: string,
  /**
   * Feature ids with an open Brain-native merge_request entry (see
   * `lib/mergeRequests`). An `auto_pr` checkout produces such an entry
   * rather than a GitHub/GitLab URL, so without this input a reviewed
   * feature stayed "in-progress" forever and the READY TO MERGE column
   * never moved. Optional: callers that don't display lifecycle can omit
   * it — they simply never see `ready-to-merge`.
   */
  openMRs?: ReadonlySet<string>,
): DerivedFeature[] {
  // Bucketed collector keyed by feature_id. Preserves insertion
  // order so the result is deterministic on identical input, which
  // makes tests and diff-based UI updates simpler.
  const groups = new Map<string, Task[]>();
  for (const t of tasks) {
    const fid = t.feature_id;
    if (!fid) continue; // filter out unassigned tasks
    if (t.status === "archived") continue; // archived counts toward nothing
    let bucket = groups.get(fid);
    if (!bucket) {
      bucket = [];
      groups.set(fid, bucket);
    }
    bucket.push(t);
  }

  const out: DerivedFeature[] = [];
  for (const [fid, bucketTasks] of groups) {
    const f = deriveOne(fid, bucketTasks, projectId);
    // Brain-native MR fold — deliberately narrow: it upgrades `finished`
    // and nothing else.
    //
    // "Ready to merge" is a claim about the WORK, not just about the
    // existence of an entry: it says every task is done and only the merge
    // is outstanding. So a feature that still has a blocked or running
    // task keeps the lifecycle its tasks earned, even with an open MR
    // entry sitting on it — a follow-up task added after checkout must not
    // be papered over by a badge announcing the feature is ready to go.
    // (The earlier "anything but merged" fold did paper over exactly that.)
    //
    // BOTH terminal work states are promoted, and the distinction that used
    // to justify excluding one of them does not exist: `openMRFeatureIds`
    // already filters to OPEN entries (pending/active/in_progress), so an
    // entry reaching here is by construction not stale. Excluding
    // `validated` was safe only while that state was unreachable — the
    // moment it became reachable it captured exactly the shape checkout
    // leaves behind (work validated, generated checkout task completed),
    // so the READY TO MERGE lane emptied and features with a merge still
    // parked were folded away as done by `isFeatureDone`.
    if (
      (f.lifecycle === "finished" || f.lifecycle === "validated") &&
      openMRs?.has(fid)
    ) {
      f.lifecycle = "ready-to-merge";
    }
    out.push(f);
  }
  return out;
}

function deriveOne(
  featureId: string,
  tasks: readonly Task[],
  projectId: string,
): DerivedFeature {
  let total = 0;
  let completed = 0; // completed + validated
  // The `work*` pair excludes generated tasks (the checkout task and any
  // other automation-authored sibling). See the module docstring: counting
  // them made the terminal lifecycle unreachable.
  let workTotal = 0;
  let workValidated = 0;
  let blocked = 0;
  let active = 0; // pending + in_progress
  let resumable = 0;
  let prUrl: string | undefined;
  let mergePolicy: string | undefined;
  let dependsOn: string[] | undefined;
  const ownerTaskIds: string[] = [];

  for (const t of tasks) {
    total++;
    ownerTaskIds.push(t.id);
    const isWork = t.generated !== true;
    if (isWork) workTotal++;
    switch (t.status) {
      case "completed":
        completed++;
        break;
      case "validated":
        completed++;
        if (isWork) workValidated++;
        break;
      case "blocked":
        blocked++;
        break;
      case "pending":
      case "in_progress":
      case "active":
        active++;
        break;
    }
    if (t.is_abandoned) resumable++;
    if (!prUrl) {
      const url = extractPrUrl(t);
      if (url) prUrl = url;
    }
    if (!mergePolicy && t.merge_policy) mergePolicy = t.merge_policy;
    if (!dependsOn && t.feature_depends_on && t.feature_depends_on.length > 0) {
      // Drop self-references and blanks here rather than in the tree
      // builder, so every consumer of `dependsOn` sees clean data.
      const clean = t.feature_depends_on.filter((d) => !!d && d !== featureId);
      if (clean.length > 0) dependsOn = clean;
    }
  }

  const progress = total > 0 ? completed / total : 0;
  const allDone = total > 0 && completed === total;
  // Note the conjunction with allDone: every task must be finished (the full
  // denominator, generated ones included) AND every work task must carry the
  // verified stamp. Without it a feature whose checkout task is still pending
  // would skip straight past `finished`.
  const allValidated =
    allDone && workTotal > 0 && workValidated === workTotal;

  // Priority: validated > finished > blocked > in-progress.
  // `ready-to-merge` is not decidable here — it comes from the project's
  // merge_request entries, which only deriveFeatures has — so the fold
  // above promotes `finished` to it after the fact.
  //
  // `prUrl` is deliberately NOT consulted. A forge URL describes an
  // artifact attached to the feature, never the state of its work; when it
  // was a lifecycle it outranked both `allDone` and `blocked` on the
  // strength of a regex over task prose. See the module docstring.
  // We evaluate top-down and land on the first match.
  let lifecycle: FeatureLifecycle;
  if (allValidated) {
    lifecycle = "validated";
  } else if (allDone) {
    lifecycle = "finished";
  } else if (blocked > 0 && active === 0) {
    lifecycle = "blocked";
  } else {
    lifecycle = "in-progress";
  }

  return {
    id: featureId,
    projectId,
    name: featureId,
    progress,
    lifecycle,
    taskCount: { total, completed, blocked, active },
    mergePolicy,
    prUrl,
    ownerTaskIds,
    resumableCount: resumable,
    dependsOn: dependsOn ?? [],
  };
}

/**
 * A feature whose work is over — there is nothing left in it to act on.
 *
 * Drives the default collapse state of a feature's task list: a finished
 * or validated feature is history, so its rows start folded and the feature
 * reads as one line. `ready-to-merge` is deliberately NOT done — it is
 * still waiting on the merge executor.
 */
export const isFeatureDone = (f: DerivedFeature): boolean =>
  f.lifecycle === "finished" || f.lifecycle === "validated";

/**
 * Sort features into the canonical panes-v2 display order:
 *
 *   blocked → in-progress → ready-to-merge → finished → validated
 *
 * `blocked` sits at the top because it demands attention; `validated`
 * goes last because the UI collapses that bucket by default. Within
 * a bucket, features sort alphabetically by id for stability.
 *
 * A feature carrying a forge URL sorts on its own work state; the URL is
 * a link chip, not a rank. See the module docstring.
 */
export function sortFeatures(feats: DerivedFeature[]): DerivedFeature[] {
  const rank: Record<FeatureLifecycle, number> = {
    blocked: 0,
    "in-progress": 1,
    "ready-to-merge": 2,
    finished: 3,
    validated: 4,
  };
  // Copy first so callers keep their input untouched.
  return [...feats].sort((a, b) => {
    const dr = rank[a.lifecycle] - rank[b.lifecycle];
    if (dr !== 0) return dr;
    return a.id.localeCompare(b.id);
  });
}

/**
 * Build the feature dependency forest from `feature_depends_on`.
 *
 * Root ordering follows the caller's array order, so piping through
 * {@link sortFeatures} first keeps the canonical blocked → in-progress
 * → ready-to-merge → finished → validated sequence at every level of
 * the tree.
 *
 * Features whose dependency is not in the input (a merged feature the
 * user has collapsed, or one that lives in another project) stay roots
 * — the tree never hides a feature because its parent was filtered out.
 */
export function buildFeatureForest(
  features: readonly DerivedFeature[],
): DepNode<DerivedFeature>[] {
  return buildDepForest(features, {
    id: (f) => f.id,
    deps: (f) => f.dependsOn,
  });
}

// Prebuilt regexes so hot paths don't recompile per call. Both are
// intentionally permissive on the host portion so self-hosted GitLab
// instances (any hostname starting with `gitlab`) work; GitHub is
// pinned to github.com since GHE hostnames vary.
const GITLAB_MR_RE = /https:\/\/gitlab[^\s]*\/-\/merge_requests\/\d+/;
const GITHUB_PR_RE = /https:\/\/github\.com\/[^\s]+\/pull\/\d+/;

/**
 * Extract a merge-request or pull-request URL from a task. See
 * module docstring for precedence rules.
 */
export function extractPrUrl(task: Task): string | undefined {
  if (task.mr_url) return task.mr_url;
  if (task.merge_request_url) return task.merge_request_url;
  const content = task.content;
  if (!content) return undefined;
  const gl = GITLAB_MR_RE.exec(content);
  if (gl) return gl[0];
  const gh = GITHUB_PR_RE.exec(content);
  if (gh) return gh[0];
  return undefined;
}
