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

// =============================================================================
// Types
// =============================================================================

export interface TaskRunnerOptions {
  projectId: string;
  config?: RunnerConfig;
  mode?: ExecutionMode;
}

export interface RunnerStatusInfo {
  status: RunnerStatus;
  projectId: string;
  runnerId: string;
  startedAt: string | null;
  runningTasks: RunningTask[];
  stats: RunnerStats;
}

// =============================================================================
// Task Runner Class
// =============================================================================

export class TaskRunner {
  // Core identity
  private readonly projectId: string;
  private readonly runnerId: string;
  private readonly mode: ExecutionMode;

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

  // Event handling
  private eventHandlers: EventHandler[] = [];

  // Logger
  private readonly logger = getLogger();

  constructor(options: TaskRunnerOptions) {
    this.projectId = options.projectId;
    this.runnerId = this.generateRunnerId();
    this.mode = options.mode ?? "background";
    this.config = options.config ?? getRunnerConfig();

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

    this.logger.info("Starting runner", {
      projectId: this.projectId,
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
    };
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

      // Step 3: Get ready tasks
      const readyTasks = await this.apiClient.getReadyTasks(this.projectId);

      // Step 4: Filter out already running tasks (from both processManager and tuiTasks)
      const runningIds = new Set([
        ...this.processManager.getAll().map((info) => info.task.id),
        ...Array.from(this.tuiTasks.keys()),
      ]);
      const availableTasks = readyTasks.filter(
        (task) => !runningIds.has(task.id)
      );

      if (availableTasks.length === 0) {
        if (isDebugEnabled()) {
          this.logger.debug("No available tasks to start");
        }
        this.emitEvent({
          type: "poll_complete",
          readyCount: 0,
          runningCount,
        });
        return;
      }

      // Step 5: Start tasks up to capacity
      const slotsAvailable = this.config.maxParallel - runningCount;
      const tasksToStart = availableTasks.slice(0, slotsAvailable);

      for (const task of tasksToStart) {
        await this.claimAndSpawn(task);
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

  private async claimAndSpawn(task: ResolvedTask): Promise<boolean> {
    // Step 1: Claim task via API
    const claim = await this.apiClient.claimTask(
      this.projectId,
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
      const result = await this.executor.spawn(task, this.projectId, {
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
        projectId: this.projectId,
        pid: result.pid,
        paneId: result.paneId,
        windowName: result.windowName,
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir: this.executor.resolveWorkdir(task),
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
      });
      this.tuiLog('info', `Task started: ${task.title}`, task.id);

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
      await this.apiClient.releaseTask(this.projectId, task.id);
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

        if (status === "completed" || status === "blocked") {
          const completionStatus = status === "completed" 
            ? CompletionStatus.Completed 
            : CompletionStatus.Blocked;
          
          await this.handleTuiTaskCompletion(taskId, task, completionStatus);
        }
      } catch (error) {
        this.logger.debug("Failed to check TUI task status", {
          taskId,
          error: String(error),
        });
      }
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
      });
      this.tuiLog('info', `Task completed: ${info.task.title} (${Math.round(result.duration / 1000)}s)`, taskId);
    } else {
      this.stats.failed++;
      this.emitEvent({ type: "task_failed", result });
      this.logger.warn("Task failed", {
        taskId,
        title: info.task.title,
        status: result.status,
        exitCode: result.exitCode,
      });
      this.tuiLog('error', `Task failed: ${info.task.title} (${result.status})`, taskId);

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
      });
      this.tuiLog('info', `Task completed: ${task.title} (${Math.round(duration / 1000)}s)`, taskId);
    } else {
      this.stats.failed++;
      this.emitEvent({ type: "task_failed", result });
      this.logger.warn("TUI task failed", {
        taskId,
        title: task.title,
        status: result.status,
      });
      this.tuiLog('error', `Task failed: ${task.title} (${result.status})`, taskId);
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
    // Load previous state
    const prevState = this.stateManager.load();
    if (!prevState || prevState.runningTasks.length === 0) {
      return;
    }

    this.logger.info("Found interrupted tasks", {
      count: prevState.runningTasks.length,
    });

    // Restore stats from previous run
    this.stats = {
      completed: prevState.stats.completed,
      failed: prevState.stats.failed,
      totalRuntime: prevState.stats.totalRuntime,
    };

    // Try to resume each interrupted task
    for (const task of prevState.runningTasks) {
      await this.resumeTask(task);
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
      };

      if (result.proc) {
        this.processManager.add(task.id, newRunningTask, result.proc);
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
        projectId: this.projectId,
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
      });

      this.logger.info("Ink TUI dashboard initialized", { projectId: this.projectId });
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
   */
  private async cleanupTaskTmux(task: RunningTask): Promise<void> {
    // For TUI mode: close the standalone window
    if (task.windowName) {
      try {
        this.logger.debug("Closing tmux window", {
          taskId: task.id,
          windowName: task.windowName,
        });
        await Bun.$`tmux kill-window -t ${task.windowName}`.quiet();
      } catch {
        // Window might already be closed
        this.logger.debug("Window already closed", { windowName: task.windowName });
      }
    }

    // Fallback for panes without tmuxManager
    if (task.paneId && !this.tmuxManager) {
      try {
        await Bun.$`tmux kill-pane -t ${task.paneId}`.quiet();
      } catch {
        // Pane might already be closed
      }
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
    taskId?: string
  ): void {
    if (this.tuiAddLog) {
      this.tuiAddLog({ level, message, taskId });
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
 * Reset the task runner singleton (useful for testing).
 */
export function resetTaskRunner(): void {
  taskRunnerInstance = null;
}
