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
 *   merged         — every task in the feature has status "validated".
 *                    (Future signal: `mergedAt` timestamp; not wired yet.)
 *   mr-open        — a merge request exists ON A GIT SERVER: some task
 *                    carries an MR/PR URL, and not all tasks are
 *                    validated. This is the only lifecycle with a URL to
 *                    open, and callers render the badge as a link.
 *   ready-to-merge — the feature is otherwise `finished` AND an open
 *                    Brain-native `merge_request` ENTRY names it, with no
 *                    forge URL anywhere in it. The checkout agent validated
 *                    the work and parked a merge intent for deterministic
 *                    merge execution — nothing has been opened on a git
 *                    server, and there is nothing to click. See
 *                    `lib/mergeRequests`.
 *   finished       — 100% of tasks are in {completed, validated} AND no
 *                    MR signal of either kind is present.
 *   blocked        — any task is status "blocked" AND no task is
 *                    "pending" or "in_progress". A pending sibling means
 *                    work is still active → in-progress wins.
 *   in-progress    — default. Any task in {pending, in_progress} lands
 *                    here.
 *
 * Priority (highest wins):
 *   merged > mr-open > ready-to-merge > finished > blocked > in-progress.
 *
 * The two MR states were ONE state (`mr-open`) until 2026-09-03, which
 * made the badge a lie half the time: an `auto_pr` checkout produces a
 * Brain entry, not a forge PR, so features with nothing on the git server
 * still read "MR OPEN" and offered no link to follow. Keep them distinct.
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
  | "mr-open"
  | "ready-to-merge"
  | "merged";

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
    // The two states this used to guard against are excluded for free:
    // `merged` (all tasks validated — done, whatever stale entry remains)
    // and `mr-open` (a real forge URL, strictly more actionable, and the
    // only one of the two the user can click through to).
    if (f.lifecycle === "finished" && openMRs?.has(fid)) {
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
  let validated = 0;
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
    switch (t.status) {
      case "completed":
        completed++;
        break;
      case "validated":
        completed++;
        validated++;
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
  const allValidated = total > 0 && validated === total;
  const allDone = total > 0 && completed === total;

  // Priority: merged > mr-open > finished > blocked > in-progress.
  // `ready-to-merge` is not decidable here — it comes from the project's
  // merge_request entries, which only deriveFeatures has — so the fold
  // above promotes `finished` to it after the fact.
  // We evaluate top-down and land on the first match.
  let lifecycle: FeatureLifecycle;
  if (allValidated) {
    lifecycle = "merged";
  } else if (prUrl) {
    lifecycle = "mr-open";
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
 * or merged feature is history, so its rows start folded and the feature
 * reads as one line. `mr-open` and `ready-to-merge` are deliberately NOT
 * done — both are waiting on someone: a reviewer on the git server, or
 * the merge executor.
 */
export const isFeatureDone = (f: DerivedFeature): boolean =>
  f.lifecycle === "finished" || f.lifecycle === "merged";

/**
 * Sort features into the canonical panes-v2 display order:
 *
 *   blocked → in-progress → mr-open → ready-to-merge → finished → merged
 *
 * `blocked` sits at the top because it demands attention; `merged`
 * goes last because the UI collapses that bucket by default. Within
 * a bucket, features sort alphabetically by id for stability.
 *
 * `mr-open` outranks `ready-to-merge` because it is the one waiting on a
 * person — a parked merge intent is waiting on the merge executor.
 */
export function sortFeatures(feats: DerivedFeature[]): DerivedFeature[] {
  const rank: Record<FeatureLifecycle, number> = {
    blocked: 0,
    "in-progress": 1,
    "mr-open": 2,
    "ready-to-merge": 3,
    finished: 4,
    merged: 5,
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
 * → mr-open → ready-to-merge → finished → merged sequence at every level
 * of the tree.
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
