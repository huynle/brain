/**
 * Brain API - Entry CRUD Endpoints
 *
 * REST API endpoints for entry operations using the BrainService.
 */

import { Hono } from "hono";
import { getBrainService } from "../core/brain-service";
import type {
  CreateEntryRequest,
  UpdateEntryRequest,
  ListEntriesRequest,
  EntryType,
  EntryStatus,
  BrainEntry,
} from "../core/types";
import { ENTRY_TYPES, ENTRY_STATUSES, PRIORITIES } from "../core/types";

// =============================================================================
// Validation Helpers
// =============================================================================

interface ValidationError {
  field: string;
  message: string;
}

function validateCreateRequest(body: unknown): {
  valid: boolean;
  errors: ValidationError[];
  data?: CreateEntryRequest;
} {
  const errors: ValidationError[] = [];

  if (!body || typeof body !== "object") {
    return { valid: false, errors: [{ field: "body", message: "Request body is required" }] };
  }

  const req = body as Record<string, unknown>;

  // Required fields
  if (!req.type || typeof req.type !== "string") {
    errors.push({ field: "type", message: "type is required and must be a string" });
  } else if (!ENTRY_TYPES.includes(req.type as EntryType)) {
    errors.push({
      field: "type",
      message: `type must be one of: ${ENTRY_TYPES.join(", ")}`,
    });
  }

  if (!req.title || typeof req.title !== "string") {
    errors.push({ field: "title", message: "title is required and must be a string" });
  } else if (req.title.length === 0) {
    errors.push({ field: "title", message: "title cannot be empty" });
  }

  if (!req.content || typeof req.content !== "string") {
    errors.push({ field: "content", message: "content is required and must be a string" });
  }

  // Optional fields
  if (req.tags !== undefined) {
    if (!Array.isArray(req.tags)) {
      errors.push({ field: "tags", message: "tags must be an array" });
    } else if (!req.tags.every((t) => typeof t === "string")) {
      errors.push({ field: "tags", message: "all tags must be strings" });
    }
  }

  if (req.status !== undefined) {
    if (typeof req.status !== "string" || !ENTRY_STATUSES.includes(req.status as EntryStatus)) {
      errors.push({
        field: "status",
        message: `status must be one of: ${ENTRY_STATUSES.join(", ")}`,
      });
    }
  }

  if (req.priority !== undefined) {
    if (typeof req.priority !== "string" || !PRIORITIES.includes(req.priority as "high" | "medium" | "low")) {
      errors.push({
        field: "priority",
        message: `priority must be one of: ${PRIORITIES.join(", ")}`,
      });
    }
  }

  if (req.global !== undefined && typeof req.global !== "boolean") {
    errors.push({ field: "global", message: "global must be a boolean" });
  }

  if (req.project !== undefined && typeof req.project !== "string") {
    errors.push({ field: "project", message: "project must be a string" });
  }

  if (req.relatedEntries !== undefined) {
    if (!Array.isArray(req.relatedEntries)) {
      errors.push({ field: "relatedEntries", message: "relatedEntries must be an array" });
    } else if (!req.relatedEntries.every((e) => typeof e === "string")) {
      errors.push({ field: "relatedEntries", message: "all relatedEntries must be strings" });
    }
  }

  if (req.depends_on !== undefined) {
    if (!Array.isArray(req.depends_on)) {
      errors.push({ field: "depends_on", message: "depends_on must be an array" });
    } else if (!req.depends_on.every((d) => typeof d === "string")) {
      errors.push({ field: "depends_on", message: "all depends_on must be strings" });
    }
  }

  if (req.parent_id !== undefined) {
    if (typeof req.parent_id !== "string") {
      errors.push({ field: "parent_id", message: "parent_id must be a string" });
    } else if (!/^[a-z0-9]{8}$/.test(req.parent_id)) {
      errors.push({ field: "parent_id", message: "parent_id must be an 8-character alphanumeric ID" });
    }
  }

  // Validation for execution context fields
  if (req.workdir !== undefined && typeof req.workdir !== "string") {
    errors.push({ field: "workdir", message: "workdir must be a string" });
  }
  if (req.worktree !== undefined && typeof req.worktree !== "string") {
    errors.push({ field: "worktree", message: "worktree must be a string" });
  }
  if (req.git_remote !== undefined && typeof req.git_remote !== "string") {
    errors.push({ field: "git_remote", message: "git_remote must be a string" });
  }
  if (req.git_branch !== undefined && typeof req.git_branch !== "string") {
    errors.push({ field: "git_branch", message: "git_branch must be a string" });
  }

  if (errors.length > 0) {
    return { valid: false, errors };
  }

  return {
    valid: true,
    errors: [],
    data: {
      type: req.type as EntryType,
      title: req.title as string,
      content: req.content as string,
      tags: req.tags as string[] | undefined,
      status: req.status as EntryStatus | undefined,
      priority: req.priority as "high" | "medium" | "low" | undefined,
      global: req.global as boolean | undefined,
      project: req.project as string | undefined,
      relatedEntries: req.relatedEntries as string[] | undefined,
      depends_on: req.depends_on as string[] | undefined,
      parent_id: req.parent_id as string | undefined,
      // Execution context for tasks
      workdir: req.workdir as string | undefined,
      worktree: req.worktree as string | undefined,
      git_remote: req.git_remote as string | undefined,
      git_branch: req.git_branch as string | undefined,
    },
  };
}

