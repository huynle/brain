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

function countOccurrences(content: string, value: string): number {
  return content.split(value).length - 1;
}

describe("Claude Code MCP cron tool parity", () => {
  const sourcePath = join(import.meta.dir, "brain-mcp.ts");
  const source = readFileSync(sourcePath, "utf-8");

  test("registers all cron tools in tool definitions", () => {
    for (const toolName of CRON_TOOL_NAMES) {
      expect(source).toContain(`name: "${toolName}"`);
    }
  });

  test("implements handlers for all cron tools", () => {
    for (const toolName of CRON_TOOL_NAMES) {
      expect(source).toContain(`case "${toolName}"`);
    }
  });

  test("keeps tool definition and handler counts aligned", () => {
    const toolDefinitions = countOccurrences(source, 'name: "brain_cron_');
    const handlers = countOccurrences(source, 'case "brain_cron_');

    expect(toolDefinitions).toBe(CRON_TOOL_NAMES.length);
    expect(handlers).toBe(CRON_TOOL_NAMES.length);
  });
});
