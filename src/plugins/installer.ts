/**
 * Brain Plugin Installer
 *
 * Handles installation of brain plugins to various AI coding assistant targets.
 *
 * NOTE: Plugin source files are embedded into the compiled binary using
 * `import ... with { type: "file" }`. This allows `brain install` to work
 * from both `bun run` and compiled standalone executables.
 */

import { existsSync, mkdirSync, readFileSync, writeFileSync, renameSync } from "fs";
import { join, dirname } from "path";
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

    // Replace template variables (only for TypeScript files)
    if (targetPath.endsWith(".ts")) {
      content = content.replace(/\{\{GENERATED_DATE\}\}/g, new Date().toISOString());
      if (apiUrl) {
        content = content.replace(
          /const BRAIN_API_URL = process\.env\.BRAIN_API_URL \|\| "http:\/\/localhost:3333"/,
          `const BRAIN_API_URL = process.env.BRAIN_API_URL || "${apiUrl}"`
        );
      }
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
