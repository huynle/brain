/**
 * lib/taskGroups — the groups on a project card that are NOT features.
 *
 * Two of them hold real tasks and answer to no `feature_id`: the
 * ungrouped bucket in the Tasks tab, and each bucket inside the Archived
 * tab. Their keys and their bucketing live here rather than in a
 * component so both tabs read one definition — and so the keys, which are
 * written into PERSISTED fold state, cannot drift apart between two
 * surfaces that must agree on them.
 */
import { buildTaskForest } from "./taskTree";
import { flattenDepForest } from "./depTree";
import type { Task } from "./types";

export const NO_FEATURE = "__nofeat__";
export const archivedKey = (featureId: string) => `__archived__:${featureId}`;

/**
 * Bucket archived tasks by feature, preserving dependency order inside
 * each bucket.
 *
 * `deriveFeatures` skips archived tasks, so an all-archived feature has
 * no DerivedFeature to hang rows under — but the raw `feature_id`
 * survives archiving, and that is all a header needs. Real features
 * first in id order, the ungrouped bucket last, matching the live list.
 */
export function bucketArchived(archivedTasks: readonly Task[]) {
  const buckets = new Map<string, Task[]>();
  for (const t of archivedTasks) {
    const key = t.feature_id ?? NO_FEATURE;
    const arr = buckets.get(key);
    if (arr) arr.push(t);
    else buckets.set(key, [t]);
  }
  const keys = [...buckets.keys()]
    .filter((k) => k !== NO_FEATURE)
    .sort((a, b) => a.localeCompare(b));
  if (buckets.has(NO_FEATURE)) keys.push(NO_FEATURE);
  return keys.map((key) => {
    const bucket = buckets.get(key)!;
    return {
      key,
      label: key === NO_FEATURE ? "No feature" : key,
      tasks: bucket,
      rows: flattenDepForest(buildTaskForest(bucket)),
    };
  });
}
