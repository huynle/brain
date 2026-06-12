// @ts-nocheck - This file is installed to OpenCode, not compiled by brain-api
/**
 * Brain API Client Plugin for OpenCode
 *
 * A thin API client that calls the Brain API instead of direct database access
 * and SQLite access. This is a drop-in replacement for the original brain.ts
 * plugin with identical tool interfaces.
 *
 * Configuration (reads from config file first, env vars override):
 *
 * Config file: ~/.config/brain/config.yaml
 *   mcp:
 *       api_url: https://brain.huynle.com      # Brain API URL
 *   runner:
 *       brain_api_url: https://brain.huynle.com # Fallback if mcp.api_url not set
 *       api_token: <token>                      # Auth token for protected endpoints
 *
 * Environment variables (override config file):
 * - BRAIN_API_URL: Base URL for the Brain API (default: http://localhost:3333)
 * - BRAIN_API_TOKEN: Auth token for protected Brain API endpoints
 *
 * ============================================================================
 * AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY
 * ============================================================================
 * This file was installed by: brain install opencode
 * To update: brain install opencode
 * To check status: brain doctor
 * Source: https://github.com/huynle/brain-api
 * Generated: {{GENERATED_DATE}}
 */

import type { Plugin } from "@opencode-ai/plugin";
import { tool } from "@opencode-ai/plugin";
import { execSync } from "child_process";
import { randomUUID } from "crypto";
import { readFileSync } from "fs";
import { mkdir, readFile, writeFile } from "fs/promises";
import { arch, hostname, homedir, platform, userInfo } from "os";
import { basename, dirname, join } from "path";

// ============================================================================
// Types
// ============================================================================

type BrainEntryType =
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
  | "task"
  | "dream"
  | "automation";

type BrainEntryStatus =
  | "draft"
  | "pending"
  | "active"
  | "in_progress"
  | "blocked"
  | "cancelled"
  | "completed"
  | "validated"
  | "superseded"
  | "archived";

type TaskPriority = "high" | "medium" | "low";

// ============================================================================
// Configuration
// ============================================================================

/**
 * Load brain config from ~/.config/brain/config.yaml, falling back to env vars.
 *
 * Priority order (highest wins):
 * 1. Environment variables: BRAIN_API_URL, BRAIN_API_TOKEN
 * 2. Config file: ~/.config/brain/config.yaml
 *    - mcp.api_url → API URL
 *    - runner.api_token → API token
 *    - runner.brain_api_url → API URL (fallback if mcp.api_url not set)
 * 3. Default: http://localhost:3333
 */
function loadBrainConfig(): { apiUrl: string; apiToken?: string } {
  let fileApiUrl: string | undefined;
  let fileApiToken: string | undefined;

  try {
    const configPath = join(homedir(), ".config", "brain", "config.yaml");
    const raw = readFileSync(configPath, "utf-8");

    // Minimal YAML parser - extract the fields we need without a YAML dependency.
    // Handles the two config structures:
    //   mcp:
    //       api_url: https://...
    //   runner:
    //       brain_api_url: https://...
    //       api_token: ...
    let currentSection = "";
    for (const line of raw.split("\n")) {
      const trimmed = line.trimEnd();

      // Detect top-level section headers (no leading whitespace)
      if (/^[a-z_]+:\s*$/.test(trimmed)) {
        currentSection = trimmed.replace(":", "").trim();
        continue;
      }

      // Extract key: value pairs under known sections
      const kvMatch = trimmed.match(/^\s+([a-z_]+):\s*(.+)$/);
      if (!kvMatch) continue;

      const [, key, value] = kvMatch;
      // Strip surrounding quotes if present
      const cleanValue = value.replace(/^["']|["']$/g, "").trim();
      if (!cleanValue) continue;

      if (currentSection === "mcp" && key === "api_url") {
        fileApiUrl = cleanValue;
      } else if (currentSection === "runner") {
        if (key === "brain_api_url" && !fileApiUrl) {
          fileApiUrl = cleanValue;
        }
        if (key === "api_token") {
          fileApiToken = cleanValue;
        }
      }
    }
  } catch {
    // Config file not found or unreadable — fall through to defaults
  }

  return {
    apiUrl: process.env.BRAIN_API_URL || fileApiUrl || "http://localhost:3333",
    apiToken: process.env.BRAIN_API_TOKEN || fileApiToken,
  };
}

const BRAIN_CONFIG = loadBrainConfig();
const BRAIN_API_URL = BRAIN_CONFIG.apiUrl;
const BRAIN_API_TOKEN = BRAIN_CONFIG.apiToken;

function getAuthHeaders(): Record<string, string> {
  if (!BRAIN_API_TOKEN) {
    return {};
  }

  return {
    Authorization: `Bearer ${BRAIN_API_TOKEN}`,
  };
}

// ============================================================================
// Health Check & Connection State
// ============================================================================

interface BrainConnectionState {
  available: boolean;
  lastCheck: number;
  lastError?: string;
  version?: string;
}

// Connection state - checked on first tool use and cached
let connectionState: BrainConnectionState = {
  available: false,
  lastCheck: 0,
};

const HEALTH_CHECK_INTERVAL_MS = 30_000; // Re-check every 30 seconds when healthy
const HEALTH_CHECK_RETRY_MS = 5_000; // Re-check every 5 seconds when unhealthy (faster reconnect)

async function checkBrainHealth(): Promise<BrainConnectionState> {
  const now = Date.now();
  
  // Use shorter retry interval when unhealthy for faster reconnect
  const cacheInterval = connectionState.available 
    ? HEALTH_CHECK_INTERVAL_MS 
    : HEALTH_CHECK_RETRY_MS;
  
  // Return cached state if checked recently
  if (now - connectionState.lastCheck < cacheInterval) {
    return connectionState;
  }

  try {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 5000); // 5 second timeout
    
    const response = await fetch(`${BRAIN_API_URL}/api/v1/health`, {
      signal: controller.signal,
      headers: getAuthHeaders(),
    });
    clearTimeout(timeoutId);

    if (response.ok) {
      const data = await response.json();
      connectionState = {
        available: true,
        lastCheck: now,
        version: data.version,
      };
    } else {
      connectionState = {
        available: false,
        lastCheck: now,
        lastError: `Server returned ${response.status}: ${response.statusText}`,
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    const isConnectionRefused = errorMessage.includes("ECONNREFUSED") || 
                                 errorMessage.includes("fetch failed") ||
                                 errorMessage.includes("aborted");
    
    connectionState = {
      available: false,
      lastCheck: now,
      lastError: isConnectionRefused 
        ? `Cannot connect to Brain API at ${BRAIN_API_URL}. Is the server running? Start it with: brain start`
        : `Health check failed: ${errorMessage}`,
    };
  }

  return connectionState;
}

function formatUnavailableMessage(state: BrainConnectionState): string {
  return `**BRAIN API UNAVAILABLE**

The Brain API server is not reachable at ${BRAIN_API_URL}.

**Error:** ${state.lastError || "Unknown error"}

**What this means:**
- Brain tools (save, recall, search, etc.) will not work
- Your knowledge and notes are not accessible right now

**To fix this:**
1. Start the Brain API server: \`brain start\`
2. Or check if it's running: \`brain status\`
3. Or check the logs: \`brain logs\`

**Alternative:** If you don't need brain functionality for this task, you can proceed without it.
The brain tools will automatically reconnect when the server becomes available.`;
}

// ============================================================================
// Execution Context
// ============================================================================

interface ExecutionContext {
  projectId: string; // Short project name (last path segment)
  workdir: string; // $HOME-relative path to main repo
  directory: string; // Absolute current working directory
  gitRoot?: string; // Current worktree root
  gitCommonDir?: string; // Common .git directory for linked worktrees
  gitWorktreeMain?: string; // Main worktree path from git worktree list
  gitRemote?: string; // Git remote URL
  gitBranch?: string; // Current branch (worktree derived from this)
  folderName?: string; // Basename used as a project hint
}

interface ClientIdentity {
  client_id: string;
  kind: string;
  host_id: string;
  hostname: string;
  os: string;
  arch: string;
  username: string;
  home_dir: string;
}

/**
 * Extract a short project name from a $HOME-relative path.
 * e.g. "projects/brain-api" → "brain-api", "brain-api" → "brain-api"
 * The task API validates project IDs with /^[a-zA-Z0-9_-]+$/ (no slashes).
 */
function resolveProjectName(homeRelativePath: string): string {
  const segments = homeRelativePath.split("/").filter(Boolean);
  return segments[segments.length - 1] || homeRelativePath;
}

/**
 * Checks if an automation event pattern matches a given event name.
 * Supports exact match and wildcard prefix (e.g., "task.*" matches "task.completed").
 */
function matchesEvent(pattern: string, eventName: string): boolean {
  if (pattern === eventName) return true;
  if (pattern.endsWith(".*")) {
    const prefix = pattern.slice(0, -2);
    return eventName.startsWith(prefix + ".");
  }
  if (pattern === "*") return true;
  return false;
}

function getExecutionContext(directory: string): ExecutionContext {
  const home = homedir();

  // Get main repo path (resolves worktrees to their main repo)
  let mainRepoPath = directory;
  let gitRoot: string | undefined;
  let gitCommonDir: string | undefined;
  let gitRemote: string | undefined;
  let gitBranch: string | undefined;

  try {
    // Check if we're in a worktree and get the main repo path
    const worktreeList = execSync("git worktree list --porcelain", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();

    const lines = worktreeList.split("\n");
    const firstWorktreeLine = lines.find((l) => l.startsWith("worktree "));
    if (firstWorktreeLine) {
      mainRepoPath = firstWorktreeLine.replace("worktree ", "");
    }

    gitRoot = execSync("git rev-parse --show-toplevel", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();

    gitCommonDir = execSync("git rev-parse --git-common-dir", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();

    // Get git remote
    gitRemote = execSync("git remote get-url origin", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();

    // Get current branch (used to derive worktree path later)
    gitBranch = execSync("git branch --show-current", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();
  } catch {
    // Not a git repo or git not available
  }

  // Make paths relative to $HOME
  const makeHomeRelative = (path: string): string => {
    if (path.startsWith(home)) {
      return path.slice(home.length + 1); // +1 for the slash
    }
    return path;
  };

  // projectId = short name (last path segment), used by task API which validates
  // with ProjectIdSchema (alphanumeric, hyphens, underscores only — no slashes)
  const homePath = makeHomeRelative(mainRepoPath);
  const projectId = resolveProjectName(homePath);
  const workdir = homePath;

  return {
    projectId,
    workdir,
    directory,
    gitRoot,
    gitCommonDir,
    gitWorktreeMain: mainRepoPath,
    gitRemote,
    gitBranch,
    folderName: basename(mainRepoPath),
  };
}

async function loadOrCreateClientIdentity(): Promise<ClientIdentity> {
  const configDir = join(homedir(), ".config", "brain");
  const hostID = await loadOrCreateID(join(configDir, "host_id"), "host");
  const clientID = await loadOrCreateID(join(configDir, "opencode_client_id"), "opencode");

  let username = "";
  try {
    username = userInfo().username;
  } catch {
    username = process.env.USER || process.env.USERNAME || "";
  }

  return {
    client_id: clientID,
    kind: "opencode",
    host_id: hostID,
    hostname: hostname(),
    os: platform(),
    arch: arch(),
    username,
    home_dir: homedir(),
  };
}

async function loadOrCreateID(path: string, prefix: string): Promise<string> {
  let id = "";

  try {
    id = (await readFile(path, "utf-8")).trim();
  } catch {
    id = `${prefix}-${randomUUID()}`;
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, id + "\n", "utf-8");
  }

  return id;
}

const ENTRY_TYPES: BrainEntryType[] = [
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
  "dream",
  "automation",
];

const ENTRY_STATUSES: BrainEntryStatus[] = [
  "draft",
  "pending",
  "active",
  "in_progress",
  "blocked",
  "cancelled",
  "completed",
  "validated",
  "superseded",
  "archived",
];

type BrainPriority = "high" | "medium" | "low";

const PRIORITIES: BrainPriority[] = ["high", "medium", "low"];

// ============================================================================
// API Client
// ============================================================================

interface ApiError {
  error: string;
  message: string;
  details?: unknown;
}

interface AttachmentDerived {
  id: string;
  kind?: string;
  content_type?: string;
  size?: number;
  storage_key?: string;
  created?: string;
}

interface AttachmentReference {
  id: string;
  filename?: string;
  content_type?: string;
  size?: number;
  sha256?: string;
  metadata?: Record<string, string>;
  download_url?: string;
  text_url?: string;
  role?: string;
  caption?: string;
  derived?: AttachmentDerived[];
}

interface Attachment extends AttachmentReference {
  filename: string;
  content_type: string;
  size: number;
  storage_key?: string;
  created?: string;
  modified?: string;
}

interface AttachmentDerivedText {
  id?: string;
  kind?: string;
  status: string;
  content_type?: string;
  text?: string;
  error?: string;
  metadata?: Record<string, string>;
  created?: string;
  modified?: string;
}

interface AttachmentLinkedEntry {
  path: string;
  role?: string;
}

interface AttachmentExtractionResult {
  attachment: Attachment;
  derived_text: AttachmentDerivedText;
  linked_entries?: AttachmentLinkedEntry[];
}

class BrainUnavailableError extends Error {
  constructor(state: BrainConnectionState) {
    super(formatUnavailableMessage(state));
    this.name = "BrainUnavailableError";
  }
}

async function apiRequest<T>(
  method: string,
  path: string,
  body?: unknown,
  queryParams?: Record<string, string | number | boolean | undefined>
): Promise<T> {
  // Check health before making request
  const health = await checkBrainHealth();
  if (!health.available) {
    throw new BrainUnavailableError(health);
  }

  let url = `${BRAIN_API_URL}/api/v1${path}`;

  // Add query parameters
  if (queryParams) {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(queryParams)) {
      if (value !== undefined) {
        params.append(key, String(value));
      }
    }
    const queryString = params.toString();
    if (queryString) {
      url += `?${queryString}`;
    }
  }

  const options: RequestInit = {
    method,
    headers: {
      "Content-Type": "application/json",
      ...getAuthHeaders(),
    },
  };

  if (body && (method === "POST" || method === "PATCH" || method === "PUT")) {
    options.body = JSON.stringify(body);
  }

  try {
    const response = await fetch(url, options);

    if (!response.ok) {
      let errorData: ApiError;
      try {
        errorData = await response.json();
      } catch {
        errorData = {
          error: "API Error",
          message: `HTTP ${response.status}: ${response.statusText}`,
        };
      }
      throw new Error(errorData.message || `API error: ${response.status}`);
    }

    return response.json();
  } catch (error) {
    // If fetch itself fails (connection error), mark as unavailable and throw helpful message
    if (error instanceof BrainUnavailableError) {
      throw error;
    }
    
    const errorMessage = error instanceof Error ? error.message : String(error);
    const isConnectionError = errorMessage.includes("ECONNREFUSED") || 
                              errorMessage.includes("fetch failed") ||
                              errorMessage.includes("network");
    
    if (isConnectionError) {
      // Mark as unavailable for future requests
      connectionState = {
        available: false,
        lastCheck: Date.now(),
        lastError: `Connection lost: ${errorMessage}`,
      };
      throw new BrainUnavailableError(connectionState);
    }
    
    throw error;
  }
}

async function attachmentUploadRequest(
  projectId: string,
  filePath: string,
  metadata?: Record<string, unknown>
): Promise<{ attachment: Attachment }> {
  const health = await checkBrainHealth();
  if (!health.available) {
    throw new BrainUnavailableError(health);
  }

  const data = await readFile(filePath);
  const form = new FormData();
  form.append("project_id", projectId);
  if (metadata && Object.keys(metadata).length > 0) {
    form.append("metadata", JSON.stringify(metadata));
  }
  form.append("file", new Blob([data]), basename(filePath));

  const response = await fetch(`${BRAIN_API_URL}/api/v1/attachments`, {
    method: "POST",
    headers: getAuthHeaders(),
    body: form,
  });

  if (!response.ok) {
    throw new Error(await formatAPIError(response));
  }

  return response.json();
}

async function attachmentTextRequest(
  projectId: string,
  attachmentId: string
): Promise<string> {
  const health = await checkBrainHealth();
  if (!health.available) {
    throw new BrainUnavailableError(health);
  }

  const params = new URLSearchParams({ project_id: projectId });
  const response = await fetch(
    `${BRAIN_API_URL}/api/v1/attachments/${encodeURIComponent(attachmentId)}/text?${params.toString()}`,
    { headers: getAuthHeaders() }
  );

  if (!response.ok) {
    throw new Error(await formatAPIError(response));
  }

  return response.text();
}

async function attachmentDownloadRequest(
  projectId: string,
  attachmentId: string,
  outputPath: string
): Promise<void> {
  const health = await checkBrainHealth();
  if (!health.available) {
    throw new BrainUnavailableError(health);
  }

  const params = new URLSearchParams({ project_id: projectId });
  const response = await fetch(
    `${BRAIN_API_URL}/api/v1/attachments/${encodeURIComponent(attachmentId)}/content?${params.toString()}`,
    { headers: getAuthHeaders() }
  );

  if (!response.ok) {
    throw new Error(await formatAPIError(response));
  }

  const parent = dirname(outputPath);
  if (parent && parent !== ".") {
    await mkdir(parent, { recursive: true });
  }
  const bytes = new Uint8Array(await response.arrayBuffer());
  await writeFile(outputPath, bytes);
}

async function attachmentExtractRequest(
  projectId: string,
  attachmentId: string,
  body?: Record<string, unknown>
): Promise<AttachmentExtractionResult> {
  return apiRequest<AttachmentExtractionResult>(
    "POST",
    `/attachments/${encodeURIComponent(attachmentId)}/extract`,
    body && Object.keys(body).length > 0 ? body : undefined,
    { project_id: projectId }
  );
}

async function formatAPIError(response: Response): Promise<string> {
  try {
    const errorData = await response.json() as ApiError;
    return errorData.message || `HTTP ${response.status}: ${response.statusText}`;
  } catch {
    return `HTTP ${response.status}: ${response.statusText}`;
  }
}

function formatDerivedAttachments(derived?: AttachmentDerived[]): string {
  if (!derived || derived.length === 0) return "";
  const items = derived.map((item) => {
    const parts = [item.id, item.kind, item.content_type, item.size ? `${item.size} bytes` : undefined, item.storage_key].filter(Boolean);
    return parts.join(" / ");
  });
  return items.join("; ");
}

function formatAttachmentReference(attachment: AttachmentReference): string {
  const parts = [`\`${attachment.id}\``];
  if (attachment.filename) parts.push(attachment.filename);
  if (attachment.content_type) parts.push(attachment.content_type);
  if (attachment.size) parts.push(`${attachment.size} bytes`);
  if (attachment.role) parts.push(`role: ${attachment.role}`);

  let line = `- ${parts.join(" — ")}`;
  if (attachment.caption) line += `\n  Caption: ${attachment.caption}`;
  if (attachment.sha256) line += `\n  SHA256: ${attachment.sha256}`;
  if (attachment.download_url) line += `\n  Download: ${attachment.download_url}`;
  if (attachment.text_url) line += `\n  Text: ${attachment.text_url}`;
  const derived = formatDerivedAttachments(attachment.derived);
  if (derived) line += `\n  Derived: ${derived}`;
  if (attachment.metadata && Object.keys(attachment.metadata).length > 0) {
    const metadata = Object.keys(attachment.metadata)
      .sort()
      .map((key) => `${key}=${attachment.metadata?.[key]}`)
      .join(", ");
    line += `\n  Metadata: ${metadata}`;
  }
  return line;
}

function formatAttachmentReferences(attachments?: AttachmentReference[]): string {
  if (!attachments || attachments.length === 0) return "";
  return ["", "### Attachments", ...attachments.map(formatAttachmentReference)].join("\n");
}

function formatAttachment(attachment: Attachment): string {
  return formatAttachmentReference(attachment);
}

function metadataValue(metadata: Record<string, string> | undefined, ...keys: string[]): string | undefined {
  if (!metadata) return undefined;
  for (const key of keys) {
    const value = metadata[key]?.trim();
    if (value) return value;
  }
  return undefined;
}

function formatAttachmentExtractionResult(result: AttachmentExtractionResult): string {
  const derived = result.derived_text || { status: "unknown" };
  const metadata = derived.metadata || {};
  const attachmentID = result.attachment?.id || metadata.attachment_id || "unknown";
  const lines = [
    `## Attachment Extraction: ${attachmentID}`,
    "",
    `Status: ${derived.status || "unknown"}`,
  ];

  if (result.attachment?.filename) lines.push(`Filename: ${result.attachment.filename}`);
  const provider = metadataValue(metadata, "provider", "extraction_provider");
  if (provider) lines.push(`Provider: ${provider}`);
  const model = metadataValue(metadata, "model", "extraction_model");
  if (model) lines.push(`Model: ${model}`);
  if (derived.error) lines.push(`Reason: ${derived.error}`);
  if (derived.content_type) lines.push(`Derived content type: ${derived.content_type}`);
  if (derived.id) lines.push(`Derived text ID: ${derived.id}`);
  if (derived.kind) lines.push(`Derived kind: ${derived.kind}`);
  if (derived.created) lines.push(`Derived created: ${derived.created}`);
  if (derived.modified) lines.push(`Derived modified: ${derived.modified}`);
  lines.push(`Text: ${(derived.text || "").length} chars`);

  const metadataKeys = Object.keys(metadata).sort();
  if (metadataKeys.length > 0) {
    lines.push("", "Metadata:");
    for (const key of metadataKeys) {
      lines.push(`- ${key}: ${metadata[key]}`);
    }
  }

  if (result.linked_entries && result.linked_entries.length > 0) {
    lines.push("", "Linked entries:");
    for (const entry of result.linked_entries) {
      lines.push(`- ${entry.path}${entry.role ? ` (role: ${entry.role})` : ""}`);
    }
  }

  return lines.join("\n").trim();
}

function addNonEmptyStringFields(
  target: Record<string, unknown>,
  source: Record<string, unknown>,
  keys: string[]
) {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "string" && value !== "") {
      target[key] = value;
    }
  }
}

function addPresentFields(
  target: Record<string, unknown>,
  source: Record<string, unknown>,
  keys: string[]
) {
  for (const key of keys) {
    const value = source[key];
    if (value !== undefined && value !== null) {
      target[key] = value;
    }
  }
}

const OPENCODE_OPTIONAL_DEFAULTS: Record<string, unknown> = {
  priority: "medium",
  feature_priority: "high",
  merge_policy: "prompt_only",
  merge_strategy: "squash",
  remote_branch_policy: "keep",
  execution_mode: "worktree",
  executor: "opencode",
  open_pr_before_merge: false,
  complete_on_idle: false,
  schedule_enabled: false,
  max_runs: 0,
};

function isOpenCodeOptionalDefault(key: string, value: unknown): boolean {
  if (!(key in OPENCODE_OPTIONAL_DEFAULTS)) return false;
  return OPENCODE_OPTIONAL_DEFAULTS[key] === value;
}

function sanitizeUpdateArgs(source: Record<string, unknown>): Record<string, unknown> {
  const clean: Record<string, unknown> = {};
  let defaultCount = 0;

  for (const [key, value] of Object.entries(source)) {
    if (value === undefined || value === null) continue;
    if (typeof value === "string" && value === "") continue;
    if (isOpenCodeOptionalDefault(key, value)) defaultCount++;
    clean[key] = value;
  }

  // OpenCode can materialize optional schema fields with defaults. Treat a
  // cluster of those sentinel values as tool noise, while preserving intentional
  // single-field updates such as priority="medium".
  if (defaultCount >= 3) {
    for (const [key, value] of Object.entries(clean)) {
      if (isOpenCodeOptionalDefault(key, value)) {
        delete clean[key];
      }
    }
  }

  return clean;
}

function sanitizeObjectArg(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) return value;

  const clean: Record<string, unknown> = {};
  for (const [key, field] of Object.entries(value as Record<string, unknown>)) {
    if (field === undefined || field === null) continue;
    if (typeof field === "string" && field === "") continue;
    if (Array.isArray(field) && field.length === 0) continue;
    clean[key] = field;
  }
  return clean;
}

function hasFields(value: unknown): boolean {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return value !== undefined && value !== null;
  }
  return Object.keys(value as Record<string, unknown>).length > 0;
}

function sanitizeBulkUpdateEntries(value: unknown): unknown {
  if (!Array.isArray(value)) return value;

  return value.map((entry) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) return entry;
    const clean: Record<string, unknown> = { ...(entry as Record<string, unknown>) };
    if (clean.updates && typeof clean.updates === "object" && !Array.isArray(clean.updates)) {
      clean.updates = sanitizeUpdateArgs(clean.updates as Record<string, unknown>);
    }
    return clean;
  });
}



