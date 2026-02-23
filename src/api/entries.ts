/**
 * Brain API - Entry CRUD Endpoints
 *
 * REST API endpoints for entry operations using the BrainService.
 * Uses OpenAPIHono with Zod schemas for automatic validation and documentation.
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { HTTPException } from "hono/http-exception";
import { getBrainService, DependencyValidationError } from "../core/brain-service";
import { getTaskService } from "../core/task-service";
import type { BrainEntry } from "../core/types";
import {
  createProjectRealtimeHub,
  publishProjectDirty,
  type ProjectRealtimeHub,
} from "../core/realtime-hub";
import {
  CreateEntryRequestSchema,
  CreateEntryResponseSchema,
  UpdateEntryRequestSchema,
  BrainEntrySchema,
  ListEntriesQuerySchema,
  ListEntriesResponseSchema,
  GetEntryResponseSchema,
  DeleteEntryResponseSchema,
  ErrorResponseSchema,
  NotFoundResponseSchema,
  ServiceUnavailableResponseSchema,
  EntryIdOrPathSchema,
  MoveEntryRequestSchema,
  MoveEntryResponseSchema,
} from "./schemas";

// =============================================================================
// Route Definitions
// =============================================================================

// POST /entries - Create a new entry
const createEntryRoute = createRoute({
  method: "post",
  path: "/",
  tags: ["Entries"],
  summary: "Create a new entry",
  description: "Creates a new brain entry with the specified type, title, and content.",
  request: {
    body: {
      content: {
        "application/json": {
          schema: CreateEntryRequestSchema,
        },
      },
    },
  },
  responses: {
    201: {
      content: { "application/json": { schema: CreateEntryResponseSchema } },
      description: "Entry created successfully",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Validation error",
    },
  },
});

// GET /entries/:id - Get entry by ID or path
// Note: Using Hono's regex syntax `:id{.+}` to capture paths with slashes
const getEntryRoute = createRoute({
  method: "get",
  path: "/:id{.+}",
  tags: ["Entries"],
  summary: "Get entry by ID or path",
  description: "Retrieves a brain entry by its 8-character ID or full path. Also returns backlinks.",
  request: {
    params: z.object({
      id: EntryIdOrPathSchema.openapi({
        param: { name: "id", in: "path" },
        description: "Entry ID (8-char alphanumeric) or full path",
      }),
    }),
  },
  responses: {
    200: {
      content: { "application/json": { schema: GetEntryResponseSchema } },
      description: "Entry found",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Entry not found",
    },
  },
});

// GET /entries - List entries with filters
const listEntriesRoute = createRoute({
  method: "get",
  path: "/",
  tags: ["Entries"],
  summary: "List entries",
  description: "Lists brain entries with optional filtering by type, status, and pagination.",
  request: {
    query: ListEntriesQuerySchema,
  },
  responses: {
    200: {
      content: { "application/json": { schema: ListEntriesResponseSchema } },
      description: "List of entries",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Validation error",
    },
    503: {
      content: { "application/json": { schema: ServiceUnavailableResponseSchema } },
      description: "Service unavailable (zk CLI not available)",
    },
  },
});

// PATCH /entries/:id - Update entry
// Note: Using Hono's regex syntax `:id{.+}` to capture paths with slashes
const updateEntryRoute = createRoute({
  method: "patch",
  path: "/:id{.+}",
  tags: ["Entries"],
  summary: "Update entry",
  description: "Updates an existing entry's status, title, or appends content.",
  request: {
    params: z.object({
      id: EntryIdOrPathSchema.openapi({
        param: { name: "id", in: "path" },
        description: "Entry ID (8-char alphanumeric) or full path",
      }),
    }),
    body: {
      content: {
        "application/json": {
          schema: UpdateEntryRequestSchema,
        },
      },
    },
  },
  responses: {
    200: {
      content: { "application/json": { schema: BrainEntrySchema } },
      description: "Entry updated successfully",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Validation error",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Entry not found",
    },
  },
});

// DELETE /entries/:id - Delete entry
// Note: Using Hono's regex syntax `:id{.+}` to capture paths with slashes
const deleteEntryRoute = createRoute({
  method: "delete",
  path: "/:id{.+}",
  tags: ["Entries"],
  summary: "Delete entry",
  description: "Deletes an entry. Requires confirm=true query parameter.",
  request: {
    params: z.object({
      id: EntryIdOrPathSchema.openapi({
        param: { name: "id", in: "path" },
        description: "Entry ID (8-char alphanumeric) or full path",
      }),
    }),
    query: z.object({
      confirm: z.literal("true").openapi({
        description: "Must be 'true' to confirm deletion",
        example: "true",
      }),
    }),
  },
  responses: {
    200: {
      content: { "application/json": { schema: DeleteEntryResponseSchema } },
      description: "Entry deleted successfully",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Confirmation required or validation error",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Entry not found",
    },
  },
});

// POST /entries/:id/move - Move entry to a different project
// Note: Using Hono's regex syntax `:id{.+}` to capture paths with slashes
const moveEntryRoute = createRoute({
  method: "post",
  path: "/:id{.+}/move",
  tags: ["Entries"],
  summary: "Move entry to different project",
  description: "Moves an entry to a different project. Cannot move in_progress tasks.",
  request: {
    params: z.object({
      id: EntryIdOrPathSchema.openapi({
        param: { name: "id", in: "path" },
        description: "Entry ID (8-char alphanumeric)",
      }),
    }),
    body: {
      content: {
        "application/json": {
          schema: MoveEntryRequestSchema,
        },
      },
    },
  },
  responses: {
    200: {
      content: { "application/json": { schema: MoveEntryResponseSchema } },
      description: "Entry moved successfully",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Validation error (e.g., same project, in_progress task)",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Entry not found",
    },
    409: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Conflict (e.g., ID collision in target project)",
    },
  },
});

// =============================================================================
// Entry Routes Factory
// =============================================================================

type EntryRouteOptions = {
  realtimeHub?: ProjectRealtimeHub;
};

function projectIdFromPath(path: string): string | null {
  const match = path.match(/^projects\/([^/]+)\//);
  return match ? match[1] : null;
}

function isTaskPath(path: string): boolean {
  return /^projects\/[^/]+\/task\//.test(path);
}

async function publishTaskSnapshot(realtimeHub: ProjectRealtimeHub, projectId: string): Promise<void> {
  const taskService = getTaskService();

  try {
    const snapshot = await taskService.getTasksWithDependencies(projectId);
    realtimeHub.publish(projectId, {
      event: "tasks_snapshot",
      payload: {
        type: "tasks_snapshot",
        transport: "sse",
        timestamp: new Date().toISOString(),
        projectId,
        tasks: snapshot.tasks,
        count: snapshot.tasks.length,
        stats: snapshot.stats,
        cycles: snapshot.cycles,
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : "Failed to load task snapshot";
    realtimeHub.publish(projectId, {
      event: "error",
      payload: {
        type: "error",
        transport: "sse",
        timestamp: new Date().toISOString(),
        projectId,
        message,
      },
    });
  }
}

export function createEntriesRoutes(options?: EntryRouteOptions): OpenAPIHono {
  const realtimeHub = options?.realtimeHub ?? createProjectRealtimeHub();

  const entries = new OpenAPIHono({
    defaultHook: (result, c) => {
      if (!result.success) {
        const errors = result.error.issues.map((issue) => ({
          field: issue.path.join("."),
          message: issue.message,
        }));

        // Check for specific error cases to provide better error messages
        const hasConfirmError = errors.some((e) => e.field === "confirm");
        if (hasConfirmError) {
          return c.json(
            {
              error: "Confirmation Required",
              message: "Delete requires confirm=true query parameter",
            },
            400
          );
        }

        // Build a message that includes the failing field names
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

  // Global error handler for JSON parsing errors
  entries.onError((err, c) => {
    // Hono throws HTTPException for malformed JSON
    if (err instanceof HTTPException && err.message.includes("Malformed JSON")) {
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

  // POST /entries - Create a new entry
  entries.openapi(createEntryRoute, async (c) => {
    const body = c.req.valid("json");
    const service = getBrainService();
    
    try {
      const result = await service.save(body);
      const projectId = projectIdFromPath(result.path);
      if (projectId) {
        publishProjectDirty(realtimeHub, projectId);
      }
      if (body.type === "task" && body.project) {
        await publishTaskSnapshot(realtimeHub, body.project);
      }
      return c.json(result, 201);
    } catch (error) {
      if (error instanceof DependencyValidationError) {
        return c.json(
          {
            error: "Validation Error",
            message: "Invalid task dependencies",
            details: error.errors.map((e) => ({ field: "depends_on", message: e })),
          },
          400
        );
      }
      throw error;
    }
  });

  // GET /entries/{id} - Get entry by ID or path
  entries.openapi(getEntryRoute, async (c) => {
    const { id } = c.req.valid("param");

    try {
      const service = getBrainService();
      const entry = await service.recall(id);

      // Get backlinks for the entry
      let backlinks: BrainEntry[] = [];
      try {
        backlinks = await service.getBacklinks(entry.path);
      } catch {
        // Backlinks may fail if zk is not available, that's ok
      }

      return c.json(
        {
          ...entry,
          backlinks: backlinks.map((b) => ({
            id: b.id,
            path: b.path,
            title: b.title,
            type: b.type,
          })),
        },
        200
      );
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("No entry found")) {
          return c.json(
            {
              error: "Not Found",
              message: `Entry not found: ${id}`,
            },
            404
          );
        }
        if (error.message.includes("No exact match")) {
          return c.json(
            {
              error: "Not Found",
              message: error.message,
            },
            404
          );
        }
      }
      throw error;
    }
  });

  // GET /entries - List entries with filters
  entries.openapi(listEntriesRoute, async (c) => {
    const query = c.req.valid("query");

    const request = {
      type: query.type,
      status: query.status,
      feature_id: query.feature_id,
      filename: query.filename,
      tags: query.tags ? query.tags.split(",").map((t: string) => t.trim()) : undefined,
      limit: query.limit,
      offset: query.offset,
      global: query.global,
      sortBy: query.sortBy,
    };

    try {
      const service = getBrainService();
      const result = await service.list(request);

      return c.json(result, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
        return c.json(
          {
            error: "Service Unavailable",
            message: "zk CLI is required for listing entries",
          },
          503
        );
      }
      throw error;
    }
  });

  // PATCH /entries/:id - Update entry
  entries.openapi(updateEntryRoute, async (c) => {
    const { id } = c.req.valid("param");
    const body = c.req.valid("json");

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

      const result = await service.update(entryPath, body);
      const projectId = projectIdFromPath(entryPath);

      if (projectId) {
        publishProjectDirty(realtimeHub, projectId);
      }

      if (result.type === "task") {
        if (projectId) {
          await publishTaskSnapshot(realtimeHub, projectId);
        }
      }

      return c.json(result, 200);
    } catch (error) {
      if (error instanceof DependencyValidationError) {
        return c.json(
          {
            error: "Validation Error",
            message: "Invalid task dependencies",
            details: error.errors.map((e) => ({ field: "depends_on", message: e })),
          },
          400
        );
      }
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
        if (error.message.includes("No updates specified")) {
          return c.json(
            {
              error: "Validation Error",
              message: error.message,
            },
            400
          );
        }
      }
      throw error;
    }
  });

  // DELETE /entries/{id} - Delete entry
  entries.openapi(deleteEntryRoute, async (c) => {
    const { id } = c.req.valid("param");
    // confirm is validated by the schema - if we get here, it's "true"

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

      await service.delete(entryPath);
      const projectId = projectIdFromPath(entryPath);

      if (projectId) {
        publishProjectDirty(realtimeHub, projectId);
      }

      if (isTaskPath(entryPath)) {
        if (projectId) {
          await publishTaskSnapshot(realtimeHub, projectId);
        }
      }

      return c.json(
        {
          message: "Entry deleted successfully",
          path: entryPath,
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

  // POST /entries/:id/move - Move entry to different project
  // OpenAPI route handles short 8-char IDs (e.g., POST /entries/abc12def/move)
  entries.openapi(moveEntryRoute, async (c) => {
    const { id } = c.req.valid("param");
    const body = c.req.valid("json");
    return handleMoveEntry(c, id, body.project, realtimeHub);
  });

  // Multi-segment wildcard patterns for full-path IDs (e.g., POST /entries/projects/x/task/y.md/move)
  // Hono's /:id{.+}/move doesn't work because .+ greedily captures "/move" as part of the ID.
  // Same approach as graph.ts uses for /backlinks, /outlinks, /related.
  const MOVE_PATTERNS = [
    "/*/move",
    "/*/*/move",
    "/*/*/*/move",
    "/*/*/*/*/move",
    "/*/*/*/*/*/move",
    "/*/*/*/*/*/*/move",
  ];
  entries.on("POST", MOVE_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/move");
    // Parse JSON body manually since we're not using openapi validation here
    const body = await c.req.json<{ project: string }>();
    if (!body.project) {
      return c.json(
        { error: "Validation Error", message: "Missing required field: project" },
        400
      );
    }
    return handleMoveEntry(c, id, body.project, realtimeHub);
  });

  return entries;
}

