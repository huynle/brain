/**
 * Brain Plugin Installer
 *
 * Handles installation of brain plugins to various AI coding assistant targets.
 */

import { existsSync, mkdirSync, readFileSync, writeFileSync, copyFileSync, renameSync } from "fs";
import { join, dirname } from "path";
import { homedir } from "os";
import type { InstallTarget, InstallOptions, InstallResult } from "./shared/types";

// ============================================================================
// Target Configuration
// ============================================================================

interface TargetConfig {
  name: string;
  description: string;
  configDir: string | ((home: string) => string);
  pluginDir: string | ((home: string) => string);
  pluginFile: string;
  sourceFile: string;
  configFile?: string;
  configUpdater?: (configPath: string, pluginPath: string) => void;
  postInstall?: (pluginPath: string) => string[];
}

const TARGETS: Record<InstallTarget, TargetConfig> = {
  opencode: {
    name: "OpenCode",
    description: "OpenCode AI coding assistant",
    configDir: (home) => join(home, "dot/config/opencode"),
    pluginDir: (home) => join(home, "dot/config/opencode/plugin"),
    pluginFile: "brain.ts",
    sourceFile: "targets/opencode/brain.ts",
    postInstall: () => [
      "Plugin installed. OpenCode will automatically load it on next start.",
      "Make sure the Brain API server is running: brain start",
    ],
  },
  "claude-code": {
    name: "Claude Code",
    description: "Anthropic Claude Code (MCP server)",
    configDir: (home) => join(home, ".config/claude-code"),
    pluginDir: (home) => join(home, ".config/claude-code/mcp-servers"),
    pluginFile: "brain-mcp.ts",
    sourceFile: "targets/claude-code/brain-mcp.ts",
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
    sourceFile: "targets/cursor/brain-extension.ts",
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
    sourceFile: "targets/antigravity/brain.ts",
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

export async function installPlugin(options: InstallOptions): Promise<InstallResult> {
  const { target, force = false, dryRun = false, apiUrl } = options;
  const config = getTargetConfig(target);
  const { pluginDir, pluginPath, configPath } = resolveTargetPaths(target);

  // Get source file path (relative to this module)
  const sourceDir = dirname(new URL(import.meta.url).pathname);
  const sourcePath = join(sourceDir, config.sourceFile);

  // Check if source exists
  if (!existsSync(sourcePath)) {
    return {
      success: false,
      target,
      installedPath: pluginPath,
      message: `Source plugin not found: ${sourcePath}. This target may not be fully implemented yet.`,
    };
  }

  // Check if target directory exists
  if (!existsSync(pluginDir)) {
    if (dryRun) {
      return {
        success: true,
        target,
        installedPath: pluginPath,
        message: `[DRY RUN] Would create directory: ${pluginDir}`,
      };
    }
    mkdirSync(pluginDir, { recursive: true });
  }

  // Check if plugin already exists
  let backupPath: string | undefined;
  if (existsSync(pluginPath)) {
    if (!force) {
      return {
        success: false,
        target,
        installedPath: pluginPath,
        message: `Plugin already exists at ${pluginPath}. Use --force to overwrite.`,
      };
    }
    // Create backup
    backupPath = `${pluginPath}.backup-${Date.now()}`;
    if (!dryRun) {
      renameSync(pluginPath, backupPath);
    }
  }

  if (dryRun) {
    const messages = [
      `[DRY RUN] Would install plugin to: ${pluginPath}`,
      backupPath ? `[DRY RUN] Would backup existing to: ${backupPath}` : null,
      configPath ? `[DRY RUN] Would update config: ${configPath}` : null,
    ].filter(Boolean);
    return {
      success: true,
      target,
      installedPath: pluginPath,
      message: messages.join("\n"),
      backupPath,
    };
  }

  // Read source and process template variables
  let content = readFileSync(sourcePath, "utf-8");

  // Replace template variables
  content = content.replace(/\{\{GENERATED_DATE\}\}/g, new Date().toISOString());
  if (apiUrl) {
    content = content.replace(
      /const BRAIN_API_URL = process\.env\.BRAIN_API_URL \|\| "http:\/\/localhost:3333"/,
      `const BRAIN_API_URL = process.env.BRAIN_API_URL || "${apiUrl}"`
    );
  }

  // Write plugin file
  writeFileSync(pluginPath, content);

  // Update config if needed
  if (configPath && config.configUpdater) {
    config.configUpdater(configPath, pluginPath);
  }

  // Build result message
  const postInstallMessages = config.postInstall ? config.postInstall(pluginPath) : [];
  const messages = [
    `Successfully installed ${config.name} plugin to:`,
    `  ${pluginPath}`,
    backupPath ? `\nPrevious version backed up to:\n  ${backupPath}` : "",
    postInstallMessages.length > 0 ? `\n${postInstallMessages.join("\n")}` : "",
  ].filter(Boolean);

  return {
    success: true,
    target,
    installedPath: pluginPath,
    message: messages.join("\n"),
    backupPath,
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
