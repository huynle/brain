/**
 * Signal Handler
 *
 * Handle process signals for graceful shutdown. Ensures running tasks
 * are properly cleaned up when the runner is stopped.
 */

import type { RunnerStats, EventHandler, RunnerEvent, RunningTask } from "./types";
import { ProcessManager, getProcessManager } from "./process-manager";
import { StateManager } from "./state-manager";
import { isDebugEnabled, getRunnerConfig, loadConfig, resetConfig } from "./config";

// =============================================================================
// Types
// =============================================================================

export type ShutdownReason = "SIGTERM" | "SIGINT" | "manual" | "error";

export interface ShutdownState {
  isShuttingDown: boolean;
  reason: ShutdownReason | null;
  startedAt: string | null;
}

export interface SignalHandlerOptions {
  /** Directory for state persistence */
  stateDir: string;
  /** Project ID for state files */
  projectId: string;
  /** Timeout for waiting on running tasks (ms) */
  gracefulTimeout?: number;
  /** Timeout for force kill after SIGTERM (ms) */
  forceKillTimeout?: number;
  /** Event handler for shutdown events */
  onEvent?: EventHandler;
  /** Callback to get current running tasks */
  getRunningTasks?: () => RunningTask[];
  /** Callback to get current stats */
  getStats?: () => RunnerStats;
  /** Callback to get runner start time */
  getStartedAt?: () => string;
  /** Callback for graceful shutdown cleanup (e.g., TaskRunner.stop()) */
  onShutdown?: () => Promise<void>;
}

// =============================================================================
// Signal Handler
// =============================================================================

export class SignalHandler {
  private readonly stateManager: StateManager;
  private readonly processManager: ProcessManager;
  private readonly options: Required<SignalHandlerOptions>;
  private shutdownState: ShutdownState = {
    isShuttingDown: false,
    reason: null,
    startedAt: null,
  };
  private registered = false;

  // Bound handlers for proper removal
  private boundSigterm: () => void;
  private boundSigint: () => void;
  private boundSighup: () => void;

  constructor(options: SignalHandlerOptions, processManager?: ProcessManager) {
    this.options = {
      gracefulTimeout: 30000,
      forceKillTimeout: 5000,
      onEvent: () => {},
      getRunningTasks: () => [],
      getStats: () => ({ completed: 0, failed: 0, totalRuntime: 0 }),
      getStartedAt: () => new Date().toISOString(),
      onShutdown: async () => {},
      ...options,
    };

    this.stateManager = new StateManager(options.stateDir, options.projectId);
    this.processManager = processManager ?? getProcessManager();

    // Bind handlers once for proper removal
    this.boundSigterm = () => this.handleShutdown("SIGTERM");
    this.boundSigint = () => this.handleShutdown("SIGINT");
    this.boundSighup = () => this.handleReload();
  }

  // ========================================
  // Registration
  // ========================================

  /**
   * Register signal handlers.
   * Call this when the runner starts.
   */
  register(): void {
    if (this.registered) {
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Already registered, skipping");
      }
      return;
    }

    process.on("SIGTERM", this.boundSigterm);
    process.on("SIGINT", this.boundSigint);
    process.on("SIGHUP", this.boundSighup);

    this.registered = true;

