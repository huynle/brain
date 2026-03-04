/**
 * Migration Script Tests - Crons to Tasks
 *
 * Tests the migration of cron entity data into task frontmatter.
 * Uses temp directories with mock brain structure.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import {
  existsSync,
  mkdirSync,
  rmSync,
  readFileSync,
  writeFileSync,
} from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { parseFrontmatter } from "../core/note-utils";
import { migrateCronsToTasks } from "./migrate-crons-to-tasks";

// =============================================================================
// Helpers
// =============================================================================

function makeCronFile(
  dir: string,
  projectId: string,
  cronId: string,
  fm: Record<string, unknown>,
  body = "",
): string {
  const cronDir = join(dir, "projects", projectId, "cron");
  mkdirSync(cronDir, { recursive: true });
  const filePath = join(cronDir, `${cronId}.md`);
  const lines: string[] = [];
  for (const [key, value] of Object.entries(fm)) {
    if (key === "runs" && Array.isArray(value)) {
      lines.push("runs:");
      for (const run of value) {
        lines.push(`  - run_id: ${run.run_id}`);
        lines.push(`    status: ${run.status}`);
        lines.push(`    started: ${run.started}`);
        if (run.completed) lines.push(`    completed: ${run.completed}`);
        if (run.duration !== undefined)
          lines.push(`    duration: ${run.duration}`);
        if (run.tasks !== undefined) lines.push(`    tasks: ${run.tasks}`);
      }
    } else if (key === "tags" && Array.isArray(value)) {
      lines.push("tags:");
      for (const tag of value) {
        lines.push(`  - ${tag}`);
      }
    } else {
      lines.push(`${key}: ${value}`);
    }
  }
  const content = `---\n${lines.join("\n")}\n---\n\n${body}\n`;
  writeFileSync(filePath, content, "utf-8");
  return filePath;
}

function makeTaskFile(
  dir: string,
  projectId: string,
  taskId: string,
  fm: Record<string, unknown>,
  body = "Task body",
): string {
  const taskDir = join(dir, "projects", projectId, "task");
  mkdirSync(taskDir, { recursive: true });
  const filePath = join(taskDir, `${taskId}.md`);
  const lines: string[] = [];
  for (const [key, value] of Object.entries(fm)) {
    if (key === "cron_ids" && Array.isArray(value)) {
      lines.push("cron_ids:");
      for (const id of value) {
        lines.push(`  - ${id}`);
      }
    } else if (key === "tags" && Array.isArray(value)) {
      lines.push("tags:");
      for (const tag of value) {
        lines.push(`  - ${tag}`);
      }
    } else if (key === "depends_on" && Array.isArray(value)) {
      lines.push("depends_on:");
      for (const dep of value) {
        lines.push(`  - ${dep}`);
      }
    } else {
      lines.push(`${key}: ${value}`);
    }
  }
  const content = `---\n${lines.join("\n")}\n---\n\n${body}\n`;
  writeFileSync(filePath, content, "utf-8");
  return filePath;
}

// =============================================================================
// Test Suite
// =============================================================================

let TEST_DIR: string;

beforeEach(() => {
  TEST_DIR = join(tmpdir(), `brain-migrate-cron-test-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`);
  mkdirSync(TEST_DIR, { recursive: true });
});

afterEach(() => {
  if (existsSync(TEST_DIR)) {
    rmSync(TEST_DIR, { recursive: true, force: true });
  }
});

describe("migrateCronsToTasks", () => {
  // ---------------------------------------------------------------------------
  // Basic single-task cron migration
  // ---------------------------------------------------------------------------
  describe("single-task cron migration", () => {
    test("copies cron schedule metadata to the linked task", async () => {
      // Create a cron entry
      makeCronFile(TEST_DIR, "proj1", "cron1abc", {
        title: "Daily Backup (Cron)",
        type: "cron",
        status: "active",
        schedule: "0 2 * * *",
        next_run: "2026-03-01T02:00:00Z",
        max_runs: 10,
        starts_at: "2026-01-01T00:00:00Z",
        expires_at: "2027-01-01T00:00:00Z",
      });

      // Create a task linked to the cron
      makeTaskFile(TEST_DIR, "proj1", "task1abc", {
        title: "Daily Backup",
        type: "task",
        status: "active",
        cron_ids: ["cron1abc"],
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(1);
      expect(result.tasksUpdated).toBe(1);
      expect(result.cronFilesDeleted).toBe(1);
      expect(result.warnings).toHaveLength(0);

      // Verify task was updated with cron metadata
      const taskContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "task1abc.md"),
        "utf-8",
      );
      const { frontmatter } = parseFrontmatter(taskContent);

      expect(frontmatter.schedule).toBe("0 2 * * *");
      expect(frontmatter.next_run).toBe("2026-03-01T02:00:00Z");
      expect(frontmatter.max_runs).toBe(10);
      expect(frontmatter.starts_at).toBe("2026-01-01T00:00:00Z");
      expect(frontmatter.expires_at).toBe("2027-01-01T00:00:00Z");

      // cron_ids should be removed
      expect(frontmatter.cron_ids).toBeUndefined();

      // Cron file should be deleted
      expect(
        existsSync(
          join(TEST_DIR, "projects", "proj1", "cron", "cron1abc.md"),
        ),
      ).toBe(false);
    });

    test("copies runs history from cron to task", async () => {
      makeCronFile(TEST_DIR, "proj1", "cron2abc", {
        title: "Scheduled Task (Cron)",
        type: "cron",
        status: "active",
        schedule: "*/5 * * * *",
        runs: [
          {
            run_id: "20260228-0200",
            status: "completed",
            started: "2026-02-28T02:00:00Z",
            completed: "2026-02-28T02:01:30Z",
            duration: 90000,
            tasks: 1,
          },
          {
            run_id: "20260228-0205",
            status: "failed",
            started: "2026-02-28T02:05:00Z",
            completed: "2026-02-28T02:05:10Z",
            duration: 10000,
            tasks: 1,
          },
        ],
      });

      makeTaskFile(TEST_DIR, "proj1", "task2abc", {
        title: "Scheduled Task",
        type: "task",
        status: "active",
        cron_ids: ["cron2abc"],
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(1);
      expect(result.tasksUpdated).toBe(1);

      const taskContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "task2abc.md"),
        "utf-8",
      );
      const { frontmatter } = parseFrontmatter(taskContent);

      expect(frontmatter.schedule).toBe("*/5 * * * *");
      expect(Array.isArray(frontmatter.runs)).toBe(true);
      const runs = frontmatter.runs as Array<Record<string, unknown>>;
      expect(runs).toHaveLength(2);
      expect(runs[0].run_id).toBe("20260228-0200");
      expect(runs[0].status).toBe("completed");
      expect(runs[1].run_id).toBe("20260228-0205");
      expect(runs[1].status).toBe("failed");
    });

    test("preserves existing task body content", async () => {
      makeCronFile(TEST_DIR, "proj1", "cronbody", {
        title: "Test Cron",
        type: "cron",
        status: "active",
        schedule: "0 0 * * *",
      });

      makeTaskFile(
        TEST_DIR,
        "proj1",
        "taskbody",
        {
          title: "Test Task",
          type: "task",
          status: "active",
          cron_ids: ["cronbody"],
        },
        "This is the task body\n\nWith multiple paragraphs.",
      );

      await migrateCronsToTasks(TEST_DIR);

      const taskContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskbody.md"),
        "utf-8",
      );
      const { body } = parseFrontmatter(taskContent);
      expect(body).toContain("This is the task body");
      expect(body).toContain("With multiple paragraphs.");
    });
  });

  // ---------------------------------------------------------------------------
  // Multi-task cron migration
  // ---------------------------------------------------------------------------
  describe("multi-task cron migration", () => {
    test("copies schedule to root task (no depends_on) and removes cron_ids from all", async () => {
      makeCronFile(TEST_DIR, "proj1", "cronmulti", {
        title: "Pipeline Cron",
        type: "cron",
        status: "active",
        schedule: "0 3 * * *",
        next_run: "2026-03-01T03:00:00Z",
      });

      // Root task (no depends_on)
      makeTaskFile(TEST_DIR, "proj1", "taskroot", {
        title: "Pipeline Root",
        type: "task",
        status: "active",
        cron_ids: ["cronmulti"],
      });

      // Child task (depends on root)
      makeTaskFile(TEST_DIR, "proj1", "taskchld", {
        title: "Pipeline Child",
        type: "task",
        status: "active",
        cron_ids: ["cronmulti"],
        depends_on: ["taskroot"],
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(1);
      expect(result.tasksUpdated).toBe(2);
      expect(result.cronFilesDeleted).toBe(1);

      // Root task should have schedule metadata
      const rootContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskroot.md"),
        "utf-8",
      );
      const rootFm = parseFrontmatter(rootContent).frontmatter;
      expect(rootFm.schedule).toBe("0 3 * * *");
      expect(rootFm.next_run).toBe("2026-03-01T03:00:00Z");
      expect(rootFm.cron_ids).toBeUndefined();

      // Child task should NOT have schedule metadata, but cron_ids removed
      const childContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskchld.md"),
        "utf-8",
      );
      const childFm = parseFrontmatter(childContent).frontmatter;
      expect(childFm.schedule).toBeUndefined();
      expect(childFm.cron_ids).toBeUndefined();
      // depends_on should be preserved
      expect(childFm.depends_on).toEqual(["taskroot"]);
    });

    test("picks first task as root when all tasks have depends_on", async () => {
      makeCronFile(TEST_DIR, "proj1", "croncirc", {
        title: "Circular Cron",
        type: "cron",
        status: "active",
        schedule: "0 4 * * *",
      });

      // Both tasks have depends_on (unusual but possible)
      makeTaskFile(TEST_DIR, "proj1", "taska111", {
        title: "Task A",
        type: "task",
        status: "active",
        cron_ids: ["croncirc"],
        depends_on: ["taskb222"],
      });

      makeTaskFile(TEST_DIR, "proj1", "taskb222", {
        title: "Task B",
        type: "task",
        status: "active",
        cron_ids: ["croncirc"],
        depends_on: ["taska111"],
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(1);
      expect(result.tasksUpdated).toBe(2);

      // One of them should have the schedule (the first alphabetically)
      const taskAContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taska111.md"),
        "utf-8",
      );
      const taskBContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskb222.md"),
        "utf-8",
      );
      const fmA = parseFrontmatter(taskAContent).frontmatter;
      const fmB = parseFrontmatter(taskBContent).frontmatter;

      // First alphabetically (taska111) gets the schedule
      expect(fmA.schedule).toBe("0 4 * * *");
      expect(fmB.schedule).toBeUndefined();
      expect(fmA.cron_ids).toBeUndefined();
      expect(fmB.cron_ids).toBeUndefined();
    });
  });

  // ---------------------------------------------------------------------------
  // Edge cases
  // ---------------------------------------------------------------------------
  describe("edge cases", () => {
    test("orphaned cron (no linked tasks) is deleted with warning", async () => {
      makeCronFile(TEST_DIR, "proj1", "cronorph", {
        title: "Orphaned Cron",
        type: "cron",
        status: "active",
        schedule: "0 5 * * *",
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(1);
      expect(result.tasksUpdated).toBe(0);
      expect(result.cronFilesDeleted).toBe(1);
      expect(result.warnings.length).toBeGreaterThan(0);
      expect(result.warnings[0]).toContain("orphan");
    });

    test("task linked to multiple crons gets metadata from all", async () => {
      makeCronFile(TEST_DIR, "proj1", "cronaaaa", {
        title: "Cron A",
        type: "cron",
        status: "active",
        schedule: "0 1 * * *",
      });

      makeCronFile(TEST_DIR, "proj1", "cronbbbb", {
        title: "Cron B",
        type: "cron",
        status: "active",
        schedule: "0 2 * * *",
      });

      makeTaskFile(TEST_DIR, "proj1", "taskmult", {
        title: "Multi-Cron Task",
        type: "task",
        status: "active",
        cron_ids: ["cronaaaa", "cronbbbb"],
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(2);
      expect(result.cronFilesDeleted).toBe(2);
      // Task should be updated - last cron wins for schedule
      expect(result.tasksUpdated).toBeGreaterThanOrEqual(1);

      const taskContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskmult.md"),
        "utf-8",
      );
      const { frontmatter } = parseFrontmatter(taskContent);
      expect(frontmatter.cron_ids).toBeUndefined();
      // Should have a schedule from one of the crons
      expect(frontmatter.schedule).toBeDefined();
    });

    test("handles multiple projects", async () => {
      // Project A
      makeCronFile(TEST_DIR, "projA", "cronpra1", {
        title: "Proj A Cron",
        type: "cron",
        status: "active",
        schedule: "0 1 * * *",
      });
      makeTaskFile(TEST_DIR, "projA", "taskpra1", {
        title: "Proj A Task",
        type: "task",
        status: "active",
        cron_ids: ["cronpra1"],
      });

      // Project B
      makeCronFile(TEST_DIR, "projB", "cronprb1", {
        title: "Proj B Cron",
        type: "cron",
        status: "active",
        schedule: "0 2 * * *",
      });
      makeTaskFile(TEST_DIR, "projB", "taskprb1", {
        title: "Proj B Task",
        type: "task",
        status: "active",
        cron_ids: ["cronprb1"],
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(2);
      expect(result.tasksUpdated).toBe(2);
      expect(result.cronFilesDeleted).toBe(2);

      // Verify both projects migrated
      expect(
        existsSync(join(TEST_DIR, "projects", "projA", "cron", "cronpra1.md")),
      ).toBe(false);
      expect(
        existsSync(join(TEST_DIR, "projects", "projB", "cron", "cronprb1.md")),
      ).toBe(false);

      const taskA = parseFrontmatter(
        readFileSync(
          join(TEST_DIR, "projects", "projA", "task", "taskpra1.md"),
          "utf-8",
        ),
      ).frontmatter;
      expect(taskA.schedule).toBe("0 1 * * *");
      expect(taskA.cron_ids).toBeUndefined();

      const taskB = parseFrontmatter(
        readFileSync(
          join(TEST_DIR, "projects", "projB", "task", "taskprb1.md"),
          "utf-8",
        ),
      ).frontmatter;
      expect(taskB.schedule).toBe("0 2 * * *");
      expect(taskB.cron_ids).toBeUndefined();
    });

    test("no projects directory returns zero counts", async () => {
      // Empty brain dir, no projects/ subdirectory
      const emptyDir = join(
        tmpdir(),
        `brain-empty-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      );
      mkdirSync(emptyDir, { recursive: true });

      try {
        const result = await migrateCronsToTasks(emptyDir);
        expect(result.cronsProcessed).toBe(0);
        expect(result.tasksUpdated).toBe(0);
        expect(result.cronFilesDeleted).toBe(0);
      } finally {
        rmSync(emptyDir, { recursive: true, force: true });
      }
    });

    test("project with no cron directory is skipped gracefully", async () => {
      // Create project with only task dir, no cron dir
      const taskDir = join(TEST_DIR, "projects", "nocron", "task");
      mkdirSync(taskDir, { recursive: true });
      makeTaskFile(TEST_DIR, "nocron", "taskonly", {
        title: "Task Only",
        type: "task",
        status: "active",
      });

      const result = await migrateCronsToTasks(TEST_DIR);

      expect(result.cronsProcessed).toBe(0);
      expect(result.tasksUpdated).toBe(0);
      expect(result.cronFilesDeleted).toBe(0);
    });
  });

  // ---------------------------------------------------------------------------
  // Dry-run mode
  // ---------------------------------------------------------------------------
  describe("dry-run mode", () => {
    test("reports planned changes without modifying files", async () => {
      makeCronFile(TEST_DIR, "proj1", "crondry1", {
        title: "Dry Run Cron",
        type: "cron",
        status: "active",
        schedule: "0 6 * * *",
        next_run: "2026-03-01T06:00:00Z",
      });

      makeTaskFile(TEST_DIR, "proj1", "taskdry1", {
        title: "Dry Run Task",
        type: "task",
        status: "active",
        cron_ids: ["crondry1"],
      });

      const result = await migrateCronsToTasks(TEST_DIR, { dryRun: true });

      // Should report what would happen
      expect(result.cronsProcessed).toBe(1);
      expect(result.tasksUpdated).toBe(1);
      expect(result.cronFilesDeleted).toBe(1);
      expect(result.dryRun).toBe(true);

      // But files should NOT be modified
      const taskContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskdry1.md"),
        "utf-8",
      );
      // cron_ids should still be in the raw file (not removed by dry-run)
      expect(taskContent).toContain("cron_ids:");
      expect(taskContent).toContain("crondry1");
      const { frontmatter } = parseFrontmatter(taskContent);
      // schedule should NOT have been copied (dry-run doesn't modify files)
      expect(frontmatter.schedule).toBeUndefined();

      // Cron file should still exist
      expect(
        existsSync(
          join(TEST_DIR, "projects", "proj1", "cron", "crondry1.md"),
        ),
      ).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------
  // Preserves non-cron task fields
  // ---------------------------------------------------------------------------
  describe("field preservation", () => {
    test("preserves existing task fields not related to cron", async () => {
      makeCronFile(TEST_DIR, "proj1", "cronpres", {
        title: "Preserve Cron",
        type: "cron",
        status: "active",
        schedule: "0 7 * * *",
      });

      makeTaskFile(TEST_DIR, "proj1", "taskpres", {
        title: "Preserve Task",
        type: "task",
        status: "pending",
        priority: "high",
        cron_ids: ["cronpres"],
        tags: ["important", "daily"],
      });

      await migrateCronsToTasks(TEST_DIR);

      const taskContent = readFileSync(
        join(TEST_DIR, "projects", "proj1", "task", "taskpres.md"),
        "utf-8",
      );
      const { frontmatter } = parseFrontmatter(taskContent);

      // Cron fields added
      expect(frontmatter.schedule).toBe("0 7 * * *");
      // Existing fields preserved
      expect(frontmatter.title).toBe("Preserve Task");
      expect(frontmatter.type).toBe("task");
      expect(frontmatter.status).toBe("pending");
      expect(frontmatter.priority).toBe("high");
      expect(frontmatter.tags).toEqual(["important", "daily"]);
      // cron_ids removed
      expect(frontmatter.cron_ids).toBeUndefined();
    });
  });
});
