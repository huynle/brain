/**
 * Unit tests for task hierarchy resolution (parent_id model)
 */

import { describe, test, expect } from "bun:test";
import {
  buildLookupMaps,
  buildParentChildMap,
  getChildren,
  getParent,
  classifyTask,
  resolveTasks,
  resolveDependencies,
  sortByPriority,
  getReadyTasks,
  getWaitingTasks,
  getBlockedTasks,
  getNextTask,
  buildTaskTree,
  flattenTaskTree,
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
    parent_id: undefined,

    created: "2024-01-01",
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    user_original_request: null,
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

// =============================================================================
// Parent-Child Map Tests
// =============================================================================

describe("buildParentChildMap", () => {
  test("groups tasks by parent_id", () => {
    const tasks = [
      createTask({ id: "root", parent_id: undefined }),
      createTask({ id: "child1", parent_id: "root" }),
      createTask({ id: "child2", parent_id: "root" }),
      createTask({ id: "grandchild", parent_id: "child1" }),
    ];
    const map = buildParentChildMap(tasks);

    // Root tasks (no parent)
    const rootTasks = map.get(null);
    expect(rootTasks?.length).toBe(1);
    expect(rootTasks?.[0].id).toBe("root");

    // Children of root
    const rootChildren = map.get("root");
    expect(rootChildren?.length).toBe(2);
    expect(rootChildren?.map(t => t.id).sort()).toEqual(["child1", "child2"]);

    // Children of child1
    const child1Children = map.get("child1");
    expect(child1Children?.length).toBe(1);
    expect(child1Children?.[0].id).toBe("grandchild");
  });

  test("handles empty task list", () => {
    const map = buildParentChildMap([]);
    expect(map.size).toBe(0);
  });

  test("handles all root tasks", () => {
    const tasks = [
      createTask({ id: "a", parent_id: undefined }),
      createTask({ id: "b", parent_id: undefined }),
    ];
    const map = buildParentChildMap(tasks);

    const rootTasks = map.get(null);
    expect(rootTasks?.length).toBe(2);
  });
});

describe("getChildren", () => {
  test("returns children of a task", () => {
    const tasks = [
      createTask({ id: "parent", parent_id: undefined }),
      createTask({ id: "child1", parent_id: "parent" }),
      createTask({ id: "child2", parent_id: "parent" }),
      createTask({ id: "other", parent_id: undefined }),
    ];

    const children = getChildren("parent", tasks);
    expect(children.length).toBe(2);
    expect(children.map(c => c.id).sort()).toEqual(["child1", "child2"]);
  });

  test("returns empty array for leaf task", () => {
    const tasks = [
      createTask({ id: "parent", parent_id: undefined }),
      createTask({ id: "leaf", parent_id: "parent" }),
    ];

    const children = getChildren("leaf", tasks);
    expect(children.length).toBe(0);
  });
});

describe("getParent", () => {
  test("returns parent of a task", () => {
    const tasks = [
      createTask({ id: "parent", title: "Parent Task", parent_id: undefined }),
      createTask({ id: "child", parent_id: "parent" }),
    ];
    const maps = buildLookupMaps(tasks);

    const parent = getParent("child", maps);
    expect(parent?.id).toBe("parent");
    expect(parent?.title).toBe("Parent Task");
  });

  test("returns null for root task", () => {
    const tasks = [
      createTask({ id: "root", parent_id: undefined }),
    ];
    const maps = buildLookupMaps(tasks);

    const parent = getParent("root", maps);
    expect(parent).toBeNull();
  });

  test("returns null for missing parent", () => {
    const tasks = [
      createTask({ id: "orphan", parent_id: "nonexistent" }),
    ];
    const maps = buildLookupMaps(tasks);

    const parent = getParent("orphan", maps);
    expect(parent).toBeNull();
  });
});

// =============================================================================
// Classification Tests
// =============================================================================

describe("classifyTask", () => {
  test("classifies non-pending task as not_pending", () => {
    const task = createTask({ id: "a", status: "completed" });
    const maps = buildLookupMaps([task]);

    const result = classifyTask(task, [], maps);

    expect(result.classification).toBe("not_pending");
  });

  test("classifies leaf task (no children) as ready", () => {
    const task = createTask({ id: "a", status: "pending" });
    const maps = buildLookupMaps([task]);

    const result = classifyTask(task, [], maps);

    expect(result.classification).toBe("ready");
  });

  test("classifies task with incomplete children as waiting", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
    ];
    const maps = buildLookupMaps(tasks);

    const result = classifyTask(tasks[0], ["child"], maps);

    expect(result.classification).toBe("waiting");
  });

  test("classifies task with all completed children as ready", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child1", status: "completed", parent_id: "parent" }),
      createTask({ id: "child2", status: "validated", parent_id: "parent" }),
    ];
    const maps = buildLookupMaps(tasks);

    const result = classifyTask(tasks[0], ["child1", "child2"], maps);

    expect(result.classification).toBe("ready");
  });

  test("classifies task with blocked parent as blocked", () => {
    const tasks = [
      createTask({ id: "parent", status: "blocked" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
    ];
    const maps = buildLookupMaps(tasks);

    const result = classifyTask(tasks[1], [], maps);

    expect(result.classification).toBe("blocked");
    expect(result.blockedBy).toBe("parent");
    expect(result.reason).toBe("parent_blocked");
  });

  test("classifies task with cancelled parent as blocked", () => {
    const tasks = [
      createTask({ id: "parent", status: "cancelled" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
    ];
    const maps = buildLookupMaps(tasks);

    const result = classifyTask(tasks[1], [], maps);

    expect(result.classification).toBe("blocked");
    expect(result.blockedBy).toBe("parent");
    expect(result.reason).toBe("parent_cancelled");
  });
});

// =============================================================================
// Main Resolution Function Tests
// =============================================================================

describe("resolveTasks", () => {
  test("handles empty task list", () => {
    const result = resolveTasks([]);

    expect(result.tasks).toEqual([]);
    expect(result.stats.total).toBe(0);
  });

  test("classifies leaf task as ready", () => {
    const tasks = [createTask({ id: "a", status: "pending" })];
    const result = resolveTasks(tasks);

    expect(result.tasks[0].classification).toBe("ready");
    expect(result.tasks[0].children_ids).toEqual([]);
  });

  test("computes children_ids correctly", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child1", status: "pending", parent_id: "parent" }),
      createTask({ id: "child2", status: "pending", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const parent = result.tasks.find(t => t.id === "parent");
    expect(parent?.children_ids.sort()).toEqual(["child1", "child2"]);
  });

  test("classifies parent with pending children as waiting", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const parent = result.tasks.find(t => t.id === "parent");
    expect(parent?.classification).toBe("waiting");
  });

  test("classifies parent with completed children as ready", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child", status: "completed", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const parent = result.tasks.find(t => t.id === "parent");
    expect(parent?.classification).toBe("ready");
  });

  test("classifies non-pending task as not_pending", () => {
    const tasks = [createTask({ id: "a", status: "completed" })];
    const result = resolveTasks(tasks);

    expect(result.tasks[0].classification).toBe("not_pending");
  });
});

describe("resolveDependencies (alias)", () => {
  test("is an alias for resolveTasks", () => {
    const tasks = [createTask({ id: "a", status: "pending" })];
    const result1 = resolveTasks(tasks);
    const result2 = resolveDependencies(tasks);

    expect(result1.tasks[0].id).toBe(result2.tasks[0].id);
    expect(result1.tasks[0].classification).toBe(result2.tasks[0].classification);
  });
});

// =============================================================================
// Parent-Child Classification Tests
// =============================================================================

describe("parent-child classification", () => {
  test("leaf tasks (no children) are ready", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "leaf", status: "pending", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const leaf = result.tasks.find(t => t.id === "leaf");
    expect(leaf?.classification).toBe("ready");
  });

  test("parent with incomplete child is waiting", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const parent = result.tasks.find(t => t.id === "parent");
    expect(parent?.classification).toBe("waiting");
  });

  test("parent with all children completed is ready", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child1", status: "completed", parent_id: "parent" }),
      createTask({ id: "child2", status: "validated", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const parent = result.tasks.find(t => t.id === "parent");
    expect(parent?.classification).toBe("ready");
  });

  test("child with blocked parent is blocked", () => {
    const tasks = [
      createTask({ id: "parent", status: "blocked" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const child = result.tasks.find(t => t.id === "child");
    expect(child?.classification).toBe("blocked");
    expect(child?.blocked_by_reason).toBe("parent_blocked");
  });

  test("siblings are both ready (parallel execution)", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "sibling1", status: "pending", parent_id: "parent" }),
      createTask({ id: "sibling2", status: "pending", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);

    const sibling1 = result.tasks.find(t => t.id === "sibling1");
    const sibling2 = result.tasks.find(t => t.id === "sibling2");
    expect(sibling1?.classification).toBe("ready");
    expect(sibling2?.classification).toBe("ready");
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
    const result = resolveTasks(tasks);
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
    const result = resolveTasks(tasks);
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
      createTask({ id: "ready", status: "pending" }),
      createTask({ id: "waiting", status: "pending" }),
      createTask({ id: "child", status: "pending", parent_id: "waiting" }),
      createTask({ id: "completed", status: "completed" }),
    ];
    const result = resolveTasks(tasks);
    const ready = getReadyTasks(result);

    // "ready" is a leaf task with no children
    // "waiting" has an incomplete child, so it's waiting
    // "child" is a leaf task, so it's ready
    // "completed" is not_pending
    expect(ready.map(t => t.id).sort()).toEqual(["child", "ready"]);
  });

  test("returns tasks sorted by priority", () => {
    const tasks = [
      createTask({ id: "low", status: "pending", priority: "low" }),
      createTask({ id: "high", status: "pending", priority: "high" }),
    ];
    const result = resolveTasks(tasks);
    const ready = getReadyTasks(result);

    expect(ready[0].id).toBe("high");
    expect(ready[1].id).toBe("low");
  });
});

