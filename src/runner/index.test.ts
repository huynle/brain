/**
 * CLI Tests
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import { parseArgs, type ParsedArgs, type CLIOptions } from "./index";
import { resetConfig } from "./config";
import { resetLogger } from "./logger";

describe("CLI", () => {
  beforeEach(() => {
    resetConfig();
    resetLogger();
  });

  afterEach(() => {
    resetConfig();
    resetLogger();
  });

  describe("parseArgs", () => {
    // Helper to create argv-like array
    const argv = (...args: string[]) => ["bun", "script.ts", ...args];

    it("should parse command with no arguments", () => {
      const result = parseArgs(argv("start"));
      expect(result.command).toBe("start");
      expect(result.projectId).toBe("all");
    });

    it("should parse command with projectId", () => {
      const result = parseArgs(argv("start", "my-project"));
      expect(result.command).toBe("start");
      expect(result.projectId).toBe("my-project");
    });

    it("should parse help flag", () => {
      const result = parseArgs(argv("--help"));
      expect(result.options.help).toBe(true);
    });

    it("should parse -h flag", () => {
      const result = parseArgs(argv("-h"));
      expect(result.options.help).toBe(true);
    });

    it("should parse foreground flag", () => {
      const result = parseArgs(argv("start", "-f"));
      expect(result.options.foreground).toBe(true);
      expect(result.options.background).toBe(false);
    });

    it("should parse background flag", () => {
      const result = parseArgs(argv("start", "-b"));
      expect(result.options.background).toBe(true);
      expect(result.options.foreground).toBe(false);
    });

    it("should parse tui flag", () => {
      const result = parseArgs(argv("start", "--tui"));
      expect(result.options.tui).toBe(true);
    });

    it("should parse dashboard flag", () => {
      const result = parseArgs(argv("start", "--dashboard"));
      expect(result.options.dashboard).toBe(true);
    });

    it("should parse max-parallel option", () => {
      const result = parseArgs(argv("start", "-p", "5"));
      expect(result.options.maxParallel).toBe(5);
    });

    it("should parse --max-parallel option", () => {
      const result = parseArgs(argv("start", "--max-parallel", "10"));
      expect(result.options.maxParallel).toBe(10);
    });

    it("should parse poll-interval option", () => {
      const result = parseArgs(argv("start", "--poll-interval", "60"));
      expect(result.options.pollInterval).toBe(60);
    });

    it("should parse workdir option", () => {
      const result = parseArgs(argv("start", "-w", "/tmp/work"));
      expect(result.options.workdir).toBe("/tmp/work");
    });

    it("should parse agent option", () => {
      const result = parseArgs(argv("start", "--agent", "general"));
      expect(result.options.agent).toBe("general");
    });

    it("should parse model option", () => {
      const result = parseArgs(argv("start", "-m", "claude-sonnet"));
      expect(result.options.model).toBe("claude-sonnet");
    });

    it("should parse dry-run flag", () => {
      const result = parseArgs(argv("start", "--dry-run"));
      expect(result.options.dryRun).toBe(true);
    });

    it("should parse exclude option (repeatable)", () => {
      const result = parseArgs(argv("start", "-e", "test-*", "-e", "dev-*"));
      expect(result.options.exclude).toEqual(["test-*", "dev-*"]);
    });

    it("should parse no-resume flag", () => {
      const result = parseArgs(argv("start", "--no-resume"));
      expect(result.options.noResume).toBe(true);
    });

    it("should parse verbose flag", () => {
      const result = parseArgs(argv("start", "-v"));
      expect(result.options.verbose).toBe(true);
    });

    it("should parse combined options", () => {
      const result = parseArgs(
        argv("start", "my-project", "-f", "-p", "3", "--poll-interval", "30", "-v")
      );
      expect(result.command).toBe("start");
      expect(result.projectId).toBe("my-project");
      expect(result.options.foreground).toBe(true);
      expect(result.options.maxParallel).toBe(3);
      expect(result.options.pollInterval).toBe(30);
      expect(result.options.verbose).toBe(true);
    });

    it("should default to help command when no args", () => {
      const result = parseArgs(argv());
      expect(result.command).toBe("help");
    });

    it("should handle all commands", () => {
      const commands = [
        "start",
        "stop",
        "status",
        "run-one",
        "list",
        "ready",
        "waiting",
        "blocked",
        "logs",
      ];

      for (const cmd of commands) {
        const result = parseArgs(argv(cmd));
        expect(result.command).toBe(cmd);
      }
    });
  });
});
