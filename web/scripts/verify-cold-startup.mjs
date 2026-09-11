import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
const origin = process.env.BRAIN_READER_TEST_URL ?? "http://localhost:3338";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const browser = await chromium.launch();
const context = await browser.newContext({ serviceWorkers: "block" });
const page = await context.newPage();
let release;
const gate = new Promise((r) => (release = r));
let held = 0;
await page.route("**/api/v1/sync/entries**", async (route) => {
  held++;
  await gate;
  await route.continue().catch(() => {});
});
try {
  const stats = await (await fetch(origin + "/api/v1/stats")).json();
  assert.ok(stats.totalEntries >= 5000, JSON.stringify(stats));
  await page.goto(
    origin +
      "/?entry=" +
      encodeURIComponent("projects/bootstrap/scratch/n4999.md"),
  );
  await expect(
    page
      .locator(".entry-reader")
      .getByText("Bootstrap note 4999", { exact: true }),
  ).toBeVisible({ timeout: 10000 });
  assert.ok(held > 0, "background sync must be blocked during assertion");
  await expect(
    page.getByText("Loading projects…", { exact: true }),
  ).toHaveCount(0);
  console.log(
    "PASS cold dashboard and uncached entry render with 5,000 entries while initial sync is blocked",
  );
  await page.reload();
  await expect(
    page
      .locator(".entry-reader")
      .getByText("Bootstrap note 4999", { exact: true }),
  ).toBeVisible({ timeout: 10000 });
  console.log("PASS reload during incomplete bootstrap stays usable");
  release();
} finally {
  release();
  await browser.close();
}
