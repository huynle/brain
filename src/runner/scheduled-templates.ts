/**
 * Scheduled Task Templates
 *
 * Defines reusable templates for recurring monitoring tasks.
 * Templates generate `direct_prompt` content from a scope (all/project/feature)
 * and provide creation/toggle logic via the brain API.
 */

// =============================================================================
// Types
// =============================================================================

export type TemplateScope =
  | { type: "all" }
  | { type: "project"; project: string }
  | { type: "feature"; feature_id: string; project: string };

export interface ScheduledTaskTemplate {
  id: string;
  label: string;
  description: string;
  schedule: string;
  buildPrompt: (scope: TemplateScope) => string;
  complete_on_idle: true;
  execution_mode: "current_branch";
  tags: string[];
}

// =============================================================================
// Helpers
// =============================================================================

/**
 * Build a human-readable scope description.
 */
function describeScopeShort(scope: TemplateScope): string {
  if (scope.type === "all") return "all projects";
  if (scope.type === "project") return `project ${scope.project}`;
  return `feature ${scope.feature_id}`;
}

function describeScopeLong(scope: TemplateScope): string {
  if (scope.type === "all") return "all projects";
  if (scope.type === "project") return `project ${scope.project}`;
  return `feature ${scope.feature_id} in project ${scope.project}`;
}

// =============================================================================
// Template Registry
// =============================================================================

export const TEMPLATES: Record<string, ScheduledTaskTemplate> = {
  "blocked-inspector": {
    id: "blocked-inspector",
    label: "Blocked Task Inspector",
    description:
      "Periodically checks for blocked tasks and attempts to unblock them",
    schedule: "*/15 * * * *",
    buildPrompt: (scope: TemplateScope): string => {
      const scopeDesc = describeScopeLong(scope);
      return `Check for blocked tasks in ${scopeDesc} and attempt to unblock them.`;
    },
    complete_on_idle: true,
    execution_mode: "current_branch",
    tags: ["scheduled", "inspector", "monitoring"],
  },
};

export const TEMPLATE_LIST: ScheduledTaskTemplate[] = Object.values(TEMPLATES);

// =============================================================================
// Title & Tag Builders
// =============================================================================

/**
 * Generate a deterministic title for a scheduled task from template + scope.
 * e.g., "Blocked Task Inspector: feature auth-system"
 */
export function buildScheduledTaskTitle(
  template: ScheduledTaskTemplate,
  scope: TemplateScope,
): string {
  return `${template.label}: ${describeScopeShort(scope)}`;
}

/**
 * Generate the tag used to identify template-created tasks.
 * e.g., "monitor:blocked-inspector:feature:auth-system"
 */
export function buildScheduledTaskTag(
  templateId: string,
  scope: TemplateScope,
): string {
  if (scope.type === "all") return `monitor:${templateId}:all`;
  if (scope.type === "project")
    return `monitor:${templateId}:project:${scope.project}`;
  return `monitor:${templateId}:feature:${scope.feature_id}`;
}

// =============================================================================
// API Functions
// =============================================================================

/**
 * Create a new scheduled task from a template via brain API.
 */
export async function createScheduledTask(
  template: ScheduledTaskTemplate,
  scope: TemplateScope,
  apiBase: string,
): Promise<{ id: string; path: string }> {
  const tag = buildScheduledTaskTag(template.id, scope);
  const body: Record<string, unknown> = {
    type: "task",
    title: buildScheduledTaskTitle(template, scope),
    content:
      "## Scheduled monitoring task\n\nCreated from template: " +
      template.label,
    schedule: template.schedule,
    schedule_enabled: true,
    direct_prompt: template.buildPrompt(scope),
    complete_on_idle: template.complete_on_idle,
    execution_mode: template.execution_mode,
    tags: [...template.tags, tag],
  };

  if (scope.type === "project") {
    body.project = scope.project;
  } else if (scope.type === "feature") {
    body.project = scope.project;
    body.feature_id = scope.feature_id;
  }

  const response = await fetch(`${apiBase}/api/v1/entries`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    throw new Error(
      `Failed to create scheduled task: ${response.status} ${response.statusText}`,
    );
  }

  const result = (await response.json()) as { id: string; path: string };
  return { id: result.id, path: result.path };
}

/**
 * Find existing scheduled task for a template+scope (by tag lookup).
 */
export async function findScheduledTask(
  templateId: string,
  scope: TemplateScope,
  apiBase: string,
): Promise<{ id: string; path: string; enabled: boolean } | null> {
  const tag = buildScheduledTaskTag(templateId, scope);
  const params = new URLSearchParams({ type: "task", tags: tag, limit: "1" });
  const response = await fetch(`${apiBase}/api/v1/entries?${params}`, {
    headers: { Accept: "application/json" },
  });

  if (!response.ok) {
    throw new Error(
      `Failed to search for scheduled task: ${response.status}`,
    );
  }

  const result = (await response.json()) as {
    entries: Array<{
      id: string;
      path: string;
      schedule_enabled?: boolean;
    }>;
  };
  const entries = result.entries || [];
  if (entries.length === 0) return null;

  const entry = entries[0];
  return {
    id: entry.id,
    path: entry.path,
    enabled: entry.schedule_enabled !== false,
  };
}

/**
 * Toggle schedule_enabled on an existing scheduled task.
 */
export async function toggleScheduledTask(
  taskPath: string,
  enabled: boolean,
  apiBase: string,
): Promise<void> {
  const encoded = encodeURIComponent(taskPath);
  const response = await fetch(`${apiBase}/api/v1/entries/${encoded}`, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({ schedule_enabled: enabled }),
  });

  if (!response.ok) {
    throw new Error(
      `Failed to toggle scheduled task: ${response.status}`,
    );
  }
}
