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
    // If direct_prompt is set, use it verbatim (bypasses do-work skill workflow)
    if (task.direct_prompt) {
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Using direct_prompt for task: ${task.id}`);
      }
      return task.direct_prompt;
    }

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

  /**
   * Get the effective agent for a task (task override or config default)
   */
  private getEffectiveAgent(task: ResolvedTask): string {
    return task.agent ?? this.config.opencode.agent;
  }

  /**
   * Get the effective model for a task (task override or config default)
   */
  private getEffectiveModel(task: ResolvedTask): string {
    return task.model ?? this.config.opencode.model;
  }

  // ========================================
  // Workdir Resolution
  // ========================================

  async resolveWorkdir(task: ResolvedTask): Promise<string> {
    // Priority: target_workdir > ensured worktree > task worktree > task workdir > resolved_workdir > config default

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

    // Try to ensure worktree exists (creates if needed with git_branch)
    const worktreePath = await this.ensureWorktree(task);
    if (worktreePath) {
      return worktreePath;
    }

    if (task.worktree) {
      const worktreePathExisting = join(homedir(), task.worktree);
      if (existsSync(worktreePathExisting)) {
        return worktreePathExisting;
      }
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Worktree not found: ${worktreePathExisting}`);
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
    const workdir = options.workdir ?? await this.resolveWorkdir(task);

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

    // Use task-level overrides if provided
    const agent = this.getEffectiveAgent(task);
    const model = this.getEffectiveModel(task);

    const proc = Bun.spawn({
      cmd: [
        this.config.opencode.bin,
        "run",
        "--agent",
        agent,
        "--model",
        model,
        promptContent,
      ],
      cwd: workdir,
      stdout: Bun.file(outputFile),
      stderr: Bun.file(outputFile),
    });

    if (isDebugEnabled()) {
      console.log(
        `[OpencodeExecutor] Background process started (PID: ${proc.pid}, agent: ${agent}, model: ${model})`
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
    // Use short ID for cleaner tmux window names
    // For proper 8-char IDs: use as-is
    // For timestamp-slug IDs (1770551481834-update-...): use last 8 chars from slug
    const shortId = task.id.length > 8 
      ? task.id.slice(-8)  // Last 8 chars (from slug portion)
      : task.id;           // Already short
    const name = windowName ?? `${projectId}-${shortId}`;

    // Use task-level overrides if provided
    const agent = this.getEffectiveAgent(task);
    const model = this.getEffectiveModel(task);

    // Build runner script that OpenCode TUI runs in
    const runnerScript = join(
      this.stateDir,
      `runner_${projectId}_${task.id}.sh`
    );
    const script = `#!/bin/bash
cd "${workdir}"
"${this.config.opencode.bin}" --agent "${agent}" --model "${model}" --port 0 --prompt "$(cat '${promptFile}')"
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
    // Use task-level overrides if provided
    const agent = this.getEffectiveAgent(task);
    const model = this.getEffectiveModel(task);

    // Build runner script
    // When useTui is true, use interactive command (--port 0 --prompt) instead of headless (run)
    const runnerScript = join(
      this.stateDir,
      `runner_${projectId}_${task.id}.sh`
    );
    const opencodeCmd = useTui
      ? `"${this.config.opencode.bin}" --agent "${agent}" --model "${model}" --port 0 --prompt "$(cat '${promptFile}')"`
      : `"${this.config.opencode.bin}" run --agent "${agent}" --model "${model}" "$(cat '${promptFile}')"`;
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

  // ========================================
  // Git Worktree Support
  // ========================================

  /**
   * Ensure worktree exists for task. Creates it if missing and runs setup agent.
   * 
   * @returns The worktree path if created/exists
   * @throws Error if worktree creation or setup fails (caller should mark task as blocked)
   */
  async ensureWorktree(task: ResolvedTask): Promise<string | null> {
    if (!task.workdir || !task.git_branch) {
      return null; // No branch context, use main workdir
    }
    
    const home = homedir();
    const mainRepoPath = join(home, task.workdir);
    
    // Skip worktree for default branch (main/master)
    // These should use the main repo directly
    const defaultBranches = ['main', 'master'];
    if (defaultBranches.includes(task.git_branch)) {
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Skipping worktree for default branch: ${task.git_branch}`);
      }
      return null;
    }

    // Check if this branch is currently checked out in main repo
    try {
      const currentBranch = await Bun.$`git -C ${mainRepoPath} branch --show-current`.text();
      if (currentBranch.trim() === task.git_branch) {
        if (isDebugEnabled()) {
          console.log(`[OpencodeExecutor] Branch ${task.git_branch} already checked out in main repo`);
        }
        return null;
      }
    } catch (error) {
      // If we can't determine current branch, proceed with worktree attempt
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Could not determine current branch: ${error}`);
      }
    }

    // Check if branch is already checked out in ANY worktree (handles sibling pattern)
    try {
      const worktreeList = await Bun.$`git -C ${mainRepoPath} worktree list --porcelain`.text();
      const existingPath = this.findWorktreeForBranch(worktreeList, task.git_branch);
      if (existingPath) {
        if (isDebugEnabled()) {
          console.log(`[OpencodeExecutor] Found existing worktree for ${task.git_branch} at ${existingPath}`);
        }
        return existingPath;
      }
    } catch (error) {
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Could not list worktrees: ${error}`);
      }
    }
    
    const sanitizedBranch = task.git_branch.replace(/\//g, '-').replace(/[^a-zA-Z0-9-_]/g, '');
    const worktreePath = join(mainRepoPath, '.worktrees', sanitizedBranch);
    
    // Check if worktree already exists
    if (existsSync(worktreePath)) {
      return worktreePath;
    }
    
    // Check if main repo exists
    if (!existsSync(mainRepoPath)) {
      throw new Error(`Main repo not found: ${mainRepoPath}`);
    }
    
    // Ensure .worktrees is in .gitignore
    await this.ensureWorktreesIgnored(mainRepoPath);
    
    // Create .worktrees directory if needed
    const worktreesDir = join(mainRepoPath, '.worktrees');
    if (!existsSync(worktreesDir)) {
      await Bun.$`mkdir -p ${worktreesDir}`;
    }
    
    // Check if branch exists
    const branchExists = await this.checkBranchExists(mainRepoPath, task.git_branch);
    
    if (isDebugEnabled()) {
      console.log(`[OpencodeExecutor] Creating worktree: ${worktreePath} for branch: ${task.git_branch}`);
    }
    
    try {
      if (branchExists) {
        await Bun.$`git -C ${mainRepoPath} worktree add ${worktreePath} ${task.git_branch}`;
      } else {
        const defaultBranch = await this.getDefaultBranch(mainRepoPath);
        await Bun.$`git -C ${mainRepoPath} worktree add -b ${task.git_branch} ${worktreePath} ${defaultBranch}`;
      }
    } catch (error) {
      throw new Error(`Failed to create worktree: ${error}`);
    }
    
    if (isDebugEnabled()) {
      console.log(`[OpencodeExecutor] Worktree created, running setup agent`);
    }
    
    // Run setup agent (throws on failure)
    await this.runSetupAgent(worktreePath, task);
    
    return worktreePath;
  }

  /**
   * Parse git worktree list --porcelain output to find worktree path for a branch
   * 
   * Output format:
   * worktree /path/to/worktree
   * HEAD abc123
   * branch refs/heads/branch-name
   * 
   * worktree /path/to/another
   * ...
   */
  private findWorktreeForBranch(porcelainOutput: string, branch: string): string | null {
    const lines = porcelainOutput.split('\n');
    let currentPath: string | null = null;
    
    for (const line of lines) {
      if (line.startsWith('worktree ')) {
        currentPath = line.substring('worktree '.length);
      } else if (line.startsWith('branch refs/heads/')) {
        const branchName = line.substring('branch refs/heads/'.length);
        if (branchName === branch && currentPath) {
          return currentPath;
        }
      } else if (line === '') {
        currentPath = null; // Reset for next worktree block
      }
    }
    
    return null;
  }

  private async checkBranchExists(repoPath: string, branch: string): Promise<boolean> {
    try {
      await Bun.$`git -C ${repoPath} rev-parse --verify ${branch}`.quiet();
      return true;
    } catch {
      return false;
    }
  }

  private async getDefaultBranch(repoPath: string): Promise<string> {
    try {
      const result = await Bun.$`git -C ${repoPath} symbolic-ref refs/remotes/origin/HEAD`.text();
      return result.trim().replace('refs/remotes/origin/', '');
    } catch {
      try {
        await Bun.$`git -C ${repoPath} rev-parse --verify main`.quiet();
        return 'main';
      } catch {
        return 'master';
      }
    }
  }

  private async ensureWorktreesIgnored(repoPath: string): Promise<void> {
    const gitignorePath = join(repoPath, '.gitignore');
    
    try {
      if (existsSync(gitignorePath)) {
        const content = await Bun.file(gitignorePath).text();
        if (content.includes('.worktrees')) {
          return; // Already ignored
        }
        await Bun.write(gitignorePath, content.trimEnd() + '\n\n# Local git worktrees\n.worktrees/\n');
      } else {
        await Bun.write(gitignorePath, '# Local git worktrees\n.worktrees/\n');
      }
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Added .worktrees/ to .gitignore`);
      }
    } catch (error) {
      console.warn(`[OpencodeExecutor] Failed to update .gitignore: ${error}`);
    }
  }

  private async runSetupAgent(worktreePath: string, task: ResolvedTask): Promise<void> {
    const setupPrompt = `You are setting up a new git worktree for development.

Worktree path: ${worktreePath}
Branch: ${task.git_branch}
Task that will run here: ${task.title}

Please:
1. Check if there's a setup script (setup.sh, Makefile setup, etc.)
2. Install dependencies (npm install, pip install, etc.) based on project type
3. Set up any necessary environment (copy .env.example to .env, etc.)
4. Verify the setup works (run a quick build/typecheck if applicable)

Keep it brief - just ensure the environment is ready for development.
If setup succeeds, output "SETUP_SUCCESS" at the end.
If setup fails, output "SETUP_FAILED: <reason>" at the end.`;

    const TIMEOUT_MS = 120000; // 2 minutes

    try {
      const shellPromise = Bun.$`${this.config.opencode.bin} run --agent ${this.config.opencode.agent} --model ${this.config.opencode.model} ${setupPrompt}`
        .cwd(worktreePath)
        .text();
      
      const timeoutPromise = new Promise<never>((_, reject) => {
        setTimeout(() => reject(new Error('TIMEOUT')), TIMEOUT_MS);
      });

      const result = await Promise.race([shellPromise, timeoutPromise]);
      
      if (result.includes('SETUP_FAILED:')) {
        const failureMatch = result.match(/SETUP_FAILED:\s*(.+)/);
        const reason = failureMatch?.[1] ?? 'Unknown reason';
        throw new Error(`Setup agent reported failure: ${reason}`);
      }
      
      if (isDebugEnabled()) {
        console.log(`[OpencodeExecutor] Setup agent completed successfully`);
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      const isTimeout = errorMessage === 'TIMEOUT';
      
      throw new Error(
        isTimeout 
          ? `Setup agent timed out after 2 minutes` 
          : `Setup agent failed: ${errorMessage}`
      );
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
