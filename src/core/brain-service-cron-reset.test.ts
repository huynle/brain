/**
 * Brain Service - Cron Task Auto-Reset Tests
 *
 * Tests for the auto-reset feature for cron-linked tasks.
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

const TEST_DIR = join(tmpdir(), `brain-cron-reset-test-${Date.now()}`);
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
  mkdirSync(join(TEST_DIR, "projects", "test-project", "task"), {
    recursive: true,
  });
  mkdirSync(join(TEST_DIR, "projects", "test-project", "cron"), {
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

describe("BrainService - Cron Task Auto-Reset", () => {
  describe("auto-reset on completion", () => {
    test("should reset task to pending when completed with active cron_ids", async () => {
      // Setup: Create an active cron
      const cronDir = join(TEST_DIR, "projects", "test-project", "cron");
      const cronFile = join(cronDir, "test-cron-abc123.md");
      const cronPath = "projects/test-project/cron/test-cron-abc123.md";

      writeFileSync(
        cronFile,
        `---
title: Test Cron
type: cron
status: active
schedule: "0 * * * *"
---

Test cron entry.
`
      );

      // Setup: Create a task linked to the cron
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      const taskFile = join(taskDir, "test-task-xyz789.md");
      const taskPath = "projects/test-project/task/test-task-xyz789.md";

      writeFileSync(
        taskFile,
        `---
title: Test Task
type: task
status: in_progress
cron_ids:
  - abc123
---

Test task content.
`
      );

      // Act: Mark task as completed
      await service.update(taskPath, {
        status: "completed",
      });

      // Assert: Task should be auto-reset to pending
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("pending");
      expect(updatedTask.content).toContain("Auto-reset for next cron run");
    });

    test("should NOT reset task when cron is not active", async () => {
      // Setup: Create an inactive cron
      const cronDir = join(TEST_DIR, "projects", "test-project", "cron");
      const cronFile = join(cronDir, "inactive-cron-def456.md");
      const cronPath = "projects/test-project/cron/inactive-cron-def456.md";

      writeFileSync(
        cronFile,
        `---
title: Inactive Cron
type: cron
status: completed
schedule: "0 * * * *"
---

Inactive cron entry.
`
      );

      // Setup: Create a task linked to the inactive cron
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      const taskFile = join(taskDir, "inactive-task-uvw012.md");
      const taskPath = "projects/test-project/task/inactive-task-uvw012.md";

      writeFileSync(
        taskFile,
        `---
title: Inactive Task
type: task
status: in_progress
cron_ids:
  - def456
---

Task linked to inactive cron.
`
      );

      // Act: Mark task as completed
      await service.update(taskPath, {
        status: "completed",
      });

      // Assert: Task should stay completed
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("completed");
      expect(updatedTask.content).not.toContain("Auto-reset for next cron run");
    });

    test("should reset if ANY cron is active (multiple cron_ids)", async () => {
      // Setup: Create two crons - one active, one inactive
      const cronDir = join(TEST_DIR, "projects", "test-project", "cron");
      
      const activeCronFile = join(cronDir, "active-cron-ghi789.md");
      writeFileSync(
        activeCronFile,
        `---
title: Active Cron
type: cron
status: active
schedule: "0 * * * *"
---

Active cron.
`
      );

      const inactiveCronFile = join(cronDir, "inactive-cron-jkl012.md");
      writeFileSync(
        inactiveCronFile,
        `---
title: Inactive Cron
type: cron
status: archived
schedule: "0 * * * *"
---

Inactive cron.
`
      );

      // Setup: Create a task linked to both crons
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      const taskFile = join(taskDir, "multi-cron-task-mno345.md");
      const taskPath = "projects/test-project/task/multi-cron-task-mno345.md";

      writeFileSync(
        taskFile,
        `---
title: Multi-Cron Task
type: task
status: in_progress
cron_ids:
  - ghi789
  - jkl012
---

Task linked to multiple crons.
`
      );

      // Act: Mark task as completed
      await service.update(taskPath, {
        status: "completed",
      });

      // Assert: Task should be auto-reset (because ghi789 is active)
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("pending");
      expect(updatedTask.content).toContain("Auto-reset for next cron run");
    });

    test("should NOT reset task without cron_ids", async () => {
      // Setup: Create a regular task without cron_ids
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      const taskFile = join(taskDir, "regular-task-pqr678.md");
      const taskPath = "projects/test-project/task/regular-task-pqr678.md";

      writeFileSync(
        taskFile,
        `---
title: Regular Task
type: task
status: in_progress
---

Regular task without cron.
`
      );

      // Act: Mark task as completed
      await service.update(taskPath, {
        status: "completed",
      });

      // Assert: Task should stay completed
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("completed");
      expect(updatedTask.content).not.toContain("Auto-reset for next cron run");
    });

    test("should handle validated status same as completed", async () => {
      // Setup: Create an active cron
      const cronDir = join(TEST_DIR, "projects", "test-project", "cron");
      const cronFile = join(cronDir, "validated-cron-stu901.md");

      writeFileSync(
        cronFile,
        `---
title: Validated Cron
type: cron
status: active
schedule: "0 * * * *"
---

Validated cron.
`
      );

      // Setup: Create a task linked to the cron
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      const taskFile = join(taskDir, "validated-task-vwx234.md");
      const taskPath = "projects/test-project/task/validated-task-vwx234.md";

      writeFileSync(
        taskFile,
        `---
title: Validated Task
type: task
status: in_progress
cron_ids:
  - stu901
---

Task to be validated.
`
      );

      // Act: Mark task as validated
      await service.update(taskPath, {
        status: "validated",
      });

      // Assert: Task should be auto-reset to pending
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("pending");
      expect(updatedTask.content).toContain("Auto-reset for next cron run");
    });

    test("should NOT reset non-task entries", async () => {
      // Setup: Create an active cron
      const cronDir = join(TEST_DIR, "projects", "test-project", "cron");
      const cronFile = join(cronDir, "non-task-cron-yza567.md");

      writeFileSync(
        cronFile,
        `---
title: Non-Task Cron
type: cron
status: active
schedule: "0 * * * *"
---

Non-task cron.
`
      );

      // Setup: Create a non-task entry (e.g., report) with cron_ids
      const reportDir = join(TEST_DIR, "projects", "test-project");
      mkdirSync(join(reportDir, "report"), { recursive: true });
      const reportFile = join(reportDir, "report", "test-report-bcd890.md");
      const reportPath = "projects/test-project/report/test-report-bcd890.md";

      writeFileSync(
        reportFile,
        `---
title: Test Report
type: report
status: in_progress
cron_ids:
  - yza567
---

Report content.
`
      );

      // Act: Mark report as completed
      await service.update(reportPath, {
        status: "completed",
      });

      // Assert: Report should stay completed (not a task)
      const updatedReport = await service.recall(reportPath);
      expect(updatedReport.status).toBe("completed");
      expect(updatedReport.content).not.toContain("Auto-reset for next cron run");
    });
  });

  describe("edge cases", () => {
    test("should handle missing project ID gracefully", async () => {
      // This is a synthetic test - in practice, entries always have project IDs
      // But we test the guard logic nonetheless
      
      // Setup: Create a task with cron_ids in an unusual location
      const taskFile = join(TEST_DIR, "orphan-task-efg123.md");
      const taskPath = "orphan-task-efg123.md"; // No projects/ prefix

      writeFileSync(
        taskFile,
        `---
title: Orphan Task
type: task
status: in_progress
cron_ids:
  - some-cron
---

Orphan task.
`
      );

      // Act: Mark task as completed (should not throw)
      await service.update(taskPath, {
        status: "completed",
      });

      // Assert: Task should stay completed (no project ID to look up crons)
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("completed");
    });

    test("should handle cron lookup failure gracefully", async () => {
      // Setup: Create a task with cron_ids referencing non-existent cron
      const taskDir = join(TEST_DIR, "projects", "test-project", "task");
      const taskFile = join(taskDir, "missing-cron-task-hij456.md");
      const taskPath = "projects/test-project/task/missing-cron-task-hij456.md";

      writeFileSync(
        taskFile,
        `---
title: Missing Cron Task
type: task
status: in_progress
cron_ids:
  - nonexistent-cron-999
---

Task with missing cron.
`
      );

      // Act: Mark task as completed (should not throw)
      await service.update(taskPath, {
        status: "completed",
      });

      // Assert: Task should stay completed (cron not found = not active)
      const updatedTask = await service.recall(taskPath);
      expect(updatedTask.status).toBe("completed");
    });
  });
});
