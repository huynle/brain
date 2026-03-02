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

For each blocked task found, follow these steps in order:

### Step 1: Read the Task

Call \`brain_task_get({ taskId: "<id>" })\` to get the full task content, status, and any appended notes. Pay attention to any \`blocked\` notes or error context already recorded.

### Step 2: Check Session History

Use session tools to find error context from the agent that was working on this task:

- \`session_list()\` — list recent sessions
- \`session_search({ query: "<taskId>" })\` — find sessions related to this task
- \`session_read({ sessionId: "<id>", lastN: 20 })\` — read the last messages for error context

Look for error messages, stack traces, tool failures, or the agent's last actions before the block occurred.

### Step 3: Classify the Block

Based on the task content and session history, classify into exactly one category:

| Classification | Indicators |
|---|---|
| **Worktree setup failure** | Task never started, no session history, worktree errors in runner logs |
| **Idle detection timeout** | Session shows agent went idle, possibly waiting for user input or stuck in a loop |
| **Process crash** | Session ends abruptly, exit codes in runner logs, no graceful shutdown |
| **Agent self-block** | Task has a \`blocked\` note from \`brain_update\` — the agent deliberately blocked itself with a reason |
| **Dependency block** | Task has \`depends_on\` entries that are themselves blocked or incomplete |

### Step 4: Attempt Resolution

Apply the resolution strategy matching the classification:

**Worktree setup failure:**
- Reset task to \`pending\` via \`brain_update({ path: "<task-path>", status: "pending" })\`
- The runner will automatically retry on the next poll cycle
- Append context: what failed and that you reset it

**Idle detection timeout (live process still running):**
- Use \`oc_discover()\` to find running OpenCode instances
- Match by session ID or working directory to find the stuck instance
- If found, use \`oc_status({ port })\` to confirm it's alive
- Send a nudge via \`oc_send({ port, prompt: "You appear to be idle. The task <taskId> is still incomplete. Please continue or explain what you're blocked on." })\`
- Do NOT reset the task — let the live agent continue

**Idle detection timeout (process is dead):**
- Reset to \`pending\` via \`brain_update({ path: "<task-path>", status: "pending" })\`
- Append useful context from the session history so the next agent has more information
- Include the last error or action the previous agent took

**Process crash:**
- Reset to \`pending\` via \`brain_update({ path: "<task-path>", status: "pending" })\`
- Append crash context: error messages, stack traces, exit code if available
- Note how far the previous agent got so the next attempt can continue

**Agent self-block:**
- Do **NOT** auto-reset. The agent blocked itself intentionally with a reason.
- Read the block reason carefully from the task notes.
- The runner's notification hooks already fired a desktop notification when the task was first blocked.
- Append a diagnostic note with your analysis, but leave the status as \`blocked\`.
- This requires human intervention.

**Dependency block:**
- Check each blocking dependency via \`brain_task_get({ taskId: "<depId>" })\`
- If the dependency is also blocked, recursively analyze it (depth limit: 3)
- If the dependency is \`pending\` or \`in_progress\`, no action needed — it will resolve naturally
- If the dependency is \`completed\` but the task is still blocked, reset the task to \`pending\` (stale block)
- Append your findings about the dependency chain

### Step 5: Log All Actions

After processing each task, append a run log to your own task:

\`\`\`
brain_update({
  path: "<your-own-task-path>",
  append: "## Run <ISO-timestamp>\\n\\n- Inspected <N> blocked tasks in ${scopeDesc}\\n- <taskId>: <classification> → <action taken>\\n- ..."
})
\`\`\`

## Available Tools

You have access to these tool groups:

**Brain tools** (task management):
- \`brain_tasks\` — list tasks with filters (project, status, feature_id)
- \`brain_task_get\` — get full task content by ID
- \`brain_update\` — update task status, append notes
- \`brain_recall\` — recall a brain entry by path
- \`brain_search\` — search brain entries by query

**Session tools** (read agent session history from \`~/.local/share/opencode/storage/\`):
- \`session_list\` — list recent sessions
- \`session_read\` — read messages from a session
- \`session_search\` — search sessions by keyword

**OpenCode control tools** (interact with live agent processes):
- \`oc_discover\` — find running OpenCode instances (ports, PIDs, workdirs)
- \`oc_status\` — check if an instance is alive and its current state (idle/busy)
- \`oc_send\` — send a prompt to a running instance

## Safety Rules

These rules are **non-negotiable**:

1. **NEVER change the status of \`draft\` tasks** — draft status is reserved for user orchestration and planning
2. **NEVER inspect or modify your own task** — this prevents infinite inspection loops
3. **NEVER force-unblock agent self-blocks** — if an agent called \`brain_update(status: "blocked")\` with a reason, respect that decision. It requires human review.
4. **Limit actions per run to 5** — process at most 5 blocked tasks per execution to prevent runaway changes
5. **Be conservative** — when in doubt about the cause or the right action, log your analysis but do NOT take action. A wrong unblock is worse than a delayed one.`;
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
