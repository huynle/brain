/**
 * Brain API Client
 *
 * HTTP client for task queries and status updates.
 * Includes timeout handling and health check caching.
 */

import type { RunnerConfig, ApiHealth, ClaimResult } from "./types";
import type {
  ResolvedTask,
  TaskListResponse,
  TaskNextResponse,
  EntryStatus,
  Priority,
} from "../core/types";
import type {
  FeatureStatus,
  FeatureClassification,
  FeatureTaskStats,
} from "../core/feature-service";

// =============================================================================
// Feature Types (API Response Subset)
// =============================================================================

/**
 * Feature data as returned by the API.
 * This is a subset of ComputedFeature - tasks are not included in list responses.
 */
export interface ApiFeature {
  id: string;
  priority: Priority;
  status: FeatureStatus;
  classification: FeatureClassification;
  task_stats: FeatureTaskStats;
  blocked_by_features: string[];
  waiting_on_features: string[];
}

export interface FeatureListResponse {
  features: ApiFeature[];
  count: number;
  stats?: {
    total: number;
    ready: number;
    waiting: number;
    blocked: number;
  };
}

export interface FeatureResponse {
  feature: ApiFeature;
}
import { getRunnerConfig, isDebugEnabled } from "./config";

// =============================================================================
// API Client Class
// =============================================================================

export class ApiClient {
  private config: RunnerConfig;
  private healthCache: { status: ApiHealth | null; timestamp: number } = {
    status: null,
    timestamp: 0,
  };
  private readonly healthCacheTtl = 10_000; // 10 seconds

  constructor(config?: RunnerConfig) {
    this.config = config ?? getRunnerConfig();
  }

  // ========================================
  // Health Check
  // ========================================

  async checkHealth(): Promise<ApiHealth> {
    const now = Date.now();

    // Return cached result if recent
    if (
      this.healthCache.status &&
      now - this.healthCache.timestamp < this.healthCacheTtl
    ) {
      return this.healthCache.status;
    }

    try {
      const response = await this.fetch("/health");
      const health = (await response.json()) as ApiHealth;

      this.healthCache = { status: health, timestamp: now };
      return health;
    } catch {
      const unhealthy: ApiHealth = {
        status: "unhealthy",
        zkAvailable: false,
        dbAvailable: false,
      };
      this.healthCache = { status: unhealthy, timestamp: now };
      return unhealthy;
    }
  }

  async isAvailable(): Promise<boolean> {
    const health = await this.checkHealth();
    return health.status !== "unhealthy";
  }

  // ========================================
  // Task Queries
  // ========================================

  async listProjects(): Promise<string[]> {
    const response = await this.fetch("/api/v1/tasks");

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as { projects: string[] };
    return data.projects || [];
  }

