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

// =============================================================================
// Types
// =============================================================================

export interface SpawnOptions {
  mode: ExecutionMode;
  workdir?: string;
  isResume?: boolean;
  paneId?: string;
  windowName?: string;
}

export interface SpawnResult {
  pid: number;
  proc: ReturnType<typeof Bun.spawn> | null;
  paneId?: string;
  windowName?: string;
  promptFile: string;
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
    // Priority: task worktree > task workdir > resolved_workdir > config default

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
          options.paneId
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
    const name = windowName ?? `task_${task.id}`;

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
if [[ $exit_code -eq 0 ]]; then
  echo "Task completed. Window closing in 3s..."
  sleep 3
else
  echo "Exit code: $exit_code. Press Enter to close."
  read
fi
exit 0
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

    if (isDebugEnabled()) {
      console.log(
        `[OpencodeExecutor] Created tmux window: ${name} (pid: ${pid})`
      );
    }

    return {
      pid,
      proc: null, // Can't track tmux process directly
      windowName: name,
      promptFile,
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
    targetPane?: string
  ): Promise<SpawnResult> {
    // Build runner script
    const runnerScript = join(
      this.stateDir,
      `runner_${projectId}_${task.id}.sh`
    );
    const script = `#!/bin/bash
cd "${workdir}"
"${this.config.opencode.bin}" run --agent "${this.config.opencode.agent}" --model "${this.config.opencode.model}" "$(cat '${promptFile}')"
exit_code=$?
echo ""
if [[ $exit_code -eq 0 ]]; then
  echo "Task completed. Pane closing in 3s..."
  sleep 3
else
  echo "Exit code: $exit_code. Press Enter to close."
  read
fi
exit 0
`;
    await Bun.write(runnerScript, script);
    await Bun.$`chmod +x ${runnerScript}`;

    // Split existing pane horizontally
    const splitCmd = targetPane
      ? Bun.$`tmux split-window -t ${targetPane} -h -d -P -F '#{pane_id}' ${runnerScript}`
      : Bun.$`tmux split-window -h -d -P -F '#{pane_id}' ${runnerScript}`;

    const paneIdResult = await splitCmd.text();
    const paneId = paneIdResult.trim();

    // Wait for process to start
    await new Promise((resolve) => setTimeout(resolve, 2000));

    // Get PID
    const panePidResult =
      await Bun.$`tmux list-panes -F '#{pane_id} #{pane_pid}' | grep "^${paneId} "`.text();
    const pid = parseInt(panePidResult.split(" ")[1]?.trim() ?? "0", 10);

    // Set pane title
    const shortTitle =
      task.title.substring(0, 20) + (task.title.length > 20 ? "..." : "");
    await Bun.$`tmux select-pane -t ${paneId} -T "Task:${shortTitle}"`.quiet();

    if (isDebugEnabled()) {
      console.log(
        `[OpencodeExecutor] Created dashboard pane: ${paneId} (pid: ${pid})`
      );
    }

    return {
      pid,
      proc: null,
      paneId,
      promptFile,
    };
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