describe("getWaitingTasks", () => {
  test("returns waiting tasks", () => {
    const tasks = [
      createTask({ id: "parent1", status: "pending" }),
      createTask({ id: "child1", status: "pending", parent_id: "parent1" }),
      createTask({ id: "parent2", status: "pending" }),
      createTask({ id: "child2", status: "pending", parent_id: "parent2" }),
    ];
    const result = resolveTasks(tasks);
    const waiting = getWaitingTasks(result);

    // Parents with incomplete children are waiting
    expect(waiting.length).toBe(2);
    expect(waiting.map(t => t.id).sort()).toEqual(["parent1", "parent2"]);
  });
});

describe("getBlockedTasks", () => {
  test("returns blocked tasks", () => {
    const tasks = [
      createTask({ id: "blockedParent", status: "blocked" }),
      createTask({ id: "child1", status: "pending", parent_id: "blockedParent" }),
      createTask({ id: "child2", status: "pending", parent_id: "blockedParent" }),
    ];
    const result = resolveTasks(tasks);
    const blocked = getBlockedTasks(result);

    // Children of blocked parent are blocked
    expect(blocked.length).toBe(2);
    expect(blocked.map(t => t.id).sort()).toEqual(["child1", "child2"]);
  });
});

describe("getNextTask", () => {
  test("returns highest priority ready task", () => {
    const tasks = [
      createTask({ id: "low", status: "pending", priority: "low" }),
      createTask({ id: "high", status: "pending", priority: "high" }),
      createTask({ id: "medium", status: "pending", priority: "medium" }),
    ];
    const result = resolveTasks(tasks);
    const next = getNextTask(result);

    expect(next?.id).toBe("high");
  });

  test("returns null when no ready tasks", () => {
    const tasks = [
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child", status: "in_progress", parent_id: "parent" }),
    ];
    const result = resolveTasks(tasks);
    const next = getNextTask(result);

    // Parent is waiting (has in_progress child), child is not_pending
    expect(next).toBeNull();
  });

  test("returns null for empty task list", () => {
    const result = resolveTasks([]);
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
      createTask({ id: "ready1", status: "pending" }),
      createTask({ id: "ready2", status: "pending" }),
      createTask({ id: "parent", status: "pending" }),
      createTask({ id: "child", status: "pending", parent_id: "parent" }),
      createTask({ id: "blockedParent", status: "blocked" }),
      createTask({ id: "blockedChild", status: "pending", parent_id: "blockedParent" }),
      createTask({ id: "completed", status: "completed" }),
    ];
    const result = resolveTasks(tasks);

    expect(result.stats.total).toBe(7);
    // ready: ready1, ready2, child (all leaf tasks)
    expect(result.stats.ready).toBe(3);
    // waiting: parent (has pending child)
    expect(result.stats.waiting).toBe(1);
    // blocked: blockedChild (has blocked parent)
    expect(result.stats.blocked).toBe(1);
    // not_pending: blockedParent (blocked), completed (completed)
    expect(result.stats.not_pending).toBe(2);
  });
});

