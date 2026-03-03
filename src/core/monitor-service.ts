/**
 * Monitor Service
 *
 * Server-side service for creating, finding, toggling, and deleting
 * monitor tasks from templates. Uses BrainService directly — no HTTP
 * round-trips.
 */

import { BrainService, getBrainService } from "./brain-service";
import {
  MONITOR_TEMPLATES,
  buildMonitorTitle,
  buildMonitorTag,
  parseMonitorTag,
  type MonitorScope,
} from "./monitor-templates";

// =============================================================================
// Types
// =============================================================================

export interface MonitorInfo {
  id: string;
  path: string;
  templateId: string;
  scope: MonitorScope;
  enabled: boolean;
  schedule: string;
  title: string;
}

export interface CreateMonitorResult {
  id: string;
  path: string;
  title: string;
}

// =============================================================================
// MonitorService
// =============================================================================

export class MonitorService {
  constructor(private brainService: BrainService) {}

  /**
   * Create a new monitor task from a template.
   *
   * @throws Error if template not found
   * @throws Error if monitor already exists for this template+scope
   */
  async create(
    templateId: string,
    scope: MonitorScope,
    options?: {
      schedule?: string;
      project?: string;
    },
  ): Promise<CreateMonitorResult> {
    const template = MONITOR_TEMPLATES[templateId];
    if (!template) {
      throw new Error(`Unknown monitor template: ${templateId}`);
    }

    // Check for existing monitor
    const existing = await this.find(templateId, scope);
    if (existing) {
      throw new MonitorConflictError(
        `Monitor already exists for template "${templateId}" with this scope`,
        existing.id,
        existing.path,
      );
    }

    const tag = buildMonitorTag(templateId, scope);
    const title = buildMonitorTitle(template, scope);

    // Determine project
    let project: string | undefined;
    if (scope.type === "project") {
      project = scope.project;
    } else if (scope.type === "feature") {
      project = scope.project;
    } else {
      project = options?.project;
    }

    const result = await this.brainService.save({
      type: "task",
      title,
      content: `## Monitor Task\n\nTemplate: ${template.label}\nScope: ${describeScopeLabel(scope)}\n\nThis task was created from a monitor template. It runs on schedule with complete_on_idle.`,
      schedule: options?.schedule ?? template.defaultSchedule,
      schedule_enabled: true,
      direct_prompt: template.buildPrompt(scope),
      complete_on_idle: true,
      execution_mode: "current_branch",
      tags: [...template.tags, tag],
      feature_id:
        scope.type === "feature" ? scope.feature_id : undefined,
      project,
      status: "active",
    });

    return { id: result.id, path: result.path, title };
  }

  /**
   * Create a one-shot review task for a feature, triggered by dependency completion.
   *
   * Unlike create(), this method:
   * - Does NOT set schedule/schedule_enabled (one-shot, not recurring)
   * - Auto-computes depends_on from all other tasks in the feature
   * - Sets generated metadata for dedup and tracking
   * - Status is "pending" — becomes "ready" when all deps complete (via runner)
   *
   * @throws Error if template not found
   * @throws Error if template doesn't support feature scope
   * @throws MonitorConflictError if review task already exists for this feature
   */
  async createForFeature(
    templateId: string,
    scope: { type: "feature"; feature_id: string; project: string },
    project: string,
  ): Promise<CreateMonitorResult> {
    const template = MONITOR_TEMPLATES[templateId];
    if (!template) {
      throw new Error(`Unknown monitor template: ${templateId}`);
    }

    // Check for existing review task (dedup)
    const existing = await this.find(templateId, scope);
    if (existing) {
      throw new MonitorConflictError(
        `Monitor already exists for template "${templateId}" with feature "${scope.feature_id}"`,
        existing.id,
        existing.path,
      );
    }

    // Build the prompt (will throw if template doesn't support feature scope)
    const prompt = template.buildPrompt(scope);

    const tag = buildMonitorTag(templateId, scope);
    const title = buildMonitorTitle(template, scope);

    // Fetch all tasks in the feature to compute depends_on
    const featureTasks = await this.brainService.list({
      type: "task",
      feature_id: scope.feature_id,
      limit: 200,
    });

    // Collect paths as depends_on, excluding generated tasks (other monitors/reviews)
    const dependsOn = featureTasks.entries
      .filter((entry) => !entry.generated)
      .map((entry) => entry.path);

    const result = await this.brainService.save({
      type: "task",
      title,
      content: `## Feature Review Task\n\nTemplate: ${template.label}\nFeature: ${scope.feature_id}\nProject: ${project}\n\nThis task was created as a one-shot feature review. It becomes ready when all dependency tasks complete.`,
      direct_prompt: prompt,
      complete_on_idle: true,
      execution_mode: "current_branch",
      status: "pending",
      feature_id: scope.feature_id,
      depends_on: dependsOn,
      generated: true,
      generated_kind: "feature_review",
      generated_key: `feature-review:${scope.feature_id}`,
      generated_by: "feature-completion-hook",
      tags: [...template.tags, tag],
      project,
    });

    return { id: result.id, path: result.path, title };
  }

