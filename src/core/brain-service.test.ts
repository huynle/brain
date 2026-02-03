/**
 * Brain Service - Unit Tests
 *
 * Tests for the core brain service operations.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { BrainService } from "./brain-service";
import type { BrainConfig } from "./types";

// =============================================================================
// Test Setup
// =============================================================================

const TEST_DIR = join(tmpdir(), `brain-api-test-${Date.now()}`);
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
});
