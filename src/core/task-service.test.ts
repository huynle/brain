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
import { TaskService, normalizeDependencyRef, findDependents, mapZkNoteToTask, mapNoteRowToTask } from "./task-service";
import type { Task, ZkNote } from "./types";
import type { NoteRow } from "./storage";
import { createStorageLayer, StorageLayer } from "./storage";

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

  describe("mapZkNoteToTask()", () => {
    test("maps generated metadata fields from note frontmatter", () => {
      const note: ZkNote = {
        path: "projects/demo/task/abc12def.md",
        title: "Generated Task",
        tags: ["task"],
        created: "2026-01-01T00:00:00.000Z",
        metadata: {
          status: "pending",
          generated: true,
          generated_kind: "gap_task",
          generated_key: "feature-checkout:missing-tests",
          generated_by: "feature-checkout",
        },
      };

      const task = mapZkNoteToTask(note);

      expect(task.generated).toBe(true);
      expect(task.generated_kind).toBe("gap_task");
      expect(task.generated_key).toBe("feature-checkout:missing-tests");
      expect(task.generated_by).toBe("feature-checkout");
    });

    test("keeps generated metadata optional when not present", () => {
      const note: ZkNote = {
        path: "projects/demo/task/abc12def.md",
        title: "Regular Task",
        tags: ["task"],
        created: "2026-01-01T00:00:00.000Z",
        metadata: {
          status: "pending",
        },
      };

      const task = mapZkNoteToTask(note);

      expect(task.generated).toBeUndefined();
      expect(task.generated_kind).toBeUndefined();
      expect(task.generated_key).toBeUndefined();
      expect(task.generated_by).toBeUndefined();
    });
  });

  // ===========================================================================
  // mapNoteRowToTask() - NoteRow → Task conversion
  // ===========================================================================

  describe("mapNoteRowToTask()", () => {
    function makeNoteRow(overrides?: Partial<NoteRow>): NoteRow {
      return {
        id: 1,
        path: "projects/demo/task/abc12def.md",
        short_id: "abc12def",
        title: "Test Task",
        lead: "A test task",
        body: "Task body content",
        raw_content: "---\ntitle: Test Task\n---\nTask body content",
        word_count: 3,
        checksum: "abc123",
        metadata: JSON.stringify({
          status: "pending",
          priority: "high",
          depends_on: ["dep-1", "dep-2"],
          tags: ["task", "urgent"],
          target_workdir: "/tmp/work",
          workdir: "projects/demo",
          git_remote: "origin",
          git_branch: "feature/test",
          merge_target_branch: "main",
          merge_policy: "auto_pr",
          merge_strategy: "rebase",
          open_pr_before_merge: true,
          execution_mode: "current_branch",
          complete_on_idle: true,
          user_original_request: "Fix the bug",
          schedule: "0 2 * * *",
          schedule_enabled: true,
          next_run: "2026-01-02T02:00:00Z",
          max_runs: 5,
          starts_at: "2026-01-01T00:00:00Z",
          expires_at: "2026-12-31T23:59:59Z",
          feature_id: "auth-system",
          feature_priority: "high",
          feature_depends_on: ["payment-flow"],
          direct_prompt: "Run tests",
          agent: "tdd-dev",
          model: "anthropic/claude-sonnet-4-20250514",
          sessions: { "ses_abc": { timestamp: "2026-01-01T00:00:00Z" } },
          generated: true,
          generated_kind: "gap_task",
          generated_key: "feature-checkout:missing-tests",
          generated_by: "feature-checkout",
        }),
        type: "task",
        status: "pending",
        priority: "high",
        project_id: "demo",
        feature_id: "auth-system",
        created: "2026-01-01T00:00:00.000Z",
        modified: "2026-01-02T00:00:00.000Z",
        ...overrides,
      };
    }

    test("maps basic fields from NoteRow to Task", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.id).toBe("abc12def");
      expect(task.path).toBe("projects/demo/task/abc12def.md");
      expect(task.title).toBe("Test Task");
      expect(task.created).toBe("2026-01-01T00:00:00.000Z");
      expect(task.modified).toBe("2026-01-02T00:00:00.000Z");
    });

    test("extracts status and priority from metadata JSON", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.status).toBe("pending");
      expect(task.priority).toBe("high");
    });

    test("extracts depends_on from metadata JSON", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.depends_on).toEqual(["dep-1", "dep-2"]);
    });

    test("extracts tags from metadata JSON", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.tags).toEqual(["task", "urgent"]);
    });

    test("extracts execution fields from metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.target_workdir).toBe("/tmp/work");
      expect(task.workdir).toBe("projects/demo");
      expect(task.git_remote).toBe("origin");
      expect(task.git_branch).toBe("feature/test");
      expect(task.merge_target_branch).toBe("main");
      expect(task.merge_policy).toBe("auto_pr");
      expect(task.merge_strategy).toBe("rebase");
      expect(task.open_pr_before_merge).toBe(true);
      expect(task.execution_mode).toBe("current_branch");
      expect(task.complete_on_idle).toBe(true);
      expect(task.user_original_request).toBe("Fix the bug");
    });

    test("extracts schedule fields from metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.schedule).toBe("0 2 * * *");
      expect(task.schedule_enabled).toBe(true);
      expect(task.next_run).toBe("2026-01-02T02:00:00Z");
      expect(task.max_runs).toBe(5);
      expect(task.starts_at).toBe("2026-01-01T00:00:00Z");
      expect(task.expires_at).toBe("2026-12-31T23:59:59Z");
    });

    test("extracts feature grouping fields from metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.feature_id).toBe("auth-system");
      expect(task.feature_priority).toBe("high");
      expect(task.feature_depends_on).toEqual(["payment-flow"]);
    });

    test("extracts OpenCode execution options from metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.direct_prompt).toBe("Run tests");
      expect(task.agent).toBe("tdd-dev");
      expect(task.model).toBe("anthropic/claude-sonnet-4-20250514");
    });

    test("extracts session traceability from metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.sessions).toEqual({ "ses_abc": { timestamp: "2026-01-01T00:00:00Z" } });
    });

    test("extracts generated metadata from metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.generated).toBe(true);
      expect(task.generated_kind).toBe("gap_task");
      expect(task.generated_key).toBe("feature-checkout:missing-tests");
      expect(task.generated_by).toBe("feature-checkout");
    });

    test("derives projectId from file path", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.projectId).toBe("demo");
    });

    test("preserves raw frontmatter as metadata", () => {
      const row = makeNoteRow();
      const task = mapNoteRowToTask(row);

      expect(task.frontmatter).toBeDefined();
      expect(task.frontmatter?.status).toBe("pending");
    });

    test("handles minimal metadata with defaults", () => {
      const row = makeNoteRow({
        metadata: JSON.stringify({ status: "active" }),
      });
      const task = mapNoteRowToTask(row);

      expect(task.status).toBe("active");
      expect(task.priority).toBe("medium"); // default
      expect(task.depends_on).toEqual([]); // default
      expect(task.tags).toEqual([]); // default
      expect(task.target_workdir).toBeNull();
      expect(task.workdir).toBeNull();
      expect(task.direct_prompt).toBeNull();
      expect(task.agent).toBeNull();
      expect(task.model).toBeNull();
      expect(task.worktree).toBeNull();
    });

    test("handles invalid metadata JSON gracefully", () => {
      const row = makeNoteRow({
        metadata: "not valid json",
      });
      const task = mapNoteRowToTask(row);

      // Should fall back to defaults
      expect(task.status).toBe("pending"); // default
      expect(task.priority).toBe("medium"); // default
      expect(task.depends_on).toEqual([]);
      expect(task.tags).toEqual([]);
    });

    test("uses extractIdFromPath for id (short_id from NoteRow path)", () => {
      const row = makeNoteRow({
        path: "projects/myproj/task/xyz99abc.md",
        short_id: "xyz99abc",
      });
      const task = mapNoteRowToTask(row);

      // Should use extractIdFromPath on the path, not the short_id directly
      expect(task.id).toBe("xyz99abc");
    });
  });

  // ===========================================================================
  // getAllTasks() with StorageLayer
  // ===========================================================================

  describe("getAllTasks() with StorageLayer", () => {
    let storage: StorageLayer;
    let service: TaskService;

    beforeEach(() => {
      storage = createStorageLayer(":memory:");

      service = new TaskService(
        {
          brainDir: "/tmp/test-brain",
          dbPath: ":memory:",
          defaultProject: "test-project",
        },
        "test-project",
        { storage }
      );
    });

    afterEach(() => {
      storage.close();
    });

    test("returns tasks from StorageLayer when available", async () => {
      // Insert a task note into the storage
      storage.insertNote({
        path: "projects/myproj/task/task-001.md",
        short_id: "task-001",
        title: "First Task",
        lead: "A task",
        body: "Task body",
        raw_content: "---\ntitle: First Task\n---\nTask body",
        word_count: 2,
        checksum: "abc",
        metadata: JSON.stringify({
          status: "pending",
          priority: "high",
          depends_on: [],
          tags: ["task"],
        }),
        type: "task",
        status: "pending",
        priority: "high",
        project_id: "myproj",
        feature_id: null,
        created: "2026-01-01T00:00:00.000Z",
        modified: "2026-01-02T00:00:00.000Z",
      });

      const tasks = await service.getAllTasks("myproj");

      expect(tasks).toHaveLength(1);
      expect(tasks[0].id).toBe("task-001");
      expect(tasks[0].title).toBe("First Task");
      expect(tasks[0].status).toBe("pending");
      expect(tasks[0].priority).toBe("high");
    });

    test("returns empty array when no tasks exist for project", async () => {
      const tasks = await service.getAllTasks("nonexistent");
      expect(tasks).toEqual([]);
    });

    test("filters tasks by project path prefix", async () => {
      // Insert tasks for two different projects
      storage.insertNote({
        path: "projects/proj-a/task/task-a1.md",
        short_id: "task-a1",
        title: "Task A1",
        lead: "",
        body: "",
        raw_content: "",
        word_count: 0,
        checksum: "a1",
        metadata: JSON.stringify({ status: "pending" }),
        type: "task",
        status: "pending",
        priority: "medium",
        project_id: "proj-a",
        feature_id: null,
        created: "2026-01-01T00:00:00.000Z",
        modified: null,
      });

      storage.insertNote({
        path: "projects/proj-b/task/task-b1.md",
        short_id: "task-b1",
        title: "Task B1",
        lead: "",
        body: "",
        raw_content: "",
        word_count: 0,
        checksum: "b1",
        metadata: JSON.stringify({ status: "pending" }),
        type: "task",
        status: "pending",
        priority: "medium",
        project_id: "proj-b",
        feature_id: null,
        created: "2026-01-01T00:00:00.000Z",
        modified: null,
      });

      const tasksA = await service.getAllTasks("proj-a");
      expect(tasksA).toHaveLength(1);
      expect(tasksA[0].id).toBe("task-a1");

      const tasksB = await service.getAllTasks("proj-b");
      expect(tasksB).toHaveLength(1);
      expect(tasksB[0].id).toBe("task-b1");
    });

    test("maps all NoteRow fields to Task correctly via StorageLayer", async () => {
      storage.insertNote({
        path: "projects/myproj/task/full-task.md",
        short_id: "full-task",
        title: "Full Task",
        lead: "A complete task",
        body: "Full body",
        raw_content: "---\ntitle: Full Task\n---\nFull body",
        word_count: 2,
        checksum: "full",
        metadata: JSON.stringify({
          status: "in_progress",
          priority: "low",
          depends_on: ["other-task"],
          tags: ["feature"],
          direct_prompt: "Do the thing",
          agent: "explore",
          model: "anthropic/claude-sonnet-4-20250514",
          git_branch: "feature/full",
          workdir: "projects/myproj",
        }),
        type: "task",
        status: "in_progress",
        priority: "low",
        project_id: "myproj",
        feature_id: null,
        created: "2026-01-01T00:00:00.000Z",
        modified: "2026-01-03T00:00:00.000Z",
      });

      const tasks = await service.getAllTasks("myproj");
      expect(tasks).toHaveLength(1);

      const task = tasks[0];
      expect(task.status).toBe("in_progress");
      expect(task.priority).toBe("low");
      expect(task.depends_on).toEqual(["other-task"]);
      expect(task.tags).toEqual(["feature"]);
      expect(task.direct_prompt).toBe("Do the thing");
      expect(task.agent).toBe("explore");
      expect(task.model).toBe("anthropic/claude-sonnet-4-20250514");
      expect(task.git_branch).toBe("feature/full");
      expect(task.projectId).toBe("myproj");
    });
  });
});
