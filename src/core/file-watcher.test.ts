/**
 * Tests for file-watcher.ts
 *
 * Covers: FileWatcher lifecycle, file detection (create/modify/delete),
 * debouncing, ignore patterns, error handling, and path conversion.
 *
 * Uses real filesystem events via Bun's fs.watch with temp directories.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import {
  mkdtempSync,
  writeFileSync,
  mkdirSync,
  rmSync,
  unlinkSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { FileWatcher } from "./file-watcher";
import type { Indexer } from "./indexer";

// =============================================================================
// Test Helpers
// =============================================================================

let tempDir: string;

beforeEach(() => {
  tempDir = mkdtempSync(join(tmpdir(), "file-watcher-test-"));
});

afterEach(() => {
  rmSync(tempDir, { recursive: true, force: true });
});

/** Helper: write a file in the temp dir. */
function writeFile(relativePath: string, content: string): string {
  const fullPath = join(tempDir, relativePath);
  const dir = fullPath.substring(0, fullPath.lastIndexOf("/"));
  if (dir) {
    mkdirSync(dir, { recursive: true });
  }
  writeFileSync(fullPath, content, "utf-8");
  return relativePath;
}

/** Helper: wait for a specified number of milliseconds. */
function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Create a mock indexer that records calls. */
function createMockIndexer() {
  const calls: { method: string; path: string }[] = [];
  const errors: Error[] = [];

  const indexer = {
    indexFile: async (relativePath: string) => {
      calls.push({ method: "indexFile", path: relativePath });
      // If there's a pending error to throw, throw it
      if (errors.length > 0) {
        throw errors.shift()!;
      }
    },
    removeFile: async (relativePath: string) => {
      calls.push({ method: "removeFile", path: relativePath });
      if (errors.length > 0) {
        throw errors.shift()!;
      }
    },
    // Helper to queue an error for the next call
    _queueError: (err: Error) => {
      errors.push(err);
    },
  } as unknown as Indexer & { _queueError: (err: Error) => void };

  return { indexer, calls };
}

// =============================================================================
// Lifecycle
// =============================================================================

describe("FileWatcher lifecycle", () => {
  test("isRunning() returns false before start", () => {
    const { indexer } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer);

    expect(watcher.isRunning()).toBe(false);
  });

  test("isRunning() returns true after start", () => {
    const { indexer } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer);

    watcher.start();
    expect(watcher.isRunning()).toBe(true);

    watcher.stop();
  });

  test("isRunning() returns false after stop", () => {
    const { indexer } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer);

    watcher.start();
    watcher.stop();
    expect(watcher.isRunning()).toBe(false);
  });

  test("stop() is safe to call when not running", () => {
    const { indexer } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer);

    // Should not throw
    watcher.stop();
    expect(watcher.isRunning()).toBe(false);
  });

  test("start() is idempotent - calling twice doesn't create duplicate watchers", () => {
    const { indexer } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer);

    watcher.start();
    watcher.start(); // second call should be no-op
    expect(watcher.isRunning()).toBe(true);

    watcher.stop();
    expect(watcher.isRunning()).toBe(false);
  });
});

// =============================================================================
// File Detection
// =============================================================================

describe("FileWatcher file detection", () => {
  test("detects new .md files and calls indexFile", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    // Create a new markdown file
    writeFile("test-note.md", "---\ntitle: Test\n---\nBody.");

    // Wait for debounce + processing
    await wait(300);

    watcher.stop();

    // Should have called indexFile with relative path
    const indexCalls = calls.filter((c) => c.method === "indexFile");
    expect(indexCalls.length).toBeGreaterThanOrEqual(1);
    expect(indexCalls.some((c) => c.path === "test-note.md")).toBe(true);
  });

  test("detects modified .md files and calls indexFile", async () => {
    const { indexer, calls } = createMockIndexer();

    // Create file before starting watcher
    writeFile("existing.md", "---\ntitle: Original\n---\nOriginal body.");

    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });
    watcher.start();

    // Wait a bit for watcher to settle
    await wait(100);

    // Modify the file
    writeFile("existing.md", "---\ntitle: Modified\n---\nModified body.");

    // Wait for debounce + processing
    await wait(300);

    watcher.stop();

    // Should have called indexFile for the modification
    const indexCalls = calls.filter((c) => c.method === "indexFile");
    expect(indexCalls.length).toBeGreaterThanOrEqual(1);
    expect(indexCalls.some((c) => c.path === "existing.md")).toBe(true);
  });

  test("detects deleted .md files and calls removeFile", async () => {
    const { indexer, calls } = createMockIndexer();

    // Create file before starting watcher
    writeFile("to-delete.md", "---\ntitle: Delete Me\n---\nBody.");

    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });
    watcher.start();

    // Wait for watcher to settle
    await wait(100);

    // Delete the file
    unlinkSync(join(tempDir, "to-delete.md"));

    // Wait for debounce + processing
    await wait(300);

    watcher.stop();

    // Should have called removeFile with relative path
    const removeCalls = calls.filter((c) => c.method === "removeFile");
    expect(removeCalls.length).toBeGreaterThanOrEqual(1);
    expect(removeCalls.some((c) => c.path === "to-delete.md")).toBe(true);
  });

  test("detects .md files in subdirectories", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    // Create a file in a subdirectory
    writeFile("projects/test/note.md", "---\ntitle: Nested\n---\nBody.");

    // Wait for debounce + processing
    await wait(300);

    watcher.stop();

    const indexCalls = calls.filter((c) => c.method === "indexFile");
    expect(indexCalls.length).toBeGreaterThanOrEqual(1);
    expect(
      indexCalls.some((c) => c.path === "projects/test/note.md")
    ).toBe(true);
  });
});

