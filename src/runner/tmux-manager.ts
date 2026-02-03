/**
 * Tmux Manager
 *
 * Manages tmux dashboard layout for dashboard mode execution.
 * Provides visual interface showing task status and OpenCode panes.
 */

import { isDebugEnabled } from "./config";

// =============================================================================
// Types
// =============================================================================

export interface PaneInfo {
  taskId: string;
  paneId: string;
  title: string;
}

export interface DashboardLayout {
  sessionName: string;
  windowName: string;
  statusPaneId: string;
  logPaneId: string;
  taskPanes: PaneInfo[];
}

export interface StatusInfo {
  projectId: string;
  status: string;
  ready: number;
  running: number;
  waiting: number;
  blocked: number;
  completed: number;
  recentCompletions: string[];
}

// =============================================================================
// TmuxManager Class
// =============================================================================

export class TmuxManager {
  private layout: DashboardLayout | null = null;
  private statusUpdateInterval: ReturnType<typeof setInterval> | null = null;
  private currentStatus: StatusInfo | null = null;

  /**
   * Check if tmux is available on the system.
   */
  async isTmuxAvailable(): Promise<boolean> {
    try {
      const result = await Bun.$`which tmux`.quiet();
      return result.exitCode === 0;
    } catch {
      return false;
    }
  }

  /**
   * Check if we're currently inside a tmux session.
   */
  async isInsideTmux(): Promise<boolean> {
    return process.env.TMUX !== undefined;
  }

