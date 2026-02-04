/**
 * Brain Doctor - Service
 *
 * Diagnostic service for validating and fixing brain configuration.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync, accessSync, constants } from "fs";
import { join, dirname } from "path";
import { createHash } from "crypto";
import { isZkAvailable, getZkVersion, execZk } from "../zk-client";
import { getDb } from "../db";
import { ENTRY_TYPES, type EntryType } from "../types";
import type { Check, CheckStatus, DoctorResult, DoctorOptions } from "./types";

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