// =============================================================================
// Ignore Patterns
// =============================================================================

describe("FileWatcher ignore patterns", () => {
  test("ignores non-.md files", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    // Create non-markdown files
    writeFile("readme.txt", "text file");
    writeFile("data.json", '{"key": "value"}');
    writeFile("script.ts", "console.log('hello')");

    await wait(300);

    watcher.stop();

    // Should NOT have called indexFile for non-.md files
    expect(calls).toHaveLength(0);
  });

  test("ignores .zk/ directory", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    // Create file in .zk directory
    writeFile(".zk/templates/default.md", "---\ntitle: Template\n---\nBody.");

    await wait(300);

    watcher.stop();

    // Should NOT have called indexFile for .zk/ files
    expect(calls).toHaveLength(0);
  });

  test("ignores node_modules/ directory", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    // Create file in node_modules
    writeFile("node_modules/pkg/README.md", "# Package");

    await wait(300);

    watcher.stop();

    // Should NOT have called indexFile for node_modules/ files
    expect(calls).toHaveLength(0);
  });

  test("ignores custom patterns from options", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, {
      debounceMs: 50,
      ignorePatterns: ["drafts/"],
    });

    watcher.start();

    // Create file in custom ignored directory
    writeFile("drafts/wip.md", "---\ntitle: WIP\n---\nBody.");

    // Also create a valid file
    writeFile("valid.md", "---\ntitle: Valid\n---\nBody.");

    await wait(300);

    watcher.stop();

    // Should only have indexed the valid file
    const indexCalls = calls.filter((c) => c.method === "indexFile");
    expect(indexCalls.some((c) => c.path === "valid.md")).toBe(true);
    expect(indexCalls.some((c) => c.path.startsWith("drafts/"))).toBe(false);
  });

  test("ignores temp files (starting with . or ending with ~)", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    // Create temp files
    writeFile(".hidden.md", "---\ntitle: Hidden\n---\nBody.");
    writeFile("backup.md~", "---\ntitle: Backup\n---\nBody.");

    await wait(300);

    watcher.stop();

    // Should NOT have called indexFile for temp files
    expect(calls).toHaveLength(0);
  });
});

// =============================================================================
// Debouncing
// =============================================================================

describe("FileWatcher debouncing", () => {
  test("batches rapid changes to same file into single indexFile call", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 200 });

    watcher.start();

    // Rapidly modify the same file multiple times
    writeFile("rapid.md", "---\ntitle: V1\n---\nBody v1.");
    await wait(20);
    writeFile("rapid.md", "---\ntitle: V2\n---\nBody v2.");
    await wait(20);
    writeFile("rapid.md", "---\ntitle: V3\n---\nBody v3.");

    // Wait for debounce to fire
    await wait(500);

    watcher.stop();

    // Should have batched into a single call (or at most 2 if timing is tight)
    const indexCalls = calls.filter(
      (c) => c.method === "indexFile" && c.path === "rapid.md"
    );
    // The key assertion: far fewer calls than the 3 writes
    expect(indexCalls.length).toBeLessThanOrEqual(2);
    expect(indexCalls.length).toBeGreaterThanOrEqual(1);
  });

  test("processes different files independently during debounce", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 100 });

    watcher.start();

    // Create two different files
    writeFile("file-a.md", "---\ntitle: A\n---\nBody A.");
    writeFile("file-b.md", "---\ntitle: B\n---\nBody B.");

    // Wait for debounce + processing
    await wait(400);

    watcher.stop();

    // Both files should have been indexed
    const indexCalls = calls.filter((c) => c.method === "indexFile");
    expect(indexCalls.some((c) => c.path === "file-a.md")).toBe(true);
    expect(indexCalls.some((c) => c.path === "file-b.md")).toBe(true);
  });
});

// =============================================================================
// Error Handling
// =============================================================================

describe("FileWatcher error handling", () => {
  test("continues watching after indexFile throws", async () => {
    const { indexer, calls } = createMockIndexer();

    // Queue an error for the first indexFile call
    indexer._queueError(new Error("Simulated index failure"));

    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });
    watcher.start();

    // Create first file (will trigger error)
    writeFile("error-file.md", "---\ntitle: Error\n---\nBody.");

    await wait(300);

    // Create second file (should still work)
    writeFile("ok-file.md", "---\ntitle: OK\n---\nBody.");

    await wait(300);

    watcher.stop();

    // Both files should have been attempted
    const indexCalls = calls.filter((c) => c.method === "indexFile");
    expect(indexCalls.some((c) => c.path === "error-file.md")).toBe(true);
    expect(indexCalls.some((c) => c.path === "ok-file.md")).toBe(true);

    // Watcher should still be functional (was running until we stopped it)
    expect(watcher.isRunning()).toBe(false);
  });
});

// =============================================================================
// Path Conversion
// =============================================================================

describe("FileWatcher path conversion", () => {
  test("converts absolute paths to relative paths for indexer", async () => {
    const { indexer, calls } = createMockIndexer();
    const watcher = new FileWatcher(tempDir, indexer, { debounceMs: 50 });

    watcher.start();

    writeFile("subdir/note.md", "---\ntitle: Nested\n---\nBody.");

    await wait(300);

    watcher.stop();

    // All paths passed to indexer should be relative (no leading /)
    for (const call of calls) {
      expect(call.path.startsWith("/")).toBe(false);
      expect(call.path.includes(tempDir)).toBe(false);
    }
  });
});
