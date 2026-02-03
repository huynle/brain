/**
 * Tmux Manager
 *
 * Manages tmux dashboard layout for dashboard mode execution.
 * Provides visual interface showing task status and OpenCode panes.
 *
 * Layout matches do-work script:
 * +------------------+---------------------------+
 * |                  |                           |
 * |   Task List      |    OpenCode/Tasks         |
 * |   (25% width)    |    (75% width)            |
 * |   Live refresh   |                           |
 * |                  +---------------------------+
 * |                  |   Logs (20% height)       |
 * +------------------+---------------------------+
 */

import { isDebugEnabled } from "./config";
import { mkdir, writeFile, chmod, rm } from "fs/promises";
import { join } from "path";
import { homedir } from "os";

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
  taskListPaneId: string;  // Left pane with live task list
  taskAreaPaneId: string;  // Main area for OpenCode task panes (placeholder)
  logPaneId: string;       // Bottom right for logs
  taskPanes: PaneInfo[];   // Active task panes
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
  private stateDir: string;
  private taskListScriptPath: string | null = null;
  private logsScriptPath: string | null = null;

  constructor() {
    this.stateDir = join(homedir(), ".local", "state", "brain-runner");
  }

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
   * Create the task list script that refreshes every 10 seconds.
   * Curls the brain-api for task list with dependency resolution.
   */
  private async createTaskListScript(projectId: string): Promise<string> {
    await mkdir(this.stateDir, { recursive: true });
    const scriptPath = join(this.stateDir, `task_list_${projectId}.sh`);
    
    // Get API URL from environment or use default
    const apiUrl = process.env.BRAIN_API_URL ?? "http://localhost:3000";
    
    const script = `#!/bin/bash
# Task List Refresh Script
# Curls brain-api for task list with dependency resolution
# Refreshes every 10 seconds

PROJECT_ID="${projectId}"
API_URL="${apiUrl}"

show_tasks() {
  clear
  
  local display_id="\${PROJECT_ID:0:20}"
  [[ \${#PROJECT_ID} -gt 20 ]] && display_id="\${display_id}..."
  
  echo -e "\\033[1m=== \$display_id ===\\033[0m"
  echo ""
  
  # Curl the brain-api for task list
  local response
  response=$(curl -s "\${API_URL}/api/v1/tasks/\${PROJECT_ID}" 2>/dev/null)
  
  if [[ $? -ne 0 ]] || [[ -z "\$response" ]] || echo "\$response" | jq -e '.error' >/dev/null 2>&1; then
    echo -e "\\033[31mError: Could not fetch tasks from API\\033[0m"
    echo ""
    echo "Last updated: $(date +%H:%M:%S)"
    return 1
  fi
  
  # Extract stats from API response
  local ready_count waiting_count blocked_count completed_count in_progress_count
  ready_count=$(echo "\$response" | jq -r '.stats.ready // 0' 2>/dev/null)
  waiting_count=$(echo "\$response" | jq -r '.stats.waiting // 0' 2>/dev/null)
  blocked_count=$(echo "\$response" | jq -r '.stats.blocked // 0' 2>/dev/null)
  completed_count=$(echo "\$response" | jq -r '.stats.completed // 0' 2>/dev/null)
  in_progress_count=$(echo "\$response" | jq -r '.stats.in_progress // 0' 2>/dev/null)
  
  # Display Ready tasks
  echo -e "\\033[32mReady (\$ready_count):\\033[0m"
  local ready_output
  ready_output=$(echo "\$response" | jq -r '.tasks[] | select(.state == "ready") | "  - " + .title' 2>/dev/null | head -10)
  if [[ -n "\$ready_output" ]]; then
    echo "\$ready_output"
  else
    echo "  (none)"
  fi
  echo ""
  
  # Display In Progress tasks
  echo -e "\\033[34mIn Progress (\$in_progress_count):\\033[0m"
  local progress_output
  progress_output=$(echo "\$response" | jq -r '.tasks[] | select(.status == "in_progress") | "  - " + .title' 2>/dev/null)
  if [[ -n "\$progress_output" ]]; then
    echo "\$progress_output"
  else
    echo "  (none)"
  fi
  echo ""
  
  # Display Waiting tasks (with dependencies)
  echo -e "\\033[33mWaiting (\$waiting_count):\\033[0m"
  local waiting_output
  waiting_output=$(echo "\$response" | jq -r '.tasks[] | select(.state == "waiting") | "  - " + .title + " (needs: " + ((.blocking_deps // []) | join(", ")) + ")"' 2>/dev/null | head -5)
  if [[ -n "\$waiting_output" ]]; then
    echo "\$waiting_output"
  else
    echo "  (none)"
  fi
  echo ""
  
  # Display Blocked tasks
  echo -e "\\033[31mBlocked (\$blocked_count):\\033[0m"
  local blocked_output
  blocked_output=$(echo "\$response" | jq -r '.tasks[] | select(.state == "blocked" or .status == "blocked") | "  - " + .title' 2>/dev/null | head -3)
  if [[ -n "\$blocked_output" ]]; then
    echo "\$blocked_output"
  else
    echo "  (none)"
  fi
  echo ""
  
  # Display Completed count
  echo -e "\\033[32mCompleted (\$completed_count)\\033[0m"
  echo ""
  
  echo "Last updated: $(date +%H:%M:%S)"
}

# Main loop - refresh every 10 seconds
while true; do
  show_tasks
  sleep 10
done
`;
    
    await writeFile(scriptPath, script);
    await chmod(scriptPath, 0o755);
    return scriptPath;
  }

  /**
   * Create the logs watch script.
   */
  private async createLogsScript(): Promise<string> {
    await mkdir(this.stateDir, { recursive: true });
    const scriptPath = join(this.stateDir, "logs_watch.sh");
    
    const logFile = join(homedir(), ".local", "log", "brain-runner.log");
    
    const script = `#!/bin/bash
LOG_FILE="${logFile}"
echo -e "\\033[1mLogs\\033[0m"
echo -e "\\033[2m---------------------------------------\\033[0m"
if [[ -f "\$LOG_FILE" ]]; then
  tail -f "\$LOG_FILE" 2>/dev/null | while read -r line; do
    if [[ "\$line" == *"[ERROR]"* ]]; then
      echo -e "\\033[31m\$line\\033[0m"
    elif [[ "\$line" == *"[WARN]"* ]]; then
      echo -e "\\033[33m\$line\\033[0m"
    elif [[ "\$line" == *"[INFO]"* ]]; then
      echo -e "\\033[32m\$line\\033[0m"
    elif [[ "\$line" == *"[DEBUG]"* ]]; then
      echo -e "\\033[2m\$line\\033[0m"
    else
      echo "\$line"
    fi
  done
else
  echo "Waiting for log file..."
  while [[ ! -f "\$LOG_FILE" ]]; do sleep 1; done
  exec "\$0"
fi
`;
    
    await writeFile(scriptPath, script);
    await chmod(scriptPath, 0o755);
    return scriptPath;
  }

  /**
   * Create a placeholder script for the task area.
   */
  private async createPlaceholderScript(): Promise<string> {
    await mkdir(this.stateDir, { recursive: true });
    const scriptPath = join(this.stateDir, "task_placeholder.sh");
    
    const script = `#!/bin/bash
clear
echo ""
echo -e "\\033[1m  Waiting for tasks...\\033[0m"
echo ""
echo -e "\\033[2m  Tasks will appear here when processing starts.\\033[0m"
echo ""
# Keep the pane alive
while true; do sleep 60; done
`;
    
    await writeFile(scriptPath, script);
    await chmod(scriptPath, 0o755);
    return scriptPath;
  }

  /**
   * Create the dashboard layout matching do-work:
   * +------------------+---------------------------+
   * |                  |                           |
   * |   Task List      |    OpenCode/Tasks         |
   * |   (25% width)    |    (75% width)            |
   * |   Live refresh   |                           |
   * |                  +---------------------------+
   * |                  |   Logs (20% height)       |
   * +------------------+---------------------------+
   */
  async createDashboard(sessionName: string): Promise<DashboardLayout> {
    if (!await this.isTmuxAvailable()) {
      throw new Error("tmux is not available on this system");
    }

    const windowName = `dashboard-${sessionName}`;

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Creating dashboard: ${windowName}`);
    }

    // Create scripts for dashboard panes
    this.taskListScriptPath = await this.createTaskListScript(sessionName);
    this.logsScriptPath = await this.createLogsScript();
    const placeholderScriptPath = await this.createPlaceholderScript();

    // Check if we're inside tmux
    const insideTmux = await this.isInsideTmux();

    if (insideTmux) {
      // Create a new window with the task list script
      await Bun.$`tmux new-window -d -n ${windowName} ${this.taskListScriptPath}`.quiet();
    } else {
      // Create a new session with the task list script
      try {
        await Bun.$`tmux new-session -d -s ${sessionName} -n ${windowName} ${this.taskListScriptPath}`.quiet();
      } catch {
        // Session might already exist, try to create window
        await Bun.$`tmux new-window -d -t ${sessionName} -n ${windowName} ${this.taskListScriptPath}`.quiet();
      }
    }

    // Small delay for window creation
    await Bun.sleep(300);

    // Get the task list pane ID (the initial pane)
    const taskListPaneResult = await Bun.$`tmux list-panes -t ${windowName} -F '#{pane_id}'`.text();
    const taskListPaneId = taskListPaneResult.trim().split('\n')[0];

    // Split horizontally: right side gets 75% for task area
    const taskAreaResult = await Bun.$`tmux split-window -t ${taskListPaneId} -h -d -P -F '#{pane_id}' -l 75% ${placeholderScriptPath}`.text();
    const taskAreaPaneId = taskAreaResult.trim();

    // Small delay for split
    await Bun.sleep(200);

    // Split the task area vertically: bottom 20% for logs
    const logPaneResult = await Bun.$`tmux split-window -t ${taskAreaPaneId} -v -d -P -F '#{pane_id}' -l 20% ${this.logsScriptPath}`.text();
    const logPaneId = logPaneResult.trim();

    // Set pane titles
    await Bun.$`tmux select-pane -t ${taskListPaneId} -T "Tasks"`.quiet();
    await Bun.$`tmux select-pane -t ${taskAreaPaneId} -T "OpenCode"`.quiet();
    await Bun.$`tmux select-pane -t ${logPaneId} -T "Logs"`.quiet();

    this.layout = {
      sessionName,
      windowName,
      taskListPaneId,
      taskAreaPaneId,
      logPaneId,
      taskPanes: [],
    };

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Dashboard created: taskList=${taskListPaneId}, taskArea=${taskAreaPaneId}, log=${logPaneId}`);
    }

    return this.layout;
  }

  /**
   * Add a pane for a task in the dashboard.
   * Matches do-work's create_task_pane function:
   * - First task: replaces placeholder pane (split above logs, takes 80%)
   * - Additional tasks: split horizontally from last task pane
   */
  async addTaskPane(taskId: string, title: string, command: string): Promise<string> {
    if (!this.layout) {
      throw new Error("Dashboard not created. Call createDashboard first.");
    }

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Adding task pane: ${taskId} - ${title}`);
    }

    let paneId: string;

    if (this.layout.taskPanes.length === 0) {
      // First task: kill placeholder and split above logs pane
      // First, check if taskAreaPaneId (placeholder) still exists
      const placeholderAlive = await this.isPaneAlive(this.layout.taskAreaPaneId);
      
      if (placeholderAlive) {
        // Kill the placeholder pane
        try {
          await Bun.$`tmux kill-pane -t ${this.layout.taskAreaPaneId}`.quiet();
        } catch {
          // Placeholder might already be gone
        }
      }

      // Split above logs pane, take 80% of the space
      const paneResult = await Bun.$`tmux split-window -t ${this.layout.logPaneId} -v -d -P -F '#{pane_id}' -b -l 80% ${command}`.text();
      paneId = paneResult.trim();
    } else {
      // Additional task: split horizontally from the last task pane
      const lastTaskPane = this.layout.taskPanes[this.layout.taskPanes.length - 1];
      const paneResult = await Bun.$`tmux split-window -t ${lastTaskPane.paneId} -h -d -P -F '#{pane_id}' ${command}`.text();
      paneId = paneResult.trim();
    }

    // Small delay for pane creation
    await Bun.sleep(200);

    // Set pane title
    const shortTitle = title.length > 25 ? title.substring(0, 22) + "..." : title;
    await Bun.$`tmux select-pane -t ${paneId} -T "Task:${shortTitle}"`.quiet();

    const paneInfo: PaneInfo = { taskId, paneId, title };
    this.layout.taskPanes.push(paneInfo);

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
   * Update the status information.
   * Note: The task list pane refreshes automatically via its script.
   * This method stores the status for reference but doesn't need to
   * update the pane directly since the script handles refresh.
   */
  async updateStatusPane(status: StatusInfo): Promise<void> {
    this.currentStatus = status;
    
    // The task list pane now handles its own refresh via the script
    // that reads from the brain API. No need to send keys to the pane.
    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Status updated: ready=${status.ready}, running=${status.running}, waiting=${status.waiting}`);
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
   * Cleanup all dashboard panes, scripts, and close the window.
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

    // Clean up script files
    try {
      if (this.taskListScriptPath) {
        await rm(this.taskListScriptPath, { force: true });
      }
      if (this.logsScriptPath) {
        await rm(this.logsScriptPath, { force: true });
      }
      // Also clean up placeholder script
      const placeholderPath = join(this.stateDir, "task_placeholder.sh");
      await rm(placeholderPath, { force: true });
    } catch {
      // Ignore cleanup errors for scripts
    }

    this.layout = null;
    this.taskListScriptPath = null;
    this.logsScriptPath = null;
  }

  /**
   * Rebalance task panes for even horizontal distribution.
   * Only rebalances the task panes, not the sidebar or logs.
   */
  private async rebalancePanes(): Promise<void> {
    if (!this.layout || this.layout.taskPanes.length < 2) {
      return;
    }

    try {
      // Even out horizontal space among task panes
      // Use select-layout with even-horizontal for just the task area
      // Note: This is a best-effort rebalance
      for (const pane of this.layout.taskPanes) {
        if (await this.isPaneAlive(pane.paneId)) {
          await Bun.$`tmux resize-pane -t ${pane.paneId} -x 50%`.quiet();
        }
      }
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
