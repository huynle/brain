/**
 * Feature Service Tests
 */

import { describe, it, expect } from "bun:test";
import {
  computeFeatures,
  computeFeatureStatus,
  resolveFeatureDependencies,
  getReadyFeatures,
  getWaitingFeatures,
  getBlockedFeatures,
  computeAndResolveFeatures,
  findFeatureCycles,
  buildFeatureAdjacencyList,
  buildFeatureLookupMaps,
} from "./feature-service";
import type { ResolvedTask } from "./types";

// Helper to create mock tasks
function mockTask(overrides: Partial<ResolvedTask> = {}): ResolvedTask {
  return {
    id: "task1",
    path: "projects/test/task/task1.md",
    title: "Test Task",
    priority: "medium",
    status: "pending",
    depends_on: [],
    created: "2024-01-01",
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    user_original_request: null,
    resolved_deps: [],
    unresolved_deps: [],
    classification: "ready",
    blocked_by: [],
    waiting_on: [],
    in_cycle: false,
    resolved_workdir: null,
    ...overrides,
  };
}

describe("computeFeatures", () => {
  it("groups tasks by feature_id", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth" }),
      mockTask({ id: "t2", feature_id: "auth" }),
      mockTask({ id: "t3", feature_id: "payments" }),
    ];

    const features = computeFeatures(tasks);

    expect(features).toHaveLength(2);
    expect(features.find((f) => f.id === "auth")?.tasks).toHaveLength(2);
    expect(features.find((f) => f.id === "payments")?.tasks).toHaveLength(1);
  });

  it("skips tasks without feature_id", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth" }),
      mockTask({ id: "t2" }), // No feature_id
    ];

    const features = computeFeatures(tasks);

    expect(features).toHaveLength(1);
    expect(features[0].id).toBe("auth");
    expect(features[0].tasks).toHaveLength(1);
  });

  it("uses highest priority from any task", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth", priority: "low" }),
      mockTask({ id: "t2", feature_id: "auth", priority: "high" }),
      mockTask({ id: "t3", feature_id: "auth", priority: "medium" }),
    ];

    const features = computeFeatures(tasks);

    expect(features[0].priority).toBe("high");
  });

  it("collects feature dependencies from all tasks", () => {
    const tasks = [
      mockTask({
        id: "t1",
        feature_id: "payments",
        feature_depends_on: ["auth"],
      }),
      mockTask({
        id: "t2",
        feature_id: "payments",
        feature_depends_on: ["users", "auth"],
      }),
    ];

    const features = computeFeatures(tasks);

    expect(features[0].depends_on_features).toContain("auth");
    expect(features[0].depends_on_features).toContain("users");
    expect(features[0].depends_on_features).toHaveLength(2); // Deduped
  });
});

describe("computeFeatureStatus", () => {
  it("returns completed when all tasks completed", () => {
    const tasks = [
      mockTask({ status: "completed" }),
      mockTask({ status: "validated" }),
    ];

    expect(computeFeatureStatus(tasks)).toBe("completed");
  });

  it("returns in_progress when any task in_progress", () => {
    const tasks = [
      mockTask({ status: "pending" }),
      mockTask({ status: "in_progress" }),
      mockTask({ status: "completed" }),
    ];

    expect(computeFeatureStatus(tasks)).toBe("in_progress");
  });

  it("returns blocked when any task blocked and none in_progress", () => {
    const tasks = [
      mockTask({ status: "pending" }),
      mockTask({ status: "blocked" }),
    ];

    expect(computeFeatureStatus(tasks)).toBe("blocked");
  });

  it("returns pending when all pending", () => {
    const tasks = [mockTask({ status: "pending" }), mockTask({ status: "pending" })];

    expect(computeFeatureStatus(tasks)).toBe("pending");
  });

  it("returns pending for empty array", () => {
    expect(computeFeatureStatus([])).toBe("pending");
  });
});

describe("resolveFeatureDependencies", () => {
  it("classifies features with no deps as ready", () => {
    const tasks = [mockTask({ id: "t1", feature_id: "auth", status: "pending" })];
    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);

    expect(resolved[0].classification).toBe("ready");
    expect(resolved[0].blocked_by_features).toHaveLength(0);
    expect(resolved[0].waiting_on_features).toHaveLength(0);
  });

  it("classifies features waiting on pending deps", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth", status: "pending" }),
      mockTask({
        id: "t2",
        feature_id: "payments",
        status: "pending",
        feature_depends_on: ["auth"],
      }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);

    const payments = resolved.find((f) => f.id === "payments")!;
    expect(payments.classification).toBe("waiting");
    expect(payments.waiting_on_features).toContain("auth");
  });

  it("classifies features with completed deps as ready", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth", status: "completed" }),
      mockTask({
        id: "t2",
        feature_id: "payments",
        status: "pending",
        feature_depends_on: ["auth"],
      }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);

    const payments = resolved.find((f) => f.id === "payments")!;
    expect(payments.classification).toBe("ready");
  });

  it("classifies features with blocked deps as blocked", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth", status: "blocked" }),
      mockTask({
        id: "t2",
        feature_id: "payments",
        status: "pending",
        feature_depends_on: ["auth"],
      }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);

    const payments = resolved.find((f) => f.id === "payments")!;
    expect(payments.classification).toBe("blocked");
    expect(payments.blocked_by_features).toContain("auth");
  });

  it("handles missing feature references gracefully", () => {
    const tasks = [
      mockTask({
        id: "t1",
        feature_id: "payments",
        status: "pending",
        feature_depends_on: ["nonexistent"],
      }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);

    // Should be ready since the dependency doesn't exist
    expect(resolved[0].classification).toBe("ready");
  });
});

