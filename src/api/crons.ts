import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { getBrainService } from "../core/brain-service";
import { resolveCronPipeline, canTriggerPipeline, generateRunId } from "../core/cron-service";
import { getTaskService } from "../core/task-service";
import type { BrainEntry, CronRun } from "../core/types";
import {
  EntryIdSchema,
  ProjectIdSchema,
  CronListResponseSchema,
  CronDetailResponseSchema,
  CronTriggerResponseSchema,
  CronRunsResponseSchema,
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
    return null;
  }
}

function sortRunsDesc(runs: CronRun[]): CronRun[] {
  return [...runs].sort((a, b) =>
    new Date(b.started).getTime() - new Date(a.started).getTime()
  );
}

export function createCronRoutes(): OpenAPIHono {
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

  return crons;
}

export default createCronRoutes;
