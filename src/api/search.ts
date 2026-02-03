/**
 * Brain API - Search Endpoints
 *
 * REST API endpoints for search and context injection operations.
 */

import { Hono } from "hono";
import { getBrainService } from "../core/brain-service";
import type {
  SearchRequest,
  InjectRequest,
  EntryType,
  EntryStatus,
} from "../core/types";
import { ENTRY_TYPES, ENTRY_STATUSES } from "../core/types";

// =============================================================================
// Validation Helpers
// =============================================================================

interface ValidationError {
  field: string;
  message: string;
}

function validateSearchRequest(body: unknown): {
  valid: boolean;
  errors: ValidationError[];
  data?: SearchRequest;
} {
  const errors: ValidationError[] = [];

  if (!body || typeof body !== "object") {
    return { valid: false, errors: [{ field: "body", message: "Request body is required" }] };
  }

  const req = body as Record<string, unknown>;

  // Required: query
  if (!req.query || typeof req.query !== "string") {
    errors.push({ field: "query", message: "query is required and must be a string" });
  } else if (req.query.trim().length === 0) {
    errors.push({ field: "query", message: "query cannot be empty" });
  }

  // Optional: type
  if (req.type !== undefined) {
    if (typeof req.type !== "string" || !ENTRY_TYPES.includes(req.type as EntryType)) {
      errors.push({
        field: "type",
        message: `type must be one of: ${ENTRY_TYPES.join(", ")}`,
      });
    }
  }

  // Optional: status
  if (req.status !== undefined) {
    if (typeof req.status !== "string" || !ENTRY_STATUSES.includes(req.status as EntryStatus)) {
      errors.push({
        field: "status",
        message: `status must be one of: ${ENTRY_STATUSES.join(", ")}`,
      });
    }
  }

  // Optional: limit
  if (req.limit !== undefined) {
    if (typeof req.limit !== "number" || !Number.isInteger(req.limit) || req.limit < 1) {
      errors.push({ field: "limit", message: "limit must be a positive integer" });
    }
  }

  // Optional: global
  if (req.global !== undefined && typeof req.global !== "boolean") {
    errors.push({ field: "global", message: "global must be a boolean" });
  }

  if (errors.length > 0) {
    return { valid: false, errors };
  }

  return {
    valid: true,
    errors: [],
    data: {
      query: (req.query as string).trim(),
      type: req.type as EntryType | undefined,
      status: req.status as EntryStatus | undefined,
      limit: req.limit as number | undefined,
      global: req.global as boolean | undefined,
    },
  };
}

function validateInjectRequest(body: unknown): {
  valid: boolean;
  errors: ValidationError[];
  data?: InjectRequest;
} {
  const errors: ValidationError[] = [];

  if (!body || typeof body !== "object") {
    return { valid: false, errors: [{ field: "body", message: "Request body is required" }] };
  }

  const req = body as Record<string, unknown>;

  // Required: query
  if (req.query === undefined || req.query === null || typeof req.query !== "string") {
    errors.push({ field: "query", message: "query is required and must be a string" });
  } else if (req.query.trim().length === 0) {
    errors.push({ field: "query", message: "query cannot be empty" });
  }

  // Optional: maxEntries
  if (req.maxEntries !== undefined) {
    if (typeof req.maxEntries !== "number" || !Number.isInteger(req.maxEntries) || req.maxEntries < 1) {
      errors.push({ field: "maxEntries", message: "maxEntries must be a positive integer" });
    }
  }

  // Optional: type
  if (req.type !== undefined) {
    if (typeof req.type !== "string" || !ENTRY_TYPES.includes(req.type as EntryType)) {
      errors.push({
        field: "type",
        message: `type must be one of: ${ENTRY_TYPES.join(", ")}`,
      });
    }
  }

  if (errors.length > 0) {
    return { valid: false, errors };
  }

  return {
    valid: true,
    errors: [],
    data: {
      query: (req.query as string).trim(),
      maxEntries: req.maxEntries as number | undefined,
      type: req.type as EntryType | undefined,
    },
  };
}

// =============================================================================
// Search Routes
// =============================================================================

export function createSearchRoutes(): Hono {
  const search = new Hono();

  // POST /search - Full-text search
  search.post("/search", async (c) => {
    try {
      const body = await c.req.json();
      const validation = validateSearchRequest(body);

      if (!validation.valid) {
        return c.json(
          {
            error: "Validation Error",
            message: "Invalid request body",
            details: validation.errors,
          },
          400
        );
      }

      const service = getBrainService();
      const result = await service.search(validation.data!);

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
      });
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
  search.post("/inject", async (c) => {
    try {
      const body = await c.req.json();
      const validation = validateInjectRequest(body);

      if (!validation.valid) {
        return c.json(
          {
            error: "Validation Error",
            message: "Invalid request body",
            details: validation.errors,
          },
          400
        );
      }

      const service = getBrainService();
      const result = await service.inject(validation.data!);

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
      });
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
      // Note: inject gracefully handles zk unavailable by returning a message
      throw error;
    }
  });

  return search;
}
