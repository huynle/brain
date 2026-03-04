/**
 * Brain Doctor - Service
 *
 * Diagnostic service for validating and fixing brain configuration.
 */

import { existsSync, mkdirSync, accessSync, constants } from "fs";
import { join } from "path";
import { homedir } from "os";
import { getDb } from "../db";
import { createStorageLayer } from "../storage";
import type { Check, DoctorResult, DoctorOptions } from "./types";
import { TOOLS, checkToolVersion, type ToolInfo } from "./version-checker";

// =============================================================================
// DoctorService Class
// =============================================================================

export class DoctorService {
  private brainDir: string;
  private options: DoctorOptions;

  constructor(brainDir: string, options: DoctorOptions = {}) {
    this.brainDir = brainDir;
    this.options = options;
  }

  // ===========================================================================
  // Public Methods
  // ===========================================================================

  /**
   * Run all diagnostic checks and return results.
   */
  async diagnose(): Promise<DoctorResult> {
    const checks: Check[] = [];
    const timestamp = new Date().toISOString();

    // Run all checks
    checks.push(await this.checkStorageLayer());
    checks.push(await this.checkDatabaseHealth());
    checks.push(await this.checkDirectoryPermissions());

    // Version checks (can be skipped with --skip-version-check for offline use)
    if (!this.options.skipVersionCheck) {
      const versionChecks = await this.checkToolVersions();
      checks.push(...versionChecks);
    }

    // OpenCode integration checks (if OpenCode is installed)
    const opencodeChecks = await this.checkOpenCodeIntegration();
    checks.push(...opencodeChecks);

    // Calculate summary
    const summary = {
      passed: checks.filter((c) => c.status === "pass").length,
      failed: checks.filter((c) => c.status === "fail").length,
      warnings: checks.filter((c) => c.status === "warn").length,
      skipped: checks.filter((c) => c.status === "skip").length,
    };

    const healthy = summary.failed === 0;

    return {
      brainDir: this.brainDir,
      timestamp,
      checks,
      summary,
      healthy,
    };
  }

  /**
   * Run diagnosis and fix any fixable issues.
   */
  async fix(): Promise<DoctorResult> {
    const result = await this.diagnose();

    // Find fixable failures
    const fixable = result.checks.filter(
      (c) => (c.status === "fail" || (this.options.force && c.status === "warn")) && c.fixable
    );

    if (fixable.length === 0) {
      return result;
    }

    // Apply fixes
    for (const check of fixable) {
      await this.applyFix(check);
    }

    // Re-run diagnosis to get updated results
    return this.diagnose();
  }

  // ===========================================================================
  // Check Methods
  // ===========================================================================

  /**
   * Check if the StorageLayer database (brain.db) exists and is accessible.
   */
  async checkStorageLayer(): Promise<Check> {
    const dbPath = join(this.brainDir, "brain.db");

    if (!existsSync(dbPath)) {
      return {
        name: "storage-layer",
        status: "fail",
        message: "brain.db not found",
        fixable: true,
        details: `Expected at: ${dbPath}`,
      };
    }

    try {
      const sl = createStorageLayer(dbPath);
      const stats = sl.getStats();
      sl.close();

      return {
        name: "storage-layer",
        status: "pass",
        message: "brain.db is accessible",
        fixable: false,
        details: `total: ${stats.total}, types: ${Object.keys(stats.byType).length}`,
      };
    } catch (err) {
      return {
        name: "storage-layer",
        status: "fail",
        message: "brain.db is not accessible",
        fixable: true,
        details: err instanceof Error ? err.message : String(err),
      };
    }
  }

  /**
   * Check if the database is healthy by running a simple query.
   */
  async checkDatabaseHealth(): Promise<Check> {
    try {
      const db = getDb();
      const result = db.prepare("SELECT 1 as test").get() as { test: number } | undefined;

      if (!result || result.test !== 1) {
        return {
          name: "database-health",
          status: "fail",
          message: "Database query returned unexpected result",
          fixable: false,
        };
      }

      return {
        name: "database-health",
        status: "pass",
        message: "Database is healthy",
        fixable: false,
      };
    } catch (err) {
      return {
        name: "database-health",
        status: "fail",
        message: "Database connection failed",
        fixable: false,
        details: err instanceof Error ? err.message : String(err),
      };
    }
  }

