/**
 * Brain Plugin Installer
 *
 * Handles installation of brain plugins to various AI coding assistant targets.
 *
 * NOTE: Plugin source files are embedded into the compiled binary using
 * `import ... with { type: "file" }`. This allows `brain install` to work
 * from both `bun run` and compiled standalone executables.
 */

import { existsSync, mkdirSync, readFileSync, writeFileSync, renameSync, readdirSync, unlinkSync } from "fs";
import { join, dirname, basename } from "path";
import { homedir } from "os";
import type {
  InstallTarget,
  InstallOptions,
  InstallResult,
  AdditionalFile,
  InstalledFile,
} from "./shared/types";

// ============================================================================
// Embedded Plugin Sources
// ============================================================================
// These imports embed the plugin files into the compiled binary.
// When running via `bun run`, they resolve to the actual file paths.
// When running as a compiled binary, they point to embedded data.
// The `with { type: "file" }` syntax is Bun-specific and not recognized by tsc.

// OpenCode main plugins
// @ts-ignore - Bun import attribute syntax
import opencodePluginPath from "./targets/opencode/brain.ts" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import opencodePlanningPluginPath from "./targets/opencode/brain-planning.ts" with { type: "file" };

// OpenCode skills
// @ts-ignore - Bun import attribute syntax
import skillDoWorkPath from "./targets/opencode/skill/do-work/SKILL.md" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import skillBrainPlanningPath from "./targets/opencode/skill/brain-planning/SKILL.md" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import skillUsingBrainPath from "./targets/opencode/skill/using-brain/SKILL.md" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import skillProjectPlanningPath from "./targets/opencode/skill/project-planning/SKILL.md" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import skillWritingPlansPath from "./targets/opencode/skill/writing-plans/SKILL.md" with { type: "file" };

// OpenCode commands
// @ts-ignore - Bun import attribute syntax
import commandDoPath from "./targets/opencode/command/do.md" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import commandWorkPath from "./targets/opencode/command/work.md" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import commandPlanToTasksPath from "./targets/opencode/command/plan-to-tasks.md" with { type: "file" };

// Claude Code MCP server
// @ts-ignore - Bun import attribute syntax
import claudeCodePluginPath from "./targets/claude-code/brain-mcp.ts" with { type: "file" };
// Note: cursor and antigravity plugins don't exist yet, we'll handle them gracefully

// Map of target to embedded plugin path
const EMBEDDED_PLUGINS: Partial<Record<InstallTarget, string>> = {
  opencode: opencodePluginPath,
  "claude-code": claudeCodePluginPath,
};

// Map of additional embedded files for each target
const EMBEDDED_ADDITIONAL_FILES: Partial<Record<InstallTarget, Record<string, string>>> = {
  opencode: {
    "plugin/brain-planning.ts": opencodePlanningPluginPath,
    "skill/do-work/SKILL.md": skillDoWorkPath,
    "skill/brain-planning/SKILL.md": skillBrainPlanningPath,
    "skill/using-brain/SKILL.md": skillUsingBrainPath,
    "skill/project-planning/SKILL.md": skillProjectPlanningPath,
    "skill/writing-plans/SKILL.md": skillWritingPlansPath,
    "command/do.md": commandDoPath,
    "command/work.md": commandWorkPath,
    "command/plan-to-tasks.md": commandPlanToTasksPath,
  },
};

// ============================================================================
// Auto-Generated Header
// ============================================================================

/**
 * Generate the auto-generated header comment for installed files.
 */
function generateHeader(fileType: "ts" | "md"): string {
  const date = new Date().toISOString();
  const lines = [
    "AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY",
    "",
    "This file was installed by: brain install opencode",
    "To update: brain install opencode --force",
    "To check status: brain doctor",
    "Source: https://github.com/huynle/brain-api",
    `Generated: ${date}`,
  ];

  if (fileType === "ts") {
    return [
      "// ============================================================================",
      ...lines.map((l) => (l ? `// ${l}` : "//")),
      "// ============================================================================",
      "",
    ].join("\n");
  } else {
    // Markdown
    return [
      "<!--",
      ...lines,
      "-->",
      "",
    ].join("\n");
  }
}

// ============================================================================
// Target Configuration
// ============================================================================

