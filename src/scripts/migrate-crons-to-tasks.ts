/**
 * Migration Script: Crons to Tasks
 *
 * Converts existing cron entity data into task frontmatter and cleans up cron files.
 * This flattens the cron/task relationship so tasks own their own schedule metadata.
 *
 * Run: bun run src/scripts/migrate-crons-to-tasks.ts [--dry-run]
 */

import {
  existsSync,
  readdirSync,
  readFileSync,
  writeFileSync,
  unlinkSync,
} from "fs";
import { join } from "path";
import { homedir } from "os";
import {
  parseFrontmatter,
  serializeFrontmatter,
} from "../core/zk-client";

// =============================================================================
// Types
// =============================================================================

export interface MigrationResult {
  cronsProcessed: number;
  tasksUpdated: number;
  cronFilesDeleted: number;
  warnings: string[];
  dryRun: boolean;
}

/** Fields we copy from cron to task */
const CRON_SCHEDULE_FIELDS = [
  "schedule",
  "next_run",
  "max_runs",
  "starts_at",
  "expires_at",
  "runs",
] as const;

// =============================================================================
// Core Migration
// =============================================================================

/**
 * Migrate all cron entries into task frontmatter across all projects.
 *
 * For each cron:
 * 1. Find tasks that reference it via cron_ids
 * 2. Copy schedule metadata to the appropriate task (root task for multi-task crons)
 * 3. Remove cron_ids from all affected tasks
 * 4. Delete the cron file
 */
export async function migrateCronsToTasks(
  brainDir: string,
  options?: { dryRun?: boolean },
): Promise<MigrationResult> {
  const dryRun = options?.dryRun ?? false;
  const result: MigrationResult = {
    cronsProcessed: 0,
    tasksUpdated: 0,
    cronFilesDeleted: 0,
    warnings: [],
    dryRun,
  };

  const projectsDir = join(brainDir, "projects");
  if (!existsSync(projectsDir)) {
    return result;
  }

  // Discover all projects
  const projects = readdirSync(projectsDir, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name);

  for (const projectId of projects) {
    migrateProject(brainDir, projectId, result, dryRun);
  }

  return result;
}

/**
 * Migrate all crons within a single project.
 */
function migrateProject(
  brainDir: string,
  projectId: string,
  result: MigrationResult,
  dryRun: boolean,
): void {
  const cronDir = join(brainDir, "projects", projectId, "cron");
  const taskDir = join(brainDir, "projects", projectId, "task");

  if (!existsSync(cronDir)) {
    return;
  }

  // Read all cron files
  const cronFiles = readdirSync(cronDir)
    .filter((f) => f.endsWith(".md"))
    .sort();

  if (cronFiles.length === 0) {
    return;
  }

  // Read all task files and parse their frontmatter (we need to scan for cron_ids)
  const taskEntries = loadTaskEntries(taskDir);

  // Track which tasks have been updated (to avoid double-counting)
  const updatedTaskIds = new Set<string>();

  for (const cronFile of cronFiles) {
    const cronId = cronFile.replace(/\.md$/, "");
    const cronPath = join(cronDir, cronFile);

    const cronContent = readFileSync(cronPath, "utf-8");
    const { frontmatter: cronFm } = parseFrontmatter(cronContent);

    result.cronsProcessed++;

    // Find all tasks that reference this cron
    const linkedTasks = taskEntries.filter((t) => {
      const cronIds = t.frontmatter.cron_ids;
      return Array.isArray(cronIds) && cronIds.includes(cronId);
    });

    if (linkedTasks.length === 0) {
      // Orphaned cron - no tasks reference it
      result.warnings.push(
        `orphan: cron ${cronId} in project ${projectId} has no linked tasks, deleting`,
      );
    } else if (linkedTasks.length === 1) {
      // Single-task cron: copy all schedule metadata to the task
      const task = linkedTasks[0];
      copyCronMetadataToTask(cronFm, task);
      if (!updatedTaskIds.has(task.id)) {
        updatedTaskIds.add(task.id);
      }
    } else {
      // Multi-task cron: find the root task (no depends_on within the pipeline)
      const linkedTaskIds = new Set(linkedTasks.map((t) => t.id));
      let rootTask = linkedTasks.find((t) => {
        const deps = t.frontmatter.depends_on;
        if (!Array.isArray(deps) || deps.length === 0) return true;
        // Root = no dependencies that are within this cron's linked tasks
        return !deps.some((dep: string) => linkedTaskIds.has(dep));
      });

      // Fallback: if all tasks have internal deps (cycle), pick first alphabetically
      if (!rootTask) {
        rootTask = linkedTasks.sort((a, b) => a.id.localeCompare(b.id))[0];
      }

      // Copy schedule metadata only to root task
      copyCronMetadataToTask(cronFm, rootTask);
      if (!updatedTaskIds.has(rootTask.id)) {
        updatedTaskIds.add(rootTask.id);
      }

      // Remove cron_ids from non-root tasks (they don't get schedule metadata)
      for (const task of linkedTasks) {
        if (task.id !== rootTask.id) {
          removeCronIdFromTask(task, cronId);
          if (!updatedTaskIds.has(task.id)) {
            updatedTaskIds.add(task.id);
          }
        }
      }
    }

    // Delete the cron file
    if (!dryRun) {
      unlinkSync(cronPath);
    }
    result.cronFilesDeleted++;
  }

  // Write all modified tasks
  if (!dryRun) {
    for (const task of taskEntries) {
      if (task.modified) {
        const newFrontmatterStr = serializeFrontmatter(task.frontmatter);
        const newContent = `---\n${newFrontmatterStr}---\n\n${task.body}\n`;
        writeFileSync(task.filePath, newContent, "utf-8");
      }
    }
  }

  result.tasksUpdated += updatedTaskIds.size;
}