  /**
   * Check versions of external tools (opencode, claude, bun).
   */
  async checkToolVersions(): Promise<Check[]> {
    const checks: Check[] = [];

    for (const tool of TOOLS) {
      const check = await this.checkSingleToolVersion(tool);
      checks.push(check);
    }

    return checks;
  }

  /**
   * Check version for a single tool.
   */
  async checkSingleToolVersion(tool: ToolInfo): Promise<Check> {
    try {
      const result = await checkToolVersion(tool);

      if (!result.isInstalled) {
        // Tool not installed
        if (tool.required) {
          return {
            name: `version-${tool.name}`,
            status: "fail",
            message: `${tool.name} not installed (required)`,
            fixable: false,
            details: `Install from: ${tool.installUrl}`,
          };
        }
        return {
          name: `version-${tool.name}`,
          status: "skip",
          message: `${tool.name} not installed (optional)`,
          fixable: false,
          details: `Install from: ${tool.installUrl}`,
        };
      }

      if (result.isOutdated) {
        return {
          name: `version-${tool.name}`,
          status: "warn",
          message: `${tool.name} is outdated: ${result.installed} → ${result.latest}`,
          fixable: false,
          details: `Update from: ${tool.installUrl}`,
        };
      }

      // Could not fetch latest version (network issue)
      if (!result.latest) {
        return {
          name: `version-${tool.name}`,
          status: "pass",
          message: `${tool.name} ${result.installed} (could not check latest)`,
          fixable: false,
        };
      }

      return {
        name: `version-${tool.name}`,
        status: "pass",
        message: `${tool.name} ${result.installed} (latest: ${result.latest})`,
        fixable: false,
      };
    } catch (err) {
      return {
        name: `version-${tool.name}`,
        status: "warn",
        message: `Failed to check ${tool.name} version`,
        fixable: false,
        details: err instanceof Error ? err.message : String(err),
      };
    }
  }

  /**
   * Check if the BRAIN_DIR has read/write permissions.
   */
  async checkDirectoryPermissions(): Promise<Check> {
    try {
      // Check if directory exists
      if (!existsSync(this.brainDir)) {
        return {
          name: "directory-permissions",
          status: "fail",
          message: "BRAIN_DIR does not exist",
          fixable: true,
          details: `Path: ${this.brainDir}`,
        };
      }

      // Check read permission
      accessSync(this.brainDir, constants.R_OK);

      // Check write permission
      accessSync(this.brainDir, constants.W_OK);

      return {
        name: "directory-permissions",
        status: "pass",
        message: "BRAIN_DIR has read/write permissions",
        fixable: false,
      };
    } catch (err) {
      return {
        name: "directory-permissions",
        status: "fail",
        message: "BRAIN_DIR permission check failed",
        fixable: false,
        details: err instanceof Error ? err.message : String(err),
      };
    }
  }

