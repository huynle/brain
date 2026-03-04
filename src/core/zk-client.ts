/**
 * Brain API - ZK Client
 *
 * Wrapper functions for the zk CLI (https://github.com/zk-org/zk)
 * Extracted from the OpenCode brain plugin for use by brain-service
 */

import { spawn } from "child_process";
import { existsSync } from "fs";
import { getConfig } from "../config";
import type {
  ZkNote,
  EntryType,
  EntryStatus,
  Priority,
} from "./types";
import { ENTRY_TYPES, ENTRY_STATUSES } from "./types";

// Re-export frontmatter functions for backward compatibility
export {
  parseFrontmatter,
  serializeFrontmatter,
  generateFrontmatter,
  escapeYamlValue,
  unescapeYamlValue,
  formatYamlMultilineValue,
  normalizeTitle,
  sanitizeTitle,
  sanitizeTag,
  sanitizeSimpleValue,
  sanitizeDependsOnEntry,
} from "./frontmatter";
export type { GenerateFrontmatterOptions, CronRun } from "./frontmatter";

// =============================================================================
// Types
// =============================================================================

export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

// =============================================================================
// ZK Command Execution
// =============================================================================

/**
 * Execute a zk command with arguments.
 * Automatically adds --notebook-dir and --no-input flags.
 */
export async function execZk(
  args: string[],
  timeout = 30000
): Promise<ExecResult> {
  const config = getConfig();
  const brainDir = config.brain.brainDir;

  return new Promise((resolve, reject) => {
    const fullArgs = ["--notebook-dir", brainDir, "--no-input", ...args];

    const proc = spawn("zk", fullArgs, {
      timeout,
      env: { ...process.env },
      cwd: brainDir,
    });

    let stdout = "";
    let stderr = "";

    proc.stdout.on("data", (data) => {
      stdout += data.toString();
    });
    proc.stderr.on("data", (data) => {
      stderr += data.toString();
    });
    proc.on("error", (error) =>
      reject(new Error(`Failed to spawn zk: ${error.message}`))
    );
    proc.on("close", (code) => resolve({ stdout, stderr, exitCode: code ?? 1 }));
  });
}

/**
 * Execute `zk new` with content piped via stdin.
 * Returns the path of the created note.
 */
export async function execZkNew(
  args: string[],
  content: string,
  timeout = 30000
): Promise<ExecResult> {
  const config = getConfig();
  const brainDir = config.brain.brainDir;

  return new Promise((resolve, reject) => {
    const fullArgs = [
      "--notebook-dir",
      brainDir,
      "--no-input",
      "new",
      "--interactive",
      "--print-path",
      ...args,
    ];

    const proc = spawn("zk", fullArgs, {
      timeout,
      env: { ...process.env },
      cwd: brainDir,
    });

    let stdout = "";
    let stderr = "";

    proc.stdout.on("data", (data) => {
      stdout += data.toString();
    });
    proc.stderr.on("data", (data) => {
      stderr += data.toString();
    });
    proc.on("error", (error) =>
      reject(new Error(`Failed to spawn zk: ${error.message}`))
    );
    proc.on("close", (code) => resolve({ stdout, stderr, exitCode: code ?? 1 }));

    // Write content to stdin and close
    proc.stdin.write(content);
    proc.stdin.end();
  });
}

// =============================================================================
// ZK Utility Functions
// =============================================================================

/**
 * Extract the ID (filename stem) from a note path.
 * e.g., "global/plan/abc12def.md" -> "abc12def"
 */
export function extractIdFromPath(path: string): string {
  const filename = path.split("/").pop() || path;
  return filename.replace(/\.md$/, "");
}

/**
 * Generate an 8-character alphanumeric ID similar to what zk generates.
 * Used as fallback when zk is not available or cannot handle special characters.
 */
export function generateShortId(): string {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  let id = "";
  for (let i = 0; i < 8; i++) {
    id += chars[Math.floor(Math.random() * chars.length)];
  }
  return id;
}

/**
 * Generate a markdown link to a brain entry.
 * Format: [title](id) or [id](id) if no title
 * zk can resolve IDs to full paths for navigation
 */
export function generateMarkdownLink(id: string, title?: string): string {
  if (title) {
    return `[${title}](${id})`;
  }
  return `[${id}](${id})`;
}

/**
 * Check if the zk CLI is available on the system.
 */
export async function isZkAvailable(): Promise<boolean> {
  try {
    const result = await execZk(["--version"]);
    return result.exitCode === 0;
  } catch {
    return false;
  }
}

/**
 * Get the version of the zk CLI.
 */
export async function getZkVersion(): Promise<string | null> {
  try {
    const result = await execZk(["--version"]);
    if (result.exitCode !== 0) return null;
    const match = result.stdout.match(/zk\s+(\d+\.\d+\.\d+)/);
    return match ? match[1] : result.stdout.trim();
  } catch {
    return null;
  }
}

/**
 * Parse JSON output from zk commands.
 * Handles both array output and newline-delimited JSON.
 */