interface TargetConfig {
  name: string;
  description: string;
  configDir: string | ((home: string) => string);
  pluginDir: string | ((home: string) => string);
  pluginFile: string;
  configFile?: string;
  configUpdater?: (configPath: string, pluginPath: string) => void;
  postInstall?: (pluginPath: string) => string[];
  /** Additional files to install (skills, commands, etc.) */
  additionalFiles?: AdditionalFile[];
}

const TARGETS: Record<InstallTarget, TargetConfig> = {
  opencode: {
    name: "OpenCode",
    description: "OpenCode AI coding assistant (with skills + commands)",
    configDir: (home) => join(home, ".config/opencode"),
    pluginDir: (home) => join(home, ".config/opencode/plugin"),
    pluginFile: "brain.ts",
    postInstall: () => [
      "All brain components installed. OpenCode will automatically load them on next start.",
      "Make sure the Brain API server is running: brain start",
    ],
    additionalFiles: [
      // Additional plugin
      {
        sourcePath: "plugin/brain-planning.ts",
        targetDir: (home) => join(home, ".config/opencode/plugin"),
        targetFile: "brain-planning.ts",
        description: "Brain planning enforcement plugin",
        componentType: "plugin",
      },
      // Skills
      {
        sourcePath: "skill/do-work/SKILL.md",
        targetDir: (home) => join(home, ".config/opencode/skill/do-work"),
        targetFile: "SKILL.md",
        description: "do-work skill (task queue processing)",
        componentType: "skill",
      },
      {
        sourcePath: "skill/brain-planning/SKILL.md",
        targetDir: (home) => join(home, ".config/opencode/skill/brain-planning"),
        targetFile: "SKILL.md",
        description: "brain-planning skill (plan persistence)",
        componentType: "skill",
      },
      {
        sourcePath: "skill/using-brain/SKILL.md",
        targetDir: (home) => join(home, ".config/opencode/skill/using-brain"),
        targetFile: "SKILL.md",
        description: "using-brain skill (knowledge patterns)",
        componentType: "skill",
      },
      {
        sourcePath: "skill/project-planning/SKILL.md",
        targetDir: (home) => join(home, ".config/opencode/skill/project-planning"),
        targetFile: "SKILL.md",
        description: "project-planning skill (feature planning)",
        componentType: "skill",
      },
      {
        sourcePath: "skill/writing-plans/SKILL.md",
        targetDir: (home) => join(home, ".config/opencode/skill/writing-plans"),
        targetFile: "SKILL.md",
        description: "writing-plans skill (implementation plans)",
        componentType: "skill",
      },
      // Commands
      {
        sourcePath: "command/do.md",
        targetDir: (home) => join(home, ".config/opencode/command"),
        targetFile: "do.md",
        description: "/do command (add tasks to queue)",
        componentType: "command",
      },
      {
        sourcePath: "command/work.md",
        targetDir: (home) => join(home, ".config/opencode/command"),
        targetFile: "work.md",
        description: "/work command (process task queue)",
        componentType: "command",
      },
      {
        sourcePath: "command/plan-to-tasks.md",
        targetDir: (home) => join(home, ".config/opencode/command"),
        targetFile: "plan-to-tasks.md",
        description: "/plan-to-tasks command (convert plan to tasks)",
        componentType: "command",
      },
    ],
  },
  "claude-code": {
    name: "Claude Code",
    description: "Anthropic Claude Code (MCP server)",
    configDir: (home) => join(home, ".config/claude-code"),
    pluginDir: (home) => join(home, ".config/claude-code/mcp-servers"),
    pluginFile: "brain-mcp.ts",
    configFile: "mcp-config.json",
    configUpdater: (configPath, pluginPath) => {
      let config: Record<string, unknown> = {};
      if (existsSync(configPath)) {
        config = JSON.parse(readFileSync(configPath, "utf-8"));
      }
      const servers = (config.mcpServers as Record<string, unknown>) || {};
      servers["brain"] = {
        command: "bun",
        args: ["run", pluginPath],
        env: {
          BRAIN_API_URL: "http://localhost:3333",
        },
      };
      config.mcpServers = servers;
      writeFileSync(configPath, JSON.stringify(config, null, 2));
    },
    postInstall: () => [
      "MCP server installed. Restart Claude Code to load the brain tools.",
      "Make sure the Brain API server is running: brain start",
    ],
  },
  cursor: {
    name: "Cursor IDE",
    description: "Cursor AI-powered IDE",
    configDir: (home) => join(home, ".cursor"),
    pluginDir: (home) => join(home, ".cursor/extensions/brain"),
    pluginFile: "brain-extension.ts",
    postInstall: () => [
      "Cursor extension scaffolding installed.",
      "Note: Full Cursor extension support is coming soon.",
      "For now, use the Brain API directly via HTTP.",
    ],
  },
  antigravity: {
    name: "Antigravity",
    description: "Antigravity AI coding assistant",
    configDir: (home) => join(home, ".config/antigravity"),
    pluginDir: (home) => join(home, ".config/antigravity/plugins"),
    pluginFile: "brain.ts",
    postInstall: () => [
      "Plugin installed for Antigravity.",
      "Make sure the Brain API server is running: brain start",
    ],
  },
};

