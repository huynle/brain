/**
 * Brain API - Note Utilities
 *
 * Pure utility functions for working with brain entries/notes.
 * Frontmatter functions are re-exported from ./frontmatter.
 *
 * Provides ID generation, slugification, and frontmatter helpers
 * used by brain-service.ts and task-service.ts.
 */

import type {
  ZkNote,
  EntryType,
  EntryStatus,
  Priority,
} from "./types";
import { ENTRY_TYPES, ENTRY_STATUSES } from "./types";

// Re-export frontmatter functions
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
// ID and Link Utilities
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
 * Generate an 8-character alphanumeric ID.
 * Used for creating new brain entries.
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
 */
export function generateMarkdownLink(id: string, title?: string): string {
  if (title) {
    return `[${title}](${id})`;
  }
  return `[${id}](${id})`;
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
