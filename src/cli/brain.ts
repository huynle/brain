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
 */

import { spawn, spawnSync } from "child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync, unlinkSync } from "fs";
import { homedir } from "os";
import { join, dirname } from "path";

// =============================================================================
// Configuration
// =============================================================================

const HOME = homedir();
const BRAIN_API_DIR = process.env.BRAIN_API_DIR || join(HOME, "projects/brain-api");
const DEFAULT_PORT = "3333";
const PORT = process.env.BRAIN_PORT || process.env.PORT || DEFAULT_PORT;
const PID_FILE = join(HOME, ".local/run/brain-api.pid");
const LOG_FILE = join(HOME, ".local/log/brain-api.log");

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

Environment:
  BRAIN_PORT      Server port (default: ${DEFAULT_PORT})
  BRAIN_API_DIR   Brain API directory (default: ~/projects/brain-api)

Examples:
  brain start
  brain status
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
  console.log(`BRAIN_PORT=${PORT}`);
  console.log(`BRAIN_API_DIR=${BRAIN_API_DIR}`);
  console.log(`PID_FILE=${PID_FILE}`);
  console.log(`LOG_FILE=${LOG_FILE}`);
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
