/**
 * Brain Runner Configuration
 *
 * Loads configuration from environment variables and optional config file.
 * Supports YAML (preferred) and JSON (backward compat).
 *
 * File priority: config.yaml > config.yml > config.json
 * Environment variables override file settings.
 * CLI flags override both.
 */

import { homedir } from "os";
import { join } from "path";
import { existsSync, readFileSync, mkdirSync, writeFileSync } from "fs";
import { parse as parseYaml } from "yaml";
import type { RunnerConfig, OpencodeConfig } from "./types";

// =============================================================================
// Constants
// =============================================================================

const CONFIG_DIR = join(homedir(), ".config", "brain-runner");
const CONFIG_FILES = [
  join(CONFIG_DIR, "config.yaml"),
  join(CONFIG_DIR, "config.yml"),
  join(CONFIG_DIR, "config.json"),
];
const DEFAULT_CONFIG_PATH = CONFIG_FILES[0]; // config.yaml

// =============================================================================
// Default YAML Config Template
// =============================================================================

const DEFAULT_CONFIG_YAML = `# =============================================================================
# Brain Runner Configuration
# =============================================================================
# File: ~/.config/brain-runner/config.yaml
#
# All values shown are defaults. Uncomment and modify as needed.
# Environment variables override file settings. CLI flags override both.
#
# Precedence (highest to lowest):
#   1. CLI flags (--max-parallel, --poll-interval, etc.)
#   2. Environment variables (BRAIN_API_URL, RUNNER_MAX_PARALLEL, etc.)
#   3. This config file
#   4. Built-in defaults
# =============================================================================

# -----------------------------------------------------------------------------
# API Connection
# -----------------------------------------------------------------------------

# Brain API server URL
# Env: BRAIN_API_URL
# brain_api_url: "http://localhost:3333"

# HTTP request timeout for API calls (milliseconds)
# Env: RUNNER_API_TIMEOUT
# api_timeout: 5000

# -----------------------------------------------------------------------------
# Polling
# -----------------------------------------------------------------------------

# Seconds between project discovery polls
# Env: RUNNER_POLL_INTERVAL | CLI: --poll-interval N
# Must be >= 1
# poll_interval: 30

# Seconds between task status polls within a project
# Env: RUNNER_TASK_POLL_INTERVAL
# Must be >= 1
# task_poll_interval: 5

# -----------------------------------------------------------------------------
# Parallelism & Resource Limits
# -----------------------------------------------------------------------------

# Maximum concurrent task executions (1-100)
# Env: RUNNER_MAX_PARALLEL | CLI: -p N, --max-parallel N
# max_parallel: 2

# Hard limit on total child processes (must be >= max_parallel, 1-100)
# Env: RUNNER_MAX_TOTAL_PROCESSES
# max_total_processes: 10

# Pause spawning if available memory falls below this percentage (0 = disable)
# Env: RUNNER_MEMORY_THRESHOLD
# Must be 0-100
# memory_threshold_percent: 10

# -----------------------------------------------------------------------------
# Timeouts & Idle Detection
# -----------------------------------------------------------------------------

# Per-task execution timeout in milliseconds (0 = no timeout)
# Env: RUNNER_TASK_TIMEOUT
# task_timeout: 0

# Time in milliseconds before an idle task is considered blocked
# Env: RUNNER_IDLE_THRESHOLD
# idle_detection_threshold: 60000

# -----------------------------------------------------------------------------
# Directories
# -----------------------------------------------------------------------------

# Working directory for spawned task processes
# Env: RUNNER_WORK_DIR | CLI: -w DIR, --workdir DIR
# Supports ~ for home directory
# work_dir: "~"

# Persistent state directory (runner state JSON, PIDs)
# Env: RUNNER_STATE_DIR
# Supports ~ for home directory
# state_dir: "~/.local/state/brain-runner"

# Log file directory
# Env: RUNNER_LOG_DIR
# Supports ~ for home directory
# log_dir: "~/.local/log"

# -----------------------------------------------------------------------------
# OpenCode Executor
# -----------------------------------------------------------------------------

# opencode:
#   # Path to the opencode binary
#   # Env: OPENCODE_BIN
#   bin: "opencode"
#
#   # Agent name to use (empty = default agent)
#   # Env: OPENCODE_AGENT | CLI: --agent NAME
#   agent: ""
#
#   # Model identifier (empty = use opencode's configured default)
#   # Env: OPENCODE_MODEL | CLI: -m NAME, --model NAME
#   model: ""

# -----------------------------------------------------------------------------
# Project Filtering
# -----------------------------------------------------------------------------

# Glob patterns for projects to always exclude
# CLI: -e PATTERN, --exclude PATTERN (additive, per-invocation)
# exclude_projects: []
# Example:
# exclude_projects:
#   - "test-*"
#   - "legacy-*"

# -----------------------------------------------------------------------------
# Automation
# -----------------------------------------------------------------------------

# Auto-create "Blocked Task Inspector" and "Feature Code Review" monitors
# for every new feature_id detected at runtime.
# Overridable at runtime via the global Settings popup (S key).
# Env: BRAIN_AUTO_MONITORS
# CLI: --auto-monitors
# auto_monitors: false
`;