function validateUpdateRequest(body: unknown): {
  valid: boolean;
  errors: ValidationError[];
  data?: UpdateEntryRequest;
} {
  const errors: ValidationError[] = [];

  if (!body || typeof body !== "object") {
    return { valid: false, errors: [{ field: "body", message: "Request body is required" }] };
  }

  const req = body as Record<string, unknown>;

  // At least one field must be provided
  const hasStatus = req.status !== undefined;
  const hasTitle = req.title !== undefined;
  const hasAppend = req.append !== undefined;
  const hasNote = req.note !== undefined;

  if (!hasStatus && !hasTitle && !hasAppend && !hasNote) {
    errors.push({
      field: "body",
      message: "At least one of status, title, append, or note must be provided",
    });
  }

  if (req.status !== undefined) {
    if (typeof req.status !== "string" || !ENTRY_STATUSES.includes(req.status as EntryStatus)) {
      errors.push({
        field: "status",
        message: `status must be one of: ${ENTRY_STATUSES.join(", ")}`,
      });
    }
  }

  if (req.title !== undefined && typeof req.title !== "string") {
    errors.push({ field: "title", message: "title must be a string" });
  }

  if (req.append !== undefined && typeof req.append !== "string") {
    errors.push({ field: "append", message: "append must be a string" });
  }

  if (req.note !== undefined && typeof req.note !== "string") {
    errors.push({ field: "note", message: "note must be a string" });
  }

  if (errors.length > 0) {
    return { valid: false, errors };
  }

  return {
    valid: true,
    errors: [],
    data: {
      status: req.status as EntryStatus | undefined,
      title: req.title as string | undefined,
      append: req.append as string | undefined,
      note: req.note as string | undefined,
    },
  };
}

// =============================================================================
// Entry Routes
// =============================================================================

export function createEntriesRoutes(): Hono {
  const entries = new Hono();

  // POST /entries - Create a new entry
  entries.post("/", async (c) => {
    try {
      const body = await c.req.json();
      const validation = validateCreateRequest(body);

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
      const result = await service.save(validation.data!);

      return c.json(result, 201);
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
      throw error;
    }
  });

  // GET /entries/:id - Get entry by ID or path
  entries.get("/:id{.+}", async (c) => {
    const id = c.req.param("id");

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

      return c.json({
        ...entry,
        backlinks: backlinks.map((b) => ({
          id: b.id,
          path: b.path,
          title: b.title,
          type: b.type,
        })),
      });
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
  entries.get("/", async (c) => {
    const query = c.req.query();

    const request: ListEntriesRequest = {
      type: query.type as EntryType | undefined,
      status: query.status as EntryStatus | undefined,
      filename: query.filename,
      parent_id: query.parent_id,
      limit: query.limit ? parseInt(query.limit, 10) : undefined,
      offset: query.offset ? parseInt(query.offset, 10) : undefined,
      global: query.global === "true",
      sortBy: query.sortBy as "created" | "modified" | "priority" | undefined,
    };

    // Validate type if provided
    if (request.type && !ENTRY_TYPES.includes(request.type)) {
      return c.json(
        {
          error: "Validation Error",
          message: `Invalid type. Must be one of: ${ENTRY_TYPES.join(", ")}`,
        },
        400
      );
    }

    // Validate status if provided
    if (request.status && !ENTRY_STATUSES.includes(request.status)) {
      return c.json(
        {
          error: "Validation Error",
          message: `Invalid status. Must be one of: ${ENTRY_STATUSES.join(", ")}`,
        },
        400
      );
    }

    // Validate sortBy if provided
    if (request.sortBy && !["created", "modified", "priority"].includes(request.sortBy)) {
      return c.json(
        {
          error: "Validation Error",
          message: "Invalid sortBy. Must be one of: created, modified, priority",
        },
        400
      );
    }

    // Validate parent_id if provided
    if (request.parent_id && !/^[a-z0-9]{8}$/.test(request.parent_id)) {
      return c.json(
        {
          error: "Validation Error",
          message: "Invalid parent_id. Must be an 8-character alphanumeric ID",
        },
        400
      );
    }

    try {
      const service = getBrainService();
      const result = await service.list(request);

      return c.json(result);
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
  entries.patch("/:id{.+}", async (c) => {
    const id = c.req.param("id");

    try {
      const body = await c.req.json();
      const validation = validateUpdateRequest(body);

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

      const result = await service.update(entryPath, validation.data!);

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

  // DELETE /entries/:id - Delete entry
  entries.delete("/:id{.+}", async (c) => {
    const id = c.req.param("id");
    const confirm = c.req.query("confirm");

    if (confirm !== "true") {
      return c.json(
        {
          error: "Confirmation Required",
          message: "Delete requires confirm=true query parameter",
        },
        400
      );
    }

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

      return c.json({
        message: "Entry deleted successfully",
        path: entryPath,
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

  return entries;
}
