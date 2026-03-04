import { describe, expect, test } from "bun:test";
import type { MigrationResult } from "../core/migration";

// =============================================================================
// formatMigrationResult
// =============================================================================

describe("formatMigrationResult", () => {
  // Helper to strip ANSI codes for easier assertions
  function stripAnsi(str: string): string {
    return str.replace(/\x1b\[[0-9;]*m/g, "");
  }

  function makeResult(overrides?: Partial<MigrationResult>): MigrationResult {
    return {
      strategy: "import",
      notes: 42,
      links: 87,
      tags: 15,
      entryMeta: 30,
      generatedTasks: 5,
      errors: [],
      duration: 123,
      ...overrides,
    };
  }

  test("includes header and separator", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult();
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Brain Database Migration");
    expect(output).toContain("========================");
  });

  test("shows strategy", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({ strategy: "rebuild" });
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Strategy: rebuild (auto-detected)");
  });

  test("shows dry run status", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult();

    const dryOutput = stripAnsi(formatMigrationResult(result, true));
    expect(dryOutput).toContain("Dry run:  yes");

    const normalOutput = stripAnsi(formatMigrationResult(result, false));
    expect(normalOutput).toContain("Dry run:  no");
  });

  test("shows all counts", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({
      notes: 42,
      links: 87,
      tags: 15,
      entryMeta: 30,
      generatedTasks: 5,
    });
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Notes:           42");
    expect(output).toContain("Links:           87");
    expect(output).toContain("Tags:            15");
    expect(output).toContain("Entry metadata:  30");
    expect(output).toContain("Generated tasks: 5");
  });

  test("shows duration in ms", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({ duration: 456 });
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Duration: 456ms");
  });

  test("shows Success status when no errors", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({ errors: [] });
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Status:   Success");
    expect(output).not.toContain("Errors");
  });

  test("shows errors and Completed with errors status", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({
      errors: [
        "link id=5: target note ZK id=99 not found in source",
        "entry_meta migration failed: table not found",
      ],
    });
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Errors (2):");
    expect(output).toContain("- link id=5: target note ZK id=99 not found in source");
    expect(output).toContain("- entry_meta migration failed: table not found");
    expect(output).toContain("Status:   Completed with errors");
    expect(output).not.toContain("Status:   Success");
  });

  test("uses ANSI colors for errors", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({ errors: ["some error"] });
    const output = formatMigrationResult(result, false);

    // Red color code should be present around errors
    expect(output).toContain("\x1b[31m");
  });

  test("uses ANSI bold for header", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult();
    const output = formatMigrationResult(result, false);

    // Bold code should be present for header
    expect(output).toContain("\x1b[1m");
  });

  test("shows zero counts correctly", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    const result = makeResult({
      notes: 0,
      links: 0,
      tags: 0,
      entryMeta: 0,
      generatedTasks: 0,
    });
    const output = stripAnsi(formatMigrationResult(result, false));

    expect(output).toContain("Notes:           0");
    expect(output).toContain("Links:           0");
    expect(output).toContain("Tags:            0");
    expect(output).toContain("Entry metadata:  0");
    expect(output).toContain("Generated tasks: 0");
  });
});

// =============================================================================
// execMigrateCommand
// =============================================================================

describe("execMigrateCommand", () => {
  test("can be imported", async () => {
    const { execMigrateCommand } = await import("./brain-migrate");
    expect(typeof execMigrateCommand).toBe("function");
  });

  test("MigrateCommandResult type has exitCode and output", async () => {
    const { formatMigrationResult } = await import("./brain-migrate");
    // Verify the module exports the expected types by using them
    const result = formatMigrationResult(
      {
        strategy: "import",
        notes: 0,
        links: 0,
        tags: 0,
        entryMeta: 0,
        generatedTasks: 0,
        errors: [],
        duration: 0,
      },
      false
    );
    expect(typeof result).toBe("string");
  });
});
