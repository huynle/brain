import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { parseDate } from "chrono-node";
import { getBrainService } from "../core/brain-service";
import {
  resolveCronPipeline,
  canTriggerPipeline,
  canRunWithinBounds,
  generateRunId,
} from "../core/cron-service";
import { getTaskService } from "../core/task-service";
import {
  createProjectRealtimeHub,
  publishProjectDirty,
  type ProjectRealtimeHub,
} from "../core/realtime-hub";
import type { BrainEntry, CronRun, ResolvedTask } from "../core/types";
import {
  EntryIdSchema,
  TaskIdSchema,
  ProjectIdSchema,
  CreateCronRequestSchema,
  UpdateCronRequestSchema,
  CronMutationResponseSchema,
  DeleteCronQuerySchema,
  DeleteCronResponseSchema,
  CronListResponseSchema,
  CronDetailResponseSchema,
  CronTriggerResponseSchema,
  CronRunsResponseSchema,
  CronLinkedTasksRequestSchema,
  CronLinkedTasksResponseSchema,
  CronLinkedTasksMutationResponseSchema,
  ErrorResponseSchema,
  NotFoundResponseSchema,
  ServiceUnavailableResponseSchema,
} from "./schemas";

const ProjectIdParamSchema = z.object({
  projectId: ProjectIdSchema.openapi({
    param: { name: "projectId", in: "path" },
    example: "my-project",
  }),
});

const CronIdParamSchema = z.object({
  projectId: ProjectIdSchema.openapi({
    param: { name: "projectId", in: "path" },
    example: "my-project",
  }),
  cronId: EntryIdSchema.openapi({
    param: { name: "cronId", in: "path" },
    example: "abc12def",
  }),
});

const CronTaskIdParamSchema = z.object({
  projectId: ProjectIdSchema.openapi({
    param: { name: "projectId", in: "path" },
    example: "my-project",
  }),
  cronId: EntryIdSchema.openapi({
    param: { name: "cronId", in: "path" },
    example: "abc12def",
  }),
  taskId: TaskIdSchema.openapi({
    param: { name: "taskId", in: "path" },
    example: "abc12def",
  }),
});