// =============================================================================
// Snake_case to camelCase Key Mapping
// =============================================================================

/** Maps snake_case YAML keys to camelCase RunnerConfig keys */
const KEY_MAP: Record<string, string> = {
  brain_api_url: "brainApiUrl",
  poll_interval: "pollInterval",
  task_poll_interval: "taskPollInterval",
  max_parallel: "maxParallel",
  state_dir: "stateDir",
  log_dir: "logDir",
  work_dir: "workDir",
  api_timeout: "apiTimeout",
  task_timeout: "taskTimeout",
  idle_detection_threshold: "idleDetectionThreshold",
  max_total_processes: "maxTotalProcesses",
  memory_threshold_percent: "memoryThresholdPercent",
  exclude_projects: "excludeProjects",
  auto_monitors: "autoMonitors",
};

/**
 * Normalize parsed config keys from snake_case to camelCase.
 * Accepts both snake_case (YAML-idiomatic) and camelCase (backward compat).
 */
function normalizeKeys(raw: Record<string, unknown>): Partial<RunnerConfig> {
  const result: Record<string, unknown> = {};

  for (const [key, value] of Object.entries(raw)) {
    const mappedKey = KEY_MAP[key] ?? key;
    result[mappedKey] = value;
  }

  return result as Partial<RunnerConfig>;
}

/**
 * Expand ~ to home directory in path strings.
 */
function expandTilde(path: string): string {
  if (path.startsWith("~/")) {
    return join(homedir(), path.slice(2));
  }
  if (path === "~") {
    return homedir();
  }
  return path;
}

// =============================================================================
// Environment Variable Helpers
// =============================================================================

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

// =============================================================================
// Config File Loading
// =============================================================================

/**
 * Load config from file. Checks config.yaml, config.yml, config.json in order.
 * Returns null if no config file found.
 */
function loadConfigFile(): Partial<RunnerConfig> | null {
  for (const configFile of CONFIG_FILES) {
    if (!existsSync(configFile)) continue;

    try {
      const content = readFileSync(configFile, "utf-8");
      const raw = configFile.endsWith(".json")
        ? JSON.parse(content)
        : parseYaml(content);

      if (!raw || typeof raw !== "object") return null;

      return normalizeKeys(raw as Record<string, unknown>);
    } catch (error) {
      console.warn(`Failed to load config file: ${configFile}`, error);
      return null;
    }
  }

  return null;
}

/**
 * Validate configuration values are within reasonable ranges.
 * Throws an error if validation fails.
 */
function validateConfig(config: RunnerConfig): void {
  const errors: string[] = [];

  // Validate maxParallel
  if (config.maxParallel < 1 || config.maxParallel > 100) {
    errors.push(`maxParallel must be between 1 and 100, got ${config.maxParallel}`);
  }

  // Validate maxTotalProcesses
  if (config.maxTotalProcesses < 1 || config.maxTotalProcesses > 100) {
    errors.push(`maxTotalProcesses must be between 1 and 100, got ${config.maxTotalProcesses}`);
  }

  // Validate memoryThresholdPercent
  if (config.memoryThresholdPercent < 0 || config.memoryThresholdPercent > 100) {
    errors.push(`memoryThresholdPercent must be between 0 and 100, got ${config.memoryThresholdPercent}`);
  }

  // Validate maxTotalProcesses >= maxParallel
  if (config.maxTotalProcesses < config.maxParallel) {
    errors.push(`maxTotalProcesses (${config.maxTotalProcesses}) must be >= maxParallel (${config.maxParallel})`);
  }

  // Validate poll intervals
  if (config.pollInterval < 1) {
    errors.push(`pollInterval must be >= 1, got ${config.pollInterval}`);
  }
  if (config.taskPollInterval < 1) {
    errors.push(`taskPollInterval must be >= 1, got ${config.taskPollInterval}`);
  }

  // Validate timeouts (positive numbers)
  if (config.apiTimeout < 0) {
    errors.push(`apiTimeout must be >= 0, got ${config.apiTimeout}`);
  }
  if (config.taskTimeout < 0) {
    errors.push(`taskTimeout must be >= 0, got ${config.taskTimeout}`);
  }
  if (config.idleDetectionThreshold < 0) {
    errors.push(`idleDetectionThreshold must be >= 0, got ${config.idleDetectionThreshold}`);
  }

  if (errors.length > 0) {
    throw new Error(`Invalid runner configuration:\n  - ${errors.join("\n  - ")}`);
  }
}

// =============================================================================
// Config Loading
// =============================================================================