  /**
   * Create the dashboard layout with status and log panes.
   */
  async createDashboard(sessionName: string): Promise<DashboardLayout> {
    if (!await this.isTmuxAvailable()) {
      throw new Error("tmux is not available on this system");
    }

    const windowName = `dashboard-${sessionName}`;

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Creating dashboard: ${windowName}`);
    }

    // Check if we're inside tmux
    const insideTmux = await this.isInsideTmux();

    if (insideTmux) {
      // Create a new window in the current session
      await Bun.$`tmux new-window -d -n ${windowName}`.quiet();
    } else {
      // Create a new session with the dashboard window
      try {
        await Bun.$`tmux new-session -d -s ${sessionName} -n ${windowName}`.quiet();
      } catch {
        // Session might already exist, try to create window
        await Bun.$`tmux new-window -d -t ${sessionName} -n ${windowName}`.quiet();
      }
    }

    // Get the base pane ID
    const basePaneResult = await Bun.$`tmux list-panes -t ${windowName} -F '#{pane_id}'`.text();
    const basePaneId = basePaneResult.trim();

    // Split horizontally: left side for status/log, right side for tasks
    // Left pane (30%) - will be split vertically for status and log
    // Right pane (70%) - for task panes
    const leftPaneResult = await Bun.$`tmux split-window -t ${basePaneId} -h -d -p 70 -P -F '#{pane_id}'`.text();
    const taskAreaPaneId = leftPaneResult.trim();

    // The original pane becomes the left side (status area)
    // Split left pane vertically: top for status, bottom for log
    const logPaneResult = await Bun.$`tmux split-window -t ${basePaneId} -v -d -p 50 -P -F '#{pane_id}'`.text();
    const logPaneId = logPaneResult.trim();

    // basePaneId is now the status pane (top-left)
    const statusPaneId = basePaneId;

    // Set pane titles
    await Bun.$`tmux select-pane -t ${statusPaneId} -T "Status"`.quiet();
    await Bun.$`tmux select-pane -t ${logPaneId} -T "Logs"`.quiet();
    await Bun.$`tmux select-pane -t ${taskAreaPaneId} -T "Tasks"`.quiet();

    // Close the task area placeholder pane - we'll create task panes dynamically
    await Bun.$`tmux kill-pane -t ${taskAreaPaneId}`.quiet();

    this.layout = {
      sessionName,
      windowName,
      statusPaneId,
      logPaneId,
      taskPanes: [],
    };

    // Initial status display
    await this.updateStatusPane({
      projectId: sessionName,
      status: "initializing",
      ready: 0,
      running: 0,
      waiting: 0,
      blocked: 0,
      completed: 0,
      recentCompletions: [],
    });

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Dashboard created: status=${statusPaneId}, log=${logPaneId}`);
    }

    return this.layout;
  }

  /**
   * Add a pane for a task in the dashboard.
   */
  async addTaskPane(taskId: string, title: string, command: string): Promise<string> {
    if (!this.layout) {
      throw new Error("Dashboard not created. Call createDashboard first.");
    }

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Adding task pane: ${taskId} - ${title}`);
    }

    // Find where to split - use status pane as reference and split to the right
    const targetPane = this.layout.taskPanes.length > 0
      ? this.layout.taskPanes[this.layout.taskPanes.length - 1].paneId
      : this.layout.logPaneId;

    // Split vertically (new pane below) within the task area
    const paneResult = await Bun.$`tmux split-window -t ${targetPane} -h -d -P -F '#{pane_id}' ${command}`.text();
    const paneId = paneResult.trim();

    // Set pane title
    const shortTitle = title.length > 25 ? title.substring(0, 22) + "..." : title;
    await Bun.$`tmux select-pane -t ${paneId} -T "Task:${shortTitle}"`.quiet();

    const paneInfo: PaneInfo = { taskId, paneId, title };
    this.layout.taskPanes.push(paneInfo);

    // Rebalance panes
    await this.rebalancePanes();

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Task pane added: ${paneId} for ${taskId}`);
    }

    return paneId;
  }

  /**
   * Remove a task pane from the dashboard.
   */
  async removeTaskPane(taskId: string): Promise<boolean> {
    if (!this.layout) {
      return false;
    }

    const paneIndex = this.layout.taskPanes.findIndex(p => p.taskId === taskId);
    if (paneIndex === -1) {
      return false;
    }

    const pane = this.layout.taskPanes[paneIndex];

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Removing task pane: ${taskId} (${pane.paneId})`);
    }

    try {
      await Bun.$`tmux kill-pane -t ${pane.paneId}`.quiet();
    } catch {
      // Pane might already be closed
      if (isDebugEnabled()) {
        console.log(`[TmuxManager] Pane ${pane.paneId} already closed`);
      }
    }

    this.layout.taskPanes.splice(paneIndex, 1);

    // Rebalance remaining panes
    if (this.layout.taskPanes.length > 0) {
      await this.rebalancePanes();
    }

    return true;
  }

  /**
   * Update the status pane with current runner information.
   */
  async updateStatusPane(status: StatusInfo): Promise<void> {
    if (!this.layout) {
      return;
    }

    this.currentStatus = status;

    const now = new Date().toLocaleTimeString();
    const statusText = `
+==================================+
|       BRAIN RUNNER STATUS        |
+==================================+
| Project: ${status.projectId.padEnd(23)}|
| Status:  ${status.status.padEnd(23)}|
+----------------------------------+
| Ready:     ${String(status.ready).padStart(5)}                |
| Running:   ${String(status.running).padStart(5)}                |
| Waiting:   ${String(status.waiting).padStart(5)}                |
| Blocked:   ${String(status.blocked).padStart(5)}                |
| Completed: ${String(status.completed).padStart(5)}                |
+----------------------------------+
| Recent Completions:              |
${status.recentCompletions.slice(0, 3).map(c => `|  - ${c.substring(0, 28).padEnd(28)}|`).join('\n') || '|  (none)                          |'}
+----------------------------------+
| Updated: ${now.padEnd(23)}|
+==================================+
`.trim();

    try {
      // Clear the pane and write status
      const escapedStatus = statusText.replace(/'/g, "'\\''");
      await Bun.$`tmux send-keys -t ${this.layout.statusPaneId} "clear" Enter`.quiet();
      await Bun.$`tmux send-keys -t ${this.layout.statusPaneId} "cat << 'EOF'\n${statusText}\nEOF" Enter`.quiet();
    } catch (error) {
      if (isDebugEnabled()) {
        console.log(`[TmuxManager] Failed to update status pane:`, error);
      }
    }
  }

  /**
   * Send content to the log pane.
   */
  async writeToLogPane(message: string): Promise<void> {
    if (!this.layout) {
      return;
    }

    try {
      const timestamp = new Date().toLocaleTimeString();
      const escapedMessage = message.replace(/'/g, "'\\''");
      await Bun.$`tmux send-keys -t ${this.layout.logPaneId} "echo '[${timestamp}] ${escapedMessage}'" Enter`.quiet();
    } catch (error) {
      if (isDebugEnabled()) {
        console.log(`[TmuxManager] Failed to write to log pane:`, error);
      }
    }
  }

  /**
   * Start periodic status updates.
   */
  startStatusUpdates(intervalMs: number = 5000): void {
    if (this.statusUpdateInterval) {
      clearInterval(this.statusUpdateInterval);
    }

    this.statusUpdateInterval = setInterval(() => {
      if (this.currentStatus) {
        this.updateStatusPane(this.currentStatus);
      }
    }, intervalMs);
  }

  /**
   * Stop periodic status updates.
   */
  stopStatusUpdates(): void {
    if (this.statusUpdateInterval) {
      clearInterval(this.statusUpdateInterval);
      this.statusUpdateInterval = null;
    }
  }

  /**
   * Get the current dashboard layout.
   */
  getLayout(): DashboardLayout | null {
    return this.layout;
  }

  /**
   * Get pane info for a task.
   */
  getTaskPane(taskId: string): PaneInfo | undefined {
    return this.layout?.taskPanes.find(p => p.taskId === taskId);
  }

  /**
   * Check if a pane still exists.
   */
  async isPaneAlive(paneId: string): Promise<boolean> {
    try {
      const result = await Bun.$`tmux list-panes -a -F '#{pane_id}'`.text();
      return result.split('\n').includes(paneId);
    } catch {
      return false;
    }
  }

  /**
   * Cleanup all dashboard panes and close the window.
   */
  async cleanup(): Promise<void> {
    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Cleaning up dashboard`);
    }

    this.stopStatusUpdates();

    if (!this.layout) {
      return;
    }

    try {
      // Kill all task panes
      for (const pane of this.layout.taskPanes) {
        try {
          await Bun.$`tmux kill-pane -t ${pane.paneId}`.quiet();
        } catch {
          // Pane might already be closed
        }
      }

      // Kill the entire dashboard window
      await Bun.$`tmux kill-window -t ${this.layout.windowName}`.quiet();
    } catch (error) {
      if (isDebugEnabled()) {
        console.log(`[TmuxManager] Cleanup error:`, error);
      }
    }

    this.layout = null;
  }

  /**
   * Rebalance pane sizes for even distribution.
   */
  private async rebalancePanes(): Promise<void> {
    if (!this.layout) {
      return;
    }

    try {
      // Select the window and rebalance
      await Bun.$`tmux select-layout -t ${this.layout.windowName} tiled`.quiet();
    } catch {
      // Layout adjustment might fail if panes are in odd states
      if (isDebugEnabled()) {
        console.log(`[TmuxManager] Failed to rebalance panes`);
      }
    }
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let tmuxManagerInstance: TmuxManager | null = null;

/**
 * Get the TmuxManager singleton.
 */
export function getTmuxManager(): TmuxManager {
  if (!tmuxManagerInstance) {
    tmuxManagerInstance = new TmuxManager();
  }
  return tmuxManagerInstance;
}

/**
 * Reset the TmuxManager singleton (useful for testing).
 */
export function resetTmuxManager(): void {
  if (tmuxManagerInstance) {
    tmuxManagerInstance.stopStatusUpdates();
  }
  tmuxManagerInstance = null;
}
