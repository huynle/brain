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
// Input Sanitization Functions
// =============================================================================

/**
 * Normalize a title for user-friendly display.
 * - Strips control characters (except spaces)
 * - Replaces newlines/carriage returns/tabs with spaces
 * - Collapses multiple spaces to single space
 * - Trims whitespace
 * - Truncates to 200 characters
 * 
 * This version is returned to clients.
 */
export function normalizeTitle(title: string): string {
  // Strip control characters (except spaces), including \n, \r, \t, \0
  let result = title.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, "");
  // Replace newlines, carriage returns, and tabs with spaces
  result = result.replace(/[\n\r\t]/g, " ");
  // Trim leading/trailing whitespace
  result = result.trim();
  // Collapse internal runs of whitespace to single space
  result = result.replace(/\s+/g, " ");
  // Truncate to 200 chars
  return result.slice(0, 200);
}

/**
 * Sanitize a title for safe use in YAML frontmatter (double-quoted string).
 * - First normalizes the title (see normalizeTitle)
 * - Then escapes backslashes and double quotes for YAML
 * 
 * This version is written to files.
 */
export function sanitizeTitle(title: string): string {
  const normalized = normalizeTitle(title);
  // Escape backslashes and double quotes (templates wrap title in double quotes)
  return normalized.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

/**
 * Sanitize a tag for safe use in YAML frontmatter.
 * - Strips control characters (newlines, carriage returns, tabs, null bytes)
 * - Trims whitespace
 * - Returns null for empty tags or tags containing colons (which would break YAML)
 */
export function sanitizeTag(tag: string): string | null {
  // Strip control characters including \n, \r, \t, \0
  let result = tag.replace(/[\x00-\x1F\x7F]/g, "");
  // Trim whitespace
  result = result.trim();
  // Return null for empty tags
  if (result.length === 0) return null;
  // Return null for tags with colons (would be parsed as YAML key-value pairs)
  if (result.includes(":")) return null;
  return result;
}

/**
 * Sanitize a simple value (workdir, git_remote, git_branch, projectId).
 * - Strips control characters (newlines, carriage returns, null bytes)
 * - Replaces newlines with spaces
 * - Collapses multiple spaces to single space
 * - Trims whitespace
 */
export function sanitizeSimpleValue(value: string): string {
  // Strip null bytes
  let result = value.replace(/\0/g, "");
  // Replace newlines and carriage returns with spaces
  result = result.replace(/[\n\r]/g, " ");
  // Collapse multiple spaces to single space
  result = result.replace(/\s+/g, " ");
  // Trim whitespace
  return result.trim();
}

/**
 * Sanitize a depends_on entry.
 * - Strips control characters (newlines, carriage returns, null bytes)
 * - Trims whitespace
 * Note: Quote escaping happens at format time, not here
 */
export function sanitizeDependsOnEntry(dep: string): string {
  // Strip control characters including \n, \r, \0
  let result = dep.replace(/[\x00-\x1F\x7F]/g, "");
  // Trim whitespace
  return result.trim();
}

// =============================================================================
// Frontmatter Functions
// =============================================================================

/**
 * Parse a YAML value that may be quoted (with possible escaped characters inside).
 * Returns the unescaped value.
 */
function parseYamlStringValue(rawValue: string): string {
  const trimmed = rawValue.trim();

  // Double-quoted string: handle escape sequences
  if (trimmed.startsWith('"')) {
    // Match the full double-quoted string, accounting for escaped quotes
    const match = trimmed.match(/^"((?:[^"\\]|\\.)*)"\s*$/);
    if (match) {
      return unescapeYamlValue(`"${match[1]}"`);
    }
  }

  // Single-quoted string: no escape sequences in YAML single quotes (except '' for ')
  if (trimmed.startsWith("'")) {
    const match = trimmed.match(/^'((?:[^']|'')*)'\s*$/);
    if (match) {
      return match[1].replace(/''/g, "'");
    }
  }

  // Unquoted value
  return trimmed;
}

/**
 * Parse YAML frontmatter from markdown content.
 * Returns the frontmatter object and the body content.
 */
