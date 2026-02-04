/**
 * Brain API - HTTP Server
 *
 * OpenAPIHono-based HTTP server with routes, middleware, and OpenAPI documentation
 */

import { OpenAPIHono } from "@hono/zod-openapi";
import { cors } from "hono/cors";
import { logger } from "hono/logger";
import type { Config } from "./core/types";
import { createEntriesRoutes } from "./api/entries";
import { createGraphRoutes } from "./api/graph";
import { createHealthRoutes, getHealthStatus } from "./api/health";
import { createSearchRoutes } from "./api/search";
import { createSectionRoutes } from "./api/sections";
import { createTaskRoutes } from "./api/tasks";

export function createApp(config: Config): OpenAPIHono {
  const app = new OpenAPIHono();

  // Middleware
  app.use("*", cors());
  app.use("*", logger());

  // API v1 routes
  const api = new OpenAPIHono();

  // Health check endpoint
  api.get("/health", async (c) => {
    const health = await getHealthStatus();
    return c.json({
      ...health,
      version: "0.1.0",
    });
  });

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

  // OpenAPI documentation endpoint
  // Returns the OpenAPI 3.0 specification as JSON
  app.doc("/api/v1/openapi.json", (c) => ({
    openapi: "3.0.0",
    info: {
      title: "Brain API",
      version: "0.1.0",
      description: `REST API service for AI agent memory and knowledge management.

Brain API provides a comprehensive set of endpoints for managing "brain entries" - markdown notes in a Zettelkasten-style knowledge graph. 

## Features
- **Entry Management**: Create, read, update, delete brain entries
- **Full-text Search**: Search entries by content and metadata
- **Knowledge Graph**: Query backlinks, outlinks, and related entries
- **Task Management**: Manage tasks with dependency resolution
- **Section Extraction**: Extract sections from markdown entries

## Authentication
Authentication is optional and can be enabled via the \`ENABLE_AUTH\` environment variable.
When enabled, pass the API key in the \`Authorization\` header.`,
      contact: {
        name: "Brain API Contributors",
        url: "https://github.com/huynle/brain",
      },
      license: {
        name: "MIT",
        url: "https://opensource.org/licenses/MIT",
      },
    },
    servers: [
      {
        url: new URL(c.req.url).origin,
        description: "Current server",
      },
      {
        url: "http://localhost:3333",
        description: "Local development server",
      },
    ],
    tags: [
      {
        name: "Entries",
        description: "CRUD operations for brain entries",
      },
      {
        name: "Search",
        description: "Full-text search and context injection",
      },
      {
        name: "Graph",
        description: "Knowledge graph traversal (backlinks, outlinks, related)",
      },
      {
        name: "Sections",
        description: "Extract and query markdown sections",
      },
      {
        name: "Tasks",
        description: "Task management with dependency resolution",
      },
      {
        name: "Health",
        description: "Health checks, statistics, and maintenance",
      },
    ],
  }));

  // Optional: OpenAPI 3.1 endpoint
  app.doc31("/api/v1/openapi31.json", (c) => ({
    openapi: "3.1.0",
    info: {
      title: "Brain API",
      version: "0.1.0",
      description: "REST API service for AI agent memory and knowledge management.",
    },
    servers: [
      {
        url: new URL(c.req.url).origin,
        description: "Current server",
      },
    ],
  }));

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
