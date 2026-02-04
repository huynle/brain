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
import { join } from "path";
import { homedir } from "os";
import type { InstallTarget, InstallOptions, InstallResult } from "./shared/types";

// ============================================================================
// Embedded Plugin Sources
// ============================================================================
// These imports embed the plugin files into the compiled binary.
// When running via `bun run`, they resolve to the actual file paths.
// When running as a compiled binary, they point to embedded data.
// The `with { type: "file" }` syntax is Bun-specific and not recognized by tsc.

// @ts-ignore - Bun import attribute syntax
import opencodePluginPath from "./targets/opencode/brain.ts" with { type: "file" };
// @ts-ignore - Bun import attribute syntax
import claudeCodePluginPath from "./targets/claude-code/brain-mcp.ts" with { type: "file" };
// Note: cursor and antigravity plugins don't exist yet, we'll handle them gracefully

// Map of target to embedded plugin path
const EMBEDDED_PLUGINS: Partial<Record<InstallTarget, string>> = {
  opencode: opencodePluginPath,
  "claude-code": claudeCodePluginPath,
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
}

const TARGETS: Record<InstallTarget, TargetConfig> = {
  opencode: {
    name: "OpenCode",
    description: "OpenCode AI coding assistant",
    configDir: (home) => join(home, "dot/config/opencode"),
    pluginDir: (home) => join(home, "dot/config/opencode/plugin"),
    pluginFile: "brain.ts",
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

export async function installPlugin(options: InstallOptions): Promise<InstallResult> {
  const { target, force = false, dryRun = false, apiUrl } = options;
  const config = getTargetConfig(target);
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

  // Read source from embedded file (works in both bun run and compiled binary)
  // Bun.file() handles both regular paths and $bunfs:// paths
  let content = await Bun.file(embeddedPath).text();

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
