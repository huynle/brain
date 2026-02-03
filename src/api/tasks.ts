/**
 * Task API Routes
 * 
 * Endpoints for task queries with dependency resolution.
 * Follows entries.ts patterns for consistency.
 */

import { Hono } from "hono";
import { getTaskService } from "../core/task-service";
import type { TaskListResponse, TaskNextResponse } from "../core/types";

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

  return tasks;
}

export default createTaskRoutes;