// ============================================================================
// Installer
// ============================================================================

export function getTargetConfig(target: InstallTarget): TargetConfig {
  const config = TARGETS[target];
  if (!config) {
    throw new Error(`Unknown target: ${target}. Available: ${Object.keys(TARGETS).join(", ")}`);
  }
  return config;
}

export function getAvailableTargets(): Array<{ id: InstallTarget; name: string; description: string }> {
  return Object.entries(TARGETS).map(([id, config]) => ({
    id: id as InstallTarget,
    name: config.name,
    description: config.description,
  }));
}

export function resolveTargetPaths(target: InstallTarget): {
  configDir: string;
  pluginDir: string;
  pluginPath: string;
  configPath?: string;
} {
  const home = homedir();
  const config = getTargetConfig(target);

  const configDir = typeof config.configDir === "function" ? config.configDir(home) : config.configDir;
  const pluginDir = typeof config.pluginDir === "function" ? config.pluginDir(home) : config.pluginDir;
  const pluginPath = join(pluginDir, config.pluginFile);
  const configPath = config.configFile ? join(configDir, config.configFile) : undefined;

  return { configDir, pluginDir, pluginPath, configPath };
}

export function checkTargetExists(target: InstallTarget): boolean {
  const { configDir } = resolveTargetPaths(target);
  return existsSync(configDir);
}

/**
 * Install a single file from embedded source.
 * Helper function used by installPlugin for both main plugin and additional files.
 */
async function installSingleFile(
  embeddedPath: string,
  targetPath: string,
  options: { force: boolean; dryRun: boolean; apiUrl?: string }
): Promise<{ success: boolean; backupPath?: string; error?: string }> {
  const { force, dryRun, apiUrl } = options;
  const targetDir = dirname(targetPath);

  // Ensure target directory exists
  if (!existsSync(targetDir)) {
    if (!dryRun) {
      mkdirSync(targetDir, { recursive: true });
    }
  }

  // Check if file already exists
  let backupPath: string | undefined;
  if (existsSync(targetPath)) {
    if (!force) {
      return { success: false, error: `File exists: ${targetPath}` };
    }
    // Create backup
    backupPath = `${targetPath}.backup-${Date.now()}`;
    if (!dryRun) {
      renameSync(targetPath, backupPath);
    }
  }

  if (!dryRun) {
    // Read source from embedded file (works in both bun run and compiled binary)
    let content = await Bun.file(embeddedPath).text();

    // Determine file type for header
    const fileType = targetPath.endsWith(".ts") ? "ts" : "md";

    // Add auto-generated header at the top
    const header = generateHeader(fileType);

    // For TypeScript files, insert header after any existing // @ts-nocheck comment
    if (fileType === "ts") {
      // Check if file starts with @ts-nocheck
      if (content.startsWith("// @ts-nocheck")) {
        const firstNewline = content.indexOf("\n");
        content = content.slice(0, firstNewline + 1) + header + content.slice(firstNewline + 1);
      } else {
        content = header + content;
      }

      // Replace template variables
      content = content.replace(/\{\{GENERATED_DATE\}\}/g, new Date().toISOString());
      if (apiUrl) {
        content = content.replace(
          /const BRAIN_API_URL = process\.env\.BRAIN_API_URL \|\| "http:\/\/localhost:3333"/,
          `const BRAIN_API_URL = process.env.BRAIN_API_URL || "${apiUrl}"`
        );
      }
    } else {
      // For markdown files, add header at the top
      content = header + content;
    }

    // Write file
    writeFileSync(targetPath, content);
  }

  return { success: true, backupPath };
}

