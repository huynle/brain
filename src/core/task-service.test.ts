/**
 * Task Service - Unit Tests
 *
 * Tests for workdir/worktree execution context in task management.
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

    test("returns null when worktree path does not exist", () => {
      const result = service.resolveWorkdir(null, "nonexistent/worktree/path");
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

    test("resolves worktree path relative to HOME", () => {
      // Create a temporary directory under home for testing
      const testWorktree = `.test-worktree-${Date.now()}`;
      const fullPath = join(homedir(), testWorktree);
      
      try {
        mkdirSync(fullPath, { recursive: true });
        
        const result = service.resolveWorkdir(null, testWorktree);
        expect(result).toBe(fullPath);
      } finally {
        try { rmSync(fullPath, { recursive: true, force: true }); } catch {}
      }
    });

    test("prioritizes worktree over workdir when both exist", () => {
      // Create both directories
      const testWorkdir = `.test-workdir-priority-${Date.now()}`;
      const testWorktree = `.test-worktree-priority-${Date.now()}`;
      const workdirPath = join(homedir(), testWorkdir);
      const worktreePath = join(homedir(), testWorktree);
      
      try {
        mkdirSync(workdirPath, { recursive: true });
        mkdirSync(worktreePath, { recursive: true });
        
        const result = service.resolveWorkdir(testWorkdir, testWorktree);
        expect(result).toBe(worktreePath);
      } finally {
        try { rmSync(workdirPath, { recursive: true, force: true }); } catch {}
        try { rmSync(worktreePath, { recursive: true, force: true }); } catch {}
      }
    });

    test("falls back to workdir when worktree does not exist", () => {
      const testWorkdir = `.test-workdir-fallback-${Date.now()}`;
      const workdirPath = join(homedir(), testWorkdir);
      
      try {
        mkdirSync(workdirPath, { recursive: true });
        
        const result = service.resolveWorkdir(testWorkdir, "nonexistent/worktree");
        expect(result).toBe(workdirPath);
      } finally {
        try { rmSync(workdirPath, { recursive: true, force: true }); } catch {}
      }
    });

    test("returns null when both worktree and workdir do not exist", () => {
      const result = service.resolveWorkdir(
        "nonexistent/workdir",
        "nonexistent/worktree"
      );
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

    test("strips projects/xxx/task/ path prefix", () => {
      const result = normalizeDependencyRef("projects/brain-api/task/1770555889709-task-name");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBeUndefined();
    });

    test("strips both path prefix and .md extension", () => {
      const result = normalizeDependencyRef("projects/brain-api/task/1770555889709-task-name.md");
      expect(result.normalized).toBe("1770555889709-task-name");
      expect(result.projectId).toBeUndefined();
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
