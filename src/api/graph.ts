/**
 * Brain API - Graph Query Endpoints
 *
 * REST API endpoints for knowledge graph queries (backlinks, outlinks, related).
 */

import { Hono } from "hono";
import { getBrainService } from "../core/brain-service";

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
// Graph Routes
// =============================================================================

export function createGraphRoutes(): Hono {
  const graph = new Hono();

  // GET /entries/:id/backlinks - Get entries that link TO this entry
  graph.on("GET", BACKLINKS_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/backlinks");

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
  });

  // GET /entries/:id/outlinks - Get entries that this entry links TO
  graph.on("GET", OUTLINKS_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/outlinks");

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
  });

  // GET /entries/:id/related - Get entries that share links with this entry
  graph.on("GET", RELATED_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/related");
    const limitParam = c.req.query("limit");
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
  });

  return graph;
}
