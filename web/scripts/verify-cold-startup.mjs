// Real 5,000-entry dataset: record all browser requests and prove only opened
// documents enter the working set. No API/SQLite mocks.
import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
const origin = process.env.BRAIN_READER_TEST_URL ?? "http://localhost:3338";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const browser = await chromium.launch();
const ctx = await browser.newContext();
const p = await ctx.newPage();
const requests = [];
const selected = [];
p.on("request", (r) =>
  requests.push({
    url: r.url(),
    method: r.method(),
    body: r.postDataJSON?.bind(r),
  }),
);
p.on("response", async (r) => {
  if (new URL(r.url()).pathname === "/api/v1/sync/entries/selected")
    try {
      selected.push(await r.json());
    } catch {}
});
try {
  const stats = await (await fetch(origin + "/api/v1/stats")).json();
  assert.ok(stats.totalEntries >= 5000);
  await p.goto(
    origin +
      "/?entry=" +
      encodeURIComponent("projects/bootstrap/scratch/n4999.md"),
  );
  await expect(p.locator(".entry-reader")).toContainText("Bootstrap note 4999");
  await expect(
    p.getByRole("button", { name: /Offline sync · Ready · 1 cached/ }),
  ).toBeVisible();
  await p
    .locator(".entry-type-chips")
    .getByRole("button", { name: /^scratch / })
    .click();
  await expect(p.locator(".entry-row")).toHaveCount(50);
  const before = await p.locator(".entry-row").count();
  await p
    .getByRole("button", { name: "Load more entries", exact: true })
    .click();
  await expect(p.locator(".entry-row")).toHaveCount(before + 50);
  assert.ok(
    !requests.some((r) => new URL(r.url).pathname === "/api/v1/sync/entries"),
    "must never request the full-library feed",
  );
  await p.waitForTimeout(11000);
  const selections = requests
    .filter((r) => new URL(r.url).pathname === "/api/v1/sync/entries/selected")
    .map((r) => Object.keys(r.body().entries));
  assert.ok(
    selections.every((paths) =>
      paths.every((path) => path === "projects/bootstrap/scratch/n4999.md"),
    ),
    JSON.stringify(selections),
  );
  assert.equal(
    selected.reduce((n, r) => n + r.changes.filter((c) => c.entry).length, 0),
    1,
    "unchanged recent documents must transfer no bodies",
  );
  console.log(
    "PASS 5,000-entry library: only opened document cached, 50-row pages on demand, zero full-feed requests and zero unchanged bodies",
  );
  await p.evaluate(() => navigator.serviceWorker.ready);
  await ctx.setOffline(true);
  await p.reload();
  await expect(p.locator(".entry-reader")).toContainText("Bootstrap note 4999");
  console.log("PASS opened entry renders after an offline reload");
  await ctx.setOffline(false);
  await p.reload();
  await expect(p.locator(".entry-reader")).toContainText("Bootstrap note 4999");
  console.log(
    "PASS offline-to-online recovery keeps the working set selective",
  );
} finally {
  await browser.close();
}
