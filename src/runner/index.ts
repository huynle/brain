#!/usr/bin/env bun
/**
 * Brain Runner CLI
 *
 * Command-line interface for the brain-runner.
 * Provides commands for starting, stopping, and monitoring the task runner.
 *
 * Usage:
 *   brain-runner start [projectId] [options]
 *   brain-runner stop [projectId]
 *   brain-runner status [projectId]
 *   brain-runner run-one [projectId]
 *   brain-runner list [projectId]
 *   brain-runner ready [projectId]
 *   brain-runner waiting [projectId]
 *   brain-runner blocked [projectId]
 *   brain-runner logs [-f]
 */

import { spawn } from "child_process";
import { existsSync, readFileSync, watchFile, unwatchFile } from "fs";
import { join } from "path";
import { getRunnerConfig, loadConfig } from "./config";
import { ApiClient, getApiClient } from "./api-client";
import { StateManager } from "./state-manager";
import { getLogger, type LogLevel } from "./logger";
import { TaskRunner, getTaskRunner, resetTaskRunner } from "./task-runner";
import type { ExecutionMode, RunnerConfig, RunnerState } from "./types";
import type { ResolvedTask } from "../core/types";

// =============================================================================
// Types
// =============================================================================

interface ParsedArgs {
  command: string;
  projectId: string;
  options: CLIOptions;
}

interface CLIOptions {
  // Execution mode
  foreground: boolean;
  background: boolean;
  tui: boolean;
  dashboard: boolean;

  // Runner settings
  maxParallel: number;
  pollInterval: number;
  workdir: string;
  agent: string;
  model: string;

  // Behavior
  dryRun: boolean;
  exclude: string[];
  noResume: boolean;
  follow: boolean;

  // Misc
  help: boolean;
  verbose: boolean;
}

// =============================================================================
// Constants
// =============================================================================

const HELP_TEXT = `
Brain Runner CLI - Process tasks from Brain API using OpenCode

Usage:
  brain-runner <command> [projectId] [options]

Commands:
  start [projectId]     Start the runner (default: all projects)
  stop [projectId]      Stop running daemon
  status [projectId]    Show runner status
  run-one [projectId]   Execute single task and exit
  list [projectId]      List all tasks for project
  ready [projectId]     List ready tasks
  waiting [projectId]   List waiting tasks
  blocked [projectId]   List blocked tasks
  logs                  View runner logs

Options:
  -f, --foreground      Run in foreground (default)
  -b, --background      Run as daemon
  --tui                 Use interactive OpenCode TUI
  --dashboard           Create tmux dashboard
  -p, --max-parallel N  Max concurrent tasks (default: 3)
  --poll-interval N     Seconds between polls (default: 30)
  -w, --workdir DIR     Working directory
  --agent NAME          OpenCode agent to use
  -m, --model NAME      Model to use
  --dry-run             Log actions without executing
  -e, --exclude PATTERN Exclude project pattern (repeatable)
  --no-resume           Skip interrupted tasks
  -v, --verbose         Enable verbose logging
  -h, --help            Show this help message

Examples:
  brain-runner start my-project -f
  brain-runner start all --max-parallel 5
  brain-runner run-one my-project --dry-run
  brain-runner ready my-project
  brain-runner logs -f
`;

const DEFAULT_PROJECT_ID = "all";

// =============================================================================
// Argument Parsing
// =============================================================================

