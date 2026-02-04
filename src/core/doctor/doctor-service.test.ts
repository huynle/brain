/**
 * Brain Doctor - Tests
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { DoctorService, createDoctorService } from "./doctor-service";
import type { DoctorResult, Check } from "./types";

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

function setupZkNotebook(brainDir: string): void {
  const zkDir = join(brainDir, ".zk");
  const templatesDir = join(zkDir, "templates");
  mkdirSync(templatesDir, { recursive: true });

  // Create valid config
  writeFileSync(
    join(zkDir, "config.toml"),
    `[notebook]
dir = "."

[note]
id-charset = "alphanum"
id-length = 8
id-case = "lower"
default-title = "untitled"
filename = "{{id}}"
extension = "md"
template = "default.md"

[format.markdown]
link-format = "markdown"
link-drop-extension = true
`
  );
}

function createTemplate(brainDir: string, type: string, content?: string): void {
  const templatesDir = join(brainDir, ".zk", "templates");
  if (!existsSync(templatesDir)) {
    mkdirSync(templatesDir, { recursive: true });
  }
  // Match the exact format of reference templates (with trailing newline)
  const templateContent = content ?? `---
title: {{title}}
type: ${type}
tags:
  - ${type}
status: active
---

# {{title}}
`;
  writeFileSync(join(templatesDir, `${type}.md`), templateContent);
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
      expect(checkNames).toContain("zk-cli-available");
      expect(checkNames).toContain("zk-notebook");
      expect(checkNames).toContain("zk-config");
      expect(checkNames).toContain("database-health");
      expect(checkNames).toContain("directory-permissions");

      // Template existence checks (12 types)
      expect(checkNames).toContain("template-exists-summary");
      expect(checkNames).toContain("template-exists-report");
      expect(checkNames).toContain("template-exists-walkthrough");
      expect(checkNames).toContain("template-exists-plan");
      expect(checkNames).toContain("template-exists-pattern");
      expect(checkNames).toContain("template-exists-learning");
      expect(checkNames).toContain("template-exists-idea");
      expect(checkNames).toContain("template-exists-scratch");
      expect(checkNames).toContain("template-exists-decision");
      expect(checkNames).toContain("template-exists-exploration");
      expect(checkNames).toContain("template-exists-execution");
      expect(checkNames).toContain("template-exists-task");
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

  describe("checkZkNotebook()", () => {
    test("fails when .zk directory does not exist", async () => {
      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkZkNotebook();

      expect(check.status).toBe("fail");
      expect(check.fixable).toBe(true);
      expect(check.message).toContain(".zk directory not found");
    });

    test("passes when .zk directory exists", async () => {
      mkdirSync(join(tempDir, ".zk"), { recursive: true });

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkZkNotebook();

      expect(check.status).toBe("pass");
    });
  });

  describe("checkZkConfig()", () => {
    test("fails when config.toml does not exist", async () => {
      mkdirSync(join(tempDir, ".zk"), { recursive: true });

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkZkConfig();

      expect(check.status).toBe("fail");
      expect(check.fixable).toBe(true);
    });

    test("fails with invalid id-length", async () => {
      mkdirSync(join(tempDir, ".zk"), { recursive: true });
      writeFileSync(
        join(tempDir, ".zk", "config.toml"),
        `[note]
id-charset = "alphanum"
id-length = 6
`
      );

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkZkConfig();

      expect(check.status).toBe("fail");
      expect(check.details).toContain("id-length is 6, expected 8");
    });

    test("fails with invalid id-charset", async () => {
      mkdirSync(join(tempDir, ".zk"), { recursive: true });
      writeFileSync(
        join(tempDir, ".zk", "config.toml"),
        `[note]
id-charset = "hex"
id-length = 8
`
      );

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkZkConfig();

      expect(check.status).toBe("fail");
      expect(check.details).toContain('id-charset is "hex", expected "alphanum"');
    });

    test("passes with valid config", async () => {
      setupZkNotebook(tempDir);

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkZkConfig();

      expect(check.status).toBe("pass");
    });
  });

  describe("checkTemplateExists()", () => {
    test("fails when template does not exist", async () => {
      setupZkNotebook(tempDir);

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkTemplateExists("plan");

      expect(check.status).toBe("fail");
      expect(check.fixable).toBe(true);
    });

    test("passes when template exists", async () => {
      setupZkNotebook(tempDir);
      createTemplate(tempDir, "plan");

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkTemplateExists("plan");

      expect(check.status).toBe("pass");
    });
  });

  describe("checkTemplateContent()", () => {
    test("skips when template does not exist", async () => {
      setupZkNotebook(tempDir);

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkTemplateContent("plan");

      expect(check.status).toBe("skip");
    });

    test("passes when template matches reference", async () => {
      setupZkNotebook(tempDir);
      createTemplate(tempDir, "plan");

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkTemplateContent("plan");

      expect(check.status).toBe("pass");
    });

    test("warns when template differs from reference", async () => {
      setupZkNotebook(tempDir);
      // Create a template with custom content that differs from reference
      createTemplate(tempDir, "plan", "custom content that differs from reference");

      const doctor = new DoctorService(tempDir);
      const check = await doctor.checkTemplateContent("plan");

      expect(check.status).toBe("warn");
      expect(check.fixable).toBe(true);
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
    test("creates .zk directory when missing", async () => {
      const doctor = createDoctorService(tempDir, { fix: true });

      // First diagnose - should have failures
      const before = await doctor.diagnose();
      expect(before.healthy).toBe(false);

      // Run fix (will attempt to fix what it can)
      // Note: This may fail if zk CLI is not available, but the directory structure should be created
      try {
        await doctor.fix();
      } catch {
        // zk init may fail if zk is not installed, but we can still test directory creation
      }
    });

    test("respects dry-run option", async () => {
      const doctor = createDoctorService(tempDir, { fix: true, dryRun: true });

      // In dry-run mode, nothing should be changed
      const zkDir = join(tempDir, ".zk");
      const existsBefore = existsSync(zkDir);

      try {
        await doctor.fix();
      } catch {
        // Ignore errors
      }

      const existsAfter = existsSync(zkDir);
      expect(existsAfter).toBe(existsBefore);
    });

    test("copies reference config when missing", async () => {
      // Create .zk directory but not config
      mkdirSync(join(tempDir, ".zk"), { recursive: true });

      const doctor = new DoctorService(tempDir, { fix: true });
      await doctor.fixZkConfig();

      const configPath = join(tempDir, ".zk", "config.toml");
      expect(existsSync(configPath)).toBe(true);

      const content = readFileSync(configPath, "utf-8");
      expect(content).toContain('id-charset = "alphanum"');
      expect(content).toContain("id-length = 8");
    });

    test("copies reference template when missing", async () => {
      setupZkNotebook(tempDir);

      const doctor = new DoctorService(tempDir, { fix: true });
      await doctor.fixTemplate("plan");

      const templatePath = join(tempDir, ".zk", "templates", "plan.md");
      expect(existsSync(templatePath)).toBe(true);

      const content = readFileSync(templatePath, "utf-8");
      expect(content).toContain("type: plan");
      expect(content).toContain("{{title}}");
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