// =============================================================================
// Handler Functions
// =============================================================================

/**
 * Extract ID from request path by removing the route prefix and suffix.
 * Handles both production paths (/api/v1/entries/...) and test paths (/entries/...).
 * e.g., "/api/v1/entries/projects/brain-api/task/abc12def.md/move" -> "projects/brain-api/task/abc12def.md"
 * e.g., "/entries/projects/brain-api/task/abc12def.md/move" -> "projects/brain-api/task/abc12def.md"
 */
function extractIdFromPath(fullPath: string, suffix: string): string {
  let path = fullPath;
  // Remove the suffix first (e.g., "/move")
  if (path.endsWith(suffix)) {
    path = path.slice(0, -suffix.length);
  }
  // Remove any /entries/ prefix (handles /api/v1/entries/ and /entries/)
  const entriesIdx = path.indexOf("/entries/");
  if (entriesIdx !== -1) {
    path = path.slice(entriesIdx + "/entries/".length);
  }
  return path;
}

/**
 * Handle move entry request - shared between OpenAPI route and wildcard routes.
 */
async function handleMoveEntry(
  c: any,
  id: string,
  project: string,
  realtimeHub: ProjectRealtimeHub
) {
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

    const sourceProjectId = projectIdFromPath(entryPath);
    const result = await service.moveEntry(entryPath, project);

    if (isTaskPath(result.oldPath)) {
      const targetProjectId = projectIdFromPath(result.newPath);

      if (sourceProjectId) {
        publishProjectDirty(realtimeHub, sourceProjectId);
        await publishTaskSnapshot(realtimeHub, sourceProjectId);
      }

      if (targetProjectId) {
        publishProjectDirty(realtimeHub, targetProjectId);
        await publishTaskSnapshot(realtimeHub, targetProjectId);
      }
    } else {
      const targetProjectId = projectIdFromPath(result.newPath);

      if (sourceProjectId) {
        publishProjectDirty(realtimeHub, sourceProjectId);
      }

      if (targetProjectId) {
        publishProjectDirty(realtimeHub, targetProjectId);
      }
    }

    return c.json(result, 200);
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
      if (error.message.includes("already in project")) {
        return c.json(
          {
            error: "Validation Error",
            message: error.message,
          },
          400
        );
      }
      if (error.message.includes("Cannot move in_progress")) {
        return c.json(
          {
            error: "Validation Error",
            message: error.message,
          },
          400
        );
      }
      if (error.message.includes("already exists in project")) {
        return c.json(
          {
            error: "Conflict",
            message: error.message,
          },
          409
        );
      }
    }
    throw error;
  }
}
