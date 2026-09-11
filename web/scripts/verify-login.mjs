// Requires an isolated auth-enabled loopback server with admin/mobile-login-fixture.
// Uses real login/refresh endpoints; intercepted expired-token responses exercise recovery.
import { chromium, webkit, expect } from "@playwright/test";
import assert from "node:assert/strict";
const origin = process.env.BRAIN_LOGIN_TEST_URL || "http://localhost:3337";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const b = await (
  process.env.BRAIN_LOGIN_BROWSER === "webkit" ? webkit : chromium
).launch();
const c = await b.newContext({
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
  serviceWorkers: "block",
});
const p = await c.newPage();
const events = [];
let refreshRequests = 0;
c.on("request", (r) => {
  if (new URL(r.url()).pathname === "/api/v1/auth/refresh") refreshRequests++;
});
p.on("response", (r) => {
  if (r.url().includes("/api/"))
    events.push([new URL(r.url()).pathname, r.status()]);
});
let release;
const wait = new Promise((r) => (release = r));
let held = false;
if (process.env.BRAIN_LOGIN_RACE === "1")
  await p.route("**/api/v1/reminders**", async (route) => {
    if (!held && !route.request().headers().authorization) {
      held = true;
      const response = await route.fetch();
      await wait;
      await route.fulfill({ response });
    } else await route.continue();
  });
try {
  await p.goto(origin);
  await p
    .getByRole("button", { name: "Sign in with password", exact: true })
    .tap();
  await p
    .getByPlaceholder("Password", { exact: true })
    .fill("mobile-login-fixture");
  await p.getByRole("button", { name: "Sign in", exact: true }).tap();
  await expect(
    p.getByRole("button", { name: "Open workspace navigation", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  release();
  await p.waitForTimeout(5000);
  await expect(
    p.getByRole("button", { name: "Open workspace navigation", exact: true }),
  ).toBeVisible();
  if (process.env.BRAIN_LOGIN_EXPIRE === "1") {
    const old = await p.evaluate(() =>
      localStorage.getItem("brain.access_token"),
    );
    await c.route("**/api/v1/**", async (route) => {
      if (route.request().headers().authorization === "Bearer " + old) {
        await new Promise((r) => setTimeout(r, 100));
        await route.fulfill({
          status: 401,
          json: { error: "Expired access token fixture" },
        });
      } else await route.continue();
    });
  }
  const second =
    process.env.BRAIN_LOGIN_EXPIRE === "1" ? await c.newPage() : null;
  await Promise.all([p.reload(), ...(second ? [second.goto(origin)] : [])]);
  await expect(
    p.getByRole("button", { name: "Open workspace navigation", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await p.waitForTimeout(5000);
  await expect(
    p.getByRole("button", { name: "Open workspace navigation", exact: true }),
  ).toBeVisible();
  if (second) {
    await expect(
      second.getByRole("button", {
        name: "Open workspace navigation",
        exact: true,
      }),
    ).toBeVisible();
    assert.equal(
      refreshRequests,
      1,
      "both tabs must share one rotating refresh",
    );
  }
  const status = await p.evaluate(
    async () =>
      (
        await fetch("/api/v1/sync/identity", {
          headers: {
            Authorization:
              "Bearer " + localStorage.getItem("brain.access_token"),
          },
        })
      ).status,
  );
  assert.equal(status, 200);
  console.log(
    "PASS real password login, reload, protected API, and two-tab refresh recovery",
    { held, refreshRequests },
  );
} catch (e) {
  console.log("Responses:", JSON.stringify(events));
  console.log("Visible:", await p.locator("body").innerText());
  throw e;
} finally {
  release();
  await b.close();
}
