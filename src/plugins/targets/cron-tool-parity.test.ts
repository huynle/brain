import { describe, expect, test } from "bun:test";
import { readFileSync } from "fs";
import { join } from "path";

const CRON_TOOL_NAMES = [
  "brain_cron_list",
  "brain_cron_get",
  "brain_cron_create",
  "brain_cron_update",
  "brain_cron_delete",
  "brain_cron_trigger",
  "brain_cron_runs",
  "brain_cron_linked_tasks",
  "brain_cron_linked_task_add",
  "brain_cron_linked_task_remove",
  "brain_cron_linked_tasks_set",
] as const;

type CronToolName = (typeof CRON_TOOL_NAMES)[number];

type CronToolContract = {
  description: string;
  method: "GET" | "POST" | "PATCH" | "DELETE";
  pathFragment: string;
  operation: string;
};

const CRON_TOOL_CONTRACT: Record<CronToolName, CronToolContract> = {
  brain_cron_list: {
    description: "List cron entries for a project.",
    method: "GET",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons",
    operation: "list",
  },
  brain_cron_get: {
    description: "Get a cron entry with pipeline tasks.",
    method: "GET",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "get",
  },
  brain_cron_create: {
    description: "Create a cron entry in a project.",
    method: "POST",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons",
    operation: "create",
  },
  brain_cron_update: {
    description: "Update a cron entry in a project.",
    method: "PATCH",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "update",
  },
  brain_cron_delete: {
    description: "Delete a cron entry from a project.",
    method: "DELETE",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "delete",
  },
  brain_cron_trigger: {
    description: "Manually trigger a cron run.",
    method: "POST",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "trigger",
  },
  brain_cron_runs: {
    description: "Get cron run history.",
    method: "GET",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "runs",
  },
  brain_cron_linked_tasks: {
    description: "List tasks linked to a cron.",
    method: "GET",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "linked_tasks",
  },
  brain_cron_linked_task_add: {
    description: "Link a task to a cron.",
    method: "POST",
    pathFragment: "/linked-tasks/${encodeURIComponent(args.taskId",
    operation: "linked_task_add",
  },
  brain_cron_linked_task_remove: {
    description: "Unlink a task from a cron.",
    method: "DELETE",
    pathFragment: "/linked-tasks/${encodeURIComponent(args.taskId",
    operation: "linked_task_remove",
  },
  brain_cron_linked_tasks_set: {
    description: "Replace all linked tasks for a cron.",
    method: "PATCH",
    pathFragment: "/crons/${encodeURIComponent(proj)}/crons/${encodeURIComponent(args.cronId",
    operation: "linked_tasks_set",
  },
};

function countOccurrences(content: string, value: string): number {
  return content.split(value).length - 1;
}

function extractBlock(source: string, startMarker: string, nextMarker: string): string {
  const start = source.indexOf(startMarker);
  expect(start).toBeGreaterThanOrEqual(0);
  const end = source.indexOf(nextMarker, start + startMarker.length);
  if (end === -1) {
    return source.slice(start);
  }
  return source.slice(start, end);
}

function extractDescription(block: string): string {
  const match = block.match(/description:\s*"([^"]+)"/);
  expect(match).toBeDefined();
  return match![1];
}

describe("Cron tool parity across plugin targets", () => {
  const claudePath = join(import.meta.dir, "claude-code", "brain-mcp.ts");
  const opencodePath = join(import.meta.dir, "opencode", "brain.ts");

  const claudeSource = readFileSync(claudePath, "utf-8");
  const opencodeSource = readFileSync(opencodePath, "utf-8");

  test("includes all required cron tools in both plugin targets", () => {
    for (const toolName of CRON_TOOL_NAMES) {
      expect(claudeSource).toContain(`name: "${toolName}"`);
      expect(claudeSource).toContain(`case "${toolName}"`);

      expect(opencodeSource).toContain(`${toolName}: tool({`);
    }
  });

  test("keeps cron tool definition/handler counts aligned in each target", () => {
    const claudeDefinitions = countOccurrences(claudeSource, 'name: "brain_cron_');
    const claudeHandlers = countOccurrences(claudeSource, 'case "brain_cron_');
    const opencodeDefinitions = countOccurrences(opencodeSource, "brain_cron_");
    const opencodeToolBlocks = countOccurrences(opencodeSource, "brain_cron_") / 2;

    expect(claudeDefinitions).toBe(CRON_TOOL_NAMES.length);
    expect(claudeHandlers).toBe(CRON_TOOL_NAMES.length);

    expect(opencodeDefinitions).toBe(CRON_TOOL_NAMES.length * 2);
    expect(opencodeToolBlocks).toBe(CRON_TOOL_NAMES.length);
  });

  test("keeps cron tool contract (description + API operation) consistent", () => {
    for (let i = 0; i < CRON_TOOL_NAMES.length; i += 1) {
      const toolName = CRON_TOOL_NAMES[i];
      const nextTool = CRON_TOOL_NAMES[i + 1];
      const contract = CRON_TOOL_CONTRACT[toolName];

      const claudeDefinition = extractBlock(
        claudeSource,
        `name: "${toolName}"`,
        nextTool ? `name: "${nextTool}"` : 'name: "brain_backlinks"'
      );
      const opencodeDefinition = extractBlock(
        opencodeSource,
        `${toolName}: tool({`,
        nextTool ? `${nextTool}: tool({` : "brain_plan_sections: tool({"
      );
      const claudeHandler = extractBlock(
        claudeSource,
        `case "${toolName}": {`,
        nextTool ? `case "${nextTool}": {` : 'case "brain_backlinks": {'
      );

      expect(extractDescription(claudeDefinition)).toBe(contract.description);
      expect(extractDescription(opencodeDefinition)).toBe(contract.description);

      expect(claudeHandler).toContain(`"${contract.method}"`);
      expect(opencodeDefinition).toContain(`"${contract.method}"`);

      expect(claudeHandler).toContain(contract.pathFragment);
      expect(opencodeDefinition).toContain(contract.pathFragment);

      expect(claudeHandler).toContain(`formatCronResult("${contract.operation}"`);
      expect(opencodeDefinition).toContain(`formatCronResult("${contract.operation}"`);

      expect(claudeHandler).toContain(`formatCronError("${contract.operation}"`);
      expect(opencodeDefinition).toContain(`formatCronError("${contract.operation}"`);
    }
  });
});
