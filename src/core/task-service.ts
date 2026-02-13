/**
 * Task Service
 *
 * Handles task queries and dependency resolution for the do-work task queue.
 * Follows BrainService patterns for consistency.
 */

import { existsSync, readdirSync } from "fs";
import { join } from "path";
import { homedir } from "os";
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
        tags: note.tags || [],
        created: note.created || "",
        modified: note.modified,
        target_workdir: (note.metadata?.target_workdir as string) || null,
        workdir: (note.metadata?.workdir as string) || null,
        worktree: null, // Deprecated: now derived from git_branch via deriveWorktreePath()
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
      resolved_workdir: this.resolveWorkdir(task.workdir, task.git_branch),
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

  // ========================================
  // Dependency Validation
  // ========================================

  /**
   * Validate depends_on task IDs before save/update.
   * 
   * Validates that each dependency reference can be resolved to an existing task.
   * Supports:
   * - Task IDs (timestamp-slug format like "1770555889709-add-settings")
   * - Task titles (exact match)
   * - Cross-project syntax: "other-project:task-id"
   * 
   * Normalizes common mistakes:
   * - Strips .md extension
   * - Strips projects/xxx/task/ path prefix
   * 
   * @param depends_on - Array of dependency references
   * @param currentProjectId - The project where the task is being created
   * @returns Validation result with normalized IDs and any errors
   */
  async validateDependencies(
    depends_on: string[],
    currentProjectId: string
  ): Promise<DependencyValidationResult> {
    if (!depends_on || depends_on.length === 0) {
      return { valid: true, normalized: [], errors: [] };
    }

    const normalized: string[] = [];
    const errors: string[] = [];

    // Get all projects that have tasks
    const allProjects = this.listProjects();

    for (const ref of depends_on) {
      const { normalized: normalizedRef, projectId: refProjectId } = normalizeDependencyRef(ref);
      const targetProject = refProjectId || currentProjectId;

      // Check if the target project exists
      if (refProjectId && !allProjects.includes(refProjectId)) {
        errors.push(
          `Project '${refProjectId}' not found. Available projects: ${allProjects.join(", ")}`
        );
        continue;
      }

      try {
        // Get tasks from the target project
        const tasks = await this.getAllTasks(targetProject);
        
        // Try to resolve: first by ID, then by title
        const matchById = tasks.find((t) => t.id === normalizedRef);
        const matchByTitle = tasks.find((t) => t.title === normalizedRef);

        if (matchById) {
          // Resolved by ID
          if (refProjectId) {
            normalized.push(`${refProjectId}:${matchById.id}`);
          } else {
            normalized.push(matchById.id);
          }
        } else if (matchByTitle) {
          // Resolved by title - store the ID for consistency
          if (refProjectId) {
            normalized.push(`${refProjectId}:${matchByTitle.id}`);
          } else {
            normalized.push(matchByTitle.id);
          }
        } else {
          // Could not resolve
          const suggestions = tasks
            .filter((t) => 
              t.id.includes(normalizedRef) || 
              t.title.toLowerCase().includes(normalizedRef.toLowerCase())
            )
            .slice(0, 3)
            .map((t) => `'${t.id}' (${t.title})`);
          
          let errorMsg = `Task '${ref}' not found in project '${targetProject}'.`;
          errorMsg += ` Use format: 'task-id' or 'project:task-id'.`;
          if (suggestions.length > 0) {
            errorMsg += ` Did you mean: ${suggestions.join(", ")}?`;
          }
          errors.push(errorMsg);
        }
      } catch {
        errors.push(
          `Failed to validate dependency '${ref}' in project '${targetProject}'.`
        );
      }
    }

    return {
      valid: errors.length === 0,
      normalized,
      errors,
    };
  }

  // ========================================
  // Workdir Resolution
  // ========================================

  /**
   * Derive the standard worktree path from git_branch and workdir.
   * Uses convention: <workdir>/.worktrees/<sanitized-branch>/
   *
   * @param workdir - $HOME-relative path to main repo (e.g., "projects/brain-api")
   * @param gitBranch - Git branch name (e.g., "feature/auth")
   * @returns Full worktree path or null if inputs missing
   */
  deriveWorktreePath(workdir: string | null, gitBranch: string | null): string | null {
    if (!workdir || !gitBranch) return null;

    // Sanitize branch name for filesystem
    // feature/auth -> feature-auth
    // fix/bug-123 -> fix-bug-123
    const sanitizedBranch = gitBranch
      .replace(/\//g, "-")
      .replace(/[^a-zA-Z0-9-_]/g, "");

    const home = homedir();
    const mainRepoPath = `${home}/${workdir}`;
    const worktreePath = `${mainRepoPath}/.worktrees/${sanitizedBranch}`;

    return worktreePath;
  }

  /**
   * Resolve working directory for task execution.
   * First checks if a derived worktree exists, then falls back to workdir.
   *
   * @param workdir - $HOME-relative path to main repo
   * @param gitBranch - Git branch name (for worktree derivation)
   * @returns Resolved absolute path or null
   */
  resolveWorkdir(workdir: string | null, gitBranch: string | null): string | null {
    const home = homedir();

    // First, check if a worktree exists for this branch
    const worktreePath = this.deriveWorktreePath(workdir, gitBranch);
    if (worktreePath && existsSync(worktreePath)) {
      return worktreePath;
    }

    // Fall back to main workdir
    if (workdir) {
      const mainPath = `${home}/${workdir}`;
      if (existsSync(mainPath)) {
        return mainPath;
      }
      console.warn(`Workdir not found: ${mainPath}`);
    }

    return null;
  }
}

