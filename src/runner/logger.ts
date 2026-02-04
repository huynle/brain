/**
 * Brain Runner Logger
 *
 * Structured logging with JSON output, multiple levels, and file/console output.
 * Follows the singleton pattern used by other runner modules.
 */

import { existsSync, mkdirSync, appendFileSync } from "fs";
import { join } from "path";
import { getRunnerConfig } from "./config";

// =============================================================================
// Types
// =============================================================================

export type LogLevel = "debug" | "info" | "warn" | "error";

export interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  context?: Record<string, unknown>;
}

export interface LoggerConfig {
  level: LogLevel;
  logDir: string;
  logFile: string;
  jsonOutput: boolean;
  colorOutput: boolean;
  suppressConsole: boolean;
}

// =============================================================================
// Constants
// =============================================================================

const LOG_LEVELS: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

const COLORS: Record<LogLevel, string> = {
  debug: "\x1b[36m", // Cyan
  info: "\x1b[32m",  // Green
  warn: "\x1b[33m",  // Yellow
  error: "\x1b[31m", // Red
};

const RESET = "\x1b[0m";
const DIM = "\x1b[2m";

// =============================================================================
// Logger Class
// =============================================================================

export class Logger {
  private config: LoggerConfig;
  private logFilePath: string | null = null;

  constructor(config?: Partial<LoggerConfig>) {
    const runnerConfig = getRunnerConfig();

    this.config = {
      level: (process.env.LOG_LEVEL as LogLevel) ?? config?.level ?? "info",
      logDir: config?.logDir ?? runnerConfig.logDir,
      logFile: config?.logFile ?? "brain-runner.log",
      jsonOutput: config?.jsonOutput ?? false,
      colorOutput: config?.colorOutput ?? process.stdout.isTTY ?? false,
      suppressConsole: config?.suppressConsole ?? false,
    };

    // Initialize log file path
    if (this.config.logDir) {
      this.initLogFile();
    }
  }

  // ========================================
  // Log Methods
  // ========================================

  debug(message: string, context?: Record<string, unknown>): void {
    this.log("debug", message, context);
  }

  info(message: string, context?: Record<string, unknown>): void {
    this.log("info", message, context);
  }

  warn(message: string, context?: Record<string, unknown>): void {
    this.log("warn", message, context);
  }

  error(message: string, context?: Record<string, unknown>): void {
    this.log("error", message, context);
  }

  // ========================================
  // Core Logging
  // ========================================

  private log(level: LogLevel, message: string, context?: Record<string, unknown>): void {
    // Check if this level should be logged
    if (LOG_LEVELS[level] < LOG_LEVELS[this.config.level]) {
      return;
    }

    const entry: LogEntry = {
      timestamp: new Date().toISOString(),
      level,
      message,
      context,
    };

    // Write to console
    this.writeConsole(entry);

    // Write to file
    if (this.logFilePath) {
      this.writeFile(entry);
    }
  }

  // ========================================
  // Output Formatters
  // ========================================

  private writeConsole(entry: LogEntry): void {
    // Skip console output when suppressed (e.g., TUI mode)
    if (this.config.suppressConsole) {
      return;
    }

    const output = this.config.jsonOutput
      ? JSON.stringify(entry)
      : this.formatConsole(entry);

    if (entry.level === "error") {
      console.error(output);
    } else if (entry.level === "warn") {
      console.warn(output);
    } else {
      console.log(output);
    }
  }

  private formatConsole(entry: LogEntry): string {
    const { timestamp, level, message, context } = entry;

    // Format timestamp
    const time = timestamp.split("T")[1].split(".")[0]; // HH:MM:SS

    // Format level with optional color
    let levelStr = level.toUpperCase().padEnd(5);
    if (this.config.colorOutput) {
      levelStr = `${COLORS[level]}${levelStr}${RESET}`;
    }

    // Build message
    let formatted = `${DIM}${time}${RESET} ${levelStr} ${message}`;

    // Add context if present
    if (context && Object.keys(context).length > 0) {
      const contextStr = Object.entries(context)
        .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
        .join(" ");
      formatted += ` ${DIM}${contextStr}${RESET}`;
    }

    return formatted;
  }

  private writeFile(entry: LogEntry): void {
    if (!this.logFilePath) return;

    try {
      const line = JSON.stringify(entry) + "\n";
      appendFileSync(this.logFilePath, line, "utf-8");
    } catch {
      // Silently fail file writes to avoid log loops
    }
  }

  // ========================================
  // Initialization
  // ========================================

  private initLogFile(): void {
    try {
      // Ensure log directory exists
      if (!existsSync(this.config.logDir)) {
        mkdirSync(this.config.logDir, { recursive: true });
      }

      this.logFilePath = join(this.config.logDir, this.config.logFile);
    } catch {
      // Silently fail if we can't create log directory
      this.logFilePath = null;
    }
  }

  // ========================================
  // Accessors
  // ========================================

  getLogFilePath(): string | null {
    return this.logFilePath;
  }

  setLevel(level: LogLevel): void {
    this.config.level = level;
  }

  getLevel(): LogLevel {
    return this.config.level;
  }

  /**
   * Suppress console output (useful for TUI mode).
   * Logs will still be written to the log file.
   */
  setSuppressConsole(suppress: boolean): void {
    this.config.suppressConsole = suppress;
  }

  isSuppressConsole(): boolean {
    return this.config.suppressConsole;
  }
}

// =============================================================================
// Singleton Instance
// =============================================================================

let loggerInstance: Logger | null = null;

/**
 * Get the logger singleton.
 * Creates a new instance on first call.
 */
export function getLogger(): Logger {
  if (!loggerInstance) {
    loggerInstance = new Logger();
  }
  return loggerInstance;
}

/**
 * Reset the logger singleton (useful for testing).
 */
export function resetLogger(): void {
  loggerInstance = null;
}

/**
 * Create a logger with custom configuration.
 * Does not affect the singleton.
 */
export function createLogger(config?: Partial<LoggerConfig>): Logger {
  return new Logger(config);
}
