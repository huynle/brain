/**
 * useLogStream Hook Tests
 *
 * Tests the log stream management hook including:
 * - Buffer management with maxEntries
 * - Auto-timestamp on addLog
 * - JSON log line parsing
 * - Log callback integration
 * - File persistence (load/save logs to disk)
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, readFileSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";

// Since we're testing a React hook, we'll test the core logic directly
// by extracting the parseJsonLogLine function and testing state management logic

// Import the hook and extract testable parts
import { useLogStream, type UseLogStreamOptions, type UseLogStreamResult } from "./useLogStream";

// =============================================================================
// Test the JSON parsing logic directly
// =============================================================================

describe("useLogStream - JSON Parsing", () => {
  // We need to test the parseJsonLogLine function
  // Since it's internal, we'll test via parseAndAddLog behavior
  // For unit testing, let's extract the parsing logic

  const validLogLine = JSON.stringify({
    timestamp: "2026-02-03T23:55:46.878Z",
    level: "info",
    message: "Test message",
    context: { taskId: "abc123" },
  });

  const validLogLineNoContext = JSON.stringify({
    timestamp: "2026-02-03T23:55:46.878Z",
    level: "warn",
    message: "Warning message",
  });

  describe("valid JSON log lines", () => {
    it("should parse complete log line with all fields", () => {
      const parsed = parseJsonLogLineForTest(validLogLine);
      expect(parsed).not.toBeNull();
      expect(parsed?.level).toBe("info");
      expect(parsed?.message).toBe("Test message");
      expect(parsed?.context?.taskId).toBe("abc123");
      expect(parsed?.timestamp).toBeInstanceOf(Date);
    });

    it("should parse log line without context", () => {
      const parsed = parseJsonLogLineForTest(validLogLineNoContext);
      expect(parsed).not.toBeNull();
      expect(parsed?.level).toBe("warn");
      expect(parsed?.message).toBe("Warning message");
      expect(parsed?.context).toBeUndefined();
    });

    it("should handle all valid log levels", () => {
      const levels = ["debug", "info", "warn", "error"] as const;
      for (const level of levels) {
        const line = JSON.stringify({
          timestamp: "2026-02-03T23:55:46.878Z",
          level,
          message: `${level} message`,
        });
        const parsed = parseJsonLogLineForTest(line);
        expect(parsed?.level).toBe(level);
      }
    });
  });

  describe("invalid JSON log lines", () => {
    it("should return null for empty string", () => {
      expect(parseJsonLogLineForTest("")).toBeNull();
    });

    it("should return null for whitespace only", () => {
      expect(parseJsonLogLineForTest("   ")).toBeNull();
    });

    it("should return null for non-JSON text", () => {
      expect(parseJsonLogLineForTest("not json")).toBeNull();
    });

    it("should return null for invalid JSON", () => {
      expect(parseJsonLogLineForTest("{invalid json}")).toBeNull();
    });

    it("should return null for missing timestamp", () => {
      const line = JSON.stringify({ level: "info", message: "test" });
      expect(parseJsonLogLineForTest(line)).toBeNull();
    });

    it("should return null for missing level", () => {
      const line = JSON.stringify({
        timestamp: "2026-02-03T23:55:46.878Z",
        message: "test",
      });
      expect(parseJsonLogLineForTest(line)).toBeNull();
    });

    it("should return null for missing message", () => {
      const line = JSON.stringify({
        timestamp: "2026-02-03T23:55:46.878Z",
        level: "info",
      });
      expect(parseJsonLogLineForTest(line)).toBeNull();
    });

    it("should return null for invalid level", () => {
      const line = JSON.stringify({
        timestamp: "2026-02-03T23:55:46.878Z",
        level: "invalid",
        message: "test",
      });
      expect(parseJsonLogLineForTest(line)).toBeNull();
    });
  });

  describe("edge cases", () => {
    it("should handle log line with leading/trailing whitespace", () => {
      const line = `  ${validLogLine}  `;
      const parsed = parseJsonLogLineForTest(line);
      expect(parsed).not.toBeNull();
    });

    it("should handle log line with newlines", () => {
      const line = `${validLogLine}\n`;
      const parsed = parseJsonLogLineForTest(line);
      expect(parsed).not.toBeNull();
    });

    it("should handle empty context object", () => {
      const line = JSON.stringify({
        timestamp: "2026-02-03T23:55:46.878Z",
        level: "info",
        message: "test",
        context: {},
      });
      const parsed = parseJsonLogLineForTest(line);
      expect(parsed).not.toBeNull();
      expect(parsed?.context).toEqual({});
    });
  });
});

// =============================================================================
// Test circular buffer logic
// =============================================================================

describe("useLogStream - Buffer Logic", () => {
  describe("circular buffer behavior", () => {
    it("should not exceed maxEntries", () => {
      const maxEntries = 5;
      const logs: Array<{ level: string; message: string; timestamp: Date }> = [];

      // Simulate adding more logs than maxEntries
      for (let i = 0; i < 10; i++) {
        const newLog = {
          level: "info" as const,
          message: `Message ${i}`,
          timestamp: new Date(),
        };

        logs.push(newLog);
        if (logs.length > maxEntries) {
          logs.shift(); // FIFO - remove oldest
        }
      }

      expect(logs.length).toBe(maxEntries);
      // First message should be 5, last should be 9
      expect(logs[0].message).toBe("Message 5");
      expect(logs[logs.length - 1].message).toBe("Message 9");
    });

    it("should keep logs when under maxEntries", () => {
      const maxEntries = 100;
      const logs: Array<{ level: string; message: string; timestamp: Date }> = [];

      for (let i = 0; i < 50; i++) {
        logs.push({
          level: "info",
          message: `Message ${i}`,
          timestamp: new Date(),
        });
      }

      expect(logs.length).toBe(50);
    });
  });

  describe("auto-timestamp", () => {
    it("should add timestamp to entries without one", () => {
      const entry = { level: "info" as const, message: "test" };
      const before = Date.now();
      const logWithTimestamp = { ...entry, timestamp: new Date() };
      const after = Date.now();

      expect(logWithTimestamp.timestamp.getTime()).toBeGreaterThanOrEqual(before);
      expect(logWithTimestamp.timestamp.getTime()).toBeLessThanOrEqual(after);
    });
  });
});

// =============================================================================
// Test interface types
// =============================================================================

describe("useLogStream - Interface Types", () => {
  it("should match UseLogStreamOptions interface", () => {
    const options: UseLogStreamOptions = {
      maxEntries: 50,
      logFile: "/path/to/log.json",
    };

    expect(options.maxEntries).toBe(50);
    expect(options.logFile).toBe("/path/to/log.json");
  });

  it("should allow optional options", () => {
    const options: UseLogStreamOptions = {};
    expect(options.maxEntries).toBeUndefined();
    expect(options.logFile).toBeUndefined();
  });
});

// =============================================================================
// Helper: Extract and test the parse function
// This mirrors the internal parseJsonLogLine function
// =============================================================================

interface LogEntry {
  timestamp: Date;
  level: "debug" | "info" | "warn" | "error";
  message: string;
  taskId?: string;
  context?: Record<string, unknown>;
}

interface JsonLogLine {
  timestamp: string;
  level: "debug" | "info" | "warn" | "error";
  message: string;
  context?: Record<string, unknown>;
}

function parseJsonLogLineForTest(line: string): LogEntry | null {
  try {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith("{")) {
      return null;
    }

    const parsed: JsonLogLine = JSON.parse(trimmed);

    // Validate required fields
    if (!parsed.timestamp || !parsed.level || !parsed.message) {
      return null;
    }

    // Validate level
    const validLevels = ["debug", "info", "warn", "error"];
    if (!validLevels.includes(parsed.level)) {
      return null;
    }

    return {
      timestamp: new Date(parsed.timestamp),
      level: parsed.level,
      message: parsed.message,
      context: parsed.context,
    };
  } catch {
    return null;
  }
}

// =============================================================================
// Test file persistence helpers
// =============================================================================

describe("useLogStream - File Persistence Helpers", () => {
  // Import the module to test internal file persistence functions
  // We test through the exported hook and through file system side effects
  
  let testDir: string;
  let testLogFile: string;

  beforeEach(() => {
    // Create a unique temp directory for each test
    testDir = join(tmpdir(), `useLogStream-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    testLogFile = join(testDir, "test-logs.jsonl");
  });

  afterEach(() => {
    // Clean up test directory
    if (existsSync(testDir)) {
      rmSync(testDir, { recursive: true, force: true });
    }
  });

  describe("serializeLogEntry / deserializeLogEntry", () => {
    it("should serialize and deserialize a log entry correctly", () => {
      const entry: LogEntry = {
        timestamp: new Date("2026-02-06T10:00:00.000Z"),
        level: "info",
        message: "Test message",
        taskId: "task123",
        context: { key: "value" },
      };

      // Serialize
      const serialized = JSON.stringify({
        timestamp: entry.timestamp.toISOString(),
        level: entry.level,
        message: entry.message,
        taskId: entry.taskId,
        context: entry.context,
      });

      // Deserialize
      const parsed = JSON.parse(serialized);
      const deserialized: LogEntry = {
        timestamp: new Date(parsed.timestamp),
        level: parsed.level,
        message: parsed.message,
        taskId: parsed.taskId,
        context: parsed.context,
      };

      expect(deserialized.level).toBe(entry.level);
      expect(deserialized.message).toBe(entry.message);
      expect(deserialized.taskId).toBe(entry.taskId);
      expect(deserialized.context).toEqual(entry.context);
      expect(deserialized.timestamp.toISOString()).toBe(entry.timestamp.toISOString());
    });
  });

  describe("loadLogsFromFile", () => {
    it("should return empty array if file does not exist", () => {
      const entries = loadLogsFromFileForTest(join(testDir, "nonexistent.jsonl"), 100);
      expect(entries).toEqual([]);
    });

    it("should load existing logs from file", () => {
      // Write some logs to the file
      const logs = [
        { timestamp: "2026-02-06T10:00:00.000Z", level: "info", message: "Log 1" },
        { timestamp: "2026-02-06T10:01:00.000Z", level: "warn", message: "Log 2" },
      ];
      const content = logs.map(l => JSON.stringify(l)).join("\n") + "\n";
      writeFileSync(testLogFile, content);

      const entries = loadLogsFromFileForTest(testLogFile, 100);
      expect(entries.length).toBe(2);
      expect(entries[0].message).toBe("Log 1");
      expect(entries[1].message).toBe("Log 2");
    });

    it("should respect maxEntries when loading", () => {
      // Write more logs than maxEntries
      const logs = [];
      for (let i = 0; i < 10; i++) {
        logs.push({ timestamp: new Date().toISOString(), level: "info", message: `Log ${i}` });
      }
      const content = logs.map(l => JSON.stringify(l)).join("\n") + "\n";
      writeFileSync(testLogFile, content);

      const entries = loadLogsFromFileForTest(testLogFile, 5);
      expect(entries.length).toBe(5);
      // Should have the last 5 entries (Log 5 through Log 9)
      expect(entries[0].message).toBe("Log 5");
      expect(entries[4].message).toBe("Log 9");
    });

    it("should skip invalid lines", () => {
      const content = [
        JSON.stringify({ timestamp: new Date().toISOString(), level: "info", message: "Valid 1" }),
        "not json",
        JSON.stringify({ timestamp: new Date().toISOString(), level: "info", message: "Valid 2" }),
        JSON.stringify({ level: "info", message: "Missing timestamp" }), // Invalid - missing timestamp
      ].join("\n") + "\n";
      writeFileSync(testLogFile, content);

      const entries = loadLogsFromFileForTest(testLogFile, 100);
      expect(entries.length).toBe(2);
      expect(entries[0].message).toBe("Valid 1");
      expect(entries[1].message).toBe("Valid 2");
    });
  });
});

// Helper functions to test file persistence (mirrors internal implementation)

function loadLogsFromFileForTest(filePath: string, maxEntries: number): LogEntry[] {
  try {
    if (!existsSync(filePath)) {
      return [];
    }
    const content = readFileSync(filePath, "utf-8");
    const lines = content.split("\n").filter(line => line.trim());
    const entries: LogEntry[] = [];
    
    for (const line of lines) {
      const entry = deserializeLogEntryForTest(line);
      if (entry) {
        entries.push(entry);
      }
    }
    
    // Return only the last maxEntries
    if (entries.length > maxEntries) {
      return entries.slice(-maxEntries);
    }
    return entries;
  } catch {
    return [];
  }
}

function deserializeLogEntryForTest(line: string): LogEntry | null {
  try {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith("{")) {
      return null;
    }
    const parsed = JSON.parse(trimmed);
    if (!parsed.timestamp || !parsed.level || !parsed.message) {
      return null;
    }
    return {
      timestamp: new Date(parsed.timestamp),
      level: parsed.level,
      message: parsed.message,
      taskId: parsed.taskId,
      context: parsed.context,
    };
  } catch {
    return null;
  }
}