export async function installPlugin(options: InstallOptions): Promise<InstallResult> {
  const { target, force = false, dryRun = false, apiUrl } = options;
  const config = getTargetConfig(target);
  const home = homedir();
  const { pluginDir, pluginPath, configPath } = resolveTargetPaths(target);

  // Get embedded plugin source (works in both bun run and compiled binary)
  const embeddedPath = EMBEDDED_PLUGINS[target];
  if (!embeddedPath) {
    return {
      success: false,
      target,
      installedPath: pluginPath,
      message: `Plugin for ${target} is not yet implemented.`,
    };
  }

  // Track all installed files
  const installedFiles: InstalledFile[] = [];
  const dryRunMessages: string[] = [];

  // -------------------------------------------------------------------------
  // Install main plugin
  // -------------------------------------------------------------------------
  if (dryRun) {
    dryRunMessages.push(`[DRY RUN] Would install main plugin to: ${pluginPath}`);
  }

  const mainResult = await installSingleFile(embeddedPath, pluginPath, { force, dryRun, apiUrl });
  if (!mainResult.success) {
    return {
      success: false,
      target,
      installedPath: pluginPath,
      message: `${mainResult.error}. Use --force to overwrite.`,
    };
  }

  installedFiles.push({
    sourcePath: config.pluginFile,
    targetPath: pluginPath,
    componentType: "plugin",
    description: `${config.name} main plugin`,
    backupPath: mainResult.backupPath,
  });

  // -------------------------------------------------------------------------
  // Install additional files (skills, commands, etc.)
  // -------------------------------------------------------------------------
  const additionalFiles = config.additionalFiles || [];
  const embeddedAdditional = EMBEDDED_ADDITIONAL_FILES[target] || {};

  for (const file of additionalFiles) {
    const embeddedFilePath = embeddedAdditional[file.sourcePath];
    if (!embeddedFilePath) {
      // Skip if embedded file not found (shouldn't happen if config is correct)
      console.warn(`Warning: Embedded file not found for ${file.sourcePath}`);
      continue;
    }

    const targetDir = file.targetDir(home);
    const targetFilePath = join(targetDir, file.targetFile);

    if (dryRun) {
      dryRunMessages.push(`[DRY RUN] Would install ${file.description} to: ${targetFilePath}`);
    }

    const result = await installSingleFile(embeddedFilePath, targetFilePath, { force, dryRun, apiUrl });
    if (!result.success) {
      // For additional files, we warn but continue
      console.warn(`Warning: Could not install ${file.description}: ${result.error}`);
      continue;
    }

    installedFiles.push({
      sourcePath: file.sourcePath,
      targetPath: targetFilePath,
      componentType: file.componentType,
      description: file.description,
      backupPath: result.backupPath,
    });
  }

  // -------------------------------------------------------------------------
  // Update config if needed
  // -------------------------------------------------------------------------
  if (configPath && config.configUpdater) {
    if (dryRun) {
      dryRunMessages.push(`[DRY RUN] Would update config: ${configPath}`);
    } else {
      config.configUpdater(configPath, pluginPath);
    }
  }

  // -------------------------------------------------------------------------
  // Build result message
  // -------------------------------------------------------------------------
  if (dryRun) {
    return {
      success: true,
      target,
      installedPath: pluginPath,
      message: dryRunMessages.join("\n"),
      installedFiles,
    };
  }

  // Group installed files by component type
  const plugins = installedFiles.filter((f) => f.componentType === "plugin");
  const skills = installedFiles.filter((f) => f.componentType === "skill");
  const commands = installedFiles.filter((f) => f.componentType === "command");

  const postInstallMessages = config.postInstall ? config.postInstall(pluginPath) : [];

  const messages: string[] = [
    `Successfully installed ${config.name} components:`,
    "",
  ];

  if (plugins.length > 0) {
    messages.push(`Plugins (${plugins.length}):`);
    for (const f of plugins) {
      messages.push(`  - ${f.targetPath}`);
    }
    messages.push("");
  }

  if (skills.length > 0) {
    messages.push(`Skills (${skills.length}):`);
    for (const f of skills) {
      messages.push(`  - ${f.description}`);
    }
    messages.push("");
  }

  if (commands.length > 0) {
    messages.push(`Commands (${commands.length}):`);
    for (const f of commands) {
      messages.push(`  - ${f.description}`);
    }
    messages.push("");
  }

  // Show any backups
  const backups = installedFiles.filter((f) => f.backupPath);
  if (backups.length > 0) {
    messages.push(`Backups created: ${backups.length}`);
  }

  if (postInstallMessages.length > 0) {
    messages.push("");
    messages.push(...postInstallMessages);
  }

  return {
    success: true,
    target,
    installedPath: pluginPath,
    message: messages.join("\n"),
    installedFiles,
  };
}

