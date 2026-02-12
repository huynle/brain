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
import { TaskService, normalizeDependencyRef } from "./task-service";

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
});
