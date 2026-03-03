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
// Prompt Builders
// =============================================================================

/**
 * Build the full direct_prompt for the blocked-inspector template.
 *
 * The prompt instructs an agent to triage blocked tasks, attempt resolution,
 * and log all actions. It varies based on the scope (all/project/feature).
 */
function buildBlockedInspectorPrompt(scope: TemplateScope): string {
  const scopeDesc = describeScopeLong(scope);

  // Build scope-specific discovery instructions
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

For each blocked task found, perform the following steps:

### 1. Read the Task

Call \`brain_task_get({ taskId: "<id>" })\` to understand what the task was trying to do, its dependencies, and any block reason.

### 2. Check Session History

Look for relevant session context:
- Use \`brain_recall\` or \`brain_search\` to find related entries
- Check if there are session notes or error logs appended to the task

### 3. Classify the Block

Determine which category the block falls into:

| Classification | Indicators |
|---|---|
| **Agent self-block** | Task has a block reason/note set by the agent via \`brain_update(status: "blocked", note: "...")\` |
| **Dependency block** | Task depends on other tasks that are blocked or incomplete |
| **Process crash** | Session ended unexpectedly with no block reason |
| **Idle detection timeout** | Agent went idle without completing work |
| **Worktree setup failure** | Task never started execution |

### 4. Attempt Resolution

Based on the classification:

- **Idle timeout / Process crash / Worktree failure:**
  Reset the task to pending so it can be retried:
  \`\`\`
  brain_update({
    path: "<task-path>",
    status: "pending",
    append: "## Inspector Reset\\n\\nReset from blocked to pending by Blocked Task Inspector.\\nPrevious block classification: <classification>\\nContext: <relevant error info>"
  })
  \`\`\`

- **Agent self-block:**
  Do **NOT** reset the status. The agent blocked itself for a reason. Instead, read and log the block reason. If the block reason indicates a question or need for human input, note it for the run summary.

- **Dependency block:**
  Check the blocking dependency:
  - If the dependency is itself blocked, note it (do not recurse — just log)
  - If the dependency is pending/active/in_progress, no action needed — the task will unblock naturally
  - If the dependency is completed but the task is still blocked, reset to pending

### 5. Log All Actions

After processing all tasks, append a summary to your own task:
\`\`\`
brain_update({
  path: "<your-own-task-path>",
  append: "## Run <ISO-timestamp>\\n\\n- Checked N blocked tasks in ${scopeDesc}\\n- Reset M tasks to pending\\n- N tasks with agent self-blocks (not reset)\\n- Details: ..."
})
\`\`\`

## Safety Rules

These rules are **mandatory** and must never be violated:

1. **NEVER change the status of \`draft\` tasks.** Draft status is reserved for user orchestration and must not be modified by automated processes.

2. **NEVER inspect or modify your own task.** Skip any task whose path matches your own task path to prevent infinite loops.

3. **NEVER force-unblock agent self-blocks.** If an agent set \`status: "blocked"\` with a reason, respect that decision. Log it but do not reset it.

4. **Limit actions per run to 5.** Process at most 5 unblock attempts (resets to pending) per execution. If more blocked tasks exist, log them but defer to the next run.

5. **Be conservative.** When uncertain about the cause of a block, log findings but take no action. It is better to leave a task blocked than to incorrectly reset it.

## Available Tools

You have access to these tools:
- \`brain_tasks\` — List tasks with filters (project, status, feature_id)
- \`brain_task_get\` — Get full task details by ID
- \`brain_update\` — Update task status and append notes
- \`brain_recall\` — Recall a brain entry by path
- \`brain_search\` — Search brain entries

## Output

End your run with a brief summary of actions taken. Do not produce verbose output — focus on what was checked, what was reset, and what needs human attention.`;
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
      return buildBlockedInspectorPrompt(scope);
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

// =============================================================================
// Monitor API Functions (for one-shot templates like feature-review)
// =============================================================================

/**
 * Find an existing monitor for a template+scope via the Monitor REST API.
 * Used by TUI to check if a feature-review task already exists.
 */
export async function findMonitorTask(
  templateId: string,
  scope: TemplateScope,
  apiBase: string,
): Promise<{ id: string; path: string; enabled: boolean } | null> {
  const params = new URLSearchParams({ template_id: templateId });
  if (scope.type === "project") {
    params.set("project", scope.project);
  } else if (scope.type === "feature") {
    params.set("project", scope.project);
    params.set("feature_id", scope.feature_id);
  }

  const response = await fetch(`${apiBase}/api/v1/monitors?${params}`, {
    headers: { Accept: "application/json" },
  });

  if (!response.ok) {
    throw new Error(`Failed to search for monitor task: ${response.status}`);
  }

  const result = (await response.json()) as {
    monitors: Array<{
      id: string;
      path: string;
      enabled: boolean;
    }>;
  };

  if (result.monitors.length === 0) return null;
  const monitor = result.monitors[0];
  return { id: monitor.id, path: monitor.path, enabled: monitor.enabled };
}

/**
 * Create a monitor task via the Monitor REST API.
 * For feature-review template, this routes to createForFeature() server-side.
 */
export async function createMonitorTask(
  templateId: string,
  scope: TemplateScope,
  apiBase: string,
): Promise<{ id: string; path: string }> {
  const response = await fetch(`${apiBase}/api/v1/monitors`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({ templateId, scope }),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(`Failed to create monitor task: ${response.status} ${text}`);
  }

  const result = (await response.json()) as { id: string; path: string };
  return { id: result.id, path: result.path };
}

/**
 * Delete a monitor task via the Monitor REST API.
 * Used for one-shot monitors like feature-review (toggle OFF = delete).
 */
export async function deleteMonitorTask(
  taskId: string,
  apiBase: string,
): Promise<void> {
  const response = await fetch(`${apiBase}/api/v1/monitors/${taskId}`, {
    method: "DELETE",
    headers: { Accept: "application/json" },
  });

  if (!response.ok) {
    throw new Error(`Failed to delete monitor task: ${response.status}`);
  }
}
