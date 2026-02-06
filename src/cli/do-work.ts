#!/usr/bin/env bun
/**
 * do-work - Task Runner CLI
 *
 * A simplified wrapper around brain-runner for quick task management.
 * This CLI directly imports the runner module - no external dependencies needed.
 *
 * Usage:
 *   do-work start <project>     - Start runner with TUI for a project
 *   do-work start all           - Start runner for all projects
 *   do-work start-bg <project>  - Start runner in background
 *   do-work stop [project]      - Stop runner
 *   do-work status [project]    - Show runner status
 *   do-work list                - List all projects with tasks
 *   do-work list <project>      - List all tasks for a project
 *   do-work ready <project>     - Show ready tasks
 *   do-work waiting <project>   - Show waiting tasks
 *   do-work blocked <project>   - Show blocked tasks
 *   do-work run-one <project>   - Execute single task
 *   do-work logs [-f]           - Show runner logs
 *   do-work config              - Show configuration
 */

// Import runner's main function directly - no subprocess needed
import { main as runnerMain } from "../runner/index";

const DEFAULT_API_URL = "http://localhost:3333";
const BRAIN_API_URL = process.env.BRAIN_API_URL || DEFAULT_API_URL;

// =============================================================================
// API Client
// =============================================================================

async function apiGet<T>(path: string): Promise<T> {
  const url = `${BRAIN_API_URL}/api/v1${path}`;
  const response = await fetch(url);
  if (!response.ok) {
    const error = await response.text();
    throw new Error(`API error (${response.status}): ${error}`);
  }
  return response.json() as Promise<T>;
}

// =============================================================================
// Helpers
// =============================================================================

function printHelp() {
  console.log(`do-work - Brain Task Runner CLI

Usage: do-work <command> [project] [options]

Commands:
  start <project>     Start runner with TUI for a project
  start all           Start runner for all projects
  start-bg <project>  Start runner in background
  stop [project]      Stop runner
  status [project]    Show runner status
  list                List all projects with tasks
  list <project>      List all tasks for a project
  ready <project>     Show ready tasks
  waiting <project>   Show waiting tasks
  blocked <project>   Show blocked tasks
  run-one <project>   Execute single task
  logs [-f]           Show runner logs
  config              Show current configuration

Options (for start command):
  -i, --include PATTERN   Include project pattern (repeatable)
  -e, --exclude PATTERN   Exclude project pattern (repeatable)
  -p, --max-parallel N    Max concurrent tasks (default: 3)
  --poll-interval N       Seconds between polls (default: 30)
  -w, --workdir DIR       Working directory
  --agent NAME            OpenCode agent to use
  -m, --model NAME        Model to use
  --dry-run               Log actions without executing
  --no-resume             Skip interrupted tasks
  --run                   Start processing immediately (TUI starts paused)
  -v, --verbose           Enable verbose logging

Environment:
  BRAIN_API_URL   API server URL (default: ${DEFAULT_API_URL})

Examples:
  do-work start myproject
  do-work start all
  do-work start all --include 'prod-*' --exclude 'test-*'
  do-work start myproject --max-parallel 5
  do-work ready myproject
  BRAIN_API_URL=http://server:3333 do-work start myproject
`);
}

/**
 * Run the runner with the given arguments by manipulating process.argv
 * and calling the runner's main function directly.
 */
async function runRunner(args: string[]): Promise<number> {
  // Save original argv and replace with our args
  const originalArgv = process.argv;
  process.argv = ["bun", "brain-runner", ...args];

  try {
    const exitCode = await runnerMain();
    return exitCode;
  } finally {
    // Restore original argv
    process.argv = originalArgv;
  }
}

// =============================================================================
// Commands
// =============================================================================

async function cmdStart(
  project: string | undefined,
  background: boolean,
  extraArgs: string[] = []
): Promise<number> {
  const resolvedProject = project || "all";

  const args = ["start", resolvedProject];
  if (background) {
    args.push("--background");
  } else {
    // Only add --tui if not already in extraArgs
    if (!extraArgs.includes("--tui") && !extraArgs.includes("--dashboard")) {
      args.push("--tui");
    }
  }

  // Forward additional flags
  args.push(...extraArgs);

  return runRunner(args);
}

