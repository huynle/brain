/**
 * OpenCode Executor
 *
 * Builds task prompts and spawns OpenCode processes.
 * Supports TUI, dashboard, and background execution modes.
 */

import { existsSync } from "fs";
import { join } from "path";
import { homedir } from "os";
import type {
  RunnerConfig,
  ExecutionMode,
  RunningTask,
  OpencodeConfig,
} from "./types";
import type { ResolvedTask } from "../core/types";
import { getRunnerConfig, isDebugEnabled } from "./config";
import { discoverOpencodePort } from "./opencode-port";

// =============================================================================
// Types
// =============================================================================

export interface SpawnOptions {
  mode: ExecutionMode;
  workdir?: string;
  isResume?: boolean;
  paneId?: string;
  windowName?: string;
  /** When true, use interactive TUI command even in dashboard mode */
  useTui?: boolean;
}

export interface SpawnResult {
  pid: number;
  proc: ReturnType<typeof Bun.spawn> | null;
  paneId?: string;
  windowName?: string;
  promptFile: string;
  opencodePort?: number;  // OpenCode HTTP API port (discovered via lsof)
}

// =============================================================================
// OpenCode Executor Class
// =============================================================================

export class OpencodeExecutor {
  private config: RunnerConfig;
  private stateDir: string;

  constructor(config?: RunnerConfig) {
    this.config = config ?? getRunnerConfig();
    this.stateDir = this.config.stateDir;
  }

  // ========================================
  // Prompt Building
  // ========================================

  buildPrompt(task: ResolvedTask, isResume: boolean): string {
    if (isResume) {
      return `Load the do-work-queue skill and RESUME the interrupted task at brain path: ${task.path}

IMPORTANT: This task was previously in_progress but was interrupted.

Use brain_recall to read the task details, then:
1. Check the task file for any progress notes or partial work
2. Assess what work (if any) was already completed
3. If work was partially done, continue from where it left off
4. If unclear what was done, restart the task from the beginning
5. Follow the do-work-queue skill workflow to completion
6. Mark as completed with summary (note that this was a resumed task)
7. Create atomic git commit

Start now.`;
    }

    return `Load the do-work-queue skill and process the task at brain path: ${task.path}

Use brain_recall to read the task details, then follow the do-work-queue skill workflow:
1. Mark the task as in_progress
2. Triage complexity (Route A/B/C)
3. Execute the appropriate route
4. Run tests if applicable
5. Mark as completed with summary
6. Create atomic git commit

Start now.`;
  }

  // ========================================
  // Workdir Resolution
  // ========================================

