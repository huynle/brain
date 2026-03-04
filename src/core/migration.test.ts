/**
 * Brain API - Migration Module Tests
 *
 * Tests for importing data from living-brain.db into the unified brain.db.
 * Uses in-memory databases for fast, isolated testing.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { Database } from "bun:sqlite";
import { createSchema } from "./schema";
import {
  DatabaseMigration,
  unixMillisToIso,
  type MigrationResult,
} from "./migration";

// =============================================================================
// Helpers
// =============================================================================

/**
 * Create an in-memory source database with the legacy living-brain.db schema.
 */
function createLegacySourceDb(): Database {
  const db = new Database(":memory:");
  db.exec(`
    CREATE TABLE entries_meta (
      path TEXT PRIMARY KEY,
      project_id TEXT NOT NULL,
      access_count INTEGER DEFAULT 0,
      accessed_at INTEGER,
      last_verified INTEGER,
      created_at INTEGER NOT NULL
    )
  `);
  db.exec(`
    CREATE TABLE generated_task_keys (
      project_id TEXT NOT NULL,
      generated_key TEXT NOT NULL,
      lease_owner TEXT,
      lease_expires_at INTEGER,
      task_path TEXT,
      created_at INTEGER NOT NULL,
      updated_at INTEGER NOT NULL,
      PRIMARY KEY (project_id, generated_key)
    )
  `);
  return db;
}

/**
 * Create an in-memory target database with the new unified schema.
 */
function createTargetDb(): Database {
  const db = new Database(":memory:");
  db.exec("PRAGMA foreign_keys = ON");
  createSchema(db);
  return db;
}

// =============================================================================
// unixMillisToIso
// =============================================================================

describe("unixMillisToIso", () => {
  test("converts unix millis to ISO datetime string", () => {
    // 2024-01-15T10:30:00.000Z = 1705314600000
    const ms = 1705314600000;
    const result = unixMillisToIso(ms);
    expect(result).toBe("2024-01-15 10:30:00");
  });

  test("returns null for null input", () => {
    expect(unixMillisToIso(null)).toBeNull();
  });

  test("returns null for undefined input", () => {
    expect(unixMillisToIso(undefined as unknown as number | null)).toBeNull();
  });

  test("returns null for 0", () => {
    expect(unixMillisToIso(0)).toBeNull();
  });

  test("handles epoch correctly", () => {
    // 1970-01-01T00:00:01.000Z (1 second after epoch)
    const result = unixMillisToIso(1000);
    expect(result).toBe("1970-01-01 00:00:01");
  });
});

// =============================================================================
// DatabaseMigration - migrateEntryMeta
// =============================================================================