// =============================================================================
// Task Entry Helpers
// =============================================================================

interface TaskEntry {
  id: string;
  filePath: string;
  frontmatter: Record<string, unknown>;
  body: string;
  modified: boolean;
}

/**
 * Load all task entries from a task directory.
 */
function loadTaskEntries(taskDir: string): TaskEntry[] {
  if (!existsSync(taskDir)) {
    return [];
  }

  return readdirSync(taskDir)
    .filter((f) => f.endsWith(".md"))
    .map((f) => {
      const filePath = join(taskDir, f);
      const content = readFileSync(filePath, "utf-8");
      const { frontmatter, body } = parseFrontmatter(content);
      return {
        id: f.replace(/\.md$/, ""),
        filePath,
        frontmatter,
        body,
        modified: false,
      };
    });
}

/**
 * Copy schedule-related fields from a cron's frontmatter to a task.
 * Also removes cron_ids from the task.
 */
function copyCronMetadataToTask(
  cronFm: Record<string, unknown>,
  task: TaskEntry,
): void {
  for (const field of CRON_SCHEDULE_FIELDS) {
    if (cronFm[field] !== undefined) {
      task.frontmatter[field] = cronFm[field];
    }
  }

  // Remove cron_ids entirely
  delete task.frontmatter.cron_ids;
  task.modified = true;
}

/**
 * Remove a specific cron ID from a task's cron_ids array.
 * If the array becomes empty, delete the field entirely.
 */
function removeCronIdFromTask(task: TaskEntry, cronId: string): void {
  const cronIds = task.frontmatter.cron_ids;
  if (!Array.isArray(cronIds)) return;

  const filtered = cronIds.filter((id: string) => id !== cronId);
  if (filtered.length === 0) {
    delete task.frontmatter.cron_ids;
  } else {
    task.frontmatter.cron_ids = filtered;
  }
  task.modified = true;
}

// =============================================================================
// CLI Entry Point
// =============================================================================

if (import.meta.main) {
  const args = process.argv.slice(2);
  const dryRun = args.includes("--dry-run");

  // Resolve brain dir from config or environment
  const brainDir = process.env.BRAIN_DIR ?? join(homedir(), ".brain");

  console.log(`\nMigrating crons to tasks...`);
  console.log(`Brain dir: ${brainDir}`);
  if (dryRun) {
    console.log(`Mode: DRY RUN (no changes will be made)\n`);
  } else {
    console.log(`Mode: LIVE (files will be modified)\n`);
  }

  migrateCronsToTasks(brainDir, { dryRun })
    .then((result) => {
      console.log(`\n--- Migration Summary ---`);
      console.log(`Crons processed:   ${result.cronsProcessed}`);
      console.log(`Tasks updated:     ${result.tasksUpdated}`);
      console.log(`Cron files deleted: ${result.cronFilesDeleted}`);

      if (result.warnings.length > 0) {
        console.log(`\nWarnings:`);
        for (const w of result.warnings) {
          console.log(`  - ${w}`);
        }
      }

      if (dryRun) {
        console.log(`\n(Dry run - no files were modified)`);
      }
    })
    .catch((err) => {
      console.error(`Migration failed:`, err);
      process.exit(1);
    });
}
