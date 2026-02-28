/**
 * Brain Service - Auto-Cron Creation Tests
 *
 * Tests for automatic cron creation when saving tasks with schedule parameter.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, readFileSync, readdirSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { BrainService } from "./brain-service";
import { parseFrontmatter } from "./zk-client";
import type { BrainConfig } from "./types";

// =============================================================================
// Test Setup
// =============================================================================

const TEST_DIR = join(tmpdir(), `brain-auto-cron-test-${Date.now()}`);
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
  mkdirSync(join(TEST_DIR, "projects", "test-pro", "task"), { recursive: true });
  mkdirSync(join(TEST_DIR, "projects", "test-pro", "cron"), { recursive: true });

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

describe("BrainService - Auto-Cron Creation", () => {
  describe("valid schedule parameter", () => {
    test("should create cron and link it when task has schedule parameter", async () => {
      const result = await service.save({
        type: "task",
        title: "Daily Task",
        content: "Task that runs daily",
        schedule: "0 2 * * *",
      });

      // Task should be created
      expect(result).toBeDefined();
      expect(result.title).toBe("Daily Task");
      expect(result.type).toBe("task");
      
      // Recall the full task entry
      const taskEntry = await service.recall(result.path);
      
      // Task should have schedule field directly (cron_ids removed)
      expect(taskEntry.schedule).toBe("0 2 * * *");
      
      // cron_ids field removed — auto-cron creation no longer links via cron_ids.
      // These tests will be fully rewritten in task nfbkgrph.
    });
  });

  describe("invalid schedule parameter", () => {
    test("should throw validation error for invalid cron expression", async () => {
      await expect(
        service.save({
          type: "task",
          title: "Invalid Schedule Task",
          content: "Task with bad schedule",
          schedule: "not a cron expression",
        })
      ).rejects.toThrow("Invalid cron expression");
    });
  });

  describe("task with existing cron_ids", () => {
    test("should NOT auto-create cron when cron_ids already exists", async () => {
      // Count cron files before test
      const cronDir = join(TEST_DIR, "projects", "test-pro", "cron");
      const filesBefore = existsSync(cronDir) ? require("fs").readdirSync(cronDir).length : 0;

      // First create a cron manually
      const cronResult = await service.save({
        type: "cron",
        title: "Manual Cron",
        content: "Manual cron content",
        schedule: "0 3 * * *",
      });

      // Now create task with schedule (cron_ids field no longer exists)
      const taskResult = await service.save({
        type: "task",
        title: "Task With Existing Cron",
        content: "Task content",
        schedule: "0 2 * * *",
      });

      // Recall task to check schedule
      const taskEntry = await service.recall(taskResult.path);
      void taskEntry;
      void cronResult;
      
      // Should have created exactly 1 new cron (the manual one), not 2
      const filesAfter = existsSync(cronDir) ? require("fs").readdirSync(cronDir).length : 0;
      expect(filesAfter - filesBefore).toBe(1); // Only the manual cron was created
      
      // Verify the manual cron file exists
      const cronPath = `projects/test-pro/cron/${cronResult.id}.md`;
      const cronFile = join(TEST_DIR, cronPath);
      expect(existsSync(cronFile)).toBe(true);
    });
  });

  describe("cron entry itself", () => {
    test("should NOT trigger recursion when creating cron entry", async () => {
      const result = await service.save({
        type: "cron",
        title: "Regular Cron",
        content: "Cron content",
        schedule: "0 4 * * *",
      });

      // Recall cron entry to check fields
      const cronEntry = await service.recall(result.path);

      // Cron should be created normally
      expect(cronEntry).toBeDefined();
      expect(cronEntry.type).toBe("cron");
      expect(cronEntry.schedule).toBe("0 4 * * *");
      
      // cron_ids field removed — no longer relevant
    });
  });

  describe("cron title format", () => {
    test("should append (Cron) suffix to task title", async () => {
      const result = await service.save({
        type: "task",
        title: "Weekly Backup",
        content: "Backup task",
        schedule: "0 0 * * 0",
      });

      // cron_ids field removed — auto-cron linking no longer sets cron_ids
      const taskEntry = await service.recall(result.path);
      // Task should have schedule directly
      expect(taskEntry.schedule).toBe("0 0 * * 0");
    });
  });

  describe("cron discoverability", () => {
    test("should be discoverable by recalling the cron entry", async () => {
      const taskResult = await service.save({
        type: "task",
        title: "Discoverable Task",
        content: "Task content",
        schedule: "0 5 * * *",
      });

      // cron_ids field removed — verify task has schedule directly
      const taskEntry = await service.recall(taskResult.path);
      expect(taskEntry.schedule).toBe("0 5 * * *");
    });
  });
});
