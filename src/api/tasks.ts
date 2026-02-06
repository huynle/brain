/**
 * Task API Routes
 * 
 * Endpoints for task queries with dependency resolution.
 * Uses OpenAPIHono for automatic OpenAPI documentation generation.
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { getTaskService } from "../core/task-service";
import { computeAndResolveFeatures } from "../core/feature-service";
import type { TaskClaim } from "../core/types";
import {
  ProjectIdSchema,
  ProjectListResponseSchema,
  TaskListResponseSchema,
  TaskNextResponseSchema,
  ClaimRequestSchema,
  ClaimResponseSchema,
  ClaimConflictResponseSchema,
  ReleaseResponseSchema,
  ClaimStatusResponseSchema,
  ErrorResponseSchema,
  ServiceUnavailableResponseSchema,
  ComputedFeatureSchema,
  FeatureListResponseSchema,
  NotFoundResponseSchema,
} from "./schemas";

// =============================================================================
// Claim Tracking (In-Memory)
// =============================================================================

// Key: "projectId:taskId", Value: { runnerId, claimedAt }
const claims = new Map<string, TaskClaim>();

// Stale claim threshold: 5 minutes in milliseconds
const STALE_CLAIM_MS = 5 * 60 * 1000;

function getClaimKey(projectId: string, taskId: string): string {
  return `${projectId}:${taskId}`;
}

function isClaimStale(claim: TaskClaim): boolean {
  return Date.now() - claim.claimedAt > STALE_CLAIM_MS;
}

// =============================================================================
// Path Parameter Schemas
// =============================================================================

const ProjectIdParamSchema = z.object({
  projectId: ProjectIdSchema.openapi({
    param: { name: "projectId", in: "path" },
    example: "my-project",
  }),
});

const TaskIdParamSchema = z.object({
  projectId: ProjectIdSchema.openapi({
    param: { name: "projectId", in: "path" },
    example: "my-project",
  }),
  taskId: z.string().regex(/^[a-zA-Z0-9_-]+$/).openapi({
    param: { name: "taskId", in: "path" },
    description: "Task identifier",
    example: "abc12def",
  }),
});

const FeatureIdParamSchema = z.object({
  projectId: ProjectIdSchema.openapi({
    param: { name: "projectId", in: "path" },
    example: "my-project",
  }),
  featureId: z.string().min(1).openapi({
    param: { name: "featureId", in: "path" },
    description: "Feature identifier",
    example: "auth-system",
  }),
});

// =============================================================================
// Route Definitions
// =============================================================================

// GET / - List all projects with tasks
const listProjectsRoute = createRoute({
  method: "get",
  path: "/",
  tags: ["Tasks"],
  summary: "List all projects with tasks",
  description: "Returns a list of all projects that have tasks",
  responses: {
    200: {
      description: "List of projects",
      content: {
        "application/json": {
          schema: ProjectListResponseSchema,
        },
      },
    },
  },
});

// GET /:projectId - All tasks with dependency resolution
const getTasksRoute = createRoute({
  method: "get",
  path: "/{projectId}",
  tags: ["Tasks"],
  summary: "Get all tasks for a project",
  description: "Returns all tasks with dependency resolution, stats, and cycle detection",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Task list with dependency info",
      content: {
        "application/json": {
          schema: TaskListResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable (zk CLI required)",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

// GET /:projectId/ready - Ready tasks
const getReadyTasksRoute = createRoute({
  method: "get",
  path: "/{projectId}/ready",
  tags: ["Tasks"],
  summary: "Get ready tasks",
  description: "Returns tasks that are ready to execute (all dependencies satisfied)",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Ready tasks",
      content: {
        "application/json": {
          schema: TaskListResponseSchema,
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

// GET /:projectId/waiting - Waiting tasks
const getWaitingTasksRoute = createRoute({
  method: "get",
  path: "/{projectId}/waiting",
  tags: ["Tasks"],
  summary: "Get waiting tasks",
  description: "Returns tasks that are waiting on dependencies",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Waiting tasks",
      content: {
        "application/json": {
          schema: TaskListResponseSchema,
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

// GET /:projectId/blocked - Blocked tasks
const getBlockedTasksRoute = createRoute({
  method: "get",
  path: "/{projectId}/blocked",
  tags: ["Tasks"],
  summary: "Get blocked tasks",
  description: "Returns tasks that are blocked (unresolved dependencies or in cycles)",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Blocked tasks",
      content: {
        "application/json": {
          schema: TaskListResponseSchema,
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

// GET /:projectId/next - Next task to execute
const getNextTaskRoute = createRoute({
  method: "get",
  path: "/{projectId}/next",
  tags: ["Tasks"],
  summary: "Get next task to execute",
  description: "Returns the highest priority ready task",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Next ready task",
      content: {
        "application/json": {
          schema: TaskNextResponseSchema,
        },
      },
    },
    404: {
      description: "No ready tasks available",
      content: {
        "application/json": {
          schema: TaskNextResponseSchema,
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

// POST /:projectId/:taskId/claim - Claim a task
const claimTaskRoute = createRoute({
  method: "post",
  path: "/{projectId}/{taskId}/claim",
  tags: ["Tasks"],
  summary: "Claim a task",
  description: "Claim a task for exclusive execution by a runner. Stale claims can be overridden.",
  request: {
    params: TaskIdParamSchema,
    body: {
      content: {
        "application/json": {
          schema: ClaimRequestSchema,
        },
      },
    },
  },
  responses: {
    200: {
      description: "Task claimed successfully",
      content: {
        "application/json": {
          schema: ClaimResponseSchema,
        },
      },
    },
    409: {
      description: "Task already claimed by another runner",
      content: {
        "application/json": {
          schema: ClaimConflictResponseSchema,
        },
      },
    },
  },
});

// POST /:projectId/:taskId/release - Release a claim
const releaseTaskRoute = createRoute({
  method: "post",
  path: "/{projectId}/{taskId}/release",
  tags: ["Tasks"],
  summary: "Release a task claim",
  description: "Release a previously claimed task",
  request: {
    params: TaskIdParamSchema,
  },
  responses: {
    200: {
      description: "Claim released",
      content: {
        "application/json": {
          schema: ReleaseResponseSchema,
        },
      },
    },
  },
});

// GET /:projectId/:taskId/claim-status - Get claim status
const getClaimStatusRoute = createRoute({
  method: "get",
  path: "/{projectId}/{taskId}/claim-status",
  tags: ["Tasks"],
  summary: "Get task claim status",
  description: "Check if a task is currently claimed and by whom",
  request: {
    params: TaskIdParamSchema,
  },
  responses: {
    200: {
      description: "Claim status",
      content: {
        "application/json": {
          schema: ClaimStatusResponseSchema,
        },
      },
    },
  },
});

// =============================================================================
// Feature Route Definitions
// =============================================================================

// GET /:projectId/features - List all computed features with stats
const listFeaturesRoute = createRoute({
  method: "get",
  path: "/{projectId}/features",
  tags: ["Tasks"],
  summary: "List all computed features",
  description: "Returns all computed features with task statistics and dependency resolution",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "List of features with stats",
      content: {
        "application/json": {
          schema: FeatureListResponseSchema,
        },
      },
    },
    503: {
      description: "Service unavailable (zk CLI required)",
      content: {
        "application/json": {
          schema: ServiceUnavailableResponseSchema,
        },
      },
    },
  },
});

// GET /:projectId/features/ready - Get features ready for execution
const getReadyFeaturesRoute = createRoute({
  method: "get",
  path: "/{projectId}/features/ready",
  tags: ["Tasks"],
  summary: "Get ready features",
  description: "Returns features that are ready to execute (all feature dependencies satisfied)",
  request: {
    params: ProjectIdParamSchema,
  },
  responses: {
    200: {
      description: "Ready features",
      content: {
        "application/json": {
          schema: FeatureListResponseSchema,
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

// GET /:projectId/features/:featureId - Get single feature with its tasks
const getFeatureRoute = createRoute({
  method: "get",
  path: "/{projectId}/features/{featureId}",
  tags: ["Tasks"],
  summary: "Get a single feature",
  description: "Returns a specific feature with its tasks and dependency information",
  request: {
    params: FeatureIdParamSchema,
  },
  responses: {
    200: {
      description: "Feature details",
      content: {
        "application/json": {
          schema: z.object({
            feature: ComputedFeatureSchema,
          }).openapi("GetFeatureResponse"),
        },
      },
    },
    404: {
      description: "Feature not found",
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

// =============================================================================
// Route Factory
// =============================================================================

export function createTaskRoutes(): OpenAPIHono {
  const tasks = new OpenAPIHono({
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

  // GET / - List all projects with tasks
  tasks.openapi(listProjectsRoute, (c) => {
    const taskService = getTaskService();
    const projects = taskService.listProjects();
    return c.json({
      projects,
      count: projects.length,
    }, 200);
  });

  // GET /:projectId - All tasks with dependency resolution
  tasks.openapi(getTasksRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const result = await taskService.getTasksWithDependencies(projectId);
      return c.json({
        tasks: result.tasks,
        count: result.tasks.length,
        stats: result.stats,
        cycles: result.cycles,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // GET /:projectId/ready - Ready tasks
  tasks.openapi(getReadyTasksRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const ready = await taskService.getReady(projectId);
      return c.json({
        tasks: ready,
        count: ready.length,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // GET /:projectId/waiting - Waiting tasks
  tasks.openapi(getWaitingTasksRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const waiting = await taskService.getWaiting(projectId);
      return c.json({
        tasks: waiting,
        count: waiting.length,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // GET /:projectId/blocked - Blocked tasks
  tasks.openapi(getBlockedTasksRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const blocked = await taskService.getBlocked(projectId);
      return c.json({
        tasks: blocked,
        count: blocked.length,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // GET /:projectId/next - Next task to execute
  tasks.openapi(getNextTaskRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const next = await taskService.getNext(projectId);

      // Return 404 when no ready tasks available
      if (!next) {
        return c.json({
          task: null,
          message: "No ready tasks available",
        }, 404);
      }

      return c.json({
        task: next,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // ==========================================================================
  // Task Claiming Endpoints
  // ==========================================================================

  // POST /:projectId/:taskId/claim - Claim a task
  tasks.openapi(claimTaskRoute, async (c) => {
    const { projectId, taskId } = c.req.valid("param");
    const body = c.req.valid("json");

    const claimKey = getClaimKey(projectId, taskId);
    const existingClaim = claims.get(claimKey);

    // Check if already claimed
    if (existingClaim) {
      const stale = isClaimStale(existingClaim);

      // If same runner, refresh the claim
      if (existingClaim.runnerId === body.runnerId) {
        const now = Date.now();
        claims.set(claimKey, { runnerId: body.runnerId, claimedAt: now });
        return c.json({
          success: true as const,
          taskId,
          runnerId: body.runnerId,
          claimedAt: new Date(now).toISOString(),
        }, 200);
      }

      // If not stale, return conflict
      if (!stale) {
        return c.json(
          {
            success: false as const,
            error: "conflict" as const,
            message: "Task is already claimed by another runner",
            taskId,
            claimedBy: existingClaim.runnerId,
            claimedAt: new Date(existingClaim.claimedAt).toISOString(),
            isStale: false,
          },
          409
        );
      }

      // Stale claim can be overridden - fall through to create new claim
    }

    // Create new claim
    const now = Date.now();
    claims.set(claimKey, { runnerId: body.runnerId, claimedAt: now });

    return c.json({
      success: true as const,
      taskId,
      runnerId: body.runnerId,
      claimedAt: new Date(now).toISOString(),
    }, 200);
  });

  // POST /:projectId/:taskId/release - Release a claim
  tasks.openapi(releaseTaskRoute, async (c) => {
    const { projectId, taskId } = c.req.valid("param");

    const claimKey = getClaimKey(projectId, taskId);
    const existed = claims.delete(claimKey);

    return c.json({
      success: true,
      taskId,
      message: existed ? "Claim released" : "No claim existed",
    }, 200);
  });

  // GET /:projectId/:taskId/claim-status - Get claim status
  tasks.openapi(getClaimStatusRoute, async (c) => {
    const { projectId, taskId } = c.req.valid("param");

    const claimKey = getClaimKey(projectId, taskId);
    const claim = claims.get(claimKey);

    if (!claim) {
      return c.json({
        claimed: false,
      }, 200);
    }

    return c.json({
      claimed: true,
      claimedBy: claim.runnerId,
      claimedAt: new Date(claim.claimedAt).toISOString(),
      isStale: isClaimStale(claim),
    }, 200);
  });

  // ==========================================================================
  // Feature Endpoints
  // ==========================================================================

  // GET /:projectId/features - List all computed features
  tasks.openapi(listFeaturesRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const result = await taskService.getTasksWithDependencies(projectId);
      const featureResult = computeAndResolveFeatures(result.tasks);

      return c.json({
        features: featureResult.features.map((f) => ({
          id: f.id,
          priority: f.priority,
          status: f.status,
          classification: f.classification,
          task_stats: f.task_stats,
          blocked_by_features: f.blocked_by_features,
          waiting_on_features: f.waiting_on_features,
        })),
        count: featureResult.features.length,
        stats: featureResult.stats,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // GET /:projectId/features/ready - Get ready features
  tasks.openapi(getReadyFeaturesRoute, async (c) => {
    const { projectId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const result = await taskService.getTasksWithDependencies(projectId);
      const featureResult = computeAndResolveFeatures(result.tasks);
      
      // Filter to only ready features (not completed)
      const readyFeatures = featureResult.features.filter(
        (f) => f.classification === "ready" && f.status !== "completed"
      );

      return c.json({
        features: readyFeatures.map((f) => ({
          id: f.id,
          priority: f.priority,
          status: f.status,
          classification: f.classification,
          task_stats: f.task_stats,
          blocked_by_features: f.blocked_by_features,
          waiting_on_features: f.waiting_on_features,
        })),
        count: readyFeatures.length,
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // GET /:projectId/features/:featureId - Get single feature
  tasks.openapi(getFeatureRoute, async (c) => {
    const { projectId, featureId } = c.req.valid("param");
    const taskService = getTaskService();

    try {
      const result = await taskService.getTasksWithDependencies(projectId);
      const featureResult = computeAndResolveFeatures(result.tasks);
      
      const feature = featureResult.features.find((f) => f.id === featureId);
      
      if (!feature) {
        return c.json(
          {
            error: "Not Found",
            message: `Feature '${featureId}' not found in project '${projectId}'`,
          },
          404
        );
      }

      return c.json({
        feature: {
          id: feature.id,
          priority: feature.priority,
          status: feature.status,
          classification: feature.classification,
          task_stats: feature.task_stats,
          blocked_by_features: feature.blocked_by_features,
          waiting_on_features: feature.waiting_on_features,
        },
      }, 200);
    } catch (error) {
      if (error instanceof Error && error.message.includes("zk CLI not available")) {
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

  // ==========================================================================
  // Runner Control Endpoints (Pause/Resume)
  // ==========================================================================

  // POST /runner/pause/:projectId - Pause a specific project
  tasks.post("/runner/pause/:projectId", async (c) => {
    const { getTaskRunnerOrNull } = await import("../runner/task-runner");
    const runner = getTaskRunnerOrNull();
    
    if (!runner) {
      return c.json({
        error: "Runner not initialized",
        message: "The task runner is not currently running",
      }, 404);
    }
    
    const projectId = c.req.param("projectId");
    runner.pause(projectId);
    
    return c.json({
      success: true,
      projectId,
      paused: true,
      pausedProjects: runner.getPausedProjects(),
    }, 200);
  });

  // POST /runner/resume/:projectId - Resume a specific project
  tasks.post("/runner/resume/:projectId", async (c) => {
    const { getTaskRunnerOrNull } = await import("../runner/task-runner");
    const runner = getTaskRunnerOrNull();
    
    if (!runner) {
      return c.json({
        error: "Runner not initialized",
        message: "The task runner is not currently running",
      }, 404);
    }
    
    const projectId = c.req.param("projectId");
    runner.resume(projectId);
    
    return c.json({
      success: true,
      projectId,
      paused: false,
      pausedProjects: runner.getPausedProjects(),
    }, 200);
  });

  // POST /runner/pause - Pause all projects
  tasks.post("/runner/pause", async (c) => {
    const { getTaskRunnerOrNull } = await import("../runner/task-runner");
    const runner = getTaskRunnerOrNull();
    
    if (!runner) {
      return c.json({
        error: "Runner not initialized",
        message: "The task runner is not currently running",
      }, 404);
    }
    
    runner.pauseAll();
    
    return c.json({
      success: true,
      paused: true,
      pausedProjects: runner.getPausedProjects(),
    }, 200);
  });

  // POST /runner/resume - Resume all projects
  tasks.post("/runner/resume", async (c) => {
    const { getTaskRunnerOrNull } = await import("../runner/task-runner");
    const runner = getTaskRunnerOrNull();
    
    if (!runner) {
      return c.json({
        error: "Runner not initialized",
        message: "The task runner is not currently running",
      }, 404);
    }
    
    runner.resumeAll();
    
    return c.json({
      success: true,
      paused: false,
      pausedProjects: runner.getPausedProjects(),
    }, 200);
  });

  // GET /runner/status - Get runner status including pause state
  tasks.get("/runner/status", async (c) => {
    const { getTaskRunnerOrNull } = await import("../runner/task-runner");
    const runner = getTaskRunnerOrNull();
    
    if (!runner) {
      return c.json({
        error: "Runner not initialized",
        message: "The task runner is not currently running",
      }, 404);
    }
    
    const status = runner.getStatus();
    
    return c.json({
      success: true,
      ...status,
    }, 200);
  });

  return tasks;
}

export default createTaskRoutes;
