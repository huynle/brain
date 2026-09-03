/**
 * Tests for lib/features — pure feature-lifecycle derivation.
 *
 * A feature is an emergent grouping of tasks that share the same
 * `feature_id`. `deriveFeatures` folds a task list into one row per
 * feature with:
 *   - progress (completed / total, 0..1)
 *   - lifecycle: in-progress | blocked | finished | mr-open |
 *                ready-to-merge | merged
 *   - PR URL (from task.mr_url, task.merge_request_url, or a regex
 *     scan of task.content)
 *   - ownerTaskIds so downstream drag-drop assignment can address
 *     the concrete tasks
 *
 * These tests pin the exact rollup rules from the Phase 5 plan — the
 * production module reads from them, not from prose.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  deriveFeatures,
  isFeatureDone,
  sortFeatures,
  extractPrUrl,
  buildFeatureForest,
  type DerivedFeature,
} from "./features";
import type { Task } from "./types";

/** Minimal builder — everything unset is a benign default. Tests
 *  override the fields they actually care about. */
function mkTask(over: Partial<Task> = {}): Task {
  return {
    id: over.id ?? "t",
    path: "",
    title: "",
    priority: "medium",
    status: "pending",
    ...over,
  };
}

// ─── deriveFeatures: lifecycle rollups ─────────────────────────────

test("deriveFeatures: empty task list → empty array", () => {
  const out = deriveFeatures([], "proj");
  assert.deepEqual(out, []);
});

