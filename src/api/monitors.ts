/**
 * Monitor API Routes
 *
 * REST endpoints for managing monitor tasks from templates.
 * Allows any client (TUI, MCP tools, CLI, curl) to list templates,
 * create monitors, toggle them on/off, and delete them.
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import {
  getMonitorService,
  MonitorConflictError,
} from "../core/monitor-service";
import { MONITOR_TEMPLATE_LIST } from "../core/monitor-templates";
import { ErrorResponseSchema, NotFoundResponseSchema } from "./schemas";
import {
  createProjectRealtimeHub,
  type ProjectRealtimeHub,
} from "../core/realtime-hub";
import { getTaskService } from "../core/task-service";

// =============================================================================
// Schemas
// =============================================================================

const MonitorScopeSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("all") }),
  z.object({ type: z.literal("project"), project: z.string().min(1) }),
  z.object({
    type: z.literal("feature"),
    feature_id: z.string().min(1),
    project: z.string().min(1),
  }),
]).openapi("MonitorScope");

const MonitorTemplateSchema = z.object({
  id: z.string(),
  label: z.string(),
  description: z.string(),
  defaultSchedule: z.string(),
  tags: z.array(z.string()),
}).openapi("MonitorTemplate");

const MonitorTemplateListResponseSchema = z.object({
  templates: z.array(MonitorTemplateSchema),
  count: z.number(),
}).openapi("MonitorTemplateListResponse");

const CreateMonitorRequestSchema = z.object({
  templateId: z.string().min(1).openapi({ example: "blocked-inspector" }),
  scope: MonitorScopeSchema,
  schedule: z.string().optional().openapi({
    description: "Override default schedule (cron expression)",
    example: "*/30 * * * *",
  }),
  project: z.string().optional().openapi({
    description: "Project to store the task in (for 'all' scope)",
  }),
  status: z.string().optional().openapi({
    description: "Initial status for the created task (defaults to 'pending')",
    example: "draft",
  }),
}).openapi("CreateMonitorRequest");

const CreateMonitorResponseSchema = z.object({
  id: z.string(),
  path: z.string(),
  title: z.string(),
}).openapi("CreateMonitorResponse");

const MonitorConflictResponseSchema = z.object({
  error: z.literal("Conflict"),
  message: z.string(),
  existingId: z.string(),
  existingPath: z.string(),
}).openapi("MonitorConflictResponse");

const MonitorInfoSchema = z.object({
  id: z.string(),
  path: z.string(),
  templateId: z.string(),
  scope: MonitorScopeSchema,
  enabled: z.boolean(),
  schedule: z.string(),
  title: z.string(),
}).openapi("MonitorInfo");

const MonitorListResponseSchema = z.object({
  monitors: z.array(MonitorInfoSchema),
  count: z.number(),
}).openapi("MonitorListResponse");

const ToggleMonitorRequestSchema = z.object({
  enabled: z.boolean().openapi({ example: true }),
}).openapi("ToggleMonitorRequest");

const ToggleMonitorResponseSchema = z.object({
  message: z.string(),
  taskId: z.string(),
  enabled: z.boolean(),
}).openapi("ToggleMonitorResponse");

const DeleteMonitorResponseSchema = z.object({
  message: z.string(),
  taskId: z.string(),
}).openapi("DeleteMonitorResponse");

// =============================================================================
// Route Definitions
// =============================================================================

const listTemplatesRoute = createRoute({
  method: "get",
  path: "/templates",
  tags: ["Monitors"],
  summary: "List available monitor templates",
  responses: {
    200: {
      content: {
        "application/json": { schema: MonitorTemplateListResponseSchema },
      },
      description: "List of available monitor templates",
    },
  },
});

const listMonitorsRoute = createRoute({
  method: "get",
  path: "/",
  tags: ["Monitors"],
  summary: "List active monitors",
  request: {
    query: z.object({
      project: z.string().optional().openapi({
        description: "Filter by project",
      }),
      feature_id: z.string().optional().openapi({
        description: "Filter by feature ID",
      }),
      template_id: z.string().optional().openapi({
        description: "Filter by template ID",
      }),
    }),
  },
  responses: {
    200: {
      content: {
        "application/json": { schema: MonitorListResponseSchema },
      },
      description: "List of active monitors",
    },
  },
});