export function parseZkJsonOutput(output: string): ZkNote[] {
  const trimmed = output.trim();
  if (!trimmed) return [];

  try {
    const parsed = JSON.parse(trimmed);
    if (Array.isArray(parsed)) {
      return parsed.map((raw: Record<string, unknown>) => ({
        path: raw.path as string,
        title: raw.title as string,
        lead: (raw.lead as string) || undefined,
        body: (raw.body as string) || undefined,
        rawContent: (raw.rawContent as string) || undefined,
        wordCount: raw.wordCount as number | undefined,
        tags: (raw.tags as string[]) || [],
        metadata: (raw.metadata as Record<string, unknown>) || {},
        created: (raw.created as string) || undefined,
        modified: (raw.modified as string) || undefined,
      }));
    }
    return [parsed as ZkNote];
  } catch {
    // Try newline-delimited JSON
    const lines = trimmed.split("\n").filter(Boolean);
    const notes: ZkNote[] = [];
    for (const line of lines) {
      try {
        const raw = JSON.parse(line) as Record<string, unknown>;
        notes.push({
          path: raw.path as string,
          title: raw.title as string,
          tags: (raw.tags as string[]) || [],
          metadata: (raw.metadata as Record<string, unknown>) || {},
        });
      } catch {
        continue;
      }
    }
    return notes;
  }
}

/**
 * Check if the zk notebook directory exists.
 */
export function isZkNotebookExists(): boolean {
  const config = getConfig();
  return existsSync(config.brain.brainDir);
}

// =============================================================================
// Entry Extraction Functions
// =============================================================================

/**
 * Extract the entry type from a ZkNote.
 * Checks metadata.type first, then falls back to tags.
 */
export function extractType(note: ZkNote): EntryType {
  if (note.metadata?.type && typeof note.metadata.type === "string") {
    if (ENTRY_TYPES.includes(note.metadata.type as EntryType)) {
      return note.metadata.type as EntryType;
    }
  }
  for (const type of ENTRY_TYPES) {
    if (note.tags?.includes(type)) return type;
  }
  return "scratch";
}

/**
 * Extract the entry status from a ZkNote.
 * Checks metadata.status first, then falls back to tags.
 */
export function extractStatus(note: ZkNote): EntryStatus {
  if (note.metadata?.status && typeof note.metadata.status === "string") {
    if (ENTRY_STATUSES.includes(note.metadata.status as EntryStatus)) {
      return note.metadata.status as EntryStatus;
    }
  }
  for (const status of ENTRY_STATUSES) {
    if (note.tags?.includes(status)) return status;
  }
  return "active";
}

/**
 * Extract the priority from a ZkNote.
 * Checks metadata.priority first, then falls back to tags.
 */
export function extractPriority(note: ZkNote): Priority | undefined {
  if (note.metadata?.priority && typeof note.metadata.priority === "string") {
    const p = note.metadata.priority as Priority;
    if (["high", "medium", "low"].includes(p)) {
      return p;
    }
  }
  // Check tags as fallback
  for (const p of ["high", "medium", "low"] as Priority[]) {
    if (note.tags?.includes(p) || note.tags?.includes(`priority:${p}`))
      return p;
  }
  return undefined;
}

/**
 * Priority sort order: high=0, medium=1, low=2, undefined=3
 */
const PRIORITY_ORDER: Record<string, number> = { high: 0, medium: 1, low: 2 };

export function getPrioritySortValue(priority: Priority | undefined): number {
  return priority ? PRIORITY_ORDER[priority] : 3;
}

// =============================================================================
// Text Utilities
// =============================================================================

/**
 * Convert text to a URL-friendly slug.
 */
export function slugify(text: string): string {
  return text
    .toLowerCase()
    .trim()
    .replace(/\s+/g, "-")
    .replace(/[^a-z0-9-]/g, "")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 64);
}

/**
 * Check if a filename/ID matches a pattern.
 * Supports exact match or wildcard patterns with '*'.
 *
 * Examples:
 * - "abc12def" matches "abc12def" (exact)
 * - "abc*" matches "abc12def", "abcXYZ" (prefix)
 * - "*def" matches "abc12def", "XYZdef" (suffix)
 * - "abc*def" matches "abc12def", "abcXXXdef" (contains)
 *
 * @param filename The filename/ID to check (without .md extension)
 * @param pattern The pattern to match against
 * @returns true if the filename matches the pattern
 */
export function matchesFilenamePattern(
  filename: string,
  pattern: string
): boolean {
  // Remove .md extension if present in either
  const cleanFilename = filename.replace(/\.md$/, "");
  const cleanPattern = pattern.replace(/\.md$/, "");

  // Exact match (no wildcards)
  if (!cleanPattern.includes("*")) {
    return cleanFilename === cleanPattern;
  }

  // Convert glob pattern to regex
  // Escape regex special chars except *, then replace * with .*
  const regexPattern = cleanPattern
    .replace(/[.+?^${}()|[\]\\]/g, "\\$&") // Escape special regex chars
    .replace(/\*/g, ".*"); // Convert * to .*

  const regex = new RegExp(`^${regexPattern}$`, "i");
  return regex.test(cleanFilename);
}