describe("DatabaseMigration.migrateEntryMeta", () => {
  let sourceDb: Database;
  let targetDb: Database;
  let migration: DatabaseMigration;

  beforeEach(() => {
    sourceDb = createLegacySourceDb();
    targetDb = createTargetDb();
    migration = new DatabaseMigration();
  });

  afterEach(() => {
    sourceDb.close();
    targetDb.close();
  });

  test("migrates entries_meta rows with timestamp conversion", () => {
    // Insert source data with INTEGER timestamps
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run(
        "projects/test/plan/abc.md",
        "test-project",
        5,
        1705314600000, // 2024-01-15T10:30:00Z
        1705401000000, // 2024-01-16T10:30:00Z
        1705228200000  // 2024-01-14T10:30:00Z
      );

    const count = migration.migrateEntryMeta(sourceDb, targetDb);
    expect(count).toBe(1);

    // Verify target data
    const row = targetDb
      .prepare("SELECT * FROM entry_meta WHERE path = ?")
      .get("projects/test/plan/abc.md") as {
      path: string;
      project_id: string;
      access_count: number;
      last_accessed: string;
      last_verified: string;
      created_at: string;
    };

    expect(row.path).toBe("projects/test/plan/abc.md");
    expect(row.project_id).toBe("test-project");
    expect(row.access_count).toBe(5);
    expect(row.last_accessed).toBe("2024-01-15 10:30:00");
    expect(row.last_verified).toBe("2024-01-16 10:30:00");
    expect(row.created_at).toBe("2024-01-14 10:30:00");
  });

  test("handles null timestamps gracefully", () => {
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("projects/test/task/xyz.md", "test-project", 0, null, null, 1705228200000);

    const count = migration.migrateEntryMeta(sourceDb, targetDb);
    expect(count).toBe(1);

    const row = targetDb
      .prepare("SELECT * FROM entry_meta WHERE path = ?")
      .get("projects/test/task/xyz.md") as {
      last_accessed: string | null;
      last_verified: string | null;
      created_at: string;
    };

    expect(row.last_accessed).toBeNull();
    expect(row.last_verified).toBeNull();
    expect(row.created_at).toBe("2024-01-14 10:30:00");
  });

  test("migrates multiple rows", () => {
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("path/a.md", "proj-a", 1, 1705314600000, null, 1705228200000);
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("path/b.md", "proj-b", 2, null, 1705401000000, 1705228200000);
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("path/c.md", "proj-c", 3, 1705314600000, 1705401000000, 1705228200000);

    const count = migration.migrateEntryMeta(sourceDb, targetDb);
    expect(count).toBe(3);

    const rows = targetDb
      .prepare("SELECT COUNT(*) as count FROM entry_meta")
      .get() as { count: number };
    expect(rows.count).toBe(3);
  });

  test("returns 0 for empty source", () => {
    const count = migration.migrateEntryMeta(sourceDb, targetDb);
    expect(count).toBe(0);
  });

  test("is idempotent — duplicate runs do not error (INSERT OR IGNORE)", () => {
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("projects/test/plan/abc.md", "test-project", 5, 1705314600000, null, 1705228200000);

    const count1 = migration.migrateEntryMeta(sourceDb, targetDb);
    expect(count1).toBe(1);

    // Run again — should not error, should not duplicate
    const count2 = migration.migrateEntryMeta(sourceDb, targetDb);
    expect(count2).toBe(1); // still reports 1 row processed from source

    const rows = targetDb
      .prepare("SELECT COUNT(*) as count FROM entry_meta")
      .get() as { count: number };
    expect(rows.count).toBe(1); // only 1 row in target
  });
});

// =============================================================================
// DatabaseMigration - migrateGeneratedTasks
// =============================================================================

describe("DatabaseMigration.migrateGeneratedTasks", () => {
  let sourceDb: Database;
  let targetDb: Database;
  let migration: DatabaseMigration;

  beforeEach(() => {
    sourceDb = createLegacySourceDb();
    targetDb = createTargetDb();
    migration = new DatabaseMigration();
  });

  afterEach(() => {
    sourceDb.close();
    targetDb.close();
  });

  test("migrates rows with task_path, transforms compound PK", () => {
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run(
        "my-project",
        "feature-review:auth:round-1",
        null,
        null,
        "projects/my-project/task/rev123.md",
        1705228200000,
        1705314600000
      );

    const count = migration.migrateGeneratedTasks(sourceDb, targetDb);
    expect(count).toBe(1);

    const row = targetDb
      .prepare("SELECT * FROM generated_tasks WHERE key = ?")
      .get("my-project:feature-review:auth:round-1") as {
      key: string;
      task_path: string;
      feature_id: string | null;
      created_at: string;
    };

    expect(row.key).toBe("my-project:feature-review:auth:round-1");
    expect(row.task_path).toBe("projects/my-project/task/rev123.md");
    expect(row.feature_id).toBeNull(); // source doesn't have feature_id
    expect(row.created_at).toBe("2024-01-14 10:30:00");
  });

  test("skips rows where task_path IS NULL (lease-only rows)", () => {
    // Row with task_path (should be migrated)
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("proj", "key-with-task", null, null, "projects/proj/task/abc.md", 1705228200000, 1705228200000);

    // Row without task_path (lease-only, should be skipped)
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("proj", "key-lease-only", "agent-1", 1705401000000, null, 1705228200000, 1705228200000);

    const count = migration.migrateGeneratedTasks(sourceDb, targetDb);
    expect(count).toBe(1);

    const rows = targetDb
      .prepare("SELECT COUNT(*) as count FROM generated_tasks")
      .get() as { count: number };
    expect(rows.count).toBe(1);
  });

  test("migrates multiple rows", () => {
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("proj-a", "key-1", null, null, "path/task1.md", 1705228200000, 1705228200000);
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("proj-b", "key-2", null, null, "path/task2.md", 1705314600000, 1705314600000);

    const count = migration.migrateGeneratedTasks(sourceDb, targetDb);
    expect(count).toBe(2);
  });

  test("returns 0 for empty source", () => {
    const count = migration.migrateGeneratedTasks(sourceDb, targetDb);
    expect(count).toBe(0);
  });

  test("is idempotent — duplicate runs do not error (INSERT OR IGNORE)", () => {
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("proj", "key-1", null, null, "path/task.md", 1705228200000, 1705228200000);

    migration.migrateGeneratedTasks(sourceDb, targetDb);
    // Run again — should not error
    const count2 = migration.migrateGeneratedTasks(sourceDb, targetDb);
    expect(count2).toBe(1); // still reports 1 from source

    const rows = targetDb
      .prepare("SELECT COUNT(*) as count FROM generated_tasks")
      .get() as { count: number };
    expect(rows.count).toBe(1);
  });
});

