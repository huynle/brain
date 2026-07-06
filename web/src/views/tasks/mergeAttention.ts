// Merge-attention: derive "this feature just finished and probably needs
// merging" client-side, without server merge-state.
//
// A feature is merge-ready when:
//   - every non-automation-generated task is in {completed, validated}
//     (mirrors the server's feature.completed semantics), AND
//   - it carries merge configuration: a merge_target_branch, or a
//     merge_policy in {auto_pr, prompt_only} together with a git_branch —
//     auto_merge is excluded (the runner merges those itself), AND
//   - the newest completion is within the recency window (stale features
//     shouldn't nag forever).

import type { Task } from "../../lib/types";

export const MERGE_ATTENTION_WINDOW_MS = 14 * 24 * 60 * 60 * 1000;

const DONE = new Set(["completed", "validated"]);

function isAutomationTask(t: Task): boolean {
  return (t.generated_by ?? "").startsWith("automation:");
}

function completionTime(t: Task): number {
  const v = t.completed_at || t.modified || "";
  const n = Date.parse(v);
  return Number.isNaN(n) ? 0 : n;
}

function hasMergeConfig(t: Task): boolean {
  if (t.merge_policy === "auto_merge") return false;
  if (t.merge_target_branch) return true;
  if ((t.merge_policy === "auto_pr" || t.merge_policy === "prompt_only") && t.git_branch) return true;
  return false;
}

export interface MergeAttention {
  feature: string;
  /** Newest completion time (ms epoch) across the feature's tasks. */
  completedAt: number;
  taskCount: number;
}

/**
 * mergeReadyFeatures scans tasks (any statuses) grouped by feature_id and
 * returns the features currently deserving merge attention, newest first.
 */
export function mergeReadyFeatures(tasks: Task[], now = Date.now()): MergeAttention[] {
  const byFeature = new Map<string, Task[]>();
  for (const t of tasks) {
    if (!t.feature_id) continue;
    const arr = byFeature.get(t.feature_id);
    if (arr) arr.push(t);
    else byFeature.set(t.feature_id, [t]);
  }

  const out: MergeAttention[] = [];
  for (const [feature, ts] of byFeature) {
    const relevant = ts.filter((t) => !isAutomationTask(t));
    if (relevant.length === 0) continue;
    if (!relevant.every((t) => DONE.has(t.status))) continue;
    if (!relevant.some(hasMergeConfig)) continue;
    const completedAt = Math.max(...relevant.map(completionTime));
    if (completedAt === 0 || now - completedAt > MERGE_ATTENTION_WINDOW_MS) continue;
    out.push({ feature, completedAt, taskCount: relevant.length });
  }
  out.sort((a, b) => b.completedAt - a.completedAt);
  return out;
}