function parseArgs(argv: string[]): ParsedArgs {
  const args = argv.slice(2); // Skip 'bun' and script path

  const options: CLIOptions = {
    foreground: true,
    background: false,
    tui: false,
    dashboard: false,
    maxParallel: 0, // 0 means use config default
    pollInterval: 0,
    workdir: "",
    agent: "",
    model: "",
    dryRun: false,
    exclude: [],
    noResume: false,
    follow: false,
    help: false,
    verbose: false,
  };

  let command = "";
  let projectId = DEFAULT_PROJECT_ID;
  let i = 0;

  while (i < args.length) {
    const arg = args[i];

    // Help flag
    if (arg === "-h" || arg === "--help") {
      options.help = true;
      i++;
      continue;
    }

    // Verbose flag
    if (arg === "-v" || arg === "--verbose") {
      options.verbose = true;
      i++;
      continue;
    }

    // Execution mode flags
    if (arg === "-f" || arg === "--foreground") {
      options.foreground = true;
      options.background = false;
      i++;
      continue;
    }

    if (arg === "-b" || arg === "--background") {
      options.background = true;
      options.foreground = false;
      i++;
      continue;
    }

    if (arg === "--tui") {
      options.tui = true;
      i++;
      continue;
    }

    if (arg === "--dashboard") {
      options.dashboard = true;
      i++;
      continue;
    }

    // Runner settings with values
    if (arg === "-p" || arg === "--max-parallel") {
      options.maxParallel = parseInt(args[++i], 10) || 3;
      i++;
      continue;
    }

    if (arg === "--poll-interval") {
      options.pollInterval = parseInt(args[++i], 10) || 30;
      i++;
      continue;
    }

    if (arg === "-w" || arg === "--workdir") {
      options.workdir = args[++i] || "";
      i++;
      continue;
    }

    if (arg === "--agent") {
      options.agent = args[++i] || "";
      i++;
      continue;
    }

    if (arg === "-m" || arg === "--model") {
      options.model = args[++i] || "";
      i++;
      continue;
    }

    // Behavior flags
    if (arg === "--dry-run") {
      options.dryRun = true;
      i++;
      continue;
    }

    if (arg === "-e" || arg === "--exclude") {
      options.exclude.push(args[++i] || "");
      i++;
      continue;
    }

    if (arg === "--no-resume") {
      options.noResume = true;
      i++;
      continue;
    }

    // Logs follow flag
    if (arg === "-f" && command === "logs") {
      options.follow = true;
      i++;
      continue;
    }

    // Positional arguments
    if (!arg.startsWith("-")) {
      if (!command) {
        command = arg;
      } else if (projectId === DEFAULT_PROJECT_ID) {
        projectId = arg;
      }
    }

    i++;
  }

  // Default command
  if (!command) {
    command = "help";
  }

  return { command, projectId, options };
}

// =============================================================================
// Command Handlers
// =============================================================================

async function handleStart(projectId: string, options: CLIOptions): Promise<number> {
  const logger = getLogger();
  const config = getRunnerConfig();

  // Check if already running
  const stateManager = new StateManager(config.stateDir, projectId);
  if (stateManager.isPidRunning()) {
    logger.error("Runner already running", { projectId, pid: stateManager.loadPid() });
    return 1;
  }

  // Determine execution mode
  let mode: ExecutionMode = "background";
  if (options.tui) mode = "tui";
  else if (options.dashboard) mode = "dashboard";
  else if (options.foreground) mode = "background"; // foreground still uses background mode internally

  logger.info("Starting runner", {
    projectId,
    mode,
    maxParallel: options.maxParallel || config.maxParallel,
    pollInterval: options.pollInterval || config.pollInterval,
    dryRun: options.dryRun,
  });

  if (options.dryRun) {
    logger.info("[DRY RUN] Would start runner - no action taken");
    return 0;
  }

  // Build config overrides from CLI options
  const configOverrides: Partial<RunnerConfig> = {};
  if (options.maxParallel > 0) configOverrides.maxParallel = options.maxParallel;
  if (options.pollInterval > 0) configOverrides.pollInterval = options.pollInterval;
  if (options.workdir) configOverrides.workDir = options.workdir;

  // Create and start TaskRunner
  try {
    const runner = getTaskRunner({
      projectId,
      mode,
      config: { ...config, ...configOverrides },
    });

    // Set up graceful shutdown handler
    const shutdown = async () => {
      logger.info("Received shutdown signal");
      await runner.stop();
      process.exit(0);
    };

    process.on("SIGINT", shutdown);
    process.on("SIGTERM", shutdown);

    // Start the runner
    await runner.start();

    // If in foreground mode, keep running
    if (options.foreground || mode === "dashboard") {
      logger.info("Runner started in foreground mode. Press Ctrl+C to stop.");
      // Keep the process alive
      await new Promise(() => {}); // Never resolves - process runs until signal
    }

    return 0;
  } catch (error) {
    logger.error("Failed to start runner", { error: String(error) });
    return 1;
  }
}