// =============================================================================
// DatabaseMigration - migrateFromLivingBrainDb (orchestrator)
// =============================================================================

describe("DatabaseMigration.migrateFromLivingBrainDb", () => {
  let sourceDb: Database;
  let targetDb: Database;
  let migration: DatabaseMigration;

  beforeEach(() => {
    sourceDb = createLegacySourceDb();
    targetDb = createTargetDb();
    migration = new DatabaseMigration();
  });

  afterEach(() => {
    sourceDb.close();
    targetDb.close();
  });

  test("orchestrates both entry_meta and generated_tasks migration", () => {
    // Insert source data
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("projects/test/plan/abc.md", "test", 3, 1705314600000, null, 1705228200000);
    sourceDb
      .prepare(
        "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("projects/test/task/def.md", "test", 1, null, 1705401000000, 1705228200000);

    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("test", "review-key", null, null, "projects/test/task/rev.md", 1705228200000, 1705228200000);
    // Lease-only row — should be skipped
    sourceDb
      .prepare(
        "INSERT INTO generated_task_keys (project_id, generated_key, lease_owner, lease_expires_at, task_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run("test", "lease-key", "agent-1", 1705401000000, null, 1705228200000, 1705228200000);

    const result = migration.migrateFromLivingBrainDb(sourceDb, targetDb);

    expect(result.entryMeta).toBe(2);
    expect(result.generatedTasks).toBe(1);
    expect(result.errors).toEqual([]);
    expect(result.duration).toBeGreaterThanOrEqual(0);
    expect(result.strategy).toBe("import");
  });

  test("returns result with zero counts for empty source", () => {
    const result = migration.migrateFromLivingBrainDb(sourceDb, targetDb);

    expect(result.entryMeta).toBe(0);
    expect(result.generatedTasks).toBe(0);
    expect(result.errors).toEqual([]);
    expect(result.strategy).toBe("import");
  });

  test("captures errors without throwing", () => {
    // Close source DB to force an error
    sourceDb.close();

    const result = migration.migrateFromLivingBrainDb(sourceDb, targetDb);

    expect(result.errors.length).toBeGreaterThan(0);
    expect(result.entryMeta).toBe(0);
    expect(result.generatedTasks).toBe(0);
  });
});

// =============================================================================
// MigrationResult interface shape
// =============================================================================

describe("MigrationResult interface", () => {
  test("has all required fields", () => {
    const result: MigrationResult = {
      strategy: "import",
      notes: 0,
      links: 0,
      tags: 0,
      entryMeta: 0,
      generatedTasks: 0,
      errors: [],
      duration: 0,
    };

    expect(result.strategy).toBe("import");
    expect(result.notes).toBe(0);
    expect(result.links).toBe(0);
    expect(result.tags).toBe(0);
    expect(result.entryMeta).toBe(0);
    expect(result.generatedTasks).toBe(0);
    expect(result.errors).toEqual([]);
    expect(result.duration).toBe(0);
  });

  test("strategy can be 'rebuild'", () => {
    const result: MigrationResult = {
      strategy: "rebuild",
      notes: 10,
      links: 5,
      tags: 3,
      entryMeta: 2,
      generatedTasks: 1,
      errors: ["some error"],
      duration: 150,
    };

    expect(result.strategy).toBe("rebuild");
    expect(result.errors).toEqual(["some error"]);
  });
});
