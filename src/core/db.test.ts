import { afterAll, beforeAll, beforeEach, describe, expect, test } from "bun:test";
import { existsSync, rmSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { closeDatabase, initDatabase } from "./db";
import * as dbModule from "./db";

const TEST_DIR = join(tmpdir(), `brain-db-test-${Date.now()}`);

describe("db generated task coordination primitives", () => {
  beforeAll(() => {
    process.env.BRAIN_DIR = TEST_DIR;
    closeDatabase();
  });

  afterAll(() => {
    closeDatabase();
    if (existsSync(TEST_DIR)) {
      rmSync(TEST_DIR, { recursive: true, force: true });
    }
  });

  beforeEach(() => {
    const db = initDatabase();
    db.run("DELETE FROM generated_task_keys");
  });

  test("initializes generated_task_keys table", () => {
    const db = initDatabase();

    const table = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'generated_task_keys'"
      )
      .get() as { name: string } | undefined;

    expect(table?.name).toBe("generated_task_keys");

    const columns = db
      .prepare("PRAGMA table_info(generated_task_keys)")
      .all() as Array<{ name: string }>;

    expect(columns.map((column) => column.name)).toEqual(
      expect.arrayContaining([
        "project_id",
        "generated_key",
        "lease_owner",
        "lease_expires_at",
        "task_path",
        "created_at",
        "updated_at",
      ])
    );
  });

  test("acquires lease for a new generated key", () => {
    const acquireGeneratedTaskLease = (dbModule as Record<string, unknown>)
      .acquireGeneratedTaskLease as
      | ((
          projectId: string,
          generatedKey: string,
          leaseOwner: string,
          leaseDurationMs: number
        ) => { status: string })
      | undefined;

    expect(typeof acquireGeneratedTaskLease).toBe("function");
    if (!acquireGeneratedTaskLease) return;

    const result = acquireGeneratedTaskLease(
      "test-project",
      "feature-checkout:test:round-1",
      "runner-a",
      60_000
    );

    expect(result.status).toBe("acquired");

    const row = initDatabase()
      .prepare(
        "SELECT lease_owner, task_path FROM generated_task_keys WHERE project_id = ? AND generated_key = ?"
      )
      .get("test-project", "feature-checkout:test:round-1") as
      | { lease_owner: string | null; task_path: string | null }
      | undefined;

    expect(row?.lease_owner).toBe("runner-a");
    expect(row?.task_path).toBeNull();
  });

  test("returns existing task path once key is finalized", () => {
    const acquireGeneratedTaskLease = (dbModule as Record<string, unknown>)
      .acquireGeneratedTaskLease as
      | ((
          projectId: string,
          generatedKey: string,
          leaseOwner: string,
          leaseDurationMs: number
        ) => { status: string; taskPath?: string })
      | undefined;
    const completeGeneratedTaskLease = (dbModule as Record<string, unknown>)
      .completeGeneratedTaskLease as
      | ((
          projectId: string,
          generatedKey: string,
          leaseOwner: string,
          taskPath: string
        ) => boolean)
      | undefined;

    expect(typeof acquireGeneratedTaskLease).toBe("function");
    expect(typeof completeGeneratedTaskLease).toBe("function");
    if (!acquireGeneratedTaskLease || !completeGeneratedTaskLease) return;

    const first = acquireGeneratedTaskLease(
      "test-project",
      "feature-checkout:test:round-2",
      "runner-a",
      60_000
    );
    expect(first.status).toBe("acquired");

    const completed = completeGeneratedTaskLease(
      "test-project",
      "feature-checkout:test:round-2",
      "runner-a",
      "projects/test-project/task/abc12345.md"
    );
    expect(completed).toBe(true);

    const second = acquireGeneratedTaskLease(
      "test-project",
      "feature-checkout:test:round-2",
      "runner-b",
      60_000
    );

    expect(second.status).toBe("exists");
    expect(second.taskPath).toBe("projects/test-project/task/abc12345.md");
  });

  test("rejects concurrent lease acquisition while active", () => {
    const acquireGeneratedTaskLease = (dbModule as Record<string, unknown>)
      .acquireGeneratedTaskLease as
      | ((
          projectId: string,
          generatedKey: string,
          leaseOwner: string,
          leaseDurationMs: number
        ) => { status: string })
      | undefined;

    expect(typeof acquireGeneratedTaskLease).toBe("function");
    if (!acquireGeneratedTaskLease) return;

    const first = acquireGeneratedTaskLease(
      "test-project",
      "feature-checkout:test:round-3",
      "runner-a",
      60_000
    );
    const second = acquireGeneratedTaskLease(
      "test-project",
      "feature-checkout:test:round-3",
      "runner-b",
      60_000
    );

    expect(first.status).toBe("acquired");
    expect(second.status).toBe("busy");
  });
});
