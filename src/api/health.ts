/**
 * Brain API - Health and Stats Endpoints
 *
 * REST API endpoints for health checks, statistics, and maintenance operations.
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { getBrainService } from "../core/brain-service";
import { getDb } from "../core/db";
import { isZkAvailable, isZkNotebookExists } from "../core/zk-client";
import type { HealthResponse } from "../core/types";
import {
  StatsResponseSchema,
  OrphansResponseSchema,
  StaleResponseSchema,
  VerifyResponseSchema,
  LinkRequestSchema,
  LinkResponseSchema,
  ErrorResponseSchema,
  NotFoundResponseSchema,
  ServiceUnavailableResponseSchema,
  EntryTypeSchema,
} from "./schemas";

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Check if the database is available and responding.
 */
function isDatabaseAvailable(): boolean {
  try {
    const db = getDb();
    const result = db.prepare("SELECT 1 as test").get() as { test: number };
    return result?.test === 1;
  } catch {
    return false;
  }
}

// =============================================================================
// Query Schemas
// =============================================================================

const StatsQuerySchema = z.object({
  global: z
    .string()
    .optional()
    .openapi({
      param: { in: "query" },
      description: "If true, get global stats",
      example: "true",
    }),
});

const OrphansQuerySchema = z.object({
  type: EntryTypeSchema.optional().openapi({
    param: { in: "query" },
    description: "Filter by entry type",
  }),
  limit: z
    .string()
    .optional()
    .openapi({
      param: { in: "query" },
      description: "Maximum number of entries to return (1-100)",
      example: "20",
    }),
});

const StaleQuerySchema = z.object({
  days: z
    .string()
    .optional()
    .openapi({
      param: { in: "query" },
      description: "Days since last verification (1-365)",
      example: "30",
    }),
  type: EntryTypeSchema.optional().openapi({
    param: { in: "query" },
    description: "Filter by entry type",
  }),
  limit: z
    .string()
    .optional()
    .openapi({
      param: { in: "query" },
      description: "Maximum number of entries to return (1-100)",
      example: "20",
    }),
});

// =============================================================================
// Route Definitions
// =============================================================================

