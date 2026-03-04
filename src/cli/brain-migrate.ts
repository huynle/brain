/**
 * Brain Migrate CLI - Database migration command
 *
 * Extracted as a testable module from brain.ts CLI.
 * Returns structured results instead of printing directly.
 */

import type { MigrationResult } from "../core/migration";

// =============================================================================
// Types
// =============================================================================

export interface MigrateCommandResult {
  exitCode: number;
  output: string;
}

// =============================================================================
// Formatting
// =============================================================================

/**
 * Format a MigrationResult into a human-readable string with ANSI colors.
 *
 * @param result - The migration result to format
 * @param dryRun - Whether this was a dry-run execution
 * @returns Formatted string ready for console output
 */
export function formatMigrationResult(result: MigrationResult, dryRun: boolean): string {
  const lines: string[] = [];

  lines.push(`\x1b[1mBrain Database Migration\x1b[0m`);
  lines.push("========================");
  lines.push("");
  lines.push(`Strategy: ${result.strategy} (auto-detected)`);
  lines.push(`Dry run:  ${dryRun ? "yes" : "no"}`);
  lines.push("");
  lines.push("Results:");
  lines.push(`  Notes:           ${result.notes}`);
  lines.push(`  Links:           ${result.links}`);
  lines.push(`  Tags:            ${result.tags}`);
  lines.push(`  Entry metadata:  ${result.entryMeta}`);
  lines.push(`  Generated tasks: ${result.generatedTasks}`);
  lines.push("");

  if (result.errors.length > 0) {
    lines.push(`\x1b[31mErrors (${result.errors.length}):\x1b[0m`);
    for (const err of result.errors) {
      lines.push(`  \x1b[31m- ${err}\x1b[0m`);
    }
    lines.push("");
  }

  lines.push(`Duration: ${result.duration}ms`);

  if (result.errors.length > 0) {
    lines.push(`Status:   \x1b[33mCompleted with errors\x1b[0m`);
  } else {
    lines.push(`Status:   \x1b[32mSuccess\x1b[0m`);
  }

  return lines.join("\n");
}

// =============================================================================
// Command Execution
// =============================================================================

/**
 * Execute the migrate command.
 *
 * @param args - CLI arguments after "migrate"
 * @returns MigrateCommandResult with exit code and output
 */
export async function execMigrateCommand(args: string[]): Promise<MigrateCommandResult> {
  const dryRun = args.includes("--dry-run");
  const forceRebuild = args.includes("--rebuild");

  // Get brainDir from config
  const { getConfig } = await import("../config");
  const config = getConfig();
  const brainDir = config.brain.brainDir;

  const { join } = await import("path");
  const targetDbPath = join(brainDir, "brain.db");

  // Run migration
  const { DatabaseMigration } = await import("../core/migration");
  const migration = new DatabaseMigration();

  try {
    const result = await migration.autoMigrate(brainDir, targetDbPath, {
      dryRun,
      forceRebuild,
    });

    const output = formatMigrationResult(result, dryRun);
    const exitCode = result.errors.length > 0 ? 1 : 0;

    return { exitCode, output };
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    return {
      exitCode: 1,
      output: `\x1b[31mMigration failed: ${message}\x1b[0m`,
    };
  }
}
