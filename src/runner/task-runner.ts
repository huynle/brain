/**
 * Task Runner
 *
 * Main orchestration class that ties together polling, spawning, and monitoring.
 * This is the heart of the runner.
 */

import { randomBytes } from "crypto";
import type {
  RunnerConfig,
  ExecutionMode,
  RunningTask,
  TaskResult,
  RunnerStats,
  RunnerStatus,
  RunnerEvent,
  EventHandler,
} from "./types";
import type { ResolvedTask, EntryStatus } from "../core/types";
import { getRunnerConfig, isDebugEnabled } from "./config";
import { ApiClient, getApiClient } from "./api-client";
import { ProcessManager, getProcessManager, CompletionStatus } from "./process-manager";
import { StateManager } from "./state-manager";
import { OpencodeExecutor, getOpencodeExecutor } from "./opencode-executor";
import { SignalHandler, setupSignalHandler } from "./signals";
import { getLogger } from "./logger";
import { TmuxManager, getTmuxManager, type StatusInfo } from "./tmux-manager";
import { startDashboard, type DashboardHandle } from "./tui";
import type { LogEntry } from "./tui/types";
import { checkOpencodeStatus, isPidAlive, discoverOpencodePort } from "./opencode-port";
import { getAvailableMemoryPercent, getProcessResourceUsage } from "./system-utils";
import type { ResourceMetrics } from "./tui/types";

// =============================================================================
// Types
// =============================================================================

export interface TaskRunnerOptions {
  projectId?: string;           // Legacy single project
  projects?: string[];          // Multiple projects (Phase 2)
  config?: RunnerConfig;
  mode?: ExecutionMode;
  startPaused?: boolean;        // Start with all projects paused (TUI mode default)
}

export interface RunnerStatusInfo {
  status: RunnerStatus;
  projectId: string;
  runnerId: string;
  startedAt: string | null;
  runningTasks: RunningTask[];
  stats: RunnerStats;
  pausedProjects: string[];
}

// =============================================================================
// Task Runner Class
// =============================================================================

export class TaskRunner {
  // Core identity
  private readonly projects: string[];     // All projects to poll
  private readonly projectId: string;      // Legacy: first project (for backward compat)
  private readonly runnerId: string;
  private readonly mode: ExecutionMode;
  private readonly isMultiProject: boolean;

  // Configuration
  private readonly config: RunnerConfig;

  // Components
  private readonly apiClient: ApiClient;
  private readonly processManager: ProcessManager;
  private readonly stateManager: StateManager;
  private readonly executor: OpencodeExecutor;
  private signalHandler: SignalHandler | null = null;
  private tmuxManager: TmuxManager | null = null;
  private tuiDashboard: DashboardHandle | null = null;
  private tuiAddLog: ((entry: Omit<LogEntry, 'timestamp'>) => void) | null = null;

  // State
  private status: RunnerStatus = "idle";
  private startedAt: string | null = null;
  private stats: RunnerStats = { completed: 0, failed: 0, totalRuntime: 0 };
  private pollTimer: ReturnType<typeof setTimeout> | null = null;
  private isPolling = false;

  // TUI mode task tracking (tasks spawned in tmux windows without proc handles)
  private tuiTasks: Map<string, RunningTask> = new Map();

  // Pause state: in-memory only (not persisted across restarts)
  // Paused projects are excluded from task polling
  private pauseCache: Set<string> = new Set();
  private readonly startPaused: boolean;

  // Enabled features when project is paused (whitelist approach)
  // Only used when pauseCache contains the project - in-memory only
  // These features can run even when their project is paused
  private enabledFeatures: Set<string> = new Set();

  // Per-project concurrent task limits (in-memory only)
  // Projects not in this map use no limit (only global maxParallel applies)
  private projectLimits: Map<string, number> = new Map();

  // Pending resume queue: orphaned in_progress tasks detected on startup when paused
  // These are prioritized when resume/resumeAll is called
  // Map key is taskId, value is RunningTask placeholder
  private pendingResumeTasks: Map<string, RunningTask> = new Map();

  // Event handling
  private eventHandlers: EventHandler[] = [];

  // Logger
  private readonly logger = getLogger();

  constructor(options: TaskRunnerOptions) {
    // Handle both legacy single-project and new multi-project options
    if (options.projects && options.projects.length > 0) {
      this.projects = options.projects;
      this.projectId = options.projects[0]; // First project for backward compat
      this.isMultiProject = options.projects.length > 1;
    } else if (options.projectId) {
      this.projects = [options.projectId];
      this.projectId = options.projectId;
      this.isMultiProject = false;
    } else {
      throw new Error("TaskRunner requires either projectId or projects option");
    }
    
    this.runnerId = this.generateRunnerId();
    this.mode = options.mode ?? "background";
    this.config = options.config ?? getRunnerConfig();
    this.startPaused = options.startPaused ?? false;

    // Initialize components
    this.apiClient = getApiClient();
    this.processManager = getProcessManager();
    this.stateManager = new StateManager(this.config.stateDir, this.projectId);
    this.executor = getOpencodeExecutor();
  }

  // ========================================
  // Lifecycle Methods
  // ========================================

  /**
   * Start the polling loop.
   */
  async start(): Promise<void> {
    if (this.status !== "idle") {
      this.logger.warn("Runner already started", {
        projectId: this.projectId,
        status: this.status,
      });
      return;
    }

    // Suppress console output in TUI mode - logs go to file only
    // This must happen BEFORE any logging so the TUI display is clean
    if (this.mode === "tui") {
      this.logger.setSuppressConsole(true);
    }

    this.logger.info("Starting runner", {
      projects: this.projects,
      projectCount: this.projects.length,
      isMultiProject: this.isMultiProject,
      runnerId: this.runnerId,
      mode: this.mode,
      maxParallel: this.config.maxParallel,
    });

    // Initialize state
    this.status = "polling";
    this.startedAt = new Date().toISOString();
    this.stats = { completed: 0, failed: 0, totalRuntime: 0 };

    // Initialize dashboard if in dashboard or TUI mode
    // Note: TUI mode implies dashboard (like do-work script where --tui sets USE_DASHBOARD=true)
    if (this.mode === "dashboard" || this.mode === "tui") {
      await this.initializeDashboard();
    }

    // Set up signal handling
    this.setupSignalHandler();

    // Save PID for external stop commands
    this.stateManager.savePid(process.pid);

    // If configured to start paused, pause all projects BEFORE checking interrupted tasks
    // This ensures orphaned in_progress tasks are queued for resume rather than immediately spawned
    if (this.startPaused) {
      await this.pauseAll();
      this.logger.info("Runner started paused - press 'P' to begin processing", {
        projectCount: this.projects.length,
      });
      this.tuiLog('warn', "Runner started paused - press 'P' to begin processing");
    }

    // Check for interrupted tasks and try to resume (or queue if paused)
    await this.handleInterruptedTasks();

    // Save initial state
    this.saveState();

    // Start polling loop
    this.schedulePoll();
  }

  /**
   * Stop the runner gracefully.
   */
  async stop(): Promise<void> {
    if (this.status === "stopped") {
      return;
    }

    this.logger.info("Stopping runner", { projectId: this.projectId });

    this.status = "stopped";

    // Cancel pending poll
    if (this.pollTimer) {
      clearTimeout(this.pollTimer);
      this.pollTimer = null;
    }

    // Wait for current poll to complete
    while (this.isPolling) {
      await this.sleep(100);
    }

    // Kill all running tasks (from processManager)
    if (this.processManager.runningCount() > 0) {
      this.logger.info("Killing running tasks", {
        count: this.processManager.runningCount(),
      });
      await this.processManager.killAll();
    }

    // Clean up TUI mode tasks (kill processes and tmux windows)
    if (this.tuiTasks.size > 0) {
      this.logger.info("Cleaning up TUI tasks", {
        count: this.tuiTasks.size,
      });
      
      for (const task of this.tuiTasks.values()) {
        // SAFETY: Guard against dangerous PIDs that could kill multiple processes
        // PID 0 = current process group, PID -1 = ALL user processes (catastrophic!)
        if (task.pid <= 0) {
          this.logger.warn("Skipping invalid PID in TUI task cleanup", {
            taskId: task.id,
            pid: task.pid,
          });
          continue;
        }
        
        // First, explicitly kill the OpenCode process if still alive
        if (isPidAlive(task.pid)) {
          this.logger.warn("Killing orphan TUI task process", {
            taskId: task.id,
            pid: task.pid,
          });
          this.tuiLog("warn", `Killing orphan process ${task.pid} for task ${task.id}`);
          
          try {
            // Send SIGTERM first for graceful shutdown
            process.kill(task.pid, "SIGTERM");
            
            // Wait up to 2 seconds for graceful shutdown
            const startWait = Date.now();
            while (isPidAlive(task.pid) && Date.now() - startWait < 2000) {
              await this.sleep(100);
            }
            
            // If still alive, force kill with SIGKILL
            if (isPidAlive(task.pid)) {
              this.logger.warn("Process did not terminate gracefully, sending SIGKILL", {
                taskId: task.id,
                pid: task.pid,
              });
              this.tuiLog("warn", `Force killing process ${task.pid} for task ${task.id}`);
              process.kill(task.pid, "SIGKILL");
            }
          } catch (err) {
            // Process may have exited between check and kill
            this.logger.debug("Error killing process (may have already exited)", {
              taskId: task.id,
              pid: task.pid,
              error: err instanceof Error ? err.message : String(err),
            });
          }
        }
        
        // Then clean up the tmux window
        await this.cleanupTaskTmux(task);
      }
      this.tuiTasks.clear();
    }

    // Cleanup dashboard (either TUI or tmux)
    if (this.tuiDashboard) {
      this.tuiDashboard.unmount();
      this.tuiDashboard = null;
      this.tuiAddLog = null;
    }
    if (this.tmuxManager) {
      await this.tmuxManager.cleanup();
      this.tmuxManager = null;
    }

    // Save final state
    this.saveState();

    // Clear PID file
    this.stateManager.clearPid();

    // Emit shutdown event
    this.emitEvent({ type: "shutdown", reason: "manual" });

    this.logger.info("Runner stopped", {
      projectId: this.projectId,
      stats: this.stats,
    });
  }

