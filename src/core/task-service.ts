/**
 * Task Service
 *
 * Handles task queries and dependency resolution for the do-work task queue.
 * Follows BrainService patterns for consistency.
 */

import { existsSync } from "fs";
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

    // Ensure zk index is up to date
    await execZk(["index", "--quiet"]);

    // Query for task entries
    const result = await execZk([
      "list",
      "--format",
      "json",
      "--quiet",
      "--tag",
      "task",
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
        parent_id: (note.metadata?.parent_id as string) || null,
        created: note.created || "",
        workdir: (note.metadata?.workdir as string) || null,
        worktree: (note.metadata?.worktree as string) || null,
        git_remote: (note.metadata?.git_remote as string) || null,
        git_branch: (note.metadata?.git_branch as string) || null,
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