export function parseFrontmatter(content: string): {
  frontmatter: Record<string, unknown>;
  body: string;
} {
  const match = content.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!match) return { frontmatter: {}, body: content };

  const yaml = match[1];
  const body = match[2].trim();
  const frontmatter: Record<string, unknown> = {};
  const tags: string[] = [];
  const dependsOn: string[] = [];
  const featureDependsOn: string[] = [];
  let inTags = false;
  let inDependsOn = false;
  let inFeatureDependsOn = false;

  for (const line of yaml.split("\n")) {
    // Title: may contain special characters, quotes, etc.
    const titleMatch = line.match(/^title:\s*(.+)$/);
    if (titleMatch) {
      frontmatter.title = parseYamlStringValue(titleMatch[1]);
      inTags = false;
      continue;
    }

    const typeMatch = line.match(/^type:\s*(\w+)\s*$/);
    if (typeMatch) {
      frontmatter.type = typeMatch[1];
      inTags = false;
      continue;
    }

    const statusMatch = line.match(/^status:\s*(\w+)\s*$/);
    if (statusMatch) {
      frontmatter.status = statusMatch[1];
      inTags = false;
      continue;
    }

    // ProjectId: may be quoted
    const projectMatch = line.match(/^projectId:\s*(.+)$/);
    if (projectMatch) {
      frontmatter.projectId = parseYamlStringValue(projectMatch[1]);
      inTags = false;
      continue;
    }

    // Extract name field (used for execution entries) - may be quoted
    const nameMatch = line.match(/^name:\s*(.+)$/);
    if (nameMatch) {
      frontmatter.name = parseYamlStringValue(nameMatch[1]);
      inTags = false;
      continue;
    }

    // Handle priority
    const priorityMatch = line.match(/^priority:\s*(\w+)\s*$/);
    if (priorityMatch) {
      frontmatter.priority = priorityMatch[1];
      inTags = false;
      inFeatureDependsOn = false;
      continue;
    }

    // Handle feature_id
    const featureIdMatch = line.match(/^feature_id:\s*(.+)$/);
    if (featureIdMatch) {
      frontmatter.feature_id = parseYamlStringValue(featureIdMatch[1]);
      inTags = false;
      inDependsOn = false;
      inFeatureDependsOn = false;
      continue;
    }

    // Handle feature_priority
    const featurePriorityMatch = line.match(/^feature_priority:\s*(\w+)\s*$/);
    if (featurePriorityMatch) {
      frontmatter.feature_priority = featurePriorityMatch[1];
      inTags = false;
      inDependsOn = false;
      inFeatureDependsOn = false;
      continue;
    }

    // Handle created timestamp
    const createdMatch = line.match(/^created:\s*(.+)$/);
    if (createdMatch) {
      frontmatter.created = parseYamlStringValue(createdMatch[1]);
      inTags = false;
      continue;
    }

    // Handle parent_id
    const parentIdMatch = line.match(/^parent_id:\s*(.+)$/);
    if (parentIdMatch) {
      frontmatter.parent_id = parseYamlStringValue(parentIdMatch[1]);
      inTags = false;
      continue;
    }

    // Handle execution context fields for tasks
    const workdirMatch = line.match(/^workdir:\s*(.+)$/);
    if (workdirMatch) {
      frontmatter.workdir = parseYamlStringValue(workdirMatch[1]);
      inTags = false;
      continue;
    }

    const gitRemoteMatch = line.match(/^git_remote:\s*(.+)$/);
    if (gitRemoteMatch) {
      frontmatter.git_remote = parseYamlStringValue(gitRemoteMatch[1]);
      inTags = false;
      continue;
    }

    const gitBranchMatch = line.match(/^git_branch:\s*(.+)$/);
    if (gitBranchMatch) {
      frontmatter.git_branch = parseYamlStringValue(gitBranchMatch[1]);
      inTags = false;
      continue;
    }

    // Handle user_original_request - can be single-line or multi-line (literal block scalar)
    // Single-line: user_original_request: simple request
    // Multi-line: user_original_request: |
    //               line 1
    //               line 2
    const userRequestSingleMatch = line.match(/^user_original_request:\s*(.+)$/);
    if (userRequestSingleMatch && userRequestSingleMatch[1].trim() !== "|") {
      frontmatter.user_original_request = parseYamlStringValue(userRequestSingleMatch[1]);
      inTags = false;
      continue;
    }
    const userRequestBlockMatch = line.match(/^user_original_request:\s*\|\s*$/);
    if (userRequestBlockMatch) {
      // Start collecting multi-line content
      inTags = false;
      // Mark that we're in user_original_request block mode
      // We'll handle this with a dedicated parser below
      continue;
    }

    // Handle tags array
    if (line.match(/^tags:\s*$/)) {
      inTags = true;
      inDependsOn = false;
      continue;
    }

    // Handle depends_on array
    if (line.match(/^depends_on:\s*$/)) {
      inDependsOn = true;
      inTags = false;
      inFeatureDependsOn = false;
      continue;
    }

    // Handle feature_depends_on array
    if (line.match(/^feature_depends_on:\s*$/)) {
      inFeatureDependsOn = true;
      inTags = false;
      inDependsOn = false;
      continue;
    }

    if (inTags) {
      const tagMatch = line.match(/^\s+-\s+(.+?)\s*$/);
      if (tagMatch) {
        tags.push(parseYamlStringValue(tagMatch[1]));
      } else if (!line.match(/^\s/)) {
        // No longer indented, exit tags section
        inTags = false;
      }
    }

    if (inDependsOn) {
      const depMatch = line.match(/^\s+-\s+(.+?)\s*$/);
      if (depMatch) {
        dependsOn.push(parseYamlStringValue(depMatch[1]));
      } else if (!line.match(/^\s/)) {
        // No longer indented, exit depends_on section
        inDependsOn = false;
      }
    }

    if (inFeatureDependsOn) {
      const depMatch = line.match(/^\s+-\s+(.+?)\s*$/);
      if (depMatch) {
        featureDependsOn.push(parseYamlStringValue(depMatch[1]));
      } else if (!line.match(/^\s/)) {
        // No longer indented, exit feature_depends_on section
        inFeatureDependsOn = false;
      }
    }
  }

  if (tags.length > 0) {
    frontmatter.tags = tags;
  }

  if (dependsOn.length > 0) {
    frontmatter.depends_on = dependsOn;
  }

  if (featureDependsOn.length > 0) {
    frontmatter.feature_depends_on = featureDependsOn;
  }

  // Handle multi-line user_original_request (literal block scalar)
  // This needs special handling because it spans multiple lines
  // The block continues while lines are either empty OR indented with 2+ spaces
  // It stops at non-empty, non-indented lines (like --- or other YAML keys)
  const userRequestBlockStart = yaml.indexOf("user_original_request: |");
  if (userRequestBlockStart !== -1) {
    const afterHeader = yaml.slice(userRequestBlockStart + "user_original_request: |".length);
    const lines = afterHeader.split("\n").slice(1); // Skip the line with just "|"
    
    const blockLines: string[] = [];
    for (const line of lines) {
      // Stop at non-empty lines that don't start with spaces (like --- or other keys)
      if (line.length > 0 && !line.startsWith("  ") && !line.startsWith("\t")) {
        break;
      }
      blockLines.push(line);
    }
    
    // Find the last non-empty line to trim trailing empty lines
    let lastContentIdx = blockLines.length - 1;
    while (lastContentIdx >= 0 && blockLines[lastContentIdx].trim() === "") {
      lastContentIdx--;
    }
    const contentLines = blockLines.slice(0, lastContentIdx + 1);
    
    // Remove the 2-space indent from each line
    // Empty lines in literal blocks are preserved (they just don't have indent)
    const unindentedLines = contentLines.map((l) => 
      l.startsWith("  ") ? l.slice(2) : l
    );
    frontmatter.user_original_request = unindentedLines.join("\n");
  }

  return { frontmatter, body };
}