  /**
   * Execute a single task and return.
   * Useful for testing or one-shot execution.
   */
  async runOnce(): Promise<TaskResult | null> {
    this.logger.info("Running single task", { projectId: this.projectId });

    // Check API availability
    const healthy = await this.apiClient.isAvailable();
    if (!healthy) {
      this.logger.error("API not available");
      return null;
    }

    // Get next task
    const task = await this.apiClient.getNextTask(this.projectId);
    if (!task) {
      this.logger.info("No ready tasks found", { projectId: this.projectId });
      return null;
    }

    // Execute the task
    return this.executeTask(task);
  }

  /**
   * Get current runner status.
   */
  getStatus(): RunnerStatusInfo {
    // Combine processManager tasks with TUI tasks
    const allRunningTasks = [
      ...this.processManager.getAll().map((info) => info.task),
      ...Array.from(this.tuiTasks.values()),
    ];
    
    return {
      status: this.status,
      projectId: this.projectId,
      runnerId: this.runnerId,
      startedAt: this.startedAt,
      runningTasks: allRunningTasks,
      stats: { ...this.stats },
      pausedProjects: Array.from(this.pauseCache),
    };
  }

  /**
   * Get resource metrics (CPU/memory) for all running OpenCode processes.
   * Used by TUI StatusBar to display system resource usage.
   */
  getResourceMetrics(): ResourceMetrics {
    // Collect PIDs from both processManager and tuiTasks
    const pids: number[] = [];
    
    // From processManager (background mode)
    for (const info of this.processManager.getAll()) {
      if (info.task.pid > 0) {
        pids.push(info.task.pid);
      }
    }
    
    // From tuiTasks (TUI mode - tasks spawned in tmux windows)
    for (const task of this.tuiTasks.values()) {
      if (task.pid > 0) {
        pids.push(task.pid);
      }
    }
    
    const usage = getProcessResourceUsage(pids);
    
    return {
      cpuPercent: usage.cpuPercent,
      memoryMB: usage.memoryMB,
      processCount: usage.processCount,
    };
  }

  // ========================================
  // Pause/Resume Methods
  // ========================================

  /**
   * Pause a specific project.
   * Paused projects are tracked in pauseCache - no new tasks will be started.
   * Running tasks will complete, but no new tasks will be picked up.
   */
  async pause(projectId: string): Promise<void> {
    if (!this.projects.includes(projectId)) {
      this.logger.warn("Attempted to pause unknown project", { projectId });
      return;
    }
    if (this.pauseCache.has(projectId)) {
      return; // Already paused
    }

    this.pauseCache.add(projectId);
    this.logger.info("Project paused", { projectId });
    this.tuiLog('warn', `Project paused: ${projectId}`, undefined, projectId);
    this.emitEvent({ type: "project_paused", projectId });
  }

  /**
   * Resume a paused project.
   * Removes the project from pauseCache so new tasks can be picked up.
   * PRIORITY: First processes any pending orphaned in_progress tasks for this project.
   * NOTE: Clears enabled features since they're no longer needed when project is fully unpaused.
   */
  async resume(projectId: string): Promise<void> {
    if (!this.pauseCache.has(projectId)) {
      return; // Not paused
    }

    this.pauseCache.delete(projectId);
    
    // Clear enabled features when project is fully unpaused
    // (they're no longer needed since all features can run)
    if (this.enabledFeatures.size > 0) {
      const clearedCount = this.enabledFeatures.size;
      this.enabledFeatures.clear();
      this.logger.info("Cleared enabled features on project resume", { 
        projectId, 
        clearedCount,
      });
    }
    
    this.logger.info("Project resumed", { projectId });
    this.tuiLog('info', `Project resumed: ${projectId}`, undefined, projectId);
    this.emitEvent({ type: "project_resumed", projectId });

    // Process any pending orphaned in_progress tasks for this project FIRST
    await this.processPendingResumeTasks(projectId);
  }

  /**
   * Process pending orphaned in_progress tasks that were queued during startup.
   * These take priority over new tasks because they represent interrupted work.
   * @param projectId - Optional. If provided, only process tasks for this project.
   */
  private async processPendingResumeTasks(projectId?: string): Promise<void> {
    const tasksToProcess: RunningTask[] = [];
    
    for (const [taskId, task] of this.pendingResumeTasks) {
      // Filter by project if specified
      if (projectId && task.projectId !== projectId) {
        continue;
      }
      // Skip if project is still paused
      if (this.pauseCache.has(task.projectId)) {
        continue;
      }
      tasksToProcess.push(task);
    }

    if (tasksToProcess.length === 0) {
      return;
    }

    this.logger.info("Processing pending orphaned tasks", {
      count: tasksToProcess.length,
      projectId: projectId ?? "all",
    });

    // Sort by priority (high > medium > low)
    const priorityOrder: Record<string, number> = { high: 0, medium: 1, low: 2 };
    tasksToProcess.sort((a, b) => {
      const aPriority = priorityOrder[a.priority] ?? 1;
      const bPriority = priorityOrder[b.priority] ?? 1;
      return aPriority - bPriority;
    });

    // Resume each pending task (respects maxParallel via resumeTask)
    for (const task of tasksToProcess) {
      // Check capacity before each resume
      const runningCount = this.processManager.runningCount() + this.tuiTasks.size;
      if (runningCount >= this.config.maxParallel) {
        this.logger.info("At max parallel capacity, leaving remaining pending tasks in queue", {
          remaining: tasksToProcess.length - tasksToProcess.indexOf(task),
        });
        break;
      }

      // Remove from pending queue
      this.pendingResumeTasks.delete(task.id);

      // Resume the task
      await this.resumeTask(task);
    }
  }

  /**
   * Pause all projects.
   */
  async pauseAll(): Promise<void> {
    await Promise.allSettled(
      this.projects.map(projectId => this.pause(projectId))
    );
    this.logger.info("All projects paused", { count: this.projects.length });
    this.tuiLog('warn', `All projects paused (${this.projects.length})`);
    this.emitEvent({ type: "all_paused" });
  }

  /**
   * Resume all paused projects.
   * PRIORITY: First processes any pending orphaned in_progress tasks.
   * NOTE: Clears enabled features since they're no longer needed when all projects are unpaused.
   */
  async resumeAll(): Promise<void> {
    const count = this.pauseCache.size;
    const pendingCount = this.pendingResumeTasks.size;
    const pausedIds = Array.from(this.pauseCache);
    
    // Clear pause state for all projects first
    for (const projectId of pausedIds) {
      this.pauseCache.delete(projectId);
      this.emitEvent({ type: "project_resumed", projectId });
    }
    
    // Clear enabled features when all projects are fully unpaused
    // (they're no longer needed since all features can run)
    if (this.enabledFeatures.size > 0) {
      const clearedCount = this.enabledFeatures.size;
      this.enabledFeatures.clear();
      this.logger.info("Cleared enabled features on resumeAll", { clearedCount });
    }
    
    this.logger.info("All projects resumed", { count, pendingOrphanedTasks: pendingCount });
    this.tuiLog('info', `All projects resumed (${count})${pendingCount > 0 ? `, processing ${pendingCount} orphaned task(s)` : ''}`);
    this.emitEvent({ type: "all_resumed" });

    // Process ALL pending orphaned in_progress tasks (no project filter)
    if (pendingCount > 0) {
      await this.processPendingResumeTasks();
    }
  }

  /**
   * Check if a specific project is paused (synchronous, uses local cache).
   */
  isPaused(projectId: string): boolean {
    return this.pauseCache.has(projectId);
  }

  /**
   * Get array of all paused project IDs.
   */
  getPausedProjects(): string[] {
    return Array.from(this.pauseCache);
  }

  /**
   * Check if all projects are paused.
   */
  isAllPaused(): boolean {
    return this.pauseCache.size === this.projects.length && this.projects.length > 0;
  }

  // ========================================
  // Feature Enable/Disable Methods (Whitelist for Paused Projects)
  // ========================================

  /**
   * Enable a feature to run while project is paused (whitelist approach).
   * Only has effect when the project containing this feature is paused.
   * When a project is paused, normally no tasks run. But if a feature is enabled,
   * tasks from that feature can still run.
   */
  enableFeature(featureId: string): void {
    this.enabledFeatures.add(featureId);
    this.logger.info("Enabled feature (will run even when project paused)", { featureId });
    this.tuiLog('info', `Feature enabled: ${featureId} (will run when project paused)`);
    this.emitEvent({ type: "feature_enabled", featureId });
  }

  /**
   * Disable a feature from running while project is paused.
   * Removes the feature from the enabled whitelist.
   */
  disableFeature(featureId: string): void {
    this.enabledFeatures.delete(featureId);
    this.logger.info("Disabled feature", { featureId });
    this.tuiLog('info', `Feature disabled: ${featureId}`);
    this.emitEvent({ type: "feature_disabled", featureId });
  }

  /**
   * Get all enabled features.
   */
  getEnabledFeatures(): string[] {
    return Array.from(this.enabledFeatures);
  }

  /**
   * Check if a feature is enabled (can run when project is paused).
   */
  isEnabledFeature(featureId: string): boolean {
    return this.enabledFeatures.has(featureId);
  }

  // ========================================
  // Per-Project Concurrent Task Limits
  // ========================================

  /**
   * Set the concurrent task limit for a specific project.
   * Set to 0 or undefined to remove the limit (use global maxParallel only).
   */
  setProjectLimit(projectId: string, limit: number | undefined): void {
    if (limit === undefined || limit <= 0) {
      this.projectLimits.delete(projectId);
      this.logger.info("Removed per-project limit", { projectId });
    } else {
      this.projectLimits.set(projectId, limit);
      this.logger.info("Set per-project limit", { projectId, limit });
    }
    this.tuiLog('info', `Project limit: ${projectId} = ${limit ?? 'no limit'}`, undefined, projectId);
  }

  /**
   * Get the concurrent task limit for a specific project.
   * Returns undefined if no limit is set (uses global maxParallel only).
   */
  getProjectLimit(projectId: string): number | undefined {
    return this.projectLimits.get(projectId);
  }

  /**
   * Get all per-project limits as a Map.
   */
  getProjectLimits(): Map<string, number> {
    return new Map(this.projectLimits);
  }

