/**
 * Brain API - Utility Functions
 *
 * Common utility functions for string manipulation and logging.
 */

import { getConfig } from "./config";

// =============================================================================
// App Info Utilities
// =============================================================================

/**
 * Get application info (name and version) from config.
 * @returns Object with appName and version
 */
export function getAppInfo(): { appName: string; version: string } {
  const config = getConfig();
  return {
    appName: "brain-api",
    version: "1.0.0",
  };
}

/**
 * Check if debug mode is enabled.
 * @returns true if debug mode is enabled
 */
export function isDebug(): boolean {
  const config = getConfig();
  return config.server.logLevel === "debug";
}

// =============================================================================
// String Manipulation Utilities
// =============================================================================

/**
 * Truncate a string to a maximum length, adding ellipsis if truncated.
 * @param str - The string to truncate
 * @param maxLength - Maximum length (default: 50)
 * @param suffix - Suffix to add when truncated (default: "...")
 */
export function truncate(str: string, maxLength = 50, suffix = "..."): string {
  if (str.length <= maxLength) return str;
  return str.slice(0, maxLength - suffix.length) + suffix;
}

/**
 * Convert a string to kebab-case.
 * @param str - The string to convert
 */
export function toKebabCase(str: string): string {
  return str
    .replace(/([a-z])([A-Z])/g, "$1-$2")
    .replace(/[\s_]+/g, "-")
    .toLowerCase();
}

/**
 * Convert a string to camelCase.
 * @param str - The string to convert
 */
export function toCamelCase(str: string): string {
  return str
    .replace(/[-_\s]+(.)?/g, (_, char) => (char ? char.toUpperCase() : ""))
    .replace(/^[A-Z]/, (char) => char.toLowerCase());
}

/**
 * Capitalize the first letter of a string.
 * @param str - The string to capitalize
 */
export function capitalize(str: string): string {
  if (!str) return str;
  return str.charAt(0).toUpperCase() + str.slice(1);
}

/**
 * Slugify a string for use in URLs or file names.
 * @param str - The string to slugify
 */
export function slugify(str: string): string {
  return str
    .toLowerCase()
    .trim()
    .replace(/[^\w\s-]/g, "")
    .replace(/[\s_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// =============================================================================
// Logging Utilities
// =============================================================================

/** Log level for simple console logging */
export type SimpleLogLevel = "debug" | "info" | "warn" | "error";

const LOG_PREFIXES: Record<SimpleLogLevel, string> = {
  debug: "[DEBUG]",
  info: "[INFO]",
  warn: "[WARN]",
  error: "[ERROR]",
};

/**
 * Simple log function with level prefix.
 * For structured logging, use the Logger class from src/runner/logger.ts
 * @param level - Log level
 * @param message - Message to log
 * @param data - Optional data to include
 */
export function log(
  level: SimpleLogLevel,
  message: string,
  data?: unknown
): void {
  const prefix = LOG_PREFIXES[level];
  const timestamp = new Date().toISOString();

  if (data !== undefined) {
    console[level](`${timestamp} ${prefix} ${message}`, data);
  } else {
    console[level](`${timestamp} ${prefix} ${message}`);
  }
}

/**
 * Create a prefixed logger for a specific module.
 * @param moduleName - Name of the module for prefixing
 */
export function createPrefixedLogger(moduleName: string) {
  return {
    debug: (msg: string, data?: unknown) =>
      log("debug", `[${moduleName}] ${msg}`, data),
    info: (msg: string, data?: unknown) =>
      log("info", `[${moduleName}] ${msg}`, data),
    warn: (msg: string, data?: unknown) =>
      log("warn", `[${moduleName}] ${msg}`, data),
    error: (msg: string, data?: unknown) =>
      log("error", `[${moduleName}] ${msg}`, data),
  };
}

/**
 * Format an error for logging, extracting message and stack.
 * @param err - The error to format
 */
export function formatError(err: unknown): { message: string; stack?: string } {
  if (err instanceof Error) {
    return {
      message: err.message,
      stack: err.stack,
    };
  }
  return {
    message: String(err),
  };
}

// =============================================================================
// Time Utilities
// =============================================================================

/**
 * Format milliseconds to a human-readable duration string.
 * @param ms - Duration in milliseconds
 */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3600000) return `${(ms / 60000).toFixed(1)}m`;
  return `${(ms / 3600000).toFixed(1)}h`;
}

/**
 * Create a simple timer for measuring elapsed time.
 */
export function createTimer() {
  const start = Date.now();
  return {
    elapsed: () => Date.now() - start,
    elapsedFormatted: () => formatDuration(Date.now() - start),
  };
}
