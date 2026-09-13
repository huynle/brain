import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
const origin = process.env.BRAIN_READER_TEST_URL ?? "http://localhost:3335";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const project = "startup-" + Date.now();
async function create(type, i) {
  const r = await fetch(origin + "/api/v1/entries", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      type,
      project,
      title: `Startup ${type} ${i}`,
      status: type === "task" ? "pending" : "active",
      content:
        "Cached startup verification.\n" +
        "A substantial note body. ".repeat(400),
    }),
  });
  assert.ok(r.ok, await r.clone().text());
  return r.json();
}
const notes = [];
for (let i = 0; i < 60; i++)
  notes.push(await create(i < 10 ? "task" : "scratch", i));
const b = await chromium.launch();
const ctx = await b.newContext({ serviceWorkers: "block" });
await ctx.addInitScript(() => {
  window.dbCalls = [];
  window.cacheReady = false;
  const Original = window.Worker;
  window.Worker = class extends Original {
    constructor(...args) {
      super(...args);
      this.addEventListener("message", (e) => {
        if (e.data?.result?.ready) window.cacheReady = true;
      });
    }
    postMessage(message, ...args) {
      if (message?.method) window.dbCalls.push(message.method);
      return super.postMessage(message, ...args);
    }
  };
});
const p = await ctx.newPage();
const url = origin + "/?entry=" + encodeURIComponent(notes[10].path);
try {
  await p.goto(url);
  await expect(
    p.locator(".entry-reader").getByText("Startup scratch 10", { exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await expect
    .poll(() => p.evaluate(() => window.cacheReady), { timeout: 120000 })
    .toBe(true);
  await p.getByRole("button", { name: "Overview", exact: true }).click();
  await expect(
    p.getByText("Startup task 0", { exact: true }).first(),
  ).toBeVisible();
  await p.getByRole("button", { name: "Entries", exact: true }).click();
  const requests = [];
  p.on("request", (r) => requests.push(r.url()));
  let release;
  const gate = new Promise((resolve) => (release = resolve));
  await p.route("**/api/v1/sync/entries/selected", async (route) => {
    await gate;
    await route.continue().catch(() => {});
  });
  await p.route("**/api/v1/tasks/stream**", async (route) => {
    await gate;
    await route.continue().catch(() => {});
  });
  const start = Date.now();
  await p.reload();
  await expect(
    p.locator(".entry-reader").getByText("Startup scratch 10", { exact: true }),
  ).toBeVisible({ timeout: 5000 });
  const warmMs = Date.now() - start;
  assert.ok(
    !requests.some((u) => new URL(u).pathname === "/api/v1/sync/entries"),
    "warm startup must never drain the full library feed",
  );
  console.log(
    "PASS cached entry visible before sync/server snapshots, warm reload ms:",
    warmMs,
  );
  await p.getByRole("button", { name: "Overview", exact: true }).click();
  await expect(
    p.getByText("Startup task 0", { exact: true }).first(),
  ).toBeVisible({ timeout: 5000 });
  console.log("PASS cached tasks visible before server snapshots");
  await p.getByRole("button", { name: "Entries", exact: true }).click();
  release();
  await p.unroute("**/api/v1/sync/entries/selected");
  await p.unroute("**/api/v1/tasks/stream**");
  await expect(
    p.getByRole("button", { name: /Offline sync · Ready/ }),
  ).toBeVisible({ timeout: 30000 });
  await p.waitForTimeout(1500);
  const before = await p.evaluate(
    () => window.dbCalls.filter((m) => m === "list" || m === "summary").length,
  );
  await p.waitForTimeout(11500);
  const after = await p.evaluate(
    () => window.dbCalls.filter((m) => m === "list" || m === "summary").length,
  );
  assert.equal(after, before, "unchanged sync must not reload cached lists");
  console.log("PASS unchanged sync does not rescan lists or sidebar metadata");
  // Verify a server change still invalidates and updates the cached reading pane.
  const r = await fetch(origin + "/api/v1/entries/" + notes[10].path, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content: "Updated after warm startup" }),
  });
  assert.ok(r.ok, await r.text());
  await expect(
    p.getByText("Updated after warm startup", { exact: true }),
  ).toBeVisible({ timeout: 20000 });
  console.log("PASS server changes still update the visible entry");
  console.log("Demo:", url);
} finally {
  await b.close();
}
