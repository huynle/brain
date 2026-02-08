/**
 * Task Service
 *
 * Handles task queries and dependency resolution for the do-work task queue.
 * Follows BrainService patterns for consistency.
 */

import { existsSync, readdirSync } from "fs";
import { join } from "path";
import { getConfig } from "../config";
import type { BrainConfig, Task, ResolvedTask, DependencyResult } from "./types";
import {
  execZk,
  parseZkJsonOutput,
  extractIdFromPath,
  isZkNotebookExists,
  isZkAvailable,
} from "./zk-client";
import {
  resolveDependencies,
  getReadyTasks,
  getWaitingTasks,
  getBlockedTasks,
  getNextTask,
} from "./task-deps";

// =============================================================================
// TaskService Class
// =============================================================================

export class TaskService {
  private config: BrainConfig;
  private projectId: string;

  constructor(config?: BrainConfig, projectId?: string) {
    const fullConfig = getConfig();
    this.config = config || fullConfig.brain;
    this.projectId = projectId || fullConfig.brain.defaultProject;
  }

  // ========================================
  // Project Discovery
  // ========================================

  /**
   * List all projects that have tasks.
   * Scans the projects/ directory for subdirectories containing a task/ folder.
   */
  listProjects(): string[] {
    const projectsDir = join(this.config.brainDir, "projects");
    
    if (!existsSync(projectsDir)) {
      return [];
    }

    try {
      const entries = readdirSync(projectsDir, { withFileTypes: true });
      return entries
        .filter((entry) => {
          if (!entry.isDirectory()) return false;
          // Check if project has a task/ subdirectory
          const taskDir = join(projectsDir, entry.name, "task");
          return existsSync(taskDir);
        })
        .map((entry) => entry.name)
        .sort();
    } catch {
      return [];
    }
  }

  // ========================================
  // Task Queries
  // ========================================

  /**
   * Get all tasks for a project (raw, before dependency resolution)
   */
  async getAllTasks(projectId: string): Promise<Task[]> {
    const projectDir = `projects/${projectId}/task`;

    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    if (!zkAvailable) {
      throw new Error(
        "zk CLI not available. Install from https://github.com/zk-org/zk"
      );
    }

    // NOTE: Removed zk index --quiet from here for performance.
    // zk index is called once at startup via BrainService.
    // Calling it on every task fetch added significant latency.

    // Query for task entries (directory path is sufficient, no tag filter needed)
    const result = await execZk([
      "list",
      "--format",
      "json",
      "--quiet",
      projectDir,
    ]);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return [];
    }

    const notes = parseZkJsonOutput(result.stdout);