const createMonitorRoute = createRoute({
  method: "post",
  path: "/",
  tags: ["Monitors"],
  summary: "Create a monitor from a template",
  request: {
    body: {
      content: {
        "application/json": { schema: CreateMonitorRequestSchema },
      },
    },
  },
  responses: {
    201: {
      content: {
        "application/json": { schema: CreateMonitorResponseSchema },
      },
      description: "Monitor created successfully",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Invalid request",
    },
    409: {
      content: {
        "application/json": { schema: MonitorConflictResponseSchema },
      },
      description: "Monitor already exists for this template+scope",
    },
  },
});

const TaskIdParamSchema = z.object({
  taskId: z.string().min(1).openapi({
    param: { name: "taskId", in: "path" },
    example: "abc12def",
  }),
});

const toggleMonitorRoute = createRoute({
  method: "patch",
  path: "/{taskId}/toggle",
  tags: ["Monitors"],
  summary: "Toggle a monitor on/off",
  request: {
    params: TaskIdParamSchema,
    body: {
      content: {
        "application/json": { schema: ToggleMonitorRequestSchema },
      },
    },
  },
  responses: {
    200: {
      content: {
        "application/json": { schema: ToggleMonitorResponseSchema },
      },
      description: "Monitor toggled successfully",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Monitor not found",
    },
  },
});

const deleteMonitorRoute = createRoute({
  method: "delete",
  path: "/{taskId}",
  tags: ["Monitors"],
  summary: "Delete a monitor by task ID",
  request: {
    params: TaskIdParamSchema,
  },
  responses: {
    200: {
      content: {
        "application/json": { schema: DeleteMonitorResponseSchema },
      },
      description: "Monitor deleted successfully",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Monitor not found",
    },
  },
});

const DeleteByScopeRequestSchema = z.object({
  templateId: z.string().min(1).openapi({ example: "feature-review" }),
  scope: MonitorScopeSchema,
}).openapi("DeleteByScopeRequest");

const DeleteByScopeResponseSchema = z.object({
  message: z.string(),
  taskId: z.string(),
  path: z.string(),
}).openapi("DeleteByScopeResponse");

const deleteByScopeRoute = createRoute({
  method: "delete",
  path: "/by-scope",
  tags: ["Monitors"],
  summary: "Delete a monitor by templateId + scope",
  description: "Convenience endpoint: finds the monitor matching the templateId+scope combo and deletes it in one call. No task ID needed.",
  request: {
    body: {
      content: {
        "application/json": { schema: DeleteByScopeRequestSchema },
      },
    },
  },
  responses: {
    200: {
      content: {
        "application/json": { schema: DeleteByScopeResponseSchema },
      },
      description: "Monitor found and deleted",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "No monitor found for this templateId+scope",
    },
  },
});

// =============================================================================
// Route Factory
// =============================================================================

type MonitorRouteOptions = {
  realtimeHub?: ProjectRealtimeHub;
};

async function publishTaskSnapshot(
  realtimeHub: ProjectRealtimeHub,
  projectId: string,
): Promise<void> {
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
  } catch {
    // Best-effort — don't fail the mutation if snapshot publishing fails
  }
}

