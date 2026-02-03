/**
 * Process Manager
 *
 * Track spawned OpenCode processes, detect completion, and handle cleanup.
 * This is the core of parallel task execution.
 */

import type { Subprocess } from "bun";
import type { RunningTask, TaskResult, RunnerConfig } from "./types";
import type { EntryStatus } from "../core/types";
import { getRunnerConfig, isDebugEnabled } from "./config";
import { ApiClient, getApiClient } from "./api-client";

// =============================================================================
// Types
// =============================================================================

export enum CompletionStatus {
  Running = "running",
  Completed = "completed",
  Failed = "failed",
  Blocked = "blocked",
  Timeout = "timeout",
  Crashed = "crashed",
}

// Bun's Subprocess type - we use a minimal interface to support both Bun.spawn result and mocks
export type BunSubprocess = Pick<Subprocess, "pid" | "exited" | "kill" | "exitCode">;

export interface ProcessInfo {
  task: RunningTask;
  proc: BunSubprocess;
  exitCode: number | null;
  exited: boolean;
  exitedAt?: string;
}

export interface ProcessState {
  taskId: string;
  task: RunningTask;
  pid: number;
  exitCode: number | null;
  exited: boolean;
  exitedAt?: string;
}

// =============================================================================
// Process Manager
// =============================================================================

export class ProcessManager {
  private processes: Map<string, ProcessInfo> = new Map();
  private config: RunnerConfig;
  private apiClient: ApiClient;

  constructor(config?: RunnerConfig, apiClient?: ApiClient) {
    this.config = config ?? getRunnerConfig();
    this.apiClient = apiClient ?? getApiClient();
  }

  // ========================================
  // Process Tracking
  // ========================================

  /**
   * Track a new process with exit handler.
   * Uses Bun's Subprocess which has .exited Promise instead of event emitters.
   */
  add(taskId: string, task: RunningTask, proc: BunSubprocess): void {
    if (this.processes.has(taskId)) {
      throw new Error(`Task ${taskId} is already being tracked`);
    }

    const info: ProcessInfo = {
      task,
      proc,
      exitCode: null,
      exited: false,
    };

    // Set up exit handler using Bun's Promise-based .exited
    proc.exited.then((exitCode) => {
      info.exitCode = exitCode ?? 0;
      info.exited = true;
      info.exitedAt = new Date().toISOString();

      if (isDebugEnabled()) {
        console.log(
          `[ProcessManager] Process exited: ${taskId} with code ${exitCode}`
        );
      }
    }).catch((error: Error) => {
      info.exitCode = -1;
      info.exited = true;
      info.exitedAt = new Date().toISOString();

      if (isDebugEnabled()) {
        console.log(`[ProcessManager] Process error: ${taskId}`, error.message);
      }
    });

    this.processes.set(taskId, info);

    if (isDebugEnabled()) {
      console.log(
        `[ProcessManager] Added process: ${taskId} (PID: ${proc.pid})`
      );
    }
  }

  /**
   * Remove and return task info.
   */
  remove(taskId: string): ProcessInfo | undefined {
    const info = this.processes.get(taskId);
    if (info) {
      this.processes.delete(taskId);

      if (isDebugEnabled()) {
        console.log(`[ProcessManager] Removed process: ${taskId}`);
      }
    }
    return info;
  }

  /**
   * Get process info.
   */
  get(taskId: string): ProcessInfo | undefined {
    return this.processes.get(taskId);
  }

  /**
   * Check if process is alive.
   */
  isRunning(taskId: string): boolean {
    const info = this.processes.get(taskId);
    if (!info) return false;
    return !info.exited;
  }

  /**
   * Get all process info.
   */
  getAll(): ProcessInfo[] {
    return Array.from(this.processes.values());
  }

  /**
   * Get only running tasks.
   */
  getAllRunning(): ProcessInfo[] {
    return this.getAll().filter((info) => !info.exited);
  }

  /**
   * Total tracked processes.
   */
  count(): number {
    return this.processes.size;
  }