    // Transform to Task interface
    return notes
      .filter((note) => note.path.includes(`projects/${projectId}/`))
      .map((note) => ({
        id: extractIdFromPath(note.path),
        path: note.path,
        title: note.title,
        priority: (note.metadata?.priority as Task["priority"]) || "medium",
        status: (note.metadata?.status as Task["status"]) || "pending",
        depends_on: (note.metadata?.depends_on as string[]) || [],
        created: note.created || "",
        modified: note.modified,
        target_workdir: (note.metadata?.target_workdir as string) || null,
        workdir: (note.metadata?.workdir as string) || null,
        worktree: (note.metadata?.worktree as string) || null,
        git_remote: (note.metadata?.git_remote as string) || null,
        git_branch: (note.metadata?.git_branch as string) || null,
        user_original_request:
          (note.metadata?.user_original_request as string) || null,
        // Feature grouping fields
        feature_id: (note.metadata?.feature_id as string) ?? undefined,
        feature_priority: (note.metadata?.feature_priority as Task["feature_priority"]) ?? undefined,
        feature_depends_on: (note.metadata?.feature_depends_on as string[]) ?? undefined,
      }));
  }

  /**
   * Get all tasks with dependency resolution
   */
  async getTasksWithDependencies(projectId: string): Promise<DependencyResult> {
    const tasks = await this.getAllTasks(projectId);
    const result = resolveDependencies(tasks);

    // Resolve workdirs for all tasks
    result.tasks = result.tasks.map((task) => ({
      ...task,
      resolved_workdir: this.resolveWorkdir(task.workdir, task.worktree),
    }));

    return result;
  }

  /**
   * Get ready tasks (pending with all dependencies satisfied)
   */
  async getReady(projectId: string): Promise<ResolvedTask[]> {
    const result = await this.getTasksWithDependencies(projectId);
    return getReadyTasks(result);
  }

  /**
   * Get waiting tasks (pending, waiting on incomplete dependencies)
   */
  async getWaiting(projectId: string): Promise<ResolvedTask[]> {
    const result = await this.getTasksWithDependencies(projectId);
    return getWaitingTasks(result);
  }

  /**
   * Get blocked tasks (blocked by blocked/cancelled dependencies or circular)
   */
  async getBlocked(projectId: string): Promise<ResolvedTask[]> {
    const result = await this.getTasksWithDependencies(projectId);
    return getBlockedTasks(result);
  }

  /**
   * Get the next task to execute (highest priority ready task)
   */
  async getNext(projectId: string): Promise<ResolvedTask | null> {
    const result = await this.getTasksWithDependencies(projectId);
    return getNextTask(result);
  }

  // ========================================
  // Feature-based Queries
  // ========================================

  /**
   * Get all tasks belonging to a specific feature
   * @param projectId - The project to search in
   * @param featureId - The feature ID to filter by
   * @returns Tasks with matching feature_id, sorted by priority
   */
  async getTasksByFeature(projectId: string, featureId: string): Promise<Task[]> {
    const allTasks = await this.getAllTasks(projectId);
    return allTasks
      .filter((task) => task.feature_id === featureId)
      .sort((a, b) => {
        // Sort by priority: high > medium > low
        const priorityOrder = { high: 0, medium: 1, low: 2 };
        return priorityOrder[a.priority] - priorityOrder[b.priority];
      });
  }

  /**
   * Get specific tasks by their IDs with full dependency resolution
   * @param projectId - The project to search in
   * @param taskIds - Array of task IDs to fetch
   * @returns Tasks matching the provided IDs with dependency info, plus list of not found IDs
   */
  async getTasksByIds(
    projectId: string,
    taskIds: string[]
  ): Promise<{
    tasks: ResolvedTask[];
    notFound: string[];
  }> {
    // Return empty result if no IDs provided
    if (!taskIds || taskIds.length === 0) {
      return { tasks: [], notFound: [] };
    }

    // Get all resolved tasks
    const result = await this.getTasksWithDependencies(projectId);

    // Normalize IDs for case-insensitive matching
    const normalizedIds = new Set(taskIds.map((id) => id.toLowerCase()));

    // Filter to matching tasks
    const matchedTasks = result.tasks.filter((task) =>
      normalizedIds.has(task.id.toLowerCase())
    );

    // Track which IDs were found
    const foundIds = new Set(matchedTasks.map((t) => t.id.toLowerCase()));

    // Identify not found IDs (preserving original case from input)
    const notFound = taskIds.filter((id) => !foundIds.has(id.toLowerCase()));

    return {
      tasks: matchedTasks,
      notFound,
    };
  }

  // ========================================
  // Workdir Resolution
  // ========================================

  /**
   * Resolve a $HOME-relative workdir to an absolute path
   * Tries worktree first (more specific), then workdir
   * Returns null if neither exists (caller decides fallback)
   */
  resolveWorkdir(workdir: string | null, worktree: string | null): string | null {
    const home = process.env.HOME || "";

    // Try worktree first (more specific)
    if (worktree) {
      const worktreePath = `${home}/${worktree}`;
      if (existsSync(worktreePath)) {
        return worktreePath;
      }
      // Log warning but continue to try workdir
      console.warn(`Worktree not found: ${worktreePath}, trying workdir`);
    }

    // Try main workdir
    if (workdir) {
      const workdirPath = `${home}/${workdir}`;
      if (existsSync(workdirPath)) {
        return workdirPath;
      }
      console.warn(`Workdir not found: ${workdirPath}`);
    }

    return null;
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let taskServiceInstance: TaskService | null = null;

export function getTaskService(): TaskService {
  if (!taskServiceInstance) {
    taskServiceInstance = new TaskService();
  }
  return taskServiceInstance;
}

export default TaskService;
