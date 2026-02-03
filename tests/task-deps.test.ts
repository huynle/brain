/**
 * Unit tests for task dependency resolution
 */

import { describe, test, expect } from "bun:test";
import {
  buildLookupMaps,
  resolveDep,
  getParentChain,
  buildAdjacencyList,
  findCycles,
  classifyTask,
  resolveDependencies,
  sortByPriority,
  getReadyTasks,
  getWaitingTasks,
  getBlockedTasks,
  getNextTask,
} from "../src/core/task-deps";
import type { Task } from "../src/core/types";

// =============================================================================
// Test Fixtures
// =============================================================================

function createTask(overrides: Partial<Task> = {}): Task {
  return {
    id: "task1",
    path: "projects/test/task/task1.md",
    title: "Test Task",
    priority: "medium",
    status: "pending",
    depends_on: [],
    parent_id: null,
    created: "2024-01-01",
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    ...overrides,
  };
}

// =============================================================================
// Lookup Maps Tests
// =============================================================================

describe("buildLookupMaps", () => {
  test("creates byId map", () => {
    const tasks = [
      createTask({ id: "a", title: "Task A" }),
      createTask({ id: "b", title: "Task B" }),
    ];
    const maps = buildLookupMaps(tasks);

    expect(maps.byId.get("a")?.title).toBe("Task A");
    expect(maps.byId.get("b")?.title).toBe("Task B");
  });

  test("creates titleToId map", () => {
    const tasks = [
      createTask({ id: "a", title: "Task A" }),
      createTask({ id: "b", title: "Task B" }),
    ];
    const maps = buildLookupMaps(tasks);

    expect(maps.titleToId.get("Task A")).toBe("a");
    expect(maps.titleToId.get("Task B")).toBe("b");
  });

  test("handles empty task list", () => {
    const maps = buildLookupMaps([]);

    expect(maps.byId.size).toBe(0);
    expect(maps.titleToId.size).toBe(0);
  });
});

describe("resolveDep", () => {
  test("resolves by ID", () => {
    const tasks = [createTask({ id: "abc123", title: "My Task" })];
    const maps = buildLookupMaps(tasks);

    expect(resolveDep("abc123", maps)).toBe("abc123");
  });

  test("resolves by title", () => {
    const tasks = [createTask({ id: "abc123", title: "My Task" })];
    const maps = buildLookupMaps(tasks);

    expect(resolveDep("My Task", maps)).toBe("abc123");
  });

  test("returns null for unknown reference", () => {
    const tasks = [createTask({ id: "abc123", title: "My Task" })];
    const maps = buildLookupMaps(tasks);

    expect(resolveDep("unknown", maps)).toBeNull();
  });
});

// =============================================================================
// Parent Hierarchy Tests
// =============================================================================

describe("getParentChain", () => {
  test("returns empty for task with no parent", () => {
    const tasks = [createTask({ id: "a", parent_id: null })];
    const maps = buildLookupMaps(tasks);

    expect(getParentChain("a", maps)).toEqual([]);
  });

  test("returns parent chain", () => {
    const tasks = [
      createTask({ id: "child", parent_id: "parent" }),
      createTask({ id: "parent", parent_id: "grandparent" }),
      createTask({ id: "grandparent", parent_id: null }),
    ];
    const maps = buildLookupMaps(tasks);

    expect(getParentChain("child", maps)).toEqual(["parent", "grandparent"]);
  });

  test("handles circular parent references", () => {
    const tasks = [
      createTask({ id: "a", parent_id: "b" }),
      createTask({ id: "b", parent_id: "a" }),
    ];
    const maps = buildLookupMaps(tasks);

    // Should not infinite loop
    const chain = getParentChain("a", maps);
    expect(chain.length).toBeLessThan(10);
  });

  test("handles missing parent", () => {
    const tasks = [createTask({ id: "a", parent_id: "nonexistent" })];
    const maps = buildLookupMaps(tasks);

    // Returns the reference even if parent doesn't exist (stops at first missing)
    expect(getParentChain("a", maps)).toEqual(["nonexistent"]);
  });
});

// =============================================================================
// Adjacency List Tests
// =============================================================================