  /**
   * Currently running count.
   */
  runningCount(): number {
    return this.getAllRunning().length;
  }

  // ========================================
  // Completion Detection
  // ========================================

  /**
   * Non-blocking completion check.
   * Checks both process state and task file status via API.
   */
  async checkCompletion(
    taskId: string,
    checkTaskFile: boolean = true
  ): Promise<CompletionStatus> {
    const info = this.processes.get(taskId);
    if (!info) {
      return CompletionStatus.Crashed;
    }

    // Check for timeout
    const startedAt = new Date(info.task.startedAt).getTime();
    const elapsed = Date.now() - startedAt;
    if (elapsed > this.config.taskTimeout) {
      return CompletionStatus.Timeout;
    }

    // If process has exited but we haven't checked the task file yet
    if (info.exited && !checkTaskFile) {
      // Without checking task file, we can only report crash
      return info.exitCode === 0
        ? CompletionStatus.Completed
        : CompletionStatus.Crashed;
    }

    // If process is still running, check task file for early completion
    if (checkTaskFile) {
      try {
        const status = await this.getTaskStatus(info.task.path);
        if (status) {
          if (status === "completed") return CompletionStatus.Completed;
          if (status === "blocked") return CompletionStatus.Blocked;
          // failed, archived, etc
          if (status !== "in_progress" && status !== "pending") {
            return CompletionStatus.Failed;
          }
        }
      } catch {
        // API error - continue with process state check
        if (isDebugEnabled()) {
          console.log(
            `[ProcessManager] Failed to check task status for ${taskId}`
          );
        }
      }
    }

    // Process still running and task still in progress
    if (!info.exited) {
      return CompletionStatus.Running;
    }

    // Process exited but task file didn't update to completion
    return CompletionStatus.Crashed;
  }

  /**
   * Get task status from API.
   */
  private async getTaskStatus(taskPath: string): Promise<EntryStatus | null> {
    try {
      const encodedPath = encodeURIComponent(taskPath);
      const response = await fetch(
        `${this.config.brainApiUrl}/api/v1/entries/${encodedPath}`
      );
      if (!response.ok) return null;
      const entry = await response.json();
      return entry.status as EntryStatus;
    } catch {
      return null;
    }
  }

  // ========================================
  // Process Control
  // ========================================

  /**
   * Kill specific process.
   */
  async kill(
    taskId: string,
    signal: NodeJS.Signals = "SIGTERM"
  ): Promise<boolean> {
    const info = this.processes.get(taskId);
    if (!info) return false;

    if (info.exited) {
      return true; // Already exited
    }

    if (isDebugEnabled()) {
      console.log(
        `[ProcessManager] Killing process: ${taskId} with signal ${signal}`
      );
    }

    // Send signal
    info.proc.kill(signal);

    // Wait for exit with timeout
    const exited = await this.waitForExit(info, 5000);

    // Force kill if didn't exit
    if (!exited && signal !== "SIGKILL") {
      if (isDebugEnabled()) {
        console.log(
          `[ProcessManager] Force killing process: ${taskId} with SIGKILL`
        );
      }
      info.proc.kill("SIGKILL");
      await this.waitForExit(info, 2000);
    }

    return info.exited;
  }

  /**
   * Gracefully terminate all processes.
   */
  async killAll(): Promise<void> {
    if (isDebugEnabled()) {
      console.log(
        `[ProcessManager] Killing all processes (${this.count()} total)`
      );
    }

    const taskIds = Array.from(this.processes.keys());

    // Send SIGTERM to all
    for (const taskId of taskIds) {
      const info = this.processes.get(taskId);
      if (info && !info.exited) {
        info.proc.kill("SIGTERM");
      }
    }

    // Wait for graceful exit
    await Promise.all(
      taskIds.map(async (taskId) => {
        const info = this.processes.get(taskId);
        if (info) {
          await this.waitForExit(info, 5000);
        }
      })
    );

    // Force kill any remaining
    for (const taskId of taskIds) {
      const info = this.processes.get(taskId);
      if (info && !info.exited) {
        if (isDebugEnabled()) {
          console.log(`[ProcessManager] Force killing: ${taskId}`);
        }
        info.proc.kill("SIGKILL");
      }
    }
  }