  /**
   * Get count of running tasks for a specific project.
   */
  getRunningCountForProject(projectId: string): number {
    let count = 0;
    for (const info of this.processManager.getAll()) {
      if (info.task.projectId === projectId) {
        count++;
      }
    }
    for (const task of this.tuiTasks.values()) {
      if (task.projectId === projectId) {
        count++;
      }
    }
    return count;
  }

  /**
   * Check if a project is at its per-project limit.
   * Returns false if no per-project limit is set.
   */
  isProjectAtLimit(projectId: string): boolean {
    const limit = this.projectLimits.get(projectId);
    if (limit === undefined) {
      return false; // No per-project limit
    }
    const running = this.getRunningCountForProject(projectId);
    return running >= limit;
  }

// ========================================
  // Event Handling
  // ========================================

  /**
   * Add an event handler.
   */
  on(handler: EventHandler): void {
    this.eventHandlers.push(handler);
  }

  /**
   * Remove an event handler.
   */
  off(handler: EventHandler): void {
    const idx = this.eventHandlers.indexOf(handler);
    if (idx !== -1) {
      this.eventHandlers.splice(idx, 1);
    }
  }

  private emitEvent(event: RunnerEvent): void {
    for (const handler of this.eventHandlers) {
      try {
        handler(event);
      } catch (error) {
        this.logger.error("Event handler error", { error: String(error) });
      }
    }
  }

  // ========================================
  // Polling Loop
  // ========================================

  private schedulePoll(): void {
    if (this.status === "stopped") {
      return;
    }

    this.pollTimer = setTimeout(async () => {
      await this.poll();
      this.schedulePoll();
    }, this.config.pollInterval * 1000);
  }

  private async poll(): Promise<void> {
    if (this.status === "stopped" || this.isPolling) {
      return;
    }

    this.isPolling = true;
    this.status = "polling";

    try {
      // Check API health
      const healthy = await this.apiClient.isAvailable();
      if (!healthy) {
        this.logger.warn("API not available, skipping poll");
        return;
      }

      // Step 1: Check completion of running tasks
      await this.checkRunningTasks();

      // Step 2: Check if at max parallel capacity (include TUI tasks)
      // Note: maxParallel is SHARED across ALL projects
      const runningCount = this.processManager.runningCount() + this.tuiTasks.size;
      if (runningCount >= this.config.maxParallel) {
        if (isDebugEnabled()) {
          this.logger.debug("At max parallel capacity", {
            running: runningCount,
            max: this.config.maxParallel,
          });
        }
        return;
      }

      // Step 2b: Hard limit regardless of tracking state (safety net for tracking bugs)
      // Note: runningCount already computed above combines processManager + tuiTasks
      if (runningCount >= this.config.maxTotalProcesses) {
        this.logger.error("Reached absolute process limit, refusing new tasks", {
          totalRunning: runningCount,
          maxTotalProcesses: this.config.maxTotalProcesses,
          processManagerCount: this.processManager.runningCount(),
          tuiTasksCount: this.tuiTasks.size,
        });
        this.tuiLog('error', `Process limit reached (${runningCount}/${this.config.maxTotalProcesses})`);
        return;
      }

      // Warning when approaching process limit (80% threshold)
      if (runningCount >= this.config.maxTotalProcesses * 0.8) {
        this.logger.warn("Approaching process limit", {
          totalRunning: runningCount,
          maxTotalProcesses: this.config.maxTotalProcesses,
        });
      }

      // Step 2c: Memory check - pause spawning if system memory is low
      const availableMemory = getAvailableMemoryPercent();
      if (availableMemory < this.config.memoryThresholdPercent) {
        this.logger.warn("System memory low, pausing task spawning", {
          availablePercent: availableMemory.toFixed(1),
          threshold: this.config.memoryThresholdPercent,
        });
        this.tuiLog('warn', `Memory low (${availableMemory.toFixed(1)}%), pausing spawning`);
        this.emitEvent({
          type: "poll_complete",
          readyCount: 0,
          runningCount,
        });
        return;
      }

      // Step 3: Get next tasks from non-paused projects using feature-aware ordering
      // Uses getNextTask() which respects feature dependencies and priorities
      // Also filter out projects that are at their per-project concurrent task limit
      // EXCEPTION: Paused projects with enabled features are still included (tasks filtered later)
      const activeProjects = this.projects.filter(p => {
        if (this.pauseCache.has(p)) {
          // Project is paused, but check if any features are enabled
          if (this.enabledFeatures.size > 0) {
            // Allow project - we'll filter by enabled features later
            if (isDebugEnabled()) {
              this.logger.debug("Project paused but has enabled features, allowing for feature-filtered polling", {
                projectId: p,
                enabledFeatures: this.getEnabledFeatures(),
              });
            }
            return true;
          }
          return false;
        }
        if (this.isProjectAtLimit(p)) {
          if (isDebugEnabled()) {
            this.logger.debug("Project at per-project limit, skipping", {
              projectId: p,
              limit: this.getProjectLimit(p),
              running: this.getRunningCountForProject(p),
            });
          }
          return false;
        }
        return true;
      });
      if (activeProjects.length === 0) {
        // All projects are paused or at limit, skip fetching tasks
        if (isDebugEnabled()) {
          this.logger.debug("All projects paused or at limit, skipping task fetch");
        }
        return;
      }

      // Build set of already running task keys to filter out
      const runningKeys = new Set([
        ...this.processManager.getAll().map((info) => `${info.task.projectId}:${info.task.id}`),
        ...Array.from(this.tuiTasks.values()).map((task) => `${task.projectId}:${task.id}`),
      ]);

      // Track per-project running counts for this poll cycle (to enforce limits during multi-task selection)
      const projectRunningCounts = new Map<string, number>();
      for (const projectId of this.projects) {
        projectRunningCounts.set(projectId, this.getRunningCountForProject(projectId));
      }

      // Step 4: Fill available slots using feature-ordered task selection
      // For each slot, get the next task from each active project and pick the highest priority
      const slotsAvailable = this.config.maxParallel - runningCount;
      const tasksToStart: (ResolvedTask & { _pollProjectId: string })[] = [];

      for (let slot = 0; slot < slotsAvailable; slot++) {
        // Filter projects that aren't at their per-project limit for this iteration
        const eligibleProjects = activeProjects.filter(projectId => {
          const limit = this.projectLimits.get(projectId);
          if (limit === undefined) return true; // No per-project limit
          const running = projectRunningCounts.get(projectId) ?? 0;
          return running < limit;
        });

        if (eligibleProjects.length === 0) {
          // All projects are at their per-project limits
          break;
        }

        // Get next task from each eligible project (respects feature ordering)
        const nextTaskResults = await Promise.allSettled(
          eligibleProjects.map(async (projectId) => {
            const task = await this.apiClient.getNextTask(projectId);
            if (task) {
              return { ...task, _pollProjectId: projectId };
            }
            return null;
          })
        );

        // Collect valid next tasks
        const candidateTasks: (ResolvedTask & { _pollProjectId: string })[] = [];
        for (const result of nextTaskResults) {
          if (result.status === 'fulfilled' && result.value) {
            const task = result.value;
            // Skip if already running or already queued to start
            const key = `${task._pollProjectId}:${task.id}`;
            if (runningKeys.has(key)) {
              continue;
            }
            // When project is paused but has enabled features, only allow those features
            if (this.pauseCache.has(task._pollProjectId)) {
              if (!task.feature_id || !this.enabledFeatures.has(task.feature_id)) {
                if (isDebugEnabled()) {
                  this.logger.debug("Skipping task - project paused and feature not enabled", {
                    taskId: task.id,
                    projectId: task._pollProjectId,
                    featureId: task.feature_id,
                    enabledFeatures: this.getEnabledFeatures(),
                  });
                }
                continue;
              }
            }
            candidateTasks.push(task);
          }
        }

        if (candidateTasks.length === 0) {
          // No more tasks available from any project
          break;
        }

        // Pick the highest priority task across all projects
        // Priority order: high (0) > medium (1) > low (2)
        const priorityOrder: Record<string, number> = { high: 0, medium: 1, low: 2 };
        candidateTasks.sort((a, b) => {
          const aPriority = priorityOrder[a.priority] ?? 1;
          const bPriority = priorityOrder[b.priority] ?? 1;
          if (aPriority !== bPriority) return aPriority - bPriority;
          // Secondary: prefer tasks with feature_id (feature-grouped work)
          if (a.feature_id && !b.feature_id) return -1;
          if (!a.feature_id && b.feature_id) return 1;
          // Tertiary: by created date (older first)
          return (a.created || "").localeCompare(b.created || "");
        });

        const selectedTask = candidateTasks[0];
        tasksToStart.push(selectedTask);
        
        // Mark as "running" for next iteration's filter
        runningKeys.add(`${selectedTask._pollProjectId}:${selectedTask.id}`);
        
        // Update per-project running count for next iteration
        const currentCount = projectRunningCounts.get(selectedTask._pollProjectId) ?? 0;
        projectRunningCounts.set(selectedTask._pollProjectId, currentCount + 1);

        // Log feature context if task belongs to a feature
        if (selectedTask.feature_id) {
          this.logger.info("Selected task from feature", {
            taskId: selectedTask.id,
            title: selectedTask.title,
            featureId: selectedTask.feature_id,
            projectId: selectedTask._pollProjectId,
          });
        }
      }

      if (tasksToStart.length === 0) {
        if (isDebugEnabled()) {
          this.logger.debug("No available tasks to start", {
            projects: this.projects.length,
          });
        }
        this.emitEvent({
          type: "poll_complete",
          readyCount: 0,
          runningCount,
        });
        return;
      }

      // Step 5: Start the selected tasks
      for (const task of tasksToStart) {
        // Use the tagged project ID for claiming/spawning
        await this.claimAndSpawn(task, task._pollProjectId);
      }

      // Step 6: Save state
      this.saveState();

      // Emit poll complete event
      const totalRunning = this.processManager.runningCount() + this.tuiTasks.size;
      this.emitEvent({
        type: "poll_complete",
        readyCount: tasksToStart.length,
        runningCount: totalRunning,
      });
    } catch (error) {
      this.logger.error("Poll error", { error: String(error) });
    } finally {
      this.isPolling = false;
      // Note: status could be changed to "stopped" by stop() during await operations
      if ((this.status as string) !== "stopped") {
        const totalRunning = this.processManager.runningCount() + this.tuiTasks.size;
        this.status = totalRunning > 0 ? "processing" : "polling";
      }
    }
  }

