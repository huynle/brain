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

  // Pause state: persisted via project root task status (active = running, blocked = paused)
  // Local cache for synchronous access in poll loop; synced on pause/resume calls
  private pauseCache: Set<string> = new Set();
  private readonly startPaused: boolean;

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

    // Check for interrupted tasks and try to resume
    await this.handleInterruptedTasks();

    // Save initial state
    this.saveState();

    // Start polling loop
    this.schedulePoll();

    // If configured to start paused, pause all projects immediately
    if (this.startPaused) {
      await this.pauseAll();
      this.logger.info("Runner started paused - press 'P' to begin processing", {
        projectCount: this.projects.length,
      });
      this.tuiLog('warn', "Runner started paused - press 'P' to begin processing");
    }
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

    // Clean up TUI mode tasks (tmux windows)
    if (this.tuiTasks.size > 0) {
      this.logger.info("Cleaning up TUI task windows", {
        count: this.tuiTasks.size,
      });
      for (const task of this.tuiTasks.values()) {
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

  // ========================================
  // Pause/Resume Methods
  // ========================================

  /**
   * Pause a specific project.
   * Sets the project root task to status: blocked, which blocks all children via depends_on cascade.
   * Running tasks will complete, but no new tasks will be started.
   */
  async pause(projectId: string): Promise<void> {
    if (!this.projects.includes(projectId)) {
      this.logger.warn("Attempted to pause unknown project", { projectId });
      return;
    }
    if (this.pauseCache.has(projectId)) {
      return; // Already paused
    }

    try {
      const rootPath = await this.findProjectRootPath(projectId);
      if (rootPath) {
        await this.apiClient.updateTaskStatus(rootPath, "blocked");
      }
    } catch (error) {
      this.logger.error("Failed to persist pause state to root task", {
        projectId,
        error: String(error),
      });
    }

    this.pauseCache.add(projectId);
    this.logger.info("Project paused", { projectId });
    this.tuiLog('warn', `Project paused: ${projectId}`, undefined, projectId);
    this.emitEvent({ type: "project_paused", projectId });
  }

  /**
   * Resume a paused project.
   * Sets the project root task to status: active, which unblocks children.
   */
  async resume(projectId: string): Promise<void> {
    if (!this.pauseCache.has(projectId)) {
      return; // Not paused
    }

    try {
      const rootPath = await this.findProjectRootPath(projectId);
      if (rootPath) {
        await this.apiClient.updateTaskStatus(rootPath, "active");
      }
    } catch (error) {
      this.logger.error("Failed to persist resume state to root task", {
        projectId,
        error: String(error),
      });
    }

    this.pauseCache.delete(projectId);
    this.logger.info("Project resumed", { projectId });
    this.tuiLog('info', `Project resumed: ${projectId}`, undefined, projectId);
    this.emitEvent({ type: "project_resumed", projectId });
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
   */
  async resumeAll(): Promise<void> {
    const count = this.pauseCache.size;
    const pausedIds = Array.from(this.pauseCache);
    await Promise.allSettled(
      pausedIds.map(projectId => this.resume(projectId))
    );
    this.logger.info("All projects resumed", { count });
    this.tuiLog('info', `All projects resumed (${count})`);
    this.emitEvent({ type: "all_resumed" });
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

  /**
   * Find the path of a project's root task (title = projectId, no depends_on, status active or blocked).
   * Returns the task path for status updates, or null if not found.
   */
  private async findProjectRootPath(projectId: string): Promise<string | null> {
    try {
      const tasks = await this.apiClient.getAllTasks(projectId);
      const root = tasks.find(t =>
        t.title === projectId &&
        (!t.depends_on || t.depends_on.length === 0) &&
        (t.status === "active" || t.status === "blocked")
      );
      return root?.path ?? null;
    } catch (error) {
      this.logger.error("Failed to find project root task", {
        projectId,
        error: String(error),
      });
      return null;
    }
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

      // Step 3: Get ready tasks from non-paused projects in parallel
      const activeProjects = this.projects.filter(p => !this.pauseCache.has(p));
      if (activeProjects.length === 0) {
        // All projects are paused, skip fetching tasks
        if (isDebugEnabled()) {
          this.logger.debug("All projects paused, skipping task fetch");
        }
        return;
      }

      const readyTasksResults = await Promise.allSettled(
        activeProjects.map(async (projectId) => {
          const tasks = await this.apiClient.getReadyTasks(projectId);
          // Tag each task with its project ID for tracking
          return tasks.map(task => ({ ...task, _pollProjectId: projectId }));
        })
      );

      // Merge ready tasks from all projects (handle partial failures)
      const allReadyTasks: (ResolvedTask & { _pollProjectId: string })[] = [];
      for (const result of readyTasksResults) {
        if (result.status === 'fulfilled') {
          allReadyTasks.push(...result.value);
        }
      }

      // Step 4: Filter out already running tasks
      // Use composite key "projectId:taskId" to avoid collisions across projects
      const runningKeys = new Set([
        ...this.processManager.getAll().map((info) => `${info.task.projectId}:${info.task.id}`),
        ...Array.from(this.tuiTasks.values()).map((task) => `${task.projectId}:${task.id}`),
      ]);
      const availableTasks = allReadyTasks.filter(
        (task) => !runningKeys.has(`${task._pollProjectId}:${task.id}`)
      );

      if (availableTasks.length === 0) {
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

      // Step 5: Start tasks up to capacity (shared pool across all projects)
      const slotsAvailable = this.config.maxParallel - runningCount;
      const tasksToStart = availableTasks.slice(0, slotsAvailable);

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
        readyCount: availableTasks.length,
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

    // Step 3: Spawn OpenCode process
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
      });

      // Step 4: Track the process
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
        workdir: this.executor.resolveWorkdir(task),
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
      this.tuiLog('info', `Task started: ${task.title}`, task.id, effectiveProjectId);

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
        }
        // Note: "blocked" status is NOT treated as completion
        // Blocked tasks stay in tuiTasks to allow resume detection
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

    // Create result
    const result: TaskResult = {
      taskId,
      status: status === CompletionStatus.Completed ? "completed" : "blocked",
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
   * Resume them before picking up new work.
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
          });

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
            workdir: this.executor.resolveWorkdir(task),
          };

          await this.resumeTask(runningTask);
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
    });

    // Fetch current task state
    const task = await this.apiClient.getNextTask(this.projectId);
    if (!task || task.id !== runningTask.id) {
      // Task may have been completed or changed
      this.logger.info("Task no longer pending, skipping resume", {
        taskId: runningTask.id,
      });
      return;
    }

    // Spawn with resume flag
    // When dashboard is active (TUI mode), spawn as panes not windows
    // But preserve TUI flag to use interactive command
    const effectiveMode = this.tmuxManager ? "dashboard" : this.mode;
    const useTui = this.mode === "tui";
    try {
      const result = await this.executor.spawn(task, this.projectId, {
        mode: effectiveMode,
        paneId: this.tmuxManager?.getLayout()?.taskAreaPaneId,
        isResume: true,
        useTui,
      });

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
  // Task Cancellation
  // ========================================

  /**
   * Cancel a task by ID.
   * - Kills the running process if active
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
        // Pause/resume callbacks (async — persisted via root task status)
        onPause: (projectId) => this.pause(projectId),
        onResume: (projectId) => this.resume(projectId),
        onPauseAll: () => this.pauseAll(),
        onResumeAll: () => this.resumeAll(),
        getPausedProjects: () => this.getPausedProjects(),
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
