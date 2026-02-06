/**
 * Tests for Brain Runner Configuration
 */

import { describe, it, expect, beforeEach, afterEach, mock } from "bun:test";
import { homedir } from "os";
import { join } from "path";
import { loadConfig, getRunnerConfig, resetConfig, isDebugEnabled } from "./config";
import type { RunnerConfig } from "./types";

describe("config", () => {
  // Save original env vars
  const originalEnv = { ...process.env };

  beforeEach(() => {
    // Reset singleton before each test
    resetConfig();
    // Clear relevant env vars
    delete process.env.BRAIN_API_URL;
    delete process.env.RUNNER_POLL_INTERVAL;
    delete process.env.RUNNER_TASK_POLL_INTERVAL;
    delete process.env.RUNNER_MAX_PARALLEL;
    delete process.env.RUNNER_MAX_TOTAL_PROCESSES;
    delete process.env.RUNNER_MEMORY_THRESHOLD;
    delete process.env.RUNNER_IDLE_THRESHOLD;
    delete process.env.RUNNER_STATE_DIR;
    delete process.env.RUNNER_LOG_DIR;
    delete process.env.RUNNER_WORK_DIR;
    delete process.env.RUNNER_API_TIMEOUT;
    delete process.env.RUNNER_TASK_TIMEOUT;
    delete process.env.OPENCODE_BIN;
    delete process.env.OPENCODE_AGENT;
    delete process.env.OPENCODE_MODEL;
    delete process.env.DEBUG;
  });

  afterEach(() => {
    // Restore original env vars
    process.env = { ...originalEnv };
    resetConfig();
  });

  describe("loadConfig", () => {
    it("returns default config values when no env vars set", () => {
      const config = loadConfig();

      expect(config.brainApiUrl).toBe("http://localhost:3333");
      expect(config.pollInterval).toBe(30);
      expect(config.taskPollInterval).toBe(5);
      expect(config.maxParallel).toBe(2);
      expect(config.maxTotalProcesses).toBe(10);
      expect(config.memoryThresholdPercent).toBe(10);
      expect(config.idleDetectionThreshold).toBe(60000);
      expect(config.stateDir).toBe(join(homedir(), ".local", "state", "brain-runner"));
      expect(config.logDir).toBe(join(homedir(), ".local", "log"));
      expect(config.workDir).toBe(homedir());
      expect(config.apiTimeout).toBe(5000);
      expect(config.taskTimeout).toBe(1800000);
      expect(config.opencode.bin).toBe("opencode");
      expect(config.opencode.agent).toBe("general");
      expect(config.opencode.model).toBe("anthropic/claude-opus-4-5");
      expect(config.excludeProjects).toEqual([]);
    });

    it("uses env var overrides", () => {
      process.env.BRAIN_API_URL = "http://brain.local:8080";
      process.env.RUNNER_POLL_INTERVAL = "60";
      process.env.RUNNER_MAX_PARALLEL = "5";
      process.env.OPENCODE_BIN = "/usr/local/bin/opencode";
      process.env.OPENCODE_AGENT = "tdd-dev";
      process.env.OPENCODE_MODEL = "anthropic/claude-sonnet-4-20250514";

      const config = loadConfig();

      expect(config.brainApiUrl).toBe("http://brain.local:8080");
      expect(config.pollInterval).toBe(60);
      expect(config.maxParallel).toBe(5);
      expect(config.opencode.bin).toBe("/usr/local/bin/opencode");
      expect(config.opencode.agent).toBe("tdd-dev");
      expect(config.opencode.model).toBe("anthropic/claude-sonnet-4-20250514");
    });

    it("handles invalid integer env vars gracefully", () => {
      process.env.RUNNER_POLL_INTERVAL = "not-a-number";
      process.env.RUNNER_MAX_PARALLEL = "";

      const config = loadConfig();

      // Should fall back to defaults
      expect(config.pollInterval).toBe(30);
      expect(config.maxParallel).toBe(2);
    });

    it("loads new process limit config options from env vars", () => {
      process.env.RUNNER_MAX_TOTAL_PROCESSES = "20";
      process.env.RUNNER_MEMORY_THRESHOLD = "15";

      const config = loadConfig();

      expect(config.maxTotalProcesses).toBe(20);
      expect(config.memoryThresholdPercent).toBe(15);
    });
  });

  describe("config validation", () => {
    it("throws error when maxParallel is out of range", () => {
      process.env.RUNNER_MAX_PARALLEL = "0";
      expect(() => loadConfig()).toThrow(/maxParallel must be between 1 and 100/);

      resetConfig();
      process.env.RUNNER_MAX_PARALLEL = "101";
      expect(() => loadConfig()).toThrow(/maxParallel must be between 1 and 100/);
    });

    it("throws error when maxTotalProcesses is out of range", () => {
      process.env.RUNNER_MAX_TOTAL_PROCESSES = "0";
      expect(() => loadConfig()).toThrow(/maxTotalProcesses must be between 1 and 100/);

      resetConfig();
      process.env.RUNNER_MAX_TOTAL_PROCESSES = "101";
      expect(() => loadConfig()).toThrow(/maxTotalProcesses must be between 1 and 100/);
    });

    it("throws error when memoryThresholdPercent is out of range", () => {
      process.env.RUNNER_MEMORY_THRESHOLD = "-1";
      expect(() => loadConfig()).toThrow(/memoryThresholdPercent must be between 0 and 100/);

      resetConfig();
      process.env.RUNNER_MEMORY_THRESHOLD = "101";
      expect(() => loadConfig()).toThrow(/memoryThresholdPercent must be between 0 and 100/);
    });

    it("throws error when maxTotalProcesses < maxParallel", () => {
      process.env.RUNNER_MAX_PARALLEL = "5";
      process.env.RUNNER_MAX_TOTAL_PROCESSES = "3";
      expect(() => loadConfig()).toThrow(/maxTotalProcesses.*must be >= maxParallel/);
    });

    it("throws error when pollInterval is less than 1", () => {
      process.env.RUNNER_POLL_INTERVAL = "0";
      expect(() => loadConfig()).toThrow(/pollInterval must be >= 1/);
    });

    it("accepts valid configuration values", () => {
      process.env.RUNNER_MAX_PARALLEL = "5";
      process.env.RUNNER_MAX_TOTAL_PROCESSES = "10";
      process.env.RUNNER_MEMORY_THRESHOLD = "20";

      const config = loadConfig();

      expect(config.maxParallel).toBe(5);
      expect(config.maxTotalProcesses).toBe(10);
      expect(config.memoryThresholdPercent).toBe(20);
    });

    it("allows memoryThresholdPercent to be 0 (disable memory checks)", () => {
      process.env.RUNNER_MEMORY_THRESHOLD = "0";

      const config = loadConfig();
      expect(config.memoryThresholdPercent).toBe(0);
    });
  });

  describe("getRunnerConfig", () => {
    it("returns singleton instance", () => {
      const config1 = getRunnerConfig();
      const config2 = getRunnerConfig();

      expect(config1).toBe(config2);
    });

    it("caches config after first call", () => {
      process.env.BRAIN_API_URL = "http://first.local";
      const config1 = getRunnerConfig();

      // Change env var after first call
      process.env.BRAIN_API_URL = "http://second.local";
      const config2 = getRunnerConfig();

      // Should still have first value (cached)
      expect(config1.brainApiUrl).toBe("http://first.local");
      expect(config2.brainApiUrl).toBe("http://first.local");
    });

    it("reloads config after resetConfig", () => {
      process.env.BRAIN_API_URL = "http://first.local";
      const config1 = getRunnerConfig();

      resetConfig();
      process.env.BRAIN_API_URL = "http://second.local";
      const config2 = getRunnerConfig();

      expect(config1.brainApiUrl).toBe("http://first.local");
      expect(config2.brainApiUrl).toBe("http://second.local");
    });
  });

  describe("isDebugEnabled", () => {
    it("returns false by default", () => {
      expect(isDebugEnabled()).toBe(false);
    });

    it("returns true when DEBUG=true", () => {
      process.env.DEBUG = "true";
      expect(isDebugEnabled()).toBe(true);
    });

    it("returns true when DEBUG=1", () => {
      process.env.DEBUG = "1";
      expect(isDebugEnabled()).toBe(true);
    });

    it("returns false for other values", () => {
      process.env.DEBUG = "false";
      expect(isDebugEnabled()).toBe(false);

      process.env.DEBUG = "0";
      expect(isDebugEnabled()).toBe(false);

      process.env.DEBUG = "yes";
      expect(isDebugEnabled()).toBe(false);
    });
  });
});
