/**
 * Brain Doctor - Service
 *
 * Diagnostic service for validating and fixing brain configuration.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync, accessSync, constants } from "fs";
import { join, dirname } from "path";
import { createHash } from "crypto";
import { homedir } from "os";
import { isZkAvailable, getZkVersion, execZk } from "../zk-client";
import { getDb } from "../db";
import { ENTRY_TYPES, type EntryType } from "../types";
import type { Check, CheckStatus, DoctorResult, DoctorOptions, VersionCheck } from "./types";
import { TOOLS, checkToolVersion, type ToolInfo } from "./version-checker";

// =============================================================================
// Constants
// =============================================================================

// Path to assets directory relative to this file
// import.meta.dir is /path/to/src/core/doctor
// We need /path/to/src/assets
const ASSETS_DIR = join(dirname(dirname(import.meta.dir)), "assets");
const REFERENCE_CONFIG_PATH = join(ASSETS_DIR, "zk-config.toml");
const REFERENCE_TEMPLATES_DIR = join(ASSETS_DIR, "templates");

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
    checks.push(await this.checkZkCliAvailable());
    checks.push(await this.checkZkNotebook());
    checks.push(await this.checkZkConfig());

    // Template checks
    for (const type of ENTRY_TYPES) {
      checks.push(await this.checkTemplateExists(type));
    }
    for (const type of ENTRY_TYPES) {
      checks.push(await this.checkTemplateContent(type));
    }

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
   * Check if zk CLI is available on the system.
   */
  async checkZkCliAvailable(): Promise<Check> {
    const available = await isZkAvailable();

    if (!available) {
      return {
        name: "zk-cli-available",
        status: "fail",
        message: "zk CLI not found",
        fixable: false,
        details: "Install zk from https://github.com/zk-org/zk",
      };
    }

    const version = await getZkVersion();
    return {
      name: "zk-cli-available",
      status: "pass",
      message: `zk CLI available (version ${version || "unknown"})`,
      fixable: false,
    };
  }

  /**
   * Check if zk notebook (.zk directory) exists in BRAIN_DIR.
   */
  async checkZkNotebook(): Promise<Check> {
    const zkDir = join(this.brainDir, ".zk");
    const exists = existsSync(zkDir);

    if (!exists) {
      return {
        name: "zk-notebook",
        status: "fail",
        message: ".zk directory not found",
        fixable: true,
        details: `Expected at: ${zkDir}`,
      };
    }

    return {
      name: "zk-notebook",
      status: "pass",
      message: ".zk notebook directory exists",
      fixable: false,
    };
  }

  /**
   * Validate zk config settings (id-length, id-charset).
   */
  async checkZkConfig(): Promise<Check> {
    const configPath = join(this.brainDir, ".zk", "config.toml");

    if (!existsSync(configPath)) {
      return {
        name: "zk-config",
        status: "fail",
        message: "config.toml not found",
        fixable: true,
        details: `Expected at: ${configPath}`,
      };
    }

    try {
      const content = readFileSync(configPath, "utf-8");
      const issues: string[] = [];

      // Check id-length = 8
      const idLengthMatch = content.match(/id-length\s*=\s*(\d+)/);
      if (!idLengthMatch) {
        issues.push("id-length not set");
      } else if (idLengthMatch[1] !== "8") {
        issues.push(`id-length is ${idLengthMatch[1]}, expected 8`);
      }

      // Check id-charset = "alphanum"
      const idCharsetMatch = content.match(/id-charset\s*=\s*"(\w+)"/);
      if (!idCharsetMatch) {
        issues.push("id-charset not set");
      } else if (idCharsetMatch[1] !== "alphanum") {
        issues.push(`id-charset is "${idCharsetMatch[1]}", expected "alphanum"`);
      }

      if (issues.length > 0) {
        return {
          name: "zk-config",
          status: "fail",
          message: "config.toml has invalid settings",
          fixable: true,
          details: issues.join("; "),
        };
      }

      return {
        name: "zk-config",
        status: "pass",
        message: "config.toml settings valid",
        fixable: false,
      };
    } catch (err) {
      return {
        name: "zk-config",
        status: "fail",
        message: "Failed to read config.toml",
        fixable: true,
        details: err instanceof Error ? err.message : String(err),
      };
    }
  }

  /**
   * Check if a template file exists for the given entry type.
   */
  async checkTemplateExists(type: EntryType): Promise<Check> {
    const templatePath = join(this.brainDir, ".zk", "templates", `${type}.md`);
    const exists = existsSync(templatePath);

    if (!exists) {
      return {
        name: `template-exists-${type}`,
        status: "fail",
        message: `Template ${type}.md not found`,
        fixable: true,
        details: `Expected at: ${templatePath}`,
      };
    }

    return {
      name: `template-exists-${type}`,
      status: "pass",
      message: `Template ${type}.md exists`,
      fixable: false,
    };
  }

  /**
   * Check if a template matches the reference template by hash.
   */
  async checkTemplateContent(type: EntryType): Promise<Check> {
    const templatePath = join(this.brainDir, ".zk", "templates", `${type}.md`);
    const referencePath = join(REFERENCE_TEMPLATES_DIR, `${type}.md`);

    if (!existsSync(templatePath)) {
      return {
        name: `template-content-${type}`,
        status: "skip",
        message: `Template ${type}.md does not exist`,
        fixable: false,
      };
    }

    if (!existsSync(referencePath)) {
      return {
        name: `template-content-${type}`,
        status: "warn",
        message: `Reference template ${type}.md not found`,
        fixable: false,
        details: `Expected reference at: ${referencePath}`,
      };
    }

    try {
      const currentContent = readFileSync(templatePath, "utf-8");
      const referenceContent = readFileSync(referencePath, "utf-8");

      const currentHash = this.hashContent(currentContent);
      const referenceHash = this.hashContent(referenceContent);

      if (currentHash !== referenceHash) {
        return {
          name: `template-content-${type}`,
          status: "warn",
          message: `Template ${type}.md differs from reference`,
          fixable: true,
          details: "Use --fix --force to reset to reference",
        };
      }

      return {
        name: `template-content-${type}`,
        status: "pass",
        message: `Template ${type}.md matches reference`,
        fixable: false,
      };
    } catch (err) {
      return {
        name: `template-content-${type}`,
        status: "fail",
        message: `Failed to read template ${type}.md`,
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
   * Check versions of external tools (opencode, claude, bun, zk).
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
    const home = homedir();
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
        details: "Run: brain install opencode --force",
      });
    }

    // Check required skills
    const requiredSkills = [
      { name: "do-work", description: "task queue processing" },
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
          details: `Run: brain install opencode --force (${skill.description})`,
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
          details: `Run: brain install opencode --force (${cmd.description})`,
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
      case "zk-notebook":
        await this.fixZkNotebook();
        break;
      case "zk-config":
        await this.fixZkConfig();
        break;
      case "directory-permissions":
        await this.fixDirectoryPermissions();
        break;
      default:
        // Handle template fixes
        if (check.name.startsWith("template-exists-") || check.name.startsWith("template-content-")) {
          const type = check.name.replace(/^template-(exists|content)-/, "") as EntryType;
          await this.fixTemplate(type);
        }
    }
  }

  /**
   * Initialize zk notebook by running `zk init`.
   */
  async fixZkNotebook(): Promise<void> {
    // Ensure BRAIN_DIR exists
    if (!existsSync(this.brainDir)) {
      mkdirSync(this.brainDir, { recursive: true });
    }

    // Run zk init
    const result = await execZk(["init", "--no-input", this.brainDir]);
    if (result.exitCode !== 0) {
      throw new Error(`zk init failed: ${result.stderr}`);
    }

    // Also copy our reference config
    await this.fixZkConfig();
  }

  /**
   * Copy reference zk config to BRAIN_DIR.
   */
  async fixZkConfig(): Promise<void> {
    const zkDir = join(this.brainDir, ".zk");
    const configPath = join(zkDir, "config.toml");

    // Ensure .zk directory exists
    if (!existsSync(zkDir)) {
      mkdirSync(zkDir, { recursive: true });
    }

    // Copy reference config
    if (!existsSync(REFERENCE_CONFIG_PATH)) {
      throw new Error(`Reference config not found at: ${REFERENCE_CONFIG_PATH}`);
    }

    const content = readFileSync(REFERENCE_CONFIG_PATH, "utf-8");
    writeFileSync(configPath, content, "utf-8");
  }

  /**
   * Copy reference template to BRAIN_DIR.
   */
  async fixTemplate(type: EntryType): Promise<void> {
    const templatesDir = join(this.brainDir, ".zk", "templates");
    const templatePath = join(templatesDir, `${type}.md`);
    const referencePath = join(REFERENCE_TEMPLATES_DIR, `${type}.md`);

    // Ensure templates directory exists
    if (!existsSync(templatesDir)) {
      mkdirSync(templatesDir, { recursive: true });
    }

    // Copy reference template
    if (!existsSync(referencePath)) {
      throw new Error(`Reference template not found at: ${referencePath}`);
    }

    const content = readFileSync(referencePath, "utf-8");
    writeFileSync(templatePath, content, "utf-8");
  }

  /**
   * Create BRAIN_DIR if it doesn't exist.
   */
  async fixDirectoryPermissions(): Promise<void> {
    if (!existsSync(this.brainDir)) {
      mkdirSync(this.brainDir, { recursive: true });
    }
  }

  // ===========================================================================
  // Utility Methods
  // ===========================================================================

  /**
   * Generate a hash of content for comparison.
   */
  private hashContent(content: string): string {
    return createHash("sha256").update(content).digest("hex");
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
