/**
 * Brain Plugin - Shared Types
 *
 * Platform-agnostic types used across all plugin targets.
 */

// ============================================================================
// Entry Types
// ============================================================================

export type BrainEntryType =
  | "summary"
  | "report"
  | "walkthrough"
  | "plan"
  | "pattern"
  | "learning"
  | "idea"
  | "scratch"
  | "decision"
  | "exploration"
  | "execution"
  | "task";

export type BrainEntryStatus =
  | "draft"
  | "pending"
  | "active"
  | "in_progress"
  | "blocked"
  | "completed"
  | "validated"
  | "superseded"
  | "archived";

export type TaskPriority = "high" | "medium" | "low";

export const ENTRY_TYPES: BrainEntryType[] = [
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
];

export const ENTRY_STATUSES: BrainEntryStatus[] = [
  "draft",
  "pending",
  "active",
  "in_progress",
  "blocked",
  "completed",
  "validated",
  "superseded",
  "archived",
];

// ============================================================================
// API Response Types
// ============================================================================

export interface ApiError {
  error: string;
  message: string;
  details?: unknown;
}

export interface SaveResponse {
  id: string;
  path: string;
  title: string;
  type: string;
  status: string;
  link: string;
}

export interface RecallResponse {
  id: string;
  path: string;
  title: string;
  type: string;
  status: string;
  content: string;
  tags: string[];
  access_count?: number;
  backlinks?: Array<{ id: string; title: string; path: string }>;
}

export interface SearchResult {
  id: string;
  path: string;
  title: string;
  type: string;
  status: string;
  snippet: string;
}

export interface SearchResponse {
  results: SearchResult[];
  total: number;
}

export interface ListEntry {
  id: string;
  path: string;
  title: string;
  type: string;
  status: string;
  priority?: string;
  access_count?: number;
}

export interface ListResponse {
  entries: ListEntry[];
  total: number;
}

export interface InjectResponse {
  context: string;
  entries: Array<{
    id: string;
    path: string;
    title: string;
    type: string;
  }>;
}

export interface GraphEntry {
  id: string;
  path: string;
  title: string;
  type: string;
}

export interface GraphResponse {
  entries: GraphEntry[];
  total: number;
  message?: string;
}

export interface StaleEntry extends GraphEntry {
  daysSinceVerified: number | null;
}

export interface StaleResponse {
  entries: StaleEntry[];
  total: number;
}

export interface UpdateResponse {
  path: string;
  title: string;
  status: string;
  changes: string[];
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

export interface LinkResponse {
  link: string;
  id: string;
  path: string;
  title: string;
}

export interface SectionResponse {
  title: string;
  content: string;
  level: number;
  line: number;
}

export interface SectionsResponse {
  sections: Array<{
    title: string;
    level: number;
    line: number;
  }>;
  total: number;
}

// ============================================================================
// Execution Context
// ============================================================================

export interface ExecutionContext {
  projectId: string; // Human-readable, $HOME-relative
  workdir: string; // $HOME-relative path to main worktree
  worktree?: string; // Specific worktree path if in a worktree
  gitRemote?: string; // Git remote URL
  gitBranch?: string; // Current branch
}

// ============================================================================
// Installation Targets
// ============================================================================

export type InstallTarget = "opencode" | "claude-code" | "cursor" | "antigravity";

export interface InstallOptions {
  target: InstallTarget;
  force?: boolean;
  dryRun?: boolean;
  apiUrl?: string;
}

export interface InstallResult {
  success: boolean;
  target: InstallTarget;
  installedPath: string;
  message: string;
  backupPath?: string;
}
