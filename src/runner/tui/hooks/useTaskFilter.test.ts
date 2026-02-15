/**
 * useTaskFilter Hook Tests
 *
 * Tests the task filter hook including:
 * - Filter matching logic (title, feature_id, tags)
 * - Navigation order calculation
 * 
 * Note: Since React hooks require a React context, we test the core logic
 * (taskMatchesFilter) directly and test the hook behavior via component tests.
 */

import { describe, it, expect } from "bun:test";
import type { TaskDisplay } from "../types";

// Import the hook to test exports
import { useTaskFilter, taskMatchesFilter, type FilterMode } from "./useTaskFilter";

// =============================================================================
// Test Helpers
// =============================================================================

function createTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  return {
    id: overrides.id || "task-1",
    path: overrides.path || "path/to/task",
    title: overrides.title || "Test Task",
    status: overrides.status || "pending",
    priority: overrides.priority || "medium",
    tags: overrides.tags || [],
    dependencies: overrides.dependencies || [],
    dependents: overrides.dependents || [],
    dependencyTitles: overrides.dependencyTitles || [],
    dependentTitles: overrides.dependentTitles || [],
    feature_id: overrides.feature_id,
    ...overrides,
  };
}

// =============================================================================
// Filter Matching Tests (Pure Function)
// =============================================================================

describe("taskMatchesFilter - Title Matching", () => {
  it("should match exact title", () => {
    const task = createTask({ title: "Fix authentication bug" });
    expect(taskMatchesFilter(task, "Fix authentication bug")).toBe(true);
  });

  it("should match partial title", () => {
    const task = createTask({ title: "Fix authentication bug" });
    expect(taskMatchesFilter(task, "authentication")).toBe(true);
  });

  it("should match title case-insensitively", () => {
    const task = createTask({ title: "Fix Authentication Bug" });
    expect(taskMatchesFilter(task, "authentication")).toBe(true);
    expect(taskMatchesFilter(task, "AUTHENTICATION")).toBe(true);
    expect(taskMatchesFilter(task, "AuThEnTiCaTiOn")).toBe(true);
  });

  it("should not match non-matching title", () => {
    const task = createTask({ title: "Fix authentication bug" });
    expect(taskMatchesFilter(task, "database")).toBe(false);
  });

  it("should return true for empty filter", () => {
    const task = createTask({ title: "Any task" });
    expect(taskMatchesFilter(task, "")).toBe(true);
  });
});

describe("taskMatchesFilter - Feature ID Matching", () => {
  it("should match feature_id", () => {
    const task = createTask({ title: "Some task", feature_id: "auth-system" });
    expect(taskMatchesFilter(task, "auth-system")).toBe(true);
  });

  it("should match partial feature_id", () => {
    const task = createTask({ title: "Some task", feature_id: "auth-system" });
    expect(taskMatchesFilter(task, "auth")).toBe(true);
    expect(taskMatchesFilter(task, "system")).toBe(true);
  });

  it("should match feature_id case-insensitively", () => {
    const task = createTask({ title: "Some task", feature_id: "Auth-System" });
    expect(taskMatchesFilter(task, "auth-system")).toBe(true);
    expect(taskMatchesFilter(task, "AUTH-SYSTEM")).toBe(true);
  });

  it("should handle undefined feature_id", () => {
    const task = createTask({ title: "Some task", feature_id: undefined });
    expect(taskMatchesFilter(task, "auth")).toBe(false);
  });
});

describe("taskMatchesFilter - Tag Matching", () => {
  it("should match single tag", () => {
    const task = createTask({ title: "Some task", tags: ["bug"] });
    expect(taskMatchesFilter(task, "bug")).toBe(true);
  });

  it("should match one of multiple tags", () => {
    const task = createTask({ title: "Some task", tags: ["bug", "urgent", "p0"] });
    expect(taskMatchesFilter(task, "bug")).toBe(true);
    expect(taskMatchesFilter(task, "urgent")).toBe(true);
    expect(taskMatchesFilter(task, "p0")).toBe(true);
  });

  it("should match partial tag", () => {
    const task = createTask({ title: "Some task", tags: ["authentication"] });
    expect(taskMatchesFilter(task, "auth")).toBe(true);
  });

  it("should match tag case-insensitively", () => {
    const task = createTask({ title: "Some task", tags: ["URGENT"] });
    expect(taskMatchesFilter(task, "urgent")).toBe(true);
    expect(taskMatchesFilter(task, "Urgent")).toBe(true);
  });

  it("should handle empty tags array", () => {
    const task = createTask({ title: "Some task", tags: [] });
    expect(taskMatchesFilter(task, "tag")).toBe(false);
  });
});

