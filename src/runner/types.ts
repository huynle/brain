/**
 * Brain Runner Types
 *
 * Type definitions for the brain-runner module that processes tasks
 * from the Brain API using OpenCode.
 */

import type { Priority } from "../core/types";

// =============================================================================
// Configuration Types
// =============================================================================

export interface RunnerConfig {
  brainApiUrl: string;
  pollInterval: number; // seconds
  taskPollInterval: number; // seconds
  maxParallel: number;
  stateDir: string;
  logDir: string;
  workDir: string;
  apiTimeout: number; // ms
  taskTimeout: number; // ms
  idleDetectionThreshold: number; // ms, time before idle task becomes blocked (default 60000)

  opencode: OpencodeConfig;

  excludeProjects: string[]; // glob patterns
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
  startedAt: string; // ISO timestamp
  isResume: boolean;
  workdir: string;
  opencodePort?: number;   // OpenCode HTTP API port (discovered via lsof)
  idleSince?: string;      // ISO timestamp when idle was first detected
}

export interface TaskResult {
  taskId: string;
  status: "completed" | "failed" | "blocked" | "cancelled" | "timeout" | "crashed";
  startedAt: string;
  completedAt: string;
  duration: number; // ms
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
  totalRuntime: number; // ms
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
  | { type: "task_cancelled"; taskId: string; taskPath: string }
  | { type: "poll_complete"; readyCount: number; runningCount: number }
  | { type: "state_saved"; path: string }
  | { type: "shutdown"; reason: string }
  | { type: "project_paused"; projectId: string }
  | { type: "project_resumed"; projectId: string }
  | { type: "all_paused" }
  | { type: "all_resumed" };

export type EventHandler = (event: RunnerEvent) => void;

// =============================================================================
// Multi-Project Types (Phase 2)
// =============================================================================

export interface MultiProjectConfig {
  projects: string[];           // Resolved project list
  isMultiProject: boolean;      // true when "all" was specified
}

export interface MultiProjectRunnerOptions {
  projects: string[];
  mode: ExecutionMode;
  config: RunnerConfig;
}