// =============================================================================
// Tree Building Tests
// =============================================================================

describe("buildTaskTree", () => {
  test("builds tree from tasks", () => {
    const tasks = [
      createTask({ id: "root1", status: "pending" }),
      createTask({ id: "child1", status: "pending", parent_id: "root1" }),
      createTask({ id: "child2", status: "pending", parent_id: "root1" }),
      createTask({ id: "grandchild", status: "pending", parent_id: "child1" }),
      createTask({ id: "root2", status: "pending" }),
    ];
    const result = resolveTasks(tasks);
    const tree = buildTaskTree(result.tasks);

    // Should have 2 root nodes
    expect(tree.length).toBe(2);

    // Find root1
    const root1 = tree.find(n => n.task.id === "root1");
    expect(root1).toBeDefined();
    expect(root1?.children.length).toBe(2);

    // Find child1 under root1
    const child1 = root1?.children.find(n => n.task.id === "child1");
    expect(child1).toBeDefined();
    expect(child1?.children.length).toBe(1);
    expect(child1?.children[0].task.id).toBe("grandchild");
  });

  test("handles empty task list", () => {
    const tree = buildTaskTree([]);
    expect(tree).toEqual([]);
  });
});

describe("flattenTaskTree", () => {
  test("flattens tree with correct depth", () => {
    const tasks = [
      createTask({ id: "root", status: "pending" }),
      createTask({ id: "child", status: "pending", parent_id: "root" }),
      createTask({ id: "grandchild", status: "pending", parent_id: "child" }),
    ];
    const result = resolveTasks(tasks);
    const tree = buildTaskTree(result.tasks);
    const flat = flattenTaskTree(tree);

    expect(flat.length).toBe(3);
    expect(flat[0].task.id).toBe("root");
    expect(flat[0].depth).toBe(0);
    expect(flat[1].task.id).toBe("child");
    expect(flat[1].depth).toBe(1);
    expect(flat[2].task.id).toBe("grandchild");
    expect(flat[2].depth).toBe(2);
  });

  test("handles empty tree", () => {
    const flat = flattenTaskTree([]);
    expect(flat).toEqual([]);
  });
});
