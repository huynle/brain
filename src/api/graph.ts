/**
 * Brain API - Graph Query Endpoints
 *
 * REST API endpoints for knowledge graph queries (backlinks, outlinks, related).
 * Uses OpenAPIHono for automatic OpenAPI documentation generation.
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { getBrainService } from "../core/brain-service";
import {
  GraphResponseSchema,
  ErrorResponseSchema,
  NotFoundResponseSchema,
  ServiceUnavailableResponseSchema,
  EntryIdOrPathSchema,
} from "./schemas";

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Resolve entry path from ID or path string
 */
async function resolveEntryPath(id: string): Promise<string | null> {
  const service = getBrainService();

  // Check if it's an 8-character ID
  if (/^[a-z0-9]{8}$/.test(id)) {
    try {
      const entry = await service.recall(id);
      return entry.path;
    } catch {
      return null;
    }
  }

  // Otherwise treat as path
  return id;
}

/**
 * Extract ID from request path by removing the suffix (/backlinks, /outlinks, /related)
 */
function extractIdFromPath(path: string, suffix: string): string {
  // Remove leading slash and suffix
  const withoutSuffix = path.slice(1, path.lastIndexOf(suffix));
  return withoutSuffix;
}

/**
 * Route patterns to match paths with 1-6 segments before the suffix.
 * Hono's wildcard (*) only matches a single segment, so we need multiple patterns.
 */
const BACKLINKS_PATTERNS = [
  "/*/backlinks",
  "/*/*/backlinks",
  "/*/*/*/backlinks",
  "/*/*/*/*/backlinks",
  "/*/*/*/*/*/backlinks",
  "/*/*/*/*/*/*/backlinks",
];

const OUTLINKS_PATTERNS = [
  "/*/outlinks",
  "/*/*/outlinks",
  "/*/*/*/outlinks",
  "/*/*/*/*/outlinks",
  "/*/*/*/*/*/outlinks",
  "/*/*/*/*/*/*/outlinks",
];

const RELATED_PATTERNS = [
  "/*/related",
  "/*/*/related",
  "/*/*/*/related",
  "/*/*/*/*/related",
  "/*/*/*/*/*/related",
  "/*/*/*/*/*/*/related",
];

// =============================================================================
// OpenAPI Route Definitions (for documentation)
// =============================================================================

const IdParamSchema = z.object({
  id: EntryIdOrPathSchema.openapi({
    param: {
      name: "id",
      in: "path",
    },
    description: "Entry ID (8-char) or path. For paths with multiple segments, use the full path.",
    example: "abc12def",
  }),
});

