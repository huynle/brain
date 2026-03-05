/**
 * Brain Service - Unit Tests
 *
 * Tests for the core brain service operations.
 */

import { describe, test, expect, beforeAll, afterAll, beforeEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { BrainService } from "./brain-service";
import { parseFrontmatter } from "./zk-client";
import type { BrainConfig } from "./types";
import { initDatabase, acquireGeneratedTaskLease, completeGeneratedTaskLease } from "./db";
import { TaskService } from "./task-service";

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

    test("should accept empty string for feature_id (clearing field)", async () => {
      // Create a test entry with feature_id for this test
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      const testFile = join(testDir, "feature-id-test.md");
      const featureTestPath = "projects/test-project/scratch/feature-id-test.md";

      writeFileSync(
        testFile,
        `---
title: Feature ID Test Entry
type: scratch
tags:
  - scratch
status: active
feature_id: test-feature
---

Test content.
`
      );

      // Verify initial state
      const initialContent = readFileSync(testFile, "utf-8");
      expect(initialContent).toContain("feature_id: test-feature");

      // Clear with empty string - this should NOT throw "No updates specified"
      // The key fix: previously this would throw 400 because !("") === true
      const result = await service.update(featureTestPath, { feature_id: "" });
      
      // Verify the update succeeded (the field is cleared from frontmatter by serializer)
      expect(result).toBeDefined();
      const updatedContent = readFileSync(testFile, "utf-8");
      // The serializer removes empty string values, which is expected behavior
      expect(updatedContent).not.toContain("feature_id: test-feature");
    });

    test("should accept empty string for git_branch (clearing field)", async () => {
      // Create a test entry with git_branch for this test
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      const testFile = join(testDir, "git-branch-test.md");
      const gitBranchTestPath = "projects/test-project/scratch/git-branch-test.md";

      writeFileSync(
        testFile,
        `---
title: Git Branch Test Entry
type: scratch
tags:
  - scratch
status: active
git_branch: feature/test
---

Test content.
`
      );

      // Verify initial state
      const initialContent = readFileSync(testFile, "utf-8");
      expect(initialContent).toContain("git_branch: feature/test");

      // Clear with empty string - this should NOT throw "No updates specified"
      // The key fix: previously this would throw 400 because !("") === true
      const result = await service.update(gitBranchTestPath, { git_branch: "" });
      
      // Verify the update succeeded (the field is cleared from frontmatter by serializer)
      expect(result).toBeDefined();
      const updatedContent = readFileSync(testFile, "utf-8");
      // The serializer removes empty string values, which is expected behavior
      expect(updatedContent).not.toContain("git_branch: feature/test");
    });

    test("should accept empty string for target_workdir (clearing field)", async () => {
      // Create a test entry with target_workdir for this test
      const testDir = join(TEST_DIR, "projects", "test-project", "scratch");
      const testFile = join(testDir, "target-workdir-test.md");
      const targetWorkdirTestPath = "projects/test-project/scratch/target-workdir-test.md";

      writeFileSync(
        testFile,
        `---
title: Target Workdir Test Entry
type: scratch
tags:
  - scratch
status: active
target_workdir: /path/to/dir
---

Test content.
`
      );

      // Verify initial state
      const initialContent = readFileSync(testFile, "utf-8");
      expect(initialContent).toContain("target_workdir: /path/to/dir");

      // Clear with empty string - this should NOT throw "No updates specified"
      // The key fix: previously this would throw 400 because !("") === true
      const result = await service.update(targetWorkdirTestPath, { target_workdir: "" });
      
      // Verify the update succeeded (the field is cleared from frontmatter by serializer)
      expect(result).toBeDefined();
      const updatedContent = readFileSync(testFile, "utf-8");
      // The serializer removes empty string values, which is expected behavior
      expect(updatedContent).not.toContain("target_workdir: /path/to/dir");
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

  describe("task schedule fields parity", () => {
    test("save() computes next_run for tasks with schedule", async () => {
      const result = await service.save({
        type: "task",
        title: "Nightly Scheduled Task",
        content: "Run nightly pipeline",
        schedule: "0 2 * * *",
      });

      const recalled = await service.recall(result.path);
      expect(recalled.type).toBe("task");
      expect(recalled.schedule).toBe("0 2 * * *");
      expect(typeof recalled.next_run).toBe("string");

      const parsed = new Date(recalled.next_run!);
      expect(Number.isNaN(parsed.getTime())).toBe(false);
      expect(parsed.getTime()).toBeGreaterThan(Date.now());
    });

    test("recall() includes runs fields on tasks", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const taskFile = join(taskDir, "task-with-runs.md");
      const taskPath = "projects/test-project/task/task-with-runs.md";

      writeFileSync(
        taskFile,
        `---
title: Task With Runs
type: task
status: active
schedule: "*/15 * * * *"
next_run: 2030-01-01T00:15:00.000Z
runs:
  - run_id: 20300101-0000
    status: completed
    started: 2030-01-01T00:00:00.000Z
    completed: 2030-01-01T00:00:08.000Z
    duration: 8000
    tasks: 3
---

Task body.
`
      );

      const recalled = await service.recall(taskPath);
      expect(recalled.runs).toEqual([
        {
          run_id: "20300101-0000",
          status: "completed",
          started: "2030-01-01T00:00:00.000Z",
          completed: "2030-01-01T00:00:08.000Z",
          duration: 8000,
          tasks: 3,
        },
      ]);
    });

    test("update() recalculates next_run when schedule changes on task", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const taskFile = join(taskDir, "task-update-schedule.md");
      const taskPath = "projects/test-project/task/task-update-schedule.md";

      writeFileSync(
        taskFile,
        `---
title: Task Update Schedule
type: task
status: active
schedule: 0 2 * * *
next_run: 2030-01-01T02:00:00.000Z
---

Task body.
`
      );

      const updated = await service.update(taskPath, {
        schedule: "*/5 * * * *",
      });

      expect(updated.schedule).toBe("*/5 * * * *");
      expect(updated.next_run).toBeDefined();
      expect(updated.next_run).not.toBe("2030-01-01T02:00:00.000Z");

      const content = readFileSync(taskFile, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.schedule).toBe("*/5 * * * *");
      expect(frontmatter.next_run).not.toBe("2030-01-01T02:00:00.000Z");
    });

    test("update() accepts runs and next_run updates on tasks", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const taskFile = join(taskDir, "task-update-fields.md");
      const taskPath = "projects/test-project/task/task-update-fields.md";

      writeFileSync(
        taskFile,
        `---
title: Task Update Fields
type: task
status: active
---

Task body.
`
      );

      const updated = await service.update(taskPath, {
        next_run: "2031-01-01T00:00:00.000Z",
        runs: [
          {
            run_id: "20300101-0000",
            status: "failed",
            started: "2030-01-01T00:00:00.000Z",
            failed_task: "abc12def",
          },
        ],
      });

      expect(updated.next_run).toBe("2031-01-01T00:00:00.000Z");
      expect(updated.runs).toEqual([
        {
          run_id: "20300101-0000",
          status: "failed",
          started: "2030-01-01T00:00:00.000Z",
          failed_task: "abc12def",
        },
      ]);
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

  describe("update() OpenCode execution options", () => {
    test("should update direct_prompt field", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "update-prompt-test.md");
      const testPath = "projects/test-project/task/update-prompt-test.md";

      writeFileSync(
        testFile,
        `---
title: Task to Update Prompt
type: task
tags:
  - task
status: pending
---

Task content.
`
      );

      const result = await service.update(testPath, {
        direct_prompt: "Run the tests and fix failures",
      });

      // Verify the file was updated
      const content = readFileSync(testFile, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.direct_prompt).toBe("Run the tests and fix failures");
    });

    test("should update agent field", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "update-agent-test.md");
      const testPath = "projects/test-project/task/update-agent-test.md";

      writeFileSync(
        testFile,
        `---
title: Task to Update Agent
type: task
tags:
  - task
status: pending
---

Task content.
`
      );

      const result = await service.update(testPath, {
        agent: "explore",
      });

      const content = readFileSync(testFile, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.agent).toBe("explore");
    });

    test("should update model field", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "update-model-test.md");
      const testPath = "projects/test-project/task/update-model-test.md";

      writeFileSync(
        testFile,
        `---
title: Task to Update Model
type: task
tags:
  - task
status: pending
---

Task content.
`
      );

      const result = await service.update(testPath, {
        model: "anthropic/claude-sonnet-4-20250514",
      });

      const content = readFileSync(testFile, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.model).toBe("anthropic/claude-sonnet-4-20250514");
    });

    test("should update multiline direct_prompt correctly", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "update-multiline-prompt.md");
      const testPath = "projects/test-project/task/update-multiline-prompt.md";

      writeFileSync(
        testFile,
        `---
title: Task with Multiline Prompt
type: task
tags:
  - task
status: pending
---

Task content.
`
      );

      const multilinePrompt = "Step 1: Read the code\nStep 2: Fix the bug\nStep 3: Run tests";
      const result = await service.update(testPath, {
        direct_prompt: multilinePrompt,
      });

      const content = readFileSync(testFile, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.direct_prompt).toBe(multilinePrompt);
    });

    test("should preserve existing OpenCode options when updating other fields", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "preserve-oc-options.md");
      const testPath = "projects/test-project/task/preserve-oc-options.md";

      writeFileSync(
        testFile,
        `---
title: Task with OC Options
type: task
tags:
  - task
status: pending
direct_prompt: Do the work
agent: tdd-dev
model: anthropic/claude-sonnet-4-20250514
---

Task content.
`
      );

      // Update only status - should preserve direct_prompt, agent, model
      const result = await service.update(testPath, { status: "in_progress" });

      const content = readFileSync(testFile, "utf-8");
      const { frontmatter } = parseFrontmatter(content);
      expect(frontmatter.direct_prompt).toBe("Do the work");
      expect(frontmatter.agent).toBe("tdd-dev");
      expect(frontmatter.model).toBe("anthropic/claude-sonnet-4-20250514");
    });
  });

  describe("generated metadata fields", () => {
    test("save() writes generated metadata to frontmatter and recall() returns it", async () => {
      const result = await service.save({
        type: "task",
        title: "Generated Metadata Task",
        content: "Generated task content",
        generated: true,
        generated_kind: "gap_task",
        generated_key: "feature-checkout:missing-tests",
        generated_by: "feature-checkout",
      });

      const fullPath = join(TEST_DIR, result.path);
      const savedContent = readFileSync(fullPath, "utf-8");
      const { frontmatter } = parseFrontmatter(savedContent);

      expect(frontmatter.generated).toBe(true);
      expect(frontmatter.generated_kind).toBe("gap_task");
      expect(frontmatter.generated_key).toBe("feature-checkout:missing-tests");
      expect(frontmatter.generated_by).toBe("feature-checkout");

      const recalled = await service.recall(result.path);
      expect(recalled.generated).toBe(true);
      expect(recalled.generated_kind).toBe("gap_task");
      expect(recalled.generated_key).toBe("feature-checkout:missing-tests");
      expect(recalled.generated_by).toBe("feature-checkout");
    });

    test("update() patches generated metadata fields and preserves them", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "generated-update-test.md");
      const testPath = "projects/test-project/task/generated-update-test.md";

      writeFileSync(
        testFile,
        `---
title: Generated Update Task
type: task
status: pending
generated: true
generated_kind: feature_checkout
generated_key: feature-checkout:task-1
generated_by: feature-checkout
---

Task content.
`
      );

      const updated = await service.update(testPath, {
        generated: false,
        generated_kind: "other",
        generated_key: "task-abc12def",
        generated_by: "manual",
      });

      expect(updated.generated).toBe(false);
      expect(updated.generated_kind).toBe("other");
      expect(updated.generated_key).toBe("task-abc12def");
      expect(updated.generated_by).toBe("manual");

      const preserved = await service.update(testPath, { status: "in_progress" });
      expect(preserved.generated).toBe(false);
      expect(preserved.generated_kind).toBe("other");
      expect(preserved.generated_key).toBe("task-abc12def");
      expect(preserved.generated_by).toBe("manual");
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

    test("records run_finalizations marker when task is completed with session run_id", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "task-run-finalization.md");
      const testPath = "projects/test-project/task/task-run-finalization.md";

      writeFileSync(
        testFile,
        `---
title: Task Run Finalization
type: task
status: in_progress
sessions:
  ses_run_001:
    timestamp: "2026-02-25T10:00:00.000Z"
    cron_id: cron_daily
    run_id: run_20260225_001
---

Task content.
`
      );

      await service.update(testPath, { status: "completed" });

      const updated = await service.recall(testPath);
      expect(updated.run_finalizations).toBeDefined();
      expect(updated.run_finalizations?.run_20260225_001?.status).toBe("completed");
      expect(updated.run_finalizations?.run_20260225_001?.session_id).toBe("ses_run_001");
      expect(updated.run_finalizations?.run_20260225_001?.finalized_at).toEqual(expect.any(String));
    });

    test("does not add run_finalizations when completed task has no run_id in sessions", async () => {
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      mkdirSync(taskDir, { recursive: true });

      const testFile = join(taskDir, "task-no-run-finalization.md");
      const testPath = "projects/test-project/task/task-no-run-finalization.md";

      writeFileSync(
        testFile,
        `---
title: Task Without Run Context
type: task
status: in_progress
sessions:
  ses_no_run:
    timestamp: "2026-02-25T10:10:00.000Z"
---

Task content.
`
      );

      await service.update(testPath, { status: "completed" });

      const updated = await service.recall(testPath);
      expect(updated.run_finalizations).toBeUndefined();
    });
  });

  describe("markFeatureForCheckout", () => {
    beforeEach(() => {
      const db = initDatabase();
      db.run("DELETE FROM generated_task_keys");
    });

    test("creates checkout task with deterministic key and non-generated dependencies", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalSave = service.save;

      const capturedRequests: Array<Record<string, unknown>> = [];

      TaskService.prototype.getTasksByFeature = async () => [
        {
          id: "bcd23efg",
          generated: undefined,
          feature_id: "feature-a",
        },
        {
          id: "abc12def",
          generated: false,
          feature_id: "feature-a",
        },
        {
          id: "abc12def",
          generated: false,
          feature_id: "feature-a",
        },
        {
          id: "cde34fgh",
          generated: true,
          feature_id: "feature-a",
        },
      ] as any;

      service.save = (async (request) => {
        capturedRequests.push(request as unknown as Record<string, unknown>);
        return {
          id: "chk12345",
          path: "projects/test-project/task/chk12345.md",
          title: "Feature checkout: feature-a",
          type: "task",
          status: "pending",
          link: "[[chk12345]]",
        };
      }) as typeof service.save;

      try {
        const result = await service.markFeatureForCheckout("test-project", "feature-a");

        expect(result.created).toBe(true);
        expect(result.generatedKey).toBe("feature-checkout:feature-a:round-1");
        expect(result.task.path).toBe("projects/test-project/task/chk12345.md");
        expect(capturedRequests).toHaveLength(1);
        expect(capturedRequests[0]?.generated).toBe(true);
        expect(capturedRequests[0]?.generated_kind).toBe("feature_checkout");
        expect(capturedRequests[0]?.generated_key).toBe("feature-checkout:feature-a:round-1");
        expect(capturedRequests[0]?.generated_by).toBe("feature-checkout");
        expect(capturedRequests[0]?.feature_id).toBe("feature-a");
        expect(capturedRequests[0]?.tags).toEqual(["checkout", "feature-a"]);
        expect(capturedRequests[0]?.depends_on).toEqual(["abc12def", "bcd23efg"]);
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        service.save = originalSave;
      }
    });

    test("returns existing checkout task on repeated calls with same generated key", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalSave = service.save;
      const originalRecall = service.recall;

      let saveCalls = 0;
      let recallCalls = 0;

      TaskService.prototype.getTasksByFeature = async () => [] as any;

      service.save = (async () => {
        saveCalls += 1;
        return {
          id: "repeat42",
          path: "projects/test-project/task/repeat42.md",
          title: "Feature checkout: feature-repeat",
          type: "task",
          status: "pending",
          link: "[[repeat42]]",
        };
      }) as typeof service.save;

      service.recall = (async () => {
        recallCalls += 1;
        return {
          id: "repeat42",
          path: "projects/test-project/task/repeat42.md",
          title: "Feature checkout: feature-repeat",
          type: "task",
          status: "pending",
          content: "",
        };
      }) as typeof service.recall;

      try {
        const first = await service.markFeatureForCheckout("test-project", "feature-repeat");
        const second = await service.markFeatureForCheckout("test-project", "feature-repeat");

        expect(first.created).toBe(true);
        expect(second.created).toBe(false);
        expect(first.generatedKey).toBe("feature-checkout:feature-repeat:round-1");
        expect(second.generatedKey).toBe(first.generatedKey);
        expect(first.task.path).toBe("projects/test-project/task/repeat42.md");
        expect(second.task.path).toBe(first.task.path);
        expect(saveCalls).toBe(1);
        expect(recallCalls).toBe(1);
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        service.save = originalSave;
        service.recall = originalRecall;
      }
    });

    test("returns existing checkout task when generated key already finalized", async () => {
      const generatedKey = "feature-checkout:feature-b:round-1";
      const existingPath = "projects/test-project/task/exist123.md";

      const lease = acquireGeneratedTaskLease("test-project", generatedKey, "writer", 60_000);
      expect(lease.status).toBe("acquired");
      const completed = completeGeneratedTaskLease("test-project", generatedKey, "writer", existingPath);
      expect(completed).toBe(true);

      const originalSave = service.save;
      const originalRecall = service.recall;

      let saveCalls = 0;
      service.save = (async () => {
        saveCalls += 1;
        throw new Error("save should not be called when generated task exists");
      }) as typeof service.save;

      service.recall = (async () => ({
        id: "exist123",
        path: existingPath,
        title: "Feature checkout: feature-b",
        type: "task",
        status: "pending",
        content: "",
        tags: ["checkout", "feature-b"],
      })) as typeof service.recall;

      try {
        const result = await service.markFeatureForCheckout("test-project", "feature-b");
        expect(result.created).toBe(false);
        expect(result.generatedKey).toBe(generatedKey);
        expect(result.task.path).toBe(existingPath);
        expect(saveCalls).toBe(0);
      } finally {
        service.save = originalSave;
        service.recall = originalRecall;
      }
    });

    test("rejects checkout options when execution branch matches merge target", async () => {
      await expect(
        service.markFeatureForCheckout("test-project", "feature-guard", {
          execution_branch: "main",
          merge_target_branch: "main",
        })
      ).rejects.toThrow("execution_branch must be different from merge_target_branch");
    });

    test("applies deterministic merge defaults when checkout options are omitted", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalSave = service.save;

      const capturedRequests: Array<Record<string, unknown>> = [];

      TaskService.prototype.getTasksByFeature = async () => [] as any;

      service.save = (async (request) => {
        capturedRequests.push(request as unknown as Record<string, unknown>);
        return {
          id: "chkdefaults",
          path: "projects/test-project/task/chkdefaults.md",
          title: "Feature checkout: feature-defaults",
          type: "task",
          status: "pending",
          link: "[[chkdefaults]]",
        };
      }) as typeof service.save;

      try {
        await service.markFeatureForCheckout("test-project", "feature-defaults");

        expect(capturedRequests).toHaveLength(1);
        expect(capturedRequests[0]?.merge_policy).toBe("auto_merge");
        expect(capturedRequests[0]?.merge_strategy).toBe("squash");
        expect(capturedRequests[0]?.remote_branch_policy).toBe("delete");
        expect(capturedRequests[0]?.execution_mode).toBe("worktree");
        expect(capturedRequests[0]?.open_pr_before_merge).toBe(false);
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        service.save = originalSave;
      }
    });

    test("forwards explicit remote branch policy to generated checkout task", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalSave = service.save;

      const capturedRequests: Array<Record<string, unknown>> = [];

      TaskService.prototype.getTasksByFeature = async () => [] as any;

      service.save = (async (request) => {
        capturedRequests.push(request as unknown as Record<string, unknown>);
        return {
          id: "chkremote",
          path: "projects/test-project/task/chkremote.md",
          title: "Feature checkout: feature-remote",
          type: "task",
          status: "pending",
          link: "[[chkremote]]",
        };
      }) as typeof service.save;

      try {
        await service.markFeatureForCheckout(
          "test-project",
          "feature-remote",
          { remote_branch_policy: "keep" } as any
        );

        expect(capturedRequests).toHaveLength(1);
        expect(capturedRequests[0]?.remote_branch_policy).toBe("keep");
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        service.save = originalSave;
      }
    });

    test("blocks auto-merge to protected target branch when PR-before-merge is disabled", async () => {
      await expect(
        service.markFeatureForCheckout("test-project", "feature-protected", {
          execution_branch: "feature/protected-flow",
          merge_target_branch: "main",
          merge_policy: "auto_merge",
          open_pr_before_merge: false,
        })
      ).rejects.toThrow(
        "open_pr_before_merge must be true when auto-merging into protected branch: main"
      );
    });

    test("writes checkout task content with merge safety gates and cleanup guardrail", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalSave = service.save;

      const capturedRequests: Array<Record<string, unknown>> = [];

      TaskService.prototype.getTasksByFeature = async () => [] as any;

      service.save = (async (request) => {
        capturedRequests.push(request as unknown as Record<string, unknown>);
        return {
          id: "chkgates1",
          path: "projects/test-project/task/chkgates1.md",
          title: "Feature checkout: feature-gates",
          type: "task",
          status: "pending",
          link: "[[chkgates1]]",
        };
      }) as typeof service.save;

      try {
        await service.markFeatureForCheckout("test-project", "feature-gates", {
          execution_branch: "feature/safety",
          merge_target_branch: "develop",
          merge_policy: "auto_merge",
          merge_strategy: "squash",
        });

        expect(capturedRequests).toHaveLength(1);
        const content = String(capturedRequests[0]?.content ?? "");
        expect(content).toContain("Merge intent:");
        expect(content).toContain("- merge_policy: auto_merge");
        expect(content).toContain("- merge_strategy: squash");
        expect(content).toContain("- remote_branch_policy: delete");
        expect(content).toContain("- open_pr_before_merge: false");
        expect(content).toContain("checkout validation pass");
        expect(content).toContain("merge precheck pass");
        expect(content).toContain("verification commands pass");
        expect(content).toContain("cleanup only after confirmed successful push");
        expect(content).toContain("protected branch");
        expect(content).toContain("open_pr_before_merge");
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        service.save = originalSave;
      }
    });

    test("reconciles checkout depends_on with non-generated feature tasks and skips in-progress", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalUpdateInternal = (service as any).updateInternal;

      const updateCalls: Array<{
        path: string;
        request: Record<string, unknown>;
        options: Record<string, unknown> | undefined;
      }> = [];

      TaskService.prototype.getTasksByFeature = async () =>
        [
          {
            id: "task-z",
            generated: false,
            feature_id: "feature-reconcile",
            status: "pending",
          },
          {
            id: "task-a",
            generated: false,
            feature_id: "feature-reconcile",
            status: "pending",
          },
          {
            id: "task-z",
            generated: false,
            feature_id: "feature-reconcile",
            status: "pending",
          },
          {
            id: "chk-pending",
            path: "projects/test-project/task/chk-pending.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-reconcile",
            status: "pending",
            depends_on: ["task-a"],
          },
          {
            id: "chk-active",
            path: "projects/test-project/task/chk-active.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-reconcile",
            status: "in_progress",
            depends_on: ["task-a"],
          },
          {
            id: "chk-same",
            path: "projects/test-project/task/chk-same.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-reconcile",
            status: "pending",
            depends_on: ["task-a", "task-z"],
          },
        ] as any;

      (service as any).updateInternal = async (
        path: string,
        request: Record<string, unknown>,
        options?: Record<string, unknown>
      ) => {
        updateCalls.push({
          path,
          request: request as unknown as Record<string, unknown>,
          options: options as unknown as Record<string, unknown> | undefined,
        });
        return {
          id: "chk-pending",
          path,
          title: "Feature checkout",
          type: "task",
          status: "pending",
          content: "",
        } as any;
      };

      try {
        await (service as any).reconcileFeatureCheckoutDependencies(
          "test-project",
          "feature-reconcile"
        );

        expect(updateCalls).toHaveLength(1);
        expect(updateCalls[0]?.path).toBe("projects/test-project/task/chk-pending.md");
        expect(updateCalls[0]?.request.depends_on).toEqual(["task-a", "task-z"]);
        expect(updateCalls[0]?.options).toEqual({
          skipFeatureCheckoutReconcile: true,
          skipDependsOnValidation: true,
        });
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        (service as any).updateInternal = originalUpdateInternal;
      }
    });

    test("does not reconcile terminal generated checkout tasks", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalUpdateInternal = (service as any).updateInternal;

      const updateCalls: Array<{
        path: string;
        request: Record<string, unknown>;
        options: Record<string, unknown> | undefined;
      }> = [];

      TaskService.prototype.getTasksByFeature = async () =>
        [
          {
            id: "task-a",
            generated: false,
            feature_id: "feature-terminal",
            status: "pending",
          },
          {
            id: "chk-pending",
            path: "projects/test-project/task/chk-pending.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-terminal",
            status: "pending",
            depends_on: [],
          },
          {
            id: "chk-completed",
            path: "projects/test-project/task/chk-completed.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-terminal",
            status: "completed",
            depends_on: [],
          },
          {
            id: "chk-validated",
            path: "projects/test-project/task/chk-validated.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-terminal",
            status: "validated",
            depends_on: [],
          },
          {
            id: "chk-superseded",
            path: "projects/test-project/task/chk-superseded.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-terminal",
            status: "superseded",
            depends_on: [],
          },
          {
            id: "chk-archived",
            path: "projects/test-project/task/chk-archived.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-terminal",
            status: "archived",
            depends_on: [],
          },
          {
            id: "chk-cancelled",
            path: "projects/test-project/task/chk-cancelled.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-terminal",
            status: "cancelled",
            depends_on: [],
          },
        ] as any;

      (service as any).updateInternal = async (
        path: string,
        request: Record<string, unknown>,
        options?: Record<string, unknown>
      ) => {
        updateCalls.push({
          path,
          request: request as unknown as Record<string, unknown>,
          options: options as unknown as Record<string, unknown> | undefined,
        });
        return {
          id: "chk-pending",
          path,
          title: "Feature checkout",
          type: "task",
          status: "pending",
          content: "",
        } as any;
      };

      try {
        await (service as any).reconcileFeatureCheckoutDependencies(
          "test-project",
          "feature-terminal"
        );

        expect(updateCalls).toHaveLength(1);
        expect(updateCalls[0]?.path).toBe("projects/test-project/task/chk-pending.md");
        expect(updateCalls[0]?.request.depends_on).toEqual(["task-a"]);
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        (service as any).updateInternal = originalUpdateInternal;
      }
    });

    test("reconciles feature_review tasks alongside feature_checkout tasks", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalUpdateInternal = (service as any).updateInternal;

      const updateCalls: Array<{
        path: string;
        request: Record<string, unknown>;
        options: Record<string, unknown> | undefined;
      }> = [];

      TaskService.prototype.getTasksByFeature = async () =>
        [
          {
            id: "task-a",
            generated: false,
            feature_id: "feature-review-reconcile",
            status: "pending",
          },
          {
            id: "task-b",
            generated: false,
            feature_id: "feature-review-reconcile",
            status: "pending",
          },
          {
            id: "chk-pending",
            path: "projects/test-project/task/chk-pending.md",
            generated: true,
            generated_kind: "feature_checkout",
            generated_by: "feature-checkout",
            feature_id: "feature-review-reconcile",
            status: "pending",
            depends_on: ["task-a"],
          },
          {
            id: "rev-pending",
            path: "projects/test-project/task/rev-pending.md",
            generated: true,
            generated_kind: "feature_review",
            generated_by: "feature-completion-hook",
            feature_id: "feature-review-reconcile",
            status: "pending",
            depends_on: ["task-a"],
          },
        ] as any;

      (service as any).updateInternal = async (
        path: string,
        request: Record<string, unknown>,
        options?: Record<string, unknown>
      ) => {
        updateCalls.push({
          path,
          request: request as unknown as Record<string, unknown>,
          options: options as unknown as Record<string, unknown> | undefined,
        });
        return {
          id: path.includes("chk") ? "chk-pending" : "rev-pending",
          path,
          title: "Generated task",
          type: "task",
          status: "pending",
          content: "",
        } as any;
      };

      try {
        await (service as any).reconcileFeatureCheckoutDependencies(
          "test-project",
          "feature-review-reconcile"
        );

        expect(updateCalls).toHaveLength(2);

        const checkoutUpdate = updateCalls.find((c) => c.path.includes("chk-pending"));
        expect(checkoutUpdate).toBeDefined();
        expect(checkoutUpdate?.request.depends_on).toEqual(["task-a", "task-b"]);

        const reviewUpdate = updateCalls.find((c) => c.path.includes("rev-pending"));
        expect(reviewUpdate).toBeDefined();
        expect(reviewUpdate?.request.depends_on).toEqual(["task-a", "task-b"]);
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        (service as any).updateInternal = originalUpdateInternal;
      }
    });

    test("does not reconcile terminal or in-progress feature_review tasks", async () => {
      const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;
      const originalUpdateInternal = (service as any).updateInternal;

      const updateCalls: Array<{
        path: string;
        request: Record<string, unknown>;
        options: Record<string, unknown> | undefined;
      }> = [];

      TaskService.prototype.getTasksByFeature = async () =>
        [
          {
            id: "task-a",
            generated: false,
            feature_id: "feature-review-skip",
            status: "pending",
          },
          {
            id: "rev-pending",
            path: "projects/test-project/task/rev-pending.md",
            generated: true,
            generated_kind: "feature_review",
            generated_by: "feature-completion-hook",
            feature_id: "feature-review-skip",
            status: "pending",
            depends_on: [],
          },
          {
            id: "rev-active",
            path: "projects/test-project/task/rev-active.md",
            generated: true,
            generated_kind: "feature_review",
            generated_by: "feature-completion-hook",
            feature_id: "feature-review-skip",
            status: "in_progress",
            depends_on: [],
          },
          {
            id: "rev-completed",
            path: "projects/test-project/task/rev-completed.md",
            generated: true,
            generated_kind: "feature_review",
            generated_by: "feature-completion-hook",
            feature_id: "feature-review-skip",
            status: "completed",
            depends_on: [],
          },
          {
            id: "rev-validated",
            path: "projects/test-project/task/rev-validated.md",
            generated: true,
            generated_kind: "feature_review",
            generated_by: "feature-completion-hook",
            feature_id: "feature-review-skip",
            status: "validated",
            depends_on: [],
          },
        ] as any;

      (service as any).updateInternal = async (
        path: string,
        request: Record<string, unknown>,
        options?: Record<string, unknown>
      ) => {
        updateCalls.push({
          path,
          request: request as unknown as Record<string, unknown>,
          options: options as unknown as Record<string, unknown> | undefined,
        });
        return {
          id: "rev-pending",
          path,
          title: "Feature review",
          type: "task",
          status: "pending",
          content: "",
        } as any;
      };

      try {
        await (service as any).reconcileFeatureCheckoutDependencies(
          "test-project",
          "feature-review-skip"
        );

        expect(updateCalls).toHaveLength(1);
        expect(updateCalls[0]?.path).toBe("projects/test-project/task/rev-pending.md");
        expect(updateCalls[0]?.request.depends_on).toEqual(["task-a"]);
      } finally {
        TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
        (service as any).updateInternal = originalUpdateInternal;
      }
    });
  });

  describe("feature checkout reconciliation hooks", () => {
    const originalGetTasksByFeature = TaskService.prototype.getTasksByFeature;

    beforeEach(() => {
      TaskService.prototype.getTasksByFeature = async () => {
        throw new Error("force filesystem fallback");
      };
    });

    const writeTask = (params: {
      projectId: string;
      fileId: string;
      title: string;
      featureId: string;
      status?: string;
      dependsOn?: string[];
      generated?: boolean;
      generatedKind?: string;
      generatedBy?: string;
    }) => {
      const taskDir = join(TEST_DIR, "projects", params.projectId, "task");
      mkdirSync(taskDir, { recursive: true });

      const dependsOnBlock =
        params.dependsOn && params.dependsOn.length > 0
          ? `depends_on:\n${params.dependsOn.map((id) => `  - ${id}`).join("\n")}\n`
          : "";
      const generatedBlock =
        params.generated === true
          ? `generated: true\ngenerated_kind: ${params.generatedKind || "feature_checkout"}\ngenerated_by: ${params.generatedBy || "feature-checkout"}\n`
          : "";

      const relativePath = `projects/${params.projectId}/task/${params.fileId}.md`;
      const fullPath = join(TEST_DIR, relativePath);
      writeFileSync(
        fullPath,
        `---
title: ${params.title}
type: task
status: ${params.status || "pending"}
feature_id: ${params.featureId}
projectId: ${params.projectId}
${dependsOnBlock}${generatedBlock}---

Task content.
`
      );

      return relativePath;
    };

    afterAll(() => {
      TaskService.prototype.getTasksByFeature = originalGetTasksByFeature;
    });

    const readDependsOn = (relativePath: string): string[] => {
      const raw = readFileSync(join(TEST_DIR, relativePath), "utf-8");
      const { frontmatter } = parseFrontmatter(raw);
      return ((frontmatter.depends_on as string[] | undefined) || []).slice().sort();
    };

    test("save() reconciles checkout dependencies and does not mutate active checkout", async () => {
      const projectId = `recon-save-${Date.now()}`;
      const featureId = "feature-save-scope";

      writeTask({
        projectId,
        fileId: "taska001",
        title: "Base feature task",
        featureId,
      });

      const pendingCheckoutPath = writeTask({
        projectId,
        fileId: "chkp0001",
        title: "Pending checkout",
        featureId,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["taska001"],
      });

      const activeCheckoutPath = writeTask({
        projectId,
        fileId: "chka0001",
        title: "Active checkout",
        featureId,
        status: "in_progress",
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["taska001"],
      });

      const created = await service.save({
        type: "task",
        title: "Task created through save hook",
        content: "content",
        status: "pending",
        feature_id: featureId,
        project: projectId,
      });

      const expected = ["taska001", created.id].sort();
      expect(readDependsOn(pendingCheckoutPath)).toEqual(expected);
      expect(readDependsOn(activeCheckoutPath)).toEqual(["taska001"]);
    });

    test("save() reconciles feature_review dependencies alongside checkout", async () => {
      const projectId = `recon-save-review-${Date.now()}`;
      const featureId = "feature-save-review";

      writeTask({
        projectId,
        fileId: "rvta0001",
        title: "Base feature task",
        featureId,
      });

      const pendingReviewPath = writeTask({
        projectId,
        fileId: "revp0001",
        title: "Pending review",
        featureId,
        generated: true,
        generatedKind: "feature_review",
        generatedBy: "feature-completion-hook",
        dependsOn: ["rvta0001"],
      });

      const activeReviewPath = writeTask({
        projectId,
        fileId: "reva0001",
        title: "Active review",
        featureId,
        status: "in_progress",
        generated: true,
        generatedKind: "feature_review",
        generatedBy: "feature-completion-hook",
        dependsOn: ["rvta0001"],
      });

      const created = await service.save({
        type: "task",
        title: "Task created through save hook for review",
        content: "content",
        status: "pending",
        feature_id: featureId,
        project: projectId,
      });

      const expected = ["rvta0001", created.id].sort();
      expect(readDependsOn(pendingReviewPath)).toEqual(expected);
      expect(readDependsOn(activeReviewPath)).toEqual(["rvta0001"]);
    });

    test("save() does not mutate terminal generated checkout tasks", async () => {
      const projectId = `recon-save-terminal-${Date.now()}`;
      const featureId = "feature-save-terminal";

      writeTask({
        projectId,
        fileId: "term0001",
        title: "Base feature task",
        featureId,
      });

      const pendingCheckoutPath = writeTask({
        projectId,
        fileId: "termpend",
        title: "Pending checkout",
        featureId,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["term0001"],
      });

      const completedCheckoutPath = writeTask({
        projectId,
        fileId: "termdone",
        title: "Completed checkout",
        featureId,
        status: "completed",
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["term0001"],
      });

      const terminalBefore = readFileSync(
        join(TEST_DIR, completedCheckoutPath),
        "utf-8"
      );

      const created = await service.save({
        type: "task",
        title: "Task created through save hook",
        content: "content",
        status: "pending",
        feature_id: featureId,
        project: projectId,
      });

      const expected = ["term0001", created.id].sort();
      expect(readDependsOn(pendingCheckoutPath)).toEqual(expected);

      const terminalAfter = readFileSync(
        join(TEST_DIR, completedCheckoutPath),
        "utf-8"
      );
      expect(terminalAfter).toBe(terminalBefore);
      expect(readDependsOn(completedCheckoutPath)).toEqual(["term0001"]);
    });

    test("update() feature reassignment reconciles old and new feature checkout scopes", async () => {
      const projectId = `recon-update-${Date.now()}`;
      const oldFeature = "feature-old-scope";
      const newFeature = "feature-new-scope";

      const movedTaskPath = writeTask({
        projectId,
        fileId: "movf0001",
        title: "Task to reassign",
        featureId: oldFeature,
      });

      const oldCheckoutPath = writeTask({
        projectId,
        fileId: "oldc0001",
        title: "Old feature checkout",
        featureId: oldFeature,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["movf0001"],
      });

      const newCheckoutPath = writeTask({
        projectId,
        fileId: "newc0001",
        title: "New feature checkout",
        featureId: newFeature,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: [],
      });

      await service.update(movedTaskPath, { feature_id: newFeature });

      expect(readDependsOn(oldCheckoutPath)).toEqual([]);
      expect(readDependsOn(newCheckoutPath)).toEqual(["movf0001"]);
    });

    test("delete() reconciles checkout deps and repeated reconcile is idempotent", async () => {
      const projectId = `recon-delete-${Date.now()}`;
      const featureId = "feature-delete-scope";

      writeTask({
        projectId,
        fileId: "dela0001",
        title: "Task A",
        featureId,
      });

      const taskBPath = writeTask({
        projectId,
        fileId: "delb0001",
        title: "Task B",
        featureId,
      });

      const checkoutPath = writeTask({
        projectId,
        fileId: "delc0001",
        title: "Delete feature checkout",
        featureId,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["dela0001", "delb0001"],
      });

      await service.delete(taskBPath);
      expect(readDependsOn(checkoutPath)).toEqual(["dela0001"]);

      const before = readFileSync(join(TEST_DIR, checkoutPath), "utf-8");
      await service.reconcileFeatureCheckoutDependencies(projectId, featureId);
      const after = readFileSync(join(TEST_DIR, checkoutPath), "utf-8");
      expect(after).toBe(before);
    });

    test("moveEntry() reconciles source and destination project checkout scopes", async () => {
      const sourceProject = `recon-move-src-${Date.now()}`;
      const destinationProject = `recon-move-dst-${Date.now()}`;
      const featureId = "feature-cross-project";

      const movedTaskPath = writeTask({
        projectId: sourceProject,
        fileId: "movx0001",
        title: "Cross-project task",
        featureId,
      });

      const sourceCheckoutPath = writeTask({
        projectId: sourceProject,
        fileId: "srcx0001",
        title: "Source checkout",
        featureId,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: ["movx0001"],
      });

      const destinationCheckoutPath = writeTask({
        projectId: destinationProject,
        fileId: "dstx0001",
        title: "Destination checkout",
        featureId,
        generated: true,
        generatedKind: "feature_checkout",
        generatedBy: "feature-checkout",
        dependsOn: [],
      });

      const result = await service.moveEntry(movedTaskPath, destinationProject);

      expect(result.oldPath).toBe(movedTaskPath);
      expect(result.newPath).toBe(`projects/${destinationProject}/task/movx0001.md`);
      expect(readDependsOn(sourceCheckoutPath)).toEqual([]);
      expect(readDependsOn(destinationCheckoutPath)).toEqual(["movx0001"]);
    });
  });
});