describe("buildAdjacencyList", () => {
  test("builds adjacency list from tasks", () => {
    const tasks = [
      createTask({ id: "a", depends_on: ["b", "c"] }),
      createTask({ id: "b", depends_on: [] }),
      createTask({ id: "c", depends_on: ["b"] }),
    ];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    expect(adj.get("a")).toEqual(["b", "c"]);
    expect(adj.get("b")).toEqual([]);
    expect(adj.get("c")).toEqual(["b"]);
  });

  test("filters out unresolved dependencies", () => {
    const tasks = [
      createTask({ id: "a", depends_on: ["b", "nonexistent"] }),
      createTask({ id: "b", depends_on: [] }),
    ];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    expect(adj.get("a")).toEqual(["b"]);
  });
});

// =============================================================================
// Cycle Detection Tests
// =============================================================================

describe("findCycles", () => {
  test("detects no cycles in linear chain", () => {
    const tasks = [
      createTask({ id: "a", depends_on: ["b"] }),
      createTask({ id: "b", depends_on: ["c"] }),
      createTask({ id: "c", depends_on: [] }),
    ];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    const cycles = findCycles(adj);
    expect(cycles.size).toBe(0);
  });

  test("detects simple cycle A -> B -> A", () => {
    const tasks = [
      createTask({ id: "a", depends_on: ["b"] }),
      createTask({ id: "b", depends_on: ["a"] }),
    ];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    const cycles = findCycles(adj);
    expect(cycles.has("a")).toBe(true);
    expect(cycles.has("b")).toBe(true);
  });

  test("detects longer cycle A -> B -> C -> A", () => {
    const tasks = [
      createTask({ id: "a", depends_on: ["b"] }),
      createTask({ id: "b", depends_on: ["c"] }),
      createTask({ id: "c", depends_on: ["a"] }),
    ];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    const cycles = findCycles(adj);
    expect(cycles.has("a")).toBe(true);
    expect(cycles.has("b")).toBe(true);
    expect(cycles.has("c")).toBe(true);
  });

  test("detects self-cycle A -> A", () => {
    const tasks = [createTask({ id: "a", depends_on: ["a"] })];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    const cycles = findCycles(adj);
    expect(cycles.has("a")).toBe(true);
  });

  test("handles empty adjacency list", () => {
    const adj = new Map<string, string[]>();
    const cycles = findCycles(adj);
    expect(cycles.size).toBe(0);
  });

  test("handles tasks with no dependencies", () => {
    const tasks = [
      createTask({ id: "a", depends_on: [] }),
      createTask({ id: "b", depends_on: [] }),
    ];
    const maps = buildLookupMaps(tasks);
    const adj = buildAdjacencyList(tasks, maps);

    const cycles = findCycles(adj);
    expect(cycles.size).toBe(0);
  });
});

// =============================================================================
// Classification Tests
// =============================================================================