  /**
   * Find existing monitor for a template+scope combo (by tag lookup).
   */
  async find(
    templateId: string,
    scope: MonitorScope,
  ): Promise<{ id: string; path: string; enabled: boolean; schedule: string } | null> {
    const tag = buildMonitorTag(templateId, scope);
    const result = await this.brainService.list({
      type: "task",
      tags: [tag],
      limit: 1,
    });

    if (result.entries.length === 0) return null;

    const entry = result.entries[0];
    return {
      id: entry.id,
      path: entry.path,
      enabled: entry.schedule_enabled !== false,
      schedule: entry.schedule ?? "",
    };
  }

  /**
   * List all active monitors, optionally filtered.
   */
  async list(filter?: {
    project?: string;
    feature_id?: string;
    templateId?: string;
  }): Promise<MonitorInfo[]> {
    // Use "monitor" tag to narrow zk search (all monitor tags start with "monitor:")
    const result = await this.brainService.list({
      type: "task",
      tags: ["monitor"],
      limit: 200,
    });

    const monitors: MonitorInfo[] = [];

    for (const entry of result.entries) {
      // Find the monitor tag in the entry's tags
      const monitorTag = (entry.tags || []).find((t) =>
        t.startsWith("monitor:"),
      );
      if (!monitorTag) continue;

      const parsed = parseMonitorTag(monitorTag);
      if (!parsed) continue;

      // Apply filters
      if (filter?.templateId && parsed.templateId !== filter.templateId)
        continue;
      if (filter?.project) {
        if (parsed.scope.type === "all") continue;
        if (
          parsed.scope.type === "project" &&
          parsed.scope.project !== filter.project
        )
          continue;
        if (
          parsed.scope.type === "feature" &&
          parsed.scope.project !== filter.project
        )
          continue;
      }
      if (filter?.feature_id) {
        if (parsed.scope.type !== "feature") continue;
        if (parsed.scope.feature_id !== filter.feature_id) continue;
      }

      monitors.push({
        id: entry.id,
        path: entry.path,
        templateId: parsed.templateId,
        scope: parsed.scope,
        enabled: entry.schedule_enabled !== false,
        schedule: entry.schedule ?? "",
        title: entry.title,
      });
    }

    return monitors;
  }

  /**
   * Toggle schedule_enabled on an existing monitor.
   */
  async toggle(taskId: string, enabled: boolean): Promise<{ path: string }> {
    // Resolve 8-char ID to path
    const entry = await this.brainService.recall(taskId);
    await this.brainService.update(entry.path, {
      schedule_enabled: enabled,
    });
    return { path: entry.path };
  }

  /**
   * Delete a monitor task.
   */
  async delete(taskId: string): Promise<{ path: string }> {
    // Resolve 8-char ID to path
    const entry = await this.brainService.recall(taskId);
    await this.brainService.delete(entry.path);
    return { path: entry.path };
  }
}

// =============================================================================
// Error Types
// =============================================================================

export class MonitorConflictError extends Error {
  constructor(
    message: string,
    public existingId: string,
    public existingPath: string,
  ) {
    super(message);
    this.name = "MonitorConflictError";
  }
}

// =============================================================================
// Singleton
// =============================================================================

let monitorServiceInstance: MonitorService | null = null;

export function getMonitorService(): MonitorService {
  if (!monitorServiceInstance) {
    monitorServiceInstance = new MonitorService(getBrainService());
  }
  return monitorServiceInstance;
}

// =============================================================================
// Helpers
// =============================================================================

function describeScopeLabel(scope: MonitorScope): string {
  if (scope.type === "all") return "all projects";
  if (scope.type === "project") return `project: ${scope.project}`;
  return `feature: ${scope.feature_id} (project: ${scope.project})`;
}
