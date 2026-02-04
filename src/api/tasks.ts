/**
 * Task API Routes
 * 
 * Endpoints for task queries with dependency resolution.
 * Follows entries.ts patterns for consistency.
 */

import { Hono } from "hono";
import { getTaskService } from "../core/task-service";
import type {
  TaskListResponse,
  TaskNextResponse,
  TaskClaim,
  ClaimRequest,
  ClaimResponse,
  ClaimConflictResponse,
  ReleaseResponse,
  ClaimStatusResponse,
} from "../core/types";

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
// Validation Helpers
// =============================================================================

function validateProjectId(projectId: string): string | null {
  if (!projectId || projectId.trim() === "") {
    return "Project ID is required";
  }
  // Allow alphanumeric, hyphens, underscores
  if (!/^[a-zA-Z0-9_-]+$/.test(projectId)) {
    return "Invalid project ID format";
  }
  return null;
}

// =============================================================================
// Route Factory
// =============================================================================

export function createTaskRoutes(): Hono {
  const tasks = new Hono();
  const taskService = getTaskService();

  // GET / - List all projects with tasks
  tasks.get("/", (c) => {
    const projects = taskService.listProjects();
    return c.json({
      projects,
      count: projects.length,
    });
  });

  // GET /:projectId - All tasks with dependency resolution
  tasks.get("/:projectId", async (c) => {
    const projectId = c.req.param("projectId");
    
    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    try {
      const result = await taskService.getTasksWithDependencies(projectId);
      return c.json({
        tasks: result.tasks,
        count: result.tasks.length,
        stats: result.stats,
        cycles: result.cycles,
      });
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("zk CLI not available")) {
          return c.json({ 
            error: "Service Unavailable", 
            message: error.message 
          }, 503);
        }
      }
      throw error;
    }
  });

  // GET /:projectId/ready - Ready tasks (deps satisfied)
  tasks.get("/:projectId/ready", async (c) => {
    const projectId = c.req.param("projectId");
    
    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    try {
      const ready = await taskService.getReady(projectId);
      const response: TaskListResponse = {
        tasks: ready,
        count: ready.length,
      };
      return c.json(response);
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("zk CLI not available")) {
          return c.json({ 
            error: "Service Unavailable", 
            message: error.message 
          }, 503);
        }
      }
      throw error;
    }
  });

  // GET /:projectId/waiting - Waiting tasks
  tasks.get("/:projectId/waiting", async (c) => {
    const projectId = c.req.param("projectId");
    
    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    try {
      const waiting = await taskService.getWaiting(projectId);
      const response: TaskListResponse = {
        tasks: waiting,
        count: waiting.length,
      };
      return c.json(response);
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("zk CLI not available")) {
          return c.json({ 
            error: "Service Unavailable", 
            message: error.message 
          }, 503);
        }
      }
      throw error;
    }
  });

  // GET /:projectId/blocked - Blocked tasks
  tasks.get("/:projectId/blocked", async (c) => {
    const projectId = c.req.param("projectId");
    
    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    try {
      const blocked = await taskService.getBlocked(projectId);
      const response: TaskListResponse = {
        tasks: blocked,
        count: blocked.length,
      };
      return c.json(response);
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("zk CLI not available")) {
          return c.json({ 
            error: "Service Unavailable", 
            message: error.message 
          }, 503);
        }
      }
      throw error;
    }
  });

  // GET /:projectId/next - Next task to execute
  tasks.get("/:projectId/next", async (c) => {
    const projectId = c.req.param("projectId");
    
    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    try {
      const next = await taskService.getNext(projectId);
      
      if (!next) {
        const response: TaskNextResponse = {
          task: null,
          message: "No ready tasks available",
        };
        return c.json(response, 404);
      }
      
      const response: TaskNextResponse = {
        task: next,
      };
      return c.json(response);
    } catch (error) {
      if (error instanceof Error) {
        if (error.message.includes("zk CLI not available")) {
          return c.json({ 
            error: "Service Unavailable", 
            message: error.message 
          }, 503);
        }
      }
      throw error;
    }
  });

  // ==========================================================================
  // Task Claiming Endpoints
  // ==========================================================================

  // POST /:projectId/:taskId/claim - Claim a task
  tasks.post("/:projectId/:taskId/claim", async (c) => {
    const projectId = c.req.param("projectId");
    const taskId = c.req.param("taskId");

    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    if (!taskId || !/^[a-zA-Z0-9_-]+$/.test(taskId)) {
      return c.json({ error: "Bad Request", message: "Invalid task ID format" }, 400);
    }

    let body: ClaimRequest;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "Bad Request", message: "Invalid JSON body" }, 400);
    }

    if (!body.runnerId || typeof body.runnerId !== "string") {
      return c.json({ error: "Bad Request", message: "runnerId is required" }, 400);
    }

    const claimKey = getClaimKey(projectId, taskId);
    const existingClaim = claims.get(claimKey);

    // Check if already claimed
    if (existingClaim) {
      const stale = isClaimStale(existingClaim);
      
      // If same runner, refresh the claim
      if (existingClaim.runnerId === body.runnerId) {
        const now = Date.now();
        claims.set(claimKey, { runnerId: body.runnerId, claimedAt: now });
        const response: ClaimResponse = {
          success: true,
          taskId,
          runnerId: body.runnerId,
          claimedAt: new Date(now).toISOString(),
        };
        return c.json(response);
      }

      // If not stale, return conflict
      if (!stale) {
        const response: ClaimConflictResponse = {
          success: false,
          error: "conflict",
          message: "Task is already claimed by another runner",
          taskId,
          claimedBy: existingClaim.runnerId,
          claimedAt: new Date(existingClaim.claimedAt).toISOString(),
          isStale: false,
        };
        return c.json(response, 409);
      }

      // Stale claim can be overridden - fall through to create new claim
    }

    // Create new claim
    const now = Date.now();
    claims.set(claimKey, { runnerId: body.runnerId, claimedAt: now });

    const response: ClaimResponse = {
      success: true,
      taskId,
      runnerId: body.runnerId,
      claimedAt: new Date(now).toISOString(),
    };
    return c.json(response);
  });

  // POST /:projectId/:taskId/release - Release a claim
  tasks.post("/:projectId/:taskId/release", async (c) => {
    const projectId = c.req.param("projectId");
    const taskId = c.req.param("taskId");

    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    if (!taskId || !/^[a-zA-Z0-9_-]+$/.test(taskId)) {
      return c.json({ error: "Bad Request", message: "Invalid task ID format" }, 400);
    }

    const claimKey = getClaimKey(projectId, taskId);
    const existed = claims.delete(claimKey);

    const response: ReleaseResponse = {
      success: true,
      taskId,
      message: existed ? "Claim released" : "No claim existed",
    };
    return c.json(response);
  });

  // GET /:projectId/:taskId/claim-status - Get claim status
  tasks.get("/:projectId/:taskId/claim-status", async (c) => {
    const projectId = c.req.param("projectId");
    const taskId = c.req.param("taskId");

    const validationError = validateProjectId(projectId);
    if (validationError) {
      return c.json({ error: "Bad Request", message: validationError }, 400);
    }

    if (!taskId || !/^[a-zA-Z0-9_-]+$/.test(taskId)) {
      return c.json({ error: "Bad Request", message: "Invalid task ID format" }, 400);
    }

    const claimKey = getClaimKey(projectId, taskId);
    const claim = claims.get(claimKey);

    if (!claim) {
      const response: ClaimStatusResponse = {
        claimed: false,
      };
      return c.json(response);
    }

    const response: ClaimStatusResponse = {
      claimed: true,
      claimedBy: claim.runnerId,
      claimedAt: new Date(claim.claimedAt).toISOString(),
      isStale: isClaimStale(claim),
    };
    return c.json(response);
  });

  return tasks;
}

export default createTaskRoutes;