describe("classifyTask", () => {
  test("classifies task in cycle as blocked with reason", () => {
    const task = createTask({ id: "a", status: "pending" });
    const inCycle = new Set(["a"]);
    const effectiveStatus = new Map([["a", "circular"]]);

    const result = classifyTask(task, [], [], effectiveStatus, inCycle);

    expect(result.classification).toBe("blocked");
    expect(result.reason).toBe("circular_dependency");
  });

  test("classifies non-pending task", () => {
    const task = createTask({ id: "a", status: "completed" });
    const effectiveStatus = new Map([["a", "completed"]]);

    const result = classifyTask(task, [], [], effectiveStatus, new Set());

    expect(result.classification).toBe("not_pending");
  });

  test("classifies task with blocked parent", () => {
    const task = createTask({ id: "a", status: "pending", parent_id: "parent" });
    const parentChain = ["parent"];
    const effectiveStatus = new Map([
      ["a", "pending"],
      ["parent", "blocked"],
    ]);

    const result = classifyTask(
      task,
      [],
      parentChain,
      effectiveStatus,
      new Set()
    );

    expect(result.classification).toBe("blocked_by_parent");
    expect(result.blockedBy).toContain("parent");
  });

  test("classifies task waiting on parent", () => {
    const task = createTask({ id: "a", status: "pending", parent_id: "parent" });
    const effectiveStatus = new Map([
      ["a", "pending"],
      ["parent", "pending"],
    ]);

    const result = classifyTask(task, [], [], effectiveStatus, new Set());

    expect(result.classification).toBe("waiting_on_parent");
    expect(result.waitingOn).toContain("parent");
  });

  test("classifies task with blocked dependency", () => {
    const task = createTask({ id: "a", status: "pending" });
    const resolvedDeps = ["b"];
    const effectiveStatus = new Map([
      ["a", "pending"],
      ["b", "blocked"],
    ]);

    const result = classifyTask(
      task,
      resolvedDeps,
      [],
      effectiveStatus,
      new Set()
    );

    expect(result.classification).toBe("blocked");
    expect(result.blockedBy).toContain("b");
  });

  test("classifies task waiting on dependency", () => {
    const task = createTask({ id: "a", status: "pending" });
    const resolvedDeps = ["b"];
    const effectiveStatus = new Map([
      ["a", "pending"],
      ["b", "pending"],
    ]);

    const result = classifyTask(
      task,
      resolvedDeps,
      [],
      effectiveStatus,
      new Set()
    );

    expect(result.classification).toBe("waiting");
    expect(result.waitingOn).toContain("b");
  });

  test("classifies ready task", () => {
    const task = createTask({ id: "a", status: "pending" });
    const effectiveStatus = new Map([["a", "pending"]]);

    const result = classifyTask(task, [], [], effectiveStatus, new Set());

    expect(result.classification).toBe("ready");
    expect(result.blockedBy).toEqual([]);
    expect(result.waitingOn).toEqual([]);
  });
});

// =============================================================================
// Main Resolution Function Tests
// =============================================================================

describe("resolveDependencies", () => {
  test("handles empty task list", () => {
    const result = resolveDependencies([]);

    expect(result.tasks).toEqual([]);
    expect(result.cycles).toEqual([]);
    expect(result.stats.total).toBe(0);
  });

  test("classifies task with no deps as ready", () => {
    const tasks = [createTask({ id: "a", status: "pending", depends_on: [] })];
    const result = resolveDependencies(tasks);

    expect(result.tasks[0].classification).toBe("ready");
  });

  test("classifies task with completed deps as ready", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["b"] }),
      createTask({ id: "b", status: "completed", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);

    const taskA = result.tasks.find((t) => t.id === "a");
    expect(taskA?.classification).toBe("ready");
  });

  test("classifies task with pending deps as waiting", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["b"] }),
      createTask({ id: "b", status: "pending", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);

    const taskA = result.tasks.find((t) => t.id === "a");
    expect(taskA?.classification).toBe("waiting");
    expect(taskA?.waiting_on).toContain("b");
  });

  test("classifies task with blocked deps as blocked", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["b"] }),
      createTask({ id: "b", status: "blocked", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);

    const taskA = result.tasks.find((t) => t.id === "a");
    expect(taskA?.classification).toBe("blocked");
    expect(taskA?.blocked_by).toContain("b");
  });

  test("classifies circular dependency as blocked with reason", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["b"] }),
      createTask({ id: "b", status: "pending", depends_on: ["a"] }),
    ];
    const result = resolveDependencies(tasks);

    const taskA = result.tasks.find((t) => t.id === "a");
    expect(taskA?.classification).toBe("blocked");
    expect(taskA?.blocked_by_reason).toBe("circular_dependency");
    expect(taskA?.in_cycle).toBe(true);
  });

  test("classifies non-pending task as not_pending", () => {
    const tasks = [createTask({ id: "a", status: "completed", depends_on: [] })];
    const result = resolveDependencies(tasks);

    expect(result.tasks[0].classification).toBe("not_pending");
  });

  test("tracks unresolved dependencies", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["nonexistent"] }),
    ];
    const result = resolveDependencies(tasks);

    expect(result.tasks[0].unresolved_deps).toContain("nonexistent");
  });

  test("resolves dependencies by title", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["Task B"] }),
      createTask({ id: "b", title: "Task B", status: "completed", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);

    const taskA = result.tasks.find((t) => t.id === "a");
    expect(taskA?.resolved_deps).toContain("b");
    expect(taskA?.classification).toBe("ready");
  });

  test("returns cycles in result", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["b"] }),
      createTask({ id: "b", status: "pending", depends_on: ["a"] }),
    ];
    const result = resolveDependencies(tasks);

    expect(result.cycles.length).toBe(1);
    expect(result.cycles[0]).toContain("a");
    expect(result.cycles[0]).toContain("b");
  });
});