/**
 * Escape a string for safe use in YAML frontmatter.
 * Wraps in quotes if the string contains special YAML characters.
 */
export function escapeYamlValue(value: string): string {
  // Characters that require quoting in YAML (including control characters)
  const needsQuoting =
    /[\n\r\t]|[:\#\[\]\{\}\|\>\<\!\&\*\?\`\'\"\,\@\%]|^\s|\s$|^---|^\.\.\./.test(value);

  if (!needsQuoting) {
    return value;
  }

  // Use double quotes and escape internal double quotes, backslashes, and control chars
  const escaped = value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r")
    .replace(/\t/g, "\\t");

  return `"${escaped}"`;
}

/**
 * Format a value for YAML frontmatter, using literal block scalar (|) for multiline content.
 * This preserves newlines, special characters, code blocks, etc. verbatim.
 *
 * For single-line content without problematic characters, returns simple key: value
 * For content with newlines or special YAML characters, uses literal block scalar:
 *   key: |
 *     line 1
 *     line 2
 *
 * @param key - The YAML key name
 * @param value - The value to format (can be multiline)
 * @returns Formatted YAML string
 */
export function formatYamlMultilineValue(key: string, value: string): string {
  const hasNewlines = value.includes("\n");
  // Characters that would be problematic in YAML without quoting
  const hasSpecialChars =
    /[:\#\[\]\{\}\|\>\<\!\&\*\?\`\'\"\,\@\%]|^\s|\s$|^---|^\.\.\./.test(value);

  // For simple single-line content without special chars, use plain format
  if (!hasNewlines && !hasSpecialChars) {
    return `${key}: ${value}`;
  }

  // For multiline content or content with special chars, use literal block scalar
  // The '|' indicator preserves newlines exactly as written
  // We indent each line with 2 spaces as per YAML block scalar rules
  const indentedLines = value
    .split("\n")
    .map((line) => `  ${line}`)
    .join("\n");

  return `${key}: |\n${indentedLines}`;
}

/**
 * Unescape a YAML string value that was previously escaped.
 * Handles both double-quoted and single-quoted strings.
 */
export function unescapeYamlValue(value: string): string {
  // Remove surrounding quotes if present
  let unquoted = value;
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  ) {
    unquoted = value.slice(1, -1);
  }

  // Unescape common YAML escape sequences
  // Process backslash escapes in a single pass to handle them correctly
  let result = "";
  let i = 0;
  while (i < unquoted.length) {
    if (unquoted[i] === "\\" && i + 1 < unquoted.length) {
      const next = unquoted[i + 1];
      switch (next) {
        case '"':
          result += '"';
          i += 2;
          break;
        case "'":
          result += "'";
          i += 2;
          break;
        case "\\":
          result += "\\";
          i += 2;
          break;
        case "n":
          result += "\n";
          i += 2;
          break;
        case "t":
          result += "\t";
          i += 2;
          break;
        case "r":
          result += "\r";
          i += 2;
          break;
        default:
          result += unquoted[i];
          i += 1;
          break;
      }
    } else {
      result += unquoted[i];
      i += 1;
    }
  }
  return result;
}

/**
 * Serialize a frontmatter object back to YAML string.
 * Handles all known fields with proper escaping.
 * Used by update() to preserve all fields when modifying entries.
 */
export function serializeFrontmatter(fm: Record<string, unknown>): string {
  const lines: string[] = [];

  // Emit fields in canonical order, escaping each appropriately
  if (fm.title) lines.push(`title: ${escapeYamlValue(fm.title as string)}`);
  if (fm.type) lines.push(`type: ${fm.type}`);

  // Include name field for execution entries
  if (fm.name) lines.push(`name: ${escapeYamlValue(fm.name as string)}`);

  // Handle tags array
  if (Array.isArray(fm.tags) && fm.tags.length > 0) {
    lines.push("tags:");
    for (const tag of fm.tags) {
      lines.push(`  - ${escapeYamlValue(String(tag))}`);
    }
  }

  if (fm.status) lines.push(`status: ${fm.status}`);
  if (fm.created) lines.push(`created: ${fm.created}`);
  if (fm.priority) lines.push(`priority: ${fm.priority}`);
  if (fm.parent_id) lines.push(`parent_id: ${fm.parent_id}`);
  if (fm.projectId) lines.push(`projectId: ${escapeYamlValue(fm.projectId as string)}`);

  // depends_on array
  if (Array.isArray(fm.depends_on) && fm.depends_on.length > 0) {
    lines.push("depends_on:");
    for (const dep of fm.depends_on) {
      const escaped = String(dep).replace(/\\/g, "\\\\").replace(/"/g, '\\"');
      lines.push(`  - "${escaped}"`);
    }
  }

  // Feature group fields
  if (fm.feature_id) lines.push(`feature_id: ${escapeYamlValue(fm.feature_id as string)}`);
  if (fm.feature_priority) lines.push(`feature_priority: ${fm.feature_priority}`);

  // feature_depends_on array
  if (Array.isArray(fm.feature_depends_on) && fm.feature_depends_on.length > 0) {
    lines.push("feature_depends_on:");
    for (const dep of fm.feature_depends_on) {
      const escaped = String(dep).replace(/\\/g, "\\\\").replace(/"/g, '\\"');
      lines.push(`  - "${escaped}"`);
    }
  }

  // Execution context fields
  if (fm.workdir) lines.push(`workdir: ${escapeYamlValue(fm.workdir as string)}`);
  if (fm.git_remote) lines.push(`git_remote: ${escapeYamlValue(fm.git_remote as string)}`);
  if (fm.git_branch) lines.push(`git_branch: ${escapeYamlValue(fm.git_branch as string)}`);
  if (fm.target_workdir) lines.push(`target_workdir: ${escapeYamlValue(fm.target_workdir as string)}`);

  // User original request - use multiline format if contains newlines
  if (fm.user_original_request) {
    lines.push(formatYamlMultilineValue("user_original_request", fm.user_original_request as string));
  }

  return lines.join("\n") + "\n";
}

export interface GenerateFrontmatterOptions {
  title: string;
  type: EntryType;
  tags?: string[];
  status?: EntryStatus;
  projectId?: string;
  name?: string;
  priority?: Priority;
  // Task dependencies (normalized to short IDs)
  depends_on?: string[];
  // Feature group fields for task grouping
  feature_id?: string;
  feature_priority?: Priority;
  feature_depends_on?: string[];
  // Execution context for tasks
  workdir?: string;
  git_remote?: string;
  git_branch?: string;
  target_workdir?: string;
  // User intent for validation
  user_original_request?: string;
}

/**
 * Generate YAML frontmatter for a brain entry.
 */
export function generateFrontmatter(options: GenerateFrontmatterOptions): string {
  const status = options.status || "active";
  const tags = new Set<string>(options.tags || []);
  // Add type to tags (useful for zk --tag filtering by type)
  tags.add(options.type);
  // Note: status is NOT added to tags - it's only in status: field
  // Status filtering is done in code, not via zk --tag

  const lines: string[] = [];
  lines.push(`title: ${escapeYamlValue(options.title)}`);
  lines.push(`type: ${options.type}`);

  // Include name field for execution entries
  if (options.name) {
    lines.push(`name: ${escapeYamlValue(options.name)}`);
  }

  if (tags.size > 0) {
    lines.push("tags:");
    for (const tag of tags) lines.push(`  - ${escapeYamlValue(tag)}`);
  }

  lines.push(`status: ${status}`);

  if (options.priority) {
    lines.push(`priority: ${options.priority}`);
  }

  // Task dependencies
  if (options.depends_on && options.depends_on.length > 0) {
    lines.push("depends_on:");
    for (const dep of options.depends_on) {
      const escaped = String(dep).replace(/\\/g, "\\\\").replace(/"/g, '\\"');
      lines.push(`  - "${escaped}"`);
    }
  }

  if (options.projectId) {
    lines.push(`projectId: ${escapeYamlValue(options.projectId)}`);
  }

  // Feature group fields
  if (options.feature_id) {
    lines.push(`feature_id: ${escapeYamlValue(options.feature_id)}`);
  }
  if (options.feature_priority) {
    lines.push(`feature_priority: ${options.feature_priority}`);
  }
  if (options.feature_depends_on && options.feature_depends_on.length > 0) {
    lines.push("feature_depends_on:");
    for (const dep of options.feature_depends_on) {
      const escaped = String(dep).replace(/\\/g, "\\\\").replace(/"/g, '\\"');
      lines.push(`  - "${escaped}"`);
    }
  }

  // Execution context for tasks
  if (options.workdir) {
    lines.push(`workdir: ${escapeYamlValue(options.workdir)}`);
  }
  if (options.git_remote) {
    lines.push(`git_remote: ${escapeYamlValue(options.git_remote)}`);
  }
  if (options.git_branch) {
    lines.push(`git_branch: ${escapeYamlValue(options.git_branch)}`);
  }
  if (options.target_workdir) {
    lines.push(`target_workdir: ${escapeYamlValue(options.target_workdir)}`);
  }

  // User intent for validation - use YAML literal block scalar for multiline
  if (options.user_original_request) {
    lines.push(formatYamlMultilineValue("user_original_request", options.user_original_request));
  }

  return lines.join("\n") + "\n";
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
