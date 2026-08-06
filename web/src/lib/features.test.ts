/**
 * Tests for lib/features — pure feature-lifecycle derivation.
 *
 * A feature is an emergent grouping of tasks that share the same
 * `feature_id`. `deriveFeatures` folds a task list into one row per
 * feature with:
 *   - progress (completed / total, 0..1)
 *   - lifecycle: in-progress | blocked | finished | mr-open | merged
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
  sortFeatures,
  extractPrUrl,
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

test("sortFeatures: blocked → in-progress → mr-open → finished → merged", () => {
  // Order per plan text: "blocked at top", then in-progress, mr-open,
  // finished, and merged last (collapsed at bottom).
  const feats: DerivedFeature[] = [
    mkFeat("z-merged", "merged"),
    mkFeat("y-finished", "finished"),
    mkFeat("x-mr", "mr-open"),
    mkFeat("w-inprog", "in-progress"),
    mkFeat("v-blocked", "blocked"),
  ];
  const sorted = sortFeatures(feats);
  assert.deepEqual(
    sorted.map((f) => f.lifecycle),
    ["blocked", "in-progress", "mr-open", "finished", "merged"],
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

// ─── helpers ───────────────────────────────────────────────────────

function mkFeat(id: string, lifecycle: DerivedFeature["lifecycle"]): DerivedFeature {
  return {
    id,
    projectId: "proj",
    name: id,
    progress: 0,
    lifecycle,
    taskCount: { total: 0, completed: 0, blocked: 0, active: 0 },
    ownerTaskIds: [],
    resumableCount: 0,
  };
}