test("deriveFeatures: all completed, no PR → finished", () => {
  const tasks: Task[] = [
    mkTask({ id: "1", status: "completed", feature_id: "auth" }),
    mkTask({ id: "2", status: "completed", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out.length, 1);
  assert.equal(out[0]!.lifecycle, "finished");
  assert.equal(out[0]!.progress, 1);
  assert.equal(out[0]!.prUrl, undefined);
});

test("deriveFeatures: all validated → merged", () => {
  const tasks: Task[] = [
    mkTask({ id: "1", status: "validated", feature_id: "auth" }),
    mkTask({ id: "2", status: "validated", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "merged");
});

test("deriveFeatures: one in_progress + rest completed → in-progress", () => {
  const tasks: Task[] = [
    mkTask({ id: "1", status: "completed", feature_id: "auth" }),
    mkTask({ id: "2", status: "in_progress", feature_id: "auth" }),
    mkTask({ id: "3", status: "completed", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "in-progress");
  assert.equal(out[0]!.taskCount.total, 3);
  assert.equal(out[0]!.taskCount.completed, 2);
  assert.equal(out[0]!.taskCount.active, 1);
});

test("deriveFeatures: any blocked, no pending/in_progress → blocked", () => {
  const tasks: Task[] = [
    mkTask({ id: "1", status: "blocked", feature_id: "auth" }),
    mkTask({ id: "2", status: "completed", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "blocked");
  assert.equal(out[0]!.taskCount.blocked, 1);
});

test("deriveFeatures: blocked + pending → in-progress (pending wins)", () => {
  // The rollup rule pins blocked to *no* task pending/in_progress.
  // A pending sibling means work is still active.
  const tasks: Task[] = [
    mkTask({ id: "1", status: "blocked", feature_id: "auth" }),
    mkTask({ id: "2", status: "pending", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "in-progress");
});

test("deriveFeatures: GitLab MR URL in task.content, not all validated → mr-open", () => {
  const tasks: Task[] = [
    mkTask({
      id: "1",
      status: "completed",
      feature_id: "auth",
      content:
        "Look at https://gitlab.example.com/group/proj/-/merge_requests/42 for the change.",
    }),
    mkTask({ id: "2", status: "in_progress", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "mr-open");
  assert.ok(out[0]!.prUrl!.includes("gitlab.example.com"));
  assert.ok(out[0]!.prUrl!.includes("/-/merge_requests/42"));
});

test("deriveFeatures: GitHub PR URL in task.content, not all validated → mr-open", () => {
  const tasks: Task[] = [
    mkTask({
      id: "1",
      status: "completed",
      feature_id: "auth",
      content: "PR up at https://github.com/acme/foo/pull/123 — please review.",
    }),
    mkTask({ id: "2", status: "pending", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "mr-open");
  assert.equal(out[0]!.prUrl, "https://github.com/acme/foo/pull/123");
});

test("extractPrUrl: task.mr_url takes precedence over content regex", () => {
  const t = mkTask({
    id: "1",
    // Priority field — should win over the fallback URL in content.
    mr_url: "https://gitlab.example.com/group/proj/-/merge_requests/7",
    content: "old link https://github.com/acme/foo/pull/999",
  });
  assert.equal(
    extractPrUrl(t),
    "https://gitlab.example.com/group/proj/-/merge_requests/7",
  );
});

test("extractPrUrl: merge_request_url compat alias works", () => {
  const t = mkTask({
    id: "1",
    merge_request_url:
      "https://gitlab.example.com/group/proj/-/merge_requests/9",
  });
  assert.equal(
    extractPrUrl(t),
    "https://gitlab.example.com/group/proj/-/merge_requests/9",
  );
});

test("extractPrUrl: no URL anywhere → undefined", () => {
  assert.equal(extractPrUrl(mkTask({ id: "1" })), undefined);
  assert.equal(
    extractPrUrl(mkTask({ id: "1", content: "no url here at all" })),
    undefined,
  );
});

test("deriveFeatures: PR URL present + all validated → merged (merged beats mr-open)", () => {
  const tasks: Task[] = [
    mkTask({
      id: "1",
      status: "validated",
      feature_id: "auth",
      mr_url: "https://github.com/acme/foo/pull/1",
    }),
    mkTask({ id: "2", status: "validated", feature_id: "auth" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out[0]!.lifecycle, "merged");
  // prUrl is still surfaced so the modal can link to the merged MR.
  assert.equal(out[0]!.prUrl, "https://github.com/acme/foo/pull/1");
});

test("deriveFeatures: multi-feature project → separate rows per feature_id", () => {
  const tasks: Task[] = [
    mkTask({ id: "1", status: "completed", feature_id: "auth" }),
    mkTask({ id: "2", status: "pending", feature_id: "auth" }),
    mkTask({ id: "3", status: "validated", feature_id: "ui" }),
    mkTask({ id: "4", status: "in_progress", feature_id: "storage" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out.length, 3);
  const byId = Object.fromEntries(out.map((f) => [f.id, f]));
  assert.equal(byId.auth!.lifecycle, "in-progress");
  assert.equal(byId.ui!.lifecycle, "merged");
  assert.equal(byId.storage!.lifecycle, "in-progress");
});

test("deriveFeatures: progress math 0/3, 1/3, 2/3, 3/3", () => {
  const p = (statuses: Task["status"][]) => {
    const tasks = statuses.map((s, i) =>
      mkTask({ id: `t${i}`, status: s, feature_id: "f" }),
    );
    const out = deriveFeatures(tasks, "proj");
    return out[0]!.progress;
  };
  assert.equal(p(["pending", "pending", "pending"]), 0);
  // completed counts toward progress, so does validated
  assert.equal(Math.round(p(["completed", "pending", "pending"]) * 1000), 333);
  assert.equal(Math.round(p(["completed", "validated", "pending"]) * 1000), 667);
  assert.equal(p(["completed", "completed", "validated"]), 1);
});

test("deriveFeatures: ownerTaskIds contains every task in the feature", () => {
  const tasks: Task[] = [
    mkTask({ id: "a1", status: "pending", feature_id: "auth" }),
    mkTask({ id: "a2", status: "completed", feature_id: "auth" }),
    mkTask({ id: "u1", status: "pending", feature_id: "ui" }),
  ];
  const out = deriveFeatures(tasks, "proj");
  const auth = out.find((f) => f.id === "auth")!;
  assert.deepEqual([...auth.ownerTaskIds].sort(), ["a1", "a2"]);
  const ui = out.find((f) => f.id === "ui")!;
  assert.deepEqual(ui.ownerTaskIds, ["u1"]);
});

test("deriveFeatures: task without feature_id is filtered out entirely", () => {
  const tasks: Task[] = [
    mkTask({ id: "1", status: "pending", feature_id: "auth" }),
    mkTask({ id: "2", status: "pending" }), // no feature_id
    mkTask({ id: "3", status: "pending", feature_id: "" }), // empty string
  ];
  const out = deriveFeatures(tasks, "proj");
  assert.equal(out.length, 1);
  assert.equal(out[0]!.id, "auth");
  assert.equal(out[0]!.taskCount.total, 1);
});

test("deriveFeatures: propagates projectId and mergePolicy", () => {
  const tasks: Task[] = [
    mkTask({
      id: "1",
      status: "pending",
      feature_id: "auth",
      merge_policy: "auto_merge",
    }),
  ];
  const out = deriveFeatures(tasks, "my-proj");
  assert.equal(out[0]!.projectId, "my-proj");
  assert.equal(out[0]!.mergePolicy, "auto_merge");
});

// ─── sortFeatures: canonical order ─────────────────────────────────

test("sortFeatures: blocked → in-progress → mr-open → ready-to-merge → finished → merged", () => {
  // Order per plan text: "blocked at top", then in-progress, the two MR
  // states (a real MR waits on a person, so it outranks a parked merge
  // intent), finished, and merged last (collapsed at bottom).
  const feats: DerivedFeature[] = [
    mkFeat("z-merged", "merged"),
    mkFeat("y-finished", "finished"),
    mkFeat("x2-ready", "ready-to-merge"),
    mkFeat("x-mr", "mr-open"),
    mkFeat("w-inprog", "in-progress"),
    mkFeat("v-blocked", "blocked"),
  ];
  const sorted = sortFeatures(feats);
  assert.deepEqual(
    sorted.map((f) => f.lifecycle),
    [
      "blocked",
      "in-progress",
      "mr-open",
      "ready-to-merge",
      "finished",
      "merged",
    ],
  );
});

test("sortFeatures: within the same lifecycle bucket, sorts by id", () => {
  const feats: DerivedFeature[] = [
    mkFeat("b-inprog", "in-progress"),
    mkFeat("a-inprog", "in-progress"),
    mkFeat("c-inprog", "in-progress"),
  ];
  const sorted = sortFeatures(feats);
  assert.deepEqual(
    sorted.map((f) => f.id),
    ["a-inprog", "b-inprog", "c-inprog"],
  );
});

// ─── feature dependencies ──────────────────────────────────────────

test("deriveFeatures: dependsOn comes from feature_depends_on", () => {
  const feats = deriveFeatures(
    [
      mkTask({ id: "t1", feature_id: "api", feature_depends_on: ["schema"] }),
      mkTask({ id: "t2", feature_id: "api", feature_depends_on: ["schema"] }),
    ],
    "proj",
  );
  assert.deepEqual(feats[0].dependsOn, ["schema"]);
});

test("deriveFeatures: dependsOn defaults to empty, never undefined", () => {
  const feats = deriveFeatures([mkTask({ id: "t1", feature_id: "api" })], "proj");
  assert.deepEqual(feats[0].dependsOn, []);
});

test("deriveFeatures: first non-empty feature_depends_on wins", () => {
  // A task edited in isolation can disagree with its siblings; we take
  // the first non-empty rather than unioning, matching the TUI.
  const feats = deriveFeatures(
    [
      mkTask({ id: "t1", feature_id: "api" }),
      mkTask({ id: "t2", feature_id: "api", feature_depends_on: ["schema"] }),
      mkTask({ id: "t3", feature_id: "api", feature_depends_on: ["other"] }),
    ],
    "proj",
  );
  assert.deepEqual(feats[0].dependsOn, ["schema"]);
});

test("deriveFeatures: a feature depending on itself drops the self-ref", () => {
  const feats = deriveFeatures(
    [mkTask({ id: "t1", feature_id: "api", feature_depends_on: ["api"] })],
    "proj",
  );
  assert.deepEqual(feats[0].dependsOn, []);
});

test("buildFeatureForest: dependent features nest under their dependency", () => {
  const roots = buildFeatureForest([
    mkFeat("schema", "finished"),
    mkFeat("api", "in-progress", ["schema"]),
    mkFeat("ui", "in-progress", ["api"]),
  ]);
  assert.deepEqual(
    roots.map((r) => r.id),
    ["schema"],
  );
  assert.deepEqual(
    roots[0].children.map((c) => c.id),
    ["api"],
  );
  assert.deepEqual(
    roots[0].children[0].children.map((c) => c.id),
    ["ui"],
  );
});

test("buildFeatureForest: two features sharing a dependency both nest", () => {
  const roots = buildFeatureForest([
    mkFeat("schema", "finished"),
    mkFeat("api", "in-progress", ["schema"]),
    mkFeat("docs", "in-progress", ["schema"]),
  ]);
  assert.equal(roots.length, 1);
  assert.deepEqual(
    roots[0].children.map((c) => c.id),
    ["api", "docs"],
  );
});

test("buildFeatureForest: a filtered-out dependency leaves the dependent a root", () => {
  // A dependency the caller filtered out (a merged feature, or one in
  // another project) must not take its dependent down with it — the
  // dependent stays a root rather than disappearing.
  const roots = buildFeatureForest([mkFeat("api", "in-progress", ["merged-schema"])]);
  assert.deepEqual(
    roots.map((r) => r.id),
    ["api"],
  );
});

test("buildFeatureForest: root order follows the input order", () => {
  // Callers pipe through sortFeatures first, so input order is the
  // canonical lifecycle ordering.
  const roots = buildFeatureForest([
    mkFeat("z", "blocked"),
    mkFeat("a", "in-progress"),
  ]);
  assert.deepEqual(
    roots.map((r) => r.id),
    ["z", "a"],
  );
});

test("buildFeatureForest: a dependency cycle renders both as flagged roots", () => {
  const roots = buildFeatureForest([
    mkFeat("a", "in-progress", ["b"]),
    mkFeat("b", "in-progress", ["a"]),
  ]);
  assert.equal(roots.length, 2);
  assert.ok(roots.every((r) => r.inCycle));
});

// ─── helpers ───────────────────────────────────────────────────────

function mkFeat(
  id: string,
  lifecycle: DerivedFeature["lifecycle"],
  dependsOn: string[] = [],
): DerivedFeature {
  return {
    id,
    projectId: "proj",
    name: id,
    progress: 0,
    lifecycle,
    taskCount: { total: 0, completed: 0, blocked: 0, active: 0 },
    ownerTaskIds: [],
    resumableCount: 0,
    dependsOn,
  };
}

// ─── Brain-native MR fold ──────────────────────────────────────────

test("deriveFeatures: an open Brain-native MR flips lifecycle to ready-to-merge", () => {
  // NOT mr-open: a merge_request entry is a parked merge intent inside
  // Brain, with nothing open on any git server and no url to follow.
  const feats = deriveFeatures(
    [mkTask({ id: "t1", feature_id: "api", status: "completed" })],
    "proj",
    new Set(["api"]),
  );
  assert.equal(feats[0].lifecycle, "ready-to-merge");
  assert.equal(feats[0].prUrl, undefined);
});

test("deriveFeatures: a forge MR url beats an open MR entry on the same feature", () => {
  // Both signals present. mr-open wins: it is the only one the user can
  // click through to, and the entry adds nothing the url does not say.
  const feats = deriveFeatures(
    [
      mkTask({
        id: "t1",
        feature_id: "api",
        status: "completed",
        content: "see https://github.com/acme/foo/pull/7",
      }),
    ],
    "proj",
    new Set(["api"]),
  );
  assert.equal(feats[0].lifecycle, "mr-open");
  assert.equal(feats[0].prUrl, "https://github.com/acme/foo/pull/7");
});

test("deriveFeatures: merged still trumps an open MR entry", () => {
  // All tasks validated = merged; a stale MR entry must not regress it.
  const feats = deriveFeatures(
    [mkTask({ id: "t1", feature_id: "api", status: "validated" })],
    "proj",
    new Set(["api"]),
  );
  assert.equal(feats[0].lifecycle, "merged");
});

test("deriveFeatures: MR set only affects the named feature", () => {
  const feats = deriveFeatures(
    [
      mkTask({ id: "t1", feature_id: "api", status: "completed" }),
      mkTask({ id: "t2", feature_id: "ui", status: "completed" }),
    ],
    "proj",
    new Set(["api"]),
  );
  const byId = Object.fromEntries(feats.map((f) => [f.id, f.lifecycle]));
  assert.equal(byId.api, "ready-to-merge");
  assert.equal(byId.ui, "finished");
});

// The fold upgrades `finished` and nothing else — "ready to merge" claims
// the work is done, so unfinished work has to outrank a parked MR entry.

test("deriveFeatures: a still-running task outranks an open MR entry", () => {
  // Checkout parked an MR entry, then a follow-up task was added and
  // picked up. The feature is NOT ready to merge.
  const feats = deriveFeatures(
    [
      mkTask({ id: "t1", feature_id: "api", status: "completed" }),
      mkTask({ id: "t2", feature_id: "api", status: "in_progress" }),
    ],
    "proj",
    new Set(["api"]),
  );
  assert.equal(feats[0].lifecycle, "in-progress");
});

test("deriveFeatures: a blocked task outranks an open MR entry", () => {
  const feats = deriveFeatures(
    [
      mkTask({ id: "t1", feature_id: "api", status: "completed" }),
      mkTask({ id: "t2", feature_id: "api", status: "blocked" }),
    ],
    "proj",
    new Set(["api"]),
  );
  assert.equal(feats[0].lifecycle, "blocked");
});

test("deriveFeatures: omitting the MR set preserves previous behavior", () => {
  const feats = deriveFeatures(
    [mkTask({ id: "t1", feature_id: "api", status: "completed" })],
    "proj",
  );
  assert.equal(feats[0].lifecycle, "finished");
});

// ─── archived exclusion ────────────────────────────────────────────
// Mirrors the server rule: archived counts toward nothing.

test("deriveFeatures: archived + completed still derives finished with full progress", () => {
  const out = deriveFeatures(
    [
      mkTask({ id: "1", status: "completed", feature_id: "auth" }),
      mkTask({ id: "2", status: "archived", feature_id: "auth" }),
    ],
    "proj",
  );
  assert.equal(out.length, 1);
  assert.equal(out[0]!.lifecycle, "finished");
  assert.equal(out[0]!.progress, 1);
  assert.equal(out[0]!.taskCount.total, 1);
  assert.equal(out[0]!.taskCount.completed, 1);
});

test("deriveFeatures: archived + validated still derives merged", () => {
  const out = deriveFeatures(
    [
      mkTask({ id: "1", status: "validated", feature_id: "auth" }),
      mkTask({ id: "2", status: "archived", feature_id: "auth" }),
    ],
    "proj",
  );
  assert.equal(out[0]!.lifecycle, "merged");
  assert.equal(out[0]!.progress, 1);
});

test("deriveFeatures: an all-archived feature derives no feature at all", () => {
  // It leaves the lanes entirely — same as the server computing status
  // "archived" for the feature.
  const out = deriveFeatures(
    [
      mkTask({ id: "1", status: "archived", feature_id: "auth" }),
      mkTask({ id: "2", status: "archived", feature_id: "auth" }),
      mkTask({ id: "3", status: "pending", feature_id: "ui" }),
    ],
    "proj",
  );
  assert.deepEqual(
    out.map((f) => f.id),
    ["ui"],
  );
});

test("deriveFeatures: archived tasks are absent from ownerTaskIds", () => {
  const out = deriveFeatures(
    [
      mkTask({ id: "a1", status: "pending", feature_id: "auth" }),
      mkTask({ id: "a2", status: "archived", feature_id: "auth" }),
    ],
    "proj",
  );
  assert.deepEqual(out[0]!.ownerTaskIds, ["a1"]);
});

test("deriveFeatures: an archived abandoned task does not count as resumable", () => {
  const out = deriveFeatures(
    [
      mkTask({ id: "1", status: "pending", feature_id: "auth" }),
      mkTask({
        id: "2",
        status: "archived",
        feature_id: "auth",
        is_abandoned: true,
      }),
    ],
    "proj",
  );
  assert.equal(out[0]!.resumableCount, 0);
});

test("deriveFeatures: an archived task's PR URL does not surface", () => {
  const out = deriveFeatures(
    [
      mkTask({ id: "1", status: "pending", feature_id: "auth" }),
      mkTask({
        id: "2",
        status: "archived",
        feature_id: "auth",
        mr_url: "https://github.com/acme/foo/pull/9",
      }),
    ],
    "proj",
  );
  assert.equal(out[0]!.prUrl, undefined);
  assert.equal(out[0]!.lifecycle, "in-progress");
});

// ─── isFeatureDone ───────────────────────────────────────────────
// Drives the default fold of a feature's task rows in CardTasks.

test("isFeatureDone: finished and merged are done, nothing else is", () => {
  const of = (lifecycle: DerivedFeature["lifecycle"]): DerivedFeature => ({
    id: "f",
    projectId: "p",
    name: "f",
    progress: 1,
    lifecycle,
    taskCount: { total: 1, completed: 1, blocked: 0, active: 0 },
    ownerTaskIds: ["t1"],
    resumableCount: 0,
    dependsOn: [],
  });

  assert.equal(isFeatureDone(of("finished")), true);
  assert.equal(isFeatureDone(of("merged")), true);
  assert.equal(isFeatureDone(of("in-progress")), false);
  assert.equal(isFeatureDone(of("blocked")), false);
  // An open MR is the one lifecycle that is 100% coded and still waiting
  // on a human — folding it away would hide the thing that needs doing.
  assert.equal(isFeatureDone(of("mr-open")), false);
});
