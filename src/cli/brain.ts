#!/usr/bin/env bun
/**
 * Brain API Server CLI
 *
 * A CLI tool to manage the Brain API server.
 *
 * Usage:
 *   brain start     - Start the API server (background)
 *   brain stop      - Stop the API server
 *   brain restart   - Restart the API server
 *   brain status    - Check if server is running
 *   brain health    - Show health check
 *   brain logs      - Show recent logs
 *   brain logs -f   - Follow logs
 *   brain dev       - Start in development mode
 *   brain config    - Show configuration
 *   brain doctor    - Diagnose brain configuration
 */

import { spawn, spawnSync } from "child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync, unlinkSync } from "fs";
import { homedir } from "os";
import { join, dirname } from "path";
import { createDoctorService, type Check, type DoctorResult } from "../core/doctor";

// =============================================================================
// Configuration
// =============================================================================

const HOME = homedir();
const BRAIN_API_DIR = process.env.BRAIN_API_DIR || join(HOME, "projects/brain-api");
const DEFAULT_PORT = "3333";
const PORT = process.env.BRAIN_PORT || process.env.PORT || DEFAULT_PORT;

// XDG Base Directory Specification
const XDG_STATE_HOME = process.env.XDG_STATE_HOME || join(HOME, ".local/state");
const XDG_CACHE_HOME = process.env.XDG_CACHE_HOME || join(HOME, ".cache");

const PID_FILE = join(XDG_STATE_HOME, "brain/brain.pid");
const LOG_FILE = join(XDG_STATE_HOME, "brain/brain.log");

// Ensure directories exist
function ensureDirs() {
  const dirs = [dirname(PID_FILE), dirname(LOG_FILE)];
  for (const dir of dirs) {
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true });
    }
  }
}

// =============================================================================
// Helpers
// =============================================================================

function getPid(): number | null {
  if (existsSync(PID_FILE)) {
    try {
      const pid = parseInt(readFileSync(PID_FILE, "utf-8").trim(), 10);
      if (!isNaN(pid)) return pid;
    } catch {
      // ignore
    }
  }
  return null;
}

