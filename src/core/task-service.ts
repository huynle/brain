/**
 * Task Service
 *
 * Handles task queries and dependency resolution for the brain-runner task queue.
 * Follows BrainService patterns for consistency.
 */

import { existsSync, readdirSync, readFileSync } from "fs";
import { join } from "path";
import { homedir } from "os";
import { getConfig } from "../config";
import type { BrainConfig, Task, ResolvedTask, DependencyResult, ZkNote } from "./types";
import { extractIdFromPath, parseFrontmatter } from "./note-utils";
import {
  resolveDependencies,
  getReadyTasks,
  getWaitingTasks,
  getBlockedTasks,
  getNextTask,
} from "./task-deps";
import { createStorageLayer, type StorageLayer, type NoteRow } from "./storage";

// =============================================================================
// TaskService Dependencies (optional)
// =============================================================================

export interface TaskServiceDeps {
  storage?: StorageLayer;
}

// =============================================================================
// NoteRow → Task Conversion
// =============================================================================

/**
 * Convert a StorageLayer NoteRow to the Task format used by the task runner.
 */
export function mapNoteRowToTask(noteRow: NoteRow): Task {
  // Parse the JSON metadata
  let metadata: Record<string, unknown> = {};
  try {
    metadata = noteRow.metadata ? JSON.parse(noteRow.metadata) : {};
  } catch {
    metadata = {};
  }

  // Extract projectId from file path (e.g., "projects/pwa/task/abc.md" -> "pwa")
  const pathMatch = noteRow.path.match(/^projects\/([^/]+)\//);
  const derivedProjectId = pathMatch ? pathMatch[1] : undefined;

  // Extract tags from metadata (stored as array in frontmatter)
  const tags: string[] = Array.isArray(metadata.tags)
    ? (metadata.tags as string[])
    : [];

  const sessions =
    (metadata.sessions as Task["sessions"] | undefined) || {};

  return {
    id: extractIdFromPath(noteRow.path),
    path: noteRow.path,
    title: noteRow.title,
    priority: (metadata.priority as Task["priority"]) || "medium",
    status: (metadata.status as Task["status"]) || "pending",
    depends_on: (metadata.depends_on as string[]) || [],
    tags,
    created: noteRow.created || "",
    modified: noteRow.modified || undefined,
    target_workdir: (metadata.target_workdir as string) || null,
    workdir: (metadata.workdir as string) || null,
    worktree: null, // Deprecated: now derived from git_branch via deriveWorktreePath()
    git_remote: (metadata.git_remote as string) || null,
    git_branch: (metadata.git_branch as string) || null,
    merge_target_branch: (metadata.merge_target_branch as string) || null,
    merge_policy:
      (metadata.merge_policy as Task["merge_policy"]) || "auto_merge",
    merge_strategy:
      (metadata.merge_strategy as Task["merge_strategy"]) || "squash",
    open_pr_before_merge:
      (metadata.open_pr_before_merge as boolean) || false,
    execution_mode:
      (metadata.execution_mode as Task["execution_mode"]) || "worktree",
    complete_on_idle:
      (metadata.complete_on_idle as boolean) ?? false,
    user_original_request:
      (metadata.user_original_request as string) || null,
    // Schedule fields
    schedule: (metadata.schedule as string) ?? undefined,
    schedule_enabled: (metadata.schedule_enabled as boolean) ?? undefined,
    next_run: (metadata.next_run as string) ?? undefined,
    max_runs: (metadata.max_runs as number) ?? undefined,
    starts_at: (metadata.starts_at as string) ?? undefined,
    expires_at: (metadata.expires_at as string) ?? undefined,
    runs: (metadata.runs as Task["runs"]) ?? undefined,
    // Feature grouping fields
    feature_id: (metadata.feature_id as string) ?? undefined,
    feature_priority:
      (metadata.feature_priority as Task["feature_priority"]) ?? undefined,
    feature_depends_on: (metadata.feature_depends_on as string[]) ?? undefined,
    // OpenCode execution options
    direct_prompt: (metadata.direct_prompt as string) || null,
    agent: (metadata.agent as string) || null,
    model: (metadata.model as string) || null,
    // Session traceability
    sessions,
    // Generated metadata
    generated: (metadata.generated as boolean) ?? undefined,
    generated_kind: (metadata.generated_kind as Task["generated_kind"]) ?? undefined,
    generated_key: (metadata.generated_key as string) ?? undefined,
    generated_by: (metadata.generated_by as string) ?? undefined,
    // Preserve raw frontmatter for downstream UI rendering
    frontmatter: metadata,
    // Derived from file path for self-correcting project identity
    projectId: derivedProjectId,
  };
}

// =============================================================================
// TaskService Class
// =============================================================================

export function mapZkNoteToTask(note: ZkNote): Task {
  // Extract projectId from file path (e.g., "projects/pwa/task/abc.md" -> "pwa")
  const pathMatch = note.path.match(/^projects\/([^/]+)\//);
  const derivedProjectId = pathMatch ? pathMatch[1] : undefined;
  const sessions =
    (note.metadata?.sessions as Task["sessions"] | undefined) || {};

  return {
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
    merge_target_branch: (note.metadata?.merge_target_branch as string) || null,
    merge_policy:
      (note.metadata?.merge_policy as Task["merge_policy"]) || "auto_merge",
    merge_strategy:
      (note.metadata?.merge_strategy as Task["merge_strategy"]) || "squash",
    open_pr_before_merge:
      (note.metadata?.open_pr_before_merge as boolean) || false,
    execution_mode:
      (note.metadata?.execution_mode as Task["execution_mode"]) || "worktree",

    complete_on_idle:
      (note.metadata?.complete_on_idle as boolean) ?? false,
    user_original_request:
      (note.metadata?.user_original_request as string) || null,
    // Schedule fields
    schedule: (note.metadata?.schedule as string) ?? undefined,
    schedule_enabled: (note.metadata?.schedule_enabled as boolean) ?? undefined,
    next_run: (note.metadata?.next_run as string) ?? undefined,
    max_runs: (note.metadata?.max_runs as number) ?? undefined,
    starts_at: (note.metadata?.starts_at as string) ?? undefined,
    expires_at: (note.metadata?.expires_at as string) ?? undefined,
    runs: (note.metadata?.runs as Task["runs"]) ?? undefined,
    // Feature grouping fields
    feature_id: (note.metadata?.feature_id as string) ?? undefined,
    feature_priority:
      (note.metadata?.feature_priority as Task["feature_priority"]) ?? undefined,
    feature_depends_on: (note.metadata?.feature_depends_on as string[]) ?? undefined,
    // OpenCode execution options
    direct_prompt: (note.metadata?.direct_prompt as string) || null,
    agent: (note.metadata?.agent as string) || null,
    model: (note.metadata?.model as string) || null,
    // Session traceability
    sessions,
    // Generated metadata
    generated: (note.metadata?.generated as boolean) ?? undefined,
    generated_kind: (note.metadata?.generated_kind as Task["generated_kind"]) ?? undefined,
    generated_key: (note.metadata?.generated_key as string) ?? undefined,
    generated_by: (note.metadata?.generated_by as string) ?? undefined,
    // Preserve raw frontmatter for downstream UI rendering
    frontmatter: note.metadata,
    // Derived from file path for self-correcting project identity
    projectId: derivedProjectId,
  };
}

export class TaskService {
  private config: BrainConfig;
  private projectId: string;
  private storageLayer: StorageLayer | null = null;

  constructor(config?: BrainConfig, projectId?: string, deps?: TaskServiceDeps) {
    const fullConfig = getConfig();
    this.config = config || fullConfig.brain;
    this.projectId = projectId || fullConfig.brain.defaultProject;

    if (deps?.storage) {
      this.storageLayer = deps.storage;
    }
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

    // Try StorageLayer first
    if (this.storageLayer) {
      const noteRows = this.storageLayer.listNotes({ path: projectDir });
      if (noteRows.length > 0) {
        return noteRows.map((row) => mapNoteRowToTask(row));
      }
    }

    // Fall back to file-based scan for files not yet indexed
    // (e.g., created directly on disk, or StorageLayer not available)
    return this.scanTasksFromDisk(projectDir);
  }

  /**
   * Scan task files directly from disk (fallback when StorageLayer has no data)
   */
  private scanTasksFromDisk(projectDir: string): Task[] {
    const fullDir = join(this.config.brainDir, projectDir);
    if (!existsSync(fullDir)) {
      return [];
    }

    const tasks: Task[] = [];
    const files = readdirSync(fullDir).filter((f) => f.endsWith(".md"));
    for (const file of files) {
      try {
        const filePath = join(fullDir, file);
        const content = readFileSync(filePath, "utf-8");
        const { frontmatter } = parseFrontmatter(content);
        const relativePath = `${projectDir}/${file}`;
        const note: ZkNote = {
          path: relativePath,
          title: (frontmatter.title as string) || file,
          tags: Array.isArray(frontmatter.tags) ? (frontmatter.tags as string[]) : [],
          metadata: frontmatter,
          rawContent: content,
        };
        tasks.push(mapZkNoteToTask(note));
      } catch {
        // Skip files that can't be parsed
      }
    }
    return tasks;
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
// Dependent Discovery
// =============================================================================

export interface DependentInfo {
  taskId: string;
  taskPath: string;
  projectId: string;
  depRef: string;
}

/**
 * Find all tasks that depend on a given task ID across all projects.
 * Uses dependency injection (tasksByProject map) for testability.
 * 
 * @param taskId - The task ID to find dependents for
 * @param sourceProjectId - The project the task currently belongs to
 * @param tasksByProject - Map of projectId -> Task[] for all projects
 * @returns Array of DependentInfo describing each dependent task
 */
export function findDependents(
  taskId: string,
  sourceProjectId: string,
  tasksByProject: Map<string, Task[]>
): DependentInfo[] {
  const results: DependentInfo[] = [];

  for (const [projectId, tasks] of tasksByProject) {
    for (const task of tasks) {
      if (!task.depends_on || task.depends_on.length === 0) continue;

      for (const depRef of task.depends_on) {
        const { normalized, projectId: refProjectId } = normalizeDependencyRef(depRef);

        // Determine which project this dep reference points to
        let targetProjectId: string;
        if (refProjectId) {
          // Explicit project reference (colon syntax or full path)
          targetProjectId = refProjectId;
        } else {
          // Bare ID — refers to the same project as the task containing the dep
          targetProjectId = projectId;
        }

        // Check if this dep points to our target task
        if (normalized === taskId && targetProjectId === sourceProjectId) {
          results.push({
            taskId: task.id,
            taskPath: task.path,
            projectId,
            depRef,
          });
        }
      }
    }
  }

  return results;
}

// =============================================================================
// Singleton Instance
// =============================================================================

let taskServiceInstance: TaskService | null = null;

export function getTaskService(): TaskService {
  if (!taskServiceInstance) {
    const config = getConfig();
    const storage = createStorageLayer(config.brain.dbPath);
    taskServiceInstance = new TaskService(undefined, undefined, { storage });
  }
  return taskServiceInstance;
}

export default TaskService;
