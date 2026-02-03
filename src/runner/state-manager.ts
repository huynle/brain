/**
 * Brain Runner State Manager
 *
 * Persists runner state to JSON files for recovery after restart.
 * Enables resuming interrupted tasks.
 */

import { existsSync, readFileSync, writeFileSync, unlinkSync, readdirSync } from "fs";
import { join, basename } from "path";
import type { RunnerState, RunnerStats, RunnerStatus, RunningTask } from "./types";

/**
 * Manages state persistence for a brain runner instance.
 */
export class StateManager {
  private readonly stateFile: string;
  private readonly pidFile: string;
  private readonly runningTasksFile: string;

  constructor(stateDir: string, projectId: string) {
    this.stateFile = join(stateDir, `runner-${projectId}.json`);
    this.pidFile = join(stateDir, `runner-${projectId}.pid`);
    this.runningTasksFile = join(stateDir, `running-${projectId}.json`);
  }

  /**
   * Save full runner state to disk.
   */
  save(
    status: RunnerStatus,
    runningTasks: RunningTask[],
    stats: RunnerStats,
    startedAt: string
  ): void {
    const state: RunnerState = {
      projectId: this.extractProjectId(),
      status,
      startedAt,
      updatedAt: new Date().toISOString(),
      runningTasks,
      stats,
      config: {},
    };

    writeFileSync(this.stateFile, JSON.stringify(state, null, 2), "utf-8");
  }

  /**
   * Load runner state from disk.
   * Returns null if state file doesn't exist or is corrupted.
   */
  load(): RunnerState | null {
    if (!existsSync(this.stateFile)) {
      return null;
    }

    try {
      const content = readFileSync(this.stateFile, "utf-8");
      return JSON.parse(content) as RunnerState;
    } catch {
      // Corrupted JSON or read error
      return null;
    }
  }

  /**
   * Remove all state files for this project.
   */
  clear(): void {
    for (const file of [this.stateFile, this.pidFile, this.runningTasksFile]) {
      if (existsSync(file)) {
        unlinkSync(file);
      }
    }
  }

  /**
   * Save the runner's process ID to disk.
   */
  savePid(pid: number): void {
    writeFileSync(this.pidFile, String(pid), "utf-8");
  }

  /**
   * Load the runner's process ID from disk.
   * Returns null if PID file doesn't exist.
   */
  loadPid(): number | null {
    if (!existsSync(this.pidFile)) {
      return null;
    }

    try {
      const content = readFileSync(this.pidFile, "utf-8");
      const pid = parseInt(content.trim(), 10);
      return isNaN(pid) ? null : pid;
    } catch {
      return null;
    }
  }

  /**
   * Remove the PID file.
   */
  clearPid(): void {
    if (existsSync(this.pidFile)) {
      unlinkSync(this.pidFile);
    }
  }

  /**
   * Check if the saved PID is still running.
   * Returns false if no PID file exists or the process is dead.
   */
  isPidRunning(): boolean {
    const pid = this.loadPid();
    if (pid === null) {
      return false;
    }

    try {
      // Sending signal 0 checks if process exists without killing it
      process.kill(pid, 0);
      return true;
    } catch {
      // Process doesn't exist or we don't have permission
      return false;
    }
  }

  /**
   * Save running tasks to a separate file for faster recovery.
   */
  saveRunningTasks(tasks: RunningTask[]): void {
    writeFileSync(this.runningTasksFile, JSON.stringify(tasks, null, 2), "utf-8");
  }

  /**
   * Load running tasks from disk.
   * Returns empty array if file doesn't exist or is corrupted.
   */
  loadRunningTasks(): RunningTask[] {
    if (!existsSync(this.runningTasksFile)) {
      return [];
    }

    try {
      const content = readFileSync(this.runningTasksFile, "utf-8");
      return JSON.parse(content) as RunningTask[];
    } catch {
      // Corrupted JSON or read error
      return [];
    }
  }

  /**
   * Extract the project ID from the state file path.
   */
  private extractProjectId(): string {
    const filename = basename(this.stateFile);
    // runner-{projectId}.json -> projectId
    const match = filename.match(/^runner-(.+)\.json$/);
    return match ? match[1] : "unknown";
  }

  // =========================================================================
  // Static Utilities
  // =========================================================================

  /**
   * Find all runner state files in a directory.
   * Returns array of { projectId, stateFile } objects.
   */
  static findAllRunnerStates(
    stateDir: string
  ): Array<{ projectId: string; stateFile: string }> {
    if (!existsSync(stateDir)) {
      return [];
    }

    const files = readdirSync(stateDir);
    const states: Array<{ projectId: string; stateFile: string }> = [];

    for (const file of files) {
      const match = file.match(/^runner-(.+)\.json$/);
      if (match) {
        states.push({
          projectId: match[1],
          stateFile: join(stateDir, file),
        });
      }
    }

    return states;
  }

  /**
   * Remove state files for runners with dead PIDs.
   * Returns the number of stale states cleaned up.
   */
  static cleanupStaleStates(stateDir: string): number {
    const states = StateManager.findAllRunnerStates(stateDir);
    let cleaned = 0;

    for (const { projectId, stateFile } of states) {
      const manager = new StateManager(stateDir, projectId);
      
      // Check if PID is still running
      if (!manager.isPidRunning()) {
        manager.clear();
        cleaned++;
      }
    }

    return cleaned;
  }
}
