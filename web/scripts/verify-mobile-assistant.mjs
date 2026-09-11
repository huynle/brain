import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
const origin = process.env.BRAIN_MOBILE_TEST_URL ?? "http://localhost:3340";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
  serviceWorkers: "block",
});
const page = await context.newPage();
try {
  await page.goto(origin);
  await expect(page.locator(".assistant-panel")).toBeVisible({
    timeout: 30000,
  });
  await expect(page.locator(".assistant-panel textarea")).toBeInViewport();
  console.log(
    "PASS mobile root opens Assistant with the composer immediately visible",
  );
  await page
    .getByRole("button", { name: "Close assistant", exact: true })
    .tap();
  const shortcut = page
    .getByRole("navigation", { name: "Main navigation" })
    .getByRole("button", { name: "Assistant", exact: true });
  await expect(shortcut).toBeInViewport();
  await shortcut.tap();
  await expect(page.locator(".assistant-panel")).toBeVisible();
  await page
    .getByRole("checkbox", {
      name: "Open Assistant when Brain starts on mobile",
    })
    .uncheck();
  await page.reload();
  await expect(
    page.getByRole("navigation", { name: "Main navigation" }),
  ).toBeVisible();
  await expect(page.locator(".assistant-panel")).toHaveCount(0);
  await shortcut.tap();
  await page
    .getByRole("checkbox", {
      name: "Open Assistant when Brain starts on mobile",
    })
    .check();
  const r = await fetch(
    origin + "/api/v1/entries?project=mobile-playground&type=scratch&limit=1",
  );
  const entry = (await r.json()).entries[0];
  await page.goto(origin + "/?entry=" + encodeURIComponent(entry.path));
  await expect(page.locator(".entry-reader")).toContainText(entry.title);
  await expect(page.locator(".assistant-panel")).toHaveCount(0);
  await shortcut.tap();
  await page
    .getByRole("button", { name: "Close assistant", exact: true })
    .tap();
  await expect(page.locator(".entry-reader")).toContainText(entry.title);
  console.log(
    "PASS one-tap access, persisted opt-out, and entry deep links preserve the reader",
  );
  const desktop = await browser.newContext({
    viewport: { width: 1280, height: 900 },
    serviceWorkers: "block",
  });
  const d = await desktop.newPage();
  await d.goto(origin);
  await expect(
    d.getByRole("button", { name: "Assistant", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await expect(d.locator(".assistant-panel")).toHaveCount(0);
  console.log("PASS desktop does not automatically open Assistant");
  await desktop.close();
} finally {
  await browser.close();
}
