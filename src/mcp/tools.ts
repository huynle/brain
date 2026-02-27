/**
 * Brain MCP Tools
 *
 * Registers all brain tools on an McpServer instance.
 * Each tool calls getBrainService() directly (no HTTP round-trip).
 */

import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { getBrainService } from "../core/brain-service";

// =============================================================================
// Shared Enums
// =============================================================================

const ENTRY_TYPES = [
  "summary", "report", "walkthrough", "plan", "pattern", "learning",
  "idea", "scratch", "decision", "exploration", "execution", "task",
] as const;

const ENTRY_STATUSES = [
  "draft", "pending", "active", "in_progress", "blocked",
  "completed", "validated", "superseded", "archived",
] as const;

// =============================================================================
// Tool Registration
// =============================================================================

export function registerBrainTools(server: McpServer): void {

  // --------------------------------------------------------------------------
  // brain_save
  // --------------------------------------------------------------------------
  server.registerTool("brain_save", {
    description: `Save content to the brain for future reference. Use this to persist:
- summaries: Session summaries, key decisions made
- reports: Analysis reports, code reviews, investigations
- walkthroughs: Code explanations, architecture overviews
- plans: Implementation plans, designs, roadmaps
- patterns: Reusable patterns discovered (use global:true for cross-project)
- learnings: General learnings, best practices (use global:true for cross-project)
- ideas: Ideas for future exploration
- scratch: Temporary working notes
- decision: Architectural decisions, ADRs
- exploration: Investigation notes, research findings`,
    inputSchema: {
      type: z.enum(ENTRY_TYPES).describe("Type of content being saved"),
      title: z.string().describe("Short descriptive title for the entry"),
      content: z.string().describe("The content to save (markdown supported)"),
      tags: z.array(z.string()).optional().describe("Tags for categorization"),
      status: z.enum(ENTRY_STATUSES).optional().describe("Initial status"),
      priority: z.enum(["high", "medium", "low"]).optional().describe("Priority level"),
      global: z.boolean().optional().describe("Save to global brain (cross-project)"),
      project: z.string().optional().describe("Explicit project ID/name"),
      depends_on: z.array(z.string()).optional().describe("Task dependencies - list of task IDs or titles"),
      user_original_request: z.string().optional().describe("Verbatim user request for this task. HIGHLY RECOMMENDED for tasks."),
      relatedEntries: z.array(z.string()).optional().describe("Related brain entry paths or titles to link"),
      target_workdir: z.string().optional().describe("Explicit working directory override for task execution (absolute path)"),
      feature_id: z.string().optional().describe("Feature group ID for this task (e.g., 'auth-system', 'payment-flow')"),
      feature_priority: z.enum(["high", "medium", "low"]).optional().describe("Priority level for the feature group"),
      feature_depends_on: z.array(z.string()).optional().describe("Feature IDs this feature depends on"),
      git_branch: z.string().optional().describe("Git branch for task execution"),
      merge_target_branch: z.string().optional().describe("Branch to merge completed work into"),
      merge_policy: z.enum(["prompt_only", "auto_pr", "auto_merge"]).optional().describe("Merge behavior at completion (default: auto_merge)"),
      merge_strategy: z.enum(["squash", "merge", "rebase"]).optional().describe("Merge strategy for auto-merge (default: squash)"),
      remote_branch_policy: z.enum(["keep", "delete"]).optional().describe("Policy for remote branch after merge (default: delete)"),
      open_pr_before_merge: z.boolean().optional().describe("Open PR before merge when enabled (default: false)"),
      execution_mode: z.enum(["worktree", "current_branch"]).optional().describe("Task execution mode (default: worktree)"),
      checkout_enabled: z.boolean().optional().describe("Enable checkout/worktree flow for this task (default: true)"),
      complete_on_idle: z.boolean().optional().describe("Mark task as completed when agent becomes idle (default: false)"),
      direct_prompt: z.string().optional().describe("Direct prompt to execute, bypassing do-work skill workflow"),
      agent: z.string().optional().describe("Override agent for this task (e.g., 'explore', 'tdd-dev')"),
      model: z.string().optional().describe("Override model (format: 'provider/model-id')"),
      schedule: z.string().optional().describe("Cron schedule expression (e.g., '*/5 * * * *', '0 2 * * *')"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const response = await service.save(args);
      return {
        content: [{
          type: "text" as const,
          text: `Saved to brain\n\nPath: ${response.path}\nID: ${response.id}\nTitle: ${response.title}\nType: ${response.type}\nStatus: ${response.status}`,
        }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_recall
  // --------------------------------------------------------------------------
  server.registerTool("brain_recall", {
    description: "Retrieve a specific entry from the brain by path, ID, or title. Updates access statistics.",
    inputSchema: {
      path: z.string().optional().describe("Path or ID to the note"),
      title: z.string().optional().describe("Title to search for (exact match)"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const entry = await service.recall(args.path, args.title);
      const userRequest = entry.user_original_request ? `\nUser Original Request: ${entry.user_original_request}` : "";
      return {
        content: [{
          type: "text" as const,
          text: `## ${entry.title}\n\nPath: ${entry.path}\nType: ${entry.type}\nStatus: ${entry.status}\nTags: ${entry.tags?.join(", ") || "none"}${userRequest}\n\n---\n\n${entry.content}`,
        }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_search
  // --------------------------------------------------------------------------
  server.registerTool("brain_search", {
    description: "Search the brain using full-text search. Finds entries matching your query.",
    inputSchema: {
      query: z.string().describe("Search query"),
      type: z.enum(ENTRY_TYPES).optional().describe("Filter by entry type"),
      status: z.enum(ENTRY_STATUSES).optional().describe("Filter by status"),
      limit: z.number().optional().describe("Maximum results (default: 10)"),
      global: z.boolean().optional().describe("Search only global entries"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const response = await service.search(args);
      if (response.results.length === 0) {
        return {
          content: [{ type: "text" as const, text: `No entries found matching "${args.query}"` }],
        };
      }
      const lines = [`Found ${response.total} entries:\n`];
      for (const result of response.results) {
        lines.push(`- **${result.title}** (${result.path}) - ${result.type}`);
        if (result.content) lines.push(`  > ${result.content.slice(0, 150)}...`);
      }
      return {
        content: [{ type: "text" as const, text: lines.join("\n") }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_list
  // --------------------------------------------------------------------------
  server.registerTool("brain_list", {
    description: "List entries in the brain with optional filtering by type, status, and filename.",
    inputSchema: {
      type: z.enum(ENTRY_TYPES).optional().describe("Filter by entry type"),
      status: z.enum(ENTRY_STATUSES).optional().describe("Filter by status"),
      limit: z.number().optional().describe("Maximum entries to return (default: 20)"),
      global: z.boolean().optional().describe("List only global entries"),
      sortBy: z.enum(["created", "modified", "priority"]).optional().describe("Sort order"),
      filename: z.string().optional().describe("Filter by filename pattern (supports wildcards)"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const response = await service.list(args);
      if (response.entries.length === 0) {
        return {
          content: [{ type: "text" as const, text: "No entries found" }],
        };
      }
      const lines = [`Found ${response.total} entries:\n`];
      for (const entry of response.entries) {
        lines.push(`- **${entry.title}** (${entry.path}) - ${entry.type} | ${entry.status}`);
      }
      return {
        content: [{ type: "text" as const, text: lines.join("\n") }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_inject
  // --------------------------------------------------------------------------
  server.registerTool("brain_inject", {
    description: "Search the brain and return relevant context. Use this to recall knowledge before starting a task.",
    inputSchema: {
      query: z.string().describe("What context are you looking for?"),
      maxEntries: z.number().optional().describe("Maximum entries to include (default: 5)"),
      type: z.enum(ENTRY_TYPES).optional().describe("Filter by entry type"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const response = await service.inject(args);
      if (!response.context || response.entries.length === 0) {
        return {
          content: [{ type: "text" as const, text: `No relevant context found for "${args.query}"` }],
        };
      }
      return {
        content: [{ type: "text" as const, text: response.context }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_update
  // --------------------------------------------------------------------------
  server.registerTool("brain_update", {
    description: "Update an existing brain entry's status, title, dependencies, or append content.",
    inputSchema: {
      path: z.string().describe("Path to the entry to update"),
      status: z.enum(ENTRY_STATUSES).optional().describe("New status"),
      title: z.string().optional().describe("New title"),
      append: z.string().optional().describe("Content to append"),
      note: z.string().optional().describe("Short note to add"),
      depends_on: z.array(z.string()).optional().describe("Task dependencies - list of task IDs or titles"),
      tags: z.array(z.string()).optional().describe("Replace tags array (overwrites existing tags)"),
      priority: z.enum(["high", "medium", "low"]).optional().describe("Update task priority"),
      target_workdir: z.string().optional().describe("Update task execution directory (absolute path)"),
      git_branch: z.string().optional().describe("Git branch to execute this task on"),
      merge_target_branch: z.string().optional().describe("Branch to merge completed work into"),
      merge_policy: z.enum(["prompt_only", "auto_pr", "auto_merge"]).optional().describe("Merge behavior at completion (default: auto_merge)"),
      merge_strategy: z.enum(["squash", "merge", "rebase"]).optional().describe("Merge strategy for auto-merge (default: squash)"),
      remote_branch_policy: z.enum(["keep", "delete"]).optional().describe("Policy for remote branch after merge (default: delete)"),
      open_pr_before_merge: z.boolean().optional().describe("Open PR before merge when enabled (default: false)"),
      execution_mode: z.enum(["worktree", "current_branch"]).optional().describe("Task execution mode (default: worktree)"),
      checkout_enabled: z.boolean().optional().describe("Enable checkout/worktree flow for this task (default: true)"),
      complete_on_idle: z.boolean().optional().describe("Mark task as completed when agent becomes idle (default: false)"),
      schedule: z.string().optional().describe("Cron schedule expression (e.g., '*/5 * * * *')"),
      feature_id: z.string().optional().describe("Feature group identifier (e.g., 'auth-system', 'payment-flow')"),
      feature_priority: z.enum(["high", "medium", "low"]).optional().describe("Priority for this feature group"),
      feature_depends_on: z.array(z.string()).optional().describe("Feature IDs this feature depends on"),
      direct_prompt: z.string().optional().describe("Direct prompt to execute, bypassing do-work skill workflow"),
      agent: z.string().optional().describe("Override agent for this task (e.g., 'explore', 'tdd-dev')"),
      model: z.string().optional().describe("Override model (format: 'provider/model-id')"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const { path, ...updateFields } = args;
      const entry = await service.update(path, updateFields);
      return {
        content: [{
          type: "text" as const,
          text: `Updated: ${entry.path}\nStatus: ${entry.status}\nTitle: ${entry.title}`,
        }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_stats
  // --------------------------------------------------------------------------
  server.registerTool("brain_stats", {
    description: "Get statistics about the brain storage.",
    inputSchema: {
      global: z.boolean().optional().describe("Show only global entries stats"),
    },
  }, async (args) => {
    try {
      const service = getBrainService();
      const stats = await service.getStats(args.global);
      const lines = [
        "## Brain Statistics\n",
        `Total: ${stats.totalEntries}`,
        `Global: ${stats.globalEntries}`,
        `Project: ${stats.projectEntries}`,
        "\n### By Type",
      ];
      for (const [type, count] of Object.entries(stats.byType).sort((a, b) => b[1] - a[1])) {
        lines.push(`- ${type}: ${count}`);
      }
      return {
        content: [{ type: "text" as const, text: lines.join("\n") }],
      };
    } catch (error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
        isError: true,
      };
    }
  });

  // --------------------------------------------------------------------------
  // brain_check_connection
  // --------------------------------------------------------------------------
  server.registerTool("brain_check_connection", {
    description: `Check if the Brain API server is running and accessible.

Use this tool FIRST if you're unsure whether brain tools will work.
Returns connection status, server version, and helpful troubleshooting info if unavailable.

This is useful to:
- Verify the brain is available before starting a task that needs it
- Diagnose why other brain tools are failing
- Get instructions for starting the brain server`,
    inputSchema: {},
  }, async () => {
    return {
      content: [{
        type: "text" as const,
        text: `Brain API Status: CONNECTED (in-process)

The MCP server is running embedded in the Brain API process.
All brain tools (save, recall, search, inject, etc.) are available.
No network round-trip required - tools call the service directly.`,
      }],
    };
  });
}
