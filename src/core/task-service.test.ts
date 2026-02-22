/**
 * Task Service - Unit Tests
 *
 * Tests for workdir resolution and dependency normalization.
 * Worktree paths are now derived from git_branch using deriveWorktreePath().
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir, homedir } from "os";
import { TaskService, normalizeDependencyRef, findDependents } from "./task-service";
import type { Task } from "./types";

// =============================================================================
// Test Helpers
// =============================================================================

function createTestConfig() {
  return {
    brainDir: "/tmp/test-brain",
    dbPath: "/tmp/test-brain/brain.db",
    defaultProject: "test-project",
  };
}

// =============================================================================
// Tests
// =============================================================================

describe("TaskService", () => {
  describe("resolveWorkdir()", () => {
    let service: TaskService;

    beforeEach(() => {
      service = new TaskService(createTestConfig(), "test-project");
    });

    test("returns null when neither workdir nor worktree is provided", () => {
      const result = service.resolveWorkdir(null, null);
      expect(result).toBeNull();
    });

    test("returns null when workdir path does not exist", () => {
      const result = service.resolveWorkdir("nonexistent/path/that/does/not/exist", null);
      expect(result).toBeNull();
    });

    test("returns null when git_branch has no existing worktree", () => {
      // gitBranch alone doesn't create a path - needs workdir too for derivation
      const result = service.resolveWorkdir(null, "feature/nonexistent");
      expect(result).toBeNull();
    });

    test("resolves workdir path relative to HOME", () => {
      // Create a temporary directory under home for testing
      const testWorkdir = `.test-workdir-${Date.now()}`;
      const fullPath = join(homedir(), testWorkdir);
      
      try {
        mkdirSync(fullPath, { recursive: true });
        
        const result = service.resolveWorkdir(testWorkdir, null);
        expect(result).toBe(fullPath);
      } finally {
        try { rmSync(fullPath, { recursive: true, force: true }); } catch {}
      }
    });

    test("resolves derived worktree when it exists", () => {
      // Create a workdir with a .worktrees subdirectory
      const testWorkdir = `.test-workdir-derived-${Date.now()}`;
      const workdirPath = join(homedir(), testWorkdir);
      const worktreePath = join(workdirPath, ".worktrees", "feature-auth");

      try {
        mkdirSync(worktreePath, { recursive: true });

        // When git_branch is provided and derived worktree exists, use it
        const result = service.resolveWorkdir(testWorkdir, "feature/auth");
        expect(result).toBe(worktreePath);
      } finally {
        try { rmSync(workdirPath, { recursive: true, force: true }); } catch {}
      }
    });

    test("falls back to workdir when derived worktree does not exist", () => {
      const testWorkdir = `.test-workdir-fallback-${Date.now()}`;
      const workdirPath = join(homedir(), testWorkdir);

      try {
        mkdirSync(workdirPath, { recursive: true });

        // When derived worktree doesn't exist, fall back to workdir
        const result = service.resolveWorkdir(testWorkdir, "feature/nonexistent");
        expect(result).toBe(workdirPath);
      } finally {
        try { rmSync(workdirPath, { recursive: true, force: true }); } catch {}
      }
    });

    test("returns null when both derived worktree and workdir do not exist", () => {
      const result = service.resolveWorkdir(
        "nonexistent/workdir",
        "feature/some-branch"
      );
      expect(result).toBeNull();
    });
  });

  describe("deriveWorktreePath()", () => {
    let service: TaskService;

    beforeEach(() => {
      service = new TaskService(createTestConfig(), "test-project");
    });

    test("derives worktree path with simple branch", () => {
      const result = service.deriveWorktreePath("projects/brain-api", "feature-x");
      expect(result).toBe(`${homedir()}/projects/brain-api/.worktrees/feature-x`);
    });

    test("sanitizes slashes in branch names", () => {
      const result = service.deriveWorktreePath("projects/brain-api", "feature/auth");
      expect(result).toBe(`${homedir()}/projects/brain-api/.worktrees/feature-auth`);
    });

    test("sanitizes special characters in branch names", () => {
      const result = service.deriveWorktreePath("projects/brain-api", "fix/bug#123@test");
      expect(result).toBe(`${homedir()}/projects/brain-api/.worktrees/fix-bug123test`);
    });

    test("returns null when workdir is null", () => {
      const result = service.deriveWorktreePath(null, "feature/auth");
      expect(result).toBeNull();
    });

    test("returns null when gitBranch is null", () => {
      const result = service.deriveWorktreePath("projects/brain-api", null);
      expect(result).toBeNull();
    });

    test("returns null when both are null", () => {
      const result = service.deriveWorktreePath(null, null);
      expect(result).toBeNull();
    });
  });

  describe("getOrCreateProjectRoot() - REMOVED", () => {
    // This test verifies that getOrCreateProjectRoot method has been removed
    // The project root task feature is no longer used
    test("getOrCreateProjectRoot method should not exist", () => {
      const service = new TaskService(createTestConfig(), "test-project");
      
      // The method should not exist on the service
      expect((service as unknown as Record<string, unknown>).getOrCreateProjectRoot).toBeUndefined();
    });
  });

  describe("listProjects()", () => {
    let service: TaskService;
    let testDir: string;

    beforeEach(() => {
      testDir = join(tmpdir(), `task-service-projects-test-${Date.now()}`);
      mkdirSync(testDir, { recursive: true });
      
      service = new TaskService({
        brainDir: testDir,
        dbPath: join(testDir, "brain.db"),
        defaultProject: "test-project",
      }, "test-project");
    });

    afterEach(() => {
      try {
        if (existsSync(testDir)) {
          rmSync(testDir, { recursive: true, force: true });
        }
      } catch {
        // Ignore cleanup errors
      }
    });

    test("returns empty array when projects directory does not exist", () => {
      const projects = service.listProjects();
      expect(projects).toEqual([]);
    });

    test("returns empty array when no projects have task directories", () => {
      // Create projects dir without task subdirs
      const projectsDir = join(testDir, "projects");
      mkdirSync(join(projectsDir, "proj-a"), { recursive: true });
      mkdirSync(join(projectsDir, "proj-b"), { recursive: true });
      
      const projects = service.listProjects();
      expect(projects).toEqual([]);
    });

    test("returns projects that have task directories", () => {
      // Create projects with task subdirs
      const projectsDir = join(testDir, "projects");
      mkdirSync(join(projectsDir, "proj-a", "task"), { recursive: true });
      mkdirSync(join(projectsDir, "proj-b", "task"), { recursive: true });
      mkdirSync(join(projectsDir, "proj-c"), { recursive: true }); // No task dir
      
      const projects = service.listProjects();
      expect(projects).toHaveLength(2);
      expect(projects).toContain("proj-a");
      expect(projects).toContain("proj-b");
      expect(projects).not.toContain("proj-c");
    });

    test("returns projects sorted alphabetically", () => {
      const projectsDir = join(testDir, "projects");
      mkdirSync(join(projectsDir, "zebra", "task"), { recursive: true });
      mkdirSync(join(projectsDir, "alpha", "task"), { recursive: true });
      mkdirSync(join(projectsDir, "beta", "task"), { recursive: true });
      
      const projects = service.listProjects();
      expect(projects).toEqual(["alpha", "beta", "zebra"]);
    });
  });

  describe("normalizeDependencyRef()", () => {
    test("strips .md extension", () => {
      const result = normalizeDependencyRef("1770555889709-task-name.md");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBeUndefined();
    });

    test("extracts projectId from full path", () => {
      const result = normalizeDependencyRef("projects/brain-api/task/1770555889709-task-name");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBe("brain-api");
    });

    test("extracts projectId from full path with .md extension", () => {
      const result = normalizeDependencyRef("projects/brain-api/task/1770555889709-task-name.md");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBe("brain-api");
    });

    test("extracts projectId from full path for different projects", () => {
      const result = normalizeDependencyRef("projects/pwa/task/l60p1j59.md");
      expect(result.normalized).toBe("l60p1j59");
      expect(result.projectId).toBe("pwa");
    });

    test("extracts projectId from full path without .md", () => {
      const result = normalizeDependencyRef("projects/other-project/task/abc123");
      expect(result.normalized).toBe("abc123");
      expect(result.projectId).toBe("other-project");
    });

    test("parses cross-project syntax", () => {
      const result = normalizeDependencyRef("other-project:1770555889709-task-name");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBe("other-project");
    });

    test("normalizes cross-project ref with .md extension", () => {
      const result = normalizeDependencyRef("other-project:1770555889709-task-name.md");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBe("other-project");
    });

    test("normalizes cross-project ref with path prefix", () => {
      const result = normalizeDependencyRef("other-project:projects/other-project/task/1770555889709-task-name");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBe("other-project");
    });

    test("handles simple task ID", () => {
      const result = normalizeDependencyRef("1770555889709-simple-task");
      expect(result.normalized).toBe("1770555889709-simple-task");
      expect(result.projectId).toBeUndefined();
    });

    test("handles task title (no normalization needed)", () => {
      const result = normalizeDependencyRef("My Task Title");
      expect(result.normalized).toBe("My Task Title");
      expect(result.projectId).toBeUndefined();
    });

    test("trims whitespace", () => {
      const result = normalizeDependencyRef("  1770555889709-task-name  ");
      expect(result.normalized).toBe("1770555889709-task-name");
    });
  });

  describe("findDependents()", () => {
    // Helper to create a minimal Task object for testing
    function makeTask(overrides: Partial<Task> & { id: string; path: string; depends_on: string[] }): Task {
      return {
        title: `Task ${overrides.id}`,
        priority: "medium",
        status: "pending",
        tags: [],
        cron_ids: [],
        created: "2025-01-01",
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
        ...overrides,
      };
    }

    test("returns empty array when no tasks depend on the given ID", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: [] }),
          makeTask({ id: "task-2", path: "projects/proj-a/task/task-2.md", depends_on: ["task-3"] }),
        ]],
      ]);

      const result = findDependents("task-99", "proj-a", tasksByProject);
      expect(result).toEqual([]);
    });

    test("finds dependents using bare ID in same project", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: ["task-2"] }),
          makeTask({ id: "task-2", path: "projects/proj-a/task/task-2.md", depends_on: [] }),
        ]],
      ]);

      const result = findDependents("task-2", "proj-a", tasksByProject);
      expect(result).toHaveLength(1);
      expect(result[0]).toEqual({
        taskId: "task-1",
        taskPath: "projects/proj-a/task/task-1.md",
        projectId: "proj-a",
        depRef: "task-2",
      });
    });

    test("finds dependents using cross-project colon syntax", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: [] }),
        ]],
        ["proj-b", [
          makeTask({ id: "task-10", path: "projects/proj-b/task/task-10.md", depends_on: ["proj-a:task-1"] }),
        ]],
      ]);

      const result = findDependents("task-1", "proj-a", tasksByProject);
      expect(result).toHaveLength(1);
      expect(result[0]).toEqual({
        taskId: "task-10",
        taskPath: "projects/proj-b/task/task-10.md",
        projectId: "proj-b",
        depRef: "proj-a:task-1",
      });
    });

    test("finds dependents using full path syntax", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: [] }),
        ]],
        ["proj-b", [
          makeTask({ id: "task-10", path: "projects/proj-b/task/task-10.md", depends_on: ["projects/proj-a/task/task-1.md"] }),
        ]],
      ]);

      const result = findDependents("task-1", "proj-a", tasksByProject);
      expect(result).toHaveLength(1);
      expect(result[0]).toEqual({
        taskId: "task-10",
        taskPath: "projects/proj-b/task/task-10.md",
        projectId: "proj-b",
        depRef: "projects/proj-a/task/task-1.md",
      });
    });

    test("finds multiple dependents across multiple projects", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: ["task-2"] }),
          makeTask({ id: "task-2", path: "projects/proj-a/task/task-2.md", depends_on: [] }),
          makeTask({ id: "task-3", path: "projects/proj-a/task/task-3.md", depends_on: ["task-2"] }),
        ]],
        ["proj-b", [
          makeTask({ id: "task-10", path: "projects/proj-b/task/task-10.md", depends_on: ["proj-a:task-2"] }),
        ]],
      ]);

      const result = findDependents("task-2", "proj-a", tasksByProject);
      expect(result).toHaveLength(3);

      const ids = result.map((r) => r.taskId).sort();
      expect(ids).toEqual(["task-1", "task-10", "task-3"]);
    });

    test("does not match bare ID from a different project (bare ID is same-project only)", () => {
      // task-1 exists in proj-a. proj-b has a task with bare "task-1" dep.
      // Bare "task-1" in proj-b means proj-b:task-1, NOT proj-a:task-1.
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: [] }),
        ]],
        ["proj-b", [
          makeTask({ id: "task-5", path: "projects/proj-b/task/task-5.md", depends_on: ["task-1"] }),
        ]],
      ]);

      const result = findDependents("task-1", "proj-a", tasksByProject);
      // proj-b's bare "task-1" refers to proj-b:task-1, not proj-a:task-1
      expect(result).toHaveLength(0);
    });

    test("handles task with multiple deps where only some match", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: ["task-2", "task-3", "proj-b:task-99"] }),
          makeTask({ id: "task-2", path: "projects/proj-a/task/task-2.md", depends_on: [] }),
        ]],
      ]);

      const result = findDependents("task-2", "proj-a", tasksByProject);
      expect(result).toHaveLength(1);
      expect(result[0].depRef).toBe("task-2");
    });

    test("handles empty tasksByProject map", () => {
      const tasksByProject = new Map<string, Task[]>();
      const result = findDependents("task-1", "proj-a", tasksByProject);
      expect(result).toEqual([]);
    });

    test("handles tasks with empty depends_on arrays", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: [] }),
          makeTask({ id: "task-2", path: "projects/proj-a/task/task-2.md", depends_on: [] }),
        ]],
      ]);

      const result = findDependents("task-1", "proj-a", tasksByProject);
      expect(result).toEqual([]);
    });

    test("does not include the target task itself as a dependent", () => {
      // Edge case: a task depends on itself (circular)
      const tasksByProject = new Map<string, Task[]>([
        ["proj-a", [
          makeTask({ id: "task-1", path: "projects/proj-a/task/task-1.md", depends_on: ["task-1"] }),
        ]],
      ]);

      // task-1 depends on task-1 — it IS a dependent, but we should still report it
      // (the caller decides what to do with self-references)
      const result = findDependents("task-1", "proj-a", tasksByProject);
      expect(result).toHaveLength(1);
      expect(result[0].taskId).toBe("task-1");
    });

    test("finds dependents using full path without .md extension", () => {
      const tasksByProject = new Map<string, Task[]>([
        ["proj-b", [
          makeTask({ id: "task-10", path: "projects/proj-b/task/task-10.md", depends_on: ["projects/proj-a/task/task-1"] }),
        ]],
      ]);

      const result = findDependents("task-1", "proj-a", tasksByProject);
      expect(result).toHaveLength(1);
      expect(result[0].depRef).toBe("projects/proj-a/task/task-1");
    });
  });
});
