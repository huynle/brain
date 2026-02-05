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
    description: "Update an existing brain entry's status, title, or append content.",
    inputSchema: {
      path: z.string().describe("Path to the entry to update"),
      status: z.enum(ENTRY_STATUSES).optional().describe("New status"),
      title: z.string().optional().describe("New title"),
      append: z.string().optional().describe("Content to append"),
      note: z.string().optional().describe("Short note to add"),
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