async function handleStop(projectId: string, options: CLIOptions): Promise<number> {
  const logger = getLogger();
  const config = getRunnerConfig();

  const stateManager = new StateManager(config.stateDir, projectId);
  const pid = stateManager.loadPid();

  if (!pid) {
    logger.warn("No runner found", { projectId });
    return 1;
  }

  if (!stateManager.isPidRunning()) {
    logger.warn("Runner not running (stale PID file)", { projectId, pid });
    stateManager.clearPid();
    return 1;
  }

  logger.info("Stopping runner", { projectId, pid });

  if (options.dryRun) {
    logger.info("[DRY RUN] Would send SIGTERM - no action taken");
    return 0;
  }

  try {
    process.kill(pid, "SIGTERM");
    logger.info("Sent SIGTERM to runner", { pid });

    // Wait briefly for graceful shutdown
    await new Promise((resolve) => setTimeout(resolve, 2000));

    if (stateManager.isPidRunning()) {
      logger.warn("Runner still running after SIGTERM, sending SIGKILL", { pid });
      process.kill(pid, "SIGKILL");
    }

    stateManager.clearPid();
    return 0;
  } catch (error) {
    logger.error("Failed to stop runner", { pid, error: String(error) });
    return 1;
  }
}

async function handleStatus(projectId: string, _options: CLIOptions): Promise<number> {
  const logger = getLogger();
  const config = getRunnerConfig();

  const stateManager = new StateManager(config.stateDir, projectId);
  const state = stateManager.load();
  const pid = stateManager.loadPid();
  const isRunning = stateManager.isPidRunning();

  if (!state && !pid) {
    console.log(`Runner status: NOT STARTED (project: ${projectId})`);
    return 0;
  }

  console.log(`\nRunner Status: ${projectId}`);
  console.log("─".repeat(40));
  console.log(`  Status:     ${isRunning ? "RUNNING" : "STOPPED"}`);
  console.log(`  PID:        ${pid ?? "N/A"}`);

  if (state) {
    console.log(`  Started:    ${state.startedAt}`);
    console.log(`  Updated:    ${state.updatedAt}`);
    console.log(`  Running:    ${state.runningTasks.length} task(s)`);
    console.log(`  Completed:  ${state.stats.completed}`);
    console.log(`  Failed:     ${state.stats.failed}`);

    if (state.runningTasks.length > 0) {
      console.log("\nRunning Tasks:");
      for (const task of state.runningTasks) {
        console.log(`  - ${task.title} (${task.id})`);
        console.log(`    Priority: ${task.priority}, Started: ${task.startedAt}`);
      }
    }
  }

  console.log("");
  return 0;
}

async function handleRunOne(projectId: string, options: CLIOptions): Promise<number> {
  const logger = getLogger();
  const apiClient = getApiClient();

  logger.info("Running single task", { projectId, dryRun: options.dryRun });

  // Get next task
  const task = await apiClient.getNextTask(projectId);

  if (!task) {
    logger.info("No ready tasks found", { projectId });
    return 0;
  }

  console.log(`\nNext Task: ${task.title}`);
  console.log(`  ID:       ${task.id}`);
  console.log(`  Priority: ${task.priority}`);
  console.log(`  Path:     ${task.path}`);
  console.log("");

  if (options.dryRun) {
    logger.info("[DRY RUN] Would execute task - no action taken");
    return 0;
  }

  // For now, just report the task - actual execution will be in runner core
  logger.warn("Runner core not yet implemented - task found but not executed");
  return 0;
}

async function handleList(projectId: string, _options: CLIOptions): Promise<number> {
  const apiClient = getApiClient();

  try {
    const tasks = await apiClient.getAllTasks(projectId);
    printTaskList("All Tasks", tasks);
    return 0;
  } catch (error) {
    getLogger().error("Failed to list tasks", { projectId, error: String(error) });
    return 1;
  }
}

