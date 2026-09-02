import { strict as assert } from "node:assert";
import { test } from "node:test";

import { NO_FEATURE, archivedKey, bucketArchived } from "./taskGroups";
import type { Task, TaskStatus } from "./types";

const task = (
  id: string,
  feature?: string,
  status: TaskStatus = "archived",
  depends?: string[],
): Task => ({
  id,
  path: `p/${id}.md`,
  title: `Task ${id}`,
  priority: "medium",
  status,
  ...(feature ? { feature_id: feature } : {}),
  // `resolved_deps`, not `depends_on`: the tree deliberately uses the
  // server-resolved edges (see lib/taskTree's docstring) — authored
  // `depends_on` can name ids that were never resolved.
  ...(depends ? { resolved_deps: depends } : {}),
});

// The archived key must not collide with the live feature's own fold key:
// the SAME feature can hold live and archived tasks on either side of the
// tab split, and folding one must not fold the other.
test("archivedKey is distinct from the bare feature id", () => {
  assert.notEqual(archivedKey("auth"), "auth");
  assert.match(archivedKey("auth"), /^__archived__:/);
  assert.notEqual(archivedKey(NO_FEATURE), NO_FEATURE);
});

test("bucketArchived groups by feature, ungrouped last", () => {
  const groups = bucketArchived([
    task("a", "zeta"),
    task("b"),
    task("c", "alpha"),
    task("d", "zeta"),
  ]);
  assert.deepEqual(
    groups.map((g) => g.key),
    ["alpha", "zeta", NO_FEATURE],
  );
  assert.deepEqual(
    groups.map((g) => g.label),
    ["alpha", "zeta", "No feature"],
  );
  assert.deepEqual(
    groups.map((g) => g.tasks.length),
    [1, 2, 1],
  );
});

test("bucketArchived keeps dependency order inside a bucket", () => {
  const groups = bucketArchived([
    task("child", "auth", "archived", ["parent"]),
    task("parent", "auth"),
  ]);
  assert.equal(groups.length, 1);
  // The child nests under its parent, so the parent leads and the child
  // carries a guide prefix.
  const ids = groups[0]!.rows.map((r) => r.node.item.id);
  assert.deepEqual(ids, ["parent", "child"]);
  assert.equal(groups[0]!.rows[0]!.depth, 0);
  assert.equal(groups[0]!.rows[1]!.depth, 1);
});

test("bucketArchived on an empty list yields no groups", () => {
  assert.deepEqual(bucketArchived([]), []);
});

// A feature whose tasks are ALL archived derives no DerivedFeature (that
// is the rule in lib/features), so the raw feature_id is the only thing
// left to group by — which is exactly what this does.
test("bucketArchived needs no DerivedFeature", () => {
  const groups = bucketArchived([task("only", "gone-feature")]);
  assert.equal(groups[0]!.key, "gone-feature");
  assert.equal(groups[0]!.label, "gone-feature");
});
