import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
const origin = process.env.BRAIN_READER_TEST_URL ?? "http://127.0.0.1:3333";
assert.ok(
  ["localhost", "127.0.0.1"].includes(new URL(origin).hostname),
  "Loopback development only",
);
const project = "reader-demo-" + Date.now();
const results = [];
const evidence = join(tmpdir(), project);
await mkdir(evidence);
const pass = (s) => {
  results.push(s);
  console.log("PASS", s);
};
async function request(path, body) {
  const r = await fetch(origin + path, {
    method: body ? "POST" : "GET",
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  assert.ok(r.ok, await r.clone().text());
  return r.json();
}
await expect
  .poll(
    async () => {
      try {
        return (await fetch(origin + "/api/v1/health")).status;
      } catch {
        return 0;
      }
    },
    { timeout: 30000 },
  )
  .toBe(200);
const b = await request("/api/v1/entries", {
  type: "scratch",
  project,
  title: "Linked reader note",
  content:
    "## Details\n\nThis is the second document.\n\n- [x] Rendered task list\n\n| Name | Value |\n| --- | --- |\n| Reader | Ready |",
  status: "draft",
});
const a = await request("/api/v1/entries", {
  type: "scratch",
  project,
  title: "Standalone reader demonstration",
  status: "draft",
  content:
    `A fast document view without the dashboard.\n\n[Go to linked note](${b.path}#details)\n\n[[${b.path}|Wiki alias]]\n\n[Relative note](./${b.id}.md)\n\n[URL entry](${origin}/?entry=${encodeURIComponent(b.path)})\n\n[External reference](https://example.com)\n\n[Jump to details](#details)\n\n## Details\n\n**Markdown formatting** with \\` +
    "`inline code`" +
    `.\n\n<script>window.readerUnsafe = true</script>\n\n[Unsafe](javascript:alert(1))\n\n\`\`\`text\n[[literal-code]]\n\`\`\`\n`,
});
const href = (e) => origin + "/read.html?entry=" + encodeURIComponent(e.path);
const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: 1100, height: 800 },
});
const page = await context.newPage();
const requests = [];
const errors = [];
page.on("request", (r) => requests.push(r.url()));
page.on("pageerror", (e) => errors.push(e.message));
try {
  await page.goto(href(a));
  await expect(
    page.getByRole("heading", { name: a.title, exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Markdown formatting", { exact: true }),
  ).toBeVisible();
  assert.ok(
    !requests.some((u) =>
      /\.wasm|\/sync\/entries|\/tasks|\/runners|\/events\/stream|\/assets\/app-/.test(
        u,
      ),
    ),
    requests.join("\n"),
  );
  assert.equal(context.serviceWorkers().length, 0);
  await expect(page.getByRole("button", { name: /Offline sync/ })).toHaveCount(
    0,
  );
  pass(
    "Cold reader loads one document and connections without dashboard, WASM bootstrap, task polling, or service worker installation",
  );
  await expect(page.getByRole("link", { name: "Wiki alias" })).toHaveAttribute(
    "href",
    "/read.html?entry=" + encodeURIComponent(b.path),
  );
  await expect(
    page.getByRole("link", { name: "Relative note" }),
  ).toHaveAttribute("href", "/read.html?entry=" + encodeURIComponent(b.path));
  await expect(page.getByRole("link", { name: "URL entry" })).toHaveAttribute(
    "href",
    "/read.html?entry=" + encodeURIComponent(b.path),
  );
  await expect(page.locator("pre")).toContainText("[[literal-code]]");
  assert.equal(await page.evaluate(() => window.readerUnsafe), undefined);
  await expect(
    page.getByRole("link", { name: "External reference" }),
  ).toHaveAttribute("href", "https://example.com");
  await expect(
    page.getByRole("link", { name: "External reference" }),
  ).toHaveAttribute("rel", "noopener noreferrer");
  assert.equal(await page.locator('a[href^="javascript:"]').count(), 0);
  pass(
    "Wiki, relative, full entry URLs, external links, and code render safely",
  );
  await page.getByRole("link", { name: "Jump to details" }).click();
  assert.ok(page.url().endsWith("#details"));
  await expect(page.locator("#details")).toBeVisible();
  await page.getByRole("link", { name: "Go to linked note" }).click();
  await expect(
    page.getByRole("heading", { name: b.title, exact: true }),
  ).toBeVisible();
  assert.ok(page.url().endsWith("#details"));
  await expect(page.locator("table")).toContainText("Ready");
  await expect(
    page.getByRole("link", { name: a.title, exact: true }),
  ).toBeVisible();
  pass(
    "Forward link follows the document heading and destination exposes its backlink",
  );
  await page.getByRole("link", { name: a.title, exact: true }).click();
  await expect(
    page.getByRole("heading", { name: a.title, exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: b.title, exact: true }),
  ).toBeVisible();
  await page.goForward();
  await expect(
    page.getByRole("heading", { name: a.title, exact: true }),
  ).toBeVisible();
  pass("Backlinks and browser Back/Forward retain reader mode");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: join(evidence, "reader-mobile.png"),
    fullPage: true,
  });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await page.goto(origin + "/read.html?entry=" + a.id);
  await expect(
    page.getByRole("heading", { name: a.title, exact: true }),
  ).toBeVisible();
  await page.goto(origin + "/read.html?entry=missing-entry");
  await expect(
    page.getByRole("heading", { name: "Unable to load entry" }),
  ).toBeVisible();
  await page.goto(origin + "/read.html");
  await expect(
    page.getByRole("heading", { name: "Read an entry", exact: true }),
  ).toBeVisible();
  pass(
    "Short IDs, 390px layout, missing entries, and missing URL parameters are handled",
  );
  const protectedContext = await browser.newContext();
  const protectedPage = await protectedContext.newPage();
  await protectedPage.route("**/api/v1/sync/identity", (r) =>
    r.fulfill({
      status: 401,
      contentType: "application/json",
      body: '{"error":"Unauthorized"}',
    }),
  );
  await protectedPage.goto(href(a));
  await expect(
    protectedPage.getByRole("button", {
      name: "Sign in with password",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    protectedPage.getByRole("heading", { name: a.title, exact: true }),
  ).toHaveCount(0);
  await protectedContext.close();
  pass(
    "Unauthorized reader uses existing sign-in gate without displaying private document content",
  );
  const installed = await browser.newContext();
  const installedPage = await installed.newPage();
  await installedPage.goto(origin);
  await installedPage.evaluate(() => navigator.serviceWorker.ready);
  await installedPage.goto(href(a));
  await expect(
    installedPage.getByRole("heading", { name: a.title, exact: true }),
  ).toBeVisible();
  await expect(
    installedPage.getByRole("button", { name: /Offline sync/ }),
  ).toHaveCount(0);
  await installed.close();
  pass("Installed PWA service worker preserves the separate reader route");
  await page.goto(href(a));
  await expect(
    page.getByRole("heading", { name: a.title, exact: true }),
  ).toBeVisible();
  assert.deepEqual(errors, []);
  await writeFile(
    join(evidence, "results.json"),
    JSON.stringify({ url: href(a), project, results, requests }, null, 2),
  );
  console.log("Reader URL:", href(a));
  console.log("Evidence:", evidence);
} finally {
  await browser.close();
}
