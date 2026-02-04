/**
 * Brain Runner Configuration
 *
 * Loads configuration from environment variables and optional JSON file.
 * Environment variables override file config.
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
    model: getEnv(
      "OPENCODE_MODEL",
      fileConfig.opencode?.model ?? "anthropic/claude-opus-4-5"
    ),
  };

  return {
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
    maxParallel: getEnvInt("RUNNER_MAX_PARALLEL", fileConfig.maxParallel ?? 3),
    stateDir: getEnv(
      "RUNNER_STATE_DIR",
      fileConfig.stateDir ?? join(homedir(), ".local", "state", "brain-runner")
    ),
    logDir: getEnv(
      "RUNNER_LOG_DIR",
      fileConfig.logDir ?? join(homedir(), ".local", "log")
    ),
    workDir: getEnv("RUNNER_WORK_DIR", fileConfig.workDir ?? homedir()),
    apiTimeout: getEnvInt("RUNNER_API_TIMEOUT", fileConfig.apiTimeout ?? 5000),
    taskTimeout: getEnvInt(
      "RUNNER_TASK_TIMEOUT",
      fileConfig.taskTimeout ?? 1800000
    ),
    opencode,
    excludeProjects: fileConfig.excludeProjects ?? [],
  };
}

// Singleton instance
let config: RunnerConfig | null = null;

/**
 * Get the runner configuration singleton.
 * Loads config from environment variables and optional JSON file on first call.
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
