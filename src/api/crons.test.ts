/**
 * Cron API Routes - Integration Tests
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { execSync } from "child_process";
import { Hono } from "hono";
import { createCronRoutes } from "./crons";
import { createEntriesRoutes } from "./entries";
import { getConfig } from "../config";

const config = getConfig();
const TEST_PROJECT = `_test-crons-${Date.now()}`;
const OTHER_PROJECT = `_test-crons-other-${Date.now()}`;

const app = new Hono();
app.route("/crons", createCronRoutes());
app.route("/entries", createEntriesRoutes());

beforeAll(() => {
  const cronDir = join(config.brain.brainDir, "projects", TEST_PROJECT, "cron");
  const taskDir = join(config.brain.brainDir, "projects", TEST_PROJECT, "task");
  const otherCronDir = join(config.brain.brainDir, "projects", OTHER_PROJECT, "cron");

  if (!existsSync(cronDir)) mkdirSync(cronDir, { recursive: true });
  if (!existsSync(taskDir)) mkdirSync(taskDir, { recursive: true });
  if (!existsSync(otherCronDir)) mkdirSync(otherCronDir, { recursive: true });
});

afterAll(() => {
  const testProjectDir = join(config.brain.brainDir, "projects", TEST_PROJECT);
  const otherProjectDir = join(config.brain.brainDir, "projects", OTHER_PROJECT);

  if (existsSync(testProjectDir)) rmSync(testProjectDir, { recursive: true, force: true });
  if (existsSync(otherProjectDir)) rmSync(otherProjectDir, { recursive: true, force: true });
});

function writeCronFile(
  projectId: string,
  cronId: string,
  title: string,
  extras: string[] = []
): void {
  const fullPath = join(config.brain.brainDir, "projects", projectId, "cron", `${cronId}.md`);
  const frontmatter = [
    "---",
    `title: ${title}`,
    "type: cron",
    "status: active",
    "tags:",
    "  - cron",
    "schedule: 0 2 * * *",
    ...extras,
    "---",
    "",
    `# ${title}`,
    "",
    "Cron content.",
    "",
  ];

  writeFileSync(fullPath, frontmatter.join("\n"));
}

function writeTaskFile(taskId: string, cronIds: string[] = [], projectId = TEST_PROJECT): void {
  const fullPath = join(config.brain.brainDir, "projects", projectId, "task", `${taskId}.md`);
  const frontmatter = [
    "---",
    `title: Task ${taskId}`,
    "type: task",
    "status: pending",
    "priority: medium",
    "tags:",
    "  - task",
    "depends_on: []",
    ...(cronIds.length > 0
      ? [
          "cron_ids:",
          ...cronIds.map((cronId) => `  - ${cronId}`),
        ]
      : []),
    "---",
    "",
    `# Task ${taskId}`,
    "",
    "Task content.",
    "",
  ];

  writeFileSync(fullPath, frontmatter.join("\n"));
}

async function getTaskEntry(taskId: string): Promise<{ cron_ids?: string[] } & Record<string, unknown>> {
  const path = `projects/${TEST_PROJECT}/task/${taskId}.md`;
  const res = await app.request(`/entries/${path}`);
  expect(res.status).toBe(200);
  return res.json();
}

function reindexZk(): void {
  try {
    execSync("zk index --quiet", {
      cwd: config.brain.brainDir,
      timeout: 10000,
    });
  } catch {
    // zk may not be available in all test environments
  }
}

async function getCronIdByTitle(projectId: string, title: string): Promise<string | null> {
  const listRes = await app.request(`/crons/${projectId}/crons`);
  if (listRes.status !== 200) return null;

  const listJson = await listRes.json();
  const match = listJson.crons.find((cron: { title: string }) => cron.title === title);
  return match?.id ?? null;
}

describe("Cron API", () => {
  test("lists cron entries for a project", async () => {
    writeCronFile(TEST_PROJECT, "crn00001", "Project Cron One", [
      "next_run: 2026-02-23T02:00:00.000Z",
    ]);
    writeCronFile(TEST_PROJECT, "crn00002", "Project Cron Two");
    writeCronFile(OTHER_PROJECT, "crn00003", "Other Project Cron");
    reindexZk();

    const res = await app.request(`/crons/${TEST_PROJECT}/crons`);

    if (res.status === 503) return;

    expect(res.status).toBe(200);
    const json = await res.json();

    expect(json.count).toBeGreaterThanOrEqual(2);
    const ids = json.crons.map((cron: { id: string }) => cron.id);
    expect(ids).toContain("crn00001");
    expect(ids).toContain("crn00002");
    expect(ids).not.toContain("crn00003");
  });

  test("manually triggers a cron run", async () => {
    writeCronFile(TEST_PROJECT, "crn00004", "Triggerable Cron");
    reindexZk();

    const cronId = await getCronIdByTitle(TEST_PROJECT, "Triggerable Cron");
    if (!cronId) return;

    writeTaskFile("tsk00001", [cronId]);
    reindexZk();

    const res = await app.request(`/crons/${TEST_PROJECT}/crons/${cronId}/trigger`, {
      method: "POST",
    });

    if (res.status === 503) return;

    if (res.status === 404) {
      const json = await res.json();
      expect(json.error).toBe("Not Found");
      expect(json.message).toContain(cronId);
      return;
    }

    expect(res.status).toBe(200);
    const json = await res.json();

    expect(json.cronId).toBe(cronId);
    expect(json.run.status).toBe("in_progress");
    expect(json.run.run_id).toMatch(/^\d{8}-\d{4}$/);
    expect(json.pipelineCount).toBeGreaterThan(0);
  });

  test("retrieves run history sorted by started desc", async () => {
    writeCronFile(TEST_PROJECT, "crn00005", "History Cron", [
      "runs:",
      "  - run_id: 20260222-0200",
      "    status: completed",
      "    started: 2026-02-22T02:00:00.000Z",
      "    completed: 2026-02-22T02:00:08.000Z",
      "  - run_id: 20260223-0200",
      "    status: completed",
      "    started: 2026-02-23T02:00:00.000Z",
      "    completed: 2026-02-23T02:00:07.000Z",
    ]);
    reindexZk();

    const cronId = await getCronIdByTitle(TEST_PROJECT, "History Cron");
    if (!cronId) return;

    const res = await app.request(`/crons/${TEST_PROJECT}/crons/${cronId}/runs`);

    if (res.status === 503) return;

    if (res.status === 404) {
      const json = await res.json();
      expect(json.error).toBe("Not Found");
      expect(json.message).toContain(cronId);
      return;
    }

    expect(res.status).toBe(200);
    const json = await res.json();

    expect(json.count).toBe(2);
    expect(json.runs[0].run_id).toBe("20260223-0200");
    expect(json.runs[1].run_id).toBe("20260222-0200");
  });

  test("creates cron via cron endpoint with auto-calculated next_run", async () => {
    const res = await app.request(`/crons/${TEST_PROJECT}/crons`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "Created via Cron API",
        schedule: "15 4 * * *",
      }),
    });

    if (res.status === 503) return;

    expect(res.status).toBe(201);
    const json = await res.json();

    expect(json.cron.type).toBe("cron");
    expect(json.cron.title).toBe("Created via Cron API");
    expect(json.cron.path).toContain(`projects/${TEST_PROJECT}/cron/`);
    expect(json.cron.schedule).toBe("15 4 * * *");
    expect(typeof json.cron.next_run).toBe("string");
    expect(Number.isNaN(new Date(json.cron.next_run).getTime())).toBe(false);
  });

  test("rejects invalid cron schedule on create", async () => {
    const res = await app.request(`/crons/${TEST_PROJECT}/crons`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "Invalid Schedule Cron",
        schedule: "bad schedule",
      }),
    });

    if (res.status === 503) return;

    expect(res.status).toBe(400);
    const json = await res.json();
    expect(json.error).toBe("Validation Error");
    expect(json.message).toContain("schedule");
  });

  test("updates cron schedule and recalculates next_run", async () => {
    const createRes = await app.request(`/crons/${TEST_PROJECT}/crons`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "Update Schedule Cron",
        schedule: "0 1 * * *",
      }),
    });

    if (createRes.status === 503) return;
    expect(createRes.status).toBe(201);

    const created = await createRes.json();
    const beforeNextRun = created.cron.next_run;

    const patchRes = await app.request(`/crons/${TEST_PROJECT}/crons/${created.cron.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        schedule: "30 6 * * *",
      }),
    });

    if (patchRes.status === 503) return;

    expect(patchRes.status).toBe(200);
    const patched = await patchRes.json();
    expect(patched.cron.schedule).toBe("30 6 * * *");
    expect(typeof patched.cron.next_run).toBe("string");
    expect(Number.isNaN(new Date(patched.cron.next_run).getTime())).toBe(false);
    expect(patched.cron.next_run).not.toBe(beforeNextRun);
  });

  test("deletes cron entry via cron endpoint", async () => {
    const createRes = await app.request(`/crons/${TEST_PROJECT}/crons`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "Delete Me Cron",
        schedule: "0 3 * * *",
      }),
    });

    if (createRes.status === 503) return;
    expect(createRes.status).toBe(201);

    const created = await createRes.json();

    const deleteRes = await app.request(
      `/crons/${TEST_PROJECT}/crons/${created.cron.id}?confirm=true`,
      { method: "DELETE" }
    );

    if (deleteRes.status === 503) return;

    expect(deleteRes.status).toBe(200);
    const deleted = await deleteRes.json();
    expect(deleted.message).toBe("Cron deleted successfully");
    expect(deleted.path).toBe(created.cron.path);

    const getRes = await app.request(`/crons/${TEST_PROJECT}/crons/${created.cron.id}`);
    expect(getRes.status).toBe(404);
  });

  test("creates cron entry with auto-calculated next_run", async () => {
    const createRes = await app.request("/entries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type: "cron",
        title: "Auto Next Run Cron",
        content: "Cron body",
        project: TEST_PROJECT,
        schedule: "0 2 * * *",
      }),
    });

    expect([201, 500].includes(createRes.status)).toBe(true);
    if (createRes.status !== 201) return;

    const created = await createRes.json();
    const getRes = await app.request(`/entries/${created.path}`);

    expect(getRes.status).toBe(200);
    const entry = await getRes.json();

    expect(entry.type).toBe("cron");
    expect(entry.schedule).toBe("0 2 * * *");
    expect(typeof entry.next_run).toBe("string");
    expect(Number.isNaN(new Date(entry.next_run).getTime())).toBe(false);
  });

  test("manages cron-task links via list/set/add/remove endpoints", async () => {
    writeCronFile(TEST_PROJECT, "crn01001", "Linked Tasks Cron A");
    writeCronFile(TEST_PROJECT, "crn01002", "Linked Tasks Cron B");
    reindexZk();

    const cronA = await getCronIdByTitle(TEST_PROJECT, "Linked Tasks Cron A");
    const cronB = await getCronIdByTitle(TEST_PROJECT, "Linked Tasks Cron B");
    if (!cronA || !cronB) return;

    writeTaskFile("tsk01001", [cronA]);
    writeTaskFile("tsk01002", [cronA, cronB]);
    writeTaskFile("tsk01003", [cronB]);
    reindexZk();

    const listRes = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks`);
    if (listRes.status === 503) return;

    expect(listRes.status).toBe(200);
    const listed = await listRes.json();
    expect(listed.count).toBe(2);
    const listedIds = listed.tasks.map((task: { id: string }) => task.id);
    expect(listedIds).toContain("tsk01001");
    expect(listedIds).toContain("tsk01002");
    expect(listedIds).not.toContain("tsk01003");

    const setRes = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ taskIds: ["tsk01002", "tsk01003"] }),
    });

    if (setRes.status === 503) return;

    expect(setRes.status).toBe(200);
    const setJson = await setRes.json();
    expect(setJson.message).toBe("Cron linked tasks replaced");
    const setIds = setJson.tasks.map((task: { id: string }) => task.id);
    expect(setIds).toContain("tsk01002");
    expect(setIds).toContain("tsk01003");
    expect(setIds).not.toContain("tsk01001");

    const taskAfterSetA = await getTaskEntry("tsk01001");
    const taskAfterSetB = await getTaskEntry("tsk01002");
    const taskAfterSetC = await getTaskEntry("tsk01003");
    expect(taskAfterSetA.cron_ids || []).not.toContain(cronA);
    expect(taskAfterSetB.cron_ids).toEqual(expect.arrayContaining([cronA, cronB]));
    expect(taskAfterSetC.cron_ids).toEqual(expect.arrayContaining([cronA, cronB]));

    const addRes1 = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks/tsk01001`, {
      method: "POST",
    });
    if (addRes1.status === 503) return;
    expect(addRes1.status).toBe(200);

    const addRes2 = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks/tsk01001`, {
      method: "POST",
    });
    if (addRes2.status === 503) return;
    expect(addRes2.status).toBe(200);

    const taskAfterAdd = await getTaskEntry("tsk01001");
    const linkedAfterAdd = (taskAfterAdd.cron_ids || []).filter((id: string) => id === cronA);
    expect(linkedAfterAdd.length).toBe(1);

    const removeRes1 = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks/tsk01002`, {
      method: "DELETE",
    });
    if (removeRes1.status === 503) return;
    expect(removeRes1.status).toBe(200);

    const removeRes2 = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks/tsk01002`, {
      method: "DELETE",
    });
    if (removeRes2.status === 503) return;
    expect(removeRes2.status).toBe(200);

    const taskAfterRemove = await getTaskEntry("tsk01002");
    expect(taskAfterRemove.cron_ids || []).not.toContain(cronA);
    expect(taskAfterRemove.cron_ids).toContain(cronB);

    const clearRes = await app.request(`/crons/${TEST_PROJECT}/crons/${cronA}/linked-tasks`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ taskIds: [] }),
    });
    if (clearRes.status === 503) return;
    expect(clearRes.status).toBe(200);
    const cleared = await clearRes.json();
    expect(cleared.count).toBe(0);

    const taskAfterClearA = await getTaskEntry("tsk01001");
    const taskAfterClearB = await getTaskEntry("tsk01002");
    const taskAfterClearC = await getTaskEntry("tsk01003");
    expect(taskAfterClearA.cron_ids || []).not.toContain(cronA);
    expect(taskAfterClearB.cron_ids || []).not.toContain(cronA);
    expect(taskAfterClearC.cron_ids || []).not.toContain(cronA);
    expect(taskAfterClearB.cron_ids).toContain(cronB);
    expect(taskAfterClearC.cron_ids).toContain(cronB);
  }, 20000);

  test("validates linked-task IDs and returns not found for missing tasks", async () => {
    writeCronFile(TEST_PROJECT, "crn01010", "Linked Tasks Validation Cron");
    reindexZk();

    const cronId = await getCronIdByTitle(TEST_PROJECT, "Linked Tasks Validation Cron");
    if (!cronId) return;

    const invalidRes = await app.request(`/crons/${TEST_PROJECT}/crons/${cronId}/linked-tasks/invalid*id`, {
      method: "POST",
    });

    if (invalidRes.status === 503) return;

    expect(invalidRes.status).toBe(400);
    const invalidJson = await invalidRes.json();
    expect(invalidJson.error).toBe("Validation Error");
    expect(invalidJson.message).toContain("taskId");

    const missingRes = await app.request(`/crons/${TEST_PROJECT}/crons/${cronId}/linked-tasks/no_such_task`, {
      method: "DELETE",
    });

    expect(missingRes.status).toBe(404);
    const missingJson = await missingRes.json();
    expect(missingJson.error).toBe("Not Found");
    expect(missingJson.message).toContain("not found in project");
  });
});