  async getReadyTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/ready`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as TaskListResponse;
    return data.tasks;
  }

  async getNextTask(projectId: string): Promise<ResolvedTask | null> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/next`);

    if (response.status === 404) {
      return null;
    }

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as TaskNextResponse;
    return data.task;
  }

  async getAllTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as { tasks: ResolvedTask[] };
    return data.tasks;
  }

  async getWaitingTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/waiting`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as TaskListResponse;
    return data.tasks;
  }

  async getBlockedTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/blocked`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as TaskListResponse;
    return data.tasks;
  }

  async getInProgressTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as TaskListResponse;
    // Filter to only in_progress tasks (API doesn't support status filtering)
    return data.tasks.filter(task => task.status === "in_progress");
  }

  /**
   * Get a task by its path.
   * Used for resuming orphaned in_progress tasks where getNextTask won't work.
   */
  async getTaskByPath(taskPath: string): Promise<ResolvedTask | null> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`);

    if (response.status === 404) {
      return null;
    }

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    // The entries API returns the full entry which includes task fields
    const entry = await response.json() as ResolvedTask;
    return entry;
  }

  // ========================================
  // Task Status Updates
  // ========================================

  async updateTaskStatus(taskPath: string, status: EntryStatus): Promise<void> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  async appendToTask(taskPath: string, content: string): Promise<void> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`, {
      method: "PATCH",
      body: JSON.stringify({ append: content }),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  /**
   * Get full entry details including content.
   * Used by external editor workflow to fetch content before editing.
   */
  async getEntry(taskPath: string): Promise<{ content: string; path: string; title: string }> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = await response.json() as { content: string; path: string; title: string };
    return data;
  }

  /**
   * Replace the full content of an entry.
   * Used by external editor workflow to save edited content back.
   */
  async updateEntryContent(taskPath: string, content: string): Promise<void> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`, {
      method: "PATCH",
      body: JSON.stringify({ content }),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  /**
   * Move a task to a different project.
   * This relocates the file in the brain docs folder.
   */
  async moveTask(
    taskPath: string,
    newProjectId: string
  ): Promise<{ oldPath: string; newPath: string }> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}/move`, {
      method: "POST",
      body: JSON.stringify({ project: newProjectId }),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    return (await response.json()) as { oldPath: string; newPath: string };
  }

  /**
   * Update metadata fields on an entry.
   * Supports batch updates of multiple fields at once.
   * Only provided fields are updated - undefined fields are not sent.
   */
  async updateEntryMetadata(
    taskPath: string,
    fields: {
      status?: EntryStatus;
      feature_id?: string;
      git_branch?: string;
      target_workdir?: string;
      priority?: Priority;
      tags?: string[];
      depends_on?: string[];
    }
  ): Promise<void> {
    const encodedPath = encodeURIComponent(taskPath);

    // Build payload with only defined fields
    const payload: Record<string, unknown> = {};
    if (fields.status !== undefined) payload.status = fields.status;
    if (fields.feature_id !== undefined) payload.feature_id = fields.feature_id;
    if (fields.git_branch !== undefined) payload.git_branch = fields.git_branch;
    if (fields.target_workdir !== undefined) payload.target_workdir = fields.target_workdir;
    if (fields.priority !== undefined) payload.priority = fields.priority;
    if (fields.tags !== undefined) payload.tags = fields.tags;
    if (fields.depends_on !== undefined) payload.depends_on = fields.depends_on;

    const response = await this.fetch(`/api/v1/entries/${encodedPath}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  // ========================================
  // Task Claiming
  // ========================================

  async claimTask(
    projectId: string,
    taskId: string,
    runnerId: string
  ): Promise<ClaimResult> {
    const response = await this.fetch(
      `/api/v1/tasks/${projectId}/${taskId}/claim`,
      {
        method: "POST",
        body: JSON.stringify({ runnerId }),
      }
    );

    if (response.status === 409) {
      // Task already claimed
      const data = (await response.json()) as { claimedBy: string };
      return {
        success: false,
        taskId,
        claimedBy: data.claimedBy,
        message: "Task already claimed",
      };
    }

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    return { success: true, taskId };
  }

  async releaseTask(projectId: string, taskId: string): Promise<void> {
    const response = await this.fetch(
      `/api/v1/tasks/${projectId}/${taskId}/release`,
      { method: "POST" }
    );

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  // ========================================
  // Feature Queries
  // ========================================

  /**
   * Get all computed features for a project.
   * Features aggregate tasks with the same feature_id.
   */
  async getFeatures(projectId: string): Promise<FeatureListResponse> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/features`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    return (await response.json()) as FeatureListResponse;
  }

  /**
   * Get a single feature by ID with its task information.
   * Returns null if feature not found.
   */
  async getFeature(projectId: string, featureId: string): Promise<ApiFeature | null> {
    const response = await this.fetch(
      `/api/v1/tasks/${projectId}/features/${encodeURIComponent(featureId)}`
    );

    if (response.status === 404) {
      return null;
    }

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as FeatureResponse;
    return data.feature;
  }

  /**
   * Get features that are ready for execution.
   * Ready features have all dependencies completed and are not yet complete themselves.
   */
  async getReadyFeatures(projectId: string): Promise<ApiFeature[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/features/ready`);

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    const data = (await response.json()) as FeatureListResponse;
    return data.features;
  }

  // ========================================
  // Internal Helpers
  // ========================================

  private async fetch(
    path: string,
    options: RequestInit = {}
  ): Promise<Response> {
    const url = `${this.config.brainApiUrl}${path}`;

    const controller = new AbortController();
    const timeout = setTimeout(
      () => controller.abort(),
      this.config.apiTimeout
    );

    try {
      if (isDebugEnabled()) {
        console.log(`[API] ${options.method ?? "GET"} ${path}`);
      }

      const response = await fetch(url, {
        ...options,
        signal: controller.signal,
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          ...options.headers,
        },
      });

      return response;
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        throw new ApiError(
          408,
          `Request timeout after ${this.config.apiTimeout}ms`
        );
      }
      throw error;
    } finally {
      clearTimeout(timeout);
    }
  }
}

// =============================================================================
// Error Class
// =============================================================================

export class ApiError extends Error {
  constructor(
    public readonly statusCode: number,
    message: string
  ) {
    super(`API Error (${statusCode}): ${message}`);
    this.name = "ApiError";
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let apiClientInstance: ApiClient | null = null;

export function getApiClient(): ApiClient {
  if (!apiClientInstance) {
    apiClientInstance = new ApiClient();
  }
  return apiClientInstance;
}

/**
 * Reset the API client singleton (useful for testing).
 */
export function resetApiClient(): void {
  apiClientInstance = null;
}