describe("taskMatchesFilter - Combined Matching", () => {
  it("should match across title, feature_id, and tags", () => {
    const task = createTask({
      title: "Fix login page",
      feature_id: "auth-module",
      tags: ["bug", "critical"],
    });

    // Match title
    expect(taskMatchesFilter(task, "login")).toBe(true);
    
    // Match feature_id
    expect(taskMatchesFilter(task, "auth-module")).toBe(true);
    
    // Match tag
    expect(taskMatchesFilter(task, "critical")).toBe(true);
  });

  it("should match if any field matches", () => {
    const task = createTask({
      title: "Database migration",
      feature_id: "db-upgrade",
      tags: ["migration", "schema"],
    });

    // Title doesn't match "auth" but feature_id might have it? No - nothing matches
    expect(taskMatchesFilter(task, "auth")).toBe(false);
    
    // But "migration" matches both title and tags
    expect(taskMatchesFilter(task, "migration")).toBe(true);
  });
});

describe("taskMatchesFilter - Special Characters", () => {
  it("should handle special characters in filter", () => {
    const task = createTask({ title: "Fix bug #123" });
    expect(taskMatchesFilter(task, "#123")).toBe(true);
  });

  it("should handle version numbers in tags", () => {
    const task = createTask({ title: "Release", tags: ["v1.0.0", "v2.0"] });
    expect(taskMatchesFilter(task, "v1.0")).toBe(true);
    expect(taskMatchesFilter(task, "1.0.0")).toBe(true);
  });

  it("should handle hyphens", () => {
    const task = createTask({ title: "Some task", feature_id: "my-cool-feature" });
    expect(taskMatchesFilter(task, "cool-feature")).toBe(true);
  });

  it("should handle underscores", () => {
    const task = createTask({ title: "Some task", feature_id: "my_cool_feature" });
    expect(taskMatchesFilter(task, "cool_feature")).toBe(true);
  });
});

// =============================================================================
// Filter Multiple Tasks Tests
// =============================================================================

describe("filterTasks - Multiple Tasks", () => {
  const tasks = [
    createTask({ id: "1", title: "Fix authentication bug", feature_id: "auth", tags: ["bug", "urgent"] }),
    createTask({ id: "2", title: "Add login feature", feature_id: "auth", tags: ["feature"] }),
    createTask({ id: "3", title: "Update database schema", feature_id: "database", tags: ["migration"] }),
    createTask({ id: "4", title: "Write documentation", feature_id: undefined, tags: ["docs"] }),
  ];

  it("should return all tasks when filter is empty", () => {
    const filtered = tasks.filter(t => taskMatchesFilter(t, ""));
    expect(filtered.length).toBe(4);
  });

  it("should filter by title", () => {
    const filtered = tasks.filter(t => taskMatchesFilter(t, "authentication"));
    expect(filtered.length).toBe(1);
    expect(filtered[0].id).toBe("1");
  });

  it("should filter by feature_id and find multiple matches", () => {
    const filtered = tasks.filter(t => taskMatchesFilter(t, "auth"));
    expect(filtered.length).toBe(2);
    expect(filtered.map(t => t.id)).toContain("1");
    expect(filtered.map(t => t.id)).toContain("2");
  });

  it("should filter by tag", () => {
    const filtered = tasks.filter(t => taskMatchesFilter(t, "urgent"));
    expect(filtered.length).toBe(1);
    expect(filtered[0].id).toBe("1");
  });

  it("should return empty when no match", () => {
    const filtered = tasks.filter(t => taskMatchesFilter(t, "nonexistent"));
    expect(filtered.length).toBe(0);
  });
});

// =============================================================================
// Edge Cases
// =============================================================================

describe("taskMatchesFilter - Edge Cases", () => {
  it("should handle empty task title", () => {
    const task = createTask({ title: "" });
    expect(taskMatchesFilter(task, "test")).toBe(false);
    expect(taskMatchesFilter(task, "")).toBe(true);
  });

  it("should handle whitespace in filter", () => {
    const task = createTask({ title: "Fix authentication bug" });
    expect(taskMatchesFilter(task, "authentication bug")).toBe(true);
    // Leading space matches because "fix authentication" contains " authentication" substring
    expect(taskMatchesFilter(task, " authentication")).toBe(true);
  });

  it("should handle unicode characters", () => {
    const task = createTask({ title: "Fix bug - Japanese: \u65E5\u672C\u8A9E" });
    expect(taskMatchesFilter(task, "\u65E5\u672C")).toBe(true);
  });

  it("should handle very long filter text", () => {
    const task = createTask({ title: "Short" });
    const longFilter = "a".repeat(1000);
    expect(taskMatchesFilter(task, longFilter)).toBe(false);
  });
});

// =============================================================================
// Hook Type Tests
// =============================================================================

describe("useTaskFilter - Type Exports", () => {
  it("should export FilterMode type", () => {
    // Type-level test - if this compiles, the type is exported
    const mode: FilterMode = "off";
    expect(mode).toBe("off");
  });

  it("should export useTaskFilter function", () => {
    expect(typeof useTaskFilter).toBe("function");
  });

  it("should export taskMatchesFilter function", () => {
    expect(typeof taskMatchesFilter).toBe("function");
  });
});

// =============================================================================
// visibleGroups Filtering Tests
// =============================================================================

