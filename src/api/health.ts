/**
 * Brain API - Health and Stats Endpoints
 *
 * REST API endpoints for health checks, statistics, and maintenance operations.
 */

import { Hono } from "hono";
import { getBrainService } from "../core/brain-service";
import { getDb } from "../core/db";
import { isZkAvailable, isZkNotebookExists } from "../core/zk-client";
import type {
  EntryType,
  LinkRequest,
  HealthResponse,
} from "../core/types";
import { ENTRY_TYPES } from "../core/types";

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
// Health Routes Factory
// =============================================================================

export function createHealthRoutes(): Hono {
  const health = new Hono();

  // GET /stats - Brain statistics
  health.get("/stats", async (c) => {
    const query = c.req.query();
    const global = query.global === "true";

    try {
      const service = getBrainService();
      const stats = await service.getStats(global);

      return c.json(stats);
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
  health.get("/orphans", async (c) => {
    const query = c.req.query();
    const type = query.type as EntryType | undefined;
    const limit = query.limit ? parseInt(query.limit, 10) : 20;

    // Validate type if provided
    if (type && !ENTRY_TYPES.includes(type)) {
      return c.json(
        {
          error: "Validation Error",
          message: `Invalid type. Must be one of: ${ENTRY_TYPES.join(", ")}`,
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
      const entries = await service.getOrphans(type, limit);

      return c.json({
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
      });
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
  health.get("/stale", async (c) => {
    const query = c.req.query();
    const days = query.days ? parseInt(query.days, 10) : 30;
    const type = query.type as EntryType | undefined;
    const limit = query.limit ? parseInt(query.limit, 10) : 20;

    // Validate type if provided
    if (type && !ENTRY_TYPES.includes(type)) {
      return c.json(
        {
          error: "Validation Error",
          message: `Invalid type. Must be one of: ${ENTRY_TYPES.join(", ")}`,
        },
        400
      );
    }

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

      return c.json({
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
      });
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
  // Note: Using {.+} pattern to capture full paths with slashes
  health.post("/entries/:id{.+}/verify", async (c) => {
    const id = c.req.param("id");

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

      return c.json({
        message: "Entry verified",
        path: entryPath,
        verifiedAt: new Date().toISOString(),
      });
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
  health.post("/link", async (c) => {
    try {
      const body = await c.req.json();

      // Validate request
      if (!body || typeof body !== "object") {
        return c.json(
          {
            error: "Validation Error",
            message: "Request body is required",
          },
          400
        );
      }

      const { title, path, withTitle } = body as LinkRequest;

      if (!title && !path) {
        return c.json(
          {
            error: "Validation Error",
            message: "Either title or path must be provided",
          },
          400
        );
      }

      if (title !== undefined && typeof title !== "string") {
        return c.json(
          {
            error: "Validation Error",
            message: "title must be a string",
          },
          400
        );
      }

      if (path !== undefined && typeof path !== "string") {
        return c.json(
          {
            error: "Validation Error",
            message: "path must be a string",
          },
          400
        );
      }

      if (withTitle !== undefined && typeof withTitle !== "boolean") {
        return c.json(
          {
            error: "Validation Error",
            message: "withTitle must be a boolean",
          },
          400
        );
      }

      const service = getBrainService();
      const result = await service.generateLink({ title, path, withTitle });

      return c.json(result);
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