// =============================================================================
// Dependency Validation
// =============================================================================

export interface DependencyValidationResult {
  valid: boolean;
  normalized: string[];
  errors: string[];
}

/**
 * Normalize a dependency reference by stripping common mistakes:
 * - .md extension
 * - projects/xxx/task/ path prefix
 * - Extracts task ID from full paths
 * 
 * Also parses cross-project syntax: "project:task-id"
 * 
 * Supported formats:
 * - "l60p1j59" — plain task ID (same project)
 * - "projects/pwa/task/l60p1j59.md" — full path with .md
 * - "projects/pwa/task/l60p1j59" — full path without .md
 * - "pwa:l60p1j59" — colon syntax (cross-project)
 */
export function normalizeDependencyRef(ref: string): { normalized: string; projectId?: string } {
  let normalized = ref.trim();
  
  // Check for full path syntax: "projects/pwa/task/l60p1j59.md"
  // MUST come before colon check since paths contain slashes
  const pathMatch = normalized.match(/^projects\/([^/]+)\/task\/(.+?)(?:\.md)?$/);
  if (pathMatch) {
    const projectId = pathMatch[1];
    const taskRef = pathMatch[2];
    return { normalized: taskRef, projectId };
  }
  
  // Check for cross-project syntax: "project:task-id"
  const crossProjectMatch = normalized.match(/^([^:]+):(.+)$/);
  if (crossProjectMatch) {
    const projectId = crossProjectMatch[1];
    let taskRef = crossProjectMatch[2];
    
    // Normalize the task part
    taskRef = taskRef
      .replace(/\.md$/, "")  // Strip .md extension
      .replace(/^projects\/[^/]+\/task\//, "");  // Strip path prefix
    
    // Extract ID if it's a full filename (timestamp-slug format)
    const idMatch = taskRef.match(/^(\d{13}-[^/]+)$/);
    if (idMatch) {
      return { normalized: idMatch[1], projectId };
    }
    
    return { normalized: taskRef, projectId };
  }
  
  // Same-project reference - just normalize
  normalized = normalized
    .replace(/\.md$/, "")  // Strip .md extension
    .replace(/^projects\/[^/]+\/task\//, "");  // Strip path prefix
  
  // Extract ID if it's a full filename (timestamp-slug format)
  const idMatch = normalized.match(/^(\d{13}-[^/]+)$/);
  if (idMatch) {
    return { normalized: idMatch[1] };
  }
  
  return { normalized };
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