export function loadConfig(): RunnerConfig {
  const fileConfig = loadConfigFile() ?? {};

  const opencode: OpencodeConfig = {
    bin: getEnv("OPENCODE_BIN", fileConfig.opencode?.bin ?? "opencode"),
    agent: getEnv("OPENCODE_AGENT", fileConfig.opencode?.agent ?? ""),
    model: getEnv(
      "OPENCODE_MODEL",
      fileConfig.opencode?.model ?? ""
    ),
  };

  // Expand tilde for path fields from file config
  const fileStateDir = fileConfig.stateDir ? expandTilde(fileConfig.stateDir) : undefined;
  const fileLogDir = fileConfig.logDir ? expandTilde(fileConfig.logDir) : undefined;
  const fileWorkDir = fileConfig.workDir ? expandTilde(fileConfig.workDir) : undefined;

  const config: RunnerConfig = {
    brainApiUrl: getEnv(
      "BRAIN_API_URL",
      fileConfig.brainApiUrl ?? "http://localhost:3333"
    ),
    pollInterval: getEnvInt(
      "RUNNER_POLL_INTERVAL",
      fileConfig.pollInterval ?? 30
    ),
    taskPollInterval: getEnvInt(
      "RUNNER_TASK_POLL_INTERVAL",
      fileConfig.taskPollInterval ?? 5
    ),
    maxParallel: getEnvInt("RUNNER_MAX_PARALLEL", fileConfig.maxParallel ?? 2),
    stateDir: getEnv(
      "RUNNER_STATE_DIR",
      fileStateDir ?? join(homedir(), ".local", "state", "brain-runner")
    ),
    logDir: getEnv(
      "RUNNER_LOG_DIR",
      fileLogDir ?? join(homedir(), ".local", "log")
    ),
    workDir: getEnv("RUNNER_WORK_DIR", fileWorkDir ?? homedir()),
    apiTimeout: getEnvInt("RUNNER_API_TIMEOUT", fileConfig.apiTimeout ?? 5000),
    taskTimeout: getEnvInt(
      "RUNNER_TASK_TIMEOUT",
      fileConfig.taskTimeout ?? 0
    ),
    idleDetectionThreshold: getEnvInt(
      "RUNNER_IDLE_THRESHOLD",
      fileConfig.idleDetectionThreshold ?? 60000
    ),
    maxTotalProcesses: getEnvInt(
      "RUNNER_MAX_TOTAL_PROCESSES",
      fileConfig.maxTotalProcesses ?? 10
    ),
    memoryThresholdPercent: getEnvInt(
      "RUNNER_MEMORY_THRESHOLD",
      fileConfig.memoryThresholdPercent ?? 10
    ),
    opencode,
    excludeProjects: fileConfig.excludeProjects ?? [],
    autoMonitors: getEnvBool("BRAIN_AUTO_MONITORS", (fileConfig as Record<string, unknown>).autoMonitors as boolean ?? false),
  };

  validateConfig(config);
  return config;
}

// =============================================================================
// Config Init (write default YAML)
// =============================================================================

/**
 * Get the path to the config directory.
 */
export function getConfigDir(): string {
  return CONFIG_DIR;
}

/**
 * Get the default config file path (config.yaml).
 */
export function getDefaultConfigPath(): string {
  return DEFAULT_CONFIG_PATH;
}

/**
 * Find the first existing config file, or null if none exists.
 */
export function findExistingConfigFile(): string | null {
  for (const configFile of CONFIG_FILES) {
    if (existsSync(configFile)) return configFile;
  }
  return null;
}

/**
 * Write the default config.yaml to ~/.config/brain-runner/config.yaml.
 * Returns { created: true, path } on success.
 * Returns { created: false, path } if a config file already exists.
 *
 * NOTE: Never overwrites an existing config file, even with --force.
 */
export function writeDefaultConfig(): { created: boolean; path: string } {
  // Check if ANY config file already exists (yaml, yml, or json)
  const existing = findExistingConfigFile();
  if (existing) {
    return { created: false, path: existing };
  }

  // Create config directory if it doesn't exist
  mkdirSync(CONFIG_DIR, { recursive: true });

  // Write the default YAML config
  writeFileSync(DEFAULT_CONFIG_PATH, DEFAULT_CONFIG_YAML, "utf-8");

  return { created: true, path: DEFAULT_CONFIG_PATH };
}

// =============================================================================
// Singleton
// =============================================================================

// Singleton instance
let config: RunnerConfig | null = null;

/**
 * Get the runner configuration singleton.
 * Loads config from environment variables and optional config file on first call.
 */
export function getRunnerConfig(): RunnerConfig {
  if (!config) {
    config = loadConfig();
  }
  return config;
}

/**
 * Reset the config singleton (useful for testing).
 */
export function resetConfig(): void {
  config = null;
}

/**
 * Check if debug mode is enabled via DEBUG environment variable.
 */
export function isDebugEnabled(): boolean {
  return getEnvBool("DEBUG", false);
}
