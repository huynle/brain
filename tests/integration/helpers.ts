/**
 * Integration Test Helpers
 *
 * Shared utilities for integration tests against the SQLite storage backend.
 * Provides factory functions for creating test storage instances, test data,
 * and temporary brain directories for file-based tests.
 */

import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "fs";
import { join, dirname } from "path";
import { tmpdir } from "os";
import {
  createStorageLayer,
  StorageLayer,
  type NoteRow,
  type LinkRow,
  type TagRow,
  type SearchOptions,
  type ListOptions,
  type EntryMetaRow,
  type StorageStats,
} from "../../src/core/storage";

// =============================================================================
// Re-exports for test convenience
// =============================================================================

export {
  StorageLayer,
  createStorageLayer,
  type NoteRow,
  type LinkRow,
  type TagRow,
  type SearchOptions,
  type ListOptions,
  type EntryMetaRow,
  type StorageStats,
};

// =============================================================================
// Storage Helpers
// =============================================================================

/**
 * Create an in-memory StorageLayer with schema applied.
 * Caller is responsible for calling `.close()` in afterEach.
 */
export function createTestStorage(): StorageLayer {
  return createStorageLayer(":memory:");
}

// =============================================================================
// Note Factory
// =============================================================================

/**
 * Factory for NoteRow objects with sensible defaults.
 * Follows the same pattern as makeTestNote in storage.test.ts.
 *
 * All fields have defaults; pass overrides to customize specific fields.
 * Returns a NoteRow without `id` or `indexed_at` (those are DB-assigned).
 */
export function makeTestNote(
  overrides: Partial<NoteRow> = {}
): Omit<NoteRow, "id" | "indexed_at"> {
  return {
    path: "projects/test/task/abc12def.md",
    short_id: "abc12def",
    title: "Test Note",
    lead: "A test note",
    body: "This is the body content",
    raw_content: "---\ntitle: Test Note\n---\nThis is the body content",
    word_count: 6,
    checksum: "abc123",
    metadata: JSON.stringify({ title: "Test Note" }),
    type: "task",
    status: "active",
    priority: "medium",
    project_id: "test",
    feature_id: null,
    created: "2024-01-01T00:00:00Z",
    modified: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

// =============================================================================
// Temp Directory Helpers
// =============================================================================

/**
 * Create a temporary directory for file-based tests.
 * Returns the absolute path to the created directory.
 * Caller must call `cleanupTempDir(path)` in afterEach.
 */
export function createTempBrainDir(): string {
  return mkdtempSync(join(tmpdir(), "brain-integration-test-"));
}

/**
 * Remove a temporary directory and all its contents.
 * Safe to call even if the directory doesn't exist.
 */
export function cleanupTempDir(dirPath: string): void {
  try {
    rmSync(dirPath, { recursive: true, force: true });
  } catch {
    // Ignore errors — directory may already be cleaned up
  }
}

// =============================================================================
// Markdown File Helpers
// =============================================================================

/**
 * Write a markdown file with YAML frontmatter to a brain directory.
 *
 * @param brainDir - Absolute path to the brain directory root
 * @param relativePath - Relative path within the brain dir (e.g., "projects/alpha/task/abc12def.md")
 * @param frontmatter - Object of frontmatter key-value pairs
 * @param body - Markdown body content (without frontmatter delimiters)
 */
export function writeTestMarkdownFile(
  brainDir: string,
  relativePath: string,
  frontmatter: Record<string, unknown>,
  body: string
): void {
  const fullPath = join(brainDir, relativePath);
  mkdirSync(dirname(fullPath), { recursive: true });

  // Build YAML frontmatter
  const yamlLines: string[] = [];
  for (const [key, value] of Object.entries(frontmatter)) {
    if (Array.isArray(value)) {
      yamlLines.push(`${key}:`);
      for (const item of value) {
        yamlLines.push(`  - ${item}`);
      }
    } else if (value === null || value === undefined) {
      // Skip null/undefined values
      continue;
    } else {
      yamlLines.push(`${key}: ${value}`);
    }
  }

  const content = `---\n${yamlLines.join("\n")}\n---\n\n${body}\n`;
  writeFileSync(fullPath, content);
}
