# Brain Runner Implementation Plan

**Version:** 1.0.0  
**Status:** Draft  
**Created:** 2024-02-03  

---

## Overview

### What We're Building

Brain Runner is a TypeScript-based task execution daemon that replaces the `do-work` bash script. It polls the Brain API for ready tasks, spawns OpenCode agents to execute them, and manages parallel task execution with proper state persistence and recovery.

### Why TypeScript?

1. **Type Safety:** Leverage existing types from `src/core/types.ts`
2. **Shared Code:** Reuse `TaskService` and dependency resolution logic
3. **Better Testing:** Integration with existing Bun test infrastructure
4. **Maintainability:** Easier to extend and debug than 4000+ line bash script
5. **Cross-Platform:** More consistent behavior across environments

### Key Features to Port

| Feature | Bash Implementation | TypeScript Approach |
|---------|---------------------|---------------------|
| Task Polling | `api_get_ready_tasks()` | `ApiClient.getReadyTasks()` |
| Parallel Execution | Bash associative arrays | `ProcessManager` with `Map` |
| State Persistence | JSON files in `~/.local/state` | `StateManager` class |
| OpenCode Spawning | `spawn_opencode_async()` | `OpencodeExecutor` class |
| Signal Handling | `trap` handlers | Node.js process signals |
| tmux Dashboard | Shell scripts in panes | `TmuxManager` class |

---

## Architecture Diagram

```
BRAIN SERVER (can run anywhere)         BRAIN-RUNNER (local execution)
+---------------------------+           +-----------------------------+
|  Task Service             |<-- poll --|  TaskRunner                 |
|  - /tasks/:project/ready  |           |  - Poll for ready tasks     |
|  - /tasks/:project/next   |           |  - Spawn OpenCode           |
|  Entry Service            |<-- update-|  ProcessManager             |
|  - PATCH /entries/:path   |           |  - Track PIDs               |
+---------------------------+           |  - Signal handling          |
                                        |  StateManager               |
                                        |  - Persist running tasks    |
                                        |  - Recovery on restart      |
                                        +-----------------------------+

                                        TMUX (optional dashboard)
                                        +-----------------------------+
                                        | [Tasks] | [OpenCode Panes]  |
                                        |         |                   |
                                        |  Ready  |  Task 1 (running) |
                                        |  - A    |  Task 2 (running) |
                                        |  - B    |  Task 3 (running) |
                                        |         +-------------------+
                                        | Waiting |      [Logs]       |
                                        |  - C    |  [INFO] ...       |
                                        +-----------------------------+
```

---

## Module Structure

```
src/runner/
+-- index.ts              # CLI entry point
+-- task-runner.ts        # Main orchestration class
+-- process-manager.ts    # Track spawned OpenCode processes
+-- state-manager.ts      # Persist state to JSON files
+-- api-client.ts         # HTTP client for brain
+-- opencode-executor.ts  # Build prompts, spawn OpenCode
+-- tmux-manager.ts       # Optional: tmux dashboard
+-- config.ts             # Configuration with env vars
+-- types.ts              # TypeScript interfaces
+-- logger.ts             # Structured logging
```

---

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BRAIN_API_URL` | `http://localhost:3000` | Brain API server URL |
| `RUNNER_POLL_INTERVAL` | `30` | Seconds between polling for new tasks |
| `RUNNER_TASK_POLL_INTERVAL` | `5` | Seconds between checking task completion |
| `RUNNER_MAX_PARALLEL` | `3` | Maximum concurrent tasks |
| `RUNNER_STATE_DIR` | `~/.local/state/brain-runner` | State persistence directory |
| `RUNNER_LOG_DIR` | `~/.local/log` | Log file directory |
| `RUNNER_WORK_DIR` | `$HOME` | Default working directory for OpenCode |
| `OPENCODE_BIN` | `opencode` | Path to OpenCode binary |
| `OPENCODE_AGENT` | `general` | Default OpenCode agent |
| `OPENCODE_MODEL` | `anthropic/claude-opus-4-5` | Default model |
| `RUNNER_API_TIMEOUT` | `5000` | API request timeout (ms) |
| `RUNNER_TASK_TIMEOUT` | `1800000` | Max task execution time (ms, 30 min) |
| `DEBUG` | `false` | Enable debug logging |

### Configuration File (Optional)

Location: `~/.config/brain-runner/config.json`

```json
{
  "brainApiUrl": "http://localhost:3000",
  "pollInterval": 30,
  "maxParallel": 3,
  "opencode": {
    "bin": "opencode",
    "agent": "general",
    "model": "anthropic/claude-opus-4-5"
  },
  "excludeProjects": ["test-*", "*-draft"]
}
```

---

## CLI Reference

### Commands

```bash
# Start the runner daemon
brain-runner start <project-id> [options]
brain-runner start all                    # Monitor all projects

# Run a single task
brain-runner run-one <project-id> [options]
brain-runner run-one all                  # Pick from any project

# Control running daemon
brain-runner stop [project-id]            # Stop specific or all
brain-runner status [project-id]          # Show status
brain-runner logs [-f]                    # View logs

# Query tasks (uses brain)
brain-runner list <project-id>            # List all tasks
brain-runner ready <project-id>           # List ready tasks
brain-runner waiting <project-id>         # List waiting tasks
brain-runner blocked <project-id>         # List blocked tasks
```

### Options

| Option | Short | Description |
|--------|-------|-------------|
| `--foreground` | `-f` | Run in foreground (default) |
| `--background` | `-b` | Run as daemon |
| `--tui` | | Use interactive OpenCode TUI |
| `--dashboard` | | Create tmux dashboard |
| `--max-parallel` | `-p` | Max concurrent tasks |
| `--poll-interval` | | Seconds between polls |
| `--workdir` | `-w` | Working directory |
| `--agent` | | OpenCode agent |
| `--model` | `-m` | Model to use |
| `--dry-run` | | Log actions without executing |
| `--exclude` | `-e` | Exclude project pattern |
| `--no-resume` | | Skip interrupted tasks |

---

## Implementation Tasks

---

## Task 1: Create runner types and configuration

**Priority:** high  
**Depends on:** None  
**Estimated effort:** 2 hours

### Objective

Define TypeScript interfaces and configuration loading for the brain-runner module. This establishes the foundation that all other modules depend on.

### Files to Create/Modify

- `src/runner/types.ts` - Type definitions
- `src/runner/config.ts` - Configuration loading

### Implementation Details

#### types.ts

```typescript
/**
 * Brain Runner Types
 */

import type { ResolvedTask, Priority, EntryStatus } from "../core/types";

// =============================================================================
// Configuration Types
// =============================================================================

export interface RunnerConfig {
  brainApiUrl: string;
  pollInterval: number;        // seconds
  taskPollInterval: number;    // seconds
  maxParallel: number;
  stateDir: string;
  logDir: string;
  workDir: string;
  apiTimeout: number;          // ms
  taskTimeout: number;         // ms
  
  opencode: OpencodeConfig;
  
  excludeProjects: string[];   // glob patterns
}

export interface OpencodeConfig {
  bin: string;
  agent: string;
  model: string;
}

// =============================================================================
// Execution Types
// =============================================================================

export type ExecutionMode = "tui" | "dashboard" | "background";

export interface RunningTask {
  id: string;
  path: string;
  title: string;
  priority: Priority;
  projectId: string;
  pid: number;
  paneId?: string;
  windowName?: string;
  startedAt: string;          // ISO timestamp
  isResume: boolean;
  workdir: string;
}

export interface TaskResult {
  taskId: string;
  status: "completed" | "failed" | "blocked" | "timeout" | "crashed";
  startedAt: string;
  completedAt: string;
  duration: number;           // ms
  exitCode?: number;
}

// =============================================================================
// State Types
// =============================================================================

export interface RunnerState {
  projectId: string;
  status: RunnerStatus;
  startedAt: string;
  updatedAt: string;
  runningTasks: RunningTask[];
  stats: RunnerStats;
  config: Partial<RunnerConfig>;
}

export type RunnerStatus = "idle" | "polling" | "processing" | "stopped";

export interface RunnerStats {
  completed: number;
  failed: number;
  totalRuntime: number;       // ms
}

// =============================================================================
// API Types
// =============================================================================

export interface ApiHealth {
  status: "healthy" | "degraded" | "unhealthy";
  zkAvailable: boolean;
  dbAvailable: boolean;
}

export interface ClaimResult {
  success: boolean;
  taskId: string;
  claimedBy?: string;
  message?: string;
}

// =============================================================================
// Event Types
// =============================================================================

export type RunnerEvent =
  | { type: "task_started"; task: RunningTask }
  | { type: "task_completed"; result: TaskResult }
  | { type: "task_failed"; result: TaskResult }
  | { type: "poll_complete"; readyCount: number; runningCount: number }
  | { type: "state_saved"; path: string }
  | { type: "shutdown"; reason: string };

export type EventHandler = (event: RunnerEvent) => void;
```

#### config.ts