// ============================================================================
// Plugin
// ============================================================================

export const BrainPlugin: Plugin = async ({ project, directory }) => {
  const context = getExecutionContext(directory);
  const projectId = project?.id || context.projectId || "unknown";

  return {
    tool: {
      // ========================================
      // brain_project_context
      // ========================================
      brain_project_context: tool({
        description:
          "Resolve the current workspace to a Brain project, register this Brain client/workspace automatically, and return the project's latest dream context. Use at session start, when resuming work, or whenever prior project context may be needed.",
        args: {},
        async execute() {
          try {
            const identity = await loadOrCreateClientIdentity();
            const response = await apiRequest<{
              project_id: string;
              confidence: string;
              source: string;
              dream?: {
                id?: string;
                title?: string;
                path?: string;
                content?: string;
              };
            }>("POST", "/context/resolve", {
              client: identity,
              workspace: {
                path: context.directory,
                git_root: context.gitRoot,
                git_common_dir: context.gitCommonDir,
                git_worktree_main: context.gitWorktreeMain,
                git_branch: context.gitBranch,
                git_remote: context.gitRemote,
                folder_name: context.folderName || projectId,
              },
            });

            const lines = [
              "## Brain Project Context",
              "",
              `Project: ${response.project_id}`,
              `Confidence: ${response.confidence}`,
              `Source: ${response.source}`,
              `Client: ${identity.client_id}`,
              `Host: ${identity.hostname}`,
            ];

            if (response.dream?.content) {
              lines.push("", `## Dream: ${response.dream.title || response.dream.id || "latest"}`, "", response.dream.content);
            } else {
              lines.push("", "No dream entry found for this project yet.");
            }

            return lines.join("\n");
          } catch (error) {
            return `Failed to resolve project context: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_save
      // ========================================
      brain_save: tool({
        description: `Save content to the brain for future reference. Use this to persist:
- summaries: Session summaries, key decisions made
- reports: Analysis reports, code reviews, investigations
- walkthroughs: Code explanations, architecture overviews
- plans: Implementation plans, designs, roadmaps
- patterns: Reusable patterns discovered (use global:true for cross-project)
- learnings: General learnings, best practices (use global:true for cross-project)
- ideas: Ideas for future exploration
- scratch: Temporary working notes
- decision: Architectural decisions, ADRs
- exploration: Investigation notes, research findings

Feature orchestration:
- Use feature_id to group tasks into a feature.
- Use feature_depends_on to make a feature wait for one or more other features before starting.
- Use trigger.event="feature.completed" with trigger.filter.feature_id to create post-feature tasks that activate after a feature completes.

Automation guidance:
- For user-facing requests to create an automation, monitor something, or run/check/review something repeatedly, prefer type="automation" with trigger.type="cron"/"event" and action.type="create_task". This creates a collapsible automation parent with generated run tasks in the Automations tab.
- Use type="task" with schedule/run_once_at only when the user explicitly asks for a scheduled task row or one fixed task on a clock.`,
        args: {
          type: tool.schema
            .enum(ENTRY_TYPES)
            .describe("Type of content being saved"),
          title: tool.schema
            .string()
            .describe("Short descriptive title for the entry"),
          content: tool.schema
            .string()
            .describe("The content to save (markdown supported)"),
          tags: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Tags for categorization and search"),
          status: tool.schema
            .enum(ENTRY_STATUSES)
            .optional()
            .describe(
              "Initial status. Tasks default to 'draft' (user reviews before promoting to 'pending'). Other entry types default to 'active'."
            ),
          priority: tool.schema
            .enum(["high", "medium", "low"])
            .optional()
            .describe(
              "Priority level for tasks. High = urgent/blocking, Medium = normal, Low = nice-to-have."
            ),
          depends_on: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe(
              "Task dependencies - list of task IDs or titles that must be completed before this task. Validated on save. Use format: 'task-id' for same project, 'project:task-id' for cross-project. Common mistakes (full paths, .md extension) are auto-normalized."
            ),
          feature_id: tool.schema
            .string()
            .optional()
            .describe(
              "Feature group ID for this task (e.g., 'auth-system', 'payment-flow'). Tasks with the same feature_id are grouped together for ordered execution."
            ),
          feature_priority: tool.schema
            .enum(["high", "medium", "low"])
            .optional()
            .describe(
              "Priority level for the feature group. Determines execution order relative to other features."
            ),
          feature_depends_on: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe(
              "Feature IDs this feature depends on. All tasks in dependent features must complete before this feature's tasks can start. Use this for before-feature orchestration (e.g., feature 'main' depends on feature 'preflight')."
            ),
          trigger: tool.schema
            .object({
              type: tool.schema.string().optional().describe("Trigger type for automation entries: event, cron, webhook, or session"),
              event: tool.schema.string().optional().describe("Event pattern to match, e.g. 'feature.completed', 'task.completed', or 'task.*'"),
              schedule: tool.schema.string().optional().describe("Cron schedule for cron automation triggers"),
              webhook: tool.schema.string().optional().describe("Webhook path/name for webhook-triggered automations"),
              filter: tool.schema.object({
                feature_id: tool.schema.string().optional().describe("Only match events for this feature ID"),
                project_id: tool.schema.string().optional().describe("Only match events for this project ID"),
                task_id: tool.schema.string().optional().describe("Only match events for this task ID"),
                runner_id: tool.schema.string().optional().describe("Only match events from this runner ID"),
                from_status: tool.schema.string().optional().describe("Only match status-change events from this status"),
                to_status: tool.schema.string().optional().describe("Only match status-change events to this status"),
              }).optional().describe("Event field filters. For post-feature tasks use {feature_id:'main-feature', project_id:'my-project'}."),
              once_per: tool.schema.string().optional().describe("Dedup key for automations, e.g. feature_id, session, or day"),
              cooldown: tool.schema.string().optional().describe("Minimum interval between automation firings, e.g. '5m' or '1h'"),
              max_concurrent: tool.schema.number().optional().describe("Maximum concurrent executions for this trigger"),
              ignore_automation_events: tool.schema.boolean().optional().describe("Whether to ignore events emitted by automations"),
            })
            .optional()
            .describe("Event trigger for inactive/active tasks or automation entries. For post-feature tasks use event='feature.completed' and filter.feature_id=<completed feature>."),
          action: tool.schema
            .object({
              type: tool.schema.string().optional().describe("Automation action type, e.g. create_task or script"),
              title_template: tool.schema.string().optional().describe("Template for generated task title"),
              prompt_template: tool.schema.string().optional().describe("Template for generated task prompt/content"),
              direct_prompt: tool.schema.string().optional().describe("Direct prompt for generated tasks"),
              command: tool.schema.string().optional().describe("Script command for script actions"),
              agent: tool.schema.string().optional().describe("Agent override for generated tasks"),
              model: tool.schema.string().optional().describe("Model override for generated tasks"),
              executor: tool.schema.string().optional().describe("Executor override for generated tasks"),
              target_workdir: tool.schema.string().optional().describe("Target workdir for generated tasks"),
              complete_on_idle: tool.schema.boolean().optional().describe("Whether generated automation tasks should complete when the executor becomes idle. Defaults to true for automation-generated tasks; set true explicitly unless there is a concrete reason not to."),
            })
            .optional()
            .describe("Automation action config for automation entries."),
          retry: tool.schema
            .object({
              max_attempts: tool.schema.number().optional().describe("Maximum retry attempts"),
              backoff: tool.schema.string().optional().describe("Backoff policy or duration"),
              timeout: tool.schema.string().optional().describe("Action timeout"),
            })
            .optional()
            .describe("Automation retry policy for automation entries."),
          global: tool.schema
            .boolean()
            .optional()
            .describe(
              "Save to global brain (cross-project). Recommended for patterns and learnings."
            ),
          project: tool.schema
            .string()
            .optional()
            .describe(
              "Explicit project ID/name for task organization. If not provided, uses current git repo hash or 'global'."
            ),
          relatedEntries: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe(
              "Titles or paths of related brain entries to link to. Auto-generates markdown links."
            ),
          user_original_request: tool.schema
            .string()
            .optional()
            .describe(
              "Verbatim user request for this task. HIGHLY RECOMMENDED for tasks - enables validation during task completion by comparing implementation against original intent. Supports multiline content, code blocks, and special characters. When creating multiple tasks from one user request, include this in EACH task."
            ),
          target_workdir: tool.schema
            .string()
            .optional()
            .describe(
              "Explicit working directory override for task execution (absolute path). PRIMARY USE CASE: Filing tasks across project boundaries - when an issue is detected in a dependent project, use target_workdir to file the task directly in the parent/target project so it executes there. The task runner will try this directory first before falling back to workdir resolution."
            ),
          git_branch: tool.schema
            .string()
            .optional()
            .describe(
              "Execution branch override for this task. If not provided, defaults to the current git branch of the working directory."
            ),
          merge_target_branch: tool.schema
            .string()
            .optional()
            .describe("Branch to merge completed work into"),
          merge_policy: tool.schema
            .enum(["prompt_only", "auto_pr", "auto_merge"])
            .optional()
            .describe("Merge behavior at completion (default: auto_merge)"),
          merge_strategy: tool.schema
            .enum(["squash", "merge", "rebase"])
            .optional()
            .describe("Merge strategy for auto-merge (default: squash)"),
          open_pr_before_merge: tool.schema
            .boolean()
            .optional()
            .describe("Open PR before merge when enabled (default: false)"),
          execution_mode: tool.schema
            .enum(["worktree", "current_branch"])
            .optional()
            .describe("Task execution mode (default: worktree)"),
          complete_on_idle: tool.schema
            .boolean()
            .optional()
            .describe("Mark task as completed when the agent becomes idle (default: false). Useful for fire-and-forget tasks where idle means done."),
          remote_branch_policy: tool.schema
            .enum(["keep", "delete"])
            .optional()
            .describe("Policy for remote branch after merge. 'keep' preserves the branch, 'delete' removes it (default: delete)."),
          direct_prompt: tool.schema
            .string()
            .optional()
            .describe(
              "Direct prompt to execute, bypassing default skill workflow. The prompt is sent verbatim to OpenCode when the task runs. Use for simple, self-contained commands like '/fix-tests src/' or '/lint'."
            ),
          agent: tool.schema
            .string()
            .optional()
            .describe(
              "Override the default agent for this task (e.g., 'explore', 'tdd-dev', 'build', or custom agent names)"
            ),
          model: tool.schema
            .string()
            .optional()
            .describe(
              "Override the default model for this task (format: 'provider/model-id', e.g., 'anthropic/claude-sonnet-4-20250514')"
            ),
          executor: tool.schema
            .enum(["opencode", "pi", "script"])
            .optional()
            .describe("Executor backend for this task. Runner must advertise the selected executor."),
          extensions: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Additional executor extensions to load for this task."),
          schedule: tool.schema
            .string()
            .optional()
            .describe(
              "Cron schedule expression for type='task' scheduled task rows (e.g., '*/5 * * * *', '0 2 * * *'). If the user asked to create a project-level automation, monitor, or recurring check, prefer type='automation' with trigger.type='cron' instead so runs appear under a collapsible Automation parent."
            ),
          schedule_enabled: tool.schema
            .boolean()
            .optional()
            .describe(
              "Whether the schedule is active (default true when schedule exists). Set to false to pause scheduling."
            ),
          max_runs: tool.schema
            .number()
            .optional()
            .describe(
              "Maximum number of scheduled runs before auto-disabling the schedule. When the run count reaches this limit, schedule_enabled is set to false and a note is appended. Omit or set to 0 for unlimited runs."
            ),
          run_once_at: tool.schema
            .string()
            .optional()
            .describe(
              "RFC3339 timestamp for one-time execution (e.g., '2025-12-01T00:00:00Z'). Task runs once at this time then schedule_enabled is set to false."
            ),
          timezone: tool.schema
            .string()
            .optional()
            .describe(
              "IANA timezone for schedule interpretation (e.g., 'America/Denver', 'Europe/London'). Applies to cron schedule and time window fields."
            ),
          starts_at: tool.schema
            .string()
            .optional()
            .describe(
              "RFC3339 timestamp for schedule window start. Task will not run before this time."
            ),
          expires_at: tool.schema
            .string()
            .optional()
            .describe(
              "RFC3339 timestamp for schedule window end. Task will not run after this time and schedule_enabled is set to false."
            ),
          feature_schedule: tool.schema
            .string()
            .optional()
            .describe(
              "Cron schedule for feature-level execution. Creates a feature_schedule gate task that triggers all tasks in the feature on this schedule."
            ),
          feature_starts_at: tool.schema
            .string()
            .optional()
            .describe(
              "RFC3339 timestamp for feature schedule window start. Feature gate task will not run before this time."
            ),
          feature_expires_at: tool.schema
            .string()
            .optional()
            .describe(
              "RFC3339 timestamp for feature schedule window end. Feature gate task will not run after this time."
            ),
          feature_run_once_at: tool.schema
            .string()
            .optional()
            .describe(
              "RFC3339 timestamp for one-time feature execution. Creates a feature_schedule gate task that runs once at this time."
            ),
          feature_timezone: tool.schema
            .string()
            .optional()
            .describe(
              "IANA timezone for feature schedule interpretation (e.g., 'America/Denver'). Applies to feature_schedule and feature time window fields."
            ),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              id: string;
              path: string;
              title: string;
              type: string;
              status: string;
              link: string;
            }>("POST", "/entries", {
              type: args.type,
              title: args.title,
              content: args.content,
              tags: args.tags,
              status: args.status,
              priority: args.priority,
              depends_on: args.depends_on,
              global: args.global,
              project: args.project || projectId,
              relatedEntries: args.relatedEntries,
              // Execution context for tasks
              target_workdir: args.type === "task" ? args.target_workdir : undefined,
              workdir: args.type === "task" ? context.workdir : undefined,
              git_remote: args.type === "task" ? context.gitRemote : undefined,
              git_branch:
                args.type === "task"
                  ? args.git_branch ?? context.gitBranch
                  : undefined,
              merge_target_branch:
                args.type === "task" ? args.merge_target_branch : undefined,
              merge_policy: args.type === "task" ? args.merge_policy : undefined,
              merge_strategy:
                args.type === "task" ? args.merge_strategy : undefined,
              open_pr_before_merge:
                args.type === "task" ? args.open_pr_before_merge : undefined,
              execution_mode:
                args.type === "task" ? args.execution_mode : undefined,

              complete_on_idle:
                args.type === "task" ? args.complete_on_idle : undefined,
              remote_branch_policy:
                args.type === "task" ? args.remote_branch_policy : undefined,
              // User intent for validation
              user_original_request:
                args.type === "task" ? args.user_original_request : undefined,
              // Feature grouping for tasks
              feature_id: args.type === "task" ? args.feature_id : undefined,
              feature_priority: args.type === "task" ? args.feature_priority : undefined,
              feature_depends_on: args.type === "task" ? args.feature_depends_on : undefined,
              trigger: args.type === "task" || args.type === "automation" ? args.trigger : undefined,
              action: args.type === "automation" ? args.action : undefined,
              retry: args.type === "automation" ? args.retry : undefined,
              // OpenCode execution options for tasks
              direct_prompt: args.type === "task" ? args.direct_prompt : undefined,
              agent: args.type === "task" ? args.agent : undefined,
              model: args.type === "task" ? args.model : undefined,
              executor: args.type === "task" ? args.executor : undefined,
              extensions: args.type === "task" ? args.extensions : undefined,
              // Cron scheduling for tasks
              schedule: args.type === "task" ? args.schedule : undefined,
              schedule_enabled: args.type === "task" ? args.schedule_enabled : undefined,
              max_runs: args.type === "task" ? args.max_runs : undefined,
              // Time-based scheduling fields
              run_once_at: args.type === "task" ? args.run_once_at : undefined,
              timezone: args.type === "task" ? args.timezone : undefined,
              starts_at: args.type === "task" ? args.starts_at : undefined,
              expires_at: args.type === "task" ? args.expires_at : undefined,
              // Feature schedule fields
              feature_schedule: args.type === "task" ? args.feature_schedule : undefined,
              feature_starts_at: args.type === "task" ? args.feature_starts_at : undefined,
              feature_expires_at: args.type === "task" ? args.feature_expires_at : undefined,
              feature_run_once_at: args.type === "task" ? args.feature_run_once_at : undefined,
              feature_timezone: args.type === "task" ? args.feature_timezone : undefined,
            });

            const location = args.global ? "global brain" : "project brain";

            return `Saved to ${location}

**Path:** \`${response.path}\`
**ID:** \`${response.id}\`
**Link:** \`${response.link}\`
**Title:** ${response.title}
**Type:** ${response.type}
**Status:** ${response.status}
**Tags:** ${args.tags?.length ? args.tags.join(", ") : "none"}

Use \`brain_recall\` with the path, ID, or title to retrieve it later.
Use the link \`${response.link}\` to reference this entry from other notes.
Use \`brain_update\` to change status (e.g., mark as completed).
Use \`brain_link\` to generate links to this entry from other notes.`;
          } catch (error) {
            return `Failed to save: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_recall
      // ========================================
      brain_recall: tool({
        description:
          "Retrieve a specific entry from the brain by path, ID, or title. Updates access statistics. Use include: ['attachments'] or include: ['attachments', 'attachment_text'] to include model-friendly attachment metadata for pasted-image/local-PDF workflows.",
        args: {
          path: tool.schema
            .string()
            .optional()
            .describe("Path or ID (8-char alphanumeric) to the note"),
          title: tool.schema
            .string()
            .optional()
            .describe("Title to search for (exact match)"),
          include: tool.schema
            .array(tool.schema.enum(["attachments", "attachment_text"]))
            .optional()
            .describe("Optional related data to include. Use ['attachments'] for attachment metadata or ['attachments','attachment_text'] when you need derived text references for PDFs/images."),
        },
        async execute(args) {
          if (!args.path && !args.title) {
            return "Please provide a path, ID, or title to recall";
          }

          try {
            // If title provided, search first
            let entryPath = args.path;
            if (!entryPath && args.title) {
              const searchResult = await apiRequest<{
                results: Array<{ path: string; title: string }>;
              }>("POST", "/search", {
                query: args.title,
                limit: 5,
              });

              const exactMatch = searchResult.results.find(
                (r) => r.title === args.title
              );
              if (exactMatch) {
                entryPath = exactMatch.path;
              } else if (searchResult.results.length > 0) {
                const suggestions = searchResult.results
                  .slice(0, 5)
                  .map((r) => `- "${r.title}" (Path: \`${r.path}\`)`)
                  .join("\n");
                return `No exact match for: "${args.title}"\n\n**Did you mean:**\n${suggestions}\n\nUse \`brain_recall\` with the exact path to retrieve a specific entry.`;
              } else {
                return `No entry found matching title: "${args.title}"`;
              }
            }

            const response = await apiRequest<{
              id: string;
              path: string;
              title: string;
              type: string;
              status: string;
              content: string;
              tags: string[];
              access_count?: number;
              backlinks?: Array<{ id: string; title: string; path: string }>;
              user_original_request?: string;
              attachments?: AttachmentReference[];
            }>("GET", `/entries/${entryPath}`, undefined, {
              include: args.include?.length ? args.include.join(",") : undefined,
            });

            const backlinkLinks =
              response.backlinks && response.backlinks.length > 0
                ? response.backlinks.map((b) => `[${b.title}](${b.id})`)
                : [];

            return `## ${response.title}

**Path:** \`${response.path}\`
**ID:** \`${response.id}\`
**Link:** \`[${response.title}](${response.id})\`
**Type:** ${response.type}
**Status:** ${response.status}
**Tags:** ${response.tags?.join(", ") || "none"}
**Access Count:** ${response.access_count ?? 1}
${backlinkLinks.length > 0 ? `**Backlinks:** ${backlinkLinks.join(", ")}` : ""}
${response.user_original_request ? `**User Original Request:** ${response.user_original_request}` : ""}
${formatAttachmentReferences(response.attachments)}

---

${response.content}`;
          } catch (error) {
            const identifier = args.path || args.title;
            return `No entry found${args.path ? ` at path: ${args.path}` : ` matching title: "${args.title}"`}: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_upload
      // ========================================
      brain_attachment_upload: tool({
        description: `Upload a local file as a first-class Brain attachment.

Use this for pasted-image or local-PDF workflows: save the file locally, upload it with this tool, then attach the returned attachment_id to an entry with brain_attachment_attach. The file is sent as multipart/form-data; content is not base64 encoded.`,
        args: {
          project_id: tool.schema.string().describe("Project that owns the uploaded attachment"),
          file_path: tool.schema.string().describe("Absolute or relative path to the local file to upload"),
          metadata: tool.schema.object({}).optional().describe("Optional JSON metadata stored with the attachment, e.g. {kind:'source-pdf', description:'original document'}"),
        },
        async execute(args) {
          if (!args.project_id || !args.file_path) {
            return "Please provide project_id and file_path";
          }

          try {
            const response = await attachmentUploadRequest(args.project_id, args.file_path, args.metadata);
            return `Uploaded attachment

${formatAttachment(response.attachment)}

Next: attach it to an entry with \`brain_attachment_attach\` using attachment_id \`${response.attachment.id}\`.

Workflow tip: for pasted images or local PDFs, upload the local file here, attach it to the relevant entry, then use \`brain_recall\` with include: ["attachments"] or \`brain_attachment_text\` for extracted text when available.`;
          } catch (error) {
            return `Attachment upload failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_attach
      // ========================================
      brain_attachment_attach: tool({
        description: "Attach an existing Brain attachment to an entry with optional role and caption metadata.",
        args: {
          project_id: tool.schema.string().describe("Project containing the entry and attachment"),
          entry_id: tool.schema.string().describe("Entry ID or path to attach to"),
          attachment_id: tool.schema.string().describe("Attachment ID returned by brain_attachment_upload or brain_attachment_list"),
          role: tool.schema.string().optional().describe("Optional attachment role, e.g. source, inline, image, pdf"),
          caption: tool.schema.string().optional().describe("Optional model-friendly caption describing the attachment"),
        },
        async execute(args) {
          if (!args.project_id || !args.entry_id || !args.attachment_id) {
            return "Please provide project_id, entry_id, and attachment_id";
          }

          const attachment: Record<string, unknown> = { id: args.attachment_id };
          if (args.role) attachment.role = args.role;
          if (args.caption) attachment.caption = args.caption;

          try {
            const response = await apiRequest<{
              path: string;
              entry_id: string;
              attachments: AttachmentReference[];
            }>("POST", `/entries/${args.entry_id}/attachments`, { attachment }, { project_id: args.project_id });

            return `Attached attachment ${args.attachment_id} to entry ${args.entry_id}

**Entry Path:** ${response.path}
${formatAttachmentReferences(response.attachments)}

Use \`brain_recall\` with include: ["attachments"] to view attachment metadata with the entry.`;
          } catch (error) {
            return `Attachment attach failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_detach
      // ========================================
      brain_attachment_detach: tool({
        description: "Detach an attachment from an entry. Provide role when detaching a role-specific reference.",
        args: {
          project_id: tool.schema.string().describe("Project containing the entry and attachment"),
          entry_id: tool.schema.string().describe("Entry ID or path to detach from"),
          attachment_id: tool.schema.string().describe("Attachment ID to detach"),
          role: tool.schema.string().optional().describe("Optional role to detach"),
        },
        async execute(args) {
          if (!args.project_id || !args.entry_id || !args.attachment_id) {
            return "Please provide project_id, entry_id, and attachment_id";
          }

          try {
            const response = await apiRequest<{
              path: string;
              entry_id: string;
              attachments: AttachmentReference[];
            }>(
              "DELETE",
              `/entries/${args.entry_id}/attachments/${encodeURIComponent(args.attachment_id)}`,
              undefined,
              { project_id: args.project_id, role: args.role }
            );

            return `Detached attachment ${args.attachment_id} from entry ${args.entry_id}

Remaining:${formatAttachmentReferences(response.attachments) || " none"}`;
          } catch (error) {
            return `Attachment detach failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_list
      // ========================================
      brain_attachment_list: tool({
        description: "List attachments available in a project, including metadata and derived artifacts.",
        args: {
          project_id: tool.schema.string().describe("Project whose attachments should be listed"),
        },
        async execute(args) {
          if (!args.project_id) {
            return "Please provide project_id";
          }

          try {
            const response = await apiRequest<{ attachments: Attachment[]; total: number }>(
              "GET",
              "/attachments",
              undefined,
              { project_id: args.project_id }
            );

            if (!response.attachments || response.attachments.length === 0) {
              return `No attachments found for project ${args.project_id}`;
            }

            return [`Attachments (${response.total})`, "", ...response.attachments.map(formatAttachment)].join("\n");
          } catch (error) {
            return `Attachment list failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_get
      // ========================================
      brain_attachment_get: tool({
        description: "Get attachment metadata, download/text URLs, and derived artifact references.",
        args: {
          project_id: tool.schema.string().describe("Project containing the attachment"),
          attachment_id: tool.schema.string().describe("Attachment ID to retrieve"),
        },
        async execute(args) {
          if (!args.project_id || !args.attachment_id) {
            return "Please provide project_id and attachment_id";
          }

          try {
            const response = await apiRequest<Attachment>(
              "GET",
              `/attachments/${encodeURIComponent(args.attachment_id)}`,
              undefined,
              { project_id: args.project_id }
            );
            return `${formatAttachment(response)}

Use \`brain_attachment_text\` with this attachment_id to retrieve extracted text when available.`;
          } catch (error) {
            return `Attachment get failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_extract
      // ========================================
      brain_attachment_extract: tool({
        description: "Trigger server-side media-to-text extraction for an attachment and return extraction status, provider/model, reason, and derived text metadata.",
        args: {
          project_id: tool.schema.string().describe("Project containing the attachment"),
          attachment_id: tool.schema.string().describe("Attachment ID whose text extraction should be triggered"),
          entry_id: tool.schema.string().optional().describe("Optional linked entry ID/path for extraction context"),
          metadata: tool.schema.object({}).optional().describe("Optional extraction metadata to pass through to the backend"),
        },
        async execute(args) {
          if (!args.project_id || !args.attachment_id) {
            return "Please provide project_id and attachment_id";
          }

          const body: Record<string, unknown> = {
            attachment_id: args.attachment_id,
          };
          if (args.entry_id) body.entry_id = args.entry_id;
          if (args.metadata && Object.keys(args.metadata).length > 0) {
            body.metadata = args.metadata;
          }

          try {
            const response = await attachmentExtractRequest(args.project_id, args.attachment_id, body);
            return `${formatAttachmentExtractionResult(response)}

Use \`brain_attachment_text\` with attachment_id \`${args.attachment_id}\` to retrieve extracted text when status is ready.`;
          } catch (error) {
            return `Attachment extraction failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_text
      // ========================================
      brain_attachment_text: tool({
        description: "Retrieve extracted plain text for an attachment, useful for local PDF/image OCR workflows after upload.",
        args: {
          project_id: tool.schema.string().describe("Project containing the attachment"),
          attachment_id: tool.schema.string().describe("Attachment ID whose extracted text should be retrieved"),
        },
        async execute(args) {
          if (!args.project_id || !args.attachment_id) {
            return "Please provide project_id and attachment_id";
          }

          try {
            const text = await attachmentTextRequest(args.project_id, args.attachment_id);
            if (!text.trim()) {
              return `No extracted text is available for attachment ${args.attachment_id}`;
            }
            return `## Attachment Text: ${args.attachment_id}

${text}`;
          } catch (error) {
            return `Attachment text failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_attachment_download
      // ========================================
      brain_attachment_download: tool({
        description: "Download raw attachment bytes to a local output path. Use this when an agent needs the exact original image, PDF, or media file for later processing.",
        args: {
          project_id: tool.schema.string().describe("Project containing the attachment"),
          attachment_id: tool.schema.string().describe("Attachment ID whose raw content should be downloaded"),
          output_path: tool.schema.string().describe("Local path where the downloaded bytes should be written"),
        },
        async execute(args) {
          if (!args.project_id || !args.attachment_id || !args.output_path) {
            return "Please provide project_id, attachment_id, and output_path";
          }

          try {
            await attachmentDownloadRequest(args.project_id, args.attachment_id, args.output_path);
            return `Downloaded attachment ${args.attachment_id} to ${args.output_path}`;
          } catch (error) {
            return `Attachment download failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_search
      // ========================================
      brain_search: tool({
        description:
          "Search the brain using full-text search. Finds entries matching your query.",
        args: {
          query: tool.schema.string().describe("Search query"),
          type: tool.schema
            .enum(ENTRY_TYPES)
            .optional()
            .describe("Filter by entry type"),
          status: tool.schema
            .enum(ENTRY_STATUSES)
            .optional()
            .describe(
              "Filter by status (e.g., 'active', 'completed', 'in_progress')"
            ),
          feature_id: tool.schema
            .string()
            .optional()
            .describe("Filter by feature group ID (e.g., 'auth-system', 'dark-mode')"),
          tags: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Filter by tags (AND logic - matches entries with all specified tags)"),
          limit: tool.schema
            .number()
            .optional()
            .describe("Maximum results (default: 10)"),
          global: tool.schema
            .boolean()
            .optional()
            .describe("Search only global entries"),
          project: tool.schema
            .string()
            .optional()
            .describe(
              "Filter by project ID/name (e.g., 'brain-api', 'my-project'). If not provided, searches all projects."
            ),
          priority: tool.schema
            .enum(["high", "medium", "low"])
            .optional()
            .describe("Filter by priority level"),
          strategy: tool.schema
            .enum(["fts", "exact", "like", "semantic", "hybrid"])
            .optional()
            .describe("Search strategy: 'fts' (full-text, default), 'exact' (exact phrase), 'like' (substring/wildcard), 'semantic' (embedding-based), or 'hybrid' (combined FTS + semantic)"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              results: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
                status: string;
                snippet: string;
              }>;
              total: number;
            }>("POST", "/search", {
              query: args.query,
              type: args.type,
              status: args.status,
              feature_id: args.feature_id,
              tags: args.tags,
              limit: args.limit ?? 10,
              global: args.global,
              project: args.project,
              priority: args.priority,
              strategy: args.strategy,
            });

            if (response.results.length === 0) {
              return `No entries found matching "${args.query}"`;
            }

            const lines = [
              `## Search Results for "${args.query}"`,
              "",
              `Found ${response.total} entries:`,
              "",
            ];

            for (const result of response.results) {
              lines.push(`### ${result.title}`);
              lines.push(`\`${result.path}\` | ${result.type} | ${result.status}`);
              if (result.snippet) lines.push(`> ${result.snippet}...`);
              lines.push("");
            }

            return lines.join("\n");
          } catch (error) {
            return `Search failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_list
      // ========================================
      brain_list: tool({
        description: `List entries in the brain with optional filtering by type, status, and filename.

Filename filtering supports:
- Exact match: "abc12def" finds entry with that exact ID
- Wildcard patterns: "abc*" (prefix), "*def" (suffix), "abc*def" (contains)`,
        args: {
          type: tool.schema
            .enum(ENTRY_TYPES)
            .optional()
            .describe("Filter by entry type"),
          status: tool.schema
            .enum(ENTRY_STATUSES)
            .optional()
            .describe(
              "Filter by status (e.g., 'active', 'completed', 'in_progress')"
            ),
          feature_id: tool.schema
            .string()
            .optional()
            .describe("Filter by feature group ID (e.g., 'auth-system', 'dark-mode')"),
          tags: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Filter by tags (OR logic - matches entries with any of the specified tags)"),
          filename: tool.schema
            .string()
            .optional()
            .describe(
              "Filter by filename/ID. Supports exact match or wildcard patterns with '*'"
            ),
          limit: tool.schema
            .number()
            .optional()
            .describe("Maximum entries to return (default: 20)"),
          global: tool.schema
            .boolean()
            .optional()
            .describe("List only global entries"),
          sortBy: tool.schema
            .enum(["created", "modified", "priority"])
            .optional()
            .describe(
              "Sort order: 'created' (default), 'modified', or 'priority' (high first)"
            ),
          project: tool.schema
            .string()
            .optional()
            .describe(
              "Filter by project ID/name (e.g., 'brain-api', 'my-project'). If not provided, returns entries from all projects."
            ),
          priority: tool.schema
            .enum(["high", "medium", "low"])
            .optional()
            .describe("Filter by priority level"),
          sortOrder: tool.schema
            .enum(["asc", "desc"])
            .optional()
            .describe("Sort direction: 'asc' (oldest/lowest first) or 'desc' (newest/highest first, default)"),
          offset: tool.schema
            .number()
            .optional()
            .describe("Pagination offset (skip N entries). Use with limit for pagination."),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
                status: string;
                priority?: string;
                access_count?: number;
              }>;
              total: number;
            }>(
              "GET",
              "/entries",
              undefined,
              {
                type: args.type,
                status: args.status,
                feature_id: args.feature_id,
                tags: args.tags?.join(","),
                filename: args.filename,
                limit: args.limit ?? 20,
                global: args.global,
                sortBy: args.sortBy,
                project: args.project,
                priority: args.priority,
                sortOrder: args.sortOrder,
                offset: args.offset,
              }
            );

            const filterParts = [
              args.type,
              args.status,
              args.filename ? `filename:${args.filename}` : null,
              args.tags?.length ? `tags:${args.tags.join(",")}` : null,
            ].filter(Boolean);
            const filterDesc = filterParts.join(", ");

            if (response.entries.length === 0) {
              return `No entries found${filterDesc ? ` matching: ${filterDesc}` : ""}`;
            }

            const lines = [
              `## Brain Entries${filterDesc ? ` (${filterDesc})` : ""}`,
              "",
              `Found ${response.total} entries:`,
              "",
            ];

            for (const entry of response.entries) {
              const globalBadge = entry.path.startsWith("global/") ? " [global]" : "";
              const priorityBadge = entry.priority
                ? entry.priority === "high"
                  ? " [HIGH]"
                  : entry.priority === "medium"
                    ? " [MED]"
                    : " [LOW]"
                : "";

              lines.push(`- **${entry.title}**${globalBadge}${priorityBadge}`);
              const priorityInfo = entry.priority ? ` | ${entry.priority}` : "";
              lines.push(
                `  \`${entry.path}\` (ID: \`${entry.id}\`) | ${entry.type} | ${entry.status}${priorityInfo} | ${entry.access_count ?? 0} accesses`
              );
            }

            return lines.join("\n");
          } catch (error) {
            return `List failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_inject
      // ========================================
      brain_inject: tool({
        description:
          "Search the brain and return relevant context. Use this to recall knowledge before starting a task.",
        args: {
          query: tool.schema
            .string()
            .describe("What context are you looking for?"),
          maxEntries: tool.schema
            .number()
            .optional()
            .describe("Maximum entries to include (default: 5)"),
          type: tool.schema
            .enum(ENTRY_TYPES)
            .optional()
            .describe("Filter by entry type"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              context: string;
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
              }>;
            }>("POST", "/inject", {
              query: args.query,
              maxEntries: args.maxEntries ?? 5,
              type: args.type,
            });

            if (!response.context || response.entries.length === 0) {
              return `No relevant brain context found for "${args.query}"`;
            }

            return response.context;
          } catch (error) {
            return `Failed to inject context: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_backlinks
      // ========================================
      brain_backlinks: tool({
        description: "Find entries that link TO a given entry (backlinks).",
        args: {
          path: tool.schema.string().describe("Path to the target note"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
              }>;
              total: number;
            }>("GET", `/entries/${args.path}/backlinks`);

            if (response.entries.length === 0) {
              return `No backlinks found for: ${args.path}`;
            }

            const lines = [
              `## Backlinks to: ${args.path}`,
              "",
              `Found ${response.total} entries linking to this note:`,
              "",
            ];

            for (const entry of response.entries) {
              lines.push(
                `- **${entry.title}** (\`${entry.path}\`) - ${entry.type}`
              );
            }

            return lines.join("\n");
          } catch (error) {
            return `Failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_outlinks
      // ========================================
      brain_outlinks: tool({
        description: "Find entries that a given entry links TO (outlinks).",
        args: {
          path: tool.schema.string().describe("Path to the source note"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
              }>;
              total: number;
            }>("GET", `/entries/${args.path}/outlinks`);

            if (response.entries.length === 0) {
              return `No outlinks found from: ${args.path}`;
            }

            const lines = [
              `## Outlinks from: ${args.path}`,
              "",
              `Found ${response.total} entries linked from this note:`,
              "",
            ];

            for (const entry of response.entries) {
              lines.push(
                `- **${entry.title}** (\`${entry.path}\`) - ${entry.type}`
              );
            }

            return lines.join("\n");
          } catch (error) {
            return `Failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_related
      // ========================================
      brain_related: tool({
        description:
          "Find entries that share linked notes with a given entry.",
        args: {
          path: tool.schema
            .string()
            .describe("Path to the note to find related entries for"),
          limit: tool.schema
            .number()
            .optional()
            .describe("Maximum results (default: 10)"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
              }>;
              total: number;
            }>("GET", `/entries/${args.path}/related`, undefined, {
              limit: args.limit ?? 10,
            });

            if (response.entries.length === 0) {
              return `No related entries found for: ${args.path}`;
            }

            const lines = [
              `## Related to: ${args.path}`,
              "",
              `Found ${response.total} entries sharing links:`,
              "",
            ];

            for (const entry of response.entries) {
              lines.push(
                `- **${entry.title}** (\`${entry.path}\`) - ${entry.type}`
              );
            }

            return lines.join("\n");
          } catch (error) {
            return `Failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_orphans
      // ========================================
      brain_orphans: tool({
        description:
          "Find entries with no incoming links (orphans). Useful for knowledge graph health.",
        args: {
          type: tool.schema
            .enum(ENTRY_TYPES)
            .optional()
            .describe("Filter by entry type"),
          limit: tool.schema
            .number()
            .optional()
            .describe("Maximum results (default: 20)"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
              }>;
              total: number;
              message: string;
            }>("GET", "/orphans", undefined, {
              type: args.type,
              limit: args.limit ?? 20,
            });

            if (response.entries.length === 0) {
              return `No orphan entries found${args.type ? ` of type "${args.type}"` : ""}`;
            }

            const lines = [
              `## Orphan Entries${args.type ? ` (${args.type})` : ""}`,
              "",
              `Found ${response.total} entries with no incoming links:`,
              "",
            ];

            for (const entry of response.entries) {
              lines.push(
                `- **${entry.title}** (\`${entry.path}\`) - ${entry.type}`
              );
            }

            lines.push("");
            lines.push(
              "*Consider linking these notes from related entries to improve knowledge graph connectivity.*"
            );

            return lines.join("\n");
          } catch (error) {
            return `Failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_stale
      // ========================================
      brain_stale: tool({
        description:
          "Find entries that may need verification (not verified in N days).",
        args: {
          days: tool.schema
            .number()
            .optional()
            .describe("Days threshold (default: 30)"),
          type: tool.schema
            .enum(ENTRY_TYPES)
            .optional()
            .describe("Filter by entry type"),
          limit: tool.schema
            .number()
            .optional()
            .describe("Maximum results (default: 20)"),
        },
        async execute(args) {
          const days = args.days ?? 30;

          try {
            const response = await apiRequest<{
              entries: Array<{
                id: string;
                path: string;
                title: string;
                type: string;
                daysSinceVerified: number | null;
              }>;
              total: number;
            }>("GET", "/stale", undefined, {
              days,
              type: args.type,
              limit: args.limit ?? 20,
            });

            if (response.entries.length === 0) {
              return `No stale entries found (all verified within ${days} days)`;
            }

            const lines = [
              `## Stale Entries (not verified in ${days} days)`,
              "",
              `Found ${response.total} entries needing verification:`,
              "",
            ];

            for (const entry of response.entries) {
              const daysSince =
                entry.daysSinceVerified !== null
                  ? `${entry.daysSinceVerified} days ago`
                  : "never";
              lines.push(`- **${entry.title}**`);
              lines.push(
                `  \`${entry.path}\` | Last verified: ${daysSince}`
              );
            }

            lines.push("");
            lines.push(
              "*Use `brain_verify` to mark entries as still accurate.*"
            );

            return lines.join("\n");
          } catch (error) {
            return `Failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_verify
      // ========================================
      brain_verify: tool({
        description:
          "Mark an entry as verified (still accurate). Updates the last_verified timestamp.",
        args: {
          path: tool.schema.string().describe("Path to the note to verify"),
        },
        async execute(args) {
          try {
            await apiRequest<{ message: string; path: string }>(
              "POST",
              `/entries/${args.path}/verify`
            );

            return `Verified: ${args.path}

Entry marked as still accurate. It will not appear in stale entry lists for 30 days.`;
          } catch (error) {
            return `Entry not found: ${args.path}`;
          }
        },
      }),

      // ========================================
      // brain_update
      // ========================================
      brain_update: tool({
        description: `Update an existing brain entry's status, title, dependencies, trigger configuration, or append content.

Use cases:
- Mark a plan as completed: brain_update(path: "...", status: "completed")
- Mark as in-progress: brain_update(path: "...", status: "in_progress")  
- Block with reason: brain_update(path: "...", status: "blocked", note: "Waiting on API design")
- Append progress notes: brain_update(path: "...", append: "## Progress\\n- Completed auth module")
- Update title: brain_update(path: "...", title: "New Title")
- Update dependencies: brain_update(path: "...", depends_on: ["task-id-1", "task-id-2"])
- Update feature dependencies: brain_update(path: "...", feature_depends_on: ["pre-feature"])
- Add a post-feature trigger: brain_update(path: "...", trigger: {event:"feature.completed", filter:{feature_id:"main-feature"}})
- Update tags: brain_update(path: "...", tags: ["tag1", "tag2"])
- Update priority: brain_update(path: "...", priority: "high")

Statuses: draft, active, in_progress, blocked, completed, validated, superseded, archived`,
        args: {
          path: tool.schema.string().describe("Path to the entry to update"),
          status: tool.schema
            .enum(ENTRY_STATUSES)
            .optional()
            .describe("New status for the entry"),
          title: tool.schema
            .string()
            .optional()
            .describe("New title for the entry"),
          append: tool.schema
            .string()
            .optional()
            .describe("Content to append to the entry body"),
          note: tool.schema
            .string()
            .optional()
            .describe("Short note to add (e.g., reason for status change)"),
          depends_on: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Task dependencies - list of task IDs or titles. Validated on update. Use format: 'task-id' for same project, 'project:task-id' for cross-project."),
          tags: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Replace tags array (overwrites existing tags)"),
          priority: tool.schema
            .enum(PRIORITIES)
            .optional()
            .describe("Update task priority"),
          feature_id: tool.schema
            .string()
            .optional()
            .describe("Feature group identifier (e.g., 'auth-system', 'payment-flow')"),
          feature_priority: tool.schema
            .enum(PRIORITIES)
            .optional()
            .describe("Priority for this feature group"),
          feature_depends_on: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Feature IDs this feature depends on. Use this for feature-to-feature ordering."),
          trigger: tool.schema
            .object({
              type: tool.schema.string().optional().describe("Trigger type for automation entries: event, cron, webhook, or session"),
              event: tool.schema.string().optional().describe("Event pattern to match, e.g. 'feature.completed', 'task.completed', or 'task.*'"),
              schedule: tool.schema.string().optional().describe("Cron schedule for cron automation triggers"),
              webhook: tool.schema.string().optional().describe("Webhook path/name for webhook-triggered automations"),
              filter: tool.schema.object({
                feature_id: tool.schema.string().optional().describe("Only match events for this feature ID"),
                project_id: tool.schema.string().optional().describe("Only match events for this project ID"),
                task_id: tool.schema.string().optional().describe("Only match events for this task ID"),
                runner_id: tool.schema.string().optional().describe("Only match events from this runner ID"),
                from_status: tool.schema.string().optional().describe("Only match status-change events from this status"),
                to_status: tool.schema.string().optional().describe("Only match status-change events to this status"),
              }).optional().describe("Event field filters. For post-feature tasks use {feature_id:'main-feature', project_id:'my-project'}."),
              once_per: tool.schema.string().optional().describe("Dedup key for automations, e.g. feature_id, session, or day"),
              cooldown: tool.schema.string().optional().describe("Minimum interval between automation firings, e.g. '5m' or '1h'"),
              max_concurrent: tool.schema.number().optional().describe("Maximum concurrent executions for this trigger"),
              ignore_automation_events: tool.schema.boolean().optional().describe("Whether to ignore events emitted by automations"),
            })
            .optional()
            .describe("Event trigger for inactive/active tasks or automation entries. For post-feature tasks use event='feature.completed' and filter.feature_id=<completed feature>."),
          action: tool.schema
            .object({
              type: tool.schema.string().optional().describe("Automation action type, e.g. create_task or script"),
              title_template: tool.schema.string().optional().describe("Template for generated task title"),
              prompt_template: tool.schema.string().optional().describe("Template for generated task prompt/content"),
              direct_prompt: tool.schema.string().optional().describe("Direct prompt for generated tasks"),
              command: tool.schema.string().optional().describe("Script command for script actions"),
              agent: tool.schema.string().optional().describe("Agent override for generated tasks"),
              model: tool.schema.string().optional().describe("Model override for generated tasks"),
              executor: tool.schema.string().optional().describe("Executor override for generated tasks"),
              target_workdir: tool.schema.string().optional().describe("Target workdir for generated tasks"),
            })
            .optional()
            .describe("Automation action config for automation entries."),
          retry: tool.schema
            .object({
              max_attempts: tool.schema.number().optional().describe("Maximum retry attempts"),
              backoff: tool.schema.string().optional().describe("Backoff policy or duration"),
              timeout: tool.schema.string().optional().describe("Action timeout"),
            })
            .optional()
            .describe("Automation retry policy for automation entries."),
          target_workdir: tool.schema
            .string()
            .optional()
            .describe("Update task execution directory (absolute path). PRIMARY USE CASE: Cross-project task filing - ensures task executes in the correct project. Task runner will try this first before fallback."),
          git_branch: tool.schema
            .string()
            .optional()
            .describe("Git branch to execute this task on"),
          merge_target_branch: tool.schema
            .string()
            .optional()
            .describe("Branch to merge completed work into"),
          merge_policy: tool.schema
            .enum(["prompt_only", "auto_pr", "auto_merge"])
            .optional()
            .describe("Merge behavior at completion (default: auto_merge)"),
          merge_strategy: tool.schema
            .enum(["squash", "merge", "rebase"])
            .optional()
            .describe("Merge strategy for auto-merge (default: squash)"),
          open_pr_before_merge: tool.schema
            .boolean()
            .optional()
            .describe("Open PR before merge when enabled (default: false)"),
          execution_mode: tool.schema
            .enum(["worktree", "current_branch"])
            .optional()
            .describe("Task execution mode (default: worktree)"),
          complete_on_idle: tool.schema
            .boolean()
            .optional()
            .describe("Mark task as completed when agent becomes idle (default: false)"),
          remote_branch_policy: tool.schema
            .enum(["keep", "delete"])
            .optional()
            .describe("Policy for remote branch after merge ('keep' or 'delete', default: delete)"),
          schedule: tool.schema
            .string()
            .optional()
            .describe("Cron schedule expression (e.g., '*/5 * * * *', '0 2 * * *'). When set, creates/updates linked cron entry."),
          schedule_enabled: tool.schema
            .boolean()
            .optional()
            .describe("Whether the schedule is active (default true when schedule exists). Set to false to pause scheduling."),
          max_runs: tool.schema
            .number()
            .optional()
            .describe("Maximum number of scheduled runs before auto-disabling. Omit or set to 0 for unlimited."),
          run_once_at: tool.schema
            .string()
            .optional()
            .describe("RFC3339 timestamp for one-time execution (e.g., '2025-12-01T00:00:00Z'). Task runs once at this time then schedule_enabled is set to false."),
          timezone: tool.schema
            .string()
            .optional()
            .describe("IANA timezone for schedule interpretation (e.g., 'America/Denver', 'Europe/London'). Applies to cron schedule and time window fields."),
          starts_at: tool.schema
            .string()
            .optional()
            .describe("RFC3339 timestamp for schedule window start. Task will not run before this time."),
          expires_at: tool.schema
            .string()
            .optional()
            .describe("RFC3339 timestamp for schedule window end. Task will not run after this time and schedule_enabled is set to false."),
          feature_schedule: tool.schema
            .string()
            .optional()
            .describe("Cron schedule for feature-level execution. Creates a feature_schedule gate task that triggers all tasks in the feature on this schedule."),
          feature_starts_at: tool.schema
            .string()
            .optional()
            .describe("RFC3339 timestamp for feature schedule window start. Feature gate task will not run before this time."),
          feature_expires_at: tool.schema
            .string()
            .optional()
            .describe("RFC3339 timestamp for feature schedule window end. Feature gate task will not run after this time."),
          feature_run_once_at: tool.schema
            .string()
            .optional()
            .describe("RFC3339 timestamp for one-time feature execution. Creates a feature_schedule gate task that runs once at this time."),
          feature_timezone: tool.schema
            .string()
            .optional()
            .describe("IANA timezone for feature schedule interpretation (e.g., 'America/Denver'). Applies to feature_schedule and feature time window fields."),
          direct_prompt: tool.schema
            .string()
            .optional()
            .describe("Direct prompt to execute, bypassing default skill workflow"),
          agent: tool.schema
            .string()
            .optional()
            .describe("Override agent for this task (e.g., 'explore', 'tdd-dev')"),
          model: tool.schema
            .string()
            .optional()
            .describe("Override model (format: 'provider/model-id')"),
          executor: tool.schema
            .enum(["opencode", "pi", "script"])
            .optional()
            .describe("Executor backend for this task. Runner must advertise the selected executor."),
          extensions: tool.schema
            .array(tool.schema.string())
            .optional()
            .describe("Additional executor extensions to load for this task."),
        },
        async execute(args) {
          const cleanArgs = sanitizeUpdateArgs(args as Record<string, unknown>);
          const updates: Record<string, unknown> = {};
          addNonEmptyStringFields(updates, cleanArgs, [
            "status", "title", "append", "note", "priority", "feature_id", "feature_priority",
            "target_workdir", "git_branch", "merge_target_branch", "merge_policy", "merge_strategy",
            "execution_mode", "remote_branch_policy", "schedule", "run_once_at", "timezone",
            "starts_at", "expires_at", "feature_schedule", "feature_starts_at", "feature_expires_at",
            "feature_run_once_at", "feature_timezone", "direct_prompt", "agent", "model", "executor",
          ]);
          addPresentFields(updates, cleanArgs, [
            "depends_on", "tags", "feature_depends_on", "trigger", "action", "retry",
            "open_pr_before_merge", "complete_on_idle", "schedule_enabled", "max_runs", "extensions",
          ]);

          if (Object.keys(updates).length === 0) {
            return `No updates specified. Provide at least one of: status, title, append, note, depends_on, tags, priority, feature_id, feature_priority, feature_depends_on, trigger, action, retry, target_workdir, git_branch, merge_target_branch, merge_policy, merge_strategy, open_pr_before_merge, execution_mode, complete_on_idle, remote_branch_policy, schedule, schedule_enabled, max_runs, run_once_at, timezone, starts_at, expires_at, feature_schedule, feature_starts_at, feature_expires_at, feature_run_once_at, feature_timezone, direct_prompt, agent, model, executor, extensions`;
          }

          try {
            const response = await apiRequest<{
              path: string;
              title: string;
              status: string;
              changes: string[];
            }>("PATCH", `/entries/${args.path}`, updates);

            const changes: string[] = [];
            if (cleanArgs.status) changes.push(`Status: -> ${cleanArgs.status}`);
            if (cleanArgs.title) changes.push(`Title: -> "${cleanArgs.title}"`);
            if (cleanArgs.note) changes.push(`Note: "${cleanArgs.note}"`);
            if (cleanArgs.append)
              changes.push(`Appended ${(cleanArgs.append as string).length} characters`);
            if (cleanArgs.depends_on)
              changes.push(`Dependencies: ${(cleanArgs.depends_on as unknown[]).length} task(s)`);
            if (cleanArgs.tags !== undefined)
              changes.push(`Tags: ${(cleanArgs.tags as unknown[]).length > 0 ? (cleanArgs.tags as string[]).join(", ") : "(cleared)"}`);
            if (cleanArgs.priority)
              changes.push(`Priority: ${cleanArgs.priority}`);
            if (cleanArgs.feature_id)
              changes.push(`Feature ID: ${cleanArgs.feature_id}`);
            if (cleanArgs.feature_priority)
              changes.push(`Feature Priority: ${cleanArgs.feature_priority}`);
            if (cleanArgs.feature_depends_on)
              changes.push(`Feature Dependencies: ${(cleanArgs.feature_depends_on as unknown[]).length} feature(s)`);
            if (cleanArgs.trigger !== undefined)
              changes.push(`Trigger: set`);
            if (cleanArgs.action !== undefined)
              changes.push(`Action: set`);
            if (cleanArgs.retry !== undefined)
              changes.push(`Retry: set`);
            if (cleanArgs.target_workdir)
              changes.push(`Target Workdir: ${cleanArgs.target_workdir}`);
            if (cleanArgs.git_branch)
              changes.push(`Git Branch: ${cleanArgs.git_branch}`);
            if (cleanArgs.merge_target_branch)
              changes.push(`Merge Target Branch: ${cleanArgs.merge_target_branch}`);
            if (cleanArgs.merge_policy)
              changes.push(`Merge Policy: ${cleanArgs.merge_policy}`);
            if (cleanArgs.merge_strategy)
              changes.push(`Merge Strategy: ${cleanArgs.merge_strategy}`);
            if (cleanArgs.open_pr_before_merge !== undefined)
              changes.push(`Open PR Before Merge: ${cleanArgs.open_pr_before_merge}`);
            if (cleanArgs.execution_mode)
              changes.push(`Execution Mode: ${cleanArgs.execution_mode}`);

            if (cleanArgs.complete_on_idle !== undefined)
              changes.push(`Complete On Idle: ${cleanArgs.complete_on_idle}`);
            if (cleanArgs.remote_branch_policy)
              changes.push(`Remote Branch Policy: ${cleanArgs.remote_branch_policy}`);
            if (cleanArgs.schedule)
              changes.push(`Schedule: ${cleanArgs.schedule}`);
            if (cleanArgs.schedule_enabled !== undefined)
              changes.push(`Schedule Enabled: ${cleanArgs.schedule_enabled}`);
            if (cleanArgs.max_runs !== undefined)
              changes.push(`Max Runs: ${cleanArgs.max_runs === 0 ? "unlimited" : cleanArgs.max_runs}`);
            if (cleanArgs.run_once_at)
              changes.push(`Run Once At: ${cleanArgs.run_once_at}`);
            if (cleanArgs.timezone)
              changes.push(`Timezone: ${cleanArgs.timezone}`);
            if (cleanArgs.starts_at)
              changes.push(`Starts At: ${cleanArgs.starts_at}`);
            if (cleanArgs.expires_at)
              changes.push(`Expires At: ${cleanArgs.expires_at}`);
            if (cleanArgs.feature_schedule)
              changes.push(`Feature Schedule: ${cleanArgs.feature_schedule}`);
            if (cleanArgs.feature_starts_at)
              changes.push(`Feature Starts At: ${cleanArgs.feature_starts_at}`);
            if (cleanArgs.feature_expires_at)
              changes.push(`Feature Expires At: ${cleanArgs.feature_expires_at}`);
            if (cleanArgs.feature_run_once_at)
              changes.push(`Feature Run Once At: ${cleanArgs.feature_run_once_at}`);
            if (cleanArgs.feature_timezone)
              changes.push(`Feature Timezone: ${cleanArgs.feature_timezone}`);
            if (cleanArgs.direct_prompt)
              changes.push(`Direct Prompt: set`);
            if (cleanArgs.agent)
              changes.push(`Agent: ${cleanArgs.agent}`);
            if (cleanArgs.model)
              changes.push(`Model: ${cleanArgs.model}`);
            if (cleanArgs.executor)
              changes.push(`Executor: ${cleanArgs.executor}`);
            if (cleanArgs.extensions)
              changes.push(`Extensions: ${(cleanArgs.extensions as unknown[]).length} extension(s)`);

            return `Updated: ${args.path}

**Changes:**
${changes.map((c) => `- ${c}`).join("\n")}

**Current Status:** ${response.status}
**Title:** ${response.title}

Use \`brain_recall\` to view the full entry.`;
          } catch (error) {
            return `Failed to update: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_bulk_update
      // ========================================
      brain_bulk_update: tool({
        description: `Bulk update multiple brain entries in a single operation.

Supports two modes:
1. **Filter mode:** Match entries by criteria and apply the same updates to all
   - Use \`filter\` + \`updates\` parameters
   - Great for feature reassignment: move all tasks from one feature to another
2. **Explicit mode:** Update specific entries with per-entry updates
   - Use \`entries\` parameter (array of {path, updates})
   - Great for batch status changes on known entries

Primary use case: reassigning tasks across features or projects in bulk.
Important: omit filter fields you do not want to match. Do not include \`priority\` in the filter unless you intentionally want to update only one priority.

Examples:
- Reassign all tasks in a feature: filter={feature_id:"old-feature"}, updates={feature_id:"new-feature"}
- Bulk status change: filter={status:"pending",project:"my-proj"}, updates={status:"cancelled"}
- Per-entry updates: entries=[{path:"projects/x/task/abc.md",updates:{status:"completed"}}]`,
        args: {
          filter: tool.schema
            .object({
              feature_id: tool.schema.string().optional().describe("Filter by feature group ID"),
              project: tool.schema.string().optional().describe("Filter by project ID"),
              type: tool.schema.enum(ENTRY_TYPES).optional().describe("Filter by entry type"),
              status: tool.schema.enum(ENTRY_STATUSES).optional().describe("Filter by status"),
              tags: tool.schema.array(tool.schema.string()).optional().describe("Filter by tags"),
              priority: tool.schema.enum(PRIORITIES).optional().describe("Filter by priority"),
            })
            .optional()
            .describe("Filter criteria to select entries (use with 'updates')"),
          updates: tool.schema
            .object({
              status: tool.schema.enum(ENTRY_STATUSES).optional().describe("New status"),
              title: tool.schema.string().optional().describe("New title"),
              append: tool.schema.string().optional().describe("Content to append"),
              note: tool.schema.string().optional().describe("Short note to add"),
              tags: tool.schema.array(tool.schema.string()).optional().describe("Replace tags"),
              priority: tool.schema.enum(PRIORITIES).optional().describe("New priority"),
              feature_id: tool.schema.string().optional().describe("New feature group ID"),
              feature_priority: tool.schema.enum(PRIORITIES).optional().describe("New feature priority"),
              feature_depends_on: tool.schema.array(tool.schema.string()).optional().describe("New feature dependencies"),
              target_workdir: tool.schema.string().optional().describe("New target workdir"),
              git_branch: tool.schema.string().optional().describe("New git branch"),
              merge_target_branch: tool.schema.string().optional().describe("New merge target branch"),
              merge_policy: tool.schema.enum(["prompt_only", "auto_pr", "auto_merge"]).optional().describe("New merge policy"),
              merge_strategy: tool.schema.enum(["squash", "merge", "rebase"]).optional().describe("New merge strategy"),
              execution_mode: tool.schema.enum(["worktree", "current_branch"]).optional().describe("New execution mode"),
              direct_prompt: tool.schema.string().optional().describe("New direct prompt"),
              agent: tool.schema.string().optional().describe("New agent override"),
              model: tool.schema.string().optional().describe("New model override"),
            })
            .optional()
            .describe("Updates to apply to all matched entries (use with 'filter')"),
          entries: tool.schema
            .array(
              tool.schema.object({
                path: tool.schema.string().describe("Entry path"),
                updates: tool.schema.object({
                  status: tool.schema.enum(ENTRY_STATUSES).optional().describe("New status"),
                  title: tool.schema.string().optional().describe("New title"),
                  append: tool.schema.string().optional().describe("Content to append"),
                  note: tool.schema.string().optional().describe("Short note to add"),
                  tags: tool.schema.array(tool.schema.string()).optional().describe("Replace tags"),
                  priority: tool.schema.enum(PRIORITIES).optional().describe("New priority"),
                  feature_id: tool.schema.string().optional().describe("New feature group ID"),
                  feature_priority: tool.schema.enum(PRIORITIES).optional().describe("New feature priority"),
                  feature_depends_on: tool.schema.array(tool.schema.string()).optional().describe("New feature dependencies"),
                }).describe("Updates for this specific entry"),
              })
            )
            .optional()
            .describe("Explicit list of entries with per-entry updates"),
          dry_run: tool.schema
            .boolean()
            .optional()
            .describe("Preview changes without applying them (default: false)"),
        },
        async execute(args) {
          const filter = sanitizeObjectArg(args.filter);
          const updates = sanitizeUpdateArgs((args.updates ?? {}) as Record<string, unknown>);

          // Validate: must have (filter+updates) XOR entries
          const hasFilter = hasFields(filter);
          const hasUpdates = hasFields(updates);
          const hasEntries = args.entries && args.entries.length > 0;

          if (!hasFilter && !hasEntries) {
            return "Must provide either 'filter'+'updates' or 'entries'. Use 'filter' to match entries by criteria, or 'entries' for specific paths.";
          }
          if (hasFilter && hasEntries) {
            return "Cannot use both 'filter' and 'entries' modes. Choose one: filter-based (filter+updates) or explicit (entries).";
          }
          if (hasFilter && !hasUpdates) {
            return "When using 'filter' mode, 'updates' is required to specify what changes to apply.";
          }

          try {
            const body: Record<string, unknown> = {
              dry_run: args.dry_run ?? false,
            };

            if (hasFilter) {
              body.filter = filter;
              body.updates = updates;
            } else {
              body.entries = sanitizeBulkUpdateEntries(args.entries);
            }

            const response = await apiRequest<{
              updated: number;
              failed: number;
              total: number;
              dry_run: boolean;
              results: Array<{
                path: string;
                id: string;
                title: string;
                status: string;
                error?: string;
              }>;
            }>("POST", "/entries/bulk-update", body);

            const lines: string[] = [];

            if (response.dry_run) {
              lines.push("## Bulk Update Preview (dry run)");
            } else {
              lines.push("## Bulk Update Results");
            }
            lines.push("");
            lines.push(`**Updated:** ${response.updated} | **Failed:** ${response.failed} | **Total matched:** ${response.total}`);
            lines.push("");

            // Show successful updates
            const successes = response.results.filter(r => r.status === "ok");
            if (successes.length > 0) {
              lines.push(response.dry_run ? "### Would update:" : "### Updated:");
              for (const result of successes) {
                lines.push(`- **${result.title}** (\`${result.path}\`)`);
              }
              lines.push("");
            }

            // Show failures
            const failures = response.results.filter(r => r.status !== "ok");
            if (failures.length > 0) {
              lines.push("### Failed:");
              for (const result of failures) {
                lines.push(`- **${result.title}** (\`${result.path}\`): ${result.error || "unknown error"}`);
              }
              lines.push("");
            }

            if (response.dry_run) {
              lines.push("*This was a dry run. No changes were applied. Remove `dry_run: true` to apply.*");
            }

            return lines.join("\n");
          } catch (error) {
            return `Bulk update failed: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_move
      // ========================================
      brain_move: tool({
        description: `Move a brain entry to a different project.

IMPORTANT LIMITATIONS:
- ✅ Works for: tasks, summaries, reports, plans, and other note types
- ❌ Cannot move entries currently in 'in_progress' status

Use cases:
- Bulk reassign tasks to a different project
- Move a task filed in the wrong project
- Reorganize project structure

Example: brain_move({ path: "projects/old/task/abc12def.md", project: "new-project" })`,
        args: {
          path: tool.schema
            .string()
            .describe("Path to the entry to move (e.g., 'projects/old/task/abc12def.md')"),
          project: tool.schema
            .string()
            .describe("Target project ID to move the entry to (e.g., 'my-other-project')"),
        },
        async execute(args) {
          if (!args.path || !args.project) {
            return "Please provide both path and target project";
          }

          try {
            const response = await apiRequest<{
              oldPath: string;
              newPath: string;
              project: string;
              id: string;
              title: string;
            }>("POST", `/entries/${args.path}/move`, {
              project: args.project,
            });

            return `Moved entry to project: ${response.project}

**Title:** ${response.title}
**ID:** \`${response.id}\`
**Old Path:** \`${response.oldPath}\`
**New Path:** \`${response.newPath}\`

Use \`brain_recall\` with the new path to verify.`;
          } catch (error) {
            return `Failed to move: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_stats
      // ========================================
      brain_stats: tool({
        description: "Get statistics about the brain storage.",
        args: {
          global: tool.schema
            .boolean()
            .optional()
            .describe("Show only global entries stats"),
        },
        async execute(args) {
          try {
            const response = await apiRequest<{
              dataAvailable: boolean;
              dataVersion: string | null;
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
            }>("GET", "/stats", undefined, {
              global: args.global,
            });

            const lines = [
              "## Brain Statistics",
              "",
              "### System",
              `- **Data Store:** ${response.dataAvailable ? `v${response.dataVersion}` : "Not available"}`,
              `- **Notebook:** ${response.notebookExists ? `${response.brainDir}` : "Not initialized"}`,
              `- **Database:** ${response.dbPath}`,
              "",
              "### Entries",
              `- **Total:** ${response.totalEntries}`,
              `- **Global:** ${response.globalEntries}`,
              `- **Project:** ${response.projectEntries}`,
              "",
              "### By Type",
            ];

            const sortedTypes = Object.entries(response.byType).sort(
              (a, b) => b[1] - a[1]
            );
            for (const [type, count] of sortedTypes) {
              lines.push(`- ${type}: ${count}`);
            }

            lines.push("");
            lines.push("### Health");
            lines.push(`- **Orphan Notes:** ${response.orphanCount}`);
            lines.push("");
            lines.push("### Access Tracking");
            lines.push(`- **Tracked Entries:** ${response.trackedEntries}`);
            lines.push(`- **Stale (>30 days):** ${response.staleCount}`);

            return lines.join("\n");
          } catch (error) {
            return `Failed to get stats: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_delete
      // ========================================
      brain_delete: tool({
        description:
          "Delete a specific entry from the brain by path. Use with caution.",
        args: {
          path: tool.schema.string().describe("Path to the entry to delete"),
          confirm: tool.schema
            .boolean()
            .describe("Must be true to confirm deletion"),
        },
        async execute(args) {
          if (!args.confirm) {
            return "Please set `confirm: true` to delete the entry";
          }

          try {
            await apiRequest<{ message: string; path: string }>(
              "DELETE",
              `/entries/${args.path}`,
              undefined,
              { confirm: "true" }
            );

            return `Deleted: ${args.path}`;
          } catch (error) {
            return `Entry not found: ${args.path}`;
          }
        },
      }),

      // ========================================
      // brain_link
      // ========================================
      brain_link: tool({
        description:
          "Generate a markdown link to a brain entry. Use this when referencing other brain entries to ensure proper link resolution with mkdnflow.",
        args: {
          title: tool.schema.string().optional().describe("Title to search for"),
          path: tool.schema
            .string()
            .optional()
            .describe("Direct path or ID (8-char alphanumeric) to the entry"),
          withTitle: tool.schema
            .boolean()
            .optional()
            .describe("Include title in link (default: true)"),
        },
        async execute(args) {
          if (!args.path && !args.title) {
            return JSON.stringify({
              error: "Please provide either a path, ID, or title to generate a link",
            });
          }

          try {
            const response = await apiRequest<{
              link: string;
              id: string;
              path: string;
              title: string;
            }>("POST", "/link", {
              title: args.title,
              path: args.path,
              withTitle: args.withTitle,
            });

            return JSON.stringify(response);
          } catch (error) {
            return JSON.stringify({
              error: error instanceof Error ? error.message : String(error),
            });
          }
        },
      }),

      // ========================================
      // brain_section
      // ========================================
      brain_section: tool({
        description: `Retrieve a specific section's FULL CONTENT from a brain plan by section title.

Use this when you need the detailed implementation spec for your assigned task.
Returns the exact section content including all subsections, code examples, and acceptance criteria.

Example: brain_section({ planId: "projects/abc/plan/auth.md", sectionTitle: "JWT Middleware" })

This is more precise than brain_inject (which uses fuzzy search) - it extracts the exact section you need.`,
        args: {
          planId: tool.schema
            .string()
            .describe(
              "Brain plan path (from orchestration context or brain_plan_sections)"
            ),
          sectionTitle: tool.schema
            .string()
            .describe("Section title to retrieve (can be partial match)"),
          includeSubsections: tool.schema
            .boolean()
            .optional()
            .describe("Include nested subsections (default: true)"),
        },
        async execute(args) {
          try {
            const encodedTitle = encodeURIComponent(args.sectionTitle);
            const response = await apiRequest<{
              title: string;
              content: string;
              level: number;
              line: number;
            }>(
              "GET",
              `/entries/${args.planId}/sections/${encodedTitle}`,
              undefined,
              {
                includeSubsections:
                  args.includeSubsections !== false ? "true" : "false",
              }
            );

            return JSON.stringify(
              {
                planId: args.planId,
                sectionTitle: response.title,
                content: response.content,
                lineRange: { start: response.line },
                contentLength: response.content.length,
              },
              null,
              2
            );
          } catch (error) {
            return JSON.stringify(
              {
                error: `Section "${args.sectionTitle}" not found in plan`,
                hint: "Use brain_plan_sections to list available sections",
              },
              null,
              2
            );
          }
        },
      }),

      // ========================================
      // brain_check_connection
      // ========================================
      brain_check_connection: tool({
        description: `Check if the Brain API server is running and accessible.

Use this tool FIRST if you're unsure whether brain tools will work.
Returns connection status, server version, and helpful troubleshooting info if unavailable.

This is useful to:
- Verify the brain is available before starting a task that needs it
- Diagnose why other brain tools are failing
- Get instructions for starting the brain server`,
        args: {},
        async execute() {
          // Force a fresh health check
          connectionState.lastCheck = 0;
          const health = await checkBrainHealth();

          if (health.available) {
            return `**Brain API Status: CONNECTED**

- **Server URL:** ${BRAIN_API_URL}
- **Version:** ${health.version || "unknown"}
- **Status:** Ready to use

All brain tools (save, recall, search, inject, etc.) are available.`;
          } else {
            return `**Brain API Status: UNAVAILABLE**

- **Server URL:** ${BRAIN_API_URL}
- **Error:** ${health.lastError || "Unknown error"}

**To start the Brain API server:**
\`\`\`bash
brain start
\`\`\`

**To check server status:**
\`\`\`bash
brain status
\`\`\`

**To view logs:**
\`\`\`bash
brain logs
\`\`\`

Brain tools will not work until the server is running.
You can proceed with tasks that don't require brain functionality.`;
          }
        },
      }),

      // ========================================
      // brain_tasks
      // ========================================
      brain_tasks: tool({
        description: `List all tasks for current project with dependency status (ready/waiting/blocked), stats, and cycles detected.

Use this to see:
- Which tasks are ready to work on (dependencies met)
- Which tasks are waiting (dependencies incomplete)
- Which tasks are blocked (circular deps or blocked deps)
- Overall task queue stats`,
        args: {
          status: tool.schema
            .enum(ENTRY_STATUSES)
            .optional()
            .describe("Filter by task status (pending, in_progress, completed, etc.)"),
          classification: tool.schema
            .enum(["ready", "waiting", "blocked"])
            .optional()
            .describe("Filter by dependency classification"),
          feature_id: tool.schema
            .string()
            .optional()
            .describe("Filter tasks by feature group ID (e.g., 'auth-system', 'dark-mode')"),
          limit: tool.schema
            .number()
            .optional()
            .describe("Maximum results to return (default: 50)"),
          project: tool.schema
            .string()
            .optional()
            .describe("Override auto-detected project"),
        },
        async execute(args) {
          try {
            const proj = args.project || projectId;
            
            interface TaskWithDeps {
              id: string;
              title: string;
              status: string;
              priority?: string;
              feature_id?: string;
              classification: string;
              dependsOn?: Array<{ id: string; title: string; status: string }>;
              blockedBy?: string;
            }
            
            interface TaskListResponse {
              tasks: TaskWithDeps[];
              count: number;
              stats?: {
                ready: number;
                waiting: number;
                blocked: number;
                completed: number;
                total: number;
              };
              cycles?: Array<{ taskId: string; cycle: string[] }>;
            }
            
            const response = await apiRequest<TaskListResponse>(
              "GET",
              `/tasks/${encodeURIComponent(proj)}`
            );

            // Apply filters
            let filteredTasks = response.tasks;
            
            if (args.status) {
              filteredTasks = filteredTasks.filter(t => t.status === args.status);
            }
            
            if (args.classification) {
              filteredTasks = filteredTasks.filter(t => t.classification === args.classification);
            }
            
            if (args.feature_id) {
              filteredTasks = filteredTasks.filter(t => t.feature_id === args.feature_id);
            }
            
            const limit = args.limit ?? 50;
            filteredTasks = filteredTasks.slice(0, limit);

            // Group by classification
            const ready = filteredTasks.filter(t => t.classification === "ready");
            const waiting = filteredTasks.filter(t => t.classification === "waiting");
            const blocked = filteredTasks.filter(t => t.classification === "blocked");

            const lines: string[] = [];
            lines.push(`## Tasks for project: ${proj}`);
            lines.push("");

            // Stats summary
            if (response.stats) {
              const s = response.stats;
              lines.push(`**Stats:** ${s.ready} ready | ${s.waiting} waiting | ${s.blocked} blocked | ${s.completed} completed`);
              lines.push("");
            }

            // Ready tasks
            if (ready.length > 0) {
              lines.push("### Ready (can start now)");
              for (const task of ready) {
                const priority = task.priority === "high" ? "[HIGH]" : task.priority === "medium" ? "[MED]" : "[LOW]";
                lines.push(`- **${priority} ${task.title}** (\`${task.id}\`) - ${task.status}`);
                if (task.dependsOn && task.dependsOn.length > 0) {
                  const deps = task.dependsOn.map(d => `${d.title} (${d.status})`).join(", ");
                  lines.push(`  Dependencies: ${deps}`);
                } else {
                  lines.push("  Dependencies: none");
                }
              }
              lines.push("");
            }

            // Waiting tasks
            if (waiting.length > 0) {
              lines.push("### Waiting (deps incomplete)");
              for (const task of waiting) {
                const priority = task.priority === "high" ? "[HIGH]" : task.priority === "medium" ? "[MED]" : "[LOW]";
                lines.push(`- **${priority} ${task.title}** (\`${task.id}\`) - ${task.status}`);
                if (task.dependsOn && task.dependsOn.length > 0) {
                  const incomplete = task.dependsOn.filter(d => d.status !== "completed");
                  const deps = incomplete.map(d => `${d.title} (${d.status})`).join(", ");
                  lines.push(`  Waiting on: ${deps}`);
                }
              }
              lines.push("");
            }

            // Blocked tasks
            if (blocked.length > 0) {
              lines.push("### Blocked");
              for (const task of blocked) {
                const priority = task.priority === "high" ? "[HIGH]" : task.priority === "medium" ? "[MED]" : "[LOW]";
                lines.push(`- **${priority} ${task.title}** (\`${task.id}\`) - ${task.status}`);
                lines.push(`  Blocked by: ${task.blockedBy || "circular dependency or blocked deps"}`);
              }
              lines.push("");
            }

            // Cycles warning
            if (response.cycles && response.cycles.length > 0) {
              lines.push("### Circular Dependencies Detected");
              for (const cycle of response.cycles) {
                lines.push(`- Cycle: ${cycle.cycle.join(" -> ")}`);
              }
              lines.push("");
            }

            if (filteredTasks.length === 0) {
              lines.push("*No tasks found matching criteria.*");
            }

            return lines.join("\n");
          } catch (error) {
            return `Failed to list tasks: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_task_next
      // ========================================
      brain_task_next: tool({
        description: `Get the next actionable task (highest priority ready task) with full content.

Use this to quickly find what to work on next. Returns the complete task including:
- Full markdown content for implementation
- User's original request for validation
- Dependency information

If no ready tasks, shows current queue state.`,
        args: {
          project: tool.schema
            .string()
            .optional()
            .describe("Override auto-detected project"),
        },
        async execute(args) {
          try {
            const proj = args.project || projectId;
            
            // Get next ready task from API
            interface TaskNextResponse {
              task: {
                id: string;
                path: string;
                title: string;
                status: string;
                priority?: string;
                classification: string;
                resolved_deps: string[];
                waiting_on: string[];
                blocked_by: string[];
                user_original_request?: string;
              } | null;
              message?: string;
            }
            
            const response = await apiRequest<TaskNextResponse>(
              "GET",
              `/tasks/${encodeURIComponent(proj)}/next`
            );

            // No ready task available
            if (!response.task) {
              // Get stats for context
              interface TaskListResponse {
                tasks: Array<{ classification: string; status: string }>;
                stats?: {
                  ready: number;
                  waiting: number;
                  blocked: number;
                  completed: number;
                  total: number;
                };
              }
              
              const statsResponse = await apiRequest<TaskListResponse>(
                "GET",
                `/tasks/${encodeURIComponent(proj)}`
              );
              
              const stats = statsResponse.stats || {
                ready: 0,
                waiting: statsResponse.tasks.filter(t => t.classification === "waiting").length,
                blocked: statsResponse.tasks.filter(t => t.classification === "blocked").length,
                completed: statsResponse.tasks.filter(t => t.status === "completed").length,
                total: statsResponse.tasks.length,
              };

              return `No ready tasks available.

Current state:
- ${stats.waiting} tasks waiting on dependencies
- ${stats.blocked} tasks blocked
- ${stats.completed} tasks completed

Use \`brain_tasks\` to see the full task list and dependency status.`;
            }

            const task = response.task;
            
            // Get full entry content
            const entry = await apiRequest<{
              id: string;
              path: string;
              title: string;
              type: string;
              status: string;
              content: string;
              tags: string[];
              user_original_request?: string;
            }>("GET", `/entries/${task.path}`);

            const priority = task.priority === "high" ? "HIGH" : task.priority === "medium" ? "MEDIUM" : "LOW";
            const depsCount = task.resolved_deps?.length || 0;
            
            // Count tasks that depend on this one (reverse lookup not available, show resolved deps)
            const lines: string[] = [];
            lines.push(`## Next Task: ${entry.title}`);
            lines.push("");
            lines.push(`**ID:** \`${entry.id}\``);
            lines.push(`**Path:** \`${entry.path}\``);
            lines.push(`**Priority:** ${priority}`);
            lines.push(`**Status:** ${entry.status}`);
            lines.push("");
            
            // User's original request for validation
            if (entry.user_original_request) {
              lines.push("### User Original Request");
              lines.push(`> ${entry.user_original_request.split('\n').join('\n> ')}`);
              lines.push("");
            }
            
            lines.push("### Quick Context");
            if (depsCount > 0) {
              lines.push(`- ${depsCount} dependencies (all satisfied)`);
            } else {
              lines.push("- No dependencies");
            }
            lines.push("");
            lines.push("---");
            lines.push("");
            lines.push(entry.content);

            return lines.join("\n");
          } catch (error) {
            return `Failed to get next task: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_task_get
      // ========================================
      brain_task_get: tool({
        description: `Get a specific task by ID with full dependency info, dependents list, and content.

Use this to get detailed information about a specific task including:
- Full markdown content for implementation
- User's original request for validation  
- Dependencies (what this task needs)
- Dependents (what needs this task)
- Classification (ready/waiting/blocked)`,
        args: {
          taskId: tool.schema
            .string()
            .describe("Task ID (8-char alphanumeric) or title"),
          project: tool.schema
            .string()
            .optional()
            .describe("Override auto-detected project"),
        },
        async execute(args) {
          if (!args.taskId) {
            return "Please provide a task ID or title";
          }

          try {
            const proj = args.project || projectId;
            
            // Get all tasks to find the specific task and calculate dependents
            interface TaskWithDeps {
              id: string;
              title: string;
              path: string;
              status: string;
              priority?: string;
              classification: string;
              resolved_deps: string[];
              waiting_on: string[];
              blocked_by: string[];
              dependsOn?: Array<{ id: string; title: string; status: string }>;
            }
            
            interface TaskListResponse {
              tasks: TaskWithDeps[];
              count: number;
            }
            
            const response = await apiRequest<TaskListResponse>(
              "GET",
              `/tasks/${encodeURIComponent(proj)}`
            );

            // Find the task by ID or title
            const taskId = args.taskId.toLowerCase();
            const task = response.tasks.find(
              t => t.id.toLowerCase() === taskId || 
                   t.title.toLowerCase() === taskId.toLowerCase()
            );

            if (!task) {
              // Try searching for partial match
              const partialMatches = response.tasks.filter(
                t => t.title.toLowerCase().includes(taskId) ||
                     t.id.toLowerCase().includes(taskId)
              );
              
              if (partialMatches.length > 0) {
                const suggestions = partialMatches.slice(0, 5).map(
                  t => `- ${t.title} (ID: ${t.id})`
                ).join("\n");
                return `Task not found: "${args.taskId}"\n\n**Did you mean:**\n${suggestions}`;
              }
              
              return `Task not found: "${args.taskId}"\n\nUse \`brain_tasks\` to list all tasks.`;
            }

            // Calculate dependents - tasks that have this task in their resolved_deps
            const dependents = response.tasks.filter(
              t => t.resolved_deps?.includes(task.id)
            ).map(t => ({
              id: t.id,
              title: t.title,
              status: t.status,
            }));

            // Get full entry content
            const entry = await apiRequest<{
              id: string;
              path: string;
              title: string;
              type: string;
              status: string;
              content: string;
              tags: string[];
              user_original_request?: string;
            }>("GET", `/entries/${task.path}`);

            const priority = task.priority === "high" ? "HIGH" : task.priority === "medium" ? "MEDIUM" : "LOW";
            
            const lines: string[] = [];
            lines.push(`## ${entry.title}`);
            lines.push("");
            lines.push(`**ID:** \`${entry.id}\``);
            lines.push(`**Path:** \`${entry.path}\``);
            lines.push(`**Priority:** ${priority}`);
            lines.push(`**Status:** ${entry.status}`);
            lines.push(`**Classification:** ${task.classification}`);
            lines.push("");
            
            // Dependencies section
            lines.push("### Dependencies (what this task needs)");
            if (task.dependsOn && task.dependsOn.length > 0) {
              for (const dep of task.dependsOn) {
                const statusEmoji = dep.status === "completed" ? "✓" : dep.status === "in_progress" ? "⋯" : "○";
                lines.push(`- ${statusEmoji} **${dep.title}** (\`${dep.id}\`) - ${dep.status}`);
              }
            } else {
              lines.push("*No dependencies*");
            }
            lines.push("");
            
            // Dependents section
            lines.push("### Dependents (what needs this task)");
            if (dependents.length > 0) {
              for (const dep of dependents) {
                const statusEmoji = dep.status === "completed" ? "✓" : dep.status === "in_progress" ? "⋯" : "○";
                lines.push(`- ${statusEmoji} **${dep.title}** (\`${dep.id}\`) - ${dep.status}`);
              }
            } else {
              lines.push("*No tasks depend on this one*");
            }
            lines.push("");
            
            // User's original request
            if (entry.user_original_request) {
              lines.push("### User Original Request");
              lines.push(`> ${entry.user_original_request.split('\n').join('\n> ')}`);
              lines.push("");
            }
            
            lines.push("---");
            lines.push("");
            lines.push(entry.content);

            return lines.join("\n");
          } catch (error) {
            return `Failed to get task: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_task_metadata
      // ========================================
      brain_task_metadata: tool({
        description: `Get execution metadata for a task — fields NOT included in brain_task_get.

Returns structured JSON with:
- **Execution config:** agent, model, direct_prompt, target_workdir, resolved_workdir, git_branch, git_remote
- **Merge intent:** merge_target_branch, merge_policy (default auto_merge), merge_strategy (default squash), open_pr_before_merge, execution_mode
- **Feature grouping:** feature_id, feature_priority, feature_depends_on
- **Raw dependencies:** depends_on (IDs), resolved_deps, unresolved_deps, blocked_by, blocked_by_reason, waiting_on, in_cycle
- **Timestamps:** created, modified
- **Tags and sessions:** tags[], session_ids[]

Use this when you need to know HOW a task should be executed (which agent, model, workdir, prompt)
or to inspect its dependency graph details. Complements brain_task_get which returns content and high-level status.`,
        args: {
          taskId: tool.schema
            .string()
            .describe("Task ID (8-char alphanumeric) or title"),
          project: tool.schema
            .string()
            .optional()
            .describe("Override auto-detected project"),
        },
        async execute(args) {
          if (!args.taskId) {
            return "Please provide a task ID or title";
          }

          try {
            const proj = args.project || projectId;

            interface FullTask {
              id: string;
              title: string;
              path: string;
              status: string;
              priority: string;
              classification: string;
              depends_on: string[];
              resolved_deps: string[];
              unresolved_deps: string[];
              blocked_by: string[];
              blocked_by_reason?: string;
              waiting_on: string[];
              in_cycle: boolean;
              tags: string[];
              created: string;
              modified?: string;
              target_workdir: string | null;
              workdir: string | null;
              resolved_workdir: string | null;
              git_branch: string | null;
              git_remote: string | null;
              merge_target_branch?: string | null;
              merge_policy?: "prompt_only" | "auto_pr" | "auto_merge";
              merge_strategy?: "squash" | "merge" | "rebase";
              open_pr_before_merge?: boolean;
              execution_mode?: "worktree" | "current_branch";

              agent: string | null;
              model: string | null;
              direct_prompt: string | null;
              feature_id?: string;
              feature_priority?: string;
              feature_depends_on?: string[];
              session_ids: string[];
              user_original_request: string | null;
            }

            interface TaskListResponse {
              tasks: FullTask[];
              count: number;
            }

            const response = await apiRequest<TaskListResponse>(
              "GET",
              `/tasks/${encodeURIComponent(proj)}`
            );

            const taskId = args.taskId.toLowerCase();
            const task = response.tasks.find(
              t => t.id.toLowerCase() === taskId ||
                   t.title.toLowerCase() === taskId
            );

            if (!task) {
              const partialMatches = response.tasks.filter(
                t => t.title.toLowerCase().includes(taskId) ||
                     t.id.toLowerCase().includes(taskId)
              );

              if (partialMatches.length > 0) {
                const suggestions = partialMatches.slice(0, 5).map(
                  t => `- ${t.title} (ID: ${t.id})`
                ).join("\n");
                return `Task not found: "${args.taskId}"\n\n**Did you mean:**\n${suggestions}`;
              }

              return `Task not found: "${args.taskId}"\n\nUse \`brain_tasks\` to list all tasks.`;
            }

            // Build metadata-only response (no content body)
            const metadata: Record<string, unknown> = {
              id: task.id,
              title: task.title,
              path: task.path,
              status: task.status,
              priority: task.priority,
              classification: task.classification,

              // Execution config
              execution: {
                agent: task.agent,
                model: task.model,
                direct_prompt: task.direct_prompt,
                target_workdir: task.target_workdir,
                workdir: task.workdir,
                resolved_workdir: task.resolved_workdir,
                git_branch: task.git_branch,
                git_remote: task.git_remote,
                merge_target_branch: task.merge_target_branch ?? null,
                merge_policy: task.merge_policy ?? "auto_merge",
                merge_strategy: task.merge_strategy ?? "squash",
                open_pr_before_merge: task.open_pr_before_merge ?? false,
                execution_mode: task.execution_mode ?? "worktree",

              },

              // Feature grouping
              feature: task.feature_id ? {
                id: task.feature_id,
                priority: task.feature_priority || null,
                depends_on: task.feature_depends_on || [],
              } : null,

              // Dependencies (raw IDs)
              dependencies: {
                depends_on: task.depends_on || [],
                resolved_deps: task.resolved_deps || [],
                unresolved_deps: task.unresolved_deps || [],
                blocked_by: task.blocked_by || [],
                blocked_by_reason: task.blocked_by_reason || null,
                waiting_on: task.waiting_on || [],
                in_cycle: task.in_cycle || false,
              },

              // Metadata
              tags: task.tags || [],
              created: task.created,
              modified: task.modified || null,
              session_ids: task.session_ids || [],
              user_original_request: task.user_original_request,
            };

            return JSON.stringify(metadata, null, 2);
          } catch (error) {
            return `Failed to get task metadata: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_tasks_status
      // ========================================
      brain_tasks_status: tool({
        description: `Get status of multiple tasks by ID, with optional blocking wait.

Use cases:
- Check if spawned subtasks are complete before continuing
- Wait for dependent tasks to finish before starting next phase
- Monitor multiple tasks from an orchestrator agent

Parameters:
- taskIds: Array of task IDs (8-char alphanumeric) to check
- waitFor: Optional. "completed" (default) waits until all tasks completed/validated.
           "any" returns as soon as any task status changes.
           Omit for immediate response without waiting.
- timeout: Max wait time in milliseconds (default: 60000, max: 300000)
- project: Override auto-detected project

Example - immediate check:
  brain_tasks_status({ taskIds: ["abc12def", "xyz98765"] })

Example - wait for completion:
  brain_tasks_status({ taskIds: ["abc12def"], waitFor: "completed", timeout: 120000 })`,
        args: {
          taskIds: tool.schema
            .array(tool.schema.string())
            .describe("Task IDs (8-char alphanumeric) to check"),
          waitFor: tool.schema
            .enum(["completed", "any"])
            .optional()
            .describe("Wait mode: 'completed' (all done) or 'any' (first change). Omit for immediate."),
          timeout: tool.schema
            .number()
            .optional()
            .describe("Max wait time in ms (default: 60000, max: 300000)"),
          project: tool.schema
            .string()
            .optional()
            .describe("Override auto-detected project"),
        },
        async execute(args) {
          if (!args.taskIds || args.taskIds.length === 0) {
            return "Please provide at least one task ID";
          }

          try {
            const proj = args.project || projectId;

            const response = await apiRequest<{
              tasks: Array<{
                id: string;
                title: string;
                status: string;
                classification: string;
                priority?: string;
              }>;
              notFound: string[];
              changed: boolean;
              timedOut: boolean;
            }>("POST", `/tasks/${encodeURIComponent(proj)}/status`, {
              taskIds: args.taskIds,
              waitFor: args.waitFor,
              timeout: args.timeout,
            });

            const lines: string[] = [];

            // Summary line
            if (args.waitFor) {
              if (response.timedOut) {
                lines.push(`## Task Status (TIMED OUT after ${(args.timeout || 60000) / 1000}s)`);
              } else if (response.changed) {
                lines.push(`## Task Status (condition met: ${args.waitFor})`);
              } else {
                lines.push("## Task Status");
              }
            } else {
              lines.push("## Task Status");
            }
            lines.push("");

            // Task statuses
            for (const task of response.tasks) {
              const priority = task.priority === "high" ? "[HIGH]" :
                               task.priority === "medium" ? "[MED]" : "[LOW]";
              const statusIcon = task.status === "completed" ? "✓" :
                                 task.status === "in_progress" ? "⋯" : "○";
              lines.push(`${statusIcon} **${task.title}** (${task.id}) ${priority}`);
              lines.push(`   Status: ${task.status} | Classification: ${task.classification}`);
            }

            // Not found IDs
            if (response.notFound.length > 0) {
              lines.push("");
              lines.push(`**Not found:** ${response.notFound.join(", ")}`);
            }

            // Meta info
            lines.push("");
            if (response.timedOut) {
              lines.push("*Request timed out - tasks may still be running*");
            } else if (args.waitFor && response.changed) {
              lines.push("*Condition met - returning current state*");
            }

            return lines.join("\n");
          } catch (error) {
            return `Failed to get task status: ${error instanceof Error ? error.message : String(error)}`;
          }
        },
      }),

      // ========================================
      // brain_task_trigger
      // ========================================
      brain_task_trigger: tool({
        description:
          "Manually trigger a scheduled task and its downstream dependents.",
        args: {
          taskId: tool.schema
            .string()
            .describe("Task ID (8-char alphanumeric)"),
          project: tool.schema
            .string()
            .optional()
            .describe("Override auto-detected project"),
        },
        async execute(args) {
          const proj = args.project || projectId;
          try {
            const response = await apiRequest<{
              taskId: string;
              run: unknown;
              pipeline: unknown[];
              pipelineCount: number;
              message: string;
            }>(
              "POST",
              `/tasks/${encodeURIComponent(proj)}/${encodeURIComponent(args.taskId)}/trigger`
            );
            return JSON.stringify({ operation: "task_trigger", project: proj, data: response }, null, 2);
          } catch (error) {
            return JSON.stringify({ operation: "task_trigger", project: proj, error: error instanceof Error ? error.message : String(error) }, null, 2);
          }
        },
      }),

      // ========================================
      // brain_plan_sections
      // ========================================
      brain_plan_sections: tool({
        description:
          "Extract section headers from a plan entry for orchestration mapping.",
        args: {
          path: tool.schema
            .string()
            .optional()
            .describe("Path to the plan entry"),
          title: tool.schema
            .string()
            .optional()
            .describe("Title to search for"),
        },
        async execute(args) {
          if (!args.path && !args.title) {
            return JSON.stringify({ error: "Please provide either a path or title" });
          }

          try {
            // If title provided, search first
            let entryPath = args.path;
            if (!entryPath && args.title) {
              const searchResult = await apiRequest<{
                results: Array<{ path: string; title: string }>;
              }>("POST", "/search", {
                query: args.title,
                limit: 5,
              });

              const exactMatch = searchResult.results.find(
                (r) => r.title === args.title
              );
              if (exactMatch) {
                entryPath = exactMatch.path;
              } else if (searchResult.results.length > 0) {
                const suggestions = searchResult.results
                  .slice(0, 5)
                  .map((r) => r.title);
                return JSON.stringify({
                  error: `No exact match for title: "${args.title}"`,
                  suggestions,
                  hint: "Use brain_plan_sections with the exact path instead",
                });
              } else {
                return JSON.stringify({
                  error: `No entry found matching title: "${args.title}"`,
                });
              }
            }

            const response = await apiRequest<{
              sections: Array<{
                title: string;
                level: number;
                line: number;
              }>;
              total: number;
            }>("GET", `/entries/${entryPath}/sections`);

            // Get entry details for title
            const entry = await apiRequest<{
              title: string;
              type: string;
            }>("GET", `/entries/${entryPath}`);

            return JSON.stringify(
              {
                path: entryPath,
                title: entry.title,
                type: entry.type,
                sections: response.sections,
                sectionTitles: response.sections.map((s) => s.title),
              },
              null,
              2
            );
          } catch (error) {
            return JSON.stringify({
              error: error instanceof Error ? error.message : String(error),
            });
          }
        },
      }),

      // ========================================
      // brain_monitor_enable (generic)
      // ========================================
      brain_monitor_enable: tool({
        description:
          "(Deprecated - use automations) Enable a monitor template for a feature. Creates an automated task. Prefer brain_automation_list and creating automation entries directly.",
        args: {
          template_id: tool.schema
            .string()
            .describe("Monitor template ID (e.g., 'blocked-inspector', 'feature-review')"),
          project: tool.schema
            .string()
            .describe("Project containing the feature"),
          feature_id: tool.schema
            .string()
            .describe("Feature ID to monitor"),
          schedule: tool.schema
            .string()
            .optional()
            .describe("Optional cron schedule override (for scheduled templates only)"),
        },
        async execute(args) {
          const scope = {
            type: "feature",
            project: args.project,
            feature_id: args.feature_id,
          };

          const body: Record<string, unknown> = { templateId: args.template_id, scope };
          if (args.schedule) body.schedule = args.schedule;

          try {
            const response = await apiRequest<{
              id: string;
              path: string;
              title: string;
            }>("POST", "/monitors", body);

            return [
              `Monitor "${args.template_id}" enabled for feature "${args.feature_id}":`,
              `- **Task ID:** ${response.id}`,
              `- **Title:** ${response.title}`,
            ].join("\n");
          } catch (error) {
            const msg = error instanceof Error ? error.message : String(error);
            if (msg.includes("409") || msg.toLowerCase().includes("conflict")) {
              return `Monitor "${args.template_id}" is already enabled for feature "${args.feature_id}". Disable first to reset.`;
            }
            return `Failed to enable monitor: ${msg}`;
          }
        },
      }),

      // ========================================
      // brain_monitor_disable (generic)
      // ========================================
      brain_monitor_disable: tool({
        description:
          "(Deprecated - use automations) Disable a monitor template for a feature. Permanently removes the monitor task. Prefer managing automation entries directly.",
        args: {
          template_id: tool.schema
            .string()
            .describe("Monitor template ID (e.g., 'blocked-inspector', 'feature-review')"),
          project: tool.schema
            .string()
            .describe("Project containing the feature"),
          feature_id: tool.schema
            .string()
            .describe("Feature ID"),
        },
        async execute(args) {
          const scope = {
            type: "feature",
            project: args.project,
            feature_id: args.feature_id,
          };

          try {
            const response = await apiRequest<{
              message: string;
              taskId: string;
              path: string;
            }>("DELETE", "/monitors/by-scope", { templateId: args.template_id, scope });

            return `Monitor "${args.template_id}" disabled for feature "${args.feature_id}" (task ${response.taskId} deleted).`;
          } catch (error) {
            const msg = error instanceof Error ? error.message : String(error);
            if (msg.includes("404") || msg.toLowerCase().includes("not found")) {
              return `Monitor "${args.template_id}" is not currently enabled for feature "${args.feature_id}". Nothing to disable.`;
            }
            return `Failed to disable monitor: ${msg}`;
          }
        },
      }),

      // ========================================
      // Legacy aliases (backward compatibility)
      // ========================================

      // ========================================
      // brain_feature_review_enable
      // ========================================
      brain_feature_review_enable: tool({
        description:
          "(Deprecated - use automations) Enable Feature Code Review for a feature. Creates a one-shot review task that triggers when all tasks complete. Prefer creating an automation entry with trigger type 'event' and event 'feature.all_completed'.",
        args: {
          project: tool.schema
            .string()
            .describe("Project containing the feature"),
          feature_id: tool.schema
            .string()
            .describe("Feature ID to review"),
        },
        async execute(args) {
          const scope = {
            type: "feature",
            project: args.project,
            feature_id: args.feature_id,
          };

          try {
            const response = await apiRequest<{
              id: string;
              path: string;
              title: string;
            }>("POST", "/monitors", { templateId: "feature-review", scope });

            return [
              `Feature Code Review enabled for feature "${args.feature_id}":`,
              `- **Task ID:** ${response.id}`,
              `- **Title:** ${response.title}`,
              `The review will trigger automatically when all feature tasks are completed.`,
            ].join("\n");
          } catch (error) {
            const msg = error instanceof Error ? error.message : String(error);
            if (msg.includes("409") || msg.toLowerCase().includes("conflict")) {
              return `Feature Code Review is already enabled for feature "${args.feature_id}". Use brain_feature_review_disable first to reset it.`;
            }
            return `Failed to enable Feature Code Review: ${msg}`;
          }
        },
      }),

      // ========================================
      // brain_feature_review_disable
      // ========================================
      brain_feature_review_disable: tool({
        description:
          "(Deprecated - use automations) Disable Feature Code Review for a feature. Permanently removes the review task. Prefer managing automation entries directly.",
        args: {
          project: tool.schema
            .string()
            .describe("Project containing the feature"),
          feature_id: tool.schema
            .string()
            .describe("Feature ID"),
        },
        async execute(args) {
          const scope = {
            type: "feature",
            project: args.project,
            feature_id: args.feature_id,
          };

          try {
            const response = await apiRequest<{
              message: string;
              taskId: string;
              path: string;
            }>("DELETE", "/monitors/by-scope", { templateId: "feature-review", scope });

            return `Feature Code Review disabled for feature "${args.feature_id}" (task ${response.taskId} deleted).`;
          } catch (error) {
            const msg = error instanceof Error ? error.message : String(error);
            if (msg.includes("404") || msg.toLowerCase().includes("not found")) {
              return `Feature Code Review is not currently enabled for feature "${args.feature_id}". Nothing to disable.`;
            }
            return `Failed to disable Feature Code Review: ${msg}`;
          }
        },
      }),

      // ========================================
      // brain_blocked_inspector_enable
      // ========================================
      brain_blocked_inspector_enable: tool({
        description:
          "(Deprecated - use automations) Enable Blocked Task Inspector for a feature. Creates a recurring scheduled task. Prefer creating an automation entry with trigger type 'cron'.",
        args: {
          project: tool.schema
            .string()
            .describe("Project containing the feature"),
          feature_id: tool.schema
            .string()
            .describe("Feature ID to inspect"),
          schedule: tool.schema
            .string()
            .optional()
            .describe("Cron schedule override (default: every 30 minutes)"),
        },
        async execute(args) {
          const scope = {
            type: "feature",
            project: args.project,
            feature_id: args.feature_id,
          };

          const body: Record<string, unknown> = { templateId: "blocked-inspector", scope };
          if (args.schedule) body.schedule = args.schedule;

          try {
            const response = await apiRequest<{
              id: string;
              path: string;
              title: string;
            }>("POST", "/monitors", body);

            return [
              `Blocked Task Inspector enabled for feature "${args.feature_id}":`,
              `- **Task ID:** ${response.id}`,
              `- **Title:** ${response.title}`,
              `The inspector will periodically check for blocked tasks and attempt to unblock them.`,
            ].join("\n");
          } catch (error) {
            const msg = error instanceof Error ? error.message : String(error);
            if (msg.includes("409") || msg.toLowerCase().includes("conflict")) {
              return `Blocked Task Inspector is already enabled for feature "${args.feature_id}". Use brain_blocked_inspector_disable first to reset it.`;
            }
            return `Failed to enable Blocked Task Inspector: ${msg}`;
          }
        },
      }),

      // ========================================
      // brain_blocked_inspector_disable
      // ========================================
      brain_blocked_inspector_disable: tool({
        description:
          "(Deprecated - use automations) Disable Blocked Task Inspector for a feature. Permanently removes the inspector task. Prefer managing automation entries directly.",
        args: {
          project: tool.schema
            .string()
            .describe("Project containing the feature"),
          feature_id: tool.schema
            .string()
            .describe("Feature ID"),
        },
        async execute(args) {
          const scope = {
            type: "feature",
            project: args.project,
            feature_id: args.feature_id,
          };

          try {
            const response = await apiRequest<{
              message: string;
              taskId: string;
              path: string;
            }>("DELETE", "/monitors/by-scope", { templateId: "blocked-inspector", scope });

            return `Blocked Task Inspector disabled for feature "${args.feature_id}" (task ${response.taskId} deleted).`;
          } catch (error) {
            const msg = error instanceof Error ? error.message : String(error);
            if (msg.includes("404") || msg.toLowerCase().includes("not found")) {
              return `Blocked Task Inspector is not currently enabled for feature "${args.feature_id}". Nothing to disable.`;
            }
            return `Failed to disable Blocked Task Inspector: ${msg}`;
          }
        },
      }),

      // ========================================
      // brain_automation_list
      // ========================================
      brain_automation_list: tool({
        description:
          "List active automations with their trigger type, status, and last-fired info. Automations are event-driven behaviors stored as brain entries that replace hardcoded monitors.",
        args: {
          project: tool.schema
            .string()
            .optional()
            .describe("Filter by project (optional, lists all if omitted)"),
          status: tool.schema
            .enum(["active", "archived", "draft"])
            .optional()
            .describe("Filter by status (default: all)"),
        },
        async execute(args) {
          const params: Record<string, string> = { type: "automation" };
          if (args.project) params.project = args.project;
          if (args.status) params.status = args.status;

          const query = new URLSearchParams(params).toString();
          const response = await apiRequest<{
            entries: Array<{
              id: string;
              title: string;
              type: string;
              status: string;
              project_id: string;
              trigger?: { type: string; event?: string; schedule?: string; webhook?: string };
              action?: { type: string; direct_prompt?: string; command?: string };
            }>;
            total: number;
          }>("GET", `/entries?${query}`);

          if (response.entries.length === 0) {
            return "No automations found.\n\nCreate one with `brain automation create` or save a brain entry with type: automation.";
          }

          const lines = [`## Automations (${response.total})`, ""];
          for (const entry of response.entries) {
            const triggerType = entry.trigger?.type ?? "";
            let triggerDetail = "";
            if (entry.trigger?.type === "event") triggerDetail = entry.trigger.event ?? "";
            else if (entry.trigger?.type === "cron") triggerDetail = entry.trigger.schedule ?? "";
            else if (entry.trigger?.type === "webhook") triggerDetail = entry.trigger.webhook ?? "";

            const actionType = entry.action?.type ?? "";
            const project = entry.project_id || "(global)";
            const icon = entry.status === "active" ? "●" : "○";

            lines.push(`${icon} **${entry.title}** (\`${entry.id}\`)`);
            lines.push(`  Trigger: ${triggerType} ${triggerDetail} | Action: ${actionType} | Project: ${project} | Status: ${entry.status}`);
            lines.push("");
          }

          return lines.join("\n");
        },
      }),

      // ========================================
      // brain_automation_test
      // ========================================
      brain_automation_test: tool({
        description:
          "Dry-run an event against active automations to see which would match. No tasks are created -- this is a simulation for debugging automation triggers.",
        args: {
          event: tool.schema
            .string()
            .describe("Event name to simulate (e.g., 'task.completed', 'feature.all_completed')"),
          project: tool.schema
            .string()
            .optional()
            .describe("Filter automations by project (optional)"),
        },
        async execute(args) {
          const params: Record<string, string> = { type: "automation", status: "active" };
          if (args.project) params.project = args.project;

          const query = new URLSearchParams(params).toString();
          const response = await apiRequest<{
            entries: Array<{
              id: string;
              title: string;
              trigger?: { type: string; event?: string };
              action?: { type: string; direct_prompt?: string; command?: string };
            }>;
            total: number;
          }>("GET", `/entries?${query}`);

          const lines = [`## Simulating event: "${args.event}" (dry-run)`, ""];
          let matched = 0;

          for (const entry of response.entries) {
            if (!entry.trigger || entry.trigger.type !== "event") continue;
            const pattern = entry.trigger.event ?? "";
            if (!matchesEvent(pattern, args.event)) continue;

            matched++;
            lines.push(`**MATCH:** ${entry.title} (\`${entry.id}\`)`);
            lines.push(`  Trigger: event=${pattern}`);
            if (entry.action) {
              lines.push(`  Action: ${entry.action.type}`);
              if (entry.action.direct_prompt) {
                const prompt = entry.action.direct_prompt.length > 80
                  ? entry.action.direct_prompt.slice(0, 77) + "..."
                  : entry.action.direct_prompt;
                lines.push(`  Prompt: ${prompt}`);
              }
              if (entry.action.command) {
                lines.push(`  Command: ${entry.action.command}`);
              }
            }
            lines.push("");
          }

          if (matched === 0) {
            lines.push(`No automations matched event "${args.event}" (dry-run, no tasks created)`);
          } else {
            lines.push(`---\n${matched} automation(s) would match (dry-run, no tasks created)`);
          }
          return lines.join("\n");
        },
      }),
    },
  };
};

export default BrainPlugin;
