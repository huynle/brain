/**
 * Tests for the pure selectors behind useGoals.
 *
 * The hook itself is a thin useQuery wrapper; the logic worth pinning is
 * the grouping/scoping: project-less goals dropped from byProject,
 * feature scope inclusive of task-scoped goals, task scope exact on
 * config.task_id.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  goalsForFeature,
  goalsForTask,
  indexGoalsByProject,
} from "./useGoals";
import type { GoalSummary } from "../lib/types";

function mkGoal(over: Partial<GoalSummary> = {}): GoalSummary {
  return {
    entry_id: "e-1",
    goal_id: "g-1",
    title: "A goal",
    project: "brain-api",
    status: "active",
    ...over,
  };
}

const GOALS: GoalSummary[] = [
  mkGoal({ goal_id: "g-1", project: "brain-api", feature_id: "auth" }),
  mkGoal({
    goal_id: "g-2",
    project: "brain-api",
    feature_id: "auth",
    config: { id: "g-2", task_id: "task-7" },
  }),
  mkGoal({ goal_id: "g-3", project: "brain-api" }), // project-wide
  mkGoal({ goal_id: "g-4", project: "shop", feature_id: "auth" }),
  mkGoal({ goal_id: "g-5", project: undefined }), // global — no project
];

test("indexGoalsByProject groups by project and drops project-less goals", () => {
  const byProject = indexGoalsByProject(GOALS);
  assert.deepEqual(
    [...byProject.keys()].sort(),
    ["brain-api", "shop"],
  );
  assert.deepEqual(
    byProject.get("brain-api")!.map((g) => g.goal_id),
    ["g-1", "g-2", "g-3"],
  );
  assert.deepEqual(
    byProject.get("shop")!.map((g) => g.goal_id),
    ["g-4"],
  );
});

test("goalsForFeature matches project AND feature, including task-scoped goals", () => {
  const got = goalsForFeature(GOALS, "brain-api", "auth").map(
    (g) => g.goal_id,
  );
  // g-2 is task-scoped but still belongs to the feature it was filed under.
  assert.deepEqual(got, ["g-1", "g-2"]);
  // Same feature id in another project must not bleed through.
  assert.deepEqual(
    goalsForFeature(GOALS, "shop", "auth").map((g) => g.goal_id),
    ["g-4"],
  );
});

test("goalsForFeature with blank ids returns empty", () => {
  assert.deepEqual(goalsForFeature(GOALS, "", "auth"), []);
  assert.deepEqual(goalsForFeature(GOALS, "brain-api", ""), []);
});

test("goalsForTask matches only goals pinned to that task", () => {
  assert.deepEqual(
    goalsForTask(GOALS, "brain-api", "task-7").map((g) => g.goal_id),
    ["g-2"],
  );
  assert.deepEqual(goalsForTask(GOALS, "brain-api", "task-8"), []);
  assert.deepEqual(goalsForTask(GOALS, "shop", "task-7"), []);
});

test("goalsForTask with blank ids returns empty", () => {
  assert.deepEqual(goalsForTask(GOALS, "", "task-7"), []);
  assert.deepEqual(goalsForTask(GOALS, "brain-api", ""), []);
});
