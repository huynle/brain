// Run against a local worktree deployment: npm run test:sync-mcp.
// Seeds a draft automation in a unique demo project; never starts a runner.
import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
const origin = process.env.BRAIN_SYNC_TEST_URL ?? "http://127.0.0.1:3333";
assert.ok(
  ["localhost", "127.0.0.1"].includes(new URL(origin).hostname),
  "Local development only",
);
const project = "mcp-sync-demo-" + Date.now();
const evidence = join(tmpdir(), project);
await mkdir(evidence);
const results = [];
const pass = (s) => {
  results.push(s);
  console.log("PASS", s);
};
async function request(path, body, method = body ? "POST" : "GET") {
  const r = await fetch(origin + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  assert.ok(r.ok, `${r.status}: ${await r.clone().text()}`);
  return r.json();
}
let rpcID = 0;
async function mcp(name, args = {}) {
  const r = await request("/mcp", {
    jsonrpc: "2.0",
    id: ++rpcID,
    method: "tools/call",
    params: { name, arguments: args },
  });
  assert.ok(!r.error, JSON.stringify(r));
  assert.ok(!r.result.isError, JSON.stringify(r));
  return JSON.parse(r.result.content.map((c) => c.text ?? "").join(""));
}
const seed = await request("/api/v1/entries", {
  project,
  type: "automation",
  status: "draft",
  title: project,
  content: "Original body",
  action: { type: "script", command: "echo original" },
});
const browser = await chromium.launch({ headless: process.env.HEADED !== "1" });
const context = await browser.newContext();
const page = await context.newPage();
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
let device;
async function status() {
  return (await mcp("sync_status")).devices.find((d) => d.device_id === device);
}
async function open() {
  await page.getByRole("button", { name: /Offline sync ·/ }).click();
  await page.getByLabel("Find entry", { exact: true }).fill(project);
  await page.getByLabel("Entry", { exact: true }).selectOption(seed.path);
  await expect(page.getByLabel("Entry draft")).toContainText(project);
}
async function conflict(local, server) {
  await context.setOffline(true);
  const editor = page.getByLabel("Entry draft");
  const raw = await editor.inputValue();
  await editor.fill(raw.replace(/echo [^\n]+/, "echo " + local));
  await page.getByRole("button", { name: "Save locally", exact: true }).click();
  await expect(
    page.getByText(
      "Saved on this device; awaiting server validation and sync.",
      { exact: true },
    ),
  ).toBeVisible();
  await request("/api/v1/entries/" + seed.path, { content: server }, "PATCH");
  await page.reload();
  await open();
  await expect(page.getByLabel("Entry draft")).toContainText("echo " + local);
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect
    .poll(async () => (await status())?.pending[0]?.failure, { timeout: 30000 })
    .toBe("conflict");
  return (await status()).pending[0];
}
try {
  await page.goto(origin + "/?entry=" + encodeURIComponent(seed.path));
  await expect(page.locator(".entry-reader")).toContainText(project);
  await expect(
    page.getByRole("button", {
      name: /^Offline sync · Ready · \d+ cached$/,
      exact: true,
    }),
  ).toBeVisible({ timeout: 30000 });
  await page.evaluate(() => navigator.serviceWorker.ready);
  await open();
  await expect
    .poll(
      async () => {
        const ds = (await mcp("sync_status")).devices;
        const candidates = ds.filter(
          (d) => d.connection === "online" && d.ready,
        );
        // This fresh context is the most recent reporter; pin identity using its first pending edit below.
        return candidates.length;
      },
      { timeout: 30000 },
    )
    .toBeGreaterThan(0);
  const reported = (await mcp("sync_status")).devices.find(
    (d) => d.connection === "online" && d.cache_mode === "recent",
  );
  assert.ok(
    reported && reported.cached_entries >= 1,
    "MCP must describe the selective working set",
  );
  await context.setOffline(true);
  const raw = await page.getByLabel("Entry draft").inputValue();
  await page
    .getByLabel("Entry draft")
    .fill(raw.replace("echo original", "echo offline"));
  await page.getByRole("button", { name: "Save locally", exact: true }).click();
  await expect(
    page.getByText(
      "Saved on this device; awaiting server validation and sync.",
      { exact: true },
    ),
  ).toBeVisible();
  await request(
    "/api/v1/entries/" + seed.path,
    { content: "Server concurrent body" },
    "PATCH",
  );
  await page.reload();
  await open();
  await expect(page.getByLabel("Entry draft")).toContainText("echo offline");
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect
    .poll(
      async () => {
        const d = (await mcp("sync_status")).devices.find((d) =>
          d.pending.some((p) => p.path === seed.path),
        );
        device = d?.device_id;
        return d?.pending[0]?.failure;
      },
      { timeout: 30000 },
    )
    .toBe("conflict");
  pass(
    "Real MCP sees connected browser, sync cursor, and reported offline automation conflict",
  );
  const op = (await status()).pending[0];
  const diff = await mcp("sync_diff", {
    device_id: device,
    operation_id: op.id,
  });
  assert.ok(diff.diff.includes("echo offline"));
  assert.ok(diff.server_raw.includes("Server concurrent body"));
  assert.equal(diff.operation.revision === diff.server_revision, false);
  pass(
    "MCP diff returns both full definitions and a revision-bound review token",
  );
  await context.setOffline(true);
  await expect
    .poll(async () => (await status()).connection, {
      timeout: 45000,
      intervals: [5000],
    })
    .toBe("unknown");
  pass(
    "Disconnected browser becomes unknown after report expiry, retaining last-known conflict",
  );
  const merged = diff.operation.raw.replace(
    "Original body",
    "Server concurrent body\nMerged by MCP",
  );
  const queued = await mcp("sync_reconcile", {
    device_id: device,
    operation_id: op.id,
    snapshot: diff.snapshot,
    action: "merge",
    raw: merged,
  });
  assert.equal(queued.status, "queued");
  assert.equal(
    (await request("/api/v1/entries/" + seed.path)).action.command,
    "echo original",
  );
  pass(
    "Agent merge queues while browser offline without prematurely changing server content",
  );
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect
    .poll(
      async () => {
        const s = await status();
        return [s.command?.outcome, s.pending.length];
      },
      { timeout: 30000 },
    )
    .toEqual(["applied_locally", 0]);
  const saved = await request("/api/v1/entries/" + seed.path);
  assert.equal(saved.action.command, "echo offline");
  assert.ok(saved.content.includes("Merged by MCP"));
  assert.equal(saved.status, "draft");
  pass(
    "Reconnect applies MCP merge, acknowledges command, and syncs the merged definition",
  );
  await page.reload();
  await open();
  await expect(page.getByLabel("Entry draft")).toContainText("Merged by MCP");
  const second = await conflict("second", "Second server edit");
  const d2 = await mcp("sync_diff", {
    device_id: device,
    operation_id: second.id,
  });
  await context.setOffline(true);
  await mcp("sync_reconcile", {
    device_id: device,
    operation_id: second.id,
    snapshot: d2.snapshot,
    action: "rebase",
  });
  await request(
    "/api/v1/entries/" + seed.path,
    { content: "Newer after review" },
    "PATCH",
  );
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect
    .poll(async () => (await status()).command?.outcome, { timeout: 30000 })
    .toBe("stale");
  assert.equal(
    (await request("/api/v1/entries/" + seed.path)).content,
    "Newer after review",
  );
  assert.equal((await status()).pending.length, 1);
  pass(
    "Browser refuses queued resolution when server changes after agent review",
  );
  const fresh = await mcp("sync_diff", {
    device_id: device,
    operation_id: second.id,
  });
  await mcp("sync_reconcile", {
    device_id: device,
    operation_id: second.id,
    snapshot: fresh.snapshot,
    action: "discard",
  });
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect
    .poll(async () => (await status()).pending.length, { timeout: 30000 })
    .toBe(0);
  assert.equal(
    (await request("/api/v1/entries/" + seed.path)).content,
    "Newer after review",
  );
  pass(
    "MCP discard clears only the reviewed local draft, retaining newer server data",
  );
  await page.reload();
  await open();
  await expect(page.getByLabel("Entry draft")).toContainText(
    "Newer after review",
  );
  assert.deepEqual(errors, []);
  await page.screenshot({ path: join(evidence, "verified.png") });
  await writeFile(
    join(evidence, "results.json"),
    JSON.stringify(
      {
        origin,
        project,
        device,
        entry: seed.path,
        results,
        status: await status(),
      },
      null,
      2,
    ),
  );
  console.log("Evidence:", evidence);
} finally {
  await browser.close();
}
