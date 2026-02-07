/**
 * Brain API - Core Brain Service
 *
 * Main business logic layer ported from the OpenCode brain plugin.
 * Implements all 17 brain operations.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync, unlinkSync } from "fs";
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
} from "./db";
import {
  execZk,
  execZkNew,
  extractIdFromPath,
  generateMarkdownLink,
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
  ZkNote,
} from "./types";
import { ENTRY_STATUSES } from "./types";

// =============================================================================
// Constants
// =============================================================================

const MS_PER_DAY = 24 * 60 * 60 * 1000;

// =============================================================================
// Section Type
// =============================================================================

export interface Section {
  title: string;
  level: number;
  startLine: number;
  endLine: number;
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
    if (request.worktree) request.worktree = sanitizeSimpleValue(request.worktree);
    if (request.git_remote) request.git_remote = sanitizeSimpleValue(request.git_remote);
    if (request.git_branch) request.git_branch = sanitizeSimpleValue(request.git_branch);
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

    const entryType = request.type;
    // Tasks default to 'draft' status (user reviews before promoting to 'pending')
    // All other entry types default to 'active'
    const entryStatus = request.status || (entryType === "task" ? "draft" : "active");
    const isGlobal = request.global ?? false;

    // Determine effective project ID
    const effectiveProjectId =
      request.project || (isGlobal ? "global" : this.projectId) || "global";

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
    let zkAvailable = isZkNotebookExists() && (await isZkAvailable());

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
      if (request.worktree) {
        zkArgs.push("--extra", `worktree=${request.worktree}`);
      }
      if (request.git_remote) {
        zkArgs.push("--extra", `git_remote=${request.git_remote}`);
      }
      if (request.git_branch) {
        zkArgs.push("--extra", `git_branch=${request.git_branch}`);
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
      // Fallback: manual file creation
      const filename = `${Date.now()}-${slugify(sanitizedTitle)}.md`;
      relativePath = `${dir}/${filename}`;
      noteId = filename.replace(".md", "");
      const fullPath = join(this.config.brainDir, relativePath);

      const frontmatter = generateFrontmatter({
        title: sanitizedTitle,
        type: entryType,
        tags: request.tags,
        status: entryStatus,
        projectId: isGlobal ? undefined : this.projectId,
        priority: request.priority,
        // Execution context for tasks
        workdir: request.workdir,
        worktree: request.worktree,
        git_remote: request.git_remote,
        git_branch: request.git_branch,
        // User intent for validation
        user_original_request: request.user_original_request,
      });

      const fileContent = `---\n${frontmatter}---\n\n${finalContent}\n`;
      writeFileSync(fullPath, fileContent, "utf-8");
    }

    // Initialize in database
    initEntry(relativePath, effectiveProjectId);

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
      worktree: frontmatter.worktree as string | undefined,
      git_remote: frontmatter.git_remote as string | undefined,
      git_branch: frontmatter.git_branch as string | undefined,
      // User intent for validation
      user_original_request: frontmatter.user_original_request as string | undefined,
    };
  }

  /**
   * Update an existing entry (brain_update)
   */
  async update(path: string, request: UpdateEntryRequest): Promise<BrainEntry> {
    const fullPath = join(this.config.brainDir, path);
    if (!existsSync(fullPath)) {
      throw new Error(`Entry not found: ${path}`);
    }

    if (!request.status && !request.title && !request.content && !request.append && !request.note && !request.depends_on && !request.feature_id && !request.feature_priority && !request.feature_depends_on) {
      throw new Error(
        "No updates specified. Provide at least one of: status, title, content, append, note, depends_on, feature_id, feature_priority, feature_depends_on"
      );
    }

    // Read existing content
    const content = readFileSync(fullPath, "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);

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

    // Delete file
    unlinkSync(fullPath);

    // Remove from database
    deleteEntryMeta(path);
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

    const fetchLimit = request.status ? limit * 5 : limit;
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

    const needsCodeFiltering = request.status || request.filename;
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