```typescript
/**
 * Brain Runner Configuration
 */

import { homedir } from "os";
import { join } from "path";
import { existsSync, readFileSync } from "fs";
import type { RunnerConfig, OpencodeConfig } from "./types";

const CONFIG_FILE = join(homedir(), ".config", "brain-runner", "config.json");

function getEnv(key: string, defaultValue: string): string {
  return process.env[key] ?? defaultValue;
}

function getEnvInt(key: string, defaultValue: number): number {
  const value = process.env[key];
  if (!value) return defaultValue;
  const parsed = parseInt(value, 10);
  return isNaN(parsed) ? defaultValue : parsed;
}

function getEnvBool(key: string, defaultValue: boolean): boolean {
  const value = process.env[key];
  if (!value) return defaultValue;
  return value.toLowerCase() === "true" || value === "1";
}

function loadConfigFile(): Partial<RunnerConfig> | null {
  if (!existsSync(CONFIG_FILE)) return null;
  
  try {
    const content = readFileSync(CONFIG_FILE, "utf-8");
    return JSON.parse(content);
  } catch (error) {
    console.warn(`Failed to load config file: ${CONFIG_FILE}`, error);
    return null;
  }
}

export function loadConfig(): RunnerConfig {
  const fileConfig = loadConfigFile() ?? {};
  
  const opencode: OpencodeConfig = {
    bin: getEnv("OPENCODE_BIN", fileConfig.opencode?.bin ?? "opencode"),
    agent: getEnv("OPENCODE_AGENT", fileConfig.opencode?.agent ?? "general"),
    model: getEnv("OPENCODE_MODEL", fileConfig.opencode?.model ?? "anthropic/claude-opus-4-5"),
  };

  return {
    brainApiUrl: getEnv("BRAIN_API_URL", fileConfig.brainApiUrl ?? "http://localhost:3000"),
    pollInterval: getEnvInt("RUNNER_POLL_INTERVAL", fileConfig.pollInterval ?? 30),
    taskPollInterval: getEnvInt("RUNNER_TASK_POLL_INTERVAL", fileConfig.taskPollInterval ?? 5),
    maxParallel: getEnvInt("RUNNER_MAX_PARALLEL", fileConfig.maxParallel ?? 3),
    stateDir: getEnv("RUNNER_STATE_DIR", fileConfig.stateDir ?? join(homedir(), ".local", "state", "brain-runner")),
    logDir: getEnv("RUNNER_LOG_DIR", fileConfig.logDir ?? join(homedir(), ".local", "log")),
    workDir: getEnv("RUNNER_WORK_DIR", fileConfig.workDir ?? homedir()),
    apiTimeout: getEnvInt("RUNNER_API_TIMEOUT", fileConfig.apiTimeout ?? 5000),
    taskTimeout: getEnvInt("RUNNER_TASK_TIMEOUT", fileConfig.taskTimeout ?? 1800000),
    opencode,
    excludeProjects: fileConfig.excludeProjects ?? [],
  };
}

// Singleton instance
let config: RunnerConfig | null = null;

export function getRunnerConfig(): RunnerConfig {
  if (!config) {
    config = loadConfig();
  }
  return config;
}

export function isDebugEnabled(): boolean {
  return getEnvBool("DEBUG", false);
}
```

### Acceptance Criteria

- [ ] All type interfaces are defined and exported
- [ ] Config loads from environment variables
- [ ] Config loads from optional JSON file
- [ ] Environment variables override file config
- [ ] Default values are sensible
- [ ] Types compile without errors
- [ ] `getRunnerConfig()` returns singleton instance

### Test Cases

1. Test default config values when no env vars set
2. Test env var overrides
3. Test JSON file loading
4. Test JSON file with missing fields uses defaults
5. Test invalid JSON file logs warning and continues

---

## Task 2: Create brain client

**Priority:** high  
**Depends on:** Task 1  
**Estimated effort:** 3 hours

### Objective

Create an HTTP client for interacting with the brain server. This handles task queries, status updates, and task claiming.

### Files to Create/Modify

- `src/runner/api-client.ts` - HTTP client implementation

### Implementation Details

```typescript
/**
 * Brain API Client
 * 
 * HTTP client for task queries and status updates.
 * Includes retry logic and timeout handling.
 */

import type { 
  RunnerConfig, 
  ApiHealth, 
  ClaimResult 
} from "./types";
import type { 
  ResolvedTask, 
  TaskListResponse, 
  TaskNextResponse,
  EntryStatus 
} from "../core/types";
import { getRunnerConfig, isDebugEnabled } from "./config";
import { log } from "./logger";

// =============================================================================
// API Client Class
// =============================================================================

export class ApiClient {
  private config: RunnerConfig;
  private healthCache: { status: ApiHealth | null; timestamp: number } = {
    status: null,
    timestamp: 0,
  };
  private readonly healthCacheTtl = 10_000; // 10 seconds

  constructor(config?: RunnerConfig) {
    this.config = config ?? getRunnerConfig();
  }

  // ========================================
  // Health Check
  // ========================================

  async checkHealth(): Promise<ApiHealth> {
    const now = Date.now();
    
    // Return cached result if recent
    if (
      this.healthCache.status &&
      now - this.healthCache.timestamp < this.healthCacheTtl
    ) {
      return this.healthCache.status;
    }

    try {
      const response = await this.fetch("/health");
      const health = (await response.json()) as ApiHealth;
      
      this.healthCache = { status: health, timestamp: now };
      return health;
    } catch (error) {
      const unhealthy: ApiHealth = {
        status: "unhealthy",
        zkAvailable: false,
        dbAvailable: false,
      };
      this.healthCache = { status: unhealthy, timestamp: now };
      return unhealthy;
    }
  }

  async isAvailable(): Promise<boolean> {
    const health = await this.checkHealth();
    return health.status !== "unhealthy";
  }

  // ========================================
  // Task Queries
  // ========================================

  async getReadyTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/ready`);
    
    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
    
    const data = (await response.json()) as TaskListResponse;
    return data.tasks;
  }

  async getNextTask(projectId: string): Promise<ResolvedTask | null> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/next`);
    
    if (response.status === 404) {
      return null;
    }
    
    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
    
    const data = (await response.json()) as TaskNextResponse;
    return data.task;
  }

  async getAllTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}`);
    
    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
    
    const data = (await response.json()) as { tasks: ResolvedTask[] };
    return data.tasks;
  }

  async getWaitingTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/waiting`);
    
    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
    
    const data = (await response.json()) as TaskListResponse;
    return data.tasks;
  }

  async getBlockedTasks(projectId: string): Promise<ResolvedTask[]> {
    const response = await this.fetch(`/api/v1/tasks/${projectId}/blocked`);
    
    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
    
    const data = (await response.json()) as TaskListResponse;
    return data.tasks;
  }

  // ========================================
  // Task Status Updates
  // ========================================

  async updateTaskStatus(taskPath: string, status: EntryStatus): Promise<void> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  async appendToTask(taskPath: string, content: string): Promise<void> {
    const encodedPath = encodeURIComponent(taskPath);
    const response = await this.fetch(`/api/v1/entries/${encodedPath}`, {
      method: "PATCH",
      body: JSON.stringify({ append: content }),
    });

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  // ========================================
  // Task Claiming (requires API extension)
  // ========================================

  async claimTask(projectId: string, taskId: string, runnerId: string): Promise<ClaimResult> {
    const response = await this.fetch(
      `/api/v1/tasks/${projectId}/${taskId}/claim`,
      {
        method: "POST",
        body: JSON.stringify({ runnerId }),
      }
    );

    if (response.status === 409) {
      // Task already claimed
      const data = await response.json();
      return {
        success: false,
        taskId,
        claimedBy: data.claimedBy,
        message: "Task already claimed",
      };
    }

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }

    return { success: true, taskId };
  }

  async releaseTask(projectId: string, taskId: string): Promise<void> {
    const response = await this.fetch(
      `/api/v1/tasks/${projectId}/${taskId}/release`,
      { method: "POST" }
    );

    if (!response.ok) {
      throw new ApiError(response.status, await response.text());
    }
  }

  // ========================================
  // Internal Helpers
  // ========================================

  private async fetch(
    path: string,
    options: RequestInit = {}
  ): Promise<Response> {
    const url = `${this.config.brainApiUrl}${path}`;
    
    const controller = new AbortController();
    const timeout = setTimeout(
      () => controller.abort(),
      this.config.apiTimeout
    );

    try {
      if (isDebugEnabled()) {
        log.debug(`API ${options.method ?? "GET"} ${path}`);
      }

      const response = await fetch(url, {
        ...options,
        signal: controller.signal,
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          ...options.headers,
        },
      });

      return response;
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        throw new ApiError(408, `Request timeout after ${this.config.apiTimeout}ms`);
      }
      throw error;
    } finally {
      clearTimeout(timeout);
    }
  }
}

// =============================================================================
// Error Class
// =============================================================================

export class ApiError extends Error {
  constructor(
    public readonly statusCode: number,
    message: string
  ) {
    super(`API Error (${statusCode}): ${message}`);
    this.name = "ApiError";
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let apiClientInstance: ApiClient | null = null;

export function getApiClient(): ApiClient {
  if (!apiClientInstance) {
    apiClientInstance = new ApiClient();
  }
  return apiClientInstance;
}
```

### Acceptance Criteria

- [ ] Health check with caching works
- [ ] All task query methods work (`ready`, `next`, `waiting`, `blocked`)
- [ ] Task status updates work
- [ ] Request timeout handling works
- [ ] Error responses throw `ApiError` with status code
- [ ] Singleton instance is accessible

### Test Cases

