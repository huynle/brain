/**
 * Brain API - Core Brain Service
 *
 * Main business logic layer ported from the OpenCode brain plugin.
 * Implements all 17 brain operations.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync, unlinkSync, readdirSync } from "fs";
import { join } from "path";
import { getConfig } from "../config";
import {
  initDatabase,
  getDb,
  getEntryMeta,
  recordAccess,
  initEntry,
  setVerified,
  getStaleEntries,
  getTrackedEntryCount,
  deleteEntryMeta,
  moveEntryMeta,
  acquireGeneratedTaskLease,
  completeGeneratedTaskLease,
  getGeneratedTaskKey,
} from "./db";
import {
  execZk,
  execZkNew,
  extractIdFromPath,
  generateMarkdownLink,
  generateShortId,
  isZkAvailable,
  getZkVersion,
  parseZkJsonOutput,
  isZkNotebookExists,
  extractType,
  extractStatus,
  extractPriority,
  getPrioritySortValue,
  parseFrontmatter,
  generateFrontmatter,
  escapeYamlValue,
  slugify,
  matchesFilenamePattern,
  normalizeTitle,
  sanitizeTitle,
  sanitizeTag,
  sanitizeSimpleValue,
  sanitizeDependsOnEntry,
  serializeFrontmatter,
} from "./zk-client";
import type {
  BrainConfig,
  BrainEntry,
  CreateEntryRequest,
  CreateEntryResponse,
  UpdateEntryRequest,
  ListEntriesRequest,
  ListEntriesResponse,
  SearchRequest,
  SearchResponse,
  InjectRequest,
  InjectResponse,
  LinkRequest,
  LinkResponse,
  StatsResponse,
  EntryType,
  EntryStatus,
  SessionInfo,
  RunFinalization,
  ZkNote,
  Task,
  FeatureCheckoutRequest,
} from "./types";
import { ENTRY_STATUSES } from "./types";
import { TaskService, normalizeDependencyRef, findDependents } from "./task-service";
import type { DependencyValidationResult, DependentInfo } from "./task-service";
import { getNextRun } from "./cron-service";

// =============================================================================
// Dependency Validation Error
// =============================================================================

export class DependencyValidationError extends Error {
  constructor(
    public readonly errors: string[],
    message?: string
  ) {
    super(message || `Invalid dependencies: ${errors.join("; ")}`);
    this.name = "DependencyValidationError";
  }
}

// =============================================================================
// Constants
// =============================================================================

const MS_PER_DAY = 24 * 60 * 60 * 1000;
const GENERATED_TASK_LEASE_DURATION_MS = 30_000;
const GENERATED_TASK_BUSY_WAIT_TIMEOUT_MS = 5_000;
const GENERATED_TASK_BUSY_POLL_INTERVAL_MS = 50;

function normalizeCronRuns(rawRuns: unknown): import("./types").CronRun[] | undefined {
  if (!Array.isArray(rawRuns)) {
    return undefined;
  }

  const runs = rawRuns
    .map((run) => {
      if (!run || typeof run !== "object") {
        return null;
      }

      const obj = run as Record<string, unknown>;
      const runId = typeof obj.run_id === "string" ? obj.run_id : "";
      const status = typeof obj.status === "string" ? obj.status : "";
      const started = typeof obj.started === "string" ? obj.started : "";

      if (!runId || !status || !started) {
        return null;
      }

      const normalized: import("./types").CronRun = {
        run_id: runId,
        status: status as import("./types").CronRun["status"],
        started,
      };

      if (typeof obj.completed === "string" && obj.completed) {
        normalized.completed = obj.completed;
      }
      if (obj.duration !== undefined && obj.duration !== null && `${obj.duration}` !== "") {
        const duration = Number(obj.duration);
        if (!Number.isNaN(duration)) {
          normalized.duration = duration;
        }
      }
      if (obj.tasks !== undefined && obj.tasks !== null && `${obj.tasks}` !== "") {
        const tasks = Number(obj.tasks);
        if (!Number.isNaN(tasks)) {
          normalized.tasks = tasks;
        }
      }
      if (typeof obj.failed_task === "string" && obj.failed_task) {
        normalized.failed_task = obj.failed_task;
      }
      if (typeof obj.skip_reason === "string" && obj.skip_reason) {
        normalized.skip_reason = obj.skip_reason;
      }

      return normalized;
    })
    .filter((run): run is import("./types").CronRun => run !== null);

  return runs.length > 0 ? runs : undefined;
}

function resolveLatestRunContext(
  sessions: Record<string, SessionInfo>
): { runId: string; sessionId?: string } | undefined {
  const runSessions = Object.entries(sessions)
    .filter(([, session]) => typeof session.run_id === "string" && session.run_id.length > 0)
    .map(([sessionId, session]) => ({
      sessionId,
      runId: session.run_id as string,
      timestampMs: Date.parse(session.timestamp || ""),
    }));

  if (runSessions.length === 0) {
    return undefined;
  }

  runSessions.sort((a, b) => {
    const aTs = Number.isNaN(a.timestampMs) ? 0 : a.timestampMs;
    const bTs = Number.isNaN(b.timestampMs) ? 0 : b.timestampMs;
    return bTs - aTs;
  });

  return { runId: runSessions[0].runId, sessionId: runSessions[0].sessionId };
}

// =============================================================================
// Section Type
// =============================================================================

export interface Section {
  title: string;
  level: number;
  startLine: number;
  endLine: number;
}

export interface MarkFeatureForCheckoutResult {
  created: boolean;
  generatedKey: string;
  task: CreateEntryResponse;
}

interface UpdateOptions {
  skipFeatureCheckoutReconcile?: boolean;
  skipDependsOnValidation?: boolean;
}

// =============================================================================
// Brain Service Class
// =============================================================================

export class BrainService {
  private config: BrainConfig;
  private projectId: string;

  constructor(config?: BrainConfig, projectId?: string) {
    const fullConfig = getConfig();
    this.config = config || fullConfig.brain;
    this.projectId = projectId || fullConfig.brain.defaultProject;

    // Ensure database is initialized
    initDatabase();
  }

  // ========================================
  // Entry CRUD
  // ========================================

  /**
   * Save a new entry to the brain (brain_save)
   */
  async save(request: CreateEntryRequest): Promise<CreateEntryResponse> {
    // Normalize title for user-friendly display (strips control chars, collapses whitespace)
    // This is what we return to clients
    const displayTitle = normalizeTitle(request.title);
    
    // Sanitize title for YAML (adds escaping for quotes/backslashes)
    // This is what we write to files
    const sanitizedTitle = sanitizeTitle(request.title);
    if (request.tags) {
      request.tags = request.tags
        .map(sanitizeTag)
        .filter((t): t is string => t !== null); // Drop tags that sanitize to empty
    }
    if (request.workdir) request.workdir = sanitizeSimpleValue(request.workdir);
    if (request.git_remote) request.git_remote = sanitizeSimpleValue(request.git_remote);
    if (request.git_branch) request.git_branch = sanitizeSimpleValue(request.git_branch);
    if (request.merge_target_branch) {
      request.merge_target_branch = sanitizeSimpleValue(request.merge_target_branch);
    }
    if (request.project) request.project = sanitizeSimpleValue(request.project);
    if (request.depends_on) {
      request.depends_on = request.depends_on.map(sanitizeDependsOnEntry);
    }
    // user_original_request: allow multiline but strip \r and \0
    if (request.user_original_request) {
      request.user_original_request = request.user_original_request
        .replace(/\r/g, "")
        .replace(/\0/g, "");
    }
    // direct_prompt: allow multiline but strip \r and \0 (same as user_original_request)
    if (request.direct_prompt) {
      request.direct_prompt = request.direct_prompt
        .replace(/\r/g, "")
        .replace(/\0/g, "");
    }
    // agent and model: sanitize as simple values (no newlines)
    if (request.agent) request.agent = sanitizeSimpleValue(request.agent);
    if (request.model) request.model = sanitizeSimpleValue(request.model);
    if (request.execution_mode) {
      request.execution_mode = sanitizeSimpleValue(request.execution_mode) as
        CreateEntryRequest["execution_mode"];
    }
    if (request.remote_branch_policy) {
      request.remote_branch_policy = sanitizeSimpleValue(request.remote_branch_policy) as
        CreateEntryRequest["remote_branch_policy"];
    }
    if (request.generated_key) request.generated_key = sanitizeSimpleValue(request.generated_key);
    if (request.generated_by) request.generated_by = sanitizeSimpleValue(request.generated_by);

    const entryType = request.type;
    // Tasks default to 'draft' status (user reviews before promoting to 'pending')
    // All other entry types default to 'active'
    const entryStatus = request.status || (entryType === "task" ? "draft" : "active");

    if (entryType === "task") {
      request.merge_policy ??= "auto_merge";
      request.merge_strategy ??= "squash";
      request.remote_branch_policy ??= "delete";
      request.open_pr_before_merge ??= false;
      request.execution_mode ??= "worktree";
      request.checkout_enabled ??= true;
      request.complete_on_idle ??= false;
    }

    const isGlobal = request.global ?? false;

    if ((entryType === "cron" || (entryType === "task" && request.schedule)) && request.run_once_at && !request.next_run) {
      request.next_run = request.run_once_at;
      if (request.max_runs === undefined) {
        request.max_runs = 1;
      }
    }

    if ((entryType === "cron" || (entryType === "task" && request.schedule)) && request.schedule && !request.next_run) {
      request.next_run = getNextRun(request.schedule).toISOString();
    }

    // Determine effective project ID
    const effectiveProjectId =
      request.project || (isGlobal ? "global" : this.projectId) || "global";

    // Validate depends_on for task entries
    if (entryType === "task" && request.depends_on && request.depends_on.length > 0) {
      const taskService = new TaskService(this.config, effectiveProjectId);
      const validation = await taskService.validateDependencies(
        request.depends_on,
        effectiveProjectId
      );
      
      if (!validation.valid) {
        throw new DependencyValidationError(validation.errors);
      }
      
      // Replace with normalized IDs for consistency
      request.depends_on = validation.normalized;
    }

    // Schedule fields stay on the task directly (no separate cron entry created)

    // Determine directory for zk
    const projectDir =
      request.project || (isGlobal ? "global" : this.projectId.slice(0, 8));
    const dir = isGlobal
      ? `global/${entryType}`
      : `projects/${projectDir}/${entryType}`;
    const fullDir = join(this.config.brainDir, dir);

    // Ensure directory exists
    if (!existsSync(fullDir)) {
      mkdirSync(fullDir, { recursive: true });
    }

    // Build content with optional related entries section
    let finalContent = request.content;

    if (request.relatedEntries && request.relatedEntries.length > 0) {
      const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
      const resolvedLinks: string[] = [];
      const unresolvedEntries: string[] = [];

      for (const entry of request.relatedEntries) {
        const isPath = entry.includes("/") || entry.endsWith(".md");
        const isId = /^[a-z0-9]{8}$/.test(entry);

        if (isPath || isId) {
          const entryPath = isId
            ? entry
            : entry.endsWith(".md")
              ? entry
              : `${entry}.md`;

          if (zkAvailable) {
            try {
              const result = await execZk([
                "list",
                "--format",
                "json",
                "--quiet",
                entryPath,
              ]);
              if (result.exitCode === 0) {
                const notes = parseZkJsonOutput(result.stdout);
                if (notes.length > 0) {
                  const note = notes[0];
                  const id = extractIdFromPath(note.path);
                  resolvedLinks.push(`- ${generateMarkdownLink(id, note.title)}`);
                } else {
                  unresolvedEntries.push(entry);
                }
              } else {
                unresolvedEntries.push(entry);
              }
            } catch {
              unresolvedEntries.push(entry);
            }
          } else {
            const fullEntryPath = join(
              this.config.brainDir,
              entryPath.endsWith(".md") ? entryPath : `${entryPath}.md`
            );
            if (existsSync(fullEntryPath)) {
              let entryTitle = entry;
              try {
                const entryContent = readFileSync(fullEntryPath, "utf-8");
                const { frontmatter: entryFm } = parseFrontmatter(entryContent);
                entryTitle = (entryFm.title as string) || entry;
              } catch {
                /* use entry as title */
              }
              const id = extractIdFromPath(entryPath);
              resolvedLinks.push(`- ${generateMarkdownLink(id, entryTitle)}`);
            } else {
              unresolvedEntries.push(entry);
            }
          }
        } else if (zkAvailable) {
          try {
            const result = await execZk([
              "list",
              "--format",
              "json",
              "--quiet",
              "--match",
              entry,
              "--match-strategy",
              "exact",
            ]);

            if (result.exitCode === 0) {
              const notes = parseZkJsonOutput(result.stdout);
              const exactMatch = notes.find((n) => n.title === entry);

              if (exactMatch) {
                const id = extractIdFromPath(exactMatch.path);
                resolvedLinks.push(
                  `- ${generateMarkdownLink(id, exactMatch.title)}`
                );
              } else if (notes.length > 0) {
                const firstMatch = notes[0];
                const id = extractIdFromPath(firstMatch.path);
                resolvedLinks.push(
                  `- ${generateMarkdownLink(id, firstMatch.title)}`
                );
              } else {
                unresolvedEntries.push(entry);
              }
            } else {
              unresolvedEntries.push(entry);
            }
          } catch {
            unresolvedEntries.push(entry);
          }
        } else {
          unresolvedEntries.push(entry);
        }
      }

      if (resolvedLinks.length > 0) {
        finalContent +=
          "\n\n## Related Brain Entries\n\n" + resolvedLinks.join("\n");
      }

      if (unresolvedEntries.length > 0) {
        if (resolvedLinks.length === 0) {
          finalContent += "\n\n## Related Brain Entries\n";
        }
        finalContent +=
          "\n\n<!-- Unresolved entries (could not find matching brain entries): -->\n";
        for (const unresolved of unresolvedEntries) {
          finalContent += `<!-- - ${unresolved} -->\n`;
        }
      }
    }

    // Check if zk is available
    const hasNotebookInConfiguredDir = existsSync(join(this.config.brainDir, ".zk"));
    let zkAvailable = hasNotebookInConfiguredDir && (await isZkAvailable());

    // Cron entries and tasks with schedules require schedule/next_run frontmatter fields
    // that are not guaranteed by zk templates in user notebooks. Force manual creation
    // for deterministic metadata.
    if (entryType === "cron" || (entryType === "task" && request.schedule)) {
      zkAvailable = false;
    }

    // Check if user_original_request contains characters that break zk CLI's --extra parser
    // The zk CLI fails when values contain quotes, equals signs, newlines, or other special chars
    // because it expects simple "key=value" format. When detected, force manual file creation.
    if (zkAvailable && request.user_original_request) {
      const hasNewlines = request.user_original_request.includes("\n");
      const hasSpecialChars =
        /[:\#\[\]\{\}\|\>\<\!\&\*\?\`\'\"\,\@\%\=]|^\s|\s$|^---|^\.\.\./.test(
          request.user_original_request
        );
      if (hasNewlines || hasSpecialChars) {
        // Force manual file creation path which handles escaping correctly via generateFrontmatter()
        zkAvailable = false;
      }
    }

    // Check if direct_prompt contains characters that break zk CLI's --extra parser
    // Same handling as user_original_request - force manual file creation for complex values
    if (zkAvailable && request.direct_prompt) {
      const hasNewlines = request.direct_prompt.includes("\n");
      const hasSpecialChars =
        /[:\#\[\]\{\}\|\>\<\!\&\*\?\`\'\"\,\@\%\=]|^\s|\s$|^---|^\.\.\./.test(
          request.direct_prompt
        );
      if (hasNewlines || hasSpecialChars) {
        // Force manual file creation path which handles escaping correctly via generateFrontmatter()
        zkAvailable = false;
      }
    }

    // run_finalizations is a nested map and cannot be represented safely via zk --extra.
    // Force manual file creation so metadata is preserved.
    if (zkAvailable && request.run_finalizations) {
      zkAvailable = false;
    }

    let relativePath: string;
    let noteId: string;

    if (zkAvailable) {
      const groupName = isGlobal ? `global/${entryType}` : null;

      const zkArgs: string[] = [
        "--title",
        sanitizedTitle,
        "--extra",
        `type=${entryType}`,
        "--extra",
        `status=${entryStatus}`,
      ];

      if (groupName) {
        zkArgs.push("--group", groupName);
      } else {
        zkArgs.push("--template", `${entryType}.md`);
      }

      if (request.tags && request.tags.length > 0) {
        const formattedTags = request.tags.map((t) => `\n  - ${t}`).join("");
        zkArgs.push("--extra", `tags=${formattedTags}`);
      }

      if (request.priority) {
        zkArgs.push("--extra", `priority=${request.priority}`);
      }

      if (request.depends_on && request.depends_on.length > 0) {
        const formattedDeps = request.depends_on
          .map((d) => `\n  - "${d.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`)
          .join("");
        zkArgs.push("--extra", `depends_on=${formattedDeps}`);
      }

      // Execution context for tasks
      if (request.workdir) {
        zkArgs.push("--extra", `workdir=${request.workdir}`);
      }
      if (request.git_remote) {
        zkArgs.push("--extra", `git_remote=${request.git_remote}`);
      }
      if (request.git_branch) {
        zkArgs.push("--extra", `git_branch=${request.git_branch}`);
      }
      if (request.merge_target_branch) {
        zkArgs.push("--extra", `merge_target_branch=${request.merge_target_branch}`);
      }
      if (request.merge_policy) {
        zkArgs.push("--extra", `merge_policy=${request.merge_policy}`);
      }
      if (request.merge_strategy) {
        zkArgs.push("--extra", `merge_strategy=${request.merge_strategy}`);
      }
      if (request.remote_branch_policy) {
        zkArgs.push("--extra", `remote_branch_policy=${request.remote_branch_policy}`);
      }
      if (request.open_pr_before_merge !== undefined) {
        zkArgs.push("--extra", `open_pr_before_merge=${request.open_pr_before_merge}`);
      }
      if (request.execution_mode) {
        zkArgs.push("--extra", `execution_mode=${request.execution_mode}`);
      }
      if (request.checkout_enabled !== undefined) {
        zkArgs.push("--extra", `checkout_enabled=${request.checkout_enabled}`);
      }
      if (request.complete_on_idle !== undefined) {
        zkArgs.push("--extra", `complete_on_idle=${request.complete_on_idle}`);
      }

      // User original request for validation
      // Note: Complex values (newlines, special chars) are handled above by forcing
      // the manual file creation path. If we reach here, the value is safe for zk CLI.
      if (request.user_original_request) {
        zkArgs.push(
          "--extra",
          `user_original_request=${request.user_original_request}`
        );
      }

      // OpenCode execution options
      // Note: Complex direct_prompt values (newlines, special chars) are handled above
      // by forcing the manual file creation path. If we reach here, the value is safe.
      if (request.direct_prompt) {
        zkArgs.push("--extra", `direct_prompt=${request.direct_prompt}`);
      }
      if (request.agent) {
        zkArgs.push("--extra", `agent=${request.agent}`);
      }
      if (request.model) {
        zkArgs.push("--extra", `model=${request.model}`);
      }

      if (request.generated !== undefined) {
        zkArgs.push("--extra", `generated=${request.generated}`);
      }
      if (request.generated_kind) {
        zkArgs.push("--extra", `generated_kind=${request.generated_kind}`);
      }
      if (request.generated_key) {
        zkArgs.push("--extra", `generated_key=${request.generated_key}`);
      }
      if (request.generated_by) {
        zkArgs.push("--extra", `generated_by=${request.generated_by}`);
      }

      // Target workdir for task execution
      if (request.target_workdir) {
        zkArgs.push("--extra", `target_workdir=${request.target_workdir}`);
      }

      // Feature grouping for tasks
      if (request.feature_id) {
        zkArgs.push("--extra", `feature_id=${request.feature_id}`);
      }
      if (request.feature_priority) {
        zkArgs.push("--extra", `feature_priority=${request.feature_priority}`);
      }
      if (request.feature_depends_on && request.feature_depends_on.length > 0) {
        const formattedFeatureDeps = request.feature_depends_on
          .map((d) => `\n  - "${d.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`)
          .join("");
        zkArgs.push("--extra", `feature_depends_on=${formattedFeatureDeps}`);
      }

      if (request.schedule) {
        zkArgs.push("--extra", `schedule=${request.schedule}`);
      }

      if (request.next_run) {
        zkArgs.push("--extra", `next_run=${request.next_run}`);
      }

      if (request.max_runs !== undefined) {
        zkArgs.push("--extra", `max_runs=${request.max_runs}`);
      }

      if (request.starts_at) {
        zkArgs.push("--extra", `starts_at=${request.starts_at}`);
      }

      if (request.expires_at) {
        zkArgs.push("--extra", `expires_at=${request.expires_at}`);
      }

      if (!isGlobal) {
        zkArgs.push("--extra", `projectId=${effectiveProjectId}`);
      }

      zkArgs.push(dir);

      const result = await execZkNew(zkArgs, finalContent);

      if (result.exitCode !== 0) {
        throw new Error(
          `Failed to create entry via zk: ${result.stderr || "Unknown error"}`
        );
      }

      relativePath = result.stdout
        .trim()
        .replace(this.config.brainDir + "/", "")
        .replace(this.config.brainDir, "");
      if (relativePath.startsWith("/")) {
        relativePath = relativePath.slice(1);
      }
      noteId = extractIdFromPath(relativePath);
    } else {
      // Fallback: manual file creation with 8-char ID (matching zk format)
      const shortId = generateShortId();
      const filename = `${shortId}.md`;
      relativePath = `${dir}/${filename}`;
      noteId = shortId;
      const fullPath = join(this.config.brainDir, relativePath);

      const frontmatter = generateFrontmatter({
        title: sanitizedTitle,
        type: entryType,
        tags: request.tags,
        status: entryStatus,
        projectId: isGlobal ? undefined : this.projectId,
        priority: request.priority,
        created: new Date().toISOString(),
        // Task dependencies (already normalized above)
        depends_on: request.depends_on,
        // Execution context for tasks
        workdir: request.workdir,
        git_remote: request.git_remote,
        git_branch: request.git_branch,
        merge_target_branch: request.merge_target_branch,
        merge_policy: request.merge_policy,
        merge_strategy: request.merge_strategy,
        remote_branch_policy: request.remote_branch_policy,
        open_pr_before_merge: request.open_pr_before_merge,
        execution_mode: request.execution_mode,
        checkout_enabled: request.checkout_enabled,
        complete_on_idle: request.complete_on_idle,
        target_workdir: request.target_workdir,
        // User intent for validation
        user_original_request: request.user_original_request,
        // Feature grouping
        feature_id: request.feature_id,
        feature_priority: request.feature_priority,
        feature_depends_on: request.feature_depends_on,
        // OpenCode execution options
        direct_prompt: request.direct_prompt,
        agent: request.agent,
        model: request.model,
        // Session traceability
        sessions: request.sessions,
        run_finalizations: request.run_finalizations,
        // Generated metadata
        generated: request.generated,
        generated_kind: request.generated_kind,
        generated_key: request.generated_key,
        generated_by: request.generated_by,
        // Cron metadata
        schedule: request.schedule,
        next_run: request.next_run,
        max_runs: request.max_runs,
        starts_at: request.starts_at,
        expires_at: request.expires_at,
        runs: request.runs as unknown as import("./zk-client").CronRun[] | undefined,
      });

      const fileContent = `---\n${frontmatter}---\n\n${finalContent}\n`;
      writeFileSync(fullPath, fileContent, "utf-8");
    }

    // Initialize in database
    initEntry(relativePath, effectiveProjectId);

    if (entryType === "task") {
      const featureId = this.normalizeFeatureId(request.feature_id);
      if (featureId) {
        await this.reconcileFeatureCheckoutDependencies(effectiveProjectId, featureId);
      }
    }

    // Return display title (normalized but not escaped) for client use
    const link = generateMarkdownLink(noteId, displayTitle);

    return {
      id: noteId,
      path: relativePath,
      title: displayTitle,
      type: entryType,
      status: entryStatus,
      link,
    };
  }

  /**
   * Retrieve an entry by path, ID, or title (brain_recall)
   */
  async recall(pathOrId?: string, title?: string): Promise<BrainEntry> {
    if (!pathOrId && !title) {
      throw new Error("Please provide a path, ID, or title to recall");
    }

    let note: ZkNote | null = null;
    let notePath: string | null = null;

    const isId = pathOrId && /^[a-z0-9]{8}$/.test(pathOrId);
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());

    if (zkAvailable) {
      try {
        if (pathOrId) {
          const result = await execZk([
            "list",
            "--format",
            "json",
            "--quiet",
            pathOrId,
          ]);
          if (result.exitCode === 0) {
            const notes = parseZkJsonOutput(result.stdout);
            if (notes.length > 0) {
              note = isId
                ? notes[0]
                : notes.find((n) => n.path === pathOrId) || notes[0];
              if (note) notePath = note.path;
            }
          }
        }

        if (!note && (title || (isId && !notePath))) {
          const searchTerm = title || pathOrId;
          const result = await execZk([
            "list",
            "--format",
            "json",
            "--quiet",
            "--match",
            searchTerm!,
            "--match-strategy",
            "exact",
          ]);
          if (result.exitCode === 0) {
            const notes = parseZkJsonOutput(result.stdout);
            note = notes.find((n) => n.title === searchTerm) || null;
            if (note) {
              notePath = note.path;
            } else if (notes.length > 0) {
              const suggestions = notes.slice(0, 5).map((n) => ({
                title: n.title,
                id: extractIdFromPath(n.path),
                path: n.path,
              }));
              throw new Error(
                `No exact match for: "${searchTerm}". Suggestions: ${JSON.stringify(suggestions)}`
              );
            }
          }

          // Search execution entries by name field
          if (!note && searchTerm) {
            const execResult = await execZk([
              "list",
              "--format",
              "json",
              "--quiet",
              "--tag",
              "execution",
            ]);
            if (execResult.exitCode === 0) {
              const execNotes = parseZkJsonOutput(execResult.stdout);
              for (const execNote of execNotes) {
                const execPath = join(this.config.brainDir, execNote.path);
                if (existsSync(execPath)) {
                  const execContent = readFileSync(execPath, "utf-8");
                  const { frontmatter: execFm } = parseFrontmatter(execContent);

                  if (execFm.name === searchTerm) {
                    note = {
                      ...execNote,
                      rawContent: execContent,
                      metadata: execFm,
                    };
                    notePath = execNote.path;
                    break;
                  }
                }
              }
            }
          }
        }
      } catch (error) {
        if (error instanceof Error && error.message.includes("No exact match")) {
          throw error;
        }
        // Fall through to direct file read
      }
    }

    // Fall back to direct file read
    if (!note && pathOrId && !isId) {
      const fullPath = join(this.config.brainDir, pathOrId);
      if (existsSync(fullPath)) {
        const content = readFileSync(fullPath, "utf-8");
        const { frontmatter, body } = parseFrontmatter(content);
        note = {
          path: pathOrId,
          title: (frontmatter.title as string) || pathOrId,
          rawContent: content,
          tags: [],
          metadata: frontmatter,
        };
        notePath = pathOrId;
      }
    }

    if (!note || !notePath) {
      throw new Error(
        `No entry found${pathOrId ? ` at path: ${pathOrId}` : ` matching title: "${title}"`}`
      );
    }

    // Read full content
    let content = note.rawContent;
    if (!content) {
      const fullPath = join(this.config.brainDir, notePath);
      if (existsSync(fullPath)) {
        content = readFileSync(fullPath, "utf-8");
      }
    }

    if (!content) {
      throw new Error(`Could not read content for: ${notePath}`);
    }

    const { frontmatter, body } = parseFrontmatter(content);

    // Record access
    recordAccess(notePath);
    const meta = getEntryMeta(notePath);

    const type = extractType(note);
    const status = extractStatus(note);
    const noteId = (frontmatter.id as string) || extractIdFromPath(notePath);
    const priority = extractPriority(note);

    return {
      id: noteId,
      path: notePath,
      title: (frontmatter.title as string) || note.title,
      type,
      status,
      content: body,
      tags: note.tags || [],
      priority,
      depends_on: frontmatter.depends_on as string[] | undefined,
      project_id: frontmatter.projectId as string | undefined,
      created: note.created,
      modified: note.modified,
      access_count: meta?.access_count ?? 1,
      last_verified: meta?.last_verified
        ? new Date(meta.last_verified).toISOString()
        : undefined,
      // Execution context for tasks
      workdir: frontmatter.workdir as string | undefined,
      git_remote: frontmatter.git_remote as string | undefined,
      git_branch: frontmatter.git_branch as string | undefined,
      merge_target_branch: frontmatter.merge_target_branch as string | undefined,
      merge_policy: frontmatter.merge_policy as BrainEntry["merge_policy"] | undefined,
      merge_strategy: frontmatter.merge_strategy as BrainEntry["merge_strategy"] | undefined,
      remote_branch_policy:
        frontmatter.remote_branch_policy as BrainEntry["remote_branch_policy"] | undefined,
      open_pr_before_merge: frontmatter.open_pr_before_merge as boolean | undefined,
      execution_mode: frontmatter.execution_mode as BrainEntry["execution_mode"] | undefined,
      checkout_enabled: frontmatter.checkout_enabled as boolean | undefined,
      complete_on_idle: frontmatter.complete_on_idle as boolean | undefined,
      // User intent for validation
      user_original_request: frontmatter.user_original_request as string | undefined,
      // Session traceability
      sessions: frontmatter.sessions as Record<string, SessionInfo> | undefined,
      run_finalizations:
        frontmatter.run_finalizations as Record<string, RunFinalization> | undefined,
      // Generated metadata
      generated: frontmatter.generated as boolean | undefined,
      generated_kind: frontmatter.generated_kind as import("./types").GeneratedKind | undefined,
      generated_key: frontmatter.generated_key as string | undefined,
      generated_by: frontmatter.generated_by as string | undefined,
      // Cron metadata
      schedule: frontmatter.schedule as string | undefined,
      next_run: frontmatter.next_run as string | undefined,
      max_runs: frontmatter.max_runs as number | undefined,
      starts_at: frontmatter.starts_at as string | undefined,
      expires_at: frontmatter.expires_at as string | undefined,
      runs: normalizeCronRuns(frontmatter.runs),
    };
  }

  /**
   * Update an existing entry (brain_update)
   */
  async update(path: string, request: UpdateEntryRequest): Promise<BrainEntry> {
    return this.updateInternal(path, request);
  }

  private async updateInternal(
    path: string,
    request: UpdateEntryRequest,
    options: UpdateOptions = {}
  ): Promise<BrainEntry> {
    const fullPath = join(this.config.brainDir, path);
    if (!existsSync(fullPath)) {
      throw new Error(`Entry not found: ${path}`);
    }

    if (
      request.status === undefined &&
      request.title === undefined &&
      request.content === undefined &&
      request.append === undefined &&
      request.note === undefined &&
      request.depends_on === undefined &&
      request.tags === undefined &&
      request.priority === undefined &&
      request.feature_id === undefined &&
      request.feature_priority === undefined &&
      request.feature_depends_on === undefined &&
      request.target_workdir === undefined &&
      request.git_branch === undefined &&
      request.merge_target_branch === undefined &&
      request.merge_policy === undefined &&
      request.merge_strategy === undefined &&
      request.remote_branch_policy === undefined &&
      request.open_pr_before_merge === undefined &&
      request.execution_mode === undefined &&
      request.checkout_enabled === undefined &&
      request.complete_on_idle === undefined &&
      request.direct_prompt === undefined &&
      request.agent === undefined &&
      request.model === undefined &&
      request.sessions === undefined &&
      request.run_finalizations === undefined &&
      request.generated === undefined &&
      request.generated_kind === undefined &&
      request.generated_key === undefined &&
      request.generated_by === undefined &&
      request.schedule === undefined &&
      request.next_run === undefined &&
      request.max_runs === undefined &&
      request.starts_at === undefined &&
      request.expires_at === undefined &&
      request.run_once_at === undefined &&
      request.runs === undefined
    ) {
      throw new Error(
        "No updates specified. Provide at least one of: status, title, content, append, note, depends_on, tags, priority, feature_id, feature_priority, feature_depends_on, target_workdir, git_branch, merge_target_branch, merge_policy, merge_strategy, remote_branch_policy, open_pr_before_merge, execution_mode, checkout_enabled, complete_on_idle, direct_prompt, agent, model, sessions, run_finalizations, generated, generated_kind, generated_key, generated_by, schedule, next_run, max_runs, starts_at, expires_at, run_once_at, runs"
      );
    }

    // Read existing content
    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);

    // Check if this is a task entry (for dependency validation)
    const entryType = frontmatter.type as string;
    const isTask = entryType === "task" || path.includes("/task/");
    
    // Determine project ID from path or frontmatter
    const projectMatch = path.match(/^projects\/([^/]+)\//);
    const projectId = projectMatch ? projectMatch[1] : (frontmatter.projectId as string) || this.projectId;

    // Validate depends_on for task entries
    if (
      isTask &&
      request.depends_on !== undefined &&
      request.depends_on.length > 0 &&
      !options.skipDependsOnValidation
    ) {
      const taskService = new TaskService(this.config, projectId);
      const validation = await taskService.validateDependencies(
        request.depends_on,
        projectId
      );
      
      if (!validation.valid) {
        throw new DependencyValidationError(validation.errors);
      }
      
      // Replace with normalized IDs for consistency
      request.depends_on = validation.normalized;
    }

    // Update frontmatter fields
    const oldStatus = (frontmatter.status as string) || "active";
    const newStatus = request.status || oldStatus;
    const oldTitle = (frontmatter.title as string) || path;
    const newTitle = request.title || oldTitle;

    // Build updated frontmatter preserving all existing fields
    const updatedFrontmatter = { ...frontmatter };
    updatedFrontmatter.title = newTitle;
    updatedFrontmatter.status = newStatus;

    // Update depends_on if provided
    if (request.depends_on !== undefined) {
      updatedFrontmatter.depends_on = request.depends_on;
    }

    // Update feature grouping fields if provided
    if (request.feature_id !== undefined) {
      updatedFrontmatter.feature_id = request.feature_id;
    }
    if (request.feature_priority !== undefined) {
      updatedFrontmatter.feature_priority = request.feature_priority;
    }
    if (request.feature_depends_on !== undefined) {
      updatedFrontmatter.feature_depends_on = request.feature_depends_on;
    }

    // Update tags if provided (replaces existing tags)
    if (request.tags !== undefined) {
      updatedFrontmatter.tags = request.tags;
    }

    // Update priority if provided
    if (request.priority !== undefined) {
      updatedFrontmatter.priority = request.priority;
    }

    // Update target_workdir if provided
    if (request.target_workdir !== undefined) {
      updatedFrontmatter.target_workdir = request.target_workdir;
    }

    // Update git_branch if provided
    if (request.git_branch !== undefined) {
      updatedFrontmatter.git_branch = request.git_branch;
    }
    if (request.merge_target_branch !== undefined) {
      updatedFrontmatter.merge_target_branch = request.merge_target_branch;
    }
    if (request.merge_policy !== undefined) {
      updatedFrontmatter.merge_policy = request.merge_policy;
    }
    if (request.merge_strategy !== undefined) {
      updatedFrontmatter.merge_strategy = request.merge_strategy;
    }
    if (request.remote_branch_policy !== undefined) {
      updatedFrontmatter.remote_branch_policy = request.remote_branch_policy;
    }
    if (request.open_pr_before_merge !== undefined) {
      updatedFrontmatter.open_pr_before_merge = request.open_pr_before_merge;
    }
    if (request.execution_mode !== undefined) {
      updatedFrontmatter.execution_mode = request.execution_mode;
    }
    if (request.checkout_enabled !== undefined) {
      updatedFrontmatter.checkout_enabled = request.checkout_enabled;
    }
    if (request.complete_on_idle !== undefined) {
      updatedFrontmatter.complete_on_idle = request.complete_on_idle;
    }

    // Update OpenCode execution options if provided
    if (request.direct_prompt !== undefined) {
      updatedFrontmatter.direct_prompt = request.direct_prompt;
    }
    if (request.agent !== undefined) {
      updatedFrontmatter.agent = request.agent;
    }
    if (request.model !== undefined) {
      updatedFrontmatter.model = request.model;
    }

    if (request.schedule !== undefined) {
      updatedFrontmatter.schedule = request.schedule;
      if (request.schedule && request.next_run === undefined) {
        updatedFrontmatter.next_run = getNextRun(request.schedule).toISOString();
      }
    }
    if (request.run_once_at !== undefined) {
      updatedFrontmatter.next_run = request.run_once_at;
    }
    if (request.next_run !== undefined) {
      updatedFrontmatter.next_run = request.next_run;
    }
    if (request.max_runs !== undefined) {
      updatedFrontmatter.max_runs = request.max_runs;
    }
    if (request.starts_at !== undefined) {
      updatedFrontmatter.starts_at = request.starts_at;
    }
    if (request.expires_at !== undefined) {
      updatedFrontmatter.expires_at = request.expires_at;
    }
    if (request.runs !== undefined) {
      updatedFrontmatter.runs = request.runs;
    }
    if (request.run_finalizations !== undefined) {
      updatedFrontmatter.run_finalizations = request.run_finalizations;
    }

    if (request.generated !== undefined) {
      updatedFrontmatter.generated = request.generated;
    }
    if (request.generated_kind !== undefined) {
      updatedFrontmatter.generated_kind = request.generated_kind;
    }
    if (request.generated_key !== undefined) {
      updatedFrontmatter.generated_key = request.generated_key;
    }
    if (request.generated_by !== undefined) {
      updatedFrontmatter.generated_by = request.generated_by;
    }

    // Update sessions with APPEND semantics (merge by session ID)
    if (request.sessions !== undefined && Object.keys(request.sessions).length > 0) {
      const existingSessions =
        (frontmatter.sessions as Record<string, SessionInfo> | undefined) || {};
      const requestSessions = Object.fromEntries(
        Object.entries(request.sessions).map(([sessionId, session]) => [
          sessionId,
          {
            ...session,
            timestamp: session.timestamp || new Date().toISOString(),
          },
        ])
      );

      updatedFrontmatter.sessions = {
        ...existingSessions,
        ...requestSessions,
      };
    }

    if (isTask && (newStatus === "completed" || newStatus === "validated")) {
      const allSessions =
        (updatedFrontmatter.sessions as Record<string, SessionInfo> | undefined) || {};
      const runCtx = resolveLatestRunContext(allSessions);
      if (runCtx?.runId) {
        const existingFinalizations =
          (updatedFrontmatter.run_finalizations as
            | Record<string, RunFinalization>
            | undefined) || {};

        updatedFrontmatter.run_finalizations = {
          ...existingFinalizations,
          [runCtx.runId]: {
            status: newStatus,
            finalized_at: new Date().toISOString(),
            ...(runCtx.sessionId ? { session_id: runCtx.sessionId } : {}),
          },
        };
      }
    }

    // Filter out status-tags from tags array (status is in status: field, not tags)
    if (Array.isArray(updatedFrontmatter.tags)) {
      updatedFrontmatter.tags = (updatedFrontmatter.tags as string[]).filter(
        (tag) => !ENTRY_STATUSES.includes(tag as EntryStatus)
      );
    }

    const newFrontmatter = serializeFrontmatter(updatedFrontmatter);

    // Build new body - full content replacement takes precedence
    let newBody: string;
    
    if (request.content !== undefined) {
      // Full content replacement - used by external editor workflow
      newBody = request.content;
    } else {
      // Incremental updates (append, notes, status changes)
      newBody = body;

      if (request.status && request.status !== oldStatus) {
        const timestamp = new Date().toISOString().split("T")[0];
        const statusNote = request.note
          ? `\n\n---\n*Status changed to **${newStatus}** on ${timestamp}: ${request.note}*`
          : `\n\n---\n*Status changed to **${newStatus}** on ${timestamp}*`;
        newBody += statusNote;
      } else if (request.note) {
        const timestamp = new Date().toISOString().split("T")[0];
        newBody += `\n\n---\n*Note (${timestamp}): ${request.note}*`;
      }

      if (request.append) {
        newBody += `\n\n${request.append}`;
      }
    }

    // Write updated file
    const newContent = `---\n${newFrontmatter}---\n\n${newBody}\n`;
    writeFileSync(fullPath, newContent, "utf-8");

    // Record access
    recordAccess(path);

    if (isTask && !options.skipFeatureCheckoutReconcile) {
      const featureIdsToReconcile = new Set<string>();
      const oldFeatureId = this.normalizeFeatureId(frontmatter.feature_id);
      const newFeatureId = this.normalizeFeatureId(updatedFrontmatter.feature_id);

      if (oldFeatureId) {
        featureIdsToReconcile.add(oldFeatureId);
      }
      if (newFeatureId) {
        featureIdsToReconcile.add(newFeatureId);
      }

      for (const featureId of featureIdsToReconcile) {
        await this.reconcileFeatureCheckoutDependencies(projectId, featureId);
      }
    }

    return this.recall(path);
  }

  /**
   * Delete an entry (brain_delete)
   */
  async delete(path: string): Promise<void> {
    const fullPath = join(this.config.brainDir, path);
    if (!existsSync(fullPath)) {
      throw new Error(`Entry not found: ${path}`);
    }

    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter } = parseFrontmatter(content);
    const entryType = (frontmatter.type as string) || "";
    const isTask = entryType === "task" || path.includes("/task/");
    const featureId = this.normalizeFeatureId(frontmatter.feature_id);
    const projectMatch = path.match(/^projects\/([^/]+)\//);
    const projectId =
      projectMatch?.[1] || (frontmatter.projectId as string) || this.projectId;

    // Delete file
    unlinkSync(fullPath);

    // Remove from database
    deleteEntryMeta(path);

    if (isTask && featureId) {
      await this.reconcileFeatureCheckoutDependencies(projectId, featureId);
    }
  }

  /**
   * Move an entry to a different project (brain_move)
   * 
   * Moves a brain entry from one project to another:
   * 1. Validates source entry exists
   * 2. Validates target project directory (creates if needed)
   * 3. Checks for ID collision in target
   * 4. Prevents moving in_progress tasks
   * 5. Updates frontmatter projectId
   * 6. Writes file to new path
   * 7. Deletes old file
   * 8. Updates SQLite atomically
   * 9. Runs zk index to update zk's internal index
   */
  async moveEntry(
    path: string,
    newProjectId: string
  ): Promise<{
    oldPath: string;
    newPath: string;
    project: string;
    id: string;
    title: string;
    updatedDependents: RewriteResult[];
  }> {
    const fullPath = join(this.config.brainDir, path);
    if (!existsSync(fullPath)) {
      throw new Error(`Entry not found: ${path}`);
    }

    // Read existing content and frontmatter
    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);
    
    // Extract entry type from path or frontmatter
    const pathMatch = path.match(/\/([^/]+)\/[^/]+\.md$/);
    const entryType = (frontmatter.type as string) || (pathMatch ? pathMatch[1] : "scratch");
    
    // Extract entry ID from filename
    const id = extractIdFromPath(path);
    const title = (frontmatter.title as string) || path;
    
    // Get current project from path
    const currentProjectMatch = path.match(/^projects\/([^/]+)\//);
    const currentProjectId = currentProjectMatch ? currentProjectMatch[1] : null;
    const featureId = this.normalizeFeatureId(frontmatter.feature_id);
    
    // Validate not moving to same project
    if (currentProjectId === newProjectId) {
      throw new Error(`Entry is already in project: ${newProjectId}`);
    }
    
    // Prevent moving in_progress tasks (agent may be actively working)
    const status = frontmatter.status as string;
    if (status === "in_progress") {
      throw new Error(`Cannot move in_progress task: ${id}. Complete or block it first.`);
    }
    
    // Build new path
    const newDir = `projects/${newProjectId}/${entryType}`;
    const newPath = `${newDir}/${id}.md`;
    const newFullDir = join(this.config.brainDir, newDir);
    const newFullPath = join(this.config.brainDir, newPath);
    
    // Check for ID collision in target
    if (existsSync(newFullPath)) {
      throw new Error(`Entry with ID ${id} already exists in project ${newProjectId}`);
    }
    
    // Create target directory if needed
    if (!existsSync(newFullDir)) {
      mkdirSync(newFullDir, { recursive: true });
    }
    
    // Update frontmatter with new projectId
    const updatedFrontmatter = { ...frontmatter };
    updatedFrontmatter.projectId = newProjectId;
    const newFrontmatterStr = serializeFrontmatter(updatedFrontmatter);
    
    // Build new file content
    const newContent = `---\n${newFrontmatterStr}---\n\n${body}\n`;
    
    // Write to new location
    writeFileSync(newFullPath, newContent, "utf-8");
    
    // Delete old file
    unlinkSync(fullPath);
    
    // Update SQLite atomically
    moveEntryMeta(path, newPath, newProjectId);
    
    // Run zk index to update zk's internal index (with retry on failure)
    // Uses incremental index (no --force) to avoid full reindex of entire brain.
    // Retries handle transient SQLite locks from concurrent access.
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    if (zkAvailable) {
      const MAX_INDEX_RETRIES = 3;
      const RETRY_BASE_DELAY_MS = 150;
      let indexSuccess = false;
      for (let i = 0; i < MAX_INDEX_RETRIES; i++) {
        try {
          const result = await execZk(["index", "--quiet"], 10000);
          if (result.exitCode === 0) {
            indexSuccess = true;
            break;
          }
          // Non-zero exit code — retry after delay
          if (i < MAX_INDEX_RETRIES - 1) {
            await new Promise((r) =>
              setTimeout(r, RETRY_BASE_DELAY_MS * (i + 1))
            );
          }
        } catch {
          // Spawn failure — retry after delay
          if (i < MAX_INDEX_RETRIES - 1) {
            await new Promise((r) =>
              setTimeout(r, RETRY_BASE_DELAY_MS * (i + 1))
            );
          }
        }
      }
      if (!indexSuccess) {
        console.warn(
          `[brain-service] zk index failed after ${MAX_INDEX_RETRIES} attempts after move: ${path} -> ${newPath}`
        );
      }
    }
    
    // Rewrite depends_on references in dependent tasks (task entries only)
    let updatedDependents: RewriteResult[] = [];
    if (entryType === "task" && currentProjectId) {
      try {
        // Build task map across all projects
        const taskService = new TaskService(this.config, currentProjectId);
        const allProjects = taskService.listProjects();
        const tasksByProject = new Map<string, import("./types").Task[]>();

        for (const projId of allProjects) {
          try {
            const tasks = await taskService.getAllTasks(projId);
            tasksByProject.set(projId, tasks);
          } catch {
            // Skip projects where task listing fails
          }
        }

        // Find all tasks that reference the moved task
        const dependents = findDependents(id, currentProjectId, tasksByProject);

        // Rewrite their depends_on references on disk
        updatedDependents = rewriteDependentFiles({
          brainDir: this.config.brainDir,
          dependents,
          movedTaskId: id,
          sourceProjectId: currentProjectId,
          targetProjectId: newProjectId,
        });
      } catch {
        // Dep rewriting is best-effort — don't fail the move
      }
    }

    if (entryType === "task" && featureId && currentProjectId) {
      await this.reconcileFeatureCheckoutDependencies(currentProjectId, featureId);
      await this.reconcileFeatureCheckoutDependencies(newProjectId, featureId);
    }
    
    return {
      oldPath: path,
      newPath,
      project: newProjectId,
      id,
      title,
      updatedDependents,
    };
  }

  async markFeatureForCheckout(
    projectId: string,
    featureId: string,
    options?: FeatureCheckoutRequest
  ): Promise<MarkFeatureForCheckoutResult> {
    const sanitizedProjectId = sanitizeSimpleValue(projectId);
    const sanitizedFeatureId = sanitizeSimpleValue(featureId);

    if (!sanitizedProjectId) {
      throw new Error("projectId is required");
    }
    if (!sanitizedFeatureId) {
      throw new Error("featureId is required");
    }

    const normalizedOptions = this.normalizeFeatureCheckoutOptions(options);
    const mergePolicy = normalizedOptions.merge_policy ?? "auto_merge";
    const mergeStrategy = normalizedOptions.merge_strategy ?? "squash";
    const remoteBranchPolicy = normalizedOptions.remote_branch_policy ?? "delete";
    const executionMode = normalizedOptions.execution_mode ?? "worktree";
    const openPrBeforeMerge = normalizedOptions.open_pr_before_merge ?? false;

    if (
      normalizedOptions.execution_branch
      && normalizedOptions.merge_target_branch
      && normalizedOptions.execution_branch === normalizedOptions.merge_target_branch
    ) {
      throw new Error("execution_branch must be different from merge_target_branch");
    }

    if (
      mergePolicy === "auto_merge"
      && normalizedOptions.merge_target_branch
      && this.isProtectedMergeTargetBranch(normalizedOptions.merge_target_branch)
      && openPrBeforeMerge !== true
    ) {
      throw new Error(
        `open_pr_before_merge must be true when auto-merging into protected branch: ${normalizedOptions.merge_target_branch}`
      );
    }

    const generatedKey = `feature-checkout:${sanitizedFeatureId}:round-1`;
    const leaseOwner = `brain-service:${process.pid}:${Date.now()}:${Math.random().toString(36).slice(2, 8)}`;
    const waitDeadline = Date.now() + GENERATED_TASK_BUSY_WAIT_TIMEOUT_MS;

    while (true) {
      const lease = acquireGeneratedTaskLease(
        sanitizedProjectId,
        generatedKey,
        leaseOwner,
        GENERATED_TASK_LEASE_DURATION_MS
      );

      if (lease.status === "exists" && lease.taskPath) {
        return {
          created: false,
          generatedKey,
          task: await this.getCreateResponseForPath(lease.taskPath),
        };
      }

      if (lease.status === "acquired") {
        const dependsOn = await this.getNonGeneratedFeatureTaskIds(
          sanitizedProjectId,
          sanitizedFeatureId
        );

        const createdTask = await this.save({
          type: "task",
          title: `Feature checkout: ${sanitizedFeatureId}`,
          content: this.buildFeatureCheckoutContent({
            featureId: sanitizedFeatureId,
            executionBranch: normalizedOptions.execution_branch,
            mergeTargetBranch: normalizedOptions.merge_target_branch,
            mergePolicy,
            mergeStrategy,
            remoteBranchPolicy,
            openPrBeforeMerge,
          }),
          status: "pending",
          priority: "medium",
          project: sanitizedProjectId,
          feature_id: sanitizedFeatureId,
          depends_on: dependsOn,
          tags: ["checkout", sanitizedFeatureId],
          generated: true,
          generated_kind: "feature_checkout",
          generated_key: generatedKey,
          generated_by: "feature-checkout",
          git_branch: normalizedOptions.execution_branch,
          merge_target_branch: normalizedOptions.merge_target_branch,
          merge_policy: mergePolicy,
          merge_strategy: mergeStrategy,
          remote_branch_policy: remoteBranchPolicy,
          open_pr_before_merge: openPrBeforeMerge,
          execution_mode: executionMode,
        });

        const finalized = completeGeneratedTaskLease(
          sanitizedProjectId,
          generatedKey,
          leaseOwner,
          createdTask.path
        );

        if (!finalized) {
          const row = getGeneratedTaskKey(sanitizedProjectId, generatedKey);
          if (row?.task_path) {
            return {
              created: false,
              generatedKey,
              task: await this.getCreateResponseForPath(row.task_path),
            };
          }
          throw new Error(
            `Failed to finalize generated task lease for key ${generatedKey}`
          );
        }

        return {
          created: true,
          generatedKey,
          task: createdTask,
        };
      }

      if (Date.now() >= waitDeadline) {
        const row = getGeneratedTaskKey(sanitizedProjectId, generatedKey);
        if (row?.task_path) {
          return {
            created: false,
            generatedKey,
            task: await this.getCreateResponseForPath(row.task_path),
          };
        }
        throw new Error(
          `Timed out waiting for generated task key lease: ${generatedKey}`
        );
      }

      await new Promise((resolve) =>
        setTimeout(resolve, GENERATED_TASK_BUSY_POLL_INTERVAL_MS)
      );
    }
  }

  private normalizeFeatureCheckoutOptions(options?: FeatureCheckoutRequest): FeatureCheckoutRequest {
    if (!options) {
      return {};
    }

    const normalized: FeatureCheckoutRequest = { ...options };

    if (normalized.execution_branch !== undefined) {
      const value = sanitizeSimpleValue(normalized.execution_branch);
      normalized.execution_branch = value || undefined;
    }

    if (normalized.merge_target_branch !== undefined) {
      const value = sanitizeSimpleValue(normalized.merge_target_branch);
      normalized.merge_target_branch = value || undefined;
    }

    if (normalized.merge_policy !== undefined) {
      normalized.merge_policy = sanitizeSimpleValue(normalized.merge_policy) as FeatureCheckoutRequest["merge_policy"];
    }

    if (normalized.merge_strategy !== undefined) {
      normalized.merge_strategy = sanitizeSimpleValue(normalized.merge_strategy) as FeatureCheckoutRequest["merge_strategy"];
    }

    if (normalized.execution_mode !== undefined) {
      normalized.execution_mode = sanitizeSimpleValue(normalized.execution_mode) as FeatureCheckoutRequest["execution_mode"];
    }

    if (normalized.remote_branch_policy !== undefined) {
      normalized.remote_branch_policy = sanitizeSimpleValue(normalized.remote_branch_policy) as FeatureCheckoutRequest["remote_branch_policy"];
    }

    return normalized;
  }

  private isProtectedMergeTargetBranch(branch: string): boolean {
    const normalized = branch.trim().toLowerCase();
    return normalized === "main" || normalized === "master";
  }

  private buildFeatureCheckoutContent(params: {
    featureId: string;
    executionBranch?: string;
    mergeTargetBranch?: string;
    mergePolicy: NonNullable<FeatureCheckoutRequest["merge_policy"]>;
    mergeStrategy: NonNullable<FeatureCheckoutRequest["merge_strategy"]>;
    remoteBranchPolicy: NonNullable<FeatureCheckoutRequest["remote_branch_policy"]>;
    openPrBeforeMerge: boolean;
  }): string {
    const executionBranch = params.executionBranch ?? "(default branch for execution context)";
    const mergeTargetBranch = params.mergeTargetBranch ?? "(no explicit merge target)";
    const lines = [
      `Automated feature checkout for ${params.featureId}.`,
      "",
      "Merge intent:",
      `- execution_branch: ${executionBranch}`,
      `- merge_target_branch: ${mergeTargetBranch}`,
      `- merge_policy: ${params.mergePolicy}`,
      `- merge_strategy: ${params.mergeStrategy}`,
      `- remote_branch_policy: ${params.remoteBranchPolicy}`,
      `- open_pr_before_merge: ${params.openPrBeforeMerge}`,
      "",
      "Safety gates before merge:",
      "- checkout validation pass",
      "- merge precheck pass",
      "- verification commands pass",
      "",
      "Guardrails:",
      "- If merge target is a protected branch, use open_pr_before_merge before auto merge",
      "- For optional PR-before-merge flow, ensure PR checks are green before final merge",
      "- cleanup only after confirmed successful push",
    ];

    return lines.join("\n");
  }

  async reconcileFeatureCheckoutDependencies(projectId: string, featureId: string): Promise<void> {
    const sanitizedProjectId = sanitizeSimpleValue(projectId);
    const sanitizedFeatureId = sanitizeSimpleValue(featureId);

    if (!sanitizedProjectId) {
      throw new Error("projectId is required");
    }
    if (!sanitizedFeatureId) {
      throw new Error("featureId is required");
    }

    const featureTasks = await this.getFeatureTasks(sanitizedProjectId, sanitizedFeatureId);
    const desiredDependsOn = this.extractUniqueNonGeneratedTaskIds(featureTasks);
    const checkoutTasks = this.extractFeatureCheckoutTasks(featureTasks);

    for (const checkoutTask of checkoutTasks) {
      if (checkoutTask.status === "in_progress") {
        continue;
      }
      if (this.isTerminalCheckoutStatus(checkoutTask.status)) {
        continue;
      }
      if (!checkoutTask.path) {
        continue;
      }

      const currentDependsOn = this.normalizeTaskIds(checkoutTask.depends_on);
      if (this.areTaskIdArraysEqual(currentDependsOn, desiredDependsOn)) {
        continue;
      }

      await this.updateInternal(
        checkoutTask.path,
        { depends_on: desiredDependsOn },
        { skipFeatureCheckoutReconcile: true, skipDependsOnValidation: true }
      );
    }
  }

  private normalizeFeatureId(value: unknown): string | undefined {
    if (typeof value !== "string") {
      return undefined;
    }

    const sanitized = sanitizeSimpleValue(value);
    return sanitized || undefined;
  }

  private async getCreateResponseForPath(path: string): Promise<CreateEntryResponse> {
    const entry = await this.recall(path);
    return {
      id: entry.id,
      path: entry.path,
      title: entry.title,
      type: entry.type,
      status: entry.status,
      link: generateMarkdownLink(entry.id, entry.title),
    };
  }

  private async getNonGeneratedFeatureTaskIds(
    projectId: string,
    featureId: string
  ): Promise<string[]> {
    const featureTasks = await this.getFeatureTasks(projectId, featureId);
    return this.extractUniqueNonGeneratedTaskIds(featureTasks);
  }

  private async getFeatureTasks(projectId: string, featureId: string): Promise<Task[]> {
    const taskService = new TaskService(this.config, projectId);

    try {
      return await taskService.getTasksByFeature(projectId, featureId);
    } catch {
      return this.getFeatureTasksFromFilesystem(projectId, featureId);
    }
  }

  private getFeatureTasksFromFilesystem(projectId: string, featureId: string): Task[] {
    const taskDir = join(this.config.brainDir, "projects", projectId, "task");
    if (!existsSync(taskDir)) {
      return [];
    }

    const tasks: Task[] = [];
    const entries = readdirSync(taskDir, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isFile() || !entry.name.endsWith(".md")) {
        continue;
      }

      const relPath = `projects/${projectId}/task/${entry.name}`;
      const fullPath = join(taskDir, entry.name);
      const raw = readFileSync(fullPath, "utf-8");
      const { frontmatter } = parseFrontmatter(raw);

      if (frontmatter.feature_id !== featureId) {
        continue;
      }

      tasks.push({
        id: extractIdFromPath(relPath),
        path: relPath,
        title: (frontmatter.title as string) || extractIdFromPath(relPath),
        priority: (frontmatter.priority as Task["priority"]) || "medium",
        status: (frontmatter.status as Task["status"]) || "pending",
        depends_on: (frontmatter.depends_on as string[]) || [],
        tags: (frontmatter.tags as string[]) || [],
        created: (frontmatter.created as string) || "",
        modified: undefined,
        target_workdir: (frontmatter.target_workdir as string) || null,
        workdir: (frontmatter.workdir as string) || null,
        worktree: null,
        git_remote: (frontmatter.git_remote as string) || null,
        git_branch: (frontmatter.git_branch as string) || null,
        merge_target_branch: (frontmatter.merge_target_branch as string) || null,
        merge_policy:
          (frontmatter.merge_policy as Task["merge_policy"]) || "auto_merge",
        merge_strategy:
          (frontmatter.merge_strategy as Task["merge_strategy"]) || "squash",
        remote_branch_policy:
          (frontmatter.remote_branch_policy as Task["remote_branch_policy"]) || "delete",
        open_pr_before_merge:
          (frontmatter.open_pr_before_merge as boolean) || false,
        execution_mode:
          (frontmatter.execution_mode as Task["execution_mode"]) || "worktree",
        checkout_enabled: (frontmatter.checkout_enabled as boolean) ?? true,
        user_original_request: (frontmatter.user_original_request as string) || null,
        feature_id: (frontmatter.feature_id as string) || undefined,
        feature_priority: (frontmatter.feature_priority as Task["feature_priority"]) || undefined,
        feature_depends_on: (frontmatter.feature_depends_on as string[]) || undefined,
        direct_prompt: (frontmatter.direct_prompt as string) || null,
        agent: (frontmatter.agent as string) || null,
        model: (frontmatter.model as string) || null,
        sessions: (frontmatter.sessions as Task["sessions"]) || {},
        generated: (frontmatter.generated as boolean) || undefined,
        generated_kind: (frontmatter.generated_kind as Task["generated_kind"]) || undefined,
        generated_key: (frontmatter.generated_key as string) || undefined,
        generated_by: (frontmatter.generated_by as string) || undefined,
        frontmatter: frontmatter as Record<string, unknown>,
        projectId: projectId,
      });
    }

    return tasks;
  }

  private extractUniqueNonGeneratedTaskIds(tasks: Task[]): string[] {
    const ids = new Set<string>();

    for (const task of tasks) {
      if (task.generated === true) {
        continue;
      }
      if (!task.id) {
        continue;
      }
      ids.add(task.id);
    }

    return Array.from(ids).sort();
  }

  private extractFeatureCheckoutTasks(tasks: Task[]): Task[] {
    return tasks.filter(
      (task) =>
        task.generated === true &&
        task.generated_kind === "feature_checkout" &&
        task.generated_by === "feature-checkout"
    );
  }

  private isTerminalCheckoutStatus(status: Task["status"]): boolean {
    return (
      status === "completed" ||
      status === "validated" ||
      status === "cancelled" ||
      status === "superseded" ||
      status === "archived"
    );
  }

  private normalizeTaskIds(ids: string[] | undefined): string[] {
    if (!ids || ids.length === 0) {
      return [];
    }

    const normalized = new Set<string>();
    for (const id of ids) {
      const sanitized = sanitizeDependsOnEntry(id);
      if (sanitized) {
        normalized.add(sanitized);
      }
    }

    return Array.from(normalized).sort();
  }

  private areTaskIdArraysEqual(a: string[], b: string[]): boolean {
    if (a.length !== b.length) {
      return false;
    }

    for (let i = 0; i < a.length; i += 1) {
      if (a[i] !== b[i]) {
        return false;
      }
    }

    return true;
  }

  // ========================================
  // Search & List
  // ========================================

  /**
   * Search the brain (brain_search)
   */
  async search(request: SearchRequest): Promise<SearchResponse> {
    const limit = request.limit ?? 10;
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());

    if (!zkAvailable) {
      throw new Error(
        "zk CLI not available. Install from https://github.com/zk-org/zk"
      );
    }

    const needsCodeFiltering = request.status || request.feature_id || request.tags;
    const fetchLimit = needsCodeFiltering ? limit * 5 : limit;
    const zkArgs = [
      "list",
      "--format",
      "json",
      "--quiet",
      "--match",
      request.query,
      "--limit",
      String(fetchLimit),
    ];

    if (request.type) {
      zkArgs.push("--tag", request.type);
    }

    if (request.global) {
      zkArgs.push("global");
    }

    const result = await execZk(zkArgs);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return { results: [], total: 0 };
    }

    let notes = parseZkJsonOutput(result.stdout);

    if (request.status) {
      notes = notes.filter((note) => extractStatus(note) === request.status);
    }

    // Feature ID filtering (in code since zk doesn't support frontmatter filtering)
    if (request.feature_id) {
      notes = notes.filter((note) => {
        const fm = note.metadata || {};
        return fm.feature_id === request.feature_id;
      });
    }

    // Tag filtering (OR logic - matches entries with any of the specified tags)
    if (request.tags && request.tags.length > 0) {
      notes = notes.filter((note) => {
        const noteTags = note.tags || [];
        return request.tags!.some(tag => noteTags.includes(tag));
      });
    }

    notes = notes.slice(0, limit);

    const results: BrainEntry[] = notes.map((note) => ({
      id: extractIdFromPath(note.path),
      path: note.path,
      title: note.title,
      type: extractType(note),
      status: extractStatus(note),
      content: note.lead || note.body?.slice(0, 150) || "",
      tags: note.tags || [],
      priority: extractPriority(note),
      created: note.created,
      modified: note.modified,
    }));

    return { results, total: results.length };
  }

  /**
   * List entries (brain_list)
   */
  async list(request: ListEntriesRequest): Promise<ListEntriesResponse> {
    const limit = request.limit ?? 20;
    const offset = request.offset ?? 0;
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());

    if (!zkAvailable) {
      throw new Error(
        "zk CLI not available. Install from https://github.com/zk-org/zk"
      );
    }

    const needsCodeFiltering = request.status || request.filename || request.feature_id || request.tags;
    const fetchLimit = needsCodeFiltering ? Math.max(limit * 5, 100) : limit + offset;
    const zkArgs = [
      "list",
      "--format",
      "json",
      "--quiet",
      "--limit",
      String(fetchLimit),
    ];

    if (request.type) {
      zkArgs.push("--tag", request.type);
    }

    if (request.global) {
      zkArgs.push("global");
    }

    const result = await execZk(zkArgs);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return { entries: [], total: 0, limit, offset };
    }

    let notes = parseZkJsonOutput(result.stdout);

    if (request.status) {
      notes = notes.filter((note) => extractStatus(note) === request.status);
    }

    if (request.filename) {
      notes = notes.filter((note) => {
        const noteFilename = extractIdFromPath(note.path);
        return matchesFilenamePattern(noteFilename, request.filename!);
      });
    }

    // Feature ID filtering (in code since zk doesn't support frontmatter filtering)
    if (request.feature_id) {
      notes = notes.filter((note) => {
        const fm = note.metadata || {};
        return fm.feature_id === request.feature_id;
      });
    }

    // Tag filtering (OR logic - matches entries with any of the specified tags)
    if (request.tags && request.tags.length > 0) {
      notes = notes.filter((note) => {
        const noteTags = note.tags || [];
        return request.tags!.some(tag => noteTags.includes(tag));
      });
    }

    if (request.sortBy === "priority") {
      notes.sort((a, b) => {
        const aPriority = getPrioritySortValue(extractPriority(a));
        const bPriority = getPrioritySortValue(extractPriority(b));
        if (aPriority !== bPriority) return aPriority - bPriority;
        return (a.created || "").localeCompare(b.created || "");
      });
    }

    const total = notes.length;
    notes = notes.slice(offset, offset + limit);

    const entries: BrainEntry[] = notes.map((note) => ({
      id: extractIdFromPath(note.path),
      path: note.path,
      title: note.title,
      type: extractType(note),
      status: extractStatus(note),
      content: "",
      tags: note.tags || [],
      priority: extractPriority(note),
      created: note.created,
      modified: note.modified,
      access_count: getEntryMeta(note.path)?.access_count ?? 0,
    }));

    return { entries, total, limit, offset };
  }

  /**
   * Inject context from brain (brain_inject)
   */
  async inject(request: InjectRequest): Promise<InjectResponse> {
    const limit = request.maxEntries ?? 5;
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());

    if (!zkAvailable) {
      return {
        context: `No relevant brain context found for "${request.query}" (zk not available)`,
        entries: [],
      };
    }

    const zkArgs = [
      "list",
      "--format",
      "json",
      "--quiet",
      "--match",
      request.query,
      "--limit",
      String(limit),
    ];

    if (request.type) {
      zkArgs.push("--tag", request.type);
    }

    const result = await execZk(zkArgs);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return {
        context: `No relevant brain context found for "${request.query}"`,
        entries: [],
      };
    }

    const notes = parseZkJsonOutput(result.stdout);

    if (notes.length === 0) {
      return {
        context: `No relevant brain context found for "${request.query}"`,
        entries: [],
      };
    }

    // Record access for all returned entries
    for (const note of notes) {
      recordAccess(note.path);
    }

    const lines = [
      `## Relevant Brain Context`,
      "",
      `Found ${notes.length} relevant entries for: "${request.query}"`,
      "",
      "---",
      "",
    ];

    const entries: BrainEntry[] = [];

    for (const note of notes) {
      const fullPath = join(this.config.brainDir, note.path);
      let content = "";
      if (existsSync(fullPath)) {
        const raw = readFileSync(fullPath, "utf-8");
        const { body } = parseFrontmatter(raw);
        content = body;
      }

      const type = extractType(note);
      const globalBadge = note.path.startsWith("global/") ? " (global)" : "";

      lines.push(`### ${note.title}${globalBadge}`);
      lines.push(`*Type: ${type} | Tags: ${note.tags?.join(", ") || "none"}*`);
      lines.push("");
      lines.push(content || note.body || note.lead || "(no content)");
      lines.push("");
      lines.push("---");
      lines.push("");

      entries.push({
        id: extractIdFromPath(note.path),
        path: note.path,
        title: note.title,
        type,
        status: extractStatus(note),
        content,
        tags: note.tags || [],
        priority: extractPriority(note),
        created: note.created,
        modified: note.modified,
      });
    }

    return {
      context: lines.join("\n"),
      entries,
    };
  }

  // ========================================
  // Graph Operations
  // ========================================

  /**
   * Find entries that link TO a given entry (brain_backlinks)
   */
  async getBacklinks(path: string): Promise<BrainEntry[]> {
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    if (!zkAvailable) {
      throw new Error("zk CLI not available for backlink detection");
    }

    const result = await execZk([
      "list",
      "--format",
      "json",
      "--quiet",
      "--link-to",
      path,
    ]);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return [];
    }

    const notes = parseZkJsonOutput(result.stdout);

    return notes.map((note) => ({
      id: extractIdFromPath(note.path),
      path: note.path,
      title: note.title,
      type: extractType(note),
      status: extractStatus(note),
      content: "",
      tags: note.tags || [],
      priority: extractPriority(note),
      created: note.created,
      modified: note.modified,
    }));
  }

  /**
   * Find entries that a given entry links TO (brain_outlinks)
   */
  async getOutlinks(path: string): Promise<BrainEntry[]> {
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    if (!zkAvailable) {
      throw new Error("zk CLI not available for outlink detection");
    }

    const result = await execZk([
      "list",
      "--format",
      "json",
      "--quiet",
      "--linked-by",
      path,
    ]);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return [];
    }

    const notes = parseZkJsonOutput(result.stdout);

    return notes.map((note) => ({
      id: extractIdFromPath(note.path),
      path: note.path,
      title: note.title,
      type: extractType(note),
      status: extractStatus(note),
      content: "",
      tags: note.tags || [],
      priority: extractPriority(note),
      created: note.created,
      modified: note.modified,
    }));
  }

  /**
   * Find entries that share linked notes with a given entry (brain_related)
   */
  async getRelated(path: string, limit = 10): Promise<BrainEntry[]> {
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    if (!zkAvailable) {
      throw new Error("zk CLI not available for related note detection");
    }

    const result = await execZk([
      "list",
      "--format",
      "json",
      "--quiet",
      "--related",
      path,
      "--limit",
      String(limit),
    ]);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return [];
    }

    const notes = parseZkJsonOutput(result.stdout);

    return notes.map((note) => ({
      id: extractIdFromPath(note.path),
      path: note.path,
      title: note.title,
      type: extractType(note),
      status: extractStatus(note),
      content: "",
      tags: note.tags || [],
      priority: extractPriority(note),
      created: note.created,
      modified: note.modified,
    }));
  }

  // ========================================
  // Health Operations
  // ========================================

  /**
   * Find entries with no incoming links (brain_orphans)
   */
  async getOrphans(type?: EntryType, limit = 20): Promise<BrainEntry[]> {
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    if (!zkAvailable) {
      throw new Error("zk CLI not available for orphan detection");
    }

    const zkArgs = [
      "list",
      "--format",
      "json",
      "--quiet",
      "--orphan",
      "--limit",
      String(limit),
    ];
    if (type) {
      zkArgs.push(`global/${type}`);
    }

    const result = await execZk(zkArgs);

    if (result.exitCode !== 0 || !result.stdout.trim()) {
      return [];
    }

    let notes = parseZkJsonOutput(result.stdout);

    if (type) {
      notes = notes.filter((n) => extractType(n) === type);
    }

    return notes.map((note) => ({
      id: extractIdFromPath(note.path),
      path: note.path,
      title: note.title,
      type: extractType(note),
      status: extractStatus(note),
      content: "",
      tags: note.tags || [],
      priority: extractPriority(note),
      created: note.created,
      modified: note.modified,
    }));
  }

  /**
   * Find entries that may need verification (brain_stale)
   */
  async getStale(days = 30, limit = 20): Promise<BrainEntry[]> {
    const stalePaths = getStaleEntries(days);

    if (stalePaths.length === 0) {
      return [];
    }

    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    const results: BrainEntry[] = [];

    let count = 0;
    for (const path of stalePaths) {
      if (count >= limit) break;

      const meta = getEntryMeta(path);
      let title = path;
      let type: EntryType = "scratch";
      let status: EntryStatus = "active";

      if (zkAvailable) {
        try {
          const result = await execZk([
            "list",
            "--format",
            "json",
            "--quiet",
            path,
          ]);
          if (result.exitCode === 0) {
            const notes = parseZkJsonOutput(result.stdout);
            if (notes[0]) {
              title = notes[0].title;
              type = extractType(notes[0]);
              status = extractStatus(notes[0]);
            }
          }
        } catch {
          /* use defaults */
        }
      }

      results.push({
        id: extractIdFromPath(path),
        path,
        title,
        type,
        status,
        content: "",
        tags: [],
        last_verified: meta?.last_verified
          ? new Date(meta.last_verified).toISOString()
          : undefined,
      });
      count++;
    }

    return results;
  }

  /**
   * Mark an entry as verified (brain_verify)
   */
  async verify(path: string): Promise<void> {
    const fullPath = join(this.config.brainDir, path);
    if (!existsSync(fullPath)) {
      throw new Error(`Entry not found: ${path}`);
    }

    setVerified(path);
  }

  /**
   * Get brain statistics (brain_stats)
   */
  async getStats(global?: boolean): Promise<StatsResponse> {
    const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
    const zkVersion = zkAvailable ? await getZkVersion() : null;

    let totalEntries = 0;
    let globalEntries = 0;
    let projectEntries = 0;
    const byType: Record<string, number> = {};
    let orphanCount = 0;

    if (zkAvailable) {
      try {
        const result = await execZk(["list", "--format", "json", "--quiet"]);
        if (result.exitCode === 0) {
          const notes = parseZkJsonOutput(result.stdout);
          totalEntries = notes.length;

          for (const note of notes) {
            const type = extractType(note);
            byType[type] = (byType[type] || 0) + 1;
            if (note.path.startsWith("global/")) globalEntries++;
            else projectEntries++;
          }

          const orphanResult = await execZk([
            "list",
            "--format",
            "json",
            "--quiet",
            "--orphan",
          ]);
          if (orphanResult.exitCode === 0) {
            const orphans = parseZkJsonOutput(orphanResult.stdout);
            orphanCount = orphans.length;
          }
        }
      } catch {
        /* ignore */
      }
    }

    const trackedEntries = getTrackedEntryCount();
    const staleCount = getStaleEntries(30).length;

    return {
      zkAvailable,
      zkVersion,
      notebookExists: isZkNotebookExists(),
      brainDir: this.config.brainDir,
      dbPath: this.config.dbPath,
      totalEntries,
      globalEntries,
      projectEntries,
      byType,
      orphanCount,
      trackedEntries,
      staleCount,
    };
  }

  // ========================================
  // Links
  // ========================================

  /**
   * Generate a markdown link to a brain entry (brain_link)
   */
  async generateLink(request: LinkRequest): Promise<LinkResponse> {
    if (!request.path && !request.title) {
      throw new Error(
        "Please provide either a path, ID, or title to generate a link"
      );
    }

    const includeTitle = request.withTitle !== false;
    let resolvedPath: string | null = null;
    let resolvedTitle: string | null = null;
    let resolvedId: string | null = null;

    const isId = request.path && /^[a-z0-9]{8}$/.test(request.path);

    if (request.path) {
      const zkAvailable = isZkNotebookExists() && (await isZkAvailable());

      if (zkAvailable) {
        try {
          const result = await execZk([
            "list",
            "--format",
            "json",
            "--quiet",
            request.path,
          ]);
          if (result.exitCode === 0) {
            const notes = parseZkJsonOutput(result.stdout);
            if (notes.length > 0) {
              const note = notes[0];
              resolvedPath = note.path;
              resolvedTitle = note.title;
              resolvedId = extractIdFromPath(note.path);
            }
          }
        } catch {
          /* fall through */
        }
      }

      if (!resolvedPath) {
        const pathToCheck = isId
          ? request.path
          : request.path.endsWith(".md")
            ? request.path
            : `${request.path}.md`;
        const fullPath = join(this.config.brainDir, pathToCheck);

        if (existsSync(fullPath)) {
          resolvedPath = pathToCheck;
          resolvedId = extractIdFromPath(pathToCheck);

          try {
            const content = readFileSync(fullPath, "utf-8");
            const { frontmatter } = parseFrontmatter(content);
            resolvedTitle = (frontmatter.title as string) || request.path;
          } catch {
            resolvedTitle = request.path;
          }
        } else if (!isId) {
          throw new Error(`Entry not found at path: ${request.path}`);
        }
      }
    }

    if (!resolvedPath && (request.title || (isId && !resolvedPath))) {
      const searchTerm = request.title || request.path;
      const zkAvailable = isZkNotebookExists() && (await isZkAvailable());

      if (!zkAvailable) {
        throw new Error("zk CLI not available. Cannot resolve title to path.");
      }

      try {
        const result = await execZk([
          "list",
          "--format",
          "json",
          "--quiet",
          "--match",
          searchTerm!,
          "--match-strategy",
          "exact",
        ]);

        if (result.exitCode === 0) {
          const notes = parseZkJsonOutput(result.stdout);
          const exactMatch = notes.find((n) => n.title === searchTerm);

          if (exactMatch) {
            resolvedPath = exactMatch.path;
            resolvedTitle = exactMatch.title;
            resolvedId = extractIdFromPath(exactMatch.path);
          } else if (notes.length > 0) {
            const suggestions = notes.slice(0, 5).map((n) => ({
              title: n.title,
              path: n.path,
              id: extractIdFromPath(n.path),
            }));
            throw new Error(
              `No exact match for: "${searchTerm}". Suggestions: ${JSON.stringify(suggestions)}`
            );
          } else {
            throw new Error(`No entry found matching: "${searchTerm}"`);
          }
        } else {
          throw new Error(`zk search failed: ${result.stderr || "Unknown error"}`);
        }
      } catch (error) {
        if (error instanceof Error) throw error;
        throw new Error(`Failed to search: ${String(error)}`);
      }
    }

    if (!resolvedPath || !resolvedId) {
      throw new Error("Could not resolve entry");
    }

    const link =
      includeTitle && resolvedTitle
        ? generateMarkdownLink(resolvedId, resolvedTitle)
        : generateMarkdownLink(resolvedId);

    return {
      link,
      id: resolvedId,
      path: resolvedPath,
      title: resolvedTitle || resolvedId,
    };
  }

  // ========================================
  // Plan-specific
  // ========================================

  /**
   * Extract section headers from a plan entry (brain_plan_sections)
   */
  async getPlanSections(
    pathOrTitle: string
  ): Promise<{ path: string; title: string; type: string; sections: Section[] }> {
    let notePath = pathOrTitle;

    // Find by title if needed
    if (!existsSync(join(this.config.brainDir, notePath))) {
      const zkAvailable = isZkNotebookExists() && (await isZkAvailable());
      if (zkAvailable) {
        try {
          const result = await execZk([
            "list",
            "--format",
            "json",
            "--quiet",
            "--match",
            pathOrTitle,
            "--match-strategy",
            "exact",
          ]);
          if (result.exitCode === 0) {
            const notes = parseZkJsonOutput(result.stdout);
            const exactMatch = notes.find((n) => n.title === pathOrTitle);
            if (exactMatch) {
              notePath = exactMatch.path;
            } else if (notes.length > 0) {
              const suggestions = notes.slice(0, 5).map((n) => n.title);
              throw new Error(
                `No exact match for title: "${pathOrTitle}". Suggestions: ${suggestions.join(", ")}`
              );
            }
          }
        } catch (error) {
          if (error instanceof Error) throw error;
        }
      }
    }

    const fullPath = join(this.config.brainDir, notePath);
    if (!existsSync(fullPath)) {
      throw new Error(`Entry not found: ${notePath}`);
    }

    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);
    const lines = body.split("\n");

    const sections: Section[] = [];
    let currentSection: Section | null = null;

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      const headerMatch = line.match(/^(#{1,6})\s+(.+)$/);

      if (headerMatch) {
        if (currentSection) {
          currentSection.endLine = i - 1;
          sections.push(currentSection);
        }

        currentSection = {
          title: headerMatch[2].trim(),
          level: headerMatch[1].length,
          startLine: i + 1,
          endLine: lines.length,
        };
      }
    }

    if (currentSection) {
      currentSection.endLine = lines.length;
      sections.push(currentSection);
    }

    return {
      path: notePath,
      title: (frontmatter.title as string) || notePath,
      type: (frontmatter.type as string) || "plan",
      sections,
    };
  }

  /**
   * Retrieve a specific section's content from a brain plan (brain_section)
   */
  async getSection(
    planId: string,
    sectionTitle: string,
    includeSubsections = true
  ): Promise<{
    planId: string;
    planTitle: string;
    sectionTitle: string;
    content: string;
    lineRange: { start: number; end: number };
  }> {
    const fullPath = join(this.config.brainDir, planId);
    if (!existsSync(fullPath)) {
      throw new Error(`Plan not found: ${planId}`);
    }

    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);
    const lines = body.split("\n");

    // Find section start
    let sectionStart = -1;
    let sectionLevel = 0;
    let actualTitle = "";
    const sectionTitleLower = sectionTitle.toLowerCase().trim();

    for (let i = 0; i < lines.length; i++) {
      const match = lines[i].match(/^(#{1,6})\s+(.+)$/);
      if (match) {
        const headerTitle = match[2].trim();
        const headerTitleLower = headerTitle.toLowerCase();
        if (
          headerTitleLower === sectionTitleLower ||
          headerTitleLower.includes(sectionTitleLower) ||
          sectionTitleLower.includes(headerTitleLower)
        ) {
          sectionStart = i;
          sectionLevel = match[1].length;
          actualTitle = headerTitle;
          break;
        }
      }
    }

    if (sectionStart === -1) {
      const availableSections: string[] = [];
      for (const line of lines) {
        const match = line.match(/^(#{2,4})\s+(.+)$/);
        if (match) {
          availableSections.push(match[2].trim());
        }
      }
      throw new Error(
        `Section "${sectionTitle}" not found in plan. Available sections: ${availableSections.slice(0, 15).join(", ")}`
      );
    }

    // Find section end
    let sectionEnd = lines.length;

    for (let i = sectionStart + 1; i < lines.length; i++) {
      const match = lines[i].match(/^(#{1,6})\s+/);
      if (match) {
        const level = match[1].length;
        if (includeSubsections) {
          if (level <= sectionLevel) {
            sectionEnd = i;
            break;
          }
        } else {
          sectionEnd = i;
          break;
        }
      }
    }

    const sectionContent = lines.slice(sectionStart, sectionEnd).join("\n").trim();

    // Record access
    recordAccess(planId);

    return {
      planId,
      planTitle: (frontmatter.title as string) || planId,
      sectionTitle: actualTitle,
      content: sectionContent,
      lineRange: { start: sectionStart + 1, end: sectionEnd },
    };
  }
}

// =============================================================================
// Default Export - Singleton Instance
// =============================================================================

let brainServiceInstance: BrainService | null = null;

export function getBrainService(): BrainService {
  if (!brainServiceInstance) {
    brainServiceInstance = new BrainService();
  }
  return brainServiceInstance;
}

export default BrainService;

// =============================================================================
// Dependency Rewriting for moveEntry()
// =============================================================================

export interface ComputeNewDepRefParams {
  dependentProjectId: string;
  movedTaskId: string;
  sourceProjectId: string;
  targetProjectId: string;
}

/**
 * Compute the new dependency reference after a task moves between projects.
 * Pure function — no I/O.
 *
 * Rules:
 * - If dependent is in the target project → bare ID (now local)
 * - Otherwise → "targetProjectId:movedTaskId" (cross-project)
 */
export function computeNewDepRef(params: ComputeNewDepRefParams): string {
  const { dependentProjectId, movedTaskId, targetProjectId } = params;

  if (dependentProjectId === targetProjectId) {
    // The dependent is now in the same project as the moved task → bare ref
    return movedTaskId;
  }

  // The dependent is in a different project → cross-project ref
  return `${targetProjectId}:${movedTaskId}`;
}

export interface RewriteResult {
  taskId: string;
  project: string;
  oldRef: string;
  newRef: string;
}

export interface RewriteDependentFilesParams {
  brainDir: string;
  dependents: DependentInfo[];
  movedTaskId: string;
  sourceProjectId: string;
  targetProjectId: string;
}

/**
 * Rewrite depends_on references in dependent task files after a move.
 * Reads each dependent file, replaces the old ref, writes back.
 */
export function rewriteDependentFiles(params: RewriteDependentFilesParams): RewriteResult[] {
  const { brainDir, dependents, movedTaskId, sourceProjectId, targetProjectId } = params;
  const results: RewriteResult[] = [];

  for (const dep of dependents) {
    const fullPath = join(brainDir, dep.taskPath);

    // Gracefully skip if file doesn't exist (e.g., stale index)
    if (!existsSync(fullPath)) {
      continue;
    }

    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);

    const depsArray = (frontmatter.depends_on as string[]) || [];
    const depIndex = depsArray.indexOf(dep.depRef);
    if (depIndex === -1) {
      // The ref from findDependents wasn't found in the actual file — skip
      continue;
    }

    const newRef = computeNewDepRef({
      dependentProjectId: dep.projectId,
      movedTaskId,
      sourceProjectId,
      targetProjectId,
    });

    // Replace the old ref with the new one
    depsArray[depIndex] = newRef;
    frontmatter.depends_on = depsArray;

    // Serialize and write back
    const newFrontmatterStr = serializeFrontmatter(frontmatter);
    const newContent = `---\n${newFrontmatterStr}---\n\n${body}\n`;
    writeFileSync(fullPath, newContent, "utf-8");

    results.push({
      taskId: dep.taskId,
      project: dep.projectId,
      oldRef: dep.depRef,
      newRef,
    });
  }

  return results;
}