function isProcessRunning(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function findProcessByPort(port: string): number | null {
  const result = spawnSync("lsof", ["-ti", `:${port}`], { encoding: "utf-8" });
  if (result.status === 0 && result.stdout.trim()) {
    const pid = parseInt(result.stdout.trim().split("\n")[0], 10);
    if (!isNaN(pid)) return pid;
  }
  return null;
}

async function healthCheck(port: string): Promise<boolean> {
  try {
    const response = await fetch(`http://localhost:${port}/health`);
    if (response.ok) {
      const data = await response.json();
      console.log(JSON.stringify(data, null, 2));
      return true;
    }
  } catch {
    // ignore
  }
  return false;
}

function printHelp() {
  console.log(`brain - Brain API Server CLI

Usage: brain <command> [options]

Commands:
  start       Start the API server (background)
  stop        Stop the API server
  restart     Restart the API server
  status      Check if server is running
  health      Show health check
  logs        Show recent logs
  logs -f     Follow logs
  dev         Start in development mode (foreground with hot reload)
  config      Show current configuration
  doctor      Diagnose and fix brain configuration

Doctor Options:
  brain doctor              Run diagnostics (show failures only)
  brain doctor -v           Verbose output (show all checks)
  brain doctor --fix        Fix fixable issues
  brain doctor --fix --force  Reset modified files to reference
  brain doctor --fix --dry-run  Show what would be fixed

Environment:
  BRAIN_PORT      Server port (default: ${DEFAULT_PORT})
  BRAIN_DIR       Brain data directory (default: ~/.brain)

Examples:
  brain start
  brain status
  brain doctor --fix
  BRAIN_PORT=4444 brain start
`);
}

// =============================================================================
// Commands
// =============================================================================

async function cmdStart() {
  ensureDirs();

  // Check if already running by PID
  const existingPid = getPid();
  if (existingPid && isProcessRunning(existingPid)) {
    console.log(`Brain API already running (PID: ${existingPid})`);
    return;
  }

  // Check if port is in use
  const portPid = findProcessByPort(PORT);
  if (portPid) {
    console.log(`Port ${PORT} already in use (PID: ${portPid})`);
    return;
  }

  console.log(`Starting Brain API server on port ${PORT}...`);

  // Start the server
  const logFile = Bun.file(LOG_FILE);
  const logWriter = logFile.writer();

  const proc = spawn("bun", ["run", "src/index.ts"], {
    cwd: BRAIN_API_DIR,
    env: { ...process.env, PORT },
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });

  // Write output to log file
  proc.stdout?.on("data", (data) => logWriter.write(data));
  proc.stderr?.on("data", (data) => logWriter.write(data));

  proc.unref();

  // Save PID
  writeFileSync(PID_FILE, proc.pid!.toString());

  // Wait and verify
  await new Promise((resolve) => setTimeout(resolve, 2000));

  if (isProcessRunning(proc.pid!)) {
    console.log(`Brain API started (PID: ${proc.pid})`);
    await healthCheck(PORT);
  } else {
    console.error("Failed to start Brain API");
    process.exit(1);
  }
}

function cmdStop() {
  let stopped = false;

  // Try PID file first
  const pid = getPid();
  if (pid && isProcessRunning(pid)) {
    console.log(`Stopping Brain API (PID: ${pid})...`);
    process.kill(pid, "SIGTERM");
    stopped = true;
  }

  // Clean up PID file
  if (existsSync(PID_FILE)) {
    unlinkSync(PID_FILE);
  }

  // Also check by port
  if (!stopped) {
    const portPid = findProcessByPort(PORT);
    if (portPid) {
      console.log(`Stopping Brain API on port ${PORT} (PID: ${portPid})...`);
      process.kill(portPid, "SIGTERM");
      stopped = true;
    }
  }

  if (stopped) {
    console.log("Stopped");
  } else {
    console.log("Brain API not running");
  }
}

async function cmdRestart() {
  cmdStop();
  await new Promise((resolve) => setTimeout(resolve, 1000));
  await cmdStart();
}

async function cmdStatus() {
  const pid = getPid();
  if (pid && isProcessRunning(pid)) {
    console.log(`Brain API running (PID: ${pid}) on port ${PORT}`);
    const healthy = await healthCheck(PORT);
    if (!healthy) {
      console.log("Health check failed");
    }
    return;
  }

  // Check by port
  const portPid = findProcessByPort(PORT);
  if (portPid) {
    console.log(`Brain API running on port ${PORT} (PID: ${portPid})`);
    await healthCheck(PORT);
    return;
  }

  console.log("Brain API not running");
}

async function cmdHealth() {
  const healthy = await healthCheck(PORT);
  if (!healthy) {
    console.error("Health check failed - server may not be running");
    process.exit(1);
  }
}

function cmdLogs(follow: boolean) {
  if (!existsSync(LOG_FILE)) {
    console.log("No logs found");
    return;
  }

  if (follow) {
    const proc = spawn("tail", ["-f", LOG_FILE], { stdio: "inherit" });
    proc.on("exit", () => process.exit(0));
  } else {
    const proc = spawnSync("tail", ["-100", LOG_FILE], { stdio: "inherit" });
    process.exit(proc.status || 0);
  }
}

function cmdDev() {
  console.log(`Starting Brain API in development mode on port ${PORT}...`);
  const proc = spawn("bun", ["run", "--watch", "src/index.ts"], {
    cwd: BRAIN_API_DIR,
    env: { ...process.env, PORT },
    stdio: "inherit",
  });

  proc.on("exit", (code) => process.exit(code || 0));
}

function cmdConfig() {
  const brainDir = process.env.BRAIN_DIR || join(HOME, ".brain");
  console.log(`BRAIN_PORT=${PORT}`);
  console.log(`BRAIN_DIR=${brainDir}`);
  console.log(`PID_FILE=${PID_FILE}`);
  console.log(`LOG_FILE=${LOG_FILE}`);
}

// =============================================================================
// Doctor Command
// =============================================================================

// ANSI color codes
const COLORS = {
  reset: "\x1b[0m",
  green: "\x1b[32m",
  red: "\x1b[31m",
  yellow: "\x1b[33m",
  gray: "\x1b[90m",
  bold: "\x1b[1m",
};

function getStatusIcon(status: Check["status"]): string {
  switch (status) {
    case "pass":
      return `${COLORS.green}\u2713${COLORS.reset}`;
    case "fail":
      return `${COLORS.red}\u2717${COLORS.reset}`;
    case "warn":
      return `${COLORS.yellow}\u26A0${COLORS.reset}`;
    case "skip":
      return `${COLORS.gray}-${COLORS.reset}`;
  }
}

function formatCheck(check: Check, verbose: boolean): string | null {
  // In non-verbose mode, skip passed and skipped checks
  if (!verbose && (check.status === "pass" || check.status === "skip")) {
    return null;
  }

  const icon = getStatusIcon(check.status);
  let line = `  ${icon} ${check.message}`;

  if (check.details && (verbose || check.status === "fail" || check.status === "warn")) {
    line += `\n      ${COLORS.gray}${check.details}${COLORS.reset}`;
  }

  if (check.fixable && check.status === "fail") {
    line += `\n      ${COLORS.gray}(fixable with --fix)${COLORS.reset}`;
  }

  return line;
}

function printResult(result: DoctorResult, verbose: boolean): void {
  console.log(`\n${COLORS.bold}Brain Doctor${COLORS.reset}`);
  console.log(`${COLORS.gray}BRAIN_DIR: ${result.brainDir}${COLORS.reset}\n`);

  // Group checks by category
  const categories: Record<string, Check[]> = {
    "ZK CLI": [],
    "ZK Configuration": [],
    "Templates": [],
    "Database": [],
    "Permissions": [],
  };

  for (const check of result.checks) {
    if (check.name.startsWith("zk-cli")) {
      categories["ZK CLI"].push(check);
    } else if (check.name.startsWith("zk-")) {
      categories["ZK Configuration"].push(check);
    } else if (check.name.startsWith("template-")) {
      categories["Templates"].push(check);
    } else if (check.name.startsWith("database-")) {
      categories["Database"].push(check);
    } else {
      categories["Permissions"].push(check);
    }
  }

  for (const [category, checks] of Object.entries(categories)) {
    const hasOutput = checks.some(
      (c) => verbose || c.status === "fail" || c.status === "warn"
    );
    if (!hasOutput && !verbose) continue;

    console.log(`${COLORS.bold}${category}${COLORS.reset}`);
    for (const check of checks) {
      const formatted = formatCheck(check, verbose);
      if (formatted) {
        console.log(formatted);
      }
    }
    console.log();
  }

  // Print summary
  const { passed, failed, warnings, skipped } = result.summary;
  console.log(`${COLORS.bold}Summary${COLORS.reset}`);
  console.log(
    `  ${COLORS.green}${passed} passed${COLORS.reset}, ` +
      `${COLORS.red}${failed} failed${COLORS.reset}, ` +
      `${COLORS.yellow}${warnings} warnings${COLORS.reset}, ` +
      `${COLORS.gray}${skipped} skipped${COLORS.reset}`
  );

  if (result.healthy) {
    console.log(`\n${COLORS.green}${COLORS.bold}Brain is healthy!${COLORS.reset}`);
  } else {
    console.log(`\n${COLORS.red}${COLORS.bold}Brain has issues that need attention.${COLORS.reset}`);
    const fixableCount = result.checks.filter((c) => c.status === "fail" && c.fixable).length;
    if (fixableCount > 0) {
      console.log(`${COLORS.gray}Run 'brain doctor --fix' to fix ${fixableCount} issue(s).${COLORS.reset}`);
    }
  }
}

async function cmdDoctor(args: string[]): Promise<void> {
  // Parse flags
  const fix = args.includes("--fix");
  const force = args.includes("--force");
  const dryRun = args.includes("--dry-run");
  const verbose = args.includes("-v") || args.includes("--verbose");

  // Get BRAIN_DIR from config
  // Import dynamically to avoid circular dependency issues
  const { getConfig } = await import("../config");
  const config = getConfig();
  const brainDir = config.brain.brainDir;

  // Create doctor service
  const doctor = createDoctorService(brainDir, { fix, force, dryRun, verbose });

  // Run diagnosis or fix
  let result: DoctorResult;
  if (fix) {
    if (dryRun) {
      console.log(`${COLORS.yellow}Dry run mode - no changes will be made${COLORS.reset}\n`);
    }
    result = await doctor.fix();
  } else {
    result = await doctor.diagnose();
  }

  // Print results
  printResult(result, verbose);

  // Exit with appropriate code
  process.exit(result.healthy ? 0 : 1);
}

// =============================================================================
// Main
// =============================================================================

async function main() {
  const args = process.argv.slice(2);
  const command = args[0];

  switch (command) {
    case "start":
      await cmdStart();
      break;
    case "stop":
      cmdStop();
      break;
    case "restart":
      await cmdRestart();
      break;
    case "status":
      await cmdStatus();
      break;
    case "health":
      await cmdHealth();
      break;
    case "logs":
      cmdLogs(args[1] === "-f");
      break;
    case "dev":
      cmdDev();
      break;
    case "config":
      cmdConfig();
      break;
    case "doctor":
      await cmdDoctor(args.slice(1));
      break;
    case "help":
    case "--help":
    case "-h":
      printHelp();
      break;
    default:
      if (command) {
        console.error(`Unknown command: ${command}`);
      }
      printHelp();
      process.exit(command ? 1 : 0);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