    if (isDebugEnabled()) {
      console.log("[SignalHandler] Registered signal handlers (SIGTERM, SIGINT, SIGHUP)");
    }
  }

  /**
   * Unregister signal handlers.
   * Call this during shutdown to prevent double-handling.
   */
  unregister(): void {
    if (!this.registered) {
      return;
    }

    process.off("SIGTERM", this.boundSigterm);
    process.off("SIGINT", this.boundSigint);
    process.off("SIGHUP", this.boundSighup);

    this.registered = false;

    if (isDebugEnabled()) {
      console.log("[SignalHandler] Unregistered signal handlers");
    }
  }

  // ========================================
  // Shutdown
  // ========================================

  /**
   * Check if shutdown is in progress.
   */
  isShuttingDown(): boolean {
    return this.shutdownState.isShuttingDown;
  }

  /**
   * Get current shutdown state.
   */
  getShutdownState(): ShutdownState {
    return { ...this.shutdownState };
  }

  /**
   * Trigger graceful shutdown manually.
   */
  async shutdown(reason: ShutdownReason = "manual"): Promise<number> {
    return this.handleShutdown(reason);
  }

  /**
   * Handle SIGTERM or SIGINT - graceful shutdown.
   */
  private async handleShutdown(reason: ShutdownReason): Promise<number> {
    // Prevent double shutdown
    if (this.shutdownState.isShuttingDown) {
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Shutdown already in progress, ignoring signal");
      }
      return 1;
    }

    this.shutdownState = {
      isShuttingDown: true,
      reason,
      startedAt: new Date().toISOString(),
    };

    if (isDebugEnabled()) {
      console.log(`[SignalHandler] Received ${reason}, starting graceful shutdown`);
    }

    // Unregister to prevent double-handling
    this.unregister();

    let exitCode = 0;

    try {
      // Step 1: Emit shutdown event
      this.emitEvent({ type: "shutdown", reason });

      // Step 2: Call onShutdown callback for graceful cleanup (e.g., TaskRunner.stop())
      // This handles tuiTasks cleanup and other runner-specific teardown
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Calling onShutdown callback for graceful cleanup");
      }
      try {
        await this.options.onShutdown();
      } catch (error) {
        if (isDebugEnabled()) {
          console.log("[SignalHandler] Error in onShutdown callback:", error);
        }
      }

      // Step 3: Wait for running tasks to complete
      const runningCount = this.processManager.runningCount();
      if (runningCount > 0) {
        if (isDebugEnabled()) {
          console.log(`[SignalHandler] Waiting for ${runningCount} running task(s)...`);
        }

        // Wait up to gracefulTimeout for tasks to finish
        const allExited = await this.waitForTasks(this.options.gracefulTimeout);

        if (!allExited) {
          if (isDebugEnabled()) {
            console.log("[SignalHandler] Timeout waiting for tasks, sending SIGTERM to children");
          }

          // Step 4: Send SIGTERM to remaining children
          await this.processManager.killAll();

          // Step 5: Wait a bit more for graceful termination
          const stillRunning = await this.waitForTasks(this.options.forceKillTimeout);

          if (!stillRunning) {
            if (isDebugEnabled()) {
              console.log("[SignalHandler] Force killing remaining children");
            }
            // killAll already handles force kill internally
          }
        }
      }

      // Step 6: Save final state
      this.saveState("stopped");

      if (isDebugEnabled()) {
        console.log("[SignalHandler] Shutdown complete");
      }
    } catch (error) {
      exitCode = 1;
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Error during shutdown:", error);
      }
    }

    // Step 6: Exit
    // Note: In real usage, caller should call process.exit(exitCode)
    // We return the exit code instead for testability
    return exitCode;
  }

  // ========================================
  // Reload (SIGHUP)
  // ========================================

  /**
   * Handle SIGHUP - reload configuration.
   */
  private handleReload(): void {
    if (this.shutdownState.isShuttingDown) {
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Ignoring SIGHUP during shutdown");
      }
      return;
    }

    if (isDebugEnabled()) {
      console.log("[SignalHandler] Received SIGHUP, reloading configuration");
    }

    // Reset the config singleton to force reload
    resetConfig();

    // Trigger reload by getting config again
    const newConfig = loadConfig();

    if (isDebugEnabled()) {
      console.log("[SignalHandler] Configuration reloaded:", {
        brainApiUrl: newConfig.brainApiUrl,
        pollInterval: newConfig.pollInterval,
        maxParallel: newConfig.maxParallel,
      });
    }
  }

  // ========================================
  // Helpers
  // ========================================

  /**
   * Wait for all tasks to exit.
   * Returns true if all tasks exited within timeout.
   */
  private waitForTasks(timeout: number): Promise<boolean> {
    return new Promise((resolve) => {
      const checkInterval = 100;
      let elapsed = 0;

      const interval = setInterval(() => {
        if (this.processManager.runningCount() === 0) {
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

  /**
   * Save current state to disk.
   */
  private saveState(status: "stopped" | "idle"): void {
    try {
      const runningTasks = this.options.getRunningTasks();
      const stats = this.options.getStats();
      const startedAt = this.options.getStartedAt();

      this.stateManager.save(status, runningTasks, stats, startedAt);

      if (isDebugEnabled()) {
        console.log("[SignalHandler] State saved");
      }
    } catch (error) {
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Failed to save state:", error);
      }
    }
  }

  /**
   * Emit a runner event.
   */
  private emitEvent(event: RunnerEvent): void {
    try {
      this.options.onEvent(event);
    } catch (error) {
      if (isDebugEnabled()) {
        console.log("[SignalHandler] Error in event handler:", error);
      }
    }
  }
}

// =============================================================================
// Convenience Functions
// =============================================================================

let signalHandlerInstance: SignalHandler | null = null;

/**
 * Create and register a signal handler for the runner.
 * Returns the handler instance for manual control.
 */
export function setupSignalHandler(
  options: SignalHandlerOptions,
  processManager?: ProcessManager
): SignalHandler {
  if (signalHandlerInstance) {
    signalHandlerInstance.unregister();
  }

  signalHandlerInstance = new SignalHandler(options, processManager);
  signalHandlerInstance.register();

  return signalHandlerInstance;
}

/**
 * Get the current signal handler instance.
 */
export function getSignalHandler(): SignalHandler | null {
  return signalHandlerInstance;
}

/**
 * Reset the signal handler singleton (useful for testing).
 */
export function resetSignalHandler(): void {
  if (signalHandlerInstance) {
    signalHandlerInstance.unregister();
    signalHandlerInstance = null;
  }
}
