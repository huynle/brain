/**
 * Brain API Integration Tools for Pi
 *
 * Provides tools for interacting with the Brain API from within Pi agents.
 * Tools: brain_recall, brain_save, brain_search, brain_inject, brain_update
 */

const BRAIN_API_URL = process.env.BRAIN_API_URL || "http://localhost:3333";

interface BrainEntry {
  path: string;
  title: string;
  type: string;
  status?: string;
  content: string;
  tags?: string[];
}

interface SearchResult {
  entries: BrainEntry[];
  total: number;
}

async function brainFetch(path: string, options?: RequestInit): Promise<unknown> {
  const url = `${BRAIN_API_URL}${path}`;
  const response = await fetch(url, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(`Brain API error (${response.status}): ${text}`);
  }

  return response.json();
}

// brain_recall - Retrieve a specific entry by path or title
pi.registerTool("brain_recall", {
  description:
    "Retrieve a specific entry from the brain by path or title. Use this to read plans, tasks, decisions, and other stored knowledge.",
  parameters: {
    type: "object",
    properties: {
      path: {
        type: "string",
        description: "Full path to the brain entry (e.g., projects/my-project/task/abc123.md)",
      },
      title: {
        type: "string",
        description: "Title of the entry to search for",
      },
    },
  },
  handler: async (params: Record<string, unknown>) => {
    const queryParams = new URLSearchParams();
    if (params.path) queryParams.set("path", params.path as string);
    if (params.title) queryParams.set("title", params.title as string);

    return brainFetch(`/api/v1/recall?${queryParams.toString()}`);
  },
});

// brain_save - Save content to the brain
pi.registerTool("brain_save", {
  description:
    "Save content to the brain for future reference. Supports types: summary, report, plan, task, decision, exploration, pattern, learning, idea, scratch.",
  parameters: {
    type: "object",
    properties: {
      type: {
        type: "string",
        enum: [
          "summary",
          "report",
          "plan",
          "task",
          "decision",
          "exploration",
          "pattern",
          "learning",
          "idea",
          "scratch",
        ],
        description: "Type of brain entry",
      },
      title: {
        type: "string",
        description: "Title for the entry",
      },
      content: {
        type: "string",
        description: "Markdown content to save",
      },
      tags: {
        type: "array",
        items: { type: "string" },
        description: "Tags for categorization",
      },
      status: {
        type: "string",
        enum: ["draft", "active", "completed", "archived"],
        description: "Entry status",
      },
    },
    required: ["type", "title", "content"],
  },
  handler: async (params: Record<string, unknown>) => {
    return brainFetch("/api/v1/entries", {
      method: "POST",
      body: JSON.stringify(params),
    });
  },
});

// brain_search - Search brain entries
pi.registerTool("brain_search", {
  description: "Search the brain using full-text search. Finds entries matching your query.",
  parameters: {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "Search query",
      },
      type: {
        type: "string",
        description: "Filter by entry type",
      },
      limit: {
        type: "number",
        description: "Maximum results to return",
      },
    },
    required: ["query"],
  },
  handler: async (params: Record<string, unknown>) => {
    const queryParams = new URLSearchParams();
    queryParams.set("q", params.query as string);
    if (params.type) queryParams.set("type", params.type as string);
    if (params.limit) queryParams.set("limit", String(params.limit));

    return brainFetch(`/api/v1/search?${queryParams.toString()}`);
  },
});

// brain_inject - Search and return relevant context
pi.registerTool("brain_inject", {
  description:
    "Search the brain and return relevant context. Use this to recall knowledge before starting a task.",
  parameters: {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "Context search query",
      },
      type: {
        type: "string",
        description: "Filter by entry type",
      },
      maxEntries: {
        type: "number",
        description: "Maximum entries to return",
      },
    },
    required: ["query"],
  },
  handler: async (params: Record<string, unknown>) => {
    const queryParams = new URLSearchParams();
    queryParams.set("q", params.query as string);
    if (params.type) queryParams.set("type", params.type as string);
    if (params.maxEntries) queryParams.set("limit", String(params.maxEntries));

    return brainFetch(`/api/v1/inject?${queryParams.toString()}`);
  },
});

// brain_update - Update an existing brain entry
pi.registerTool("brain_update", {
  description:
    "Update an existing brain entry's status, append content, or modify metadata.",
  parameters: {
    type: "object",
    properties: {
      path: {
        type: "string",
        description: "Path to the brain entry to update",
      },
      status: {
        type: "string",
        enum: [
          "draft",
          "pending",
          "active",
          "in_progress",
          "blocked",
          "cancelled",
          "completed",
          "validated",
          "superseded",
          "archived",
        ],
        description: "New status",
      },
      append: {
        type: "string",
        description: "Content to append to the entry",
      },
      note: {
        type: "string",
        description: "Note about the update",
      },
      title: {
        type: "string",
        description: "New title",
      },
      tags: {
        type: "array",
        items: { type: "string" },
        description: "Updated tags",
      },
    },
    required: ["path"],
  },
  handler: async (params: Record<string, unknown>) => {
    const path = params.path as string;
    const body = { ...params };
    delete body.path;

    return brainFetch(`/api/v1/entries/${encodeURIComponent(path)}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  },
});