const statsRoute = createRoute({
  method: "get",
  path: "/stats",
  tags: ["Health"],
  summary: "Get brain statistics",
  description: "Returns statistics about the brain including entry counts, types, and health metrics.",
  request: {
    query: StatsQuerySchema,
  },
  responses: {
    200: {
      description: "Brain statistics",
      content: {
        "application/json": {
          schema: StatsResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const orphansRoute = createRoute({
  method: "get",
  path: "/orphans",
  tags: ["Health"],
  summary: "Get orphan entries",
  description: "Returns entries with no incoming backlinks, useful for knowledge graph maintenance.",
  request: {
    query: OrphansQuerySchema,
  },
  responses: {
    200: {
      description: "List of orphan entries",
      content: {
        "application/json": {
          schema: OrphansResponseSchema,
        },
      },
    },
    400: {
      description: "Validation error",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const staleRoute = createRoute({
  method: "get",
  path: "/stale",
  tags: ["Health"],
  summary: "Get stale entries",
  description: "Returns entries that have not been verified recently.",
  request: {
    query: StaleQuerySchema,
  },
  responses: {
    200: {
      description: "List of stale entries",
      content: {
        "application/json": {
          schema: StaleResponseSchema,
        },
      },
    },
    400: {
      description: "Validation error",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const verifyRoute = createRoute({
  method: "post",
  path: "/entries/:id{.+}/verify",
  tags: ["Health"],
  summary: "Verify an entry",
  description: "Mark an entry as verified, updating its last_verified timestamp.",
  request: {
    params: z.object({
      id: z.string().openapi({
        param: { in: "path" },
        description: "Entry ID (8-char alphanumeric) or full path",
        example: "abc12def",
      }),
    }),
  },
  responses: {
    200: {
      description: "Entry verified successfully",
      content: {
        "application/json": {
          schema: VerifyResponseSchema,
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
  },
});

const linkRoute = createRoute({
  method: "post",
  path: "/link",
  tags: ["Health"],
  summary: "Generate markdown link",
  description: "Generate a markdown link to an entry by title or path.",
  request: {
    body: {
      content: {
        "application/json": {
          schema: LinkRequestSchema,
        },
      },
    },
  },
  responses: {
    200: {
      description: "Generated link",
      content: {
        "application/json": {
          schema: LinkResponseSchema,
        },
      },
    },
    400: {
      description: "Validation error",
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
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

// =============================================================================
// Health Routes Factory
// =============================================================================

export function createHealthRoutes(): OpenAPIHono {
  const health = new OpenAPIHono({
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

  // Handle JSON parsing errors
  health.onError((err, c) => {
    if (err instanceof SyntaxError || (err instanceof Error && err.message.includes("JSON"))) {
      return c.json(
        {
          error: "Bad Request",
          message: "Invalid JSON in request body",
        },
        400
      );
    }
    throw err;
  });

  // GET /stats - Brain statistics
  health.openapi(statsRoute, async (c) => {
    const query = c.req.valid("query");
    const global = query.global === "true";

    try {
      const service = getBrainService();
      const stats = await service.getStats(global);

      return c.json(stats, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for stats",
          },
          503
        );
      }
      throw error;
    }
  });

  // GET /orphans - Entries with no backlinks
  health.openapi(orphansRoute, async (c) => {
    const query = c.req.valid("query");
    const type = query.type;
    const limit = query.limit ? parseInt(query.limit, 10) : 20;

    // Validate limit
    if (isNaN(limit) || limit < 1 || limit > 100) {
      return c.json(
        {
          error: "Validation Error",
          message: "limit must be between 1 and 100",
        },
        400
      );
    }

    try {
      const service = getBrainService();
      const entries = await service.getOrphans(type, limit);

      return c.json(
        {
          entries: entries.map((e) => ({
            id: e.id,
            path: e.path,
            title: e.title,
            type: e.type,
            status: e.status,
            created: e.created,
          })),
          total: entries.length,
          message:
            entries.length > 0
              ? "Consider linking these notes to improve knowledge graph connectivity"
              : "No orphan entries found",
        },
        200
      );
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for orphan detection",
          },
          503
        );
      }
      throw error;
    }
  });

  // GET /stale - Entries not verified recently
  health.openapi(staleRoute, async (c) => {
    const query = c.req.valid("query");
    const days = query.days ? parseInt(query.days, 10) : 30;
    const type = query.type;
    const limit = query.limit ? parseInt(query.limit, 10) : 20;

    // Validate days
    if (isNaN(days) || days < 1 || days > 365) {
      return c.json(
        {
          error: "Validation Error",
          message: "days must be between 1 and 365",
        },
        400
      );
    }

    // Validate limit
    if (isNaN(limit) || limit < 1 || limit > 100) {
      return c.json(
        {
          error: "Validation Error",
          message: "limit must be between 1 and 100",
        },
        400
      );
    }

    try {
      const service = getBrainService();
      let entries = await service.getStale(days, limit);

      // Filter by type if provided
      if (type) {
        entries = entries.filter((e) => e.type === type);
      }

      // Calculate days since verified for each entry
      const now = Date.now();
      const MS_PER_DAY = 24 * 60 * 60 * 1000;

      return c.json(
        {
          entries: entries.map((e) => {
            const lastVerified = e.last_verified
              ? new Date(e.last_verified).getTime()
              : null;
            const daysSinceVerified = lastVerified
              ? Math.floor((now - lastVerified) / MS_PER_DAY)
              : null;

            return {
              id: e.id,
              path: e.path,
              title: e.title,
              type: e.type,
              status: e.status,
              daysSinceVerified,
              lastVerified: e.last_verified || null,
            };
          }),
          total: entries.length,
        },
        200
      );
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for stale detection",
          },
          503
        );
      }
      throw error;
    }
  });

  // POST /entries/:id/verify - Mark entry as verified
  health.openapi(verifyRoute, async (c) => {
    const { id } = c.req.valid("param");

    try {
      const service = getBrainService();

      // First, resolve the ID to a path
      let entryPath = id;
      if (/^[a-z0-9]{8}$/.test(id)) {
        // It's an ID, need to find the path
        try {
          const entry = await service.recall(id);
          entryPath = entry.path;
        } catch {
          return c.json(
            {
              error: "Not Found",
              message: `Entry not found: ${id}`,
            },
            404
          );
        }
      }

      await service.verify(entryPath);

      return c.json(
        {
          message: "Entry verified",
          path: entryPath,
          verifiedAt: new Date().toISOString(),
        },
        200
      );
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("Entry not found")) {
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
  });

  // POST /link - Generate markdown link
  health.openapi(linkRoute, async (c) => {
    try {
      const body = c.req.valid("json");
      const { title, path, withTitle } = body;

      const service = getBrainService();
      const result = await service.generateLink({ title, path, withTitle });

      return c.json(result, 200);
    } catch (error) {
      if (error instanceof SyntaxError) {
        return c.json(
          {
            error: "Bad Request",
            message: "Invalid JSON in request body",
          },
          400
        );
      }
      if (error instanceof Error) {
        if (
          error.message.includes("not found") ||
          error.message.includes("No exact match") ||
          error.message.includes("No entry found")
        ) {
          return c.json(
            {
              error: "Not Found",
              message: error.message,
            },
            404
          );
        }
        if (error.message.includes("zk CLI not available")) {
          return c.json(
            {
              error: "Service Unavailable",
              message: "zk CLI is required to resolve title to path",
            },
            503
          );
        }
      }
      throw error;
    }
  });

  return health;
}

// =============================================================================
// Enhanced Health Check
// =============================================================================

/**
 * Create the enhanced health check response.
 * This is fast and should not block on slow operations.
 */
export async function getHealthStatus(): Promise<HealthResponse> {
  const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
  const dbAvailable = isDatabaseAvailable();

  // Determine overall status
  let status: "healthy" | "degraded" | "unhealthy";
  if (zkAvailable && dbAvailable) {
    status = "healthy";
  } else if (dbAvailable) {
    // Can still function with DB but no zk
    status = "degraded";
  } else {
    status = "unhealthy";
  }

  return {
    status,
    zkAvailable,
    dbAvailable,
    timestamp: new Date().toISOString(),
  };
}
