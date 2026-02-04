/**
 * Brain API - Configuration
 *
 * Load configuration from environment variables
 */

import { join } from "path";
import { homedir } from "os";
import type { Config, BrainConfig, ServerConfig } from "./core/types";

function getEnv(key: string, defaultValue: string): string {
  return process.env[key] ?? defaultValue;
}

function getEnvBool(key: string, defaultValue: boolean): boolean {
  const value = process.env[key];
  if (value === undefined) return defaultValue;
  return value.toLowerCase() === "true" || value === "1";
}

function getEnvInt(key: string, defaultValue: number): number {
  const value = process.env[key];
  if (value === undefined) return defaultValue;
  const parsed = parseInt(value, 10);
  return isNaN(parsed) ? defaultValue : parsed;
}

export function loadConfig(): Config {
  // Brain configuration
  const brainDir = getEnv("BRAIN_DIR", join(homedir(), ".brain"));
  const dbPath = join(brainDir, "living-brain.db");

  const brain: BrainConfig = {
    brainDir,
    dbPath,
    defaultProject: getEnv("DEFAULT_PROJECT", "default"),
  };

  // Server configuration
  const server: ServerConfig = {
    port: getEnvInt("PORT", 3000),
    host: getEnv("HOST", "0.0.0.0"),
    logLevel: getEnv("LOG_LEVEL", "info") as ServerConfig["logLevel"],
    enableAuth: getEnvBool("ENABLE_AUTH", false),
    apiKey: process.env.API_KEY,
    enableTenants: getEnvBool("ENABLE_TENANTS", false),
  };

  return { brain, server };
}

// Singleton config instance
let config: Config | null = null;

export function getConfig(): Config {
  if (!config) {
    config = loadConfig();
  }
  return config;
}
