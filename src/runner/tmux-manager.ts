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
  private paneMonitorInterval: ReturnType<typeof setInterval> | null = null;
  private currentStatus: StatusInfo | null = null;
  private stateDir: string;
  private taskListScriptPath: string | null = null;
  private logsScriptPath: string | null = null;
  
  /** Callbacks to invoke when a task pane closes */
  private onPaneClosedCallbacks: ((taskId: string, paneId: string) => void)[] = [];

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
   * 
   * Displays a full dependency tree visualization with:
   * - Status summary header (counts by status)
   * - Full dependency tree showing parent-child relationships
   * - Status symbols: ● ready, ○ waiting, ✗ blocked, ▶ in_progress, ✓ completed
   */
  private async createTaskListScript(projectId: string): Promise<string> {
    await mkdir(this.stateDir, { recursive: true });
    const scriptPath = join(this.stateDir, `task_list_${projectId}.sh`);
    
    // Get API URL from environment or use default
    const apiUrl = process.env.BRAIN_API_URL ?? "http://localhost:3000";
    
    // Maximum width for the task list pane (leaving room for tree lines and border)
    const maxTitleWidth = 45;
    
    const script = `#!/bin/bash
# Task List Refresh Script - Full Dependency Tree View
# Curls brain-api for task list with dependency resolution
# Refreshes every 10 seconds

PROJECT_ID="${projectId}"
API_URL="${apiUrl}"
MAX_TITLE_WIDTH=${maxTitleWidth}

# Box drawing characters
BOX_H="─"
BOX_V="│"
BOX_TL="┌"
BOX_TR="┐"
BOX_BL="└"
BOX_BR="┘"
BOX_T="├"
BOX_TREE_L="└─"
BOX_TREE_T="├─"
BOX_TREE_V="│ "

# Status symbols
SYM_READY="●"
SYM_WAITING="○"
SYM_BLOCKED="✗"
SYM_PROGRESS="▶"
SYM_DONE="✓"
SYM_PRIORITY="!"

# Colors (ANSI escape codes)
C_RESET="\\033[0m"
C_BOLD="\\033[1m"
C_DIM="\\033[2m"
C_GREEN="\\033[32m"
C_YELLOW="\\033[33m"
C_RED="\\033[31m"
C_BLUE="\\033[34m"
C_CYAN="\\033[36m"

# Get terminal width, default to 60 if not available
get_width() {
  local width
  width=\$(tput cols 2>/dev/null || echo 60)
  echo "\$width"
}

# Truncate string to max length
truncate() {
  local str="\$1"
  local max="\$2"
  if [[ \${#str} -gt \$max ]]; then
    echo "\${str:0:\$((max-2))}.."
  else
    echo "\$str"
  fi
}

# Draw horizontal line
draw_line() {
  local width="\$1"
  local left="\$2"
  local right="\$3"
  printf "%s" "\$left"
  for ((i=0; i<width-2; i++)); do printf "%s" "\$BOX_H"; done
  printf "%s\\n" "\$right"
}

# Pad string to width
pad_right() {
  local str="\$1"
  local width="\$2"
  printf "%-\${width}s" "\$str"
}

show_tasks() {
  clear
  
  local term_width
  term_width=\$(get_width)
  local content_width=\$((term_width - 2))  # Account for left/right borders
  
  # Curl the brain-api for task list
  local response
  response=\$(curl -s "\${API_URL}/api/v1/tasks/\${PROJECT_ID}" 2>/dev/null)
  
  if [[ \$? -ne 0 ]] || [[ -z "\$response" ]] || echo "\$response" | jq -e '.error' >/dev/null 2>&1; then
    draw_line "\$term_width" "\$BOX_TL" "\$BOX_TR"
    printf "%s %-\$((content_width-1))s%s\\n" "\$BOX_V" "\${C_RED}Error: Could not fetch tasks\${C_RESET}" "\$BOX_V"
    draw_line "\$term_width" "\$BOX_BL" "\$BOX_BR"
    return 1
  fi
  
  # Calculate status counts
  local ready_count waiting_count blocked_count completed_count in_progress_count
  ready_count=\$(echo "\$response" | jq -r '.stats.ready // 0' 2>/dev/null)
  waiting_count=\$(echo "\$response" | jq -r '.stats.waiting // 0' 2>/dev/null)
  blocked_count=\$(echo "\$response" | jq -r '.stats.blocked // 0' 2>/dev/null)
  completed_count=\$(echo "\$response" | jq -r '[.tasks[] | select(.status == "completed")] | length' 2>/dev/null)
  in_progress_count=\$(echo "\$response" | jq -r '[.tasks[] | select(.status == "in_progress")] | length' 2>/dev/null)
  
  # Truncate project ID for header
  local display_id
  display_id=\$(truncate "\$PROJECT_ID" \$((content_width - 4)))
  
  # Draw header with project name
  draw_line "\$term_width" "\$BOX_TL\$BOX_H" "\$BOX_TR"
  printf "%s \${C_BOLD}%s\${C_RESET}%*s%s\\n" "\$BOX_V" "\$display_id" \$((content_width - \${#display_id} - 1)) "" "\$BOX_V"
  
  # Draw status summary line
  local status_line
  status_line="\${C_GREEN}\${SYM_READY}\${C_RESET} \${ready_count} ready   \${C_YELLOW}\${SYM_WAITING}\${C_RESET} \${waiting_count} waiting   \${C_BLUE}\${SYM_PROGRESS}\${C_RESET} \${in_progress_count} active   \${C_GREEN}\${SYM_DONE}\${C_RESET} \${completed_count} done"
  if [[ \$blocked_count -gt 0 ]]; then
    status_line="\${status_line}   \${C_RED}\${SYM_BLOCKED}\${C_RESET} \${blocked_count} blocked"
  fi
  # Print status line (plain text for width calculation, colored for display)
  local plain_status="\$SYM_READY \$ready_count ready   \$SYM_WAITING \$waiting_count waiting   \$SYM_PROGRESS \$in_progress_count active   \$SYM_DONE \$completed_count done"
  [[ \$blocked_count -gt 0 ]] && plain_status="\$plain_status   \$SYM_BLOCKED \$blocked_count blocked"
  printf "%s  %b%*s%s\\n" "\$BOX_V" "\$status_line" \$((content_width - \${#plain_status} - 2)) "" "\$BOX_V"
  
  # Draw separator
  draw_line "\$term_width" "\$BOX_T" "\$BOX_T"
  
  # Helper: print a task line with proper formatting
  print_task() {
    local symbol="\$1"
    local color="\$2"
    local title="\$3"
    local priority="\$4"
    local suffix="\$5"
    
    local display_title
    display_title=\$(truncate "\$title" \$MAX_TITLE_WIDTH)
    local priority_mark=""
    [[ "\$priority" == "high" ]] && priority_mark="!"
    
    local line="  \${color}\${symbol}\${C_RESET} \${display_title}\${priority_mark}"
    [[ -n "\$suffix" ]] && line="\${line} \${C_DIM}\${suffix}\${C_RESET}"
    
    local plain_line="  \$symbol \$display_title\$priority_mark"
    [[ -n "\$suffix" ]] && plain_line="\$plain_line \$suffix"
    local padding=\$((content_width - \${#plain_line} - 1))
    [[ \$padding -lt 0 ]] && padding=0
    printf "%s %b%*s%s\\n" "\$BOX_V" "\$line" \$padding "" "\$BOX_V"
  }
  
  # Helper: print section header
  print_section() {
    local color="\$1"
    local symbol="\$2"
    local label="\$3"
    local count="\$4"
    
    local header="\${color}\${symbol} \${label} (\${count})\${C_RESET}"
    local plain_header="\$symbol \$label (\$count)"
    local padding=\$((content_width - \${#plain_header} - 1))
    [[ \$padding -lt 0 ]] && padding=0
    printf "%s %b%*s%s\\n" "\$BOX_V" "\$header" \$padding "" "\$BOX_V"
  }
  
  # === In Progress Section ===
  if [[ \$in_progress_count -gt 0 ]]; then
    print_section "\$C_BLUE" "\$SYM_PROGRESS" "In Progress" "\$in_progress_count"
    local in_progress_output
    in_progress_output=\$(echo "\$response" | jq -r '
      [.tasks[] | select(.status == "in_progress")] | .[] |
      [.title, .priority // "medium"] | @tsv
    ' 2>/dev/null)
    if [[ -n "\$in_progress_output" ]]; then
      while IFS=\$'\\t' read -r title priority; do
        print_task "\$SYM_PROGRESS" "\$C_BLUE" "\$title" "\$priority" ""
      done <<< "\$in_progress_output"
    fi
    printf "%s%*s%s\\n" "\$BOX_V" \$content_width "" "\$BOX_V"
  fi
  
  # === Ready Section ===
  if [[ \$ready_count -gt 0 ]]; then
    print_section "\$C_GREEN" "\$SYM_READY" "Ready" "\$ready_count"
    local ready_output
    ready_output=\$(echo "\$response" | jq -r '
      [.tasks[] | select(.classification == "ready")] | sort_by(.priority | if . == "high" then 0 elif . == "medium" then 1 else 2 end) | .[] |
      [.title, .priority // "medium"] | @tsv
    ' 2>/dev/null)
    if [[ -n "\$ready_output" ]]; then
      while IFS=\$'\\t' read -r title priority; do
        print_task "\$SYM_READY" "\$C_GREEN" "\$title" "\$priority" ""
      done <<< "\$ready_output"
    else
      printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "(none)" "\$BOX_V"
    fi
    printf "%s%*s%s\\n" "\$BOX_V" \$content_width "" "\$BOX_V"
  fi
  
  # === Waiting Section ===
  if [[ \$waiting_count -gt 0 ]]; then
    print_section "\$C_YELLOW" "\$SYM_WAITING" "Waiting" "\$waiting_count"
    local waiting_output
    waiting_output=\$(echo "\$response" | jq -r '
      [.tasks[] | select(.classification == "waiting" or .classification == "waiting_on_parent")] | .[0:5] | .[] |
      [.title, .priority // "medium", (if .classification == "waiting_on_parent" then "parent" else (.waiting_on | length | tostring) + " deps" end)] | @tsv
    ' 2>/dev/null)
    if [[ -n "\$waiting_output" ]]; then
      while IFS=\$'\\t' read -r title priority suffix; do
        print_task "\$SYM_WAITING" "\$C_YELLOW" "\$title" "\$priority" "\$suffix"
      done <<< "\$waiting_output"
      [[ \$waiting_count -gt 5 ]] && printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "... and \$((waiting_count - 5)) more" "\$BOX_V"
    else
      printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "(none)" "\$BOX_V"
    fi
    printf "%s%*s%s\\n" "\$BOX_V" \$content_width "" "\$BOX_V"
  fi
  
  # === Blocked Section ===
  if [[ \$blocked_count -gt 0 ]]; then
    print_section "\$C_RED" "\$SYM_BLOCKED" "Blocked" "\$blocked_count"
    local blocked_output
    blocked_output=\$(echo "\$response" | jq -r '
      [.tasks[] | select(.classification == "blocked" or .classification == "blocked_by_parent")] | .[0:3] | .[] |
      [.title, .priority // "medium", (if .classification == "blocked_by_parent" then "by parent" else (.blocked_by | length | tostring) + " blocker(s)" end)] | @tsv
    ' 2>/dev/null)
    if [[ -n "\$blocked_output" ]]; then
      while IFS=\$'\\t' read -r title priority suffix; do
        print_task "\$SYM_BLOCKED" "\$C_RED" "\$title" "\$priority" "\$suffix"
      done <<< "\$blocked_output"
      [[ \$blocked_count -gt 3 ]] && printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "... and \$((blocked_count - 3)) more" "\$BOX_V"
    else
      printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "(none)" "\$BOX_V"
    fi
    printf "%s%*s%s\\n" "\$BOX_V" \$content_width "" "\$BOX_V"
  fi
  
  # === Completed Section ===
  if [[ \$completed_count -gt 0 ]]; then
    print_section "\$C_GREEN" "\$SYM_DONE" "Completed" "\$completed_count"
    local completed_output
    completed_output=\$(echo "\$response" | jq -r '
      [.tasks[] | select(.status == "completed")] | sort_by(.created) | reverse | .[0:5] | .[] |
      [.title, .priority // "medium"] | @tsv
    ' 2>/dev/null)
    if [[ -n "\$completed_output" ]]; then
      while IFS=\$'\\t' read -r title priority; do
        print_task "\$SYM_DONE" "\$C_GREEN" "\$title" "\$priority" ""
      done <<< "\$completed_output"
      [[ \$completed_count -gt 5 ]] && printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "... and \$((completed_count - 5)) more" "\$BOX_V"
    else
      printf "%s  %-\$((content_width-3))s%s\\n" "\$BOX_V" "(none)" "\$BOX_V"
    fi
  fi
  
  # If no tasks at all
  local total_active=\$((in_progress_count + ready_count + waiting_count + blocked_count + completed_count))
  if [[ \$total_active -eq 0 ]]; then
    printf "%s %-\$((content_width-1))s%s\\n" "\$BOX_V" "(no tasks)" "\$BOX_V"
  fi
  
  # Draw footer with timestamp
  draw_line "\$term_width" "\$BOX_T" "\$BOX_T"
  local timestamp
  timestamp=\$(date +%H:%M:%S)
  printf "%s Last updated: %s%*s%s\\n" "\$BOX_V" "\$timestamp" \$((content_width - 16)) "" "\$BOX_V"
  draw_line "\$term_width" "\$BOX_BL" "\$BOX_BR"
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
   * Add a pane for a task in the dashboard (creates the pane).
   * Matches do-work's create_task_pane function:
   * - First task: replaces placeholder pane (split above logs, takes 80%)
   * - Additional tasks: split horizontally from last task pane
   * 
   * NOTE: When using OpencodeExecutor in dashboard mode, the executor creates
   * panes directly via spawnDashboard(). Use registerTaskPane() instead to
   * track those panes without creating duplicates.
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
   * Register an existing task pane for tracking (without creating it).
   * Used when OpencodeExecutor creates the pane directly.
   */
  registerTaskPane(taskId: string, paneId: string, title: string): void {
    if (!this.layout) {
      return;
    }

    // Check if already registered
    if (this.layout.taskPanes.some(p => p.taskId === taskId)) {
      return;
    }

    const paneInfo: PaneInfo = { taskId, paneId, title };
    this.layout.taskPanes.push(paneInfo);

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Registered task pane: ${paneId} for ${taskId}`);
    }
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
   * Register a callback to be invoked when a task pane closes.
   * Used by TaskRunner to detect when OpenCode finishes in dashboard mode.
   */
  onPaneClosed(callback: (taskId: string, paneId: string) => void): void {
    this.onPaneClosedCallbacks.push(callback);
  }

  /**
   * Start monitoring task panes for completion.
   * Checks every 2 seconds if panes are still alive and invokes callbacks when they close.
   */
  startPaneMonitoring(intervalMs: number = 2000): void {
    if (this.paneMonitorInterval) {
      clearInterval(this.paneMonitorInterval);
    }

    this.paneMonitorInterval = setInterval(async () => {
      await this.checkPaneLiveness();
    }, intervalMs);

    if (isDebugEnabled()) {
      console.log(`[TmuxManager] Started pane monitoring (interval: ${intervalMs}ms)`);
    }
  }

  /**
   * Stop pane monitoring.
   */
  stopPaneMonitoring(): void {
    if (this.paneMonitorInterval) {
      clearInterval(this.paneMonitorInterval);
      this.paneMonitorInterval = null;
    }
  }

  /**
   * Check if all tracked task panes are still alive.
   * Triggers cleanup and callbacks for any closed panes.
   */
  private async checkPaneLiveness(): Promise<void> {
    if (!this.layout || this.layout.taskPanes.length === 0) {
      return;
    }

    // Get list of all alive panes
    let alivePanes: Set<string>;
    try {
      const result = await Bun.$`tmux list-panes -a -F '#{pane_id}'`.text();
      alivePanes = new Set(result.trim().split('\n').filter(Boolean));
    } catch {
      // tmux command failed - likely session closed
      return;
    }

    // Check each tracked task pane
    const closedPanes: PaneInfo[] = [];
    for (const pane of this.layout.taskPanes) {
      if (!alivePanes.has(pane.paneId)) {
        closedPanes.push(pane);
      }
    }

    // Process closed panes
    for (const pane of closedPanes) {
      if (isDebugEnabled()) {
        console.log(`[TmuxManager] Detected closed pane: ${pane.paneId} (task: ${pane.taskId})`);
      }

      // Remove from tracking
      const index = this.layout.taskPanes.findIndex(p => p.taskId === pane.taskId);
      if (index !== -1) {
        this.layout.taskPanes.splice(index, 1);
      }

      // Invoke callbacks
      for (const callback of this.onPaneClosedCallbacks) {
        try {
          callback(pane.taskId, pane.paneId);
        } catch (error) {
          if (isDebugEnabled()) {
            console.log(`[TmuxManager] Callback error:`, error);
          }
        }
      }
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
    this.stopPaneMonitoring();
    this.onPaneClosedCallbacks = [];

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