// =============================================================================
// Parent Hierarchy Classification Tests
// =============================================================================

describe("parent hierarchy classification", () => {
  test("child waiting when parent is pending", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending", depends_on: [] }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    const child = result.tasks.find((t) => t.id === "child");
    expect(child?.classification).toBe("waiting_on_parent");
  });

  test("child ready when parent is active", () => {
    const tasks = [
      createTask({ id: "parent", status: "active", depends_on: [] }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    const child = result.tasks.find((t) => t.id === "child");
    expect(child?.classification).toBe("ready");
  });

  test("child ready when parent is in_progress", () => {
    const tasks = [
      createTask({ id: "parent", status: "in_progress", depends_on: [] }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    const child = result.tasks.find((t) => t.id === "child");
    expect(child?.classification).toBe("ready");
  });

  test("child blocked when parent is blocked", () => {
    const tasks = [
      createTask({ id: "parent", status: "blocked", depends_on: [] }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    const child = result.tasks.find((t) => t.id === "child");
    expect(child?.classification).toBe("blocked_by_parent");
  });

  test("grandchild blocked when grandparent is blocked", () => {
    const tasks = [
      createTask({ id: "grandparent", status: "blocked", depends_on: [] }),
      createTask({
        id: "parent",
        status: "active",
        depends_on: [],
        parent_id: "grandparent",
      }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    const child = result.tasks.find((t) => t.id === "child");
    expect(child?.classification).toBe("blocked_by_parent");
    expect(child?.parent_chain).toContain("grandparent");
  });

  test("builds parent chain correctly", () => {
    const tasks = [
      createTask({ id: "grandparent", status: "active", depends_on: [] }),
      createTask({
        id: "parent",
        status: "active",
        depends_on: [],
        parent_id: "grandparent",
      }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    const child = result.tasks.find((t) => t.id === "child");
    expect(child?.parent_chain).toEqual(["parent", "grandparent"]);
  });
});

// =============================================================================
// Priority Sorting Tests
// =============================================================================

describe("sortByPriority", () => {
  test("sorts high before medium before low", () => {
    const tasks = [
      createTask({ id: "low", priority: "low" }),
      createTask({ id: "high", priority: "high" }),
      createTask({ id: "medium", priority: "medium" }),
    ];
    const result = resolveDependencies(tasks);
    const sorted = sortByPriority(result.tasks);

    expect(sorted[0].id).toBe("high");
    expect(sorted[1].id).toBe("medium");
    expect(sorted[2].id).toBe("low");
  });

  test("sorts by created date within same priority", () => {
    const tasks = [
      createTask({ id: "newer", priority: "high", created: "2024-01-02" }),
      createTask({ id: "older", priority: "high", created: "2024-01-01" }),
    ];
    const result = resolveDependencies(tasks);
    const sorted = sortByPriority(result.tasks);

    expect(sorted[0].id).toBe("older");
    expect(sorted[1].id).toBe("newer");
  });

  test("handles empty list", () => {
    const sorted = sortByPriority([]);
    expect(sorted).toEqual([]);
  });
});

// =============================================================================
// Utility Function Tests
// =============================================================================

describe("getReadyTasks", () => {
  test("returns only ready tasks", () => {
    const tasks = [
      createTask({ id: "ready", status: "pending", depends_on: [] }),
      createTask({ id: "waiting", status: "pending", depends_on: ["ready"] }),
      createTask({ id: "completed", status: "completed", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);
    const ready = getReadyTasks(result);

    expect(ready.length).toBe(1);
    expect(ready[0].id).toBe("ready");
  });

  test("returns tasks sorted by priority", () => {
    const tasks = [
      createTask({ id: "low", status: "pending", priority: "low", depends_on: [] }),
      createTask({ id: "high", status: "pending", priority: "high", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);
    const ready = getReadyTasks(result);

    expect(ready[0].id).toBe("high");
    expect(ready[1].id).toBe("low");
  });
});

describe("getWaitingTasks", () => {
  test("returns waiting and waiting_on_parent tasks", () => {
    const tasks = [
      createTask({ id: "ready", status: "pending", depends_on: [] }),
      createTask({ id: "waiting", status: "pending", depends_on: ["ready"] }),
      createTask({ id: "parent", status: "pending", depends_on: [] }),
      createTask({
        id: "waitingOnParent",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);
    const waiting = getWaitingTasks(result);

    expect(waiting.length).toBe(2);
    const ids = waiting.map((t) => t.id);
    expect(ids).toContain("waiting");
    expect(ids).toContain("waitingOnParent");
  });
});

describe("getBlockedTasks", () => {
  test("returns blocked and blocked_by_parent tasks", () => {
    const tasks = [
      createTask({ id: "blocker", status: "blocked", depends_on: [] }),
      createTask({ id: "blocked", status: "pending", depends_on: ["blocker"] }),
      createTask({ id: "blockedParent", status: "blocked", depends_on: [] }),
      createTask({
        id: "blockedByParent",
        status: "pending",
        depends_on: [],
        parent_id: "blockedParent",
      }),
    ];
    const result = resolveDependencies(tasks);
    const blocked = getBlockedTasks(result);

    expect(blocked.length).toBe(2);
    const ids = blocked.map((t) => t.id);
    expect(ids).toContain("blocked");
    expect(ids).toContain("blockedByParent");
  });
});

describe("getNextTask", () => {
  test("returns highest priority ready task", () => {
    const tasks = [
      createTask({ id: "low", status: "pending", priority: "low", depends_on: [] }),
      createTask({ id: "high", status: "pending", priority: "high", depends_on: [] }),
      createTask({ id: "medium", status: "pending", priority: "medium", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);
    const next = getNextTask(result);

    expect(next?.id).toBe("high");
  });

  test("returns null when no ready tasks", () => {
    const tasks = [
      createTask({ id: "a", status: "pending", depends_on: ["b"] }),
      createTask({ id: "b", status: "pending", depends_on: ["a"] }),
    ];
    const result = resolveDependencies(tasks);
    const next = getNextTask(result);

    expect(next).toBeNull();
  });

  test("returns null for empty task list", () => {
    const result = resolveDependencies([]);
    const next = getNextTask(result);

    expect(next).toBeNull();
  });
});

// =============================================================================
// Stats Tests
// =============================================================================

describe("stats calculation", () => {
  test("calculates correct stats", () => {
    const tasks = [
      createTask({ id: "ready1", status: "pending", depends_on: [] }),
      createTask({ id: "ready2", status: "pending", depends_on: [] }),
      createTask({ id: "waiting", status: "pending", depends_on: ["ready1"] }),
      createTask({ id: "blocked", status: "pending", depends_on: ["blockedDep"] }),
      createTask({ id: "blockedDep", status: "blocked", depends_on: [] }),
      createTask({ id: "completed", status: "completed", depends_on: [] }),
    ];
    const result = resolveDependencies(tasks);

    expect(result.stats.total).toBe(6);
    expect(result.stats.ready).toBe(2);
    expect(result.stats.waiting).toBe(1);
    // blocked: only pending tasks that are blocked count (blockedDep is not_pending)
    expect(result.stats.blocked).toBe(1);
    // not_pending: blockedDep (status=blocked) + completed (status=completed)
    expect(result.stats.not_pending).toBe(2);
  });

  test("counts waiting_on_parent as waiting", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending", depends_on: [] }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    expect(result.stats.waiting).toBe(1); // child is waiting_on_parent
  });

  test("counts blocked_by_parent as blocked", () => {
    const tasks = [
      createTask({ id: "parent", status: "blocked", depends_on: [] }),
      createTask({
        id: "child",
        status: "pending",
        depends_on: [],
        parent_id: "parent",
      }),
    ];
    const result = resolveDependencies(tasks);

    expect(result.stats.blocked).toBe(1); // child is blocked_by_parent
  });
});
