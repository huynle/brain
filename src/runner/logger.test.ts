/**
 * Logger Tests
 */

import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import {
  Logger,
  getLogger,
  resetLogger,
  createLogger,
  type LogLevel,
} from "./logger";
import { resetConfig } from "./config";

describe("Logger", () => {
  beforeEach(() => {
    resetLogger();
    resetConfig();
  });

  afterEach(() => {
    resetLogger();
    resetConfig();
  });

  describe("log levels", () => {
    it("should filter logs below configured level", () => {
      const logs: string[] = [];
      const originalLog = console.log;
      const originalWarn = console.warn;
      const originalError = console.error;

      console.log = (msg: string) => logs.push(`log:${msg}`);
      console.warn = (msg: string) => logs.push(`warn:${msg}`);
      console.error = (msg: string) => logs.push(`error:${msg}`);

      const logger = createLogger({ level: "warn", colorOutput: false });
      logger.debug("debug message");
      logger.info("info message");
      logger.warn("warn message");
      logger.error("error message");

      console.log = originalLog;
      console.warn = originalWarn;
      console.error = originalError;

      // Only warn and error should be logged (debug and info filtered out)
      expect(logs.length).toBe(2);
      expect(logs[0]).toContain("warn:");
      expect(logs[1]).toContain("error:");
    });

    it("should log all levels when set to debug", () => {
      const logs: string[] = [];
      const originalLog = console.log;
      const originalWarn = console.warn;
      const originalError = console.error;

      console.log = (msg: string) => logs.push(msg);
      console.warn = (msg: string) => logs.push(msg);
      console.error = (msg: string) => logs.push(msg);

      const logger = createLogger({ level: "debug", colorOutput: false });
      logger.debug("debug");
      logger.info("info");
      logger.warn("warn");
      logger.error("error");

      console.log = originalLog;
      console.warn = originalWarn;
      console.error = originalError;

      expect(logs.length).toBe(4);
    });
  });

  describe("setLevel", () => {
    it("should change the log level", () => {
      const logger = createLogger({ level: "info" });
      expect(logger.getLevel()).toBe("info");

      logger.setLevel("debug");
      expect(logger.getLevel()).toBe("debug");
    });
  });

  describe("singleton", () => {
    it("should return the same instance", () => {
      const logger1 = getLogger();
      const logger2 = getLogger();
      expect(logger1).toBe(logger2);
    });

    it("should return new instance after reset", () => {
      const logger1 = getLogger();
      resetLogger();
      const logger2 = getLogger();
      expect(logger1).not.toBe(logger2);
    });
  });

  describe("context logging", () => {
    it("should include context in output", () => {
      let output = "";
      const originalLog = console.log;
      console.log = (msg: string) => {
        output = msg;
      };

      const logger = createLogger({ level: "info", colorOutput: false });
      logger.info("test message", { key: "value", num: 42 });

      console.log = originalLog;

      expect(output).toContain("test message");
      expect(output).toContain("key=");
      expect(output).toContain("value");
    });
  });

  describe("JSON output", () => {
    it("should output JSON when configured", () => {
      let output = "";
      const originalLog = console.log;
      console.log = (msg: string) => {
        output = msg;
      };

      const logger = createLogger({
        level: "info",
        jsonOutput: true,
        colorOutput: false,
      });
      logger.info("json test", { foo: "bar" });

      console.log = originalLog;

      const parsed = JSON.parse(output);
      expect(parsed.level).toBe("info");
      expect(parsed.message).toBe("json test");
      expect(parsed.context.foo).toBe("bar");
      expect(parsed.timestamp).toBeDefined();
    });
  });

  describe("suppressConsole", () => {
    it("should suppress console output when enabled", () => {
      const logs: string[] = [];
      const originalLog = console.log;
      const originalWarn = console.warn;
      const originalError = console.error;

      console.log = (msg: string) => logs.push(msg);
      console.warn = (msg: string) => logs.push(msg);
      console.error = (msg: string) => logs.push(msg);

      const logger = createLogger({ level: "debug", colorOutput: false, suppressConsole: true });
      logger.debug("debug");
      logger.info("info");
      logger.warn("warn");
      logger.error("error");

      console.log = originalLog;
      console.warn = originalWarn;
      console.error = originalError;

      // No logs should be output to console when suppressed
      expect(logs.length).toBe(0);
    });

    it("should allow toggling suppression at runtime", () => {
      const logs: string[] = [];
      const originalLog = console.log;
      console.log = (msg: string) => logs.push(msg);

      const logger = createLogger({ level: "info", colorOutput: false });

      // Log with console output enabled
      logger.info("visible");
      expect(logs.length).toBe(1);

      // Suppress console
      logger.setSuppressConsole(true);
      logger.info("suppressed");
      expect(logs.length).toBe(1); // Still 1, no new log

      // Re-enable console
      logger.setSuppressConsole(false);
      logger.info("visible again");
      expect(logs.length).toBe(2);

      console.log = originalLog;
    });

    it("should report suppression state", () => {
      const logger = createLogger({ level: "info" });
      expect(logger.isSuppressConsole()).toBe(false);

      logger.setSuppressConsole(true);
      expect(logger.isSuppressConsole()).toBe(true);

      logger.setSuppressConsole(false);
      expect(logger.isSuppressConsole()).toBe(false);
    });
  });
});
