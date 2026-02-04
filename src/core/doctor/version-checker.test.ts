/**
 * Brain Doctor - Version Checker Tests
 */

import { describe, test, expect, mock, beforeEach, afterEach } from "bun:test";
import {
  compareVersions,
  isOutdated,
  TOOLS,
  getToolInfo,
  getInstalledVersion,
  getLatestVersion,
  checkToolVersion,
} from "./version-checker";

describe("version-checker", () => {
  describe("compareVersions", () => {
    test("equal versions return 0", () => {
      expect(compareVersions("1.0.0", "1.0.0")).toBe(0);
      expect(compareVersions("2.3.4", "2.3.4")).toBe(0);
    });

    test("first version less than second returns -1", () => {
      expect(compareVersions("1.0.0", "2.0.0")).toBe(-1);
      expect(compareVersions("1.0.0", "1.1.0")).toBe(-1);
      expect(compareVersions("1.0.0", "1.0.1")).toBe(-1);
      expect(compareVersions("0.15.2", "0.16.0")).toBe(-1);
    });

    test("first version greater than second returns 1", () => {
      expect(compareVersions("2.0.0", "1.0.0")).toBe(1);
      expect(compareVersions("1.1.0", "1.0.0")).toBe(1);
      expect(compareVersions("1.0.1", "1.0.0")).toBe(1);
    });

    test("handles different version lengths", () => {
      expect(compareVersions("1.0", "1.0.0")).toBe(0);
      expect(compareVersions("1.0.0", "1.0")).toBe(0);
      expect(compareVersions("1.0", "1.0.1")).toBe(-1);
      expect(compareVersions("1.0.1", "1.0")).toBe(1);
    });

    test("handles multi-digit version numbers", () => {
      expect(compareVersions("1.10.0", "1.9.0")).toBe(1);
      expect(compareVersions("1.9.0", "1.10.0")).toBe(-1);
      expect(compareVersions("10.0.0", "9.0.0")).toBe(1);
    });
  });

  describe("isOutdated", () => {
    test("returns true when installed < latest", () => {
      expect(isOutdated("1.0.0", "2.0.0")).toBe(true);
      expect(isOutdated("0.15.2", "0.16.0")).toBe(true);
    });

    test("returns false when installed >= latest", () => {
      expect(isOutdated("2.0.0", "1.0.0")).toBe(false);
      expect(isOutdated("1.0.0", "1.0.0")).toBe(false);
    });

    test("returns false when either version is null", () => {
      expect(isOutdated(null, "1.0.0")).toBe(false);
      expect(isOutdated("1.0.0", null)).toBe(false);
      expect(isOutdated(null, null)).toBe(false);
    });
  });

  describe("TOOLS", () => {
    test("contains expected tools", () => {
      const toolNames = TOOLS.map((t) => t.name);
      expect(toolNames).toContain("opencode");
      expect(toolNames).toContain("claude");
      expect(toolNames).toContain("bun");
      expect(toolNames).toContain("zk");
    });

    test("all tools have required properties", () => {
      for (const tool of TOOLS) {
        expect(tool.name).toBeDefined();
        expect(tool.command).toBeDefined();
        expect(tool.versionArgs).toBeInstanceOf(Array);
        expect(typeof tool.versionParser).toBe("function");
        expect(tool.githubRepo).toBeDefined();
        expect(typeof tool.tagParser).toBe("function");
        expect(typeof tool.required).toBe("boolean");
        expect(tool.installUrl).toBeDefined();
      }
    });
  });

  describe("getToolInfo", () => {
    test("returns tool info for known tools", () => {
      const bun = getToolInfo("bun");
      expect(bun).toBeDefined();
      expect(bun?.name).toBe("bun");
      expect(bun?.required).toBe(true);
    });

    test("returns undefined for unknown tools", () => {
      expect(getToolInfo("unknown-tool")).toBeUndefined();
    });
  });

  describe("version parsers", () => {
    test("opencode parser extracts version", () => {
      const tool = getToolInfo("opencode")!;
      expect(tool.versionParser("1.1.51\n")).toBe("1.1.51");
      expect(tool.versionParser("0.0.55")).toBe("0.0.55");
    });

    test("claude parser extracts version from format", () => {
      const tool = getToolInfo("claude")!;
      expect(tool.versionParser("2.1.3 (Claude Code)")).toBe("2.1.3");
      expect(tool.versionParser("1.0.0 (Claude Code)\n")).toBe("1.0.0");
    });

    test("bun parser extracts version", () => {
      const tool = getToolInfo("bun")!;
      expect(tool.versionParser("1.3.5\n")).toBe("1.3.5");
      expect(tool.versionParser("1.0.0")).toBe("1.0.0");
    });

    test("zk parser extracts version from format", () => {
      const tool = getToolInfo("zk")!;
      expect(tool.versionParser("zk 0.15.2")).toBe("0.15.2");
      expect(tool.versionParser("zk 1.0.0\n")).toBe("1.0.0");
    });
  });

  describe("tag parsers", () => {
    test("opencode tag parser strips v prefix", () => {
      const tool = getToolInfo("opencode")!;
      expect(tool.tagParser("v0.0.55")).toBe("0.0.55");
      expect(tool.tagParser("1.0.0")).toBe("1.0.0");
    });

    test("claude tag parser strips v prefix", () => {
      const tool = getToolInfo("claude")!;
      expect(tool.tagParser("v2.1.31")).toBe("2.1.31");
    });

    test("bun tag parser strips bun-v prefix", () => {
      const tool = getToolInfo("bun")!;
      expect(tool.tagParser("bun-v1.3.8")).toBe("1.3.8");
      expect(tool.tagParser("v1.0.0")).toBe("1.0.0");
    });

    test("zk tag parser strips v prefix", () => {
      const tool = getToolInfo("zk")!;
      expect(tool.tagParser("v0.15.2")).toBe("0.15.2");
    });
  });

  describe("getInstalledVersion", () => {
    test("returns version for installed tool (bun)", async () => {
      // Bun is guaranteed to be installed since we're running tests with it
      const bunTool = getToolInfo("bun")!;
      const version = await getInstalledVersion(bunTool);
      expect(version).not.toBeNull();
      expect(version).toMatch(/^\d+\.\d+\.\d+$/);
    });

    test("returns null for non-existent tool", async () => {
      const fakeTool = {
        name: "fake-tool",
        command: "nonexistent-command-12345",
        versionArgs: ["--version"],
        versionParser: (output: string) => output,
        githubRepo: "fake/repo",
        tagParser: (tag: string) => tag,
        required: false,
        installUrl: "https://example.com",
      };
      const version = await getInstalledVersion(fakeTool);
      expect(version).toBeNull();
    });
  });

  describe("checkToolVersion", () => {
    test("returns version result for bun", async () => {
      const bunTool = getToolInfo("bun")!;
      const result = await checkToolVersion(bunTool);

      expect(result.tool).toBe("bun");
      expect(result.isInstalled).toBe(true);
      expect(result.installed).not.toBeNull();
      // latest may be null if network is unavailable
      expect(typeof result.isOutdated).toBe("boolean");
    });

    test("returns not installed for fake tool", async () => {
      const fakeTool = {
        name: "fake-tool",
        command: "nonexistent-command-12345",
        versionArgs: ["--version"],
        versionParser: (output: string) => output,
        githubRepo: "fake/repo",
        tagParser: (tag: string) => tag,
        required: false,
        installUrl: "https://example.com",
      };
      const result = await checkToolVersion(fakeTool);

      expect(result.tool).toBe("fake-tool");
      expect(result.isInstalled).toBe(false);
      expect(result.installed).toBeNull();
    });
  });
});
