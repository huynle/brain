/**
 * Brain API - Search Endpoints
 *
 * REST API endpoints for search and context injection operations.
 * Uses OpenAPIHono for automatic OpenAPI documentation generation.
 */

import { OpenAPIHono, createRoute } from "@hono/zod-openapi";
import { getBrainService } from "../core/brain-service";
import {
  SearchRequestSchema,
  SearchResponseSchema,
  InjectRequestSchema,
  InjectResponseSchema,
  ErrorResponseSchema,
  ServiceUnavailableResponseSchema,
} from "./schemas";

// =============================================================================
// Route Definitions
// =============================================================================

const searchRoute = createRoute({
  method: "post",
  path: "/search",
  tags: ["Search"],
  summary: "Full-text search",
  description: "Search brain entries by query string with optional filtering by type, status, and scope",
  request: {
    body: {
      content: { "application/json": { schema: SearchRequestSchema } },
      required: true,
    },
  },
  responses: {
    200: {
      content: { "application/json": { schema: SearchResponseSchema } },
      description: "Search results with snippets",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Validation error",
    },
    503: {
      content: { "application/json": { schema: ServiceUnavailableResponseSchema } },
      description: "zk CLI unavailable",
    },
  },
});

const injectRoute = createRoute({
  method: "post",
  path: "/inject",
  tags: ["Search"],
  summary: "Context injection",
  description: "Search and format relevant entries as context for AI consumption",
  request: {
    body: {
      content: { "application/json": { schema: InjectRequestSchema } },
      required: true,
    },
  },
  responses: {
    200: {
      content: { "application/json": { schema: InjectResponseSchema } },
      description: "Formatted context and entry summaries",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Validation error",
    },
  },
});

// =============================================================================
// Search Routes
// =============================================================================

export function createSearchRoutes(): OpenAPIHono {
  const search = new OpenAPIHono({
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
  search.onError((err, c) => {
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

  // POST /search - Full-text search
  search.openapi(searchRoute, async (c) => {
    try {
      const body = c.req.valid("json");

      const service = getBrainService();
      const result = await service.search(body);

      // Format response per spec: include snippet (first 150 chars of content)
      const formattedResults = result.results.map((entry) => ({
        id: entry.id,
        path: entry.path,
        title: entry.title,
        type: entry.type,
        status: entry.status,
        snippet: entry.content?.slice(0, 150) || "",
      }));

      return c.json({
        results: formattedResults,
        total: result.total,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for search operations",
          },
          503
        );
      }
      throw error;
    }
  });

  // POST /inject - Get relevant context for a query
  search.openapi(injectRoute, async (c) => {
    const body = c.req.valid("json");

    const service = getBrainService();
    const result = await service.inject(body);

    // Format response per spec
    const formattedEntries = result.entries.map((entry) => ({
      id: entry.id,
      path: entry.path,
      title: entry.title,
      type: entry.type,
      status: entry.status,
    }));

    return c.json({
      context: result.context,
      entries: formattedEntries,
    }, 200);
  });

  return search;
}
