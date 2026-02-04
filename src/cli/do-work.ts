#!/usr/bin/env bun
/**
 * do-work - Task Runner CLI
 *
 * A CLI tool to manage brain tasks and run the task runner.
 *
 * Usage:
 *   do-work start <project>     - Start runner with TUI for a project
 *   do-work start all           - Start runner for all projects
 *   do-work start-bg <project>  - Start runner in background
 *   do-work stop [project]      - Stop runner
 *   do-work status [project]    - Show runner status
 *   do-work list <project>      - List all tasks
 *   do-work ready <project>     - Show ready tasks
 *   do-work waiting <project>   - Show waiting tasks
 *   do-work blocked <project>   - Show blocked tasks
 *   do-work tree <project>      - Show task dependency tree
 *   do-work run-one <project>   - Execute single task
 *   do-work logs [-f]           - Show runner logs
 *   do-work config              - Show configuration
 */

import { spawn, spawnSync } from "child_process";
import { existsSync } from "fs";
import { homedir } from "os";
import { join } from "path";

// =============================================================================
// Configuration
// =============================================================================

const HOME = homedir();
const DEFAULT_API_URL = "http://localhost:3333";
const BRAIN_API_URL = process.env.BRAIN_API_URL || DEFAULT_API_URL;
const BRAIN_API_DIR = process.env.BRAIN_API_DIR || join(HOME, "projects/brain-api");
const RUNNER_SCRIPT = join(BRAIN_API_DIR, "src/runner/index.ts");

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
  list <project>      List all tasks
  ready <project>     Show ready tasks
  waiting <project>   Show waiting tasks
  blocked <project>   Show blocked tasks
  tree <project>      Show task dependency tree
  run-one <project>   Execute single task
  logs [-f]           Show runner logs
  config              Show current configuration

Environment:
  BRAIN_API_URL   API server URL (default: ${DEFAULT_API_URL})
  BRAIN_API_DIR   Brain API directory (default: ~/projects/brain-api)

Examples:
  do-work start myproject
  do-work start all
  do-work ready myproject
  BRAIN_API_URL=http://server:3333 do-work start myproject
`);
}

function runRunner(args: string[]): never {
  if (!existsSync(RUNNER_SCRIPT)) {
    console.error(`Runner script not found: ${RUNNER_SCRIPT}`);
    console.error("Make sure BRAIN_API_DIR is set correctly.");
    process.exit(1);
  }

  const proc = spawnSync("bun", ["run", RUNNER_SCRIPT, ...args], {
    cwd: BRAIN_API_DIR,
    env: { ...process.env, BRAIN_API_URL },
    stdio: "inherit",
  });

  process.exit(proc.status || 0);
}

function runRunnerAsync(args: string[]): void {
  if (!existsSync(RUNNER_SCRIPT)) {
    console.error(`Runner script not found: ${RUNNER_SCRIPT}`);
    console.error("Make sure BRAIN_API_DIR is set correctly.");
    process.exit(1);
  }

  const proc = spawn("bun", ["run", RUNNER_SCRIPT, ...args], {
    cwd: BRAIN_API_DIR,
    env: { ...process.env, BRAIN_API_URL },
    stdio: "inherit",
  });

  proc.on("exit", (code) => process.exit(code || 0));
}

// =============================================================================
// Commands
// =============================================================================

function cmdStart(project: string | undefined, background: boolean) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work start <project>");
    process.exit(1);
  }

  const args = ["start", project];
  if (background) {
    args.push("--background");
  } else {
    args.push("--tui");
  }

  runRunnerAsync(args);
}

function cmdStop(project?: string) {
  const args = ["stop"];
  if (project) args.push(project);
  runRunner(args);
}

function cmdStatus(project?: string) {
  const args = ["status"];
  if (project) args.push(project);
  runRunner(args);
}

function cmdList(project: string | undefined) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work list <project>");
    process.exit(1);
  }
  runRunner(["list", project]);
}

function cmdReady(project: string | undefined) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work ready <project>");
    process.exit(1);
  }
  runRunner(["ready", project]);
}

function cmdWaiting(project: string | undefined) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work waiting <project>");
    process.exit(1);
  }
  runRunner(["waiting", project]);
}

function cmdBlocked(project: string | undefined) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work blocked <project>");
    process.exit(1);
  }
  runRunner(["blocked", project]);
}

function cmdTree(project: string | undefined) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work tree <project>");
    process.exit(1);
  }
  runRunner(["tree", project]);
}

function cmdRunOne(project: string | undefined) {
  if (!project) {
    console.error("Error: project argument required");
    console.error("Usage: do-work run-one <project>");
    process.exit(1);
  }
  runRunner(["run-one", project]);
}

function cmdLogs(follow: boolean) {
  const args = ["logs"];
  if (follow) args.push("-f");
  runRunner(args);
}

function cmdConfig() {
  console.log(`BRAIN_API_URL=${BRAIN_API_URL}`);
  console.log(`BRAIN_API_DIR=${BRAIN_API_DIR}`);
  console.log(`RUNNER_SCRIPT=${RUNNER_SCRIPT}`);
}

// =============================================================================
// Main
// =============================================================================

function main() {
  const args = process.argv.slice(2);
  const command = args[0];
  const arg1 = args[1];
  const arg2 = args[2];

  switch (command) {
    case "start":
      cmdStart(arg1, false);
      break;
    case "start-bg":
    case "background":
      cmdStart(arg1, true);
      break;
    case "stop":
      cmdStop(arg1);
      break;
    case "status":
      cmdStatus(arg1);
      break;
    case "list":
      cmdList(arg1);
      break;
    case "ready":
      cmdReady(arg1);
      break;
    case "waiting":
      cmdWaiting(arg1);
      break;
    case "blocked":
      cmdBlocked(arg1);
      break;
    case "tree":
    case "graph":
      cmdTree(arg1);
      break;
    case "run-one":
      cmdRunOne(arg1);
      break;
    case "logs":
      cmdLogs(arg1 === "-f");
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
        cmdStart(command, false);
      } else {
        printHelp();
        process.exit(command ? 1 : 0);
      }
  }
}

main();