const backlinksRoute = createRoute({
  method: "get",
  path: "/{id}/backlinks",
  tags: ["Graph"],
  summary: "Get backlinks",
  description:
    "Get entries that link TO this entry. The ID can be an 8-character entry ID or a full path (e.g., projects/my-project/plan/feature.md).",
  request: {
    params: IdParamSchema,
  },
  responses: {
    200: {
      description: "List of entries linking to this entry",
      content: {
        "application/json": {
          schema: GraphResponseSchema,
        },
      },
    },
    404: {
      description: "Entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "zk CLI not available",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const outlinksRoute = createRoute({
  method: "get",
  path: "/{id}/outlinks",
  tags: ["Graph"],
  summary: "Get outlinks",
  description:
    "Get entries that this entry links TO. The ID can be an 8-character entry ID or a full path.",
  request: {
    params: IdParamSchema,
  },
  responses: {
    200: {
      description: "List of entries this entry links to",
      content: {
        "application/json": {
          schema: GraphResponseSchema,
        },
      },
    },
    404: {
      description: "Entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "zk CLI not available",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const RelatedQuerySchema = z.object({
  limit: z
    .string()
    .optional()
    .openapi({
      param: {
        name: "limit",
        in: "query",
      },
      description: "Maximum number of related entries to return",
      example: "10",
    }),
});

const relatedRoute = createRoute({
  method: "get",
  path: "/{id}/related",
  tags: ["Graph"],
  summary: "Get related entries",
  description:
    "Get entries that share links with this entry (co-citation analysis). The ID can be an 8-character entry ID or a full path.",
  request: {
    params: IdParamSchema,
    query: RelatedQuerySchema,
  },
  responses: {
    200: {
      description: "List of related entries",
      content: {
        "application/json": {
          schema: GraphResponseSchema,
        },
      },
    },
    400: {
      description: "Invalid limit parameter",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    404: {
      description: "Entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "zk CLI not available",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

// =============================================================================
// Graph Routes
// =============================================================================

export function createGraphRoutes(): OpenAPIHono {
  const graph = new OpenAPIHono({
    defaultHook: (result, c) => {
      if (!result.success) {
        const errors = result.error.issues.map((issue) => ({
          field: issue.path.join("."),
          message: issue.message,
        }));

        const fieldNames = errors.map((e) => e.field).filter(Boolean);
        const message = fieldNames.length > 0
          ? `Invalid request: ${fieldNames.join(", ")}`
          : "Invalid request";

        return c.json(
          {
            error: "Validation Error",
            message,
            details: errors,
          },
          400
        );
      }
    },
  });

  // Register OpenAPI routes for documentation (handles simple {id} paths)
  // These also serve as functional routes for single-segment IDs
  graph.openapi(backlinksRoute, async (c) => {
    const { id } = c.req.valid("param");
    return handleBacklinks(c, id);
  });

  graph.openapi(outlinksRoute, async (c) => {
    const { id } = c.req.valid("param");
    return handleOutlinks(c, id);
  });

  graph.openapi(relatedRoute, async (c) => {
    const { id } = c.req.valid("param");
    const { limit } = c.req.valid("query");
    return handleRelated(c, id, limit);
  });

  // Register multi-segment path patterns for actual routing
  // These handle paths like "projects/my-project/plan/feature.md/backlinks"
  graph.on("GET", BACKLINKS_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/backlinks");
    return handleBacklinks(c, id);
  });

  graph.on("GET", OUTLINKS_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/outlinks");
    return handleOutlinks(c, id);
  });

  graph.on("GET", RELATED_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/related");
    const limit = c.req.query("limit");
    return handleRelated(c, id, limit);
  });

  return graph;
}

// =============================================================================
// Handler Functions
// =============================================================================

async function handleBacklinks(c: any, id: string) {
  try {
    const entryPath = await resolveEntryPath(id);

    if (!entryPath) {
      return c.json(
        {
          error: "Not Found",
          message: `Entry not found: ${id}`,
        },
        404
      );
    }

    const service = getBrainService();
    const backlinks = await service.getBacklinks(entryPath);

    return c.json({
      entries: backlinks.map((entry) => ({
        id: entry.id,
        path: entry.path,
        title: entry.title,
        type: entry.type,
      })),
      total: backlinks.length,
    });
  } catch (error) {
    if (error instanceof Error) {
      if (error.message.includes("not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for backlink queries",
          },
          503
        );
      }
      if (error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
    }
    throw error;
  }
}

async function handleOutlinks(c: any, id: string) {
  try {
    const entryPath = await resolveEntryPath(id);

    if (!entryPath) {
      return c.json(
        {
          error: "Not Found",
          message: `Entry not found: ${id}`,
        },
        404
      );
    }

    const service = getBrainService();
    const outlinks = await service.getOutlinks(entryPath);

    return c.json({
      entries: outlinks.map((entry) => ({
        id: entry.id,
        path: entry.path,
        title: entry.title,
        type: entry.type,
      })),
      total: outlinks.length,
    });
  } catch (error) {
    if (error instanceof Error) {
      if (error.message.includes("not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for outlink queries",
          },
          503
        );
      }
      if (error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
    }
    throw error;
  }
}

async function handleRelated(c: any, id: string, limitParam?: string) {
  const limit = limitParam ? parseInt(limitParam, 10) : 10;

  // Validate limit
  if (isNaN(limit) || limit < 1) {
    return c.json(
      {
        error: "Validation Error",
        message: "limit must be a positive integer",
      },
      400
    );
  }

  try {
    const entryPath = await resolveEntryPath(id);

    if (!entryPath) {
      return c.json(
        {
          error: "Not Found",
          message: `Entry not found: ${id}`,
        },
        404
      );
    }

    const service = getBrainService();
    const related = await service.getRelated(entryPath, limit);

    return c.json({
      entries: related.map((entry) => ({
        id: entry.id,
        path: entry.path,
        title: entry.title,
        type: entry.type,
      })),
      total: related.length,
    });
  } catch (error) {
    if (error instanceof Error) {
      if (error.message.includes("not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for related note queries",
          },
          503
        );
      }
      if (error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
    }
    throw error;
  }
}