/** Extract project ID from an entry path like "projects/brain-api/task/abc.md" */
function extractProjectFromPath(entryPath: string): string | null {
  const match = entryPath.match(/^projects\/([^/]+)\//);
  return match ? match[1] : null;
}

export function createMonitorRoutes(options?: MonitorRouteOptions): OpenAPIHono {
  const realtimeHub = options?.realtimeHub ?? createProjectRealtimeHub();

  const monitors = new OpenAPIHono({
    defaultHook: (result, c) => {
      if (!result.success) {
        const errors = result.error.issues.map((issue) => ({
          field: issue.path.join("."),
          message: issue.message,
        }));
        return c.json(
          {
            error: "Validation Error",
            message: `Invalid request: ${errors
              .map((e) => e.field)
              .filter(Boolean)
              .join(", ")}`,
            details: errors,
          },
          400,
        );
      }
    },
  });

  // GET /templates
  monitors.openapi(listTemplatesRoute, async (c) => {
    const templates = MONITOR_TEMPLATE_LIST.map((t) => ({
      id: t.id,
      label: t.label,
      description: t.description,
      defaultSchedule: t.defaultSchedule,
      tags: t.tags,
    }));
    return c.json({ templates, count: templates.length }, 200);
  });

  // GET /
  monitors.openapi(listMonitorsRoute, async (c) => {
    const query = c.req.valid("query");
    const service = getMonitorService();
    const monitorList = await service.list({
      project: query.project,
      feature_id: query.feature_id,
      templateId: query.template_id,
    });
    return c.json({ monitors: monitorList, count: monitorList.length }, 200);
  });

  // POST /
  monitors.openapi(createMonitorRoute, async (c) => {
    const body = c.req.valid("json");
    const service = getMonitorService();

    try {
      // Feature-review template with feature scope uses createForFeature()
      // (one-shot with depends_on, not scheduled)
      let result;
      if (body.templateId === "feature-review" && body.scope.type === "feature") {
        result = await service.createForFeature(
          body.templateId,
          body.scope,
          body.scope.project,
          { status: body.status as import("../core/types").EntryStatus },
        );
      } else {
        result = await service.create(body.templateId, body.scope, {
          schedule: body.schedule,
          project: body.project,
        });
      }
      // Publish SSE snapshot so connected TUI clients see the new task
      const projectId = extractProjectFromPath(result.path);
      if (projectId) {
        await publishTaskSnapshot(realtimeHub, projectId);
      }

      return c.json(result, 201);
    } catch (error) {
      if (error instanceof MonitorConflictError) {
        return c.json(
          {
            error: "Conflict" as const,
            message: error.message,
            existingId: error.existingId,
            existingPath: error.existingPath,
          },
          409,
        );
      }
      if (
        error instanceof Error &&
        error.message.startsWith("Unknown monitor template")
      ) {
        return c.json(
          {
            error: "Validation Error",
            message: error.message,
          },
          400,
        );
      }
      throw error;
    }
  });

  // PATCH /:taskId/toggle
  monitors.openapi(toggleMonitorRoute, async (c) => {
    const { taskId } = c.req.valid("param");
    const { enabled } = c.req.valid("json");
    const service = getMonitorService();

    try {
      const { path: entryPath } = await service.toggle(taskId, enabled);
      // Publish SSE snapshot so connected TUI clients see the updated state
      const projectId = extractProjectFromPath(entryPath);
      if (projectId) {
        await publishTaskSnapshot(realtimeHub, projectId);
      }
      return c.json(
        {
          message: `Monitor ${enabled ? "enabled" : "disabled"}`,
          taskId,
          enabled,
        },
        200,
      );
    } catch (error) {
      if (
        error instanceof Error &&
        (error.message.includes("No entry found") ||
          error.message.includes("Entry not found"))
      ) {
        return c.json(
          { error: "Not Found", message: `Monitor not found: ${taskId}` },
          404,
        );
      }
      throw error;
    }
  });

  // DELETE /by-scope — find+delete by templateId+scope (convenience endpoint)
  monitors.openapi(deleteByScopeRoute, async (c) => {
    const { templateId, scope } = c.req.valid("json");
    const service = getMonitorService();

    const result = await service.deleteByScope(templateId, scope);
    if (!result) {
      return c.json(
        {
          error: "Not Found",
          message: `No monitor found for template "${templateId}" with scope ${JSON.stringify(scope)}`,
        },
        404,
      );
    }

    // Publish SSE snapshot so connected TUI clients see the task removed
    const projectId = extractProjectFromPath(result.path);
    if (projectId) {
      await publishTaskSnapshot(realtimeHub, projectId);
    }

    return c.json(
      {
        message: "Monitor deleted successfully",
        taskId: result.id,
        path: result.path,
      },
      200,
    );
  });

  // DELETE /:taskId
  monitors.openapi(deleteMonitorRoute, async (c) => {
    const { taskId } = c.req.valid("param");
    const service = getMonitorService();

    try {
      const { path: entryPath } = await service.delete(taskId);
      // Publish SSE snapshot so connected TUI clients see the task removed
      const projectId = extractProjectFromPath(entryPath);
      if (projectId) {
        await publishTaskSnapshot(realtimeHub, projectId);
      }
      return c.json(
        { message: "Monitor deleted successfully", taskId },
        200,
      );
    } catch (error) {
      if (
        error instanceof Error &&
        (error.message.includes("No entry found") ||
          error.message.includes("Entry not found"))
      ) {
        return c.json(
          { error: "Not Found", message: `Monitor not found: ${taskId}` },
          404,
        );
      }
      throw error;
    }
  });

  return monitors;
}