  /**
   * Check OpenCode integration - plugin, skills, and commands are installed.
   */
  async checkOpenCodeIntegration(): Promise<Check[]> {
    const checks: Check[] = [];
    const home = process.env.HOME || homedir();
    const opencodeConfigDir = join(home, ".config/opencode");

    // First check if OpenCode is installed at all
    if (!existsSync(opencodeConfigDir)) {
      checks.push({
        name: "opencode-installed",
        status: "skip",
        message: "OpenCode not installed (skipping integration checks)",
        fixable: false,
        details: `Expected at: ${opencodeConfigDir}`,
      });
      return checks;
    }

    // OpenCode is installed, now check brain integration
    checks.push({
      name: "opencode-installed",
      status: "pass",
      message: "OpenCode config directory exists",
      fixable: false,
    });

    // Check main plugin
    const pluginDir = join(opencodeConfigDir, "plugin");
    const mainPluginPath = join(pluginDir, "brain.ts");
    if (existsSync(mainPluginPath)) {
      checks.push({
        name: "opencode-plugin-brain",
        status: "pass",
        message: "brain.ts plugin installed",
        fixable: false,
      });
    } else {
      checks.push({
        name: "opencode-plugin-brain",
        status: "fail",
        message: "brain.ts plugin not installed",
        fixable: true,
        details: "Run: brain install opencode",
      });
    }

    // Check planning plugin
    const planningPluginPath = join(pluginDir, "brain-planning.ts");
    if (existsSync(planningPluginPath)) {
      checks.push({
        name: "opencode-plugin-planning",
        status: "pass",
        message: "brain-planning.ts plugin installed",
        fixable: false,
      });
    } else {
      checks.push({
        name: "opencode-plugin-planning",
        status: "warn",
        message: "brain-planning.ts plugin not installed",
        fixable: true,
        details: "Run: brain install opencode",
      });
    }

    // Check required skills
    const requiredSkills = [
      { name: "brain-runner-queue", description: "task queue processing" },
      { name: "brain-planning", description: "plan persistence" },
      { name: "using-brain", description: "knowledge patterns" },
    ];

    for (const skill of requiredSkills) {
      const skillPath = join(opencodeConfigDir, "skill", skill.name, "SKILL.md");
      if (existsSync(skillPath)) {
        checks.push({
          name: `opencode-skill-${skill.name}`,
          status: "pass",
          message: `${skill.name} skill installed`,
          fixable: false,
        });
      } else {
        checks.push({
          name: `opencode-skill-${skill.name}`,
          status: "warn",
          message: `${skill.name} skill not installed`,
          fixable: true,
          details: `Run: brain install opencode (${skill.description})`,
        });
      }
    }

    // Check required commands
    const requiredCommands = [
      { name: "do", description: "add tasks to queue" },
      { name: "work", description: "process task queue" },
    ];

    for (const cmd of requiredCommands) {
      const cmdPath = join(opencodeConfigDir, "command", `${cmd.name}.md`);
      if (existsSync(cmdPath)) {
        checks.push({
          name: `opencode-command-${cmd.name}`,
          status: "pass",
          message: `/${cmd.name} command installed`,
          fixable: false,
        });
      } else {
        checks.push({
          name: `opencode-command-${cmd.name}`,
          status: "warn",
          message: `/${cmd.name} command not installed`,
          fixable: true,
          details: `Run: brain install opencode (${cmd.description})`,
        });
      }
    }

    // Check required agents
    const requiredAgents = [
      { name: "brain-planner", description: "pure coordination, delegates to subagents" },
    ];

    for (const agent of requiredAgents) {
      const agentPath = join(opencodeConfigDir, "agent", `${agent.name}.md`);
      if (existsSync(agentPath)) {
        checks.push({
          name: `opencode-agent-${agent.name}`,
          status: "pass",
          message: `${agent.name} agent installed`,
          fixable: false,
        });
      } else {
        checks.push({
          name: `opencode-agent-${agent.name}`,
          status: "warn",
          message: `${agent.name} agent not installed`,
          fixable: true,
          details: `Run: brain install opencode (${agent.description})`,
        });
      }
    }

    return checks;
  }

  // ===========================================================================
  // Fix Methods
  // ===========================================================================

  /**
   * Apply a fix for a specific check.
   */
  private async applyFix(check: Check): Promise<void> {
    if (this.options.dryRun) {
      console.log(`[DRY RUN] Would fix: ${check.name}`);
      return;
    }

    switch (check.name) {
      case "directory-permissions":
        await this.fixDirectoryPermissions();
        break;
    }
  }

  /**
   * Create BRAIN_DIR if it doesn't exist.
   */
  async fixDirectoryPermissions(): Promise<void> {
    if (!existsSync(this.brainDir)) {
      mkdirSync(this.brainDir, { recursive: true });
    }
  }

}

// =============================================================================
// Factory Function
// =============================================================================

/**
 * Create a new DoctorService instance.
 */
export function createDoctorService(brainDir: string, options: DoctorOptions = {}): DoctorService {
  return new DoctorService(brainDir, options);
}
