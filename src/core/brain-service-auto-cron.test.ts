/**
 * Brain Service - Auto-Cron Creation Tests
 *
 * Tests for automatic cron creation when saving tasks with schedule parameter.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, readFileSync } from "fs";
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
      
      // Recall the full task entry to check cron_ids
      const taskEntry = await service.recall(result.path);
      
      // Task should have cron_ids populated
      expect(taskEntry.cron_ids).toBeDefined();
      expect(taskEntry.cron_ids).toBeArray();
      expect(taskEntry.cron_ids!.length).toBe(1);
      
      // Task should NOT have schedule field (moved to cron)
      expect(taskEntry.schedule).toBeUndefined();
      
      // Cron should be created with correct title format
      const cronId = taskEntry.cron_ids![0];
      const cronPath = `projects/test-pro/cron/${cronId}.md`;
      const cronFile = join(TEST_DIR, cronPath);
      
      expect(existsSync(cronFile)).toBe(true);
      
      const cronContent = readFileSync(cronFile, "utf-8");
      const { frontmatter, body } = parseFrontmatter(cronContent);
      
      expect(frontmatter.title).toBe("Daily Task (Cron)");
      expect(frontmatter.type).toBe("cron");
      expect(frontmatter.schedule).toBe("0 2 * * *");
      expect(frontmatter.status).toBe("active");
      expect(body.trim()).toBe(""); // Empty content as per requirements
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

      // Now create task with both schedule and cron_ids
      const taskResult = await service.save({
        type: "task",
        title: "Task With Existing Cron",
        content: "Task content",
        schedule: "0 2 * * *", // This should be ignored
        cron_ids: [cronResult.id],
      });

      // Recall task to check cron_ids
      const taskEntry = await service.recall(taskResult.path);
      
      // Task should have the manually specified cron_ids (not a new auto-created one)
      expect(taskEntry.cron_ids).toEqual([cronResult.id]);
      
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
      
      // Should NOT have cron_ids (would indicate recursion)
      expect(cronEntry.cron_ids).toBeUndefined();
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

      // Recall task to get cron_ids
      const taskEntry = await service.recall(result.path);

      const cronId = taskEntry.cron_ids![0];
      const cronPath = `projects/test-pro/cron/${cronId}.md`;
      const cronFile = join(TEST_DIR, cronPath);
      
      const cronContent = readFileSync(cronFile, "utf-8");
      const { frontmatter } = parseFrontmatter(cronContent);
      
      expect(frontmatter.title).toBe("Weekly Backup (Cron)");
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

      // Recall task to get cron ID
      const taskEntry = await service.recall(taskResult.path);
      expect(taskEntry.cron_ids).toBeDefined();
      expect(taskEntry.cron_ids!.length).toBe(1);

      // Recall the auto-created cron by its path
      const cronId = taskEntry.cron_ids![0];
      const cronPath = `projects/test-pro/cron/${cronId}.md`;
      const cronEntry = await service.recall(cronPath);
      
      // Should find the auto-created cron with correct properties
      expect(cronEntry).toBeDefined();
      expect(cronEntry.title).toBe("Discoverable Task (Cron)");
      expect(cronEntry.type).toBe("cron");
      expect(cronEntry.schedule).toBe("0 5 * * *");
    });
  });
});