describe("visibleGroups - Status Visibility Filtering", () => {
  const tasks = [
    createTask({ id: "1", title: "Draft task", status: "draft" }),
    createTask({ id: "2", title: "Pending task", status: "pending" }),
    createTask({ id: "3", title: "Active task", status: "active" }),
    createTask({ id: "4", title: "In progress task", status: "in_progress" }),
    createTask({ id: "5", title: "Blocked task", status: "blocked" }),
    createTask({ id: "6", title: "Completed task", status: "completed" }),
    createTask({ id: "7", title: "Validated task", status: "validated" }),
    createTask({ id: "8", title: "Cancelled task", status: "cancelled" }),
    createTask({ id: "9", title: "Superseded task", status: "superseded" }),
    createTask({ id: "10", title: "Archived task", status: "archived" }),
  ];

  it("should filter tasks by visibleGroups - show only pending and active", () => {
    const visibleGroups = new Set(["pending", "active"]);
    const filtered = tasks.filter(t => visibleGroups.has(t.status));
    expect(filtered.length).toBe(2);
    expect(filtered.map(t => t.id)).toContain("2");
    expect(filtered.map(t => t.id)).toContain("3");
  });

  it("should show all tasks when visibleGroups includes all statuses", () => {
    const visibleGroups = new Set([
      "draft", "pending", "active", "in_progress", "blocked",
      "completed", "validated", "cancelled", "superseded", "archived"
    ]);
    const filtered = tasks.filter(t => visibleGroups.has(t.status));
    expect(filtered.length).toBe(10);
  });

  it("should show no tasks when visibleGroups is empty", () => {
    const visibleGroups = new Set<string>();
    const filtered = tasks.filter(t => visibleGroups.has(t.status));
    expect(filtered.length).toBe(0);
  });

  it("should filter out cancelled, validated, superseded, archived by default settings", () => {
    // Default visible groups from useSettingsStorage
    const visibleGroups = new Set(["draft", "pending", "active", "in_progress", "blocked", "completed"]);
    const filtered = tasks.filter(t => visibleGroups.has(t.status));
    expect(filtered.length).toBe(6);
    // Should NOT include these statuses
    expect(filtered.map(t => t.status)).not.toContain("cancelled");
    expect(filtered.map(t => t.status)).not.toContain("validated");
    expect(filtered.map(t => t.status)).not.toContain("superseded");
    expect(filtered.map(t => t.status)).not.toContain("archived");
  });
});

describe("visibleGroups - Combined with Text Filter", () => {
  const tasks = [
    createTask({ id: "1", title: "Auth draft", status: "draft" }),
    createTask({ id: "2", title: "Auth pending", status: "pending" }),
    createTask({ id: "3", title: "Database pending", status: "pending" }),
    createTask({ id: "4", title: "Auth completed", status: "completed" }),
    createTask({ id: "5", title: "Auth cancelled", status: "cancelled" }),
  ];

  it("should apply both visibleGroups AND text filter (AND logic)", () => {
    const visibleGroups = new Set(["pending", "completed"]);
    // First filter by visibleGroups, then by text
    const filtered = tasks
      .filter(t => visibleGroups.has(t.status))
      .filter(t => taskMatchesFilter(t, "auth"));
    
    expect(filtered.length).toBe(2);
    expect(filtered.map(t => t.id)).toContain("2"); // Auth pending
    expect(filtered.map(t => t.id)).toContain("4"); // Auth completed
    // Should NOT include Auth draft (wrong status) or Auth cancelled (wrong status)
    expect(filtered.map(t => t.id)).not.toContain("1");
    expect(filtered.map(t => t.id)).not.toContain("5");
  });

  it("should return empty when text matches but status is hidden", () => {
    const visibleGroups = new Set(["pending"]);
    const filtered = tasks
      .filter(t => visibleGroups.has(t.status))
      .filter(t => taskMatchesFilter(t, "cancelled"));
    
    expect(filtered.length).toBe(0);
  });
});

describe("visibleGroups - undefined means show everything", () => {
  const tasks = [
    createTask({ id: "1", title: "Draft task", status: "draft" }),
    createTask({ id: "2", title: "Cancelled task", status: "cancelled" }),
    createTask({ id: "3", title: "Archived task", status: "archived" }),
  ];

  it("should show all tasks when visibleGroups is undefined (backward compatibility)", () => {
    // Simulate the filtering logic when visibleGroups is undefined
    // When undefined, don't filter by status - show everything
    const filterByVisibleGroups = (taskList: typeof tasks, groups?: Set<string>) => {
      if (!groups) return taskList;
      return taskList.filter(t => groups.has(t.status));
    };
    
    const filtered = filterByVisibleGroups(tasks, undefined);
    expect(filtered.length).toBe(3);
  });
});

// =============================================================================
// UseTaskFilterOptions Interface Tests
// =============================================================================

describe("UseTaskFilterOptions - visibleGroups option", () => {
  it("should accept visibleGroups in options interface", () => {
    // This test verifies the interface accepts visibleGroups
    // It will fail to compile if visibleGroups is not in UseTaskFilterOptions
    const options: Parameters<typeof useTaskFilter>[0] = {
      tasks: [],
      visibleGroups: new Set(["pending", "active"]),
    };
    expect(options.visibleGroups).toBeDefined();
    expect(options.visibleGroups?.has("pending")).toBe(true);
  });
});
