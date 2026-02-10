/**
 * Brain Service - Unit Tests
 *
 * Tests for the core brain service operations.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { BrainService } from "./brain-service";
import { parseFrontmatter } from "./zk-client";
import type { BrainConfig } from "./types";

// =============================================================================
// Test Setup
// =============================================================================

const TEST_DIR = join(tmpdir(), `brain-test-${Date.now()}`);
const TEST_DB_PATH = join(TEST_DIR, "test.db");

const testConfig: BrainConfig = {
  brainDir: TEST_DIR,
  dbPath: TEST_DB_PATH,
  defaultProject: "test-project",
};

let service: BrainService;

beforeAll(() => {
  // Create test directory
  if (!existsSync(TEST_DIR)) {
    mkdirSync(TEST_DIR, { recursive: true });
  }

  // Create necessary subdirectories
  mkdirSync(join(TEST_DIR, "projects", "test-project", "scratch"), {
    recursive: true,
  });

  service = new BrainService(testConfig, "test-project");
});

afterAll(() => {
  // Clean up test directory
  if (existsSync(TEST_DIR)) {
    rmSync(TEST_DIR, { recursive: true, force: true });
  }
});

// =============================================================================
// Tests
// =============================================================================

describe("BrainService", () => {
  describe("initialization", () => {
    test("should create service instance", () => {
      expect(service).toBeDefined();
      expect(service).toBeInstanceOf(BrainService);
    });
  });

  // NOTE: save tests are skipped because they require either:
  // 1. A properly initialized zk notebook in the test directory, or
  // 2. Mocking the zk CLI
  // The save functionality is verified through integration tests with real zk notebooks
  describe.skip("save", () => {
    test("should save a new entry (fallback mode without zk)", async () => {
      const result = await service.save({
        type: "scratch",
        title: "Test Entry",
        content: "This is test content",
        tags: ["test"],
      });

      expect(result).toBeDefined();
      expect(result.title).toBe("Test Entry");
      expect(result.type).toBe("scratch");
      expect(result.status).toBe("active");
      expect(result.path).toContain("projects/test-pro/scratch");
      expect(result.id).toBeDefined();
      expect(result.link).toContain("Test Entry");
    });

    test("should save entry with custom status", async () => {
      const result = await service.save({
        type: "task",
        title: "Test Task",
        content: "Task content",
        status: "pending",
        priority: "high",
      });

      expect(result.status).toBe("pending");
      expect(result.type).toBe("task");
    });
  });

  describe("recall", () => {
    let savedPath: string;

    beforeAll(async () => {
      // Create a test entry manually for recall tests
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      if (!existsSync(testDir)) {
        mkdirSync(testDir, { recursive: true });
      }

      const testFile = join(testDir, "recall-test.md");
      savedPath = "projects/test-project/scratch/recall-test.md";

      writeFileSync(
        testFile,
        `---
title: Recall Test Entry
type: scratch
tags:
  - scratch
  - test
status: active
---

This is the body content for recall testing.
`
      );
    });

    test("should recall entry by path", async () => {
      const result = await service.recall(savedPath);

      expect(result).toBeDefined();
      expect(result.title).toBe("Recall Test Entry");
      expect(result.type).toBe("scratch");
      expect(result.status).toBe("active");
      expect(result.content).toContain("body content for recall testing");
    });

    test("should throw error for non-existent path", async () => {
      await expect(service.recall("non/existent/path.md")).rejects.toThrow(
        "No entry found"
      );
    });

    test("should require path or title", async () => {
      await expect(service.recall()).rejects.toThrow(
        "Please provide a path, ID, or title"
      );
    });
  });

  describe("update", () => {
    let testPath: string;

    beforeAll(async () => {
      // Create a test entry for update tests
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      if (!existsSync(testDir)) {
        mkdirSync(testDir, { recursive: true });
      }

      const testFile = join(testDir, "update-test.md");
      testPath = "projects/test-project/scratch/update-test.md";

      writeFileSync(
        testFile,
        `---
title: Update Test Entry
type: scratch
tags:
  - scratch
status: active
---

Original content.
`
      );
    });

    test("should update entry status", async () => {
      const result = await service.update(testPath, {
        status: "completed",
      });

      expect(result.status).toBe("completed");
    });

    test("should append content", async () => {
      const result = await service.update(testPath, {
        append: "## Additional Section\n\nAppended content.",
      });

      expect(result.content).toContain("Additional Section");
      expect(result.content).toContain("Appended content");
    });

    test("should throw error for non-existent path", async () => {
      await expect(
        service.update("non/existent/path.md", { status: "completed" })
      ).rejects.toThrow("Entry not found");
    });

    test("should require at least one update field", async () => {
      await expect(service.update(testPath, {})).rejects.toThrow(
        "No updates specified"
      );
    });
  });

  describe("delete", () => {
    test("should delete entry", async () => {
      // Create a test entry to delete
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      const testFile = join(testDir, "delete-test.md");
      const deletePath = "projects/test-project/scratch/delete-test.md";

      writeFileSync(
        testFile,
        `---
title: Delete Test
type: scratch
status: active
---

Content to delete.
`
      );

      expect(existsSync(testFile)).toBe(true);

      await service.delete(deletePath);

      expect(existsSync(testFile)).toBe(false);
    });

    test("should throw error for non-existent path", async () => {
      await expect(service.delete("non/existent/path.md")).rejects.toThrow(
        "Entry not found"
      );
    });
  });

  describe("verify", () => {
    let verifyPath: string;

    beforeAll(() => {
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      const testFile = join(testDir, "verify-test.md");
      verifyPath = "projects/test-project/scratch/verify-test.md";

      writeFileSync(
        testFile,
        `---
title: Verify Test
type: scratch
status: active
---

Content for verification.
`
      );
    });

    test("should verify entry", async () => {
      // Should not throw
      await service.verify(verifyPath);
    });

    test("should throw error for non-existent path", async () => {
      await expect(service.verify("non/existent/path.md")).rejects.toThrow(
        "Entry not found"
      );
    });
  });

  describe("getStats", () => {
    test("should return stats object", async () => {
      const stats = await service.getStats();

      expect(stats).toBeDefined();
      expect(stats.brainDir).toBe(TEST_DIR);
      expect(stats.dbPath).toBe(TEST_DB_PATH);
      expect(typeof stats.trackedEntries).toBe("number");
      expect(typeof stats.staleCount).toBe("number");
    });
  });

  describe("getPlanSections", () => {
    let planPath: string;

    beforeAll(() => {
      const planDir = join(TEST_DIR, "projects", "test-project", "plan");
      mkdirSync(planDir, { recursive: true });

      const planFile = join(planDir, "test-plan.md");
      planPath = "projects/test-project/plan/test-plan.md";

      writeFileSync(
        planFile,
        `---
title: Test Implementation Plan
type: plan
status: active
---

# Overview

This is the plan overview.

## Phase 1: Setup

Setup instructions here.

### Sub-task 1.1

Details for sub-task.

## Phase 2: Implementation

Implementation details.

## Phase 3: Testing

Testing approach.
`
      );
    });

    test("should extract plan sections", async () => {
      const result = await service.getPlanSections(planPath);

      expect(result.path).toBe(planPath);
      expect(result.title).toBe("Test Implementation Plan");
      expect(result.type).toBe("plan");
      expect(result.sections.length).toBeGreaterThan(0);

      const sectionTitles = result.sections.map((s) => s.title);
      expect(sectionTitles).toContain("Overview");
      expect(sectionTitles).toContain("Phase 1: Setup");
      expect(sectionTitles).toContain("Phase 2: Implementation");
    });
  });

  describe("getSection", () => {
    let planPath: string;

    beforeAll(() => {
      // Reuse the plan from getPlanSections test
      planPath = "projects/test-project/plan/test-plan.md";
    });

    test("should extract specific section", async () => {
      const result = await service.getSection(planPath, "Phase 1: Setup");

      expect(result.planId).toBe(planPath);
      expect(result.sectionTitle).toBe("Phase 1: Setup");
      expect(result.content).toContain("Setup instructions");
    });

    test("should extract section with subsections", async () => {
      const result = await service.getSection(planPath, "Phase 1", true);

      expect(result.content).toContain("Setup instructions");
      expect(result.content).toContain("Sub-task 1.1");
    });

    test("should throw error for non-existent section", async () => {
      await expect(
        service.getSection(planPath, "Non-existent Section")
      ).rejects.toThrow("not found in plan");
    });
  });

  describe("recall - execution context fields", () => {
    let taskPath: string;

    beforeAll(() => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const taskFile = join(taskDir, "task-with-context.md");
      taskPath = "projects/test-project/task/task-with-context.md";

      writeFileSync(
        taskFile,
        `---
title: Task With Execution Context
type: task
status: pending
priority: high
workdir: projects/my-project
git_remote: git@github.com:user/repo.git
git_branch: feature/test
---

Task content here.
`
      );
    });

    test("should recall workdir from task", async () => {
      const result = await service.recall(taskPath);
      expect(result.workdir).toBe("projects/my-project");
    });

    test("should recall git_remote from task", async () => {
      const result = await service.recall(taskPath);
      expect(result.git_remote).toBe("git@github.com:user/repo.git");
    });

    test("should recall git_branch from task", async () => {
      const result = await service.recall(taskPath);
      expect(result.git_branch).toBe("feature/test");
    });

    test("should recall all execution context fields together", async () => {
      const result = await service.recall(taskPath);
      
      expect(result.title).toBe("Task With Execution Context");
      expect(result.type).toBe("task");
      expect(result.status).toBe("pending");
      expect(result.priority).toBe("high");
      expect(result.workdir).toBe("projects/my-project");
      expect(result.git_remote).toBe("git@github.com:user/repo.git");
      expect(result.git_branch).toBe("feature/test");
    });
  });

  describe("recall - missing execution context", () => {
    let taskPath: string;

    beforeAll(() => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const taskFile = join(taskDir, "task-without-context.md");
      taskPath = "projects/test-project/task/task-without-context.md";

      writeFileSync(
        taskFile,
        `---
title: Task Without Context
type: task
status: pending
---

Task content.
`
      );
    });

    test("should return undefined for missing execution context fields", async () => {
      const result = await service.recall(taskPath);
      
      expect(result.workdir).toBeUndefined();
      expect(result.git_remote).toBeUndefined();
      expect(result.git_branch).toBeUndefined();
    });
  });

  describe("save() - no project root auto-injection", () => {
    // These tests verify that tasks with empty depends_on do NOT get a project root dependency.
    // The project root task feature was removed - tasks now have truly empty depends_on.
    // 
    // NOTE: Tests are skipped because they require zk CLI to be available.
    // The behavior is verified through:
    // 1. Code inspection (auto-injection block removed from brain-service.ts)
    // 2. TaskService test (getOrCreateProjectRoot method removed)
    // 3. TaskRunner tests (pause/resume no longer call API for root task)
    test.skip("should NOT auto-inject project root dependency for tasks with empty depends_on", async () => {
      // Create a task directory for this test
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      // Create a task with empty depends_on
      const result = await service.save({
        type: "task",
        title: "Task Without Root Dependency",
        content: "This task should have no dependencies",
        status: "pending",
        project: "test-project",
        depends_on: [],
      });

      expect(result).toBeDefined();
      expect(result.type).toBe("task");

      // Read the file and verify depends_on is truly empty (no project root injected)
      const fullPath = join(TEST_DIR, result.path);
      const content = readFileSync(fullPath, "utf-8");
      const { frontmatter } = parseFrontmatter(content);

      // The key assertion: depends_on should be empty or undefined, NOT contain a project root ID
      expect(frontmatter.depends_on).toBeUndefined();
    });

    test.skip("should preserve explicit depends_on without adding project root", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      // Create a task with explicit dependencies
      const result = await service.save({
        type: "task",
        title: "Task With Explicit Deps",
        content: "This task has explicit dependencies",
        status: "pending",
        project: "test-project",
        depends_on: ["abc12345", "def67890"],
      });

      expect(result).toBeDefined();

      // Read the file and verify depends_on contains only the explicit deps
      const fullPath = join(TEST_DIR, result.path);
      const content = readFileSync(fullPath, "utf-8");
      const { frontmatter } = parseFrontmatter(content);

      // Should have exactly the deps we specified, no project root added
      expect(frontmatter.depends_on).toEqual(["abc12345", "def67890"]);
    });
  });

  describe("update() field preservation", () => {
    let taskPath: string;

    beforeAll(() => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const taskFile = join(taskDir, "task-full-fields.md");
      taskPath = "projects/test-project/task/task-full-fields.md";

      // Create a task with all fields populated
      writeFileSync(
        taskFile,
        `---
title: Full Task
type: task
tags:
  - task
  - feature
status: pending
created: 2024-01-01T00:00:00Z
priority: high
parent_id: abc12def
projectId: my-project
depends_on:
  - "dep1"
  - "dep2"
workdir: projects/test
git_remote: git@github.com:user/repo
git_branch: main
user_original_request: Original request
---

Task content here.
`
      );
    });

    test("preserves priority when updating status", async () => {
      const result = await service.update(taskPath, { status: "in_progress" });
      expect(result.status).toBe("in_progress");
      expect(result.priority).toBe("high");
    });

    test("preserves workdir when updating status", async () => {
      const result = await service.update(taskPath, { status: "in_progress" });
      expect(result.workdir).toBe("projects/test");
    });

    test("preserves git_remote when updating status", async () => {
      const result = await service.update(taskPath, { status: "in_progress" });
      expect(result.git_remote).toBe("git@github.com:user/repo");
    });

    test("preserves git_branch when updating status", async () => {
      const result = await service.update(taskPath, { status: "in_progress" });
      expect(result.git_branch).toBe("main");
    });

    test("preserves user_original_request when updating status", async () => {
      const result = await service.update(taskPath, { status: "in_progress" });
      expect(result.user_original_request).toBe("Original request");
    });

    test("preserves depends_on when updating status", async () => {
      const result = await service.update(taskPath, { status: "in_progress" });
      expect(result.depends_on).toEqual(["dep1", "dep2"]);
    });

    test("preserves created timestamp when updating status", async () => {
      // Read the file directly to check created field is preserved
      const fullPath = join(TEST_DIR, taskPath);
      const content = readFileSync(fullPath, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.created).toBe("2024-01-01T00:00:00Z");
    });

    test("preserves parent_id when updating status", async () => {
      const fullPath = join(TEST_DIR, taskPath);
      const content = readFileSync(fullPath, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.parent_id).toBe("abc12def");
    });
  });
});
