/**
 * Brain API - Core Types
 *
 * Type definitions extracted from the OpenCode brain plugin
 */

// =============================================================================
// Entry Types
// =============================================================================

export const ENTRY_TYPES = [
  "summary",
  "report",
  "walkthrough",
  "plan",
  "pattern",
  "learning",
  "idea",
  "scratch",
  "decision",
  "exploration",
  "execution",
  "task",
] as const;

export type EntryType = (typeof ENTRY_TYPES)[number];

// =============================================================================
// Entry Statuses
// =============================================================================

export const ENTRY_STATUSES = [
  "draft", // Initial state, not ready
  "pending", // Queued, waiting to be worked on
  "active", // Ready/in use (default)
  "in_progress", // Actively being worked on
  "blocked", // Waiting on something
  "completed", // Done/implemented
  "validated", // Implementation verified working
  "superseded", // Replaced by another entry
  "archived", // No longer relevant
] as const;

export type EntryStatus = (typeof ENTRY_STATUSES)[number];

// =============================================================================
// Priority
// =============================================================================

export const PRIORITIES = ["high", "medium", "low"] as const;
export type Priority = (typeof PRIORITIES)[number];

// =============================================================================
// ZK Note (from zk CLI output)
// =============================================================================

export interface ZkNote {
  path: string;
  title: string;
  lead?: string;
  body?: string;
  rawContent?: string;
  wordCount?: number;
  tags?: string[];
  metadata?: Record<string, unknown>;
  created?: string;
  modified?: string;
}

// =============================================================================
// Entry Metadata (SQLite)
// =============================================================================

export interface EntryMeta {
  path: string;
  project_id: string;
  access_count: number;
  accessed_at: number | null;
  last_verified: number | null;
  created_at: number;
}

// =============================================================================
// Brain Entry (Full)
// =============================================================================

export interface BrainEntry {
  id: string; // 8-char alphanumeric
  path: string; // Full path in brain
  title: string;
  type: EntryType;
  status: EntryStatus;
  content: string; // Markdown body
  tags: string[];
  priority?: Priority;
  depends_on?: string[];
  parent_id?: string; // 8-char ID of parent entry for hierarchical grouping
  project_id?: string;
  created?: string; // ISO timestamp
  modified?: string; // ISO timestamp
  access_count?: number;
  last_verified?: string;

  // Execution context for tasks
  workdir?: string; // $HOME-relative path to main worktree
  worktree?: string; // Specific worktree if different from main
  git_remote?: string; // Git remote URL for verification
  git_branch?: string; // Branch context when task was created
}

// =============================================================================
// API Request/Response Types
// =============================================================================

export interface CreateEntryRequest {
  type: EntryType;
  title: string;
  content: string;
  tags?: string[];
  status?: EntryStatus;
  priority?: Priority;
  depends_on?: string[];
  parent_id?: string; // 8-char ID of parent entry for hierarchical grouping
  global?: boolean;
  project?: string;
  relatedEntries?: string[];

  // Execution context for tasks
  workdir?: string;
  worktree?: string;
  git_remote?: string;
  git_branch?: string;
}

export interface CreateEntryResponse {
  id: string;
  path: string;
  title: string;
  type: EntryType;
  status: EntryStatus;
  link: string;
}

export interface UpdateEntryRequest {
  status?: EntryStatus;
  title?: string;
  append?: string;
  note?: string;
}

export interface ListEntriesRequest {
  type?: EntryType;
  status?: EntryStatus;
  filename?: string;
  parent_id?: string; // Filter by parent entry ID
  limit?: number;
  offset?: number;
  global?: boolean;
  sortBy?: "created" | "modified" | "priority";
}

export interface ListEntriesResponse {
  entries: BrainEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface SearchRequest {
  query: string;
  type?: EntryType;
  status?: EntryStatus;
  limit?: number;
  global?: boolean;
}

export interface SearchResponse {
  results: BrainEntry[];
  total: number;
}

export interface InjectRequest {
  query: string;
  maxEntries?: number;
  type?: EntryType;
}

export interface InjectResponse {
  context: string;
  entries: BrainEntry[];
}

export interface LinkRequest {
  title?: string;
  path?: string;
  withTitle?: boolean;
}

export interface LinkResponse {
  link: string;
  id: string;
  path: string;
  title: string;
}

export interface StatsResponse {
  zkAvailable: boolean;
  zkVersion: string | null;
  notebookExists: boolean;
  brainDir: string;
  dbPath: string;
  totalEntries: number;
  globalEntries: number;
  projectEntries: number;
  byType: Record<string, number>;
  orphanCount: number;
  trackedEntries: number;
  staleCount: number;
}

export interface HealthResponse {
  status: "healthy" | "degraded" | "unhealthy";
  zkAvailable: boolean;
  dbAvailable: boolean;
  timestamp: string;
}

// =============================================================================
// Configuration
// =============================================================================

export interface BrainConfig {
  brainDir: string;
  dbPath: string;
  defaultProject: string;
}

export interface ServerConfig {
  port: number;
  host: string;
  logLevel: "debug" | "info" | "warn" | "error";
  enableAuth: boolean;
  apiKey?: string;
  enableTenants: boolean;
}

export interface Config {
  brain: BrainConfig;
  server: ServerConfig;
}