async function handleReady(projectId: string, _options: CLIOptions): Promise<number> {
  const apiClient = getApiClient();

  try {
    const tasks = await apiClient.getReadyTasks(projectId);
    printTaskList("Ready Tasks", tasks);
    return 0;
  } catch (error) {
    getLogger().error("Failed to list ready tasks", { projectId, error: String(error) });
    return 1;
  }
}

async function handleWaiting(projectId: string, _options: CLIOptions): Promise<number> {
  const apiClient = getApiClient();

  try {
    const tasks = await apiClient.getWaitingTasks(projectId);
    printTaskList("Waiting Tasks", tasks);
    return 0;
  } catch (error) {
    getLogger().error("Failed to list waiting tasks", { projectId, error: String(error) });
    return 1;
  }
}

async function handleBlocked(projectId: string, _options: CLIOptions): Promise<number> {
  const apiClient = getApiClient();

  try {
    const tasks = await apiClient.getBlockedTasks(projectId);
    printTaskList("Blocked Tasks", tasks);
    return 0;
  } catch (error) {
    getLogger().error("Failed to list blocked tasks", { projectId, error: String(error) });
    return 1;
  }
}

async function handleLogs(options: CLIOptions): Promise<number> {
  const logger = getLogger();
  const logFile = logger.getLogFilePath();

  if (!logFile || !existsSync(logFile)) {
    console.log("No log file found");
    return 1;
  }

  if (options.follow) {
    // Follow mode - watch for changes
    console.log(`Following ${logFile} (Ctrl+C to stop)\n`);

    // Print existing content
    const content = readFileSync(logFile, "utf-8");
    process.stdout.write(content);

    // Watch for changes
    let lastSize = content.length;

    watchFile(logFile, { interval: 500 }, () => {
      try {
        const newContent = readFileSync(logFile, "utf-8");
        if (newContent.length > lastSize) {
          process.stdout.write(newContent.slice(lastSize));
          lastSize = newContent.length;
        }
      } catch {
        // File may have been rotated
      }
    });

    // Wait forever until Ctrl+C
    await new Promise(() => {});
  } else {
    // Print log file content
    const content = readFileSync(logFile, "utf-8");
    console.log(content);
  }

  return 0;
}

// =============================================================================
// Helpers
// =============================================================================

function printTaskList(title: string, tasks: ResolvedTask[]): void {
  console.log(`\n${title} (${tasks.length})`);
  console.log("─".repeat(60));

  if (tasks.length === 0) {
    console.log("  No tasks found");
    console.log("");
    return;
  }

  for (const task of tasks) {
    const deps = task.resolved_deps?.length ?? 0;
    const depStr = deps > 0 ? ` [${deps} deps]` : "";

    console.log(`  [${task.priority.padEnd(6)}] ${task.title}${depStr}`);
    console.log(`           ID: ${task.id} | Status: ${task.status}`);
  }

  console.log("");
}

// =============================================================================
// Main Entry Point
// =============================================================================

async function main(): Promise<number> {
  const { command, projectId, options } = parseArgs(process.argv);

  // Set log level based on verbose flag
  if (options.verbose) {
    getLogger().setLevel("debug");
  }

  // Handle help
  if (options.help || command === "help") {
    console.log(HELP_TEXT);
    return 0;
  }

  // Route to command handler
  switch (command) {
    case "start":
      return handleStart(projectId, options);

    case "stop":
      return handleStop(projectId, options);

    case "status":
      return handleStatus(projectId, options);

    case "run-one":
      return handleRunOne(projectId, options);

    case "list":
      return handleList(projectId, options);

    case "ready":
      return handleReady(projectId, options);

    case "waiting":
      return handleWaiting(projectId, options);

    case "blocked":
      return handleBlocked(projectId, options);

    case "logs":
      return handleLogs(options);

    default:
      console.error(`Unknown command: ${command}`);
      console.log("Run 'brain-runner --help' for usage information");
      return 1;
  }
}

// Run if this is the main module
if (import.meta.main) {
  main()
    .then((code) => process.exit(code))
    .catch((error) => {
      console.error("Fatal error:", error);
      process.exit(1);
    });
}

// Export for testing
export { parseArgs, main };
export type { ParsedArgs, CLIOptions };