  resolveWorkdir(task: ResolvedTask): string {
    // Priority: target_workdir > task worktree > task workdir > resolved_workdir > config default

    // target_workdir is an explicit override, check first (absolute path)
    if (task.target_workdir) {
      if (existsSync(task.target_workdir)) {
        if (isDebugEnabled()) {
          console.log(`[OpencodeExecutor] Using target_workdir: ${task.target_workdir}`);
        }
        return task.target_workdir;
      }
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] target_workdir not found, falling back: ${task.target_workdir}`);
      }
    }

    if (task.worktree) {
      const worktreePath = join(homedir(), task.worktree);
      if (existsSync(worktreePath)) {
        return worktreePath;
      }
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Worktree not found: ${worktreePath}`);
      }
    }

    if (task.workdir) {
      const workdirPath = join(homedir(), task.workdir);
      if (existsSync(workdirPath)) {
        return workdirPath;
      }
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Workdir not found: ${workdirPath}`);
      }
    }

    if (task.resolved_workdir && existsSync(task.resolved_workdir)) {
      return task.resolved_workdir;
    }

    return this.config.workDir;
  }

  // ========================================
  // Spawning
  // ========================================

  async spawn(
    task: ResolvedTask,
    projectId: string,
    options: SpawnOptions
  ): Promise<SpawnResult> {
    const { mode, isResume = false } = options;

    // Ensure state directory exists
    await this.ensureStateDir();

    // Build and save prompt
    const prompt = this.buildPrompt(task, isResume);
    const promptFile = join(this.stateDir, `prompt_${projectId}_${task.id}.txt`);
    await Bun.write(promptFile, prompt);

    // Resolve workdir
    const workdir = options.workdir ?? this.resolveWorkdir(task);

    if (isDebugEnabled()) {
      console.log(`[OpencodeExecutor] Spawning OpenCode for: ${task.title}`);
      console.log(`[OpencodeExecutor]   Mode: ${mode}`);
      console.log(`[OpencodeExecutor]   Workdir: ${workdir}`);
      console.log(`[OpencodeExecutor]   Prompt file: ${promptFile}`);
    }

    switch (mode) {
      case "background":
        return this.spawnBackground(task, projectId, workdir, promptFile);

      case "tui":
        return this.spawnTui(
          task,
          projectId,
          workdir,
          promptFile,
          options.windowName
        );

      case "dashboard":
        return this.spawnDashboard(
          task,
          projectId,
          workdir,
          promptFile,
          options.paneId,
          options.useTui ?? false
        );

      default:
        throw new Error(`Unknown execution mode: ${mode}`);
    }
  }

  // ========================================
  // Background Mode
  // ========================================

  private async spawnBackground(
    task: ResolvedTask,
    projectId: string,
    workdir: string,
    promptFile: string
  ): Promise<SpawnResult> {
    const outputFile = join(
      this.stateDir,
      `output_${projectId}_${task.id}.log`
    );

    // Read prompt content
    const promptContent = await Bun.file(promptFile).text();

    const proc = Bun.spawn({
      cmd: [
        this.config.opencode.bin,
        "run",
        "--agent",
        this.config.opencode.agent,
        "--model",
        this.config.opencode.model,
        promptContent,
      ],
      cwd: workdir,
      stdout: Bun.file(outputFile),
      stderr: Bun.file(outputFile),
    });

    if (isDebugEnabled()) {
      console.log(
        `[OpencodeExecutor] Background process started (PID: ${proc.pid})`
      );
    }

    return {
      pid: proc.pid,
      proc,
      promptFile,
    };
  }

  // ========================================
  // TUI Mode (standalone tmux window)
  // ========================================

  private async spawnTui(
    task: ResolvedTask,
    projectId: string,
    workdir: string,
    promptFile: string,
    windowName?: string
  ): Promise<SpawnResult> {
    // Use short ID (first 8 chars) with project prefix for cleaner tmux window names
    const shortId = task.id.substring(0, 8);
    const name = windowName ?? `${projectId}-${shortId}`;

    // Build runner script that OpenCode TUI runs in
    const runnerScript = join(
      this.stateDir,
      `runner_${projectId}_${task.id}.sh`
    );
    const script = `#!/bin/bash
cd "${workdir}"
"${this.config.opencode.bin}" --agent "${this.config.opencode.agent}" --model "${this.config.opencode.model}" --port 0 --prompt "$(cat '${promptFile}')"
exit_code=$?
echo ""
echo "Task Complete (exit: $exit_code)"
# Exit immediately so window closes and monitoring can detect completion
exit $exit_code
`;
    await Bun.write(runnerScript, script);
    await Bun.$`chmod +x ${runnerScript}`;

    // Create tmux window
    await Bun.$`tmux new-window -d -n ${name} -c ${workdir} ${runnerScript}`;

    // Wait for window to be created
    await new Promise((resolve) => setTimeout(resolve, 2000));

    // Get PID from the tmux pane
    const panePidResult =
      await Bun.$`tmux list-panes -t ${name} -F '#{pane_pid}'`.text();
    const panePid = parseInt(panePidResult.trim(), 10);

    // Try to find the actual OpenCode PID
    let pid = panePid;
    try {
      const pgrepResult =
        await Bun.$`pgrep -P ${panePid} -f opencode`.text();
      const opencodePid = parseInt(pgrepResult.trim(), 10);
      if (!isNaN(opencodePid)) {
        pid = opencodePid;
      }
    } catch {
      // pgrep failed, use pane pid
    }

    // Wait for OpenCode to start its HTTP server and discover the port
    await new Promise((resolve) => setTimeout(resolve, 2500));
    const opencodePort = await discoverOpencodePort(pid);

    if (isDebugEnabled()) {
      console.log(
        `[OpencodeExecutor] Created tmux window: ${name} (pid: ${pid}, port: ${opencodePort ?? 'unknown'})`
      );
    }

    return {
      pid,
      proc: null, // Can't track tmux process directly
      windowName: name,
      promptFile,
      opencodePort: opencodePort ?? undefined,
    };
  }

  // ========================================
  // Dashboard Mode (pane in existing window)
  // ========================================

  private async spawnDashboard(
    task: ResolvedTask,
    projectId: string,
    workdir: string,
    promptFile: string,
    targetPane?: string,
    useTui: boolean = false
  ): Promise<SpawnResult> {
    // Build runner script
    // When useTui is true, use interactive command (--port 0 --prompt) instead of headless (run)
    const runnerScript = join(
      this.stateDir,
      `runner_${projectId}_${task.id}.sh`
    );
    const opencodeCmd = useTui
      ? `"${this.config.opencode.bin}" --agent "${this.config.opencode.agent}" --model "${this.config.opencode.model}" --port 0 --prompt "$(cat '${promptFile}')"`
      : `"${this.config.opencode.bin}" run --agent "${this.config.opencode.agent}" --model "${this.config.opencode.model}" "$(cat '${promptFile}')"`;
    const script = `#!/bin/bash
cd "${workdir}"
${opencodeCmd}
exit_code=$?
echo ""
echo "Task Complete (exit: $exit_code)"
# Exit immediately so pane closes and monitoring can detect completion
exit $exit_code
`;
    await Bun.write(runnerScript, script);
    await Bun.$`chmod +x ${runnerScript}`;

    // Verify target pane exists before splitting (prevents race condition)
    if (targetPane) {
      const paneExists = await this.waitForPaneReady(targetPane, 3000);
      if (!paneExists) {
        throw new Error(`Target pane ${targetPane} not ready for split`);
      }
    }

    // Split existing pane horizontally with retry logic
    let paneId: string | null = null;
    let lastError: Error | null = null;
    
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const splitCmd = targetPane
          ? Bun.$`tmux split-window -t ${targetPane} -h -d -P -F '#{pane_id}' ${runnerScript}`
          : Bun.$`tmux split-window -h -d -P -F '#{pane_id}' ${runnerScript}`;

        const paneIdResult = await splitCmd.text();
        paneId = paneIdResult.trim();
        
        if (paneId && paneId.startsWith('%')) {
          break; // Success
        }
      } catch (error) {
        lastError = error as Error;
        if (isDebugEnabled()) {
          console.log(`[OpencodeExecutor] Split attempt ${attempt + 1} failed:`, error);
        }
        // Wait before retry with exponential backoff
        await Bun.sleep(500 * (attempt + 1));
      }
    }

    if (!paneId || !paneId.startsWith('%')) {
      throw lastError ?? new Error("Failed to create dashboard pane after 3 attempts");
    }

    // Wait for pane to be fully ready
    await Bun.sleep(500);

    // Get PID with retry
    let pid = 0;
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const panePidResult =
          await Bun.$`tmux list-panes -F '#{pane_id} #{pane_pid}' | grep "^${paneId} "`.text();
        pid = parseInt(panePidResult.split(" ")[1]?.trim() ?? "0", 10);
        if (pid > 0) break;
      } catch {
        await Bun.sleep(300);
      }
    }

    // Set pane title
    const shortTitle =
      task.title.substring(0, 20) + (task.title.length > 20 ? "..." : "");
    await Bun.$`tmux select-pane -t ${paneId} -T "Task:${shortTitle}"`.quiet();

    // Discover OpenCode port if using TUI mode (--port 0)
    let opencodePort: number | undefined;
    if (useTui && pid > 0) {
      // Wait for OpenCode to start its HTTP server
      await Bun.sleep(2500);
      const discoveredPort = await discoverOpencodePort(pid);
      opencodePort = discoveredPort ?? undefined;
    }

    if (isDebugEnabled()) {
      console.log(
        `[OpencodeExecutor] Created dashboard pane: ${paneId} (pid: ${pid}, port: ${opencodePort ?? 'unknown'})`
      );
    }

    return {
      pid,
      proc: null,
      paneId,
      promptFile,
      opencodePort,
    };
  }

  /**
   * Wait for a tmux pane to be ready (exists and accessible).
   */
  private async waitForPaneReady(paneId: string, timeoutMs: number): Promise<boolean> {
    const startTime = Date.now();
    
    while (Date.now() - startTime < timeoutMs) {
      try {
        const result = await Bun.$`tmux list-panes -a -F '#{pane_id}'`.text();
        const panes = result.trim().split('\n');
        if (panes.includes(paneId)) {
          return true;
        }
      } catch {
        // tmux command failed, keep trying
      }
      await Bun.sleep(100);
    }
    
    return false;
  }

  // ========================================
  // Cleanup
  // ========================================

  async cleanup(taskId: string, projectId: string): Promise<void> {
    const promptFile = join(this.stateDir, `prompt_${projectId}_${taskId}.txt`);
    const runnerScript = join(
      this.stateDir,
      `runner_${projectId}_${taskId}.sh`
    );
    const outputFile = join(this.stateDir, `output_${projectId}_${taskId}.log`);

    for (const file of [promptFile, runnerScript, outputFile]) {
      try {
        if (existsSync(file)) {
          await Bun.$`rm -f ${file}`.quiet();
          if (isDebugEnabled()) {
            console.log(`[OpencodeExecutor] Cleaned up: ${file}`);
          }
        }
      } catch (error) {
        if (isDebugEnabled()) {
          console.log(`[OpencodeExecutor] Failed to cleanup: ${file}`);
        }
      }
    }
  }

  // ========================================
  // Helpers
  // ========================================

  private async ensureStateDir(): Promise<void> {
    if (!existsSync(this.stateDir)) {
      await Bun.$`mkdir -p ${this.stateDir}`;
    }
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let executorInstance: OpencodeExecutor | null = null;

export function getOpencodeExecutor(): OpencodeExecutor {
  if (!executorInstance) {
    executorInstance = new OpencodeExecutor();
  }
  return executorInstance;
}

/**
 * Reset the executor singleton (useful for testing).
 */
export function resetOpencodeExecutor(): void {
  executorInstance = null;
}