1. Test health check caching (doesn't re-fetch within TTL)
2. Test `getReadyTasks` returns array
3. Test `getNextTask` returns task or null for 404
4. Test `updateTaskStatus` sends correct payload
5. Test timeout triggers abort
6. Test network errors are handled gracefully
7. Mock server tests for all endpoints

---

## Task 3: Create process manager

**Priority:** high  
**Depends on:** Task 1  
**Estimated effort:** 3 hours

### Objective

Track spawned OpenCode processes, detect completion, and handle cleanup. This is the core of parallel task execution.

### Files to Create/Modify

- `src/runner/process-manager.ts` - Process tracking and management

### Implementation Details

```typescript
/**
 * Process Manager
 * 
 * Tracks spawned OpenCode processes, detects completion,
 * and handles cleanup.
 */

import type { RunningTask, TaskResult } from "./types";
import { log } from "./logger";

// =============================================================================
// Types
// =============================================================================

export interface ProcessInfo {
  task: RunningTask;
  proc: ReturnType<typeof Bun.spawn> | null;
  exitCode: number | null;
  exited: boolean;
}

export type CompletionStatus = 
  | "running" 
  | "completed" 
  | "failed" 
  | "blocked" 
  | "timeout" 
  | "crashed";

// =============================================================================
// Process Manager Class
// =============================================================================

export class ProcessManager {
  private processes: Map<string, ProcessInfo> = new Map();
  private taskTimeout: number;

  constructor(taskTimeout: number = 1800_000) {
    this.taskTimeout = taskTimeout;
  }

  // ========================================
  // Process Tracking
  // ========================================

  add(taskId: string, task: RunningTask, proc: ReturnType<typeof Bun.spawn> | null): void {
    this.processes.set(taskId, {
      task,
      proc,
      exitCode: null,
      exited: false,
    });
    
    // Set up exit handler if we have a process
    if (proc) {
      proc.exited.then((code) => {
        const info = this.processes.get(taskId);
        if (info) {
          info.exitCode = code;
          info.exited = true;
        }
      }).catch((error) => {
        log.error(`Process exit handler error for ${taskId}:`, error);
      });
    }
    
    log.info(`Process added: ${task.title} (taskId: ${taskId}, pid: ${proc?.pid ?? "none"})`);
  }

  remove(taskId: string): RunningTask | undefined {
    const info = this.processes.get(taskId);
    if (!info) return undefined;
    
    this.processes.delete(taskId);
    log.info(`Process removed: ${info.task.title} (taskId: ${taskId})`);
    
    return info.task;
  }

  get(taskId: string): ProcessInfo | undefined {
    return this.processes.get(taskId);
  }

  isRunning(taskId: string): boolean {
    const info = this.processes.get(taskId);
    if (!info) return false;
    
    // Check if process is still alive
    if (info.proc && !info.exited) {
      return true;
    }
    
    return false;
  }

  getAll(): Map<string, ProcessInfo> {
    return new Map(this.processes);
  }

  getAllRunning(): RunningTask[] {
    return Array.from(this.processes.values())
      .filter((info) => !info.exited)
      .map((info) => info.task);
  }

  count(): number {
    return this.processes.size;
  }

  runningCount(): number {
    return Array.from(this.processes.values())
      .filter((info) => !info.exited)
      .length;
  }

  // ========================================
  // Completion Detection
  // ========================================

  /**
   * Check if a task has completed (non-blocking)
   * 
   * Checks:
   * 1. Task file status (completed/blocked/failed)
   * 2. Process exit
   * 3. Timeout
   */
  async checkCompletion(
    taskId: string,
    checkTaskFile: (path: string) => Promise<string | null>
  ): Promise<CompletionStatus> {
    const info = this.processes.get(taskId);
    if (!info) return "crashed";

    const { task, proc, exited, exitCode } = info;

    // 1. Check task file status
    const fileStatus = await checkTaskFile(task.path);
    if (fileStatus === "completed") return "completed";
    if (fileStatus === "blocked") return "blocked";
    if (fileStatus === "failed") return "failed";

    // 2. Check if process exited
    if (exited) {
      // Process exited but status wasn't updated - treat as crashed
      log.warn(`Process exited (code: ${exitCode}) but task status not updated: ${task.title}`);
      return "crashed";
    }

    // 3. Check timeout
    const elapsed = Date.now() - new Date(task.startedAt).getTime();
    if (elapsed > this.taskTimeout) {
      log.warn(`Task timeout (${elapsed}ms): ${task.title}`);
      return "timeout";
    }

    return "running";
  }

  // ========================================
  // Process Control
  // ========================================

  async kill(taskId: string, signal: NodeJS.Signals = "SIGTERM"): Promise<boolean> {
    const info = this.processes.get(taskId);
    if (!info?.proc) return false;

    try {
      info.proc.kill(signal === "SIGTERM" ? 15 : 9);
      
      // Wait a bit for graceful shutdown
      await new Promise((resolve) => setTimeout(resolve, 1000));
      
      // Force kill if still running
      if (!info.exited) {
        info.proc.kill(9);
      }
      
      return true;
    } catch (error) {
      log.error(`Failed to kill process for ${taskId}:`, error);
      return false;
    }
  }

  async killAll(): Promise<void> {
    const taskIds = Array.from(this.processes.keys());
    
    log.info(`Killing all ${taskIds.length} processes...`);
    
    // First, send SIGTERM to all
    for (const taskId of taskIds) {
      const info = this.processes.get(taskId);
      if (info?.proc && !info.exited) {
        info.proc.kill(15);
      }
    }
    
    // Wait for graceful shutdown
    await new Promise((resolve) => setTimeout(resolve, 2000));
    
    // Force kill remaining
    for (const taskId of taskIds) {
      const info = this.processes.get(taskId);
      if (info?.proc && !info.exited) {
        log.warn(`Force killing: ${info.task.title}`);
        info.proc.kill(9);
      }
    }
    
    this.processes.clear();
  }

  // ========================================
  // Serialization (for state persistence)
  // ========================================

  toJSON(): RunningTask[] {
    return Array.from(this.processes.values()).map((info) => info.task);
  }

  restoreFromState(tasks: RunningTask[]): void {
    // Note: We can't restore actual process handles, only the metadata
    // This is used to check if processes are still running after restart
    for (const task of tasks) {
      // Check if PID is still alive
      try {
        process.kill(task.pid, 0);
        // Process is alive - add it with null proc reference
        this.processes.set(task.id, {
          task,
          proc: null,
          exitCode: null,
          exited: false,
        });
        log.info(`Restored running task: ${task.title} (pid: ${task.pid})`);
      } catch {
        // Process is dead - don't restore
        log.warn(`Task process no longer running: ${task.title} (pid: ${task.pid})`);
      }
    }
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let processManagerInstance: ProcessManager | null = null;

export function getProcessManager(taskTimeout?: number): ProcessManager {
  if (!processManagerInstance) {
    processManagerInstance = new ProcessManager(taskTimeout);
  }
  return processManagerInstance;
}
```

### Acceptance Criteria

- [ ] `add()` tracks process with exit handler
- [ ] `remove()` returns task info
- [ ] `isRunning()` correctly detects process state
- [ ] `checkCompletion()` returns correct status
- [ ] `kill()` sends signals and waits for exit
- [ ] `killAll()` gracefully terminates all processes
- [ ] `toJSON()` serializes running tasks
- [ ] `restoreFromState()` only restores living processes

### Test Cases

1. Test adding and removing processes
2. Test process exit detection
3. Test timeout detection
4. Test killing processes
5. Test state serialization
6. Test restoration only restores living PIDs
7. Integration test with actual spawned process

---

## Task 4: Create state manager

**Priority:** high  
**Depends on:** Task 1, Task 3  
**Estimated effort:** 2 hours

### Objective

Persist runner state to JSON files for recovery after restart. This enables resuming interrupted tasks.

### Files to Create/Modify

- `src/runner/state-manager.ts` - State persistence

### Implementation Details

```typescript
/**
 * State Manager
 * 
 * Persists runner state to JSON files for recovery after restart.
 */

import { existsSync, mkdirSync, writeFileSync, readFileSync, unlinkSync } from "fs";
import { join } from "path";
import type { RunnerState, RunningTask, RunnerStats, RunnerStatus } from "./types";
import { getRunnerConfig } from "./config";
import { log } from "./logger";

// =============================================================================
// State Manager Class
// =============================================================================

export class StateManager {
  private stateDir: string;
  private projectId: string;

  constructor(projectId: string, stateDir?: string) {
    this.projectId = projectId;
    this.stateDir = stateDir ?? getRunnerConfig().stateDir;
    this.ensureDir();
  }

  // ========================================
  // File Paths
  // ========================================

  private get stateFile(): string {
    return join(this.stateDir, `runner-${this.projectId}.json`);
  }

  private get pidFile(): string {
    return join(this.stateDir, `runner-${this.projectId}.pid`);
  }

  private get runningTasksFile(): string {
    return join(this.stateDir, `running-${this.projectId}.json`);
  }

  // ========================================
  // Directory Management
  // ========================================

  private ensureDir(): void {
    if (!existsSync(this.stateDir)) {
      mkdirSync(this.stateDir, { recursive: true });
    }
  }

  // ========================================
  // State Operations
  // ========================================

  save(
    status: RunnerStatus,
    runningTasks: RunningTask[],
    stats: RunnerStats,
    startedAt?: string
  ): void {
    const state: RunnerState = {
      projectId: this.projectId,
      status,
      startedAt: startedAt ?? new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      runningTasks,
      stats,
      config: {
        maxParallel: getRunnerConfig().maxParallel,
        pollInterval: getRunnerConfig().pollInterval,
      },
    };

    try {
      writeFileSync(this.stateFile, JSON.stringify(state, null, 2));
      log.debug(`State saved: ${this.stateFile}`);
    } catch (error) {
      log.error(`Failed to save state:`, error);
    }
  }

  load(): RunnerState | null {
    if (!existsSync(this.stateFile)) {
      return null;
    }

    try {
      const content = readFileSync(this.stateFile, "utf-8");
      return JSON.parse(content) as RunnerState;
    } catch (error) {
      log.error(`Failed to load state:`, error);
      return null;
    }
  }

  clear(): void {
    try {
      if (existsSync(this.stateFile)) {
        unlinkSync(this.stateFile);
      }
      if (existsSync(this.runningTasksFile)) {
        unlinkSync(this.runningTasksFile);
      }
      log.debug(`State cleared for ${this.projectId}`);
    } catch (error) {
      log.error(`Failed to clear state:`, error);
    }
  }

  // ========================================
  // PID File Operations
  // ========================================

  savePid(pid: number): void {
    try {
      writeFileSync(this.pidFile, String(pid));
    } catch (error) {
      log.error(`Failed to save PID:`, error);
    }
  }

  loadPid(): number | null {
    if (!existsSync(this.pidFile)) {
      return null;
    }

    try {
      const content = readFileSync(this.pidFile, "utf-8");
      return parseInt(content.trim(), 10);
    } catch (error) {
      log.error(`Failed to load PID:`, error);
      return null;
    }
  }

  clearPid(): void {
    try {
      if (existsSync(this.pidFile)) {
        unlinkSync(this.pidFile);
      }
    } catch (error) {
      log.error(`Failed to clear PID:`, error);
    }
  }

  isPidRunning(): boolean {
    const pid = this.loadPid();
    if (!pid) return false;

    try {
      process.kill(pid, 0);
      return true;
    } catch {
      return false;
    }
  }

  // ========================================
  // Running Tasks Persistence
  // ========================================

  saveRunningTasks(tasks: RunningTask[]): void {
    try {
      writeFileSync(this.runningTasksFile, JSON.stringify(tasks, null, 2));
    } catch (error) {
      log.error(`Failed to save running tasks:`, error);
    }
  }

  loadRunningTasks(): RunningTask[] {
    if (!existsSync(this.runningTasksFile)) {
      return [];
    }

    try {
      const content = readFileSync(this.runningTasksFile, "utf-8");
      return JSON.parse(content) as RunningTask[];
    } catch (error) {
      log.error(`Failed to load running tasks:`, error);
      return [];
    }
  }
}

// =============================================================================
// Static Utility Functions
// =============================================================================

/**
 * Find all state files (for listing all runners)
 */
export function findAllRunnerStates(stateDir?: string): string[] {
  const dir = stateDir ?? getRunnerConfig().stateDir;
  
  if (!existsSync(dir)) {
    return [];
  }

  const files = Bun.file(dir).name ?? [];
  // This needs proper implementation with readdir
  return [];
}

/**
 * Clean up stale state files (where PID is no longer running)
 */
export function cleanupStaleStates(stateDir?: string): void {
  const dir = stateDir ?? getRunnerConfig().stateDir;
  // Implementation would iterate through state files
  // and remove those where PID is no longer running
}
```

### Acceptance Criteria

- [ ] State file is created in correct location
- [ ] State includes all required fields
- [ ] `load()` returns null for missing file
- [ ] `load()` handles corrupted JSON gracefully
- [ ] PID file operations work correctly
- [ ] `isPidRunning()` detects live/dead PIDs
- [ ] Running tasks can be persisted and loaded

### Test Cases

1. Test save and load round-trip
2. Test load with missing file returns null
3. Test load with corrupted JSON returns null
4. Test PID file operations
5. Test `isPidRunning()` with live PID
6. Test `isPidRunning()` with dead PID
7. Test clear removes all files

---

## Task 5: Create OpenCode executor

**Priority:** high  
**Depends on:** Task 1  
**Estimated effort:** 4 hours

### Objective

Build task prompts and spawn OpenCode processes. This is the core execution component that replaces `spawn_opencode_async()` and `spawn_opencode_standalone()` from the bash script.

### Files to Create/Modify

- `src/runner/opencode-executor.ts` - OpenCode spawning and prompt building

### Implementation Details

```typescript
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
  OpencodeConfig 
} from "./types";
import type { ResolvedTask } from "../core/types";
import { getRunnerConfig } from "./config";
import { log } from "./logger";

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
      log.warn(`Worktree not found: ${worktreePath}`);
    }

    if (task.workdir) {
      const workdirPath = join(homedir(), task.workdir);
      if (existsSync(workdirPath)) {
        return workdirPath;
      }
      log.warn(`Workdir not found: ${workdirPath}`);
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
    
    // Build and save prompt
    const prompt = this.buildPrompt(task, isResume);
    const promptFile = join(this.stateDir, `prompt_${projectId}_${task.id}.txt`);
    await Bun.write(promptFile, prompt);
    
    // Resolve workdir
    const workdir = options.workdir ?? this.resolveWorkdir(task);
    
    log.info(`Spawning OpenCode for: ${task.title}`);
    log.debug(`  Mode: ${mode}`);
    log.debug(`  Workdir: ${workdir}`);
    log.debug(`  Prompt file: ${promptFile}`);

    switch (mode) {
      case "background":
        return this.spawnBackground(task, projectId, workdir, promptFile);
      
      case "tui":
        return this.spawnTui(task, projectId, workdir, promptFile, options.windowName);
      
      case "dashboard":
        return this.spawnDashboard(task, projectId, workdir, promptFile, options.paneId);
      
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
    const outputFile = join(this.stateDir, `output_${projectId}_${task.id}.log`);
    
    const proc = Bun.spawn({
      cmd: [
        this.config.opencode.bin,
        "run",
        "--agent", this.config.opencode.agent,
        "--model", this.config.opencode.model,
        await Bun.file(promptFile).text(),
      ],
      cwd: workdir,
      stdout: Bun.file(outputFile),
      stderr: Bun.file(outputFile),
    });

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
    const runnerScript = join(this.stateDir, `runner_${projectId}_${task.id}.sh`);
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
    const panePidResult = await Bun.$`tmux list-panes -t ${name} -F '#{pane_pid}'`.text();
    const panePid = parseInt(panePidResult.trim(), 10);
    
    // Try to find the actual OpenCode PID
    let pid = panePid;
    try {
      const pgrepResult = await Bun.$`pgrep -P ${panePid} -f opencode`.text();
      const opencodeePid = parseInt(pgrepResult.trim(), 10);
      if (!isNaN(opencodeePid)) {
        pid = opencodeePid;
      }
    } catch {
      // pgrep failed, use pane pid
    }

    log.info(`Created tmux window: ${name} (pid: ${pid})`);

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
    const runnerScript = join(this.stateDir, `runner_${projectId}_${task.id}.sh`);
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
    const paneIdResult = await Bun.$`tmux split-window -t ${targetPane ?? ""} -h -d -P -F '#{pane_id}' ${runnerScript}`.text();
    const paneId = paneIdResult.trim();
    
    // Wait for process to start
    await new Promise((resolve) => setTimeout(resolve, 2000));
    
    // Get PID
    const panePidResult = await Bun.$`tmux list-panes -F '#{pane_id} #{pane_pid}' | grep "^${paneId} "`.text();
    const pid = parseInt(panePidResult.split(" ")[1]?.trim() ?? "0", 10);

    // Set pane title
    const shortTitle = task.title.substring(0, 20) + (task.title.length > 20 ? "..." : "");
    await Bun.$`tmux select-pane -t ${paneId} -T "Task:${shortTitle}"`.quiet();

    log.info(`Created dashboard pane: ${paneId} (pid: ${pid})`);

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
    const runnerScript = join(this.stateDir, `runner_${projectId}_${taskId}.sh`);
    const outputFile = join(this.stateDir, `output_${projectId}_${taskId}.log`);

    for (const file of [promptFile, runnerScript, outputFile]) {
      try {
        if (existsSync(file)) {
          await Bun.$`rm -f ${file}`.quiet();
        }
      } catch (error) {
        log.debug(`Failed to cleanup: ${file}`);
      }
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
```

### Acceptance Criteria

- [ ] `buildPrompt()` generates correct prompts for new and resume tasks
- [ ] `resolveWorkdir()` correctly prioritizes workdir sources
- [ ] Background mode spawns process and redirects output
- [ ] TUI mode creates tmux window with runner script
- [ ] Dashboard mode splits pane in existing window
- [ ] PID detection works for all modes
- [ ] Cleanup removes temporary files

### Test Cases

1. Test prompt building for new task
2. Test prompt building for resume task
3. Test workdir resolution priority
4. Test background spawning (mock Bun.spawn)
5. Test TUI tmux commands generated correctly
6. Test dashboard pane splitting
7. Test cleanup removes files

---

## Task 6: Add task claiming API endpoint

**Priority:** medium  
**Depends on:** None  
**Estimated effort:** 2 hours

### Objective

Add task claiming endpoints to prevent multiple runners from picking up the same task. This is critical for distributed or multi-process runners.

### Files to Create/Modify

- `src/api/tasks.ts` - Add claim/release endpoints
- `src/core/task-service.ts` - Add claim tracking

### Implementation Details

Add to `src/api/tasks.ts`:

```typescript
// In-memory claim tracking (production would use DB)
const claimedTasks = new Map<string, { runnerId: string; claimedAt: Date }>();

// POST /:projectId/:taskId/claim
tasks.post("/:projectId/:taskId/claim", async (c) => {
  const projectId = c.req.param("projectId");
  const taskId = c.req.param("taskId");
  const body = await c.req.json();
  const runnerId = body.runnerId;

  if (!runnerId) {
    return c.json({ error: "Bad Request", message: "runnerId required" }, 400);
  }

  const claimKey = `${projectId}:${taskId}`;
  const existing = claimedTasks.get(claimKey);

  if (existing) {
    // Check if claim is stale (older than 5 minutes)
    const staleMs = 5 * 60 * 1000;
    if (Date.now() - existing.claimedAt.getTime() < staleMs) {
      return c.json({
        error: "Conflict",
        message: "Task already claimed",
        claimedBy: existing.runnerId,
      }, 409);
    }
    // Claim is stale, allow override
  }

  claimedTasks.set(claimKey, { runnerId, claimedAt: new Date() });
  
  return c.json({
    success: true,
    taskId,
    runnerId,
  });
});

// POST /:projectId/:taskId/release
tasks.post("/:projectId/:taskId/release", async (c) => {
  const projectId = c.req.param("projectId");
  const taskId = c.req.param("taskId");

  const claimKey = `${projectId}:${taskId}`;
  claimedTasks.delete(claimKey);

  return c.json({ success: true });
});

// GET /:projectId/:taskId/claim-status
tasks.get("/:projectId/:taskId/claim-status", async (c) => {
  const projectId = c.req.param("projectId");
  const taskId = c.req.param("taskId");

  const claimKey = `${projectId}:${taskId}`;
  const claim = claimedTasks.get(claimKey);

  if (!claim) {
    return c.json({ claimed: false });
  }

  return c.json({
    claimed: true,
    claimedBy: claim.runnerId,
    claimedAt: claim.claimedAt.toISOString(),
  });
});
```

### Acceptance Criteria

- [ ] `POST /claim` creates claim for task
- [ ] `POST /claim` returns 409 if already claimed
- [ ] `POST /claim` allows override of stale claims (>5 min)
- [ ] `POST /release` removes claim
- [ ] `GET /claim-status` returns claim info
- [ ] Claims are per project+task combination

### Test Cases

1. Test claiming unclaimed task succeeds
2. Test claiming already-claimed task fails with 409
3. Test stale claim can be overridden
4. Test releasing claim works
5. Test claim status returns correct info

---

## Task 7: Create main TaskRunner class

**Priority:** high  
**Depends on:** Task 2, Task 3, Task 4, Task 5  
**Estimated effort:** 5 hours

### Objective

Create the main orchestration class that ties together polling, spawning, and monitoring. This is the heart of the runner.

### Files to Create/Modify

- `src/runner/task-runner.ts` - Main orchestration class

### Implementation Details

```typescript
/**
 * Task Runner
 * 
 * Main orchestration class for the brain-runner.
 * Polls for ready tasks, spawns OpenCode, monitors completion.
 */

import type { 
  RunnerConfig, 
  ExecutionMode, 
  RunningTask, 
  TaskResult,
  RunnerEvent,
  EventHandler,
  RunnerStats,
} from "./types";
import type { ResolvedTask } from "../core/types";
import { getRunnerConfig } from "./config";
import { ApiClient, getApiClient } from "./api-client";
import { ProcessManager, getProcessManager, type CompletionStatus } from "./process-manager";
import { StateManager } from "./state-manager";
import { OpencodeExecutor, getOpencodeExecutor } from "./opencode-executor";
import { log } from "./logger";

// =============================================================================
// Task Runner Class
// =============================================================================

export class TaskRunner {
  private config: RunnerConfig;
  private projectId: string;
  private api: ApiClient;
  private processManager: ProcessManager;
  private stateManager: StateManager;
  private executor: OpencodeExecutor;
  
  private mode: ExecutionMode = "background";
  private running = false;
  private startedAt: string | null = null;
  private stats: RunnerStats = { completed: 0, failed: 0, totalRuntime: 0 };
  private eventHandlers: EventHandler[] = [];
  private pollTimer: Timer | null = null;
  private checkTimer: Timer | null = null;

  constructor(
    projectId: string,
    config?: RunnerConfig
  ) {
    this.projectId = projectId;
    this.config = config ?? getRunnerConfig();
    this.api = getApiClient();
    this.processManager = getProcessManager(this.config.taskTimeout);
    this.stateManager = new StateManager(projectId, this.config.stateDir);
    this.executor = getOpencodeExecutor();
  }

  // ========================================
  // Lifecycle
  // ========================================

  async start(mode: ExecutionMode = "background"): Promise<void> {
    if (this.running) {
      throw new Error("Runner already running");
    }

    // Check if another instance is running
    if (this.stateManager.isPidRunning()) {
      const pid = this.stateManager.loadPid();
      throw new Error(`Runner already running for ${this.projectId} (PID: ${pid})`);
    }

    this.mode = mode;
    this.running = true;
    this.startedAt = new Date().toISOString();

    // Save PID
    this.stateManager.savePid(process.pid);

    log.info(`Starting TaskRunner for project: ${this.projectId}`);
    log.info(`  Mode: ${mode}`);
    log.info(`  Max parallel: ${this.config.maxParallel}`);
    log.info(`  Poll interval: ${this.config.pollInterval}s`);

    // Restore any running tasks from previous session
    await this.recoverRunningTasks();

    // Start poll loop
    this.startPollLoop();

    // Start completion check loop
    this.startCheckLoop();

    // Save initial state
    this.saveState();
  }

  async stop(): Promise<void> {
    if (!this.running) return;

    log.info("Stopping TaskRunner...");
    this.running = false;

    // Stop timers
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
    if (this.checkTimer) {
      clearInterval(this.checkTimer);
      this.checkTimer = null;
    }

    // Kill all running processes
    await this.processManager.killAll();

    // Clear state
    this.stateManager.clearPid();
    this.saveState("stopped");

    this.emit({ type: "shutdown", reason: "manual" });
    log.info("TaskRunner stopped");
  }

  // ========================================
  // Event Handling
  // ========================================

  on(handler: EventHandler): void {
    this.eventHandlers.push(handler);
  }

  private emit(event: RunnerEvent): void {
    for (const handler of this.eventHandlers) {
      try {
        handler(event);
      } catch (error) {
        log.error("Event handler error:", error);
      }
    }
  }

  // ========================================
  // Poll Loop
  // ========================================

  private startPollLoop(): void {
    // Initial poll
    this.poll().catch((error) => log.error("Initial poll failed:", error));

    // Schedule recurring polls
    this.pollTimer = setInterval(
      () => this.poll().catch((error) => log.error("Poll failed:", error)),
      this.config.pollInterval * 1000
    );
  }

  private async poll(): Promise<void> {
    if (!this.running) return;

    const runningCount = this.processManager.runningCount();
    const availableSlots = this.config.maxParallel - runningCount;

    if (availableSlots <= 0) {
      log.debug(`No available slots (${runningCount}/${this.config.maxParallel})`);
      return;
    }

    // Check API availability
    const apiAvailable = await this.api.isAvailable();
    if (!apiAvailable) {
      log.warn("Brain API not available, skipping poll");
      return;
    }

    // Get ready tasks
    let readyTasks: ResolvedTask[];
    try {
      readyTasks = await this.api.getReadyTasks(this.projectId);
    } catch (error) {
      log.error("Failed to fetch ready tasks:", error);
      return;
    }

    // Filter out already running tasks
    const runningIds = new Set(
      this.processManager.getAllRunning().map((t) => t.id)
    );
    const availableTasks = readyTasks.filter((t) => !runningIds.has(t.id));

    if (availableTasks.length === 0) {
      log.debug("No new ready tasks");
      this.emit({ type: "poll_complete", readyCount: 0, runningCount });
      return;
    }

    // Spawn tasks up to available slots
    const toSpawn = availableTasks.slice(0, availableSlots);
    log.info(`Spawning ${toSpawn.length} task(s) (ready: ${availableTasks.length}, slots: ${availableSlots})`);

    for (const task of toSpawn) {
      await this.spawnTask(task, false);
    }

    this.emit({
      type: "poll_complete",
      readyCount: availableTasks.length - toSpawn.length,
      runningCount: this.processManager.runningCount(),
    });

    this.saveState();
  }

  // ========================================
  // Task Spawning
  // ========================================

  private async spawnTask(task: ResolvedTask, isResume: boolean): Promise<void> {
    try {
      const result = await this.executor.spawn(task, this.projectId, {
        mode: this.mode,
        isResume,
      });

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
        isResume,
        workdir: this.executor.resolveWorkdir(task),
      };

      this.processManager.add(task.id, runningTask, result.proc);
      this.emit({ type: "task_started", task: runningTask });

      log.info(`Task started: ${task.title} (id: ${task.id}, pid: ${result.pid})`);
    } catch (error) {
      log.error(`Failed to spawn task ${task.id}:`, error);
    }
  }

  // ========================================
  // Completion Check Loop
  // ========================================

  private startCheckLoop(): void {
    this.checkTimer = setInterval(
      () => this.checkCompletions().catch((error) => log.error("Check failed:", error)),
      this.config.taskPollInterval * 1000
    );
  }

  private async checkCompletions(): Promise<void> {
    if (!this.running) return;

    const processes = this.processManager.getAll();
    if (processes.size === 0) return;

    for (const [taskId, info] of processes) {
      const status = await this.processManager.checkCompletion(
        taskId,
        async (path) => this.checkTaskFileStatus(path)
      );

      if (status !== "running") {
        await this.handleTaskCompletion(taskId, status);
      }
    }
  }

  private async checkTaskFileStatus(taskPath: string): Promise<string | null> {
    // Use API to get current status
    try {
      const tasks = await this.api.getAllTasks(this.projectId);
      const task = tasks.find((t) => t.path === taskPath);
      return task?.status ?? null;
    } catch {
      return null;
    }
  }

  private async handleTaskCompletion(
    taskId: string,
    status: CompletionStatus
  ): Promise<void> {
    const info = this.processManager.get(taskId);
    if (!info) return;

    const { task } = info;
    const startTime = new Date(task.startedAt).getTime();
    const duration = Date.now() - startTime;

    const result: TaskResult = {
      taskId,
      status: status as TaskResult["status"],
      startedAt: task.startedAt,
      completedAt: new Date().toISOString(),
      duration,
    };

    // Update stats
    if (status === "completed") {
      this.stats.completed++;
      this.emit({ type: "task_completed", result });
      log.info(`Task completed: ${task.title} (${duration}ms)`);
    } else {
      this.stats.failed++;
      this.emit({ type: "task_failed", result });
      log.warn(`Task ${status}: ${task.title}`);
    }

    this.stats.totalRuntime += duration;

    // Cleanup
    await this.processManager.kill(taskId);
    this.processManager.remove(taskId);
    await this.executor.cleanup(taskId, this.projectId);

    this.saveState();
  }

  // ========================================
  // State Management
  // ========================================

  private saveState(status?: string): void {
    const runnerStatus = status ?? (this.processManager.runningCount() > 0 ? "processing" : "idle");
    
    this.stateManager.save(
      runnerStatus as any,
      this.processManager.toJSON(),
      this.stats,
      this.startedAt ?? undefined
    );

    this.emit({ type: "state_saved", path: this.config.stateDir });
  }

  private async recoverRunningTasks(): Promise<void> {
    const savedTasks = this.stateManager.loadRunningTasks();
    
    if (savedTasks.length === 0) {
      log.info("No tasks to recover");
      return;
    }

    log.info(`Recovering ${savedTasks.length} task(s) from previous session`);
    this.processManager.restoreFromState(savedTasks);
  }

  // ========================================
  // Getters
  // ========================================

  get isRunning(): boolean {
    return this.running;
  }

  get status(): string {
    if (!this.running) return "stopped";
    return this.processManager.runningCount() > 0 ? "processing" : "idle";
  }

  getStats(): RunnerStats {
    return { ...this.stats };
  }

  getRunningTasks(): RunningTask[] {
    return this.processManager.getAllRunning();
  }
}

// =============================================================================
// Factory Function
// =============================================================================

export function createTaskRunner(projectId: string): TaskRunner {
  return new TaskRunner(projectId);
}
```

### Acceptance Criteria

- [ ] `start()` initializes runner and begins polling
- [ ] `stop()` gracefully shuts down all processes
- [ ] Poll loop respects `maxParallel` limit
- [ ] Already-running tasks are not re-spawned
- [ ] Completion detection triggers result handling
- [ ] Stats are updated on completion
- [ ] State is saved after each change
- [ ] Recovery restores tasks from previous session
- [ ] Events are emitted for all significant actions

### Test Cases

1. Test start/stop lifecycle
2. Test poll loop spawns tasks
3. Test maxParallel limit is respected
4. Test running tasks are filtered from ready list
5. Test completion detection triggers cleanup
6. Test stats are updated correctly
7. Test state persistence
8. Test recovery from saved state
9. Integration test with mock API

---

## Task 8: Create runner CLI

**Priority:** high  
**Depends on:** Task 7  
**Estimated effort:** 3 hours

### Objective

Create the CLI entry point with commands for start, stop, status, and run-one.

### Files to Create/Modify

- `src/runner/index.ts` - CLI entry point
- `package.json` - Add CLI script

### Implementation Details

```typescript
#!/usr/bin/env bun
/**
 * Brain Runner CLI
 * 
 * Task execution daemon for the brain.
 */

import { parseArgs } from "util";
import { TaskRunner, createTaskRunner } from "./task-runner";
import { StateManager } from "./state-manager";
import { ApiClient, getApiClient } from "./api-client";
import { getRunnerConfig } from "./config";
import { log, setLogLevel } from "./logger";
import type { ExecutionMode } from "./types";

// =============================================================================
// CLI Parsing
// =============================================================================

function parseCliArgs() {
  const { values, positionals } = parseArgs({
    args: process.argv.slice(2),
    options: {
      foreground: { type: "boolean", short: "f", default: true },
      background: { type: "boolean", short: "b", default: false },
      tui: { type: "boolean", default: false },
      dashboard: { type: "boolean", default: false },
      "max-parallel": { type: "string", short: "p" },
      "poll-interval": { type: "string" },
      workdir: { type: "string", short: "w" },
      agent: { type: "string" },
      model: { type: "string", short: "m" },
      "dry-run": { type: "boolean", default: false },
      exclude: { type: "string", short: "e", multiple: true },
      "no-resume": { type: "boolean", default: false },
      json: { type: "boolean", default: false },
      follow: { type: "boolean", short: "f", default: false },
      help: { type: "boolean", short: "h", default: false },
      verbose: { type: "boolean", short: "v", default: false },
    },
    allowPositionals: true,
  });

  return { options: values, args: positionals };
}

// =============================================================================
// Commands
// =============================================================================

async function cmdStart(projectId: string, options: Record<string, any>): Promise<void> {
  const config = getRunnerConfig();
  
  // Determine execution mode
  let mode: ExecutionMode = "background";
  if (options.tui) mode = "tui";
  else if (options.dashboard) mode = "dashboard";
  
  // Check if already running
  const stateManager = new StateManager(projectId);
  if (stateManager.isPidRunning()) {
    const pid = stateManager.loadPid();
    console.error(`Runner already running for ${projectId} (PID: ${pid})`);
    console.error(`Use 'brain-runner stop ${projectId}' first.`);
    process.exit(1);
  }

  console.log(`Starting brain-runner for project: ${projectId}`);
  console.log(`  Mode: ${mode}`);
  console.log(`  Max parallel: ${config.maxParallel}`);
  console.log(`  Poll interval: ${config.pollInterval}s`);
  console.log();

  const runner = createTaskRunner(projectId);

  // Set up event handlers
  runner.on((event) => {
    switch (event.type) {
      case "task_started":
        console.log(`[STARTED] ${event.task.title}`);
        break;
      case "task_completed":
        console.log(`[COMPLETED] Task ${event.result.taskId} (${event.result.duration}ms)`);
        break;
      case "task_failed":
        console.log(`[FAILED] Task ${event.result.taskId}: ${event.result.status}`);
        break;
    }
  });

  // Set up signal handlers
  process.on("SIGTERM", async () => {
    console.log("\nReceived SIGTERM, shutting down...");
    await runner.stop();
    process.exit(0);
  });

  process.on("SIGINT", async () => {
    console.log("\nReceived SIGINT, shutting down...");
    await runner.stop();
    process.exit(0);
  });

  await runner.start(mode);

  // Keep process running
  if (options.foreground !== false) {
    await new Promise(() => {}); // Never resolves
  }
}

async function cmdStop(projectId?: string): Promise<void> {
  const config = getRunnerConfig();
  
  if (!projectId) {
    // Stop all runners
    console.log("Stopping all runners...");
    // Implementation would iterate through state files
    return;
  }

  const stateManager = new StateManager(projectId);
  const pid = stateManager.loadPid();

  if (!pid) {
    console.log(`No runner found for ${projectId}`);
    return;
  }

  if (!stateManager.isPidRunning()) {
    console.log(`Runner for ${projectId} is not running (stale PID file)`);
    stateManager.clearPid();
    return;
  }

  console.log(`Stopping runner for ${projectId} (PID: ${pid})...`);
  process.kill(pid, "SIGTERM");

  // Wait for shutdown
  let attempts = 0;
  while (stateManager.isPidRunning() && attempts < 10) {
    await new Promise((r) => setTimeout(r, 1000));
    attempts++;
  }

  if (stateManager.isPidRunning()) {
    console.log("Force killing...");
    process.kill(pid, "SIGKILL");
  }

  stateManager.clearPid();
  console.log(`Runner stopped for ${projectId}`);
}

async function cmdStatus(projectId?: string): Promise<void> {
  console.log("\nBrain Runner Status");
  console.log("=".repeat(50));
  
  if (projectId) {
    await showProjectStatus(projectId);
  } else {
    // Show all projects
    // Implementation would iterate through state files
  }
}

async function showProjectStatus(projectId: string): Promise<void> {
  const stateManager = new StateManager(projectId);
  const state = stateManager.load();
  const isRunning = stateManager.isPidRunning();

  console.log(`\nProject: ${projectId}`);
  console.log(`  Status: ${isRunning ? "RUNNING" : "STOPPED"}`);
  
  if (state) {
    console.log(`  Started: ${state.startedAt}`);
    console.log(`  Running tasks: ${state.runningTasks.length}`);
    console.log(`  Completed: ${state.stats.completed}`);
    console.log(`  Failed: ${state.stats.failed}`);
  }

  // Show API status
  const api = getApiClient();
  const healthy = await api.isAvailable();
  console.log(`  API: ${healthy ? "connected" : "unavailable"}`);
}

async function cmdRunOne(projectId: string, options: Record<string, any>): Promise<void> {
  const api = getApiClient();
  
  // Get next ready task
  const task = await api.getNextTask(projectId);
  
  if (!task) {
    console.log("No ready tasks available");
    
    // Show why
    const waiting = await api.getWaitingTasks(projectId);
    const blocked = await api.getBlockedTasks(projectId);
    
    if (waiting.length > 0) {
      console.log(`  ${waiting.length} task(s) waiting on dependencies`);
    }
    if (blocked.length > 0) {
      console.log(`  ${blocked.length} task(s) blocked`);
    }
    
    process.exit(1);
  }

  console.log(`Running task: ${task.title}`);
  console.log(`  Priority: ${task.priority}`);
  console.log(`  Path: ${task.path}`);

  if (options["dry-run"]) {
    console.log("\n[DRY-RUN] Would execute this task");
    return;
  }

  // Determine mode
  let mode: ExecutionMode = "background";
  if (options.tui) mode = "tui";

  const runner = createTaskRunner(projectId);
  
  // Start with single task
  await runner.start(mode);
  
  // The runner will pick up and execute the task
  // Wait for completion...
}

async function cmdList(projectId: string, options: Record<string, any>): Promise<void> {
  const api = getApiClient();
  const tasks = await api.getAllTasks(projectId);

  if (options.json) {
    console.log(JSON.stringify(tasks, null, 2));
    return;
  }

  console.log(`\nTasks for project: ${projectId}`);
  console.log("=".repeat(50));

  const grouped = {
    ready: tasks.filter((t) => t.classification === "ready"),
    waiting: tasks.filter((t) => t.classification === "waiting" || t.classification === "waiting_on_parent"),
    blocked: tasks.filter((t) => t.classification === "blocked" || t.classification === "blocked_by_parent"),
    in_progress: tasks.filter((t) => t.status === "in_progress"),
    completed: tasks.filter((t) => t.status === "completed"),
  };

  for (const [status, list] of Object.entries(grouped)) {
    if (list.length === 0) continue;
    
    console.log(`\n${status.toUpperCase()} (${list.length}):`);
    for (const task of list) {
      const badge = task.priority === "high" ? "!" : task.priority === "low" ? "-" : "*";
      console.log(`  ${badge} ${task.title} (${task.id})`);
    }
  }
}

// =============================================================================
// Help
// =============================================================================

function showHelp(): void {
  console.log(`
brain-runner - Task execution daemon for brain

USAGE:
    brain-runner <command> [options] [project-id]

COMMANDS:
    start <project-id>    Start processing tasks
    stop [project-id]     Stop runner(s)
    status [project-id]   Show status
    run-one <project-id>  Execute single task
    list <project-id>     List all tasks
    ready <project-id>    List ready tasks
    waiting <project-id>  List waiting tasks
    blocked <project-id>  List blocked tasks
    help                  Show this help

OPTIONS:
    -f, --foreground      Run in foreground (default)
    -b, --background      Run as daemon
    --tui                 Use interactive OpenCode TUI
    --dashboard           Create tmux dashboard
    -p, --max-parallel N  Max concurrent tasks
    --poll-interval N     Seconds between polls
    -w, --workdir PATH    Working directory
    --agent NAME          OpenCode agent
    -m, --model NAME      Model to use
    --dry-run             Don't execute, just log
    -e, --exclude PATTERN Exclude project pattern
    --no-resume           Skip interrupted tasks
    --json                Output as JSON
    -v, --verbose         Verbose logging

EXAMPLES:
    brain-runner start test
    brain-runner start all --max-parallel 5
    brain-runner run-one test --tui
    brain-runner status
    brain-runner stop test
`);
}

// =============================================================================
// Main
// =============================================================================

async function main(): Promise<void> {
  const { options, args } = parseCliArgs();

  if (options.help || args.length === 0) {
    showHelp();
    process.exit(0);
  }

  if (options.verbose) {
    setLogLevel("debug");
  }

  const command = args[0];
  const projectId = args[1] ?? getRunnerConfig().brainDir.split("/").pop() ?? "default";

  try {
    switch (command) {
      case "start":
        await cmdStart(projectId, options);
        break;
      case "stop":
        await cmdStop(args[1]);
        break;
      case "status":
        await cmdStatus(args[1]);
        break;
      case "run-one":
      case "runone":
      case "one":
        await cmdRunOne(projectId, options);
        break;
      case "list":
        await cmdList(projectId, options);
        break;
      case "ready":
        const ready = await getApiClient().getReadyTasks(projectId);
        console.log(JSON.stringify(ready, null, 2));
        break;
      case "waiting":
        const waiting = await getApiClient().getWaitingTasks(projectId);
        console.log(JSON.stringify(waiting, null, 2));
        break;
      case "blocked":
        const blocked = await getApiClient().getBlockedTasks(projectId);
        console.log(JSON.stringify(blocked, null, 2));
        break;
      case "help":
        showHelp();
        break;
      default:
        console.error(`Unknown command: ${command}`);
        showHelp();
        process.exit(1);
    }
  } catch (error) {
    console.error("Error:", error instanceof Error ? error.message : error);
    process.exit(1);
  }
}

main();
```

Update `package.json`:

```json
{
  "scripts": {
    "runner": "bun run src/runner/index.ts",
    "runner:start": "bun run src/runner/index.ts start",
    "runner:status": "bun run src/runner/index.ts status"
  },
  "bin": {
    "brain-runner": "./src/runner/index.ts"
  }
}
```

### Acceptance Criteria

- [ ] `start` command starts runner with correct mode
- [ ] `stop` command sends SIGTERM and waits
- [ ] `status` shows current state and API connectivity
- [ ] `run-one` executes single task
- [ ] `list` shows tasks grouped by status
- [ ] All commands handle errors gracefully
- [ ] Help text is comprehensive
- [ ] Can be run as `bun run runner` or directly

### Test Cases

1. Test CLI argument parsing
2. Test start creates runner
3. Test stop sends signals correctly
4. Test status shows correct info
5. Test run-one exits if no tasks
6. Test list shows all task groups

---

## Task 9: Add signal handling

**Priority:** high  
**Depends on:** Task 7, Task 8  
**Estimated effort:** 2 hours

### Objective

Implement proper signal handling for graceful shutdown, saving state before exit.

### Files to Create/Modify

- `src/runner/signals.ts` - Signal handling utilities

### Implementation Details

```typescript
/**
 * Signal Handling
 * 
 * Handles SIGTERM, SIGINT, SIGHUP for graceful shutdown.
 */

import { log } from "./logger";

type ShutdownHandler = () => Promise<void>;

let shutdownHandlers: ShutdownHandler[] = [];
let isShuttingDown = false;

export function registerShutdownHandler(handler: ShutdownHandler): void {
  shutdownHandlers.push(handler);
}

export function setupSignalHandlers(): void {
  const signals: NodeJS.Signals[] = ["SIGTERM", "SIGINT", "SIGHUP"];

  for (const signal of signals) {
    process.on(signal, async () => {
      if (isShuttingDown) {
        log.warn(`Received ${signal} during shutdown, forcing exit`);
        process.exit(1);
      }

      isShuttingDown = true;
      log.info(`Received ${signal}, initiating graceful shutdown...`);

      try {
        // Run all shutdown handlers in parallel
        await Promise.all(
          shutdownHandlers.map(async (handler) => {
            try {
              await handler();
            } catch (error) {
              log.error("Shutdown handler error:", error);
            }
          })
        );

        log.info("Shutdown complete");
        process.exit(0);
      } catch (error) {
        log.error("Shutdown error:", error);
        process.exit(1);
      }
    });
  }

  // Handle uncaught errors
  process.on("uncaughtException", (error) => {
    log.error("Uncaught exception:", error);
    process.exit(1);
  });

  process.on("unhandledRejection", (reason) => {
    log.error("Unhandled rejection:", reason);
    process.exit(1);
  });
}

export function isShutdownInProgress(): boolean {
  return isShuttingDown;
}
```

### Acceptance Criteria

- [ ] SIGTERM triggers graceful shutdown
- [ ] SIGINT triggers graceful shutdown
- [ ] SIGHUP triggers graceful shutdown
- [ ] Second signal during shutdown forces exit
- [ ] All registered handlers are called
- [ ] Uncaught exceptions are logged

### Test Cases

1. Test handler registration
2. Test SIGTERM triggers handlers
3. Test double signal forces exit
4. Test uncaught exception handling

---

## Task 10: Create integration tests

**Priority:** medium  
**Depends on:** Task 7  
**Estimated effort:** 4 hours

### Objective

Write integration tests for the runner components, mocking OpenCode for fast tests.

### Files to Create/Modify

- `tests/runner/api-client.test.ts`
- `tests/runner/process-manager.test.ts`
- `tests/runner/state-manager.test.ts`
- `tests/runner/task-runner.test.ts`

### Implementation Details

```typescript
// tests/runner/task-runner.test.ts
import { describe, it, expect, beforeEach, afterEach, mock } from "bun:test";
import { TaskRunner } from "../../src/runner/task-runner";
import { StateManager } from "../../src/runner/state-manager";

describe("TaskRunner", () => {
  let runner: TaskRunner;
  const testProject = "test-runner";

  beforeEach(() => {
    runner = new TaskRunner(testProject, {
      brainApiUrl: "http://localhost:3000",
      pollInterval: 1,
      taskPollInterval: 1,
      maxParallel: 2,
      stateDir: "/tmp/brain-runner-test",
      logDir: "/tmp/brain-runner-test/log",
      workDir: "/tmp",
      apiTimeout: 1000,
      taskTimeout: 10000,
      opencode: {
        bin: "echo", // Mock opencode with echo
        agent: "test",
        model: "test-model",
      },
      excludeProjects: [],
    });
  });

  afterEach(async () => {
    if (runner.isRunning) {
      await runner.stop();
    }
  });

  it("should start and stop", async () => {
    await runner.start("background");
    expect(runner.isRunning).toBe(true);
    
    await runner.stop();
    expect(runner.isRunning).toBe(false);
  });

  it("should respect maxParallel limit", async () => {
    // Mock API to return multiple ready tasks
    // Verify only maxParallel are spawned
  });

  it("should emit events on task lifecycle", async () => {
    const events: any[] = [];
    runner.on((event) => events.push(event));
    
    await runner.start("background");
    
    // Trigger task completion
    // Verify events emitted
  });

  it("should save state periodically", async () => {
    await runner.start("background");
    
    const stateManager = new StateManager(testProject, "/tmp/brain-runner-test");
    const state = stateManager.load();
    
    expect(state).not.toBeNull();
    expect(state?.projectId).toBe(testProject);
  });
});
```

### Acceptance Criteria

- [ ] API client tests with mock server
- [ ] Process manager tests with mock spawns
- [ ] State manager tests for persistence
- [ ] Task runner integration tests
- [ ] All tests pass with `bun test`
- [ ] Tests don't require actual OpenCode

### Test Cases

See individual test files for specific test cases.

---

## Task 11: Create tmux manager

**Priority:** low  
**Depends on:** Task 5  
**Estimated effort:** 3 hours

### Objective

Create a tmux manager for the optional dashboard mode with task list, running tasks, and logs panes.

### Files to Create/Modify

- `src/runner/tmux-manager.ts` - tmux dashboard management

### Implementation Details

```typescript
/**
 * Tmux Manager
 * 
 * Creates and manages the tmux dashboard for visual task monitoring.
 */

import { existsSync, writeFileSync } from "fs";
import { join } from "path";
import type { RunningTask } from "./types";
import { getRunnerConfig } from "./config";
import { log } from "./logger";

// =============================================================================
// Tmux Manager Class
// =============================================================================

export class TmuxManager {
  private projectId: string;
  private windowName: string;
  private stateDir: string;
  private tasksPaneId: string | null = null;
  private logsPaneId: string | null = null;
  private mainPaneId: string | null = null;

  constructor(projectId: string, stateDir?: string) {
    this.projectId = projectId;
    this.windowName = `brain-runner_${projectId}`;
    this.stateDir = stateDir ?? getRunnerConfig().stateDir;
  }

  // ========================================
  // Dashboard Lifecycle
  // ========================================

  async create(workdir: string): Promise<void> {
    if (await this.exists()) {
      log.warn(`Dashboard window already exists: ${this.windowName}`);
      return;
    }

    log.info(`Creating dashboard window: ${this.windowName}`);

    // Create task list script
    const taskListScript = await this.createTaskListScript();
    
    // Create logs script
    const logsScript = await this.createLogsScript();

    // Create window with task list pane
    await Bun.$`tmux new-window -d -n ${this.windowName} -c ${workdir} ${taskListScript}`;
    await this.sleep(300);

    // Split for main area (75% right)
    const mainPane = await Bun.$`tmux split-window -t "${this.windowName}.0" -h -d -P -F '#{pane_id}' -l 75% "bash"`.text();
    this.mainPaneId = mainPane.trim();
    await this.sleep(200);

    // Split main area for logs (20% bottom)
    await Bun.$`tmux split-window -t ${this.mainPaneId} -v -d -l 20% ${logsScript}`;

    // Get pane IDs
    const panes = await this.listPanes();
    this.tasksPaneId = panes[0];
    this.logsPaneId = panes[2];

    // Set pane titles
    await Bun.$`tmux select-pane -t ${this.tasksPaneId} -T "Tasks"`.quiet();
    await Bun.$`tmux select-pane -t ${this.mainPaneId} -T "Tasks"`.quiet();
    await Bun.$`tmux select-pane -t ${this.logsPaneId} -T "Logs"`.quiet();

    log.info(`Dashboard created: ${this.windowName}`);
  }

  async destroy(): Promise<void> {
    if (!(await this.exists())) return;

    log.info(`Destroying dashboard window: ${this.windowName}`);
    await Bun.$`tmux kill-window -t ${this.windowName}`.quiet();
  }

  async exists(): Promise<boolean> {
    try {
      const result = await Bun.$`tmux list-windows -F '#{window_name}'`.text();
      return result.split("\n").includes(this.windowName);
    } catch {
      return false;
    }
  }

  // ========================================
  // Pane Management
  // ========================================

  async createTaskPane(taskId: string, title: string, script: string): Promise<string | null> {
    if (!this.mainPaneId) return null;

    // Find existing task panes
    const taskPanes = await this.findTaskPanes();

    let paneId: string;
    if (taskPanes.length === 0) {
      // First task - split main pane
      paneId = await Bun.$`tmux split-window -t ${this.mainPaneId} -v -d -P -F '#{pane_id}' -b -l 80% ${script}`.text();
    } else {
      // Additional task - split last task pane horizontally
      const lastPane = taskPanes[taskPanes.length - 1];
      paneId = await Bun.$`tmux split-window -t ${lastPane} -h -d -P -F '#{pane_id}' ${script}`.text();
    }

    paneId = paneId.trim();

    // Set pane title
    const shortTitle = title.substring(0, 20) + (title.length > 20 ? "..." : "");
    await Bun.$`tmux select-pane -t ${paneId} -T "Task:${shortTitle}"`.quiet();

    return paneId;
  }

  async closePane(paneId: string): Promise<void> {
    try {
      await Bun.$`tmux kill-pane -t ${paneId}`.quiet();
    } catch {
      // Pane already closed
    }
  }

  // ========================================
  // Helpers
  // ========================================

  private async listPanes(): Promise<string[]> {
    const result = await Bun.$`tmux list-panes -t ${this.windowName} -F '#{pane_id}'`.text();
    return result.trim().split("\n");
  }

  private async findTaskPanes(): Promise<string[]> {
    const result = await Bun.$`tmux list-panes -t ${this.windowName} -F '#{pane_id} #{pane_title}'`.text();
    return result
      .trim()
      .split("\n")
      .filter((line) => line.includes("Task:"))
      .map((line) => line.split(" ")[0]);
  }

  private async createTaskListScript(): Promise<string> {
    const scriptPath = join(this.stateDir, `task_list_${this.projectId}.sh`);
    const script = `#!/bin/bash
PROJECT_ID="${this.projectId}"
BRAIN_API="${getRunnerConfig().brainApiUrl}"

show_tasks() {
  clear
  echo -e "\\033[1m Task List: \\$PROJECT_ID\\033[0m"
  echo "═══════════════════════════════════════"
  
  # Fetch ready tasks from API
  ready=$(curl -s "\$BRAIN_API/api/v1/tasks/\$PROJECT_ID/ready" 2>/dev/null | jq -r '.tasks[] | "  ✓ \\(.title)"' 2>/dev/null)
  if [[ -n "\\$ready" ]]; then
    echo -e "\\033[32mReady:\\033[0m"
    echo "\\$ready"
  fi
  
  # Fetch waiting tasks
  waiting=$(curl -s "\$BRAIN_API/api/v1/tasks/\$PROJECT_ID/waiting" 2>/dev/null | jq -r '.tasks[] | "  ⏳ \\(.title)"' 2>/dev/null)
  if [[ -n "\\$waiting" ]]; then
    echo -e "\\033[33mWaiting:\\033[0m"
    echo "\\$waiting"
  fi
  
  echo ""
  echo -e "\\033[2mRefreshes every 10s\\033[0m"
}

while true; do
  show_tasks
  sleep 10
done
`;
    writeFileSync(scriptPath, script, { mode: 0o755 });
    return scriptPath;
  }

  private async createLogsScript(): Promise<string> {
    const scriptPath = join(this.stateDir, `logs_watch_${this.projectId}.sh`);
    const logFile = join(getRunnerConfig().logDir, "brain-runner.log");
    const script = `#!/bin/bash
LOG_FILE="${logFile}"
echo -e "\\033[1m Logs\\033[0m"
echo "───────────────────────────────────────"
tail -f "\\$LOG_FILE" 2>/dev/null | while read line; do
  if [[ "\\$line" == *"[ERROR]"* ]]; then
    echo -e "\\033[31m\\$line\\033[0m"
  elif [[ "\\$line" == *"[WARN]"* ]]; then
    echo -e "\\033[33m\\$line\\033[0m"
  else
    echo "\\$line"
  fi
done
`;
    writeFileSync(scriptPath, script, { mode: 0o755 });
    return scriptPath;
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}
```

### Acceptance Criteria

- [ ] `create()` builds dashboard with correct layout
- [ ] Task panes are created dynamically
- [ ] Panes close when tasks complete
- [ ] `destroy()` cleans up window
- [ ] Task list refreshes periodically
- [ ] Logs stream with color highlighting

### Test Cases

1. Test dashboard creation
2. Test pane creation
3. Test pane closing
4. Test dashboard destruction
5. Test exists check

---

## Task 12: Add dashboard mode to runner

**Priority:** low  
**Depends on:** Task 7, Task 11  
**Estimated effort:** 2 hours

### Objective

Integrate the tmux manager with the task runner for the `--dashboard` flag.

### Files to Create/Modify

- `src/runner/task-runner.ts` - Add dashboard integration
- `src/runner/index.ts` - Add `--dashboard` handling

### Implementation Details

Update `TaskRunner.start()` to initialize dashboard:

```typescript
async start(mode: ExecutionMode = "background"): Promise<void> {
  // ... existing code ...

  // Create dashboard if in dashboard mode
  if (mode === "dashboard" || mode === "tui") {
    this.tmuxManager = new TmuxManager(this.projectId, this.config.stateDir);
    await this.tmuxManager.create(this.config.workDir);
    
    log.info(`Dashboard created: tmux select-window -t ${this.projectId}`);
  }

  // ... rest of existing code ...
}
```

Update spawning to use dashboard panes:

```typescript
private async spawnTask(task: ResolvedTask, isResume: boolean): Promise<void> {
  const options: SpawnOptions = {
    mode: this.mode,
    isResume,
  };

  // If in dashboard mode, create pane via TmuxManager
  if (this.mode === "dashboard" && this.tmuxManager) {
    const runnerScript = await this.executor.buildRunnerScript(task, this.projectId);
    const paneId = await this.tmuxManager.createTaskPane(task.id, task.title, runnerScript);
    options.paneId = paneId ?? undefined;
  }

  // ... rest of spawning logic ...
}
```

### Acceptance Criteria

- [ ] `--dashboard` creates tmux window
- [ ] Tasks spawn in dashboard panes
- [ ] Panes close on task completion
- [ ] Dashboard destroyed on runner stop
- [ ] Works with `--tui` flag

### Test Cases

1. Test dashboard mode initialization
2. Test task pane creation in dashboard
3. Test pane cleanup
4. Test dashboard cleanup on stop

---

## Migration Guide

### From Bash to TypeScript Runner

#### 1. Install Brain Runner

```bash
# In the brain project
bun install

# Or install globally
bun link
```

#### 2. Configuration Changes

| Bash Variable | TypeScript Variable | Notes |
|---------------|---------------------|-------|
| `DO_WORK_POLL_INTERVAL` | `RUNNER_POLL_INTERVAL` | Same units (seconds) |
| `DO_WORK_MAX_PARALLEL` | `RUNNER_MAX_PARALLEL` | Same default (3) |
| `DO_WORK_BRAIN_DIR` | Not needed | Uses API |
| `BRAIN_API_URL` | `BRAIN_API_URL` | Same |
| `OPENCODE_BIN` | `OPENCODE_BIN` | Same |
| `DO_WORK_MODEL` | `OPENCODE_MODEL` | Same |

#### 3. Command Equivalents

| Bash Command | TypeScript Command |
|--------------|-------------------|
| `do-work start test` | `brain-runner start test` |
| `do-work start test --tui` | `brain-runner start test --tui` |
| `do-work start all` | `brain-runner start all` |
| `do-work run-one test` | `brain-runner run-one test` |
| `do-work stop` | `brain-runner stop` |
| `do-work status` | `brain-runner status` |
| `do-work ready test` | `brain-runner ready test` |
| `do-work list test` | `brain-runner list test` |

#### 4. State File Location

- Bash: `~/.local/state/do-work-deps/`
- TypeScript: `~/.local/state/brain-runner/`

Files are not compatible - clear old state before switching.

#### 5. Known Differences

1. **Interrupted Task Recovery:** TypeScript only recovers tasks with live PIDs
2. **tmux Integration:** Slightly different pane layout
3. **Logging:** Structured JSON logs in TypeScript
4. **Error Messages:** More detailed in TypeScript

---

## Summary

This plan provides 12 tasks to fully port the do-work bash script to TypeScript:

| Phase | Tasks | Effort |
|-------|-------|--------|
| **Phase 1: Core Infrastructure** | 4 tasks | ~10 hours |
| **Phase 2: Task Execution** | 3 tasks | ~12 hours |
| **Phase 3: CLI & Integration** | 3 tasks | ~9 hours |
| **Phase 4: TUI Dashboard** | 2 tasks | ~5 hours |

**Total Estimated Effort:** ~36 hours

### Recommended Order

1. Task 1 (types/config) - Foundation
2. Task 2 (API client) - Server communication
3. Task 3 (process manager) - Process tracking
4. Task 4 (state manager) - Persistence
5. Task 5 (OpenCode executor) - Task execution
6. Task 6 (claim API) - Coordination
7. Task 7 (TaskRunner) - Main orchestration
8. Task 8 (CLI) - User interface
9. Task 9 (signals) - Graceful shutdown
10. Task 10 (tests) - Quality assurance
11. Task 11 (tmux manager) - Optional dashboard
12. Task 12 (dashboard integration) - Optional dashboard

Tasks 11-12 are optional and can be deferred if dashboard mode is not needed immediately.
