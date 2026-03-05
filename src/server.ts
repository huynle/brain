/**
 * Brain API - HTTP Server
 *
 * OpenAPIHono-based HTTP server with routes, middleware, and OpenAPI documentation
 */

import { OpenAPIHono } from "@hono/zod-openapi";
import { cors } from "hono/cors";
import { logger } from "hono/logger";
import { secureHeaders } from "hono/secure-headers";
import type { Config } from "./core/types";
import { createEntriesRoutes } from "./api/entries";
import { createGraphRoutes } from "./api/graph";
import { createHealthRoutes, getHealthStatus } from "./api/health";
import { createSearchRoutes } from "./api/search";
import { createSectionRoutes } from "./api/sections";
import { createTaskRoutes } from "./api/tasks";
import { createMcpRoutes } from "./mcp/transport";
import { createOAuthRoutes } from "./mcp/auth";
import { createProjectRealtimeHub } from "./core/realtime-hub";
import { createMonitorRoutes } from "./api/monitors";
import { createTokenRoutes } from "./api/tokens";
import { apiAuth } from "./auth";

export function createApp(config: Config): OpenAPIHono {
  const app = new OpenAPIHono();

  // Middleware - Security headers
  app.use(
    "*",
    secureHeaders({
      xContentTypeOptions: "nosniff",
      xFrameOptions: "DENY",
      xXssProtection: "0", // Disabled per modern best practice
      referrerPolicy: "strict-origin-when-cross-origin",
      ...(config.server.tls.enabled
        ? { strictTransportSecurity: "max-age=31536000; includeSubDomains" }
        : {}),
    })
  );

  // Middleware - CORS (configurable via CORS_ORIGIN env var)
  app.use(
    "*",
    cors({
      origin: config.server.corsOrigin,
      allowMethods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
      allowHeaders: ["Content-Type", "Authorization", "Accept"],
      exposeHeaders: ["X-Request-Id"],
      credentials: config.server.corsOrigin !== "*",
      maxAge: 86400,
    })
  );

  // Middleware - Logger with token sanitization
  app.use(
    "*",
    logger((message: string, ...rest: string[]) => {
      const sanitized = message.replace(/[?&]token=[^&\s]+/g, "token=***");
      console.log(sanitized, ...rest);
    })
  );

  // API v1 routes
  const api = new OpenAPIHono();
  const taskRealtimeHub = createProjectRealtimeHub();

  // Health check endpoint (unauthenticated - registered before auth middleware)
  api.get("/health", async (c) => {
    const health = await getHealthStatus();
    return c.json({
      ...health,
      version: "0.1.0",
    });
  });

  // API authentication middleware
  // Protects all routes registered AFTER this point when ENABLE_AUTH=true.
  // Health endpoint above is exempt. Accepts Bearer header or ?token= query param.
  api.use("*", apiAuth(config.server.enableAuth));

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
  api.route("/tasks", createTaskRoutes({ realtimeHub: taskRealtimeHub }));

  // Entry CRUD routes
  api.route("/entries", createEntriesRoutes({ realtimeHub: taskRealtimeHub }));

  // Monitor routes (templates, create, toggle, delete)
  api.route("/monitors", createMonitorRoutes({ realtimeHub: taskRealtimeHub }));

  // Token management routes (create, list, revoke)
  api.route("/tokens", createTokenRoutes());

  // Search routes (search and inject)
  api.route("/", createSearchRoutes());

  // Mount API routes
  app.route("/api/v1", api);

  // Mount OAuth 2.1 routes for MCP authentication
  app.route("/", createOAuthRoutes());

  // Mount MCP Streamable HTTP transport
  app.route("/", createMcpRoutes(config));

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
      {
        name: "Monitors",
        description: "Monitor template management and lifecycle",
      },
      {
        name: "Tokens",
        description: "API token management (create, list, revoke)",
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