  /**
   * Wait for a process to exit.
   */
  private waitForExit(info: ProcessInfo, timeout: number): Promise<boolean> {
    return new Promise((resolve) => {
      if (info.exited) {
        resolve(true);
        return;
      }

      const checkInterval = 100;
      let elapsed = 0;

      const interval = setInterval(() => {
        if (info.exited) {
          clearInterval(interval);
          resolve(true);
          return;
        }

        elapsed += checkInterval;
        if (elapsed >= timeout) {
          clearInterval(interval);
          resolve(false);
        }
      }, checkInterval);
    });
  }

  // ========================================
  // State Persistence
  // ========================================

  /**
   * Serialize for state persistence.
   */
  toJSON(): ProcessState[] {
    return Array.from(this.processes.values()).map((info) => ({
      taskId: info.task.id,
      task: info.task,
      pid: info.proc.pid ?? -1,
      exitCode: info.exitCode,
      exited: info.exited,
      exitedAt: info.exitedAt,
    }));
  }

  /**
   * Restore from saved state (check PIDs).
   * Only restores processes that are still alive.
   */
  restoreFromState(tasks: ProcessState[]): RunningTask[] {
    const restored: RunningTask[] = [];

    for (const state of tasks) {
      // Skip if already tracking this task
      if (this.processes.has(state.taskId)) {
        continue;
      }

      // Check if process is still alive by checking PID
      if (state.pid > 0 && this.isPidAlive(state.pid)) {
        // Process is still alive - we can't re-attach to it
        // but we can track it as interrupted
        restored.push(state.task);

        if (isDebugEnabled()) {
          console.log(
            `[ProcessManager] Found living process: ${state.taskId} (PID: ${state.pid})`
          );
        }
      } else {
        if (isDebugEnabled()) {
          console.log(
            `[ProcessManager] Process no longer alive: ${state.taskId} (PID: ${state.pid})`
          );
        }
      }
    }

    return restored;
  }

  /**
   * Check if a PID is still alive.
   */
  private isPidAlive(pid: number): boolean {
    try {
      // Sending signal 0 checks if process exists
      process.kill(pid, 0);
      return true;
    } catch {
      return false;
    }
  }

  // ========================================
  // Task Result Generation
  // ========================================

  /**
   * Generate a TaskResult from a completed process.
   */
  createTaskResult(
    taskId: string,
    status: CompletionStatus
  ): TaskResult | null {
    const info = this.processes.get(taskId);
    if (!info) return null;

    const completedAt = new Date().toISOString();
    const startedAt = info.task.startedAt;
    const duration =
      new Date(completedAt).getTime() - new Date(startedAt).getTime();

    // Map CompletionStatus to TaskResult status
    let resultStatus: TaskResult["status"];
    switch (status) {
      case CompletionStatus.Completed:
        resultStatus = "completed";
        break;
      case CompletionStatus.Failed:
        resultStatus = "failed";
        break;
      case CompletionStatus.Blocked:
        resultStatus = "blocked";
        break;
      case CompletionStatus.Timeout:
        resultStatus = "timeout";
        break;
      case CompletionStatus.Crashed:
      default:
        resultStatus = "crashed";
        break;
    }

    return {
      taskId,
      status: resultStatus,
      startedAt,
      completedAt,
      duration,
      exitCode: info.exitCode ?? undefined,
    };
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let processManagerInstance: ProcessManager | null = null;

export function getProcessManager(): ProcessManager {
  if (!processManagerInstance) {
    processManagerInstance = new ProcessManager();
  }
  return processManagerInstance;
}

/**
 * Reset the process manager singleton (useful for testing).
 */
export function resetProcessManager(): void {
  processManagerInstance = null;
}