  // ========================================
  // Task Execution
  // ========================================

  private async claimAndSpawn(task: ResolvedTask, projectId?: string): Promise<boolean> {
    // Use provided projectId or fall back to legacy single project
    const effectiveProjectId = projectId ?? this.projectId;
    
    // Step 1: Claim task via API
    const claim = await this.apiClient.claimTask(
      effectiveProjectId,
      task.id,
      this.runnerId
    );

    if (!claim.success) {
      this.logger.info("Failed to claim task", {
        taskId: task.id,
        claimedBy: claim.claimedBy,
      });
      return false;
    }

    // Step 2: Update status to in_progress
    try {
      await this.apiClient.updateTaskStatus(task.path, "in_progress");
    } catch (error) {
      this.logger.error("Failed to update task status", {
        taskId: task.id,
        error: String(error),
      });
      await this.apiClient.releaseTask(this.projectId, task.id);
      return false;
    }

    // Step 3: Resolve workdir (may create worktree if needed)
    let workdir: string;
    try {
      workdir = await this.executor.resolveWorkdir(task);
    } catch (error) {
      // Worktree creation or setup failed - mark task as blocked
      const errorMessage = error instanceof Error ? error.message : String(error);

      this.logger.error("Worktree setup failed", {
        taskId: task.id,
        error: errorMessage,
      });

      try {
        await this.apiClient.updateTaskStatus(task.path, "blocked");

        const sanitizedBranch = task.git_branch?.replace(/\//g, "-") ?? "unknown";
        const blockNote =
          `\n\n## Blocked: Worktree Setup Failed\n\n` +
          `**Time:** ${new Date().toISOString()}\n` +
          `**Error:** ${errorMessage}\n\n` +
          `The task runner attempted to create a git worktree for branch \`${task.git_branch}\` ` +
          `but setup failed. Please:\n` +
          `1. Check if the branch exists and is valid\n` +
          `2. Manually create the worktree: \`git worktree add .worktrees/${sanitizedBranch} ${task.git_branch}\`\n` +
          `3. Run any necessary setup (npm install, etc.)\n` +
          `4. Update this task to \`pending\` to retry\n`;

        await this.apiClient.appendToTask(task.path, blockNote);
      } catch (updateError) {
        this.logger.error("Failed to update task status after worktree failure", {
          taskId: task.id,
          error: String(updateError),
        });
      }

      await this.apiClient.releaseTask(effectiveProjectId, task.id);
      this.tuiLog("error", `Worktree setup failed: ${task.title}`, task.id, effectiveProjectId);
      return false;
    }

    // Step 4: Spawn OpenCode process
    // When dashboard is active (TUI mode), spawn as panes not windows
    // But preserve TUI flag to use interactive command
    const effectiveMode = this.tmuxManager ? "dashboard" : this.mode;
    const useTui = this.mode === "tui";
    try {
      const result = await this.executor.spawn(task, effectiveProjectId, {
        mode: effectiveMode,
        paneId: this.tmuxManager?.getLayout()?.taskAreaPaneId,
        isResume: false,
        useTui,
        workdir, // Pass pre-resolved workdir to avoid redundant resolution
      });

      // Step 5: Track the process
      const runningTask: RunningTask = {
        id: task.id,
        path: task.path,
        title: task.title,
        priority: task.priority,
        projectId: effectiveProjectId,
        pid: result.pid,
        paneId: result.paneId,
        windowName: result.windowName,
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir,
        opencodePort: result.opencodePort,
      };

      // Track the task: either in processManager (with proc) or tuiTasks (TUI mode)
      if (result.proc) {
        this.processManager.add(task.id, runningTask, result.proc);
      } else if (result.windowName) {
        // TUI mode: track separately since we can't get proc handle
        this.tuiTasks.set(task.id, runningTask);
      }

      this.logger.info("Task started", {
        taskId: task.id,
        title: task.title,
        pid: result.pid,
        projectId: effectiveProjectId,
      });
      this.tuiLog("info", `Task started: ${task.title}`, task.id, effectiveProjectId);

      // Handle dashboard task pane
      await this.handleDashboardTaskStart(runningTask);

      // Emit event
      this.emitEvent({ type: "task_started", task: runningTask });

      return true;
    } catch (error) {
      this.logger.error("Failed to spawn task", {
        taskId: task.id,
        error: String(error),
      });
      await this.apiClient.releaseTask(effectiveProjectId, task.id);
      return false;
    }
  }

  private async executeTask(task: ResolvedTask): Promise<TaskResult | null> {
    // Claim and spawn
    const started = await this.claimAndSpawn(task);
    if (!started) {
      return null;
    }

    // Wait for completion
    const result = await this.waitForTask(task.id);
    return result;
  }

  private async waitForTask(taskId: string): Promise<TaskResult | null> {
    const startTime = Date.now();
    const timeout = this.config.taskTimeout;

    while (Date.now() - startTime < timeout) {
      const status = await this.processManager.checkCompletion(taskId);

      if (status !== CompletionStatus.Running) {
        return this.handleTaskCompletion(taskId, status);
      }

      await this.sleep(this.config.taskPollInterval * 1000);
    }

    // Timeout
    return this.handleTaskCompletion(taskId, CompletionStatus.Timeout);
  }

  // ========================================
  // Task Completion
  // ========================================

  private async checkRunningTasks(): Promise<void> {
    const processes = this.processManager.getAll();

    for (const info of processes) {
      const status = await this.processManager.checkCompletion(info.task.id);

      if (status !== CompletionStatus.Running) {
        await this.handleTaskCompletion(info.task.id, status);
      }
    }

    // Also check TUI mode tasks (tracked separately)
    await this.checkTuiTasks();

    // Check for idle OpenCode instances and mark as blocked
    await this.checkOpencodeIdleStatus();

    // Check if blocked tasks should be auto-resumed
    await this.checkBlockedTasksForResume();
  }

  /**
   * Check TUI mode tasks for completion.
   * These are tasks spawned in tmux windows without direct proc handles.
   * We poll the brain API to detect when they complete.
   */
  private async checkTuiTasks(): Promise<void> {
    for (const [taskId, task] of this.tuiTasks) {
      try {
        // Check if TUI task process is still alive
        if (!isPidAlive(task.pid)) {
          this.logger.warn("Detected dead TUI task process, cleaning up", {
            taskId,
            pid: task.pid,
            startedAt: task.startedAt,
          });
          
          this.tuiLog('warn', `Task ${task.title} process died unexpectedly, cleaning up`, taskId, task.projectId);
          
          // Mark as crashed/failed
          await this.handleTuiTaskCompletion(taskId, task, CompletionStatus.Crashed);
          
          continue; // Move to next task in loop
        }

        // Check task status from brain API
        const encodedPath = encodeURIComponent(task.path);
        const response = await fetch(
          `${this.config.brainApiUrl}/api/v1/entries/${encodedPath}`
        );
        
        if (!response.ok) continue;
        
        const entry = await response.json();
        const status = entry.status as EntryStatus;

        if (status === "completed" || status === "cancelled") {
          const completionStatus = status === "completed" 
            ? CompletionStatus.Completed 
            : CompletionStatus.Cancelled;
          
          await this.handleTuiTaskCompletion(taskId, task, completionStatus);
          continue;
        }
        // Note: "blocked" status is NOT treated as completion
        // Blocked tasks stay in tuiTasks to allow resume detection

        // Check for timeout
        const elapsed = Date.now() - new Date(task.startedAt).getTime();
        if (elapsed > this.config.taskTimeout) {
          this.logger.warn("TUI task timed out", {
            taskId,
            elapsed,
            timeout: this.config.taskTimeout,
            elapsedMinutes: Math.round(elapsed / 60000),
          });
          
          await this.handleTuiTaskCompletion(taskId, task, CompletionStatus.Timeout);
          continue;
        }
      } catch (error) {
        this.logger.debug("Failed to check TUI task status", {
          taskId,
          error: String(error),
        });
      }
    }
  }

  // ========================================
  // OpenCode Idle Detection
  // ========================================

  /**
   * Check running tasks for idle OpenCode instances.
   * If a task's OpenCode is idle for longer than idleDetectionThreshold,
   * mark it as blocked (but don't kill the process).
   */
  private async checkOpencodeIdleStatus(): Promise<void> {
    for (const [taskId, task] of this.tuiTasks) {
      // Skip tasks without discovered ports
      if (!task.opencodePort) {
        // Try to discover the port if we haven't yet
        if (task.pid > 0 && isPidAlive(task.pid)) {
          const port = await discoverOpencodePort(task.pid);
          if (port) {
            task.opencodePort = port;
            this.logger.debug("Discovered OpenCode port", { taskId, port });
          }
        }
        continue;
      }

      // Check OpenCode status via HTTP API
      const status = await checkOpencodeStatus(task.opencodePort);

      if (status === "idle") {
        // OpenCode is idle - check if we need to mark as blocked
        const now = Date.now();
        
        if (!task.idleSince) {
          // First time detecting idle - record the timestamp
          task.idleSince = new Date().toISOString();
          this.logger.debug("OpenCode became idle", { taskId, idleSince: task.idleSince });
        } else {
          // Check if idle for longer than threshold
          const idleDuration = now - new Date(task.idleSince).getTime();
          
          if (idleDuration >= this.config.idleDetectionThreshold) {
            // Mark task as blocked
            await this.markTaskBlocked(taskId, task, "OpenCode idle - waiting for user input");
          }
        }
      } else if (status === "busy") {
        // OpenCode is busy - clear idle state
        if (task.idleSince) {
          this.logger.debug("OpenCode became busy again", { taskId });
          task.idleSince = undefined;
        }
      }
      // status === "unavailable" - skip, might be temporary
    }
  }

  /**
   * Check blocked tasks for auto-resume.
   * If a blocked task's OpenCode becomes busy again (user interacted),
   * transition it back to in_progress.
   */
  private async checkBlockedTasksForResume(): Promise<void> {
    for (const [taskId, task] of this.tuiTasks) {
      // Check current status from Brain API
      try {
        const encodedPath = encodeURIComponent(task.path);
        const response = await fetch(
          `${this.config.brainApiUrl}/api/v1/entries/${encodedPath}`
        );
        
        if (!response.ok) continue;
        
        const entry = await response.json();
        const brainStatus = entry.status as EntryStatus;

        // Only interested in blocked tasks
        if (brainStatus !== "blocked") continue;

        // Check if PID is still alive
        if (!isPidAlive(task.pid)) {
          // Process is dead - user must manually handle
          this.logger.debug("Blocked task has dead PID, skipping auto-resume", { taskId });
          continue;
        }

        // Check if we have a port to check status
        if (!task.opencodePort) {
          // Try to discover port
          const port = await discoverOpencodePort(task.pid);
          if (port) {
            task.opencodePort = port;
          } else {
            continue;
          }
        }

        // Check OpenCode status
        const opencodeStatus = await checkOpencodeStatus(task.opencodePort);

        if (opencodeStatus === "busy") {
          // User has interacted! Resume the task
          await this.resumeBlockedTask(taskId, task);
        }
      } catch (error) {
        this.logger.debug("Failed to check blocked task for resume", {
          taskId,
          error: String(error),
        });
      }
    }
  }

  /**
   * Mark a task as blocked in the Brain API.
   * Does NOT kill the process - leaves OpenCode running for user interaction.
   */
  private async markTaskBlocked(taskId: string, task: RunningTask, reason: string): Promise<void> {
    try {
      // Update status in Brain API
      await this.apiClient.updateTaskStatus(task.path, "blocked");

      // Append note to task file
      const note = `\n\n## Blocked: ${reason}\n` +
        `- Detected at: ${new Date().toISOString()}\n` +
        `- Tmux window: ${task.windowName || task.paneId || 'unknown'}\n` +
        `- To resume: Navigate to the tmux window and interact with OpenCode\n`;

      await this.apiClient.appendToTask(task.path, note);

      this.logger.info("Task marked as blocked", { taskId, reason });
      this.tuiLog('warn', `Task blocked: ${task.title} (${reason})`, taskId, task.projectId);

      // Clear idle state since we've now acted on it
      task.idleSince = undefined;
    } catch (error) {
      this.logger.error("Failed to mark task as blocked", {
        taskId,
        error: String(error),
      });
    }
  }

  /**
   * Resume a blocked task that has become active again.
   */
  private async resumeBlockedTask(taskId: string, task: RunningTask): Promise<void> {
    try {
      // Update status back to in_progress
      await this.apiClient.updateTaskStatus(task.path, "in_progress");

      // Clear idle state
      task.idleSince = undefined;

      // Append resume note
      const note = `\n\n## Resumed\n` +
        `- Resumed at: ${new Date().toISOString()}\n` +
        `- Auto-detected: User interaction with OpenCode\n`;

      await this.apiClient.appendToTask(task.path, note);

      this.logger.info("Blocked task resumed", { taskId });
      this.tuiLog('info', `Task resumed: ${task.title}`, taskId, task.projectId);
    } catch (error) {
      this.logger.error("Failed to resume blocked task", {
        taskId,
        error: String(error),
      });
    }
  }

  private async handleTaskCompletion(
    taskId: string,
    status: CompletionStatus
  ): Promise<TaskResult | null> {
    const result = this.processManager.createTaskResult(taskId, status);
    if (!result) {
      return null;
    }

    const info = this.processManager.get(taskId);
    if (!info) {
      return result;
    }

    // Update stats and task status in brain
    if (result.status === "completed") {
      this.stats.completed++;
      this.emitEvent({ type: "task_completed", result });
      this.logger.info("Task completed", {
        taskId,
        title: info.task.title,
        duration: result.duration,
        projectId: info.task.projectId,
      });
      this.tuiLog('info', `Task completed: ${info.task.title} (${Math.round(result.duration / 1000)}s)`, taskId, info.task.projectId);
    } else {
      this.stats.failed++;
      this.emitEvent({ type: "task_failed", result });
      this.logger.warn("Task failed", {
        taskId,
        title: info.task.title,
        status: result.status,
        exitCode: result.exitCode,
        projectId: info.task.projectId,
      });
      this.tuiLog('error', `Task failed: ${info.task.title} (${result.status})`, taskId, info.task.projectId);

      // Update task status to blocked in the brain (failed tasks are marked as blocked)
      try {
        await this.apiClient.updateTaskStatus(info.task.path, "blocked");

        // Append failure reason to task
        const failureNote = this.buildFailureNote(result);
        await this.apiClient.appendToTask(info.task.path, failureNote);
      } catch (error) {
        this.logger.error("Failed to update task failure status", {
          taskId,
          error: String(error),
        });
      }
    }

    this.stats.totalRuntime += result.duration;

    // Release claim
    try {
      await this.apiClient.releaseTask(this.projectId, taskId);
    } catch (error) {
      this.logger.error("Failed to release task", {
        taskId,
        error: String(error),
      });
    }

    // Cleanup executor files
    await this.executor.cleanup(taskId, this.projectId);

    // Clean up tmux window/pane (TUI mode or fallback)
    await this.cleanupTaskTmux(info.task);

    // Handle dashboard task pane removal
    await this.handleDashboardTaskComplete(taskId);

    // Remove from process manager
    this.processManager.remove(taskId);

    // Save state
    this.saveState();

    return result;
  }

  /**
   * Handle completion for TUI mode tasks.
   * These are tracked separately from processManager since we can't get proc handles.
   */
  private async handleTuiTaskCompletion(
    taskId: string,
    task: RunningTask,
    status: CompletionStatus
  ): Promise<TaskResult | null> {
    const completedAt = new Date().toISOString();
    const startedAt = task.startedAt;
    const duration =
      new Date(completedAt).getTime() - new Date(startedAt).getTime();

    // Create result - map CompletionStatus to TaskResult status
    let resultStatus: TaskResult["status"];
    switch (status) {
      case CompletionStatus.Completed:
        resultStatus = "completed";
        break;
      case CompletionStatus.Timeout:
        resultStatus = "timeout";
        break;
      case CompletionStatus.Cancelled:
        resultStatus = "cancelled";
        break;
      default:
        resultStatus = "blocked";
    }
    
    const result: TaskResult = {
      taskId,
      status: resultStatus,
      startedAt,
      completedAt,
      duration,
    };

    // Update stats
    if (status === CompletionStatus.Completed) {
      this.stats.completed++;
      this.emitEvent({ type: "task_completed", result });
      this.logger.info("TUI task completed", {
        taskId,
        title: task.title,
        duration,
        projectId: task.projectId,
      });
      this.tuiLog('info', `Task completed: ${task.title} (${Math.round(duration / 1000)}s)`, taskId, task.projectId);
    } else {
      this.stats.failed++;
      this.emitEvent({ type: "task_failed", result });
      this.logger.warn("TUI task failed", {
        taskId,
        title: task.title,
        status: result.status,
        projectId: task.projectId,
      });
      this.tuiLog('error', `Task failed: ${task.title} (${result.status})`, taskId, task.projectId);
    }

    this.stats.totalRuntime += duration;

    // Release claim
    try {
      await this.apiClient.releaseTask(this.projectId, taskId);
    } catch (error) {
      this.logger.error("Failed to release TUI task", {
        taskId,
        error: String(error),
      });
    }

    // Cleanup executor files
    await this.executor.cleanup(taskId, this.projectId);

    // Clean up tmux window
    await this.cleanupTaskTmux(task);

    // Remove from TUI tasks tracking
    this.tuiTasks.delete(taskId);

    // Save state
    this.saveState();

    return result;
  }

  // ========================================
  // Interrupted Task Recovery
  // ========================================

  private async handleInterruptedTasks(): Promise<void> {
    // Step 1: Check Brain API for in_progress tasks that have no running process
    // These should be prioritized for resume BEFORE checking local state
    await this.resumeOrphanedInProgressTasks();

    // Step 2: Load previous state
    const prevState = this.stateManager.load();
    if (!prevState || prevState.runningTasks.length === 0) {
      return;
    }

    this.logger.info("Found interrupted tasks in local state", {
      count: prevState.runningTasks.length,
    });

    // Restore stats from previous run
    this.stats = {
      completed: prevState.stats.completed,
      failed: prevState.stats.failed,
      totalRuntime: prevState.stats.totalRuntime,
    };

    // Try to resume each interrupted task from local state
    for (const task of prevState.runningTasks) {
      await this.resumeTask(task);
    }
  }

  /**
   * Check Brain API for tasks marked as in_progress but without running processes.
   * These are "orphaned" tasks from a previous runner crash/restart.
   * If paused, queue them for priority execution on resume. Otherwise, resume immediately.
   */
  private async resumeOrphanedInProgressTasks(): Promise<void> {
    // Get running task IDs we're currently tracking
    const runningTaskIds = new Set([
      ...this.processManager.getAll().map((info) => info.task.id),
      ...Array.from(this.tuiTasks.keys()),
    ]);

    // Check each project for orphaned in_progress tasks
    for (const projectId of this.projects) {
      try {
        const inProgressTasks = await this.apiClient.getInProgressTasks(projectId);

        for (const task of inProgressTasks) {
          // Skip if we're already tracking this task
          if (runningTaskIds.has(task.id)) {
            continue;
          }

          this.logger.info("Found orphaned in_progress task", {
            taskId: task.id,
            title: task.title,
            projectId,
            paused: this.pauseCache.has(projectId),
          });

          // Resolve workdir (may fail if worktree setup needed)
          let workdir: string;
          try {
            workdir = await this.executor.resolveWorkdir(task);
          } catch (error) {
            const errorMessage = error instanceof Error ? error.message : String(error);
            this.logger.warn("Failed to resolve workdir for orphaned task", {
              taskId: task.id,
              error: errorMessage,
            });
            // Skip this task for now - let claimAndSpawn handle blocking later
            continue;
          }

          // Create a RunningTask placeholder to resume
          const runningTask: RunningTask = {
            id: task.id,
            path: task.path,
            title: task.title,
            priority: task.priority,
            projectId,
            pid: 0, // No running process
            startedAt: new Date().toISOString(),
            isResume: true,
            workdir,
          };

          // If project is paused, queue for later. Otherwise resume immediately.
          if (this.pauseCache.has(projectId)) {
            this.pendingResumeTasks.set(task.id, runningTask);
            this.logger.info("Queued orphaned task for resume on unpause", {
              taskId: task.id,
              title: task.title,
              projectId,
            });
            this.tuiLog('warn', `Orphaned task queued: ${task.title} (will resume on unpause)`, task.id, projectId);
          } else {
            await this.resumeTask(runningTask);
          }
        }
      } catch (error) {
        this.logger.warn("Failed to check for orphaned tasks", {
          projectId,
          error: String(error),
        });
      }
    }
  }

  private async resumeTask(runningTask: RunningTask): Promise<void> {
    this.logger.info("Resuming interrupted task", {
      taskId: runningTask.id,
      title: runningTask.title,
      projectId: runningTask.projectId,
    });

    // Fetch current task state by path (not getNextTask which only returns ready tasks)
    const task = await this.apiClient.getTaskByPath(runningTask.path);
    if (!task) {
      // Task may have been deleted
      this.logger.info("Task no longer exists, skipping resume", {
        taskId: runningTask.id,
        path: runningTask.path,
      });
      return;
    }

    // Check if task is still in_progress (not completed, cancelled, etc.)
    if (task.status !== "in_progress") {
      this.logger.info("Task status changed, skipping resume", {
        taskId: runningTask.id,
        currentStatus: task.status,
      });
      return;
    }

    // Spawn with resume flag
    // When dashboard is active (TUI mode), spawn as panes not windows
    // But preserve TUI flag to use interactive command
    const effectiveMode = this.tmuxManager ? "dashboard" : this.mode;
    const useTui = this.mode === "tui";
    const effectiveProjectId = runningTask.projectId;
    try {
      const result = await this.executor.spawn(task, effectiveProjectId, {
        mode: effectiveMode,
        paneId: this.tmuxManager?.getLayout()?.taskAreaPaneId,
        isResume: true,
        useTui,
      });

      this.tuiLog('info', `Resuming orphaned task: ${task.title}`, task.id, effectiveProjectId);

      const newRunningTask: RunningTask = {
        ...runningTask,
        pid: result.pid,
        paneId: result.paneId,
        windowName: result.windowName,
        startedAt: new Date().toISOString(),
        isResume: true,
        opencodePort: result.opencodePort,
        idleSince: undefined, // Clear any previous idle state
      };

      if (result.proc) {
        this.processManager.add(task.id, newRunningTask, result.proc);
      } else if (result.windowName || result.paneId) {
        // TUI/Dashboard mode: track separately since we can't get proc handle
        this.tuiTasks.set(task.id, newRunningTask);
      }

      this.emitEvent({ type: "task_started", task: newRunningTask });
    } catch (error) {
      this.logger.error("Failed to resume task", {
        taskId: runningTask.id,
        error: String(error),
      });
    }
  }

  // ========================================
  // Manual Task Execution
  // ========================================

  /**
   * Execute a task manually from the TUI.
   * This allows users to run a specific task on-demand via the 'x' key.
   *
   * @param taskId - The task ID
   * @param taskPath - The task path in the brain
   * @returns true if task was successfully spawned, false otherwise
   */
  async executeTaskManually(taskId: string, taskPath: string): Promise<boolean> {
    // Step 1: Check capacity
    const runningCount = this.processManager.runningCount() + this.tuiTasks.size;
    if (runningCount >= this.config.maxParallel) {
      this.logger.warn("Cannot execute task manually: at max parallel capacity", {
        taskId,
        running: runningCount,
        max: this.config.maxParallel,
      });
      this.tuiLog('error', `Cannot execute: at max parallel capacity (${runningCount}/${this.config.maxParallel})`, taskId);
      return false;
    }

    // Step 2: Fetch task by path
    const task = await this.apiClient.getTaskByPath(taskPath);
    if (!task) {
      this.logger.warn("Cannot execute task manually: task not found", { taskId, taskPath });
      this.tuiLog('error', `Cannot execute: task not found`, taskId);
      return false;
    }

    // Step 3: Determine project ID from task path (path format: projects/<project>/task/<id>.md)
    const pathParts = taskPath.split('/');
    const projectFromPath = pathParts.length >= 2 ? pathParts[1] : this.projectId;
    const effectiveProjectId = projectFromPath || this.projectId;

    // Step 4: Claim task
    const claim = await this.apiClient.claimTask(
      effectiveProjectId,
      taskId,
      this.runnerId
    );

    if (!claim.success) {
      this.logger.info("Cannot execute task manually: claim failed", {
        taskId,
        claimedBy: claim.claimedBy,
      });
      this.tuiLog('warn', `Cannot execute: already claimed by ${claim.claimedBy}`, taskId);
      return false;
    }

    // Step 5: Update status to in_progress
    try {
      await this.apiClient.updateTaskStatus(taskPath, "in_progress");
    } catch (error) {
      this.logger.error("Failed to update task status for manual execution", {
        taskId,
        error: String(error),
      });
      await this.apiClient.releaseTask(effectiveProjectId, taskId);
      return false;
    }

    // Step 6: Resolve workdir (may create worktree if needed)
    let workdir: string;
    try {
      workdir = await this.executor.resolveWorkdir(task);
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);

      this.logger.error("Worktree setup failed for manual execution", {
        taskId,
        error: errorMessage,
      });

      try {
        await this.apiClient.updateTaskStatus(taskPath, "blocked");

        const sanitizedBranch = task.git_branch?.replace(/\//g, "-") ?? "unknown";
        const blockNote =
          `\n\n## Blocked: Worktree Setup Failed\n\n` +
          `**Time:** ${new Date().toISOString()}\n` +
          `**Error:** ${errorMessage}\n\n` +
          `The task runner attempted to create a git worktree for branch \`${task.git_branch}\` ` +
          `but setup failed. Please:\n` +
          `1. Check if the branch exists and is valid\n` +
          `2. Manually create the worktree: \`git worktree add .worktrees/${sanitizedBranch} ${task.git_branch}\`\n` +
          `3. Run any necessary setup (npm install, etc.)\n` +
          `4. Update this task to \`pending\` to retry\n`;

        await this.apiClient.appendToTask(taskPath, blockNote);
      } catch (updateError) {
        this.logger.error("Failed to update task status after worktree failure", {
          taskId,
          error: String(updateError),
        });
      }

      await this.apiClient.releaseTask(effectiveProjectId, taskId);
      this.tuiLog("error", `Worktree setup failed: ${task.title}`, taskId, effectiveProjectId);
      return false;
    }

    // Step 7: Spawn OpenCode process
    const effectiveMode = this.tmuxManager ? "dashboard" : this.mode;
    const useTui = this.mode === "tui";

    try {
      const result = await this.executor.spawn(task, effectiveProjectId, {
        mode: effectiveMode,
        paneId: this.tmuxManager?.getLayout()?.taskAreaPaneId,
        isResume: false,
        useTui,
        workdir, // Pass pre-resolved workdir
      });

      // Step 8: Track the process
      const runningTask: RunningTask = {
        id: taskId,
        path: taskPath,
        title: task.title,
        priority: task.priority,
        projectId: effectiveProjectId,
        pid: result.pid,
        paneId: result.paneId,
        windowName: result.windowName,
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir,
        opencodePort: result.opencodePort,
      };

      // Track the task: either in processManager (with proc) or tuiTasks (TUI mode)
      if (result.proc) {
        this.processManager.add(taskId, runningTask, result.proc);
      } else if (result.windowName) {
        this.tuiTasks.set(taskId, runningTask);
      }

      this.logger.info("Task manually executed", {
        taskId,
        title: task.title,
        pid: result.pid,
        projectId: effectiveProjectId,
      });
      this.tuiLog('info', `Task started manually: ${task.title}`, taskId, effectiveProjectId);

      // Handle dashboard task pane
      await this.handleDashboardTaskStart(runningTask);

      // Emit event
      this.emitEvent({ type: "task_started", task: runningTask });

      return true;
    } catch (error) {
      this.logger.error("Failed to spawn task for manual execution", {
        taskId,
        error: String(error),
      });
      await this.apiClient.releaseTask(effectiveProjectId, taskId);
      return false;
    }
  }

  // ========================================
  // Task Cancellation
  // ========================================

  /**
   * Cancel a task by ID.
   * - Kills the running process if active (explicit PID kill for TUI tasks)
   * - Updates task status to 'cancelled' in brain
   * - Releases any claim
   * - Child tasks will be blocked on next poll
   */
  async cancelTask(taskId: string, taskPath: string): Promise<void> {
    this.logger.info("Cancelling task", { taskId, taskPath });

    // 1. Check if task is running in processManager
    const processInfo = this.processManager.get(taskId);
    if (processInfo) {
      // Kill the process
      await this.processManager.kill(taskId);
      this.processManager.remove(taskId);
      this.logger.info("Killed running process for task", { taskId, pid: processInfo.proc.pid });
    }

    // 2. Check if task is in TUI tasks (tmux windows without proc handles)
    const tuiTask = this.tuiTasks.get(taskId);
    if (tuiTask) {
      // Explicitly kill the PID before tmux cleanup (same pattern as stop() method)
      // SAFETY: Guard against dangerous PIDs that could kill multiple processes
      // PID 0 = current process group, PID -1 = ALL user processes (catastrophic!)
      if (tuiTask.pid > 0 && isPidAlive(tuiTask.pid)) {
        this.logger.info("Killing TUI task process", { taskId, pid: tuiTask.pid });
        this.tuiLog('warn', `Killing process ${tuiTask.pid} for task ${tuiTask.title}`, taskId, tuiTask.projectId);
        
        try {
          // Send SIGTERM first for graceful shutdown
          process.kill(tuiTask.pid, "SIGTERM");
          
          // Wait up to 2 seconds for graceful shutdown
          const startWait = Date.now();
          while (isPidAlive(tuiTask.pid) && Date.now() - startWait < 2000) {
            await this.sleep(100);
          }
          
          // If still alive, force kill with SIGKILL
          if (isPidAlive(tuiTask.pid)) {
            this.logger.warn("Process did not terminate gracefully, sending SIGKILL", {
              taskId,
              pid: tuiTask.pid,
            });
            this.tuiLog('warn', `Force killing process ${tuiTask.pid}`, taskId, tuiTask.projectId);
            process.kill(tuiTask.pid, "SIGKILL");
          }
          
          this.logger.info("Killed TUI task process", { taskId, pid: tuiTask.pid });
        } catch (err) {
          // Process may have exited between check and kill
          this.logger.debug("Error killing process (may have already exited)", {
            taskId,
            pid: tuiTask.pid,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      } else if (tuiTask.pid <= 0) {
        this.logger.warn("Skipping invalid PID in TUI task cancellation", {
          taskId,
          pid: tuiTask.pid,
        });
      }
      
      // Then clean up the tmux window
      await this.cleanupTaskTmux(tuiTask);
      this.tuiTasks.delete(taskId);
      this.logger.info("Cleaned up TUI task window", { taskId });
    }

    // 3. Update status to 'cancelled' in brain
    try {
      await this.apiClient.updateTaskStatus(taskPath, "cancelled");
      this.logger.info("Updated task status to cancelled", { taskId });
    } catch (error) {
      this.logger.error("Failed to update task status to cancelled", {
        taskId,
        error: String(error),
      });
    }

    // 4. Release any claim (use task's projectId if available)
    const taskProjectId = processInfo?.task.projectId ?? tuiTask?.projectId ?? this.projectId;
    try {
      await this.apiClient.releaseTask(taskProjectId, taskId);
    } catch (error) {
      this.logger.error("Failed to release task claim", {
        taskId,
        projectId: taskProjectId,
        error: String(error),
      });
    }

    // 5. Append cancellation note to task
    try {
      const cancelNote = `\n\n## Cancelled\n\n**Time:** ${new Date().toISOString()}\nTask was cancelled by user.\n`;
      await this.apiClient.appendToTask(taskPath, cancelNote);
    } catch (error) {
      this.logger.error("Failed to append cancellation note", {
        taskId,
        error: String(error),
      });
    }

    // 6. Update stats
    this.stats.failed++;

    // 7. Emit event
    this.emitEvent({ type: "task_cancelled", taskId, taskPath });

    // 8. Log to TUI
    this.tuiLog('warn', `Task cancelled: ${taskId}`, taskId, taskProjectId);

    // 9. Save state
    this.saveState();
  }

  /**
   * Update a task's status via the API.
   * Called from TUI when user changes status via the popup.
   */
  async updateTaskStatus(taskId: string, taskPath: string, newStatus: EntryStatus): Promise<void> {
    this.logger.info("Updating task status", { taskId, taskPath, newStatus });

    try {
      await this.apiClient.updateTaskStatus(taskPath, newStatus);
      this.logger.info("Task status updated", { taskId, newStatus });
      this.tuiLog('info', `Task status changed to ${newStatus}: ${taskId}`, taskId);
    } catch (error) {
      this.logger.error("Failed to update task status", {
        taskId,
        newStatus,
        error: String(error),
      });
      throw error;
    }
  }

  /**
   * Edit a task in an external editor.
   * Fetches task content, writes to temp file, spawns $EDITOR, reads back and syncs.
   * Returns the new content if changes were made, null if cancelled/unchanged.
   */
  async editTask(taskId: string, taskPath: string): Promise<string | null> {
    this.logger.info("Editing task in external editor", { taskId, taskPath });

    // Import necessary modules
    const { spawnSync } = await import("child_process");
    const { writeFileSync, readFileSync, unlinkSync, mkdtempSync } = await import("fs");
    const { tmpdir } = await import("os");
    const { join } = await import("path");

    try {
      // Step 1: Fetch task content from API
      const entry = await this.apiClient.getEntry(taskPath);
      const originalContent = entry.content;

      // Step 2: Write content to temp file
      const tempDir = mkdtempSync(join(tmpdir(), "brain-task-"));
      const tempFile = join(tempDir, `${taskId}.md`);
      writeFileSync(tempFile, originalContent, "utf-8");

      // Step 3: Get editor from environment
      const editor = process.env.EDITOR || process.env.VISUAL || "vim";

      // Step 4: Exit alternate screen buffer (restore normal terminal)
      process.stdout.write("\x1b[?1049l");

      // Step 5: Spawn editor synchronously
      this.logger.info("Spawning editor", { editor, tempFile });
      const result = spawnSync(editor, [tempFile], {
        stdio: "inherit",
        env: process.env,
      });

      // Step 6: Re-enter alternate screen buffer
      process.stdout.write("\x1b[?1049h");
      process.stdout.write("\x1b[H");
      process.stdout.write("\x1b[2J");

      // Step 7: Check if editor exited successfully
      if (result.status !== 0) {
        this.logger.warn("Editor exited with non-zero status", { status: result.status });
        // Clean up temp file
        try { unlinkSync(tempFile); } catch { /* ignore */ }
        return null;
      }

      // Step 8: Read modified content
      const newContent = readFileSync(tempFile, "utf-8");

      // Step 9: Clean up temp file
      try { unlinkSync(tempFile); } catch { /* ignore */ }

      // Step 10: Check if content changed
      if (newContent === originalContent) {
        this.logger.info("No changes made to task content", { taskId });
        return null;
      }

      // Step 11: Sync changes back to brain API
      await this.apiClient.updateEntryContent(taskPath, newContent);
      this.logger.info("Task content updated from editor", { taskId });
      this.tuiLog("info", `Task edited: ${taskId}`, taskId);

      return newContent;
    } catch (error) {
      this.logger.error("Failed to edit task", {
        taskId,
        error: String(error),
      });
      // Re-enter alternate screen in case of early error
      process.stdout.write("\x1b[?1049h");
      throw error;
    }
  }

  // ========================================
  // Dashboard Management
  // ========================================

  private async initializeDashboard(): Promise<void> {
    // Use Ink TUI for "tui" mode, tmux bash dashboard for "dashboard" mode
    if (this.mode === "tui") {
      await this.initializeInkTui();
    } else {
      await this.initializeTmuxDashboard();
    }
  }

  /**
   * Initialize the new Ink-based TUI dashboard.
   * Runs in the main terminal, not in tmux panes.
   */
  private async initializeInkTui(): Promise<void> {
    try {
      this.tuiDashboard = startDashboard({
        projects: this.projects,       // Pass all projects for multi-project mode
        projectId: this.projectId,     // Legacy single project (backward compat)
        apiUrl: this.config.brainApiUrl,
        pollInterval: this.config.pollInterval * 1000, // Convert to ms
        maxLogs: 100,
        logDir: this.config.logDir,    // Enable log persistence across TUI restarts
        onExit: () => {
          this.logger.info("TUI dashboard exited");
          // Trigger graceful shutdown when user quits TUI
          this.stop();
        },
        onLogCallback: (addLog) => {
          // Store the addLog function for pushing logs to TUI
          this.tuiAddLog = addLog;
          this.logger.info("TUI log callback registered");
        },
        onCancelTask: (taskId, taskPath) => this.cancelTask(taskId, taskPath),
        onUpdateStatus: (taskId, taskPath, newStatus) => this.updateTaskStatus(taskId, taskPath, newStatus),
        onEditTask: (taskId, taskPath) => this.editTask(taskId, taskPath),
        onExecuteTask: (taskId, taskPath) => this.executeTaskManually(taskId, taskPath),
        // Pause/resume callbacks (async — persisted via root task status)
        onPause: (projectId) => this.pause(projectId),
        onResume: (projectId) => this.resume(projectId),
        onPauseAll: () => this.pauseAll(),
        onResumeAll: () => this.resumeAll(),
        getPausedProjects: () => this.getPausedProjects(),
        getRunningProcessCount: () => this.processManager.runningCount() + this.tuiTasks.size,
        getResourceMetrics: () => this.getResourceMetrics(),
        getProjectLimits: () => this.projects.map(projectId => ({
          projectId,
          limit: this.getProjectLimit(projectId),
          running: this.getRunningCountForProject(projectId),
        })),
        setProjectLimit: (projectId, limit) => this.setProjectLimit(projectId, limit),
        // Feature enable/disable callbacks (whitelist for paused projects)
        onEnableFeature: (featureId) => this.enableFeature(featureId),
        onDisableFeature: (featureId) => this.disableFeature(featureId),
        getEnabledFeatures: () => this.getEnabledFeatures(),
      });

      this.logger.info("Ink TUI dashboard initialized", { 
        projects: this.projects,
        projectCount: this.projects.length,
        isMultiProject: this.isMultiProject,
      });
    } catch (error) {
      this.logger.warn("Failed to start Ink TUI, continuing without dashboard", {
        error: String(error),
      });
      this.tuiDashboard = null;
    }
  }

  /**
   * Initialize the legacy tmux-based bash dashboard.
   * Creates panes with bash/jq scripts for task list rendering.
   */
  private async initializeTmuxDashboard(): Promise<void> {
    this.tmuxManager = getTmuxManager();

    try {
      await this.tmuxManager.createDashboard(this.projectId);
      
      // Start periodic status updates (every 5 seconds)
      this.startDashboardStatusUpdates();

      this.logger.info("Tmux dashboard initialized", { projectId: this.projectId });
    } catch (error) {
      this.logger.warn("Failed to create tmux dashboard, continuing without it", {
        error: String(error),
      });
      this.tmuxManager = null;
    }
  }

  private startDashboardStatusUpdates(): void {
    if (!this.tmuxManager) return;

    // Start with the TmuxManager's built-in status updates
    this.tmuxManager.startStatusUpdates(5000);

    // Start pane monitoring to detect when OpenCode finishes
    this.tmuxManager.startPaneMonitoring(2000);

    // Register callback for when panes close (OpenCode completed)
    this.tmuxManager.onPaneClosed(async (taskId: string, paneId: string) => {
      this.logger.info("Task pane closed", { taskId, paneId });
      await this.handleDashboardPaneClosed(taskId);
    });

    // Set up an event handler to update status on each poll
    this.on(async (event) => {
      if (event.type === "poll_complete" && this.tmuxManager) {
        const status = await this.getDashboardStatus();
        await this.tmuxManager.updateStatusPane(status);
      }
    });
  }

  private async getDashboardStatus(): Promise<StatusInfo> {
    // Get task counts from API
    let readyCount = 0;
    let waitingCount = 0;
    let blockedCount = 0;

    try {
      const [ready, waiting, blocked] = await Promise.all([
        this.apiClient.getReadyTasks(this.projectId),
        this.apiClient.getWaitingTasks(this.projectId),
        this.apiClient.getBlockedTasks(this.projectId),
      ]);
      readyCount = ready.length;
      waitingCount = waiting.length;
      blockedCount = blocked.length;
    } catch (error) {
      this.logger.debug("Failed to get task counts for dashboard", {
        error: String(error),
      });
    }

    // Get recent completions from running tasks that just completed
    const recentCompletions: string[] = [];

    return {
      projectId: this.projectId,
      status: this.status,
      ready: readyCount,
      running: this.processManager.runningCount(),
      waiting: waitingCount,
      blocked: blockedCount,
      completed: this.stats.completed,
      recentCompletions,
    };
  }

  private async handleDashboardTaskStart(task: RunningTask): Promise<void> {
    if (!this.tmuxManager) return;

    // Note: The pane is already created by OpencodeExecutor.spawnDashboard().
    // This method only registers the pane for tracking (for removal on completion).
    // The pane ID is stored in task.paneId from executor.spawn() result.
    if (task.paneId) {
      this.tmuxManager.registerTaskPane(task.id, task.paneId, task.title);
      this.logger.debug("Registered dashboard pane for task", {
        taskId: task.id,
        paneId: task.paneId,
      });
    }
  }

  private async handleDashboardTaskComplete(taskId: string): Promise<void> {
    if (!this.tmuxManager) return;

    await this.tmuxManager.removeTaskPane(taskId);
  }

  /**
   * Handle when a dashboard pane closes (detected by TmuxManager).
   * This is how we detect task completion in dashboard mode since we don't
   * have direct process handles.
   */
  private async handleDashboardPaneClosed(taskId: string): Promise<void> {
    // Check if task is still tracked as running (could have been cleaned up already)
    const paneInfo = this.tmuxManager?.getTaskPane(taskId);
    
    // Get task status from the brain API to determine completion status
    let completionStatus: CompletionStatus = CompletionStatus.Completed;
    
    // Find the task path from our tracked panes or running tasks
    const runningInfo = this.processManager.get(taskId);
    const taskPath = runningInfo?.task.path;
    
    if (taskPath) {
      try {
        const encodedPath = encodeURIComponent(taskPath);
        const response = await fetch(
          `${this.config.brainApiUrl}/api/v1/entries/${encodedPath}`
        );
        if (response.ok) {
          const entry = await response.json();
          const status = entry.status as EntryStatus;
          
          if (status === "completed") {
            completionStatus = CompletionStatus.Completed;
          } else if (status === "blocked") {
            completionStatus = CompletionStatus.Blocked;
          } else if (status === "cancelled") {
            completionStatus = CompletionStatus.Cancelled;
          } else if (status === "in_progress") {
            // Process exited while still in_progress - likely crashed
            completionStatus = CompletionStatus.Crashed;
          }
        }
      } catch (error) {
        this.logger.debug("Failed to check task status after pane closed", {
          taskId,
          error: String(error),
        });
      }
    }

    // Handle completion via the standard path if we have process info
    if (runningInfo) {
      await this.handleTaskCompletion(taskId, completionStatus);
    } else {
      // Task wasn't in processManager (common in dashboard mode)
      // Just update stats and emit event
      if (completionStatus === CompletionStatus.Completed) {
        this.stats.completed++;
        this.logger.info("Dashboard task completed", { taskId });
        this.emitEvent({
          type: "task_completed",
          result: {
            taskId,
            status: "completed",
            startedAt: new Date().toISOString(),
            completedAt: new Date().toISOString(),
            duration: 0,
          },
        });
      } else if (completionStatus === CompletionStatus.Cancelled) {
        this.stats.failed++;
        this.logger.warn("Dashboard task cancelled", { taskId });
        this.emitEvent({
          type: "task_cancelled",
          taskId,
          taskPath: taskPath ?? "",
        });
      } else {
        this.stats.failed++;
        this.logger.warn("Dashboard task failed", { taskId, status: completionStatus });
        this.emitEvent({
          type: "task_failed",
          result: {
            taskId,
            status: completionStatus === CompletionStatus.Blocked ? "blocked" : "crashed",
            startedAt: new Date().toISOString(),
            completedAt: new Date().toISOString(),
            duration: 0,
          },
        });
      }

      // Release the claim
      try {
        await this.apiClient.releaseTask(this.projectId, taskId);
      } catch (error) {
        this.logger.error("Failed to release task after pane closed", {
          taskId,
          error: String(error),
        });
      }

      // Cleanup executor files
      await this.executor.cleanup(taskId, this.projectId);
    }

    // Save state
    this.saveState();
  }

  /**
   * Clean up tmux window/pane when a task completes.
   * Handles both TUI mode (standalone windows) and dashboard mode (panes).
   * 
   * Sends Ctrl+C to gracefully stop OpenCode before killing the window/pane.
   * This ensures the OpenCode process has a chance to clean up properly.
   */
  private async cleanupTaskTmux(task: RunningTask): Promise<void> {
    // For TUI mode: close the standalone window
    if (task.windowName) {
      await this.gracefulTmuxCleanup(
        task.id,
        task.windowName,
        "window",
        async (target) => Bun.$`tmux kill-window -t ${target}`.quiet()
      );
    }

    // Fallback for panes without tmuxManager
    if (task.paneId && !this.tmuxManager) {
      await this.gracefulTmuxCleanup(
        task.id,
        task.paneId,
        "pane",
        async (target) => Bun.$`tmux kill-pane -t ${target}`.quiet()
      );
    }
  }

  /**
   * Gracefully clean up a tmux window or pane.
   * Sends Ctrl+C first to allow OpenCode to shut down gracefully,
   * then kills the window/pane after a brief delay.
   */
  private async gracefulTmuxCleanup(
    taskId: string,
    target: string,
    type: "window" | "pane",
    killFn: (target: string) => Promise<unknown>
  ): Promise<void> {
    // Graceful shutdown delay (ms) - time to wait after Ctrl+C before force-killing
    // 500ms gives OpenCode enough time to handle the interrupt signal
    const GRACEFUL_SHUTDOWN_DELAY = 500;

    // Step 1: Send Ctrl+C to gracefully stop OpenCode
    try {
      this.logger.debug(`Sending Ctrl+C to gracefully stop OpenCode`, {
        taskId,
        [type === "window" ? "windowName" : "paneId"]: target,
      });
      await Bun.$`tmux send-keys -t ${target} C-c`.quiet();
      // Wait briefly for graceful shutdown
      await this.sleep(GRACEFUL_SHUTDOWN_DELAY);
    } catch {
      // send-keys might fail if window/pane is already closed, continue to kill
      this.logger.debug(`Failed to send Ctrl+C, ${type} may already be closed`, {
        [type === "window" ? "windowName" : "paneId"]: target,
      });
    }

    // Step 2: Kill the window/pane
    try {
      this.logger.debug(`Closing tmux ${type}`, {
        taskId,
        [type === "window" ? "windowName" : "paneId"]: target,
      });
      await killFn(target);
    } catch {
      // Window/pane might already be closed
      this.logger.debug(`${type.charAt(0).toUpperCase() + type.slice(1)} already closed`, {
        [type === "window" ? "windowName" : "paneId"]: target,
      });
    }
  }

  // ========================================
  // Signal Handling
  // ========================================

  private setupSignalHandler(): void {
    this.signalHandler = setupSignalHandler(
      {
        stateDir: this.config.stateDir,
        projectId: this.projectId,
        onEvent: (event) => this.emitEvent(event),
        getRunningTasks: () =>
          this.processManager.getAll().map((info) => info.task),
        getStats: () => this.stats,
        getStartedAt: () => this.startedAt ?? new Date().toISOString(),
        // onShutdown callback for graceful cleanup including tuiTasks
        onShutdown: async () => {
          await this.stop();
        },
      },
      this.processManager
    );
  }

  // ========================================
  // State Persistence
  // ========================================

  private saveState(): void {
    try {
      const runningTasks = this.processManager.getAll().map((info) => info.task);

      this.stateManager.save(
        this.status,
        runningTasks,
        this.stats,
        this.startedAt ?? new Date().toISOString()
      );

      this.emitEvent({
        type: "state_saved",
        path: this.stateManager["stateFile"],
      });
    } catch (error) {
      this.logger.error("Failed to save state", { error: String(error) });
    }
  }

  // ========================================
  // Utilities
  // ========================================

  private generateRunnerId(): string {
    return `runner_${randomBytes(4).toString("hex")}`;
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  /**
   * Push a log entry to the TUI dashboard (if active).
   * This allows runner logs to appear in the TUI log panel.
   */
  private tuiLog(
    level: 'debug' | 'info' | 'warn' | 'error',
    message: string,
    taskId?: string,
    projectId?: string
  ): void {
    if (this.tuiAddLog) {
      this.tuiAddLog({ level, message, taskId, projectId });
    }
  }

  /**
   * Build a markdown failure note for appending to a task.
   */
  private buildFailureNote(result: TaskResult): string {
    const timestamp = new Date().toISOString();
    let note = `\n\n## Failure\n\n**Status:** ${result.status}\n**Time:** ${timestamp}\n`;

    if (result.exitCode !== undefined) {
      note += `**Exit Code:** ${result.exitCode}\n`;
    }

    if (result.status === "timeout") {
      note += `\nTask exceeded the configured timeout limit.\n`;
    } else if (result.status === "crashed") {
      note += `\nTask process crashed unexpectedly.\n`;
    } else if (result.status === "blocked") {
      note += `\nTask was marked as blocked.\n`;
    }

    return note;
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let taskRunnerInstance: TaskRunner | null = null;

export function getTaskRunner(options?: TaskRunnerOptions): TaskRunner {
  if (!taskRunnerInstance && options) {
    taskRunnerInstance = new TaskRunner(options);
  }
  if (!taskRunnerInstance) {
    throw new Error(
      "TaskRunner not initialized. Call with options first."
    );
  }
  return taskRunnerInstance;
}

/**
 * Get the task runner singleton if it exists, or null if not initialized.
 * Use this for optional access (e.g., API endpoints that may run without a runner).
 */
export function getTaskRunnerOrNull(): TaskRunner | null {
  return taskRunnerInstance;
}

/**
 * Reset the task runner singleton (useful for testing).
 */
export function resetTaskRunner(): void {
  taskRunnerInstance = null;
}
