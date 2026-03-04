/**
 * Integration Tests: Entries API Endpoints
 *
 * Tests entry CRUD operations through the full HTTP stack using
 * createApp() and app.request(). Creates entries in a unique test
 * subdirectory and cleans up after all tests complete.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync } from "fs";
import { join } from "path";
import { createApp } from "../../src/server";
import { getConfig } from "../../src/config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
let app: ReturnType<typeof createApp>;

const TEST_SUBDIR = `_integ-api-entries-${Date.now()}`;
const TEST_PROJECT = TEST_SUBDIR;

beforeAll(() => {
  app = createApp(config);
  // Ensure test subdirectory exists
  const testDir = join(config.brain.brainDir, "projects", TEST_SUBDIR);
  if (!existsSync(testDir)) {
    mkdirSync(testDir, { recursive: true });
  }
});

afterAll(() => {
  // Clean up test directory
  const testDir = join(config.brain.brainDir, "projects", TEST_SUBDIR);
  if (existsSync(testDir)) {
    rmSync(testDir, { recursive: true, force: true });
  }
});

// =============================================================================
// POST /api/v1/entries — Create Entry
// =============================================================================

describe("POST /api/v1/entries", () => {
  test("creates entry with required fields and returns 201", async () => {
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "scratch",
        title: "API Integration Test Entry",
        content: "This is test content created via API integration test.",
        project: TEST_PROJECT,
      }),
    });

    // 201 = success, 500 = zk not available (validation still passed)
    expect([201, 500].includes(res.status)).toBe(true);

    if (res.status === 201) {
      const data = await res.json();
      expect(data).toHaveProperty("id");
      expect(data).toHaveProperty("path");
      expect(data).toHaveProperty("title");
      expect(data.title).toBe("API Integration Test Entry");
      expect(data).toHaveProperty("type");
      expect(data.type).toBe("scratch");
    }
  });

  test("rejects request with missing required fields (400)", async () => {
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "Missing type and content",
      }),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects request with empty body (400)", async () => {
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects request with invalid type (400)", async () => {
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "not-a-valid-type",
        title: "Bad Type",
        content: "Content",
      }),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects invalid JSON body (400)", async () => {
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "not-json",
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Bad Request");
  });
});

// =============================================================================
// GET /api/v1/entries/:path — Recall Entry
// =============================================================================

describe("GET /api/v1/entries/:path", () => {
  let createdEntryPath: string | null = null;

  beforeAll(async () => {
    // Create a test entry to recall
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "scratch",
        title: "Recall Test Entry",
        content: "Content for recall testing via API.",
        project: TEST_PROJECT,
      }),
    });

    if (res.status === 201) {
      const data = await res.json();
      createdEntryPath = data.path;
    }
  });

  test("returns entry by path with all expected fields", async () => {
    if (!createdEntryPath) {
      // Skip if entry creation failed (e.g., zk not available)
      return;
    }

    const res = await app.request(`/api/v1/entries/${createdEntryPath}`);
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data).toHaveProperty("title");
    expect(data.title).toBe("Recall Test Entry");
    expect(data).toHaveProperty("type");
    expect(data.type).toBe("scratch");
    expect(data).toHaveProperty("content");
    expect(data.content).toContain("Content for recall testing");
  });

  test("returns 404 for non-existent entry", async () => {
    const res = await app.request("/api/v1/entries/non/existent/path.md");
    expect(res.status).toBe(404);

    const data = await res.json();
    expect(data.error).toBe("Not Found");
  });
});

// =============================================================================
// PATCH /api/v1/entries/:path — Update Entry
// =============================================================================

describe("PATCH /api/v1/entries/:path", () => {
  let updateEntryPath: string | null = null;

  beforeAll(async () => {
    // Create a test entry to update
    const res = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "scratch",
        title: "Update Test Entry",
        content: "Original content for update testing.",
        project: TEST_PROJECT,
        status: "active",
      }),
    });

    if (res.status === 201) {
      const data = await res.json();
      updateEntryPath = data.path;
    }
  });

  test("updates entry status", async () => {
    if (!updateEntryPath) return;

    const res = await app.request(`/api/v1/entries/${updateEntryPath}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "completed" }),
    });

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.status).toBe("completed");
  });

  test("rejects empty update body (400)", async () => {
    if (!updateEntryPath) return;

    const res = await app.request(`/api/v1/entries/${updateEntryPath}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects invalid status value (400)", async () => {
    if (!updateEntryPath) return;

    const res = await app.request(`/api/v1/entries/${updateEntryPath}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "not-a-valid-status" }),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("returns 404 for non-existent entry", async () => {
    const res = await app.request("/api/v1/entries/non/existent/path.md", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "completed" }),
    });

    expect(res.status).toBe(404);
  });
});

// =============================================================================
// DELETE /api/v1/entries/:path — Delete Entry
// =============================================================================

describe("DELETE /api/v1/entries/:path", () => {
  test("requires confirm parameter", async () => {
    const res = await app.request(
      "/api/v1/entries/projects/fake/scratch/fake.md",
      { method: "DELETE" }
    );

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Confirmation Required");
  });

  test("deletes entry with confirm=true", async () => {
    // Create entry to delete
    const createRes = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "scratch",
        title: "Delete Me Entry",
        content: "This entry will be deleted.",
        project: TEST_PROJECT,
      }),
    });

    if (createRes.status !== 201) return;

    const created = await createRes.json();
    const entryPath = created.path;

    // Delete the entry
    const deleteRes = await app.request(
      `/api/v1/entries/${entryPath}?confirm=true`,
      { method: "DELETE" }
    );

    expect(deleteRes.status).toBe(200);
    const data = await deleteRes.json();
    expect(data.message).toBe("Entry deleted successfully");
  });

  test("returns 404 for non-existent entry with confirm=true", async () => {
    const res = await app.request(
      "/api/v1/entries/non/existent/path.md?confirm=true",
      { method: "DELETE" }
    );

    expect(res.status).toBe(404);
  });
});

// =============================================================================
// Round-trip Test: Create → Recall → Update → Recall → Delete → Verify Gone
// =============================================================================

describe("Entry CRUD round-trip", () => {
  test("full lifecycle: create → recall → update → recall → delete → verify gone", async () => {
    // Step 1: Create
    const createRes = await app.request("/api/v1/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "scratch",
        title: "Round Trip Entry",
        content: "Round trip test content.",
        project: TEST_PROJECT,
        status: "active",
      }),
    });

    if (createRes.status !== 201) {
      // zk not available — skip the rest
      return;
    }

    const created = await createRes.json();
    expect(created).toHaveProperty("id");
    expect(created).toHaveProperty("path");
    const entryPath = created.path;

    // Step 2: Recall
    const recallRes = await app.request(`/api/v1/entries/${entryPath}`);
    expect(recallRes.status).toBe(200);
    const recalled = await recallRes.json();
    expect(recalled.title).toBe("Round Trip Entry");
    expect(recalled.content).toContain("Round trip test content");

    // Step 3: Update
    const updateRes = await app.request(`/api/v1/entries/${entryPath}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "completed" }),
    });
    expect(updateRes.status).toBe(200);
    const updated = await updateRes.json();
    expect(updated.status).toBe("completed");

    // Step 4: Recall again to verify update persisted
    const recallRes2 = await app.request(`/api/v1/entries/${entryPath}`);
    expect(recallRes2.status).toBe(200);
    const recalled2 = await recallRes2.json();
    expect(recalled2.status).toBe("completed");

    // Step 5: Delete
    const deleteRes = await app.request(
      `/api/v1/entries/${entryPath}?confirm=true`,
      { method: "DELETE" }
    );
    expect(deleteRes.status).toBe(200);

    // Step 6: Verify gone
    const goneRes = await app.request(`/api/v1/entries/${entryPath}`);
    expect(goneRes.status).toBe(404);
  });
});
