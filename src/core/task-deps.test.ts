import { describe, expect, test } from "bun:test";
import { getDownstreamTasks } from "./task-deps";
import type { Task } from "./types";

/**
 * Helper to create a minimal Task for testing.
 * Only id, depends_on, and required fields are set.
 */
function makeTask(id: string, depends_on: string[] = []): Task {
  return {
    id,
    path: `projects/test/task/${id}.md`,
    title: `Task ${id}`,
    priority: "medium",
    status: "pending",
    depends_on,
    tags: [],
    created: "2026-01-01T00:00:00Z",
    target_workdir: null,
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    user_original_request: null,
    direct_prompt: null,
    agent: null,
    model: null,
    sessions: {},
  };
}

describe("getDownstreamTasks", () => {
  test("single chain: A -> B -> C returns all in topological order", () => {
    const tasks = [
      makeTask("A"),
      makeTask("B", ["A"]),
      makeTask("C", ["B"]),
    ];

    const result = getDownstreamTasks("A", tasks);
    const ids = result.map((t) => t.id);

    expect(ids).toEqual(["A", "B", "C"]);
  });

  test("diamond: A -> B,C -> D returns all in valid topological order", () => {
    const tasks = [
      makeTask("A"),
      makeTask("B", ["A"]),
      makeTask("C", ["A"]),
      makeTask("D", ["B", "C"]),
    ];

    const result = getDownstreamTasks("A", tasks);
    const ids = result.map((t) => t.id);

    // A must be first, D must be last, B and C in middle
    expect(ids[0]).toBe("A");
    expect(ids[ids.length - 1]).toBe("D");
    expect(ids).toContain("B");
    expect(ids).toContain("C");
    expect(ids.length).toBe(4);
  });

  test("independent tasks are excluded", () => {
    const tasks = [
      makeTask("A"),
      makeTask("B", ["A"]),
      makeTask("X"), // independent
      makeTask("Y", ["X"]), // depends on X, not A
    ];

    const result = getDownstreamTasks("A", tasks);
    const ids = result.map((t) => t.id);

    expect(ids).toEqual(["A", "B"]);
  });

  test("handles cycles gracefully without hanging", () => {
    // A -> B -> C -> A (cycle), but D depends on B
    const tasks = [
      makeTask("A", ["C"]),
      makeTask("B", ["A"]),
      makeTask("C", ["B"]),
      makeTask("D", ["B"]),
    ];

    const result = getDownstreamTasks("A", tasks);
    const ids = result.map((t) => t.id);

    // All cycle participants + D should be included
    expect(ids).toContain("A");
    expect(ids).toContain("B");
    expect(ids).toContain("C");
    expect(ids).toContain("D");
    expect(ids.length).toBe(4);
  });

  test("task with no dependents returns just the root", () => {
    const tasks = [
      makeTask("A"),
      makeTask("B"),
      makeTask("C"),
    ];

    const result = getDownstreamTasks("A", tasks);
    const ids = result.map((t) => t.id);

    expect(ids).toEqual(["A"]);
  });

  test("returns empty array for nonexistent root", () => {
    const tasks = [makeTask("A"), makeTask("B", ["A"])];
    const result = getDownstreamTasks("Z", tasks);
    expect(result).toEqual([]);
  });

  test("returns empty array for empty task list", () => {
    const result = getDownstreamTasks("A", []);
    expect(result).toEqual([]);
  });

  test("deep chain: A -> B -> C -> D -> E", () => {
    const tasks = [
      makeTask("A"),
      makeTask("B", ["A"]),
      makeTask("C", ["B"]),
      makeTask("D", ["C"]),
      makeTask("E", ["D"]),
    ];

    const result = getDownstreamTasks("A", tasks);
    const ids = result.map((t) => t.id);

    expect(ids).toEqual(["A", "B", "C", "D", "E"]);
  });

  test("starting from middle of chain: B has downstream C, not A", () => {
    const tasks = [
      makeTask("A"),
      makeTask("B", ["A"]),
      makeTask("C", ["B"]),
    ];

    const result = getDownstreamTasks("B", tasks);
    const ids = result.map((t) => t.id);

    // B is root, C depends on B. A is upstream, not downstream.
    expect(ids).toEqual(["B", "C"]);
  });
});