describe("findFeatureCycles", () => {
  it("detects simple cycle", () => {
    const adjacency = new Map([
      ["a", ["b"]],
      ["b", ["a"]],
    ]);

    const cycles = findFeatureCycles(adjacency);

    expect(cycles.has("a")).toBe(true);
    expect(cycles.has("b")).toBe(true);
  });

  it("detects indirect cycle", () => {
    const adjacency = new Map([
      ["a", ["b"]],
      ["b", ["c"]],
      ["c", ["a"]],
    ]);

    const cycles = findFeatureCycles(adjacency);

    expect(cycles.has("a")).toBe(true);
    expect(cycles.has("b")).toBe(true);
    expect(cycles.has("c")).toBe(true);
  });

  it("handles no cycles", () => {
    const adjacency = new Map([
      ["a", ["b"]],
      ["b", ["c"]],
      ["c", []],
    ]);

    const cycles = findFeatureCycles(adjacency);

    expect(cycles.size).toBe(0);
  });
});

describe("resolveFeatureDependencies with cycles", () => {
  it("marks features in cycles as blocked", () => {
    const tasks = [
      mockTask({
        id: "t1",
        feature_id: "a",
        status: "pending",
        feature_depends_on: ["b"],
      }),
      mockTask({
        id: "t2",
        feature_id: "b",
        status: "pending",
        feature_depends_on: ["a"],
      }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);

    expect(resolved[0].classification).toBe("blocked");
    expect(resolved[0].in_cycle).toBe(true);
    expect(resolved[1].classification).toBe("blocked");
    expect(resolved[1].in_cycle).toBe(true);
  });
});

describe("getReadyFeatures", () => {
  it("returns ready features sorted by priority", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "low", priority: "low", status: "pending" }),
      mockTask({ id: "t2", feature_id: "high", priority: "high", status: "pending" }),
      mockTask({
        id: "t3",
        feature_id: "medium",
        priority: "medium",
        status: "pending",
      }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);
    const ready = getReadyFeatures(resolved);

    expect(ready[0].id).toBe("high");
    expect(ready[1].id).toBe("medium");
    expect(ready[2].id).toBe("low");
  });

  it("excludes completed features", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "done", status: "completed" }),
      mockTask({ id: "t2", feature_id: "pending", status: "pending" }),
    ];

    const features = computeFeatures(tasks);
    const resolved = resolveFeatureDependencies(features);
    const ready = getReadyFeatures(resolved);

    expect(ready).toHaveLength(1);
    expect(ready[0].id).toBe("pending");
  });
});

describe("computeAndResolveFeatures", () => {
  it("computes features and resolves dependencies in one call", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth", status: "completed" }),
      mockTask({
        id: "t2",
        feature_id: "payments",
        status: "pending",
        feature_depends_on: ["auth"],
      }),
    ];

    const result = computeAndResolveFeatures(tasks);

    expect(result.features).toHaveLength(2);
    expect(result.stats.total).toBe(2);
    expect(result.stats.ready).toBe(2); // auth is completed (ready), payments deps satisfied (ready)
    expect(result.cycles).toHaveLength(0);
  });

  it("reports cycles in result", () => {
    const tasks = [
      mockTask({
        id: "t1",
        feature_id: "a",
        status: "pending",
        feature_depends_on: ["b"],
      }),
      mockTask({
        id: "t2",
        feature_id: "b",
        status: "pending",
        feature_depends_on: ["a"],
      }),
    ];

    const result = computeAndResolveFeatures(tasks);

    expect(result.cycles.length).toBeGreaterThan(0);
    expect(result.stats.blocked).toBe(2);
  });
});

describe("task stats computation", () => {
  it("computes correct task stats", () => {
    const tasks = [
      mockTask({ id: "t1", feature_id: "auth", status: "pending" }),
      mockTask({ id: "t2", feature_id: "auth", status: "in_progress" }),
      mockTask({ id: "t3", feature_id: "auth", status: "completed" }),
      mockTask({ id: "t4", feature_id: "auth", status: "blocked" }),
    ];

    const features = computeFeatures(tasks);

    expect(features[0].task_stats.total).toBe(4);
    expect(features[0].task_stats.pending).toBe(1);
    expect(features[0].task_stats.in_progress).toBe(1);
    expect(features[0].task_stats.completed).toBe(1);
    expect(features[0].task_stats.blocked).toBe(1);
  });
});