export async function uninstallPlugin(target: InstallTarget, dryRun = false): Promise<InstallResult> {
  const config = getTargetConfig(target);
  const { pluginPath } = resolveTargetPaths(target);

  if (!existsSync(pluginPath)) {
    return {
      success: false,
      target,
      installedPath: pluginPath,
      message: `Plugin not found at ${pluginPath}. Nothing to uninstall.`,
    };
  }

  if (dryRun) {
    return {
      success: true,
      target,
      installedPath: pluginPath,
      message: `[DRY RUN] Would remove plugin: ${pluginPath}`,
    };
  }

  // Create backup before removing
  const backupPath = `${pluginPath}.uninstalled-${Date.now()}`;
  renameSync(pluginPath, backupPath);

  return {
    success: true,
    target,
    installedPath: pluginPath,
    message: `Uninstalled ${config.name} plugin.\nBackup saved to: ${backupPath}`,
    backupPath,
  };
}

export function getPluginStatus(target: InstallTarget): {
  installed: boolean;
  path: string;
  targetExists: boolean;
} {
  const { pluginPath, configDir } = resolveTargetPaths(target);
  return {
    installed: existsSync(pluginPath),
    path: pluginPath,
    targetExists: existsSync(configDir),
  };
}

// ============================================================================
// Backup Management
// ============================================================================

export interface BackupInfo {
  path: string;
  originalFile: string;
  timestamp: number;
  type: "backup" | "uninstalled";
  componentType: "plugin" | "skill" | "command" | "agent";
}

/**
 * Find all backup files for a target.
 */
export function findBackups(target: InstallTarget): BackupInfo[] {
  const config = getTargetConfig(target);
  const home = homedir();
  const backupsMap = new Map<string, BackupInfo>(); // Use path as key to deduplicate

  // Patterns to match: .backup-{timestamp} and .uninstalled-{timestamp}
  const backupPattern = /^(.+)\.(backup|uninstalled)-(\d+)$/;

  // Check plugin directory
  const pluginDir = typeof config.pluginDir === "function" ? config.pluginDir(home) : config.pluginDir;
  if (existsSync(pluginDir)) {
    const files = readdirSync(pluginDir);
    for (const file of files) {
      const match = file.match(backupPattern);
      if (match) {
        const fullPath = join(pluginDir, file);
        backupsMap.set(fullPath, {
          path: fullPath,
          originalFile: match[1],
          timestamp: parseInt(match[3], 10),
          type: match[2] as "backup" | "uninstalled",
          componentType: "plugin",
        });
      }
    }
  }

  // Check additional files (skills, commands) if target has them
  const additionalFiles = config.additionalFiles || [];
  for (const file of additionalFiles) {
    const targetDir = file.targetDir(home);
    if (existsSync(targetDir)) {
      const files = readdirSync(targetDir);
      for (const f of files) {
        const match = f.match(backupPattern);
        if (match) {
          const fullPath = join(targetDir, f);
          backupsMap.set(fullPath, {
            path: fullPath,
            originalFile: match[1],
            timestamp: parseInt(match[3], 10),
            type: match[2] as "backup" | "uninstalled",
            componentType: file.componentType,
          });
        }
      }
    }
  }

  // Convert to array and sort by timestamp descending (newest first)
  const backups = Array.from(backupsMap.values());
  backups.sort((a, b) => b.timestamp - a.timestamp);

  return backups;
}

/**
 * Restore the latest backup for a target.
 */