async function cmdStop(project?: string): Promise<number> {
  const args = ["stop"];
  if (project) args.push(project);
  return runRunner(args);
}

async function cmdStatus(project?: string): Promise<number> {
  const args = ["status"];
  if (project) args.push(project);
  return runRunner(args);
}

async function cmdList(project: string | undefined): Promise<number> {
  if (!project) {
    // List all projects
    try {
      const result = await apiGet<{ projects: string[]; count: number }>("/tasks");
      if (result.count === 0) {
        console.log("No projects with tasks found.");
        return 0;
      }
      console.log("Projects with tasks:");
      for (const p of result.projects) {
        console.log(`  ${p}`);
      }
      console.log(`\nTotal: ${result.count} project(s)`);
      return 0;
    } catch (error) {
      console.error(`Error: ${error instanceof Error ? error.message : error}`);
      return 1;
    }
  }
  return runRunner(["list", project]);
}

async function cmdReady(project: string | undefined): Promise<number> {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work ready <project>");
    return 1;
  }
  return runRunner(["ready", project]);
}

async function cmdWaiting(project: string | undefined): Promise<number> {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work waiting <project>");
    return 1;
  }
  return runRunner(["waiting", project]);
}

async function cmdBlocked(project: string | undefined): Promise<number> {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work blocked <project>");
    return 1;
  }
  return runRunner(["blocked", project]);
}

async function cmdRunOne(project: string | undefined): Promise<number> {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work run-one <project>");
    return 1;
  }
  return runRunner(["run-one", project]);
}

async function cmdLogs(follow: boolean): Promise<number> {
  const args = ["logs"];
  if (follow) args.push("-f");
  return runRunner(args);
}

function cmdConfig() {
  console.log(`BRAIN_API_URL=${BRAIN_API_URL}`);
}

// =============================================================================
// Argument Parsing Helpers
// =============================================================================

/**
 * Check if an argument is a flag (starts with -)
 */
function isFlag(arg: string | undefined): boolean {
  return arg !== undefined && arg.startsWith("-");
}

/**
 * Parse command line arguments into command, project, and extra args.
 * Handles cases where project may be omitted and flags come directly after command.
 */
function parseCliArgs(args: string[]): {
  command: string | undefined;
  project: string | undefined;
  extraArgs: string[];
} {
  const command = args[0];
  let project: string | undefined;
  let extraArgsStart = 1;

  // Check if second arg is a project name or a flag
  const arg1 = args[1];
  if (arg1 && !isFlag(arg1)) {
    project = arg1;
    extraArgsStart = 2;
  }

  const extraArgs = args.slice(extraArgsStart);

  return { command, project, extraArgs };
}

// =============================================================================
// Main
// =============================================================================

async function main() {
  const args = process.argv.slice(2);
  const { command, project, extraArgs } = parseCliArgs(args);

  let exitCode = 0;

  switch (command) {
    case "start":
      exitCode = await cmdStart(project, false, extraArgs);
      break;
    case "start-bg":
    case "background":
      exitCode = await cmdStart(project, true, extraArgs);
      break;
    case "stop":
      exitCode = await cmdStop(project);
      break;
    case "status":
      exitCode = await cmdStatus(project);
      break;
    case "list":
      exitCode = await cmdList(project);
      break;
    case "ready":
      exitCode = await cmdReady(project);
      break;
    case "waiting":
      exitCode = await cmdWaiting(project);
      break;
    case "blocked":
      exitCode = await cmdBlocked(project);
      break;
    case "run-one":
      exitCode = await cmdRunOne(project);
      break;
    case "logs":
      exitCode = await cmdLogs(extraArgs.includes("-f") || project === "-f");
      break;
    case "config":
      cmdConfig();
      break;
    case "help":
    case "--help":
    case "-h":
      printHelp();
      break;
    default:
      // If first arg looks like a project name (not a flag), start TUI for it
      if (command && !command.startsWith("-")) {
        exitCode = await cmdStart(command, false, extraArgs);
      } else if (!command) {
        // No command given: default to `start all`
        exitCode = await cmdStart(undefined, false, extraArgs);
      } else {
        printHelp();
        exitCode = 1;
      }
  }

  process.exit(exitCode);
}

main();