const listProjectCronsRoute = createRoute({
  method: "get",
  path: "/{projectId}/crons",
  tags: ["Crons"],
  summary: "List cron entries for a project",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Project cron entries",
      content: {
        "application/json": {
          schema: CronListResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const getProjectCronRoute = createRoute({
  method: "get",
  path: "/{projectId}/crons/{cronId}",
  tags: ["Crons"],
  summary: "Get a cron entry with pipeline tasks",
  request: {
    params: CronIdParamSchema,
  },
  responses: {
    200: {
      description: "Cron entry details and pipeline",
      content: {
        "application/json": {
          schema: CronDetailResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const triggerCronRoute = createRoute({
  method: "post",
  path: "/{projectId}/crons/{cronId}/trigger",
  tags: ["Crons"],
  summary: "Manually trigger a cron run",
  request: {
    params: CronIdParamSchema,
  },
  responses: {
    200: {
      description: "Cron trigger accepted",
      content: {
        "application/json": {
          schema: CronTriggerResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    409: {
      description: "Cron cannot be triggered",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const getCronRunsRoute = createRoute({
  method: "get",
  path: "/{projectId}/crons/{cronId}/runs",
  tags: ["Crons"],
  summary: "Get cron run history",
  request: {
    params: CronIdParamSchema,
  },
  responses: {
    200: {
      description: "Cron run history",
      content: {
        "application/json": {
          schema: CronRunsResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const listCronLinkedTasksRoute = createRoute({
  method: "get",
  path: "/{projectId}/crons/{cronId}/linked-tasks",
  tags: ["Crons"],
  summary: "List tasks linked to a cron",
  request: {
    params: CronIdParamSchema,
  },
  responses: {
    200: {
      description: "Linked tasks",
      content: {
        "application/json": {
          schema: CronLinkedTasksResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const setCronLinkedTasksRoute = createRoute({
  method: "patch",
  path: "/{projectId}/crons/{cronId}/linked-tasks",
  tags: ["Crons"],
  summary: "Replace cron linked tasks",
  request: {
    params: CronIdParamSchema,
    body: {
      content: {
        "application/json": {
          schema: CronLinkedTasksRequestSchema,
        },
      },
    },
  },
  responses: {
    200: {
      description: "Cron linked tasks updated",
      content: {
        "application/json": {
          schema: CronLinkedTasksMutationResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry or task not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const addCronLinkedTaskRoute = createRoute({
  method: "post",
  path: "/{projectId}/crons/{cronId}/linked-tasks/{taskId}",
  tags: ["Crons"],
  summary: "Add a task link to a cron",
  request: {
    params: CronTaskIdParamSchema,
  },
  responses: {
    200: {
      description: "Task linked to cron",
      content: {
        "application/json": {
          schema: CronLinkedTasksMutationResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry or task not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const removeCronLinkedTaskRoute = createRoute({
  method: "delete",
  path: "/{projectId}/crons/{cronId}/linked-tasks/{taskId}",
  tags: ["Crons"],
  summary: "Remove a task link from a cron",
  request: {
    params: CronTaskIdParamSchema,
  },
  responses: {
    200: {
      description: "Task unlinked from cron",
      content: {
        "application/json": {
          schema: CronLinkedTasksMutationResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry or task not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const createProjectCronRoute = createRoute({
  method: "post",
  path: "/{projectId}/crons",
  tags: ["Crons"],
  summary: "Create a cron entry",
  request: {
    params: ProjectIdParamSchema,
    body: {
      content: {
        "application/json": {
          schema: CreateCronRequestSchema,
        },
      },
    },
  },
  responses: {
    201: {
      description: "Cron entry created",
      content: {
        "application/json": {
          schema: CronMutationResponseSchema,
        },
      },
    },
    400: {
      description: "Validation error",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const updateProjectCronRoute = createRoute({
  method: "patch",
  path: "/{projectId}/crons/{cronId}",
  tags: ["Crons"],
  summary: "Update a cron entry",
  request: {
    params: CronIdParamSchema,
    body: {
      content: {
        "application/json": {
          schema: UpdateCronRequestSchema,
        },
      },
    },
  },
  responses: {
    200: {
      description: "Cron entry updated",
      content: {
        "application/json": {
          schema: CronMutationResponseSchema,
        },
      },
    },
    400: {
      description: "Validation error",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

const deleteProjectCronRoute = createRoute({
  method: "delete",
  path: "/{projectId}/crons/{cronId}",
  tags: ["Crons"],
  summary: "Delete a cron entry",
  request: {
    params: CronIdParamSchema,
    query: DeleteCronQuerySchema,
  },
  responses: {
    200: {
      description: "Cron entry deleted",
      content: {
        "application/json": {
          schema: DeleteCronResponseSchema,
        },
      },
    },
    400: {
      description: "Validation error",
      content: {
        "application/json": {
          schema: ErrorResponseSchema,
        },
      },
    },
    404: {
      description: "Cron entry not found",
      content: {
        "application/json": {
          schema: NotFoundResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

function isZkUnavailableError(error: unknown): error is Error {
  return error instanceof Error && error.message.includes("zk CLI not available");
}

function isProjectCron(projectId: string, entry: BrainEntry): boolean {
  return entry.type === "cron" && entry.path.startsWith(`projects/${projectId}/cron/`);
}

async function findProjectCron(projectId: string, cronId: string): Promise<BrainEntry | null> {
  const brainService = getBrainService();

  try {
    const cron = await brainService.recall(cronId);
    if (!isProjectCron(projectId, cron)) return null;
    return cron;
  } catch {
    // Fallback to direct path lookup when zk index is stale.
  }

  try {
    const cronPath = `projects/${projectId}/cron/${cronId}.md`;
    const cron = await brainService.recall(cronPath);
    if (!isProjectCron(projectId, cron)) return null;
    return cron;
  } catch {
    return null;
  }
}

function sortRunsDesc(runs: CronRun[]): CronRun[] {
  return [...runs].sort((a, b) =>
    new Date(b.started).getTime() - new Date(a.started).getTime()
  );
}

function filterLinkedTasks(cronId: string, tasks: ResolvedTask[]) {
  return tasks.filter((task) => task.cron_ids.includes(cronId));
}

function linkedTasksPayload(cronId: string, tasks: ResolvedTask[]) {
  const linkedTasks = filterLinkedTasks(cronId, tasks);
  return {
    cronId,
    tasks: linkedTasks,
    count: linkedTasks.length,
  };
}

function parseLooseDatetimeToUtc(value: string | undefined, fieldName: string): string | undefined {
  if (value === undefined) return undefined;
  const parsed = parseDate(value);
  if (!parsed || Number.isNaN(parsed.getTime())) {
    throw new Error(`Invalid ${fieldName}: could not parse datetime '${value}'`);
  }
  return parsed.toISOString();
}

function validateBoundedFields(
  startsAtIso: string | undefined,
  expiresAtIso: string | undefined,
  runOnceAtIso: string | undefined
): void {
  if (startsAtIso && expiresAtIso) {
    const startsAt = new Date(startsAtIso).getTime();
    const expiresAt = new Date(expiresAtIso).getTime();
    if (startsAt > expiresAt) {
      throw new Error("Invalid bounds: starts_at must be before or equal to expires_at");
    }
  }

  if (runOnceAtIso && startsAtIso && new Date(runOnceAtIso).getTime() < new Date(startsAtIso).getTime()) {
    throw new Error("Invalid run_once_at: must be at or after starts_at");
  }

  if (runOnceAtIso && expiresAtIso && new Date(runOnceAtIso).getTime() > new Date(expiresAtIso).getTime()) {
    throw new Error("Invalid run_once_at: must be at or before expires_at");
  }
}

type CronRouteOptions = {
  realtimeHub?: ProjectRealtimeHub;
};

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

export function createCronRoutes(options?: CronRouteOptions): OpenAPIHono {
  const realtimeHub = options?.realtimeHub ?? createProjectRealtimeHub();

  const crons = new OpenAPIHono({
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

  crons.openapi(listProjectCronsRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const brainService = getBrainService();

    try {
      const result = await brainService.list({
        type: "cron",
        limit: 1000,
        offset: 0,
      });

      const cronSummaries = result.entries.filter((entry) => isProjectCron(projectId, entry));
      const cronEntries = await Promise.all(
        cronSummaries.map(async (entry) => {
          try {
            return await brainService.recall(entry.path);
          } catch {
            return null;
          }
        })
      );

      const projectCrons = cronEntries
        .filter((entry): entry is BrainEntry => entry !== null)
        .map((entry) => ({
          id: entry.id,
          path: entry.path,
          title: entry.title,
          status: entry.status,
          schedule: entry.schedule,
          next_run: entry.next_run,
          max_runs: entry.max_runs,
          starts_at: entry.starts_at,
          expires_at: entry.expires_at,
          runs: entry.runs,
          created: entry.created,
          modified: entry.modified,
        }));

      return c.json({
        crons: projectCrons,
        count: projectCrons.length,
      }, 200);
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(getProjectCronRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const taskResult = await taskService.getTasksWithDependencies(projectId);
      const pipeline = resolveCronPipeline(cron.id, taskResult.tasks);

      return c.json({
        cron,
        pipeline,
        pipelineCount: pipeline.length,
      }, 200);
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(createProjectCronRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const body = c.req.valid("json");
    const brainService = getBrainService();

    try {
      const startsAtIso = parseLooseDatetimeToUtc(body.starts_at, "starts_at");
      const expiresAtIso = parseLooseDatetimeToUtc(body.expires_at, "expires_at");
      const runOnceAtIso = parseLooseDatetimeToUtc(body.run_once_at, "run_once_at");
      validateBoundedFields(startsAtIso, expiresAtIso, runOnceAtIso);

      const created = await brainService.save({
        type: "cron",
        title: body.title,
        content: body.content || "Cron content.",
        project: projectId,
        schedule: body.schedule,
        next_run: runOnceAtIso,
        max_runs: body.max_runs ?? (runOnceAtIso ? 1 : undefined),
        starts_at: startsAtIso,
        expires_at: expiresAtIso,
        status: body.status,
        tags: body.tags,
      });

      const cron = await brainService.recall(created.path);
      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);
      return c.json(
        {
          cron,
          message: "Cron created successfully",
        },
        201
      );
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      if (error instanceof Error && error.message.startsWith("Invalid ")) {
        return c.json(
          {
            error: "Validation Error",
            message: error.message,
          },
          400
        );
      }
      throw error;
    }
  });

  crons.openapi(updateProjectCronRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");
    const body = c.req.valid("json");
    const brainService = getBrainService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const startsAtIso = parseLooseDatetimeToUtc(body.starts_at, "starts_at");
      const expiresAtIso = parseLooseDatetimeToUtc(body.expires_at, "expires_at");
      const runOnceAtIso = parseLooseDatetimeToUtc(body.run_once_at, "run_once_at");
      validateBoundedFields(
        startsAtIso !== undefined ? startsAtIso : cron.starts_at,
        expiresAtIso !== undefined ? expiresAtIso : cron.expires_at,
        runOnceAtIso
      );

      const updated = await brainService.update(cron.path, {
        title: body.title,
        schedule: body.schedule,
        next_run: runOnceAtIso,
        max_runs: body.max_runs,
        starts_at: startsAtIso,
        expires_at: expiresAtIso,
        status: body.status,
        tags: body.tags,
        content: body.content,
      });

      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);
      return c.json({
        cron: updated,
        message: "Cron updated successfully",
      }, 200);
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      if (error instanceof Error && error.message.startsWith("Invalid ")) {
        return c.json(
          {
            error: "Validation Error",
            message: error.message,
          },
          400
        );
      }
      throw error;
    }
  });

  crons.openapi(deleteProjectCronRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");
    const brainService = getBrainService();
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const taskResult = await taskService.getTasksWithDependencies(projectId);
      const linkedTasks = taskResult.tasks.filter((task) => task.cron_ids.includes(cron.id));

      for (const task of linkedTasks) {
        await brainService.update(task.path, {
          cron_ids: task.cron_ids.filter((id) => id !== cron.id),
        });
      }

      await brainService.delete(cron.path);
      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);

      return c.json(
        {
          message: "Cron deleted successfully",
          path: cron.path,
        },
        200
      );
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(triggerCronRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");
    const brainService = getBrainService();
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const taskResult = await taskService.getTasksWithDependencies(projectId);
      const pipeline = resolveCronPipeline(cron.id, taskResult.tasks);

      const now = new Date();
      const boundsCheck = canRunWithinBounds(cron, now);
      if (!boundsCheck.canRun) {
        return c.json(
          {
            error: "Conflict",
            message: `Cron cannot be triggered: ${boundsCheck.reason || "outside run bounds"}`,
          },
          409
        );
      }

      const runId = generateRunId(now);
      const triggerCheck = canTriggerPipeline(pipeline);

      if (!triggerCheck.canTrigger) {
        return c.json(
          {
            error: "Conflict",
            message: triggerCheck.reason || "Cron pipeline cannot be triggered",
          },
          409
        );
      }

      const run: CronRun = {
        run_id: runId,
        status: "in_progress",
        started: now.toISOString(),
        tasks: pipeline.length,
      };

      const existingRuns = cron.runs || [];
      const runs = [run, ...existingRuns];
      await brainService.update(cron.path, { runs });
      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);

      return c.json({
        cronId: cron.id,
        run,
        pipeline,
        pipelineCount: pipeline.length,
        message: "Cron run triggered",
      }, 200);
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(getCronRunsRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const runs = sortRunsDesc(cron.runs || []);
      return c.json({
        cronId: cron.id,
        runs,
        count: runs.length,
      }, 200);
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(listCronLinkedTasksRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const taskResult = await taskService.getTasksWithDependencies(projectId);
      return c.json(linkedTasksPayload(cron.id, taskResult.tasks), 200);
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(setCronLinkedTasksRoute, async (c) => {
    const { projectId, cronId } = c.req.valid("param");
    const { taskIds } = c.req.valid("json");
    const brainService = getBrainService();
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const dedupedTaskIds = [...new Set(taskIds)];
      const taskResult = await taskService.getTasksWithDependencies(projectId);
      const tasksById = new Map(taskResult.tasks.map((task) => [task.id, task]));

      for (const taskId of dedupedTaskIds) {
        if (!tasksById.has(taskId)) {
          return c.json(
            {
              error: "Not Found",
              message: `Task '${taskId}' not found in project '${projectId}'`,
            },
            404
          );
        }
      }

      const targetTaskIdSet = new Set(dedupedTaskIds);
      for (const task of taskResult.tasks) {
        const hasCronLink = task.cron_ids.includes(cron.id);
        const shouldHaveCronLink = targetTaskIdSet.has(task.id);

        if (hasCronLink === shouldHaveCronLink) continue;

        if (shouldHaveCronLink) {
          await brainService.update(task.path, {
            cron_ids: [...task.cron_ids, cron.id],
          });
          continue;
        }

        await brainService.update(task.path, {
          cron_ids: task.cron_ids.filter((id) => id !== cron.id),
        });
      }

      const updatedTaskResult = await taskService.getTasksWithDependencies(projectId);
      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);
      return c.json(
        {
          ...linkedTasksPayload(cron.id, updatedTaskResult.tasks),
          message: "Cron linked tasks replaced",
        },
        200
      );
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(addCronLinkedTaskRoute, async (c) => {
    const { projectId, cronId, taskId } = c.req.valid("param");
    const brainService = getBrainService();
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const taskResult = await taskService.getTasksWithDependencies(projectId);
      const task = taskResult.tasks.find((candidate) => candidate.id === taskId);
      if (!task) {
        return c.json(
          {
            error: "Not Found",
            message: `Task '${taskId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      if (!task.cron_ids.includes(cron.id)) {
        await brainService.update(task.path, {
          cron_ids: [...task.cron_ids, cron.id],
        });
      }

      const updatedTaskResult = await taskService.getTasksWithDependencies(projectId);
      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);
      return c.json(
        {
          ...linkedTasksPayload(cron.id, updatedTaskResult.tasks),
          message: `Task '${taskId}' linked to cron '${cron.id}'`,
        },
        200
      );
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  crons.openapi(removeCronLinkedTaskRoute, async (c) => {
    const { projectId, cronId, taskId } = c.req.valid("param");
    const brainService = getBrainService();
    const taskService = getTaskService();

    try {
      const cron = await findProjectCron(projectId, cronId);
      if (!cron) {
        return c.json(
          {
            error: "Not Found",
            message: `Cron '${cronId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      const taskResult = await taskService.getTasksWithDependencies(projectId);
      const task = taskResult.tasks.find((candidate) => candidate.id === taskId);
      if (!task) {
        return c.json(
          {
            error: "Not Found",
            message: `Task '${taskId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      if (task.cron_ids.includes(cron.id)) {
        await brainService.update(task.path, {
          cron_ids: task.cron_ids.filter((id) => id !== cron.id),
        });
      }

      const updatedTaskResult = await taskService.getTasksWithDependencies(projectId);
      await publishTaskSnapshot(realtimeHub, projectId);
      publishProjectDirty(realtimeHub, projectId);
      return c.json(
        {
          ...linkedTasksPayload(cron.id, updatedTaskResult.tasks),
          message: `Task '${taskId}' unlinked from cron '${cron.id}'`,
        },
        200
      );
    } catch (error) {
      if (isZkUnavailableError(error)) {
        return c.json(
          {
            error: "Service Unavailable",
            message: error.message,
          },
          503
        );
      }
      throw error;
    }
  });

  return crons;
}

export default createCronRoutes;
