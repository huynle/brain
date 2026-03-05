/**
 * Brain Doctor - Version Checker
 *
 * Checks installed versions of external tools against their latest releases.
 */

import { spawn } from "child_process";

// =============================================================================
// Types
// =============================================================================

export interface ToolInfo {
  name: string;
  command: string;
  versionArgs: string[];
  versionParser: (output: string) => string | null;
  githubRepo: string;
  tagParser: (tag: string) => string;
  required: boolean;
  installUrl: string;
}

export interface VersionResult {
  tool: string;
  installed: string | null;
  latest: string | null;
  isOutdated: boolean;
  isInstalled: boolean;
  error?: string;
}

// =============================================================================
// Tool Definitions
// =============================================================================

export const TOOLS: ToolInfo[] = [
  {
    name: "opencode",
    command: "opencode",
    versionArgs: ["--version"],
    versionParser: (output) => output.trim().split("\n")[0] || null,
    githubRepo: "opencode-ai/opencode",
    tagParser: (tag) => tag.replace(/^v/, ""),
    required: false,
    installUrl: "https://opencode.ai",
  },
  {
    name: "claude",
    command: "claude",
    versionArgs: ["--version"],
    versionParser: (output) => {
      // "2.1.3 (Claude Code)" -> "2.1.3"
      const match = output.match(/^([\d.]+)/);
      return match ? match[1] : null;
    },
    githubRepo: "anthropics/claude-code",
    tagParser: (tag) => tag.replace(/^v/, ""),
    required: false,
    installUrl: "https://github.com/anthropics/claude-code",
  },
  {
    name: "bun",
    command: "bun",
    versionArgs: ["--version"],
    versionParser: (output) => output.trim(),
    githubRepo: "oven-sh/bun",
    tagParser: (tag) => tag.replace(/^bun-v/, "").replace(/^v/, ""),
    required: true,
    installUrl: "https://bun.sh",
  },
  {
    name: "zk",
    command: "zk",
    versionArgs: ["--version"],
    versionParser: (output) => {
      // "zk 0.15.2" -> "0.15.2"
      const match = output.match(/zk\s+([\d.]+)/);
      return match ? match[1] : null;
    },
    githubRepo: "zk-org/zk",
    tagParser: (tag) => tag.replace(/^v/, ""),
    required: true,
    installUrl: "https://github.com/zk-org/zk",
  },
];

// =============================================================================
// Version Checking Functions
// =============================================================================

/**
 * Execute a command and return its output.
 */
async function execCommand(command: string, args: string[]): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  return new Promise((resolve) => {
    const proc = spawn(command, args, {
      stdio: ["ignore", "pipe", "pipe"],
    });

    let stdout = "";
    let stderr = "";

    proc.stdout?.on("data", (data) => {
      stdout += data.toString();
    });

    proc.stderr?.on("data", (data) => {
      stderr += data.toString();
    });

    proc.on("error", () => {
      resolve({ stdout: "", stderr: "Command not found", exitCode: 1 });
    });

    proc.on("close", (code) => {
      resolve({ stdout, stderr, exitCode: code ?? 1 });
    });
  });
}

/**
 * Get the installed version of a tool.
 */
export async function getInstalledVersion(tool: ToolInfo): Promise<string | null> {
  try {
    const result = await execCommand(tool.command, tool.versionArgs);
    if (result.exitCode !== 0) {
      return null;
    }
    return tool.versionParser(result.stdout);
  } catch {
    return null;
  }
}

/**
 * Get the latest version from GitHub releases.
 */
export async function getLatestVersion(tool: ToolInfo): Promise<string | null> {
  try {
    const response = await fetch(`https://api.github.com/repos/${tool.githubRepo}/releases/latest`, {
      headers: {
        Accept: "application/vnd.github.v3+json",
        "User-Agent": "brain-doctor",
      },
    });

    if (!response.ok) {
      return null;
    }

    const data = (await response.json()) as { tag_name?: string };
    if (!data.tag_name) {
      return null;
    }

    return tool.tagParser(data.tag_name);
  } catch {
    return null;
  }
}

/**
 * Compare two semantic versions.
 * Returns: -1 if a < b, 0 if a == b, 1 if a > b
 */
export function compareVersions(a: string, b: string): number {
  const partsA = a.split(".").map((p) => parseInt(p, 10) || 0);
  const partsB = b.split(".").map((p) => parseInt(p, 10) || 0);

  const maxLength = Math.max(partsA.length, partsB.length);

  for (let i = 0; i < maxLength; i++) {
    const numA = partsA[i] || 0;
    const numB = partsB[i] || 0;

    if (numA < numB) return -1;
    if (numA > numB) return 1;
  }

  return 0;
}

/**
 * Check if a version is outdated.
 */
export function isOutdated(installed: string | null, latest: string | null): boolean {
  if (!installed || !latest) {
    return false;
  }
  return compareVersions(installed, latest) < 0;
}

/**
 * Check version for a single tool.
 */
export async function checkToolVersion(tool: ToolInfo): Promise<VersionResult> {
  const [installed, latest] = await Promise.all([getInstalledVersion(tool), getLatestVersion(tool)]);

  return {
    tool: tool.name,
    installed,
    latest,
    isOutdated: isOutdated(installed, latest),
    isInstalled: installed !== null,
  };
}

/**
 * Check versions for all tools.
 */
export async function checkAllVersions(): Promise<VersionResult[]> {
  return Promise.all(TOOLS.map(checkToolVersion));
}

/**
 * Get tool info by name.
 */
export function getToolInfo(name: string): ToolInfo | undefined {
  return TOOLS.find((t) => t.name === name);
}
