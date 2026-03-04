/**
 * Brain Doctor - Tests
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { DoctorService, createDoctorService } from "./doctor-service";

// =============================================================================
// Test Helpers
// =============================================================================

function createTempDir(): string {
  const dir = join(tmpdir(), `brain-doctor-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  mkdirSync(dir, { recursive: true });
  return dir;
}

function cleanupTempDir(dir: string): void {
  if (existsSync(dir)) {
    rmSync(dir, { recursive: true, force: true });
  }
}

// =============================================================================
// Tests
// =============================================================================

describe("DoctorService", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = createTempDir();
  });

  afterEach(() => {
    cleanupTempDir(tempDir);
  });

  describe("diagnose()", () => {
    test("returns proper structure", async () => {
      const doctor = createDoctorService(tempDir);
      const result = await doctor.diagnose();

      expect(result).toBeDefined();
      expect(result.brainDir).toBe(tempDir);
      expect(result.timestamp).toBeDefined();
      expect(Array.isArray(result.checks)).toBe(true);
      expect(result.summary).toBeDefined();
      expect(typeof result.summary.passed).toBe("number");
      expect(typeof result.summary.failed).toBe("number");
      expect(typeof result.summary.warnings).toBe("number");
      expect(typeof result.summary.skipped).toBe("number");
      expect(typeof result.healthy).toBe("boolean");
    });

    test("has checks for all expected items", async () => {
      const doctor = createDoctorService(tempDir);
      const result = await doctor.diagnose();

      const checkNames = result.checks.map((c) => c.name);

      // Core checks
      expect(checkNames).toContain("storage-layer");
      expect(checkNames).toContain("database-health");
      expect(checkNames).toContain("directory-permissions");

      // ZK checks should NOT be present (removed)
      expect(checkNames).not.toContain("zk-cli-available");
      expect(checkNames).not.toContain("zk-notebook");
      expect(checkNames).not.toContain("zk-config");
    });

    test("calculates summary correctly", async () => {
      const doctor = createDoctorService(tempDir);
      const result = await doctor.diagnose();

      const { passed, failed, warnings, skipped } = result.summary;
      const total = passed + failed + warnings + skipped;

      expect(total).toBe(result.checks.length);
    });

    test("healthy is false when there are failures", async () => {
      const doctor = createDoctorService(tempDir);
      const result = await doctor.diagnose();

      // Without setup, there should be failures
      expect(result.summary.failed).toBeGreaterThan(0);
      expect(result.healthy).toBe(false);
    });
  });

  describe("checkDirectoryPermissions()", () => {
    test("fails when directory does not exist", async () => {
      const nonExistent = join(tempDir, "nonexistent");

      const doctor = new DoctorService(nonExistent);
      const check = await doctor.checkDirectoryPermissions();

      expect(check.status).toBe("fail");
    });

    test("passes when directory exists with permissions", async () => {
      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkDirectoryPermissions();

      expect(check.status).toBe("pass");
    });
  });

  describe("fix()", () => {
    test("diagnoses failures in empty directory", async () => {
      const doctor = createDoctorService(tempDir, { fix: true });

      // First diagnose - should have failures (no brain.db)
      const before = await doctor.diagnose();
      expect(before.healthy).toBe(false);

      // Run fix (will attempt to fix what it can)
      try {
        await doctor.fix();
      } catch {
        // Some fixes may fail in test environment
      }
    });

    test("respects dry-run option", async () => {
      const doctor = createDoctorService(tempDir, { fix: true, dryRun: true });

      // In dry-run mode, nothing should be changed
      const dbPath = join(tempDir, "brain.db");
      const existsBefore = existsSync(dbPath);

      try {
        await doctor.fix();
      } catch {
        // Ignore errors
      }

      const existsAfter = existsSync(dbPath);
      expect(existsAfter).toBe(existsBefore);
    });
  });

  describe("checkOpenCodeIntegration()", () => {
    test("uses reinstall guidance without --force", async () => {
      const tempHome = createTempDir();
      const originalHome = process.env.HOME;

      try {
        process.env.HOME = tempHome;

        const pluginDir = join(tempHome, ".config", "opencode", "plugin");
        mkdirSync(pluginDir, { recursive: true });
        writeFileSync(join(pluginDir, "brain.ts"), "installed");

        const doctor = new DoctorService(tempDir);
        const checks = await doctor.checkOpenCodeIntegration();

        const remediationDetails = checks
          .map((check) => check.details)
          .filter((detail): detail is string => typeof detail === "string");

        expect(remediationDetails.some((detail) => detail.includes("Run: brain install opencode"))).toBe(true);
        expect(remediationDetails.some((detail) => detail.includes("--force"))).toBe(false);
      } finally {
        if (originalHome === undefined) {
          delete process.env.HOME;
        } else {
          process.env.HOME = originalHome;
        }
        cleanupTempDir(tempHome);
      }
    });
  });

  describe("checkStorageLayer()", () => {
    test("fails when brain.db does not exist", async () => {
      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkStorageLayer();

      expect(check.status).toBe("fail");
      expect(check.fixable).toBe(true);
      expect(check.message).toContain("brain.db not found");
    });

    test("passes when brain.db exists and is accessible", async () => {
      // Create a valid StorageLayer DB
      const { createStorageLayer } = await import("../../core/storage");
      const dbPath = join(tempDir, "brain.db");
      const sl = createStorageLayer(dbPath);
      sl.close();

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkStorageLayer();

      expect(check.status).toBe("pass");
      expect(check.message).toContain("brain.db");
      expect(check.details).toContain("total");
    });

    test("fails when brain.db is corrupted", async () => {
      // Write garbage to brain.db
      writeFileSync(join(tempDir, "brain.db"), "not a sqlite database");

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkStorageLayer();

      expect(check.status).toBe("fail");
      expect(check.fixable).toBe(true);
    });
  });

  describe("diagnose() includes storage-layer check", () => {
    test("includes storage-layer check in results", async () => {
      const doctor = createDoctorService(tempDir);
      const result = await doctor.diagnose();

      const checkNames = result.checks.map((c) => c.name);
      expect(checkNames).toContain("storage-layer");
    });
  });

  describe("createDoctorService()", () => {
    test("creates DoctorService instance", () => {
      const doctor = createDoctorService(tempDir);
      expect(doctor).toBeInstanceOf(DoctorService);
    });

    test("passes options to service", async () => {
      const doctor = createDoctorService(tempDir, { verbose: true, force: true });
      expect(doctor).toBeInstanceOf(DoctorService);
    });
  });
});