export async function restoreBackups(
  target: InstallTarget,
  options: { dryRun?: boolean } = {}
): Promise<{ success: boolean; message: string; restored: string[] }> {
  const { dryRun = false } = options;
  const backups = findBackups(target);

  if (backups.length === 0) {
    return {
      success: false,
      message: "No backups found to restore.",
      restored: [],
    };
  }

  // Group backups by target file path, keeping only the latest for each
  const latestByFile = new Map<string, BackupInfo>();
  for (const backup of backups) {
    // Use the full target path (directory + original filename) as key
    const targetPath = join(dirname(backup.path), backup.originalFile);
    if (!latestByFile.has(targetPath)) {
      latestByFile.set(targetPath, backup);
    }
  }

  const restored: string[] = [];
  const messages: string[] = [];

  for (const [targetPath, backup] of latestByFile) {
    const date = new Date(backup.timestamp).toISOString();

    if (dryRun) {
      messages.push(`[DRY RUN] Would restore: ${backup.originalFile} (from ${date})`);
      restored.push(targetPath);
      continue;
    }

    try {
      // If current file exists, remove it first
      if (existsSync(targetPath)) {
        unlinkSync(targetPath);
      }

      // Rename backup to original name
      renameSync(backup.path, targetPath);
      messages.push(`Restored: ${backup.originalFile} (from ${date})`);
      restored.push(targetPath);
    } catch (err) {
      messages.push(`Failed to restore ${backup.originalFile}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  return {
    success: restored.length > 0,
    message: messages.join("\n"),
    restored,
  };
}

/**
 * Clean up backup files for a target.
 */
export async function cleanBackups(
  target: InstallTarget,
  options: { dryRun?: boolean; keepLatest?: boolean } = {}
): Promise<{ success: boolean; message: string; deleted: string[]; kept: string[] }> {
  const { dryRun = false, keepLatest = false } = options;
  const backups = findBackups(target);

  if (backups.length === 0) {
    return {
      success: true,
      message: "No backups found to clean.",
      deleted: [],
      kept: [],
    };
  }

  const deleted: string[] = [];
  const kept: string[] = [];
  const messages: string[] = [];

  if (keepLatest) {
    // Group by target file path, keep only the latest
    const latestByFile = new Map<string, BackupInfo>();
    for (const backup of backups) {
      const targetPath = join(dirname(backup.path), backup.originalFile);
      if (!latestByFile.has(targetPath)) {
        latestByFile.set(targetPath, backup);
      }
    }

    for (const backup of backups) {
      const targetPath = join(dirname(backup.path), backup.originalFile);
      const latest = latestByFile.get(targetPath);

      if (latest && backup.path === latest.path) {
        kept.push(backup.path);
        continue;
      }

      if (dryRun) {
        messages.push(`[DRY RUN] Would delete: ${basename(backup.path)}`);
        deleted.push(backup.path);
      } else {
        try {
          unlinkSync(backup.path);
          deleted.push(backup.path);
        } catch (err) {
          messages.push(`Failed to delete ${basename(backup.path)}: ${err instanceof Error ? err.message : String(err)}`);
        }
      }
    }

    if (!dryRun) {
      messages.push(`Deleted ${deleted.length} backup(s), kept ${kept.length} latest backup(s).`);
    }
  } else {
    // Delete all backups
    for (const backup of backups) {
      if (dryRun) {
        messages.push(`[DRY RUN] Would delete: ${basename(backup.path)}`);
        deleted.push(backup.path);
      } else {
        try {
          unlinkSync(backup.path);
          deleted.push(backup.path);
        } catch (err) {
          messages.push(`Failed to delete ${basename(backup.path)}: ${err instanceof Error ? err.message : String(err)}`);
        }
      }
    }

    if (!dryRun) {
      messages.push(`Deleted ${deleted.length} backup(s).`);
    }
  }

  return {
    success: true,
    message: messages.join("\n"),
    deleted,
    kept,
  };
}

/**
 * List all backups for a target with human-readable info.
 */
export function listBackups(target: InstallTarget): string {
  const backups = findBackups(target);

  if (backups.length === 0) {
    return "No backups found.";
  }

  const lines: string[] = [`Found ${backups.length} backup(s):\n`];

  for (const backup of backups) {
    const date = new Date(backup.timestamp);
    const relativeTime = getRelativeTime(date);
    const typeLabel = backup.type === "uninstalled" ? " (uninstalled)" : "";
    lines.push(`  [${backup.componentType}] ${backup.originalFile}${typeLabel}`);
    lines.push(`    ${date.toLocaleString()} (${relativeTime})`);
    lines.push(`    ${backup.path}`);
    lines.push("");
  }

  return lines.join("\n");
}

/**
 * Get relative time string (e.g., "2 hours ago")
 */
function getRelativeTime(date: Date): string {
  const now = Date.now();
  const diff = now - date.getTime();

  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) return `${days} day${days > 1 ? "s" : ""} ago`;
  if (hours > 0) return `${hours} hour${hours > 1 ? "s" : ""} ago`;
  if (minutes > 0) return `${minutes} minute${minutes > 1 ? "s" : ""} ago`;
  return "just now";
}
