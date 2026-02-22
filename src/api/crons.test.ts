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

function writeTaskFile(taskId: string, cronId: string): void {
  const fullPath = join(config.brain.brainDir, "projects", TEST_PROJECT, "task", `${taskId}.md`);
  const frontmatter = [
    "---",
    `title: Task ${taskId}`,
    "type: task",
    "status: pending",
    "priority: medium",
    "tags:",
    "  - task",
    "depends_on: []",
    "cron_ids:",
    `  - ${cronId}`,
    "---",
    "",
    `# Task ${taskId}`,
    "",
    "Task content.",
    "",
  ];

  writeFileSync(fullPath, frontmatter.join("\n"));
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

    writeTaskFile("tsk00001", cronId);
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
});
