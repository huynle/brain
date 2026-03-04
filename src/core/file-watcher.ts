/**
 * Brain API - File Watcher
 *
 * Watches brainDir recursively for markdown file changes and triggers
 * incremental indexing via the Indexer. Uses Bun's native fs.watch
 * with recursive: true (supported on macOS and Linux).
 *
 * Features:
 * - Debounces rapid changes to batch updates
 * - Ignores .zk/, node_modules/, temp files, and custom patterns
 * - Converts absolute paths to relative paths for the Indexer
 * - Logs errors but doesn't crash on individual file failures
 */

import { watch, existsSync, type FSWatcher } from "node:fs";
import { basename } from "node:path";
import type { Indexer } from "./indexer";

// =============================================================================
// Types
// =============================================================================

export interface FileWatcherOptions {
  debounceMs?: number;
  ignorePatterns?: string[];
}

/** Pending change action: index (create/modify) or remove (delete). */
type ChangeAction = "index" | "remove";

// =============================================================================
// Default ignore patterns
// =============================================================================

const DEFAULT_IGNORE_PATTERNS = [".zk/", "node_modules/"];

// =============================================================================
// FileWatcher Class
// =============================================================================

export class FileWatcher {
  private watcher: FSWatcher | null = null;
  private running = false;
  private debounceMs: number;
  private ignorePatterns: string[];

  /** Pending changes keyed by relative path. Debounce accumulates here. */
  private pendingChanges = new Map<string, ChangeAction>();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private brainDir: string,
    private indexer: Indexer,
    private options?: FileWatcherOptions
  ) {
    this.debounceMs = options?.debounceMs ?? 100;
    this.ignorePatterns = [
      ...DEFAULT_IGNORE_PATTERNS,
      ...(options?.ignorePatterns ?? []),
    ];
  }

  /**
   * Begin watching brainDir recursively for .md file changes.
   * Idempotent — calling start() when already running is a no-op.
   */
  start(): void {
    if (this.running) return;

    this.watcher = watch(
      this.brainDir,
      { recursive: true },
      (eventType, filename) => {
        if (!filename) return;
        this.handleEvent(eventType, filename);
      }
    );

    this.running = true;
  }

  /**
   * Stop watching. Safe to call when not running.
   * Flushes any pending debounced changes.
   */
  stop(): void {
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }

    if (this.watcher) {
      this.watcher.close();
      this.watcher = null;
    }

    this.pendingChanges.clear();
    this.running = false;
  }

  /** Returns true if the watcher is currently active. */
  isRunning(): boolean {
    return this.running;
  }

  // ===========================================================================
  // Internal Methods
  // ===========================================================================

  /**
   * Handle a raw fs.watch event.
   * Filters by extension and ignore patterns, then queues the change.
   */
  private handleEvent(eventType: string, filename: string): void {
    // Normalize path separators (Windows compat, though we target macOS/Linux)
    const relativePath = filename.replace(/\\/g, "/");

    // Filter: only .md files
    if (!relativePath.endsWith(".md")) return;

    // Filter: ignore patterns
    if (this.shouldIgnore(relativePath)) return;

    // Filter: temp files (dotfiles or backup files ending with ~)
    const name = basename(relativePath);
    if (name.startsWith(".") || name.endsWith("~")) return;

    // Determine action: if file exists on disk, index it; otherwise remove it
    const fullPath = `${this.brainDir}/${relativePath}`;
    const action: ChangeAction = existsSync(fullPath) ? "index" : "remove";

    // Queue the change
    this.pendingChanges.set(relativePath, action);
    this.scheduleDebouncedFlush();
  }

  /** Check if a relative path matches any ignore pattern. */
  private shouldIgnore(relativePath: string): boolean {
    for (const pattern of this.ignorePatterns) {
      if (relativePath.startsWith(pattern) || relativePath.includes(`/${pattern}`)) {
        return true;
      }
    }
    return false;
  }

  /** Schedule a debounced flush of pending changes. */
  private scheduleDebouncedFlush(): void {
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
    }

    this.debounceTimer = setTimeout(() => {
      this.flushPendingChanges();
    }, this.debounceMs);
  }

  /** Process all pending changes. */
  private async flushPendingChanges(): Promise<void> {
    // Snapshot and clear pending changes
    const changes = new Map(this.pendingChanges);
    this.pendingChanges.clear();
    this.debounceTimer = null;

    for (const [relativePath, action] of changes) {
      try {
        if (action === "index") {
          await this.indexer.indexFile(relativePath);
        } else {
          await this.indexer.removeFile(relativePath);
        }
      } catch (err) {
        // Log error but don't crash — continue processing other files
        console.error(
          `[FileWatcher] Error processing ${action} for "${relativePath}":`,
          err instanceof Error ? err.message : String(err)
        );
      }
    }
  }
}
