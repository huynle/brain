/**
 * Monitor Templates
 *
 * Defines reusable templates for recurring monitoring tasks.
 * Templates generate `direct_prompt` content from a scope (all/project/feature)
 * and are used by MonitorService to create/find/toggle/delete monitors.
 *
 * This is the server-side equivalent of the runner's scheduled-templates.ts,
 * using BrainService directly instead of HTTP round-trips.
 */

// =============================================================================
// Types
// =============================================================================

export type MonitorScope =
  | { type: "all" }
  | { type: "project"; project: string }
  | { type: "feature"; feature_id: string; project: string };

export interface MonitorTemplate {
  id: string;
  label: string;
  description: string;
  defaultSchedule: string;
  buildPrompt: (scope: MonitorScope) => string;
  tags: string[];
}

// =============================================================================
// Helpers
// =============================================================================

function describeScopeShort(scope: MonitorScope): string {
  if (scope.type === "all") return "all projects";
  if (scope.type === "project") return `project ${scope.project}`;
  return `feature ${scope.feature_id}`;
}

function describeScopeLong(scope: MonitorScope): string {
  if (scope.type === "all") return "all projects";
  if (scope.type === "project") return `project ${scope.project}`;
  return `feature ${scope.feature_id} in project ${scope.project}`;
}

// =============================================================================
// Prompt Builders
// =============================================================================

function buildBlockedInspectorPrompt(scope: MonitorScope): string {
  const scopeDesc = describeScopeLong(scope);

  let discoveryInstructions: string;
  if (scope.type === "all") {
    discoveryInstructions = `\
Discover all projects by calling \`brain_tasks()\` with no project filter.
Then iterate each project and call \`brain_tasks({ project: "<name>", status: "blocked" })\` for each.`;
  } else if (scope.type === "project") {
    discoveryInstructions = `\
Call \`brain_tasks({ project: "${scope.project}", status: "blocked" })\` to find all blocked tasks in this project.`;
  } else {
    discoveryInstructions = `\
Call \`brain_tasks({ project: "${scope.project}", feature_id: "${scope.feature_id}", status: "blocked" })\` to find all blocked tasks for this feature.`;
  }

  return `\
You are the **Blocked Task Inspector** — an automated agent that periodically checks for blocked tasks in ${scopeDesc} and attempts to unblock them.

## Scope

${discoveryInstructions}

## Workflow

For each blocked task found:

1. Read the task with \`brain_task_get({ taskId: "<id>" })\`
2. Check session history with \`brain_recall\` or \`brain_search\`
3. Classify the block (agent self-block, dependency block, process crash, idle timeout, worktree failure)
4. Attempt resolution based on classification
5. Log all actions to your own task

## Safety Rules

1. NEVER change the status of \`draft\` tasks
2. NEVER inspect or modify your own task
3. NEVER force-unblock agent self-blocks
4. Limit actions per run to 5
5. Be conservative — when uncertain, log but take no action`;
}

// =============================================================================
// Template Registry
// =============================================================================

export const MONITOR_TEMPLATES: Record<string, MonitorTemplate> = {
  "blocked-inspector": {
    id: "blocked-inspector",
    label: "Blocked Task Inspector",
    description:
      "Periodically checks for blocked tasks and attempts to unblock them",
    defaultSchedule: "*/15 * * * *",
    buildPrompt: buildBlockedInspectorPrompt,
    tags: ["scheduled", "inspector", "monitoring"],
  },
};

export const MONITOR_TEMPLATE_LIST: MonitorTemplate[] =
  Object.values(MONITOR_TEMPLATES);

// =============================================================================
// Title & Tag Builders
// =============================================================================

/**
 * Generate a deterministic title for a monitor task from template + scope.
 * e.g., "Monitor: Blocked Task Inspector (project: brain-api)"
 */
export function buildMonitorTitle(
  template: MonitorTemplate,
  scope: MonitorScope,
): string {
  const scopeLabel = describeScopeShort(scope);
  return `Monitor: ${template.label} (${scopeLabel})`;
}

/**
 * Generate a deterministic tag for lookup — this is how we find existing monitors.
 * e.g., "monitor:blocked-inspector:project:brain-api"
 */
export function buildMonitorTag(
  templateId: string,
  scope: MonitorScope,
): string {
  if (scope.type === "all") return `monitor:${templateId}:all`;
  if (scope.type === "project")
    return `monitor:${templateId}:project:${scope.project}`;
  return `monitor:${templateId}:feature:${scope.feature_id}:${scope.project}`;
}

/**
 * Parse a monitor tag back into templateId + scope.
 * Returns null if the tag doesn't match the expected format.
 */
export function parseMonitorTag(
  tag: string,
): { templateId: string; scope: MonitorScope } | null {
  const prefix = "monitor:";
  if (!tag.startsWith(prefix)) return null;

  const rest = tag.slice(prefix.length);

  // Try "templateId:all"
  const allMatch = rest.match(/^([^:]+):all$/);
  if (allMatch) {
    return { templateId: allMatch[1], scope: { type: "all" } };
  }

  // Try "templateId:project:projectName"
  const projectMatch = rest.match(/^([^:]+):project:(.+)$/);
  if (projectMatch) {
    return {
      templateId: projectMatch[1],
      scope: { type: "project", project: projectMatch[2] },
    };
  }

  // Try "templateId:feature:featureId:projectName"
  const featureMatch = rest.match(/^([^:]+):feature:([^:]+):(.+)$/);
  if (featureMatch) {
    return {
      templateId: featureMatch[1],
      scope: {
        type: "feature",
        feature_id: featureMatch[2],
        project: featureMatch[3],
      },
    };
  }

  return null;
}
