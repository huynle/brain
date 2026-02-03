/**
 * Brain API - HTTP Server
 *
 * Hono-based HTTP server with routes and middleware
 */

import { Hono } from "hono";
import { cors } from "hono/cors";
import { logger } from "hono/logger";
import type { Config } from "./core/types";
import { createEntriesRoutes } from "./api/entries";
import { createGraphRoutes } from "./api/graph";
import { createHealthRoutes, getHealthStatus } from "./api/health";
import { createSearchRoutes } from "./api/search";
import { createSectionRoutes } from "./api/sections";
import { createTaskRoutes } from "./api/tasks";

export function createApp(config: Config): Hono {
  const app = new Hono();

  // Middleware
  app.use("*", cors());
  app.use("*", logger());

  // Health check (always available, fast)
  app.get("/health", async (c) => {
    const health = await getHealthStatus();
    return c.json({
      ...health,
      version: "0.1.0",
    });
  });

  // API v1 routes
  const api = new Hono();

  // Health and stats routes (stats, orphans, stale, verify, link)
  // NOTE: Must be registered BEFORE entry CRUD routes to avoid conflicts
  const healthRoutes = createHealthRoutes();
  api.route("/", healthRoutes);

  // Section routes (sections, sections/:title)
  // NOTE: Must be registered BEFORE entry CRUD routes because the entries
  // routes use /:id{.+} which would catch /id/sections etc.
  api.route("/entries", createSectionRoutes());

  // Graph query routes (backlinks, outlinks, related)
  // NOTE: Must be registered BEFORE entry CRUD routes because the entries
  // routes use /:id{.+} which would catch /id/backlinks etc.
  api.route("/entries", createGraphRoutes());

  // Task routes (specific paths first)
  api.route("/tasks", createTaskRoutes());

  // Entry CRUD routes
  api.route("/entries", createEntriesRoutes());

  // Search routes (search and inject)
  api.route("/", createSearchRoutes());

  // Mount API routes
  app.route("/api/v1", api);

  // 404 handler
  app.notFound((c) => {
    return c.json(
      {
        error: "Not Found",
        message: `Route ${c.req.method} ${c.req.path} not found`,
      },
      404
    );
  });

  // Error handler
  app.onError((err, c) => {
    console.error("Server error:", err);
    return c.json(
      {
        error: "Internal Server Error",
        message: err.message,
      },
      500
    );
  });

  return app;
}
