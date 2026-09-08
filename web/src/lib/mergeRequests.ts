/**
 * lib/mergeRequests — pure helpers for Brain-native merge requests.
 *
 * An `auto_pr` / `auto_merge` feature checkout produces a Brain ENTRY of
 * type `merge_request` — not a GitHub/GitLab PR. Until now the web UI's
 * feature lifecycle keyed exclusively off `task.mr_url` / a URL regex over
 * task content, so a successful checkout left the feature stuck showing
 * "active". This module is the missing link: given the project's
 * merge_request entries, which features have an open MR?
 *
 * What that yields is the `ready-to-merge` lifecycle: an entry here means
 * the work is validated and a merge intent is parked in Brain, with nothing
 * opened on any git server and no url for the user to follow.
 *
 * A real forge MR is NOT a lifecycle at all — it is a url rendered as a
 * separate `MergeRequestLink` chip beside whatever badge is true. See
 * `lib/features` for why that state was removed.
 *
 * Feature attribution is defensive by necessity. The feature-checkout
 * skill instructs the agent to set a structured `feature_id` on the entry,
 * but the entry is written by an LLM and observed drift is real — the
 * first live sonnet run set no structured fields at all. So we read, in
 * order of trust:
 *
 *   1. the structured `feature_id` field (the contract)
 *   2. a `- feature_id: <x>` line inside the "## Brain Merge Request"
 *      content block (the skill's required content shape)
 *   3. the mandated title "Merge request: <source> -> <target>", where the
 *      source branch equals the feature id by runner convention
 *
 * Pure: no react, no fetch. Tested from `mergeRequests.test.ts`.
 */
import type { BrainEntry } from "./types";

/** Entry statuses under which a merge request counts as OPEN.
 *
 * The skill's contract is `pending` ("leave the merge request in pending
 * for deterministic merge execution"). `active` and `in_progress` are
 * included defensively — an MR being processed is still open. Terminal
 * statuses (completed/validated = merged, cancelled/superseded/archived =
 * abandoned) and `draft` (not yet real) are not open.
 */
const OPEN_STATUSES: ReadonlySet<string> = new Set([
  "pending",
  "active",
  "in_progress",
]);

const CONTENT_FEATURE_RE = /^\s*-\s*feature_id:\s*(\S+)\s*$/m;
const TITLE_RE = /^Merge request:\s*(\S+)\s*->/;

/**
 * Best-effort feature id for a merge_request entry, or "" when nothing
 * usable is present. See module docstring for the trust order.
 */
export function mrFeatureId(entry: BrainEntry): string {
  if (entry.feature_id) return entry.feature_id;

  const m = CONTENT_FEATURE_RE.exec(entry.content || "");
  if (m) return m[1];

  const t = TITLE_RE.exec(entry.title || "");
  if (t) return t[1];

  return "";
}

/** True when the entry represents a merge request that is still open. */
export function isOpenMergeRequest(entry: BrainEntry): boolean {
  return OPEN_STATUSES.has(entry.status || "");
}

/**
 * Fold a project's merge_request entries into the set of feature ids that
 * currently have an open MR. Entries whose feature cannot be attributed
 * are dropped — an unattributable MR must not mark a random feature.
 */
export function openMRFeatureIds(
  entries: readonly BrainEntry[],
): Set<string> {
  const out = new Set<string>();
  for (const e of entries) {
    if (!isOpenMergeRequest(e)) continue;
    const fid = mrFeatureId(e);
    if (fid) out.add(fid);
  }
  return out;
}
