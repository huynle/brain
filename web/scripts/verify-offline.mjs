// Real production PWA + SQLite OPFS + isolated Go server. No mocked API or DB.
// Build first: just build-all. Run: npm run test:offline (from web/).
import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:net";
const root = await mkdtemp(join(tmpdir(), "brain-offline-e2e-"));
const port = await new Promise((resolvePort) => {
  const s = createServer();
  s.listen(0, "127.0.0.1", () => {
    const p = s.address().port;
    s.close(() => resolvePort(p));
  });
});
const origin = `http://127.0.0.1:${port}`;
await mkdir(join(root, "config"), { recursive: true });
const child = spawn(resolve("../bin/brain"), ["api", "--port", String(port)], {
  cwd: resolve(".."),
  env: {
    ...process.env,
    XDG_CONFIG_HOME: join(root, "config"),
    XDG_STATE_HOME: join(root, "state"),
    BRAIN_DIR: join(root, "data"),
    ENABLE_AUTH: "false",
    HOST: "127.0.0.1",
    PORT: String(port),
    BRAIN_FEATURE_CHECKOUT_ENABLED: "false",
  },
});
let logs = "";
child.stdout.on("data", (b) => (logs += b));
child.stderr.on("data", (b) => (logs += b));
let browser;
const results = [];
const pass = (name) => {
  results.push(name);
  console.log("PASS", name);
};
async function request(path, body, method = body ? "POST" : "GET") {
  const r = await fetch(origin + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  assert.ok(r.ok, `${method} ${path}: ${r.status} ${text}`);
  return text ? JSON.parse(text) : undefined;
}
async function allChanges(cursor = 0, epoch = "") {
  const changes = [];
  let p;
  do {
    p = await request(`/api/v1/sync/entries?cursor=${cursor}&epoch=${epoch}`);
    changes.push(...p.changes);
    cursor = p.cursor;
    epoch = p.epoch;
  } while (p.more);
  return { cursor, epoch, changes };
}
try {
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
  const seeds = {};
  for (const type of ["scratch", "task", "automation"])
    seeds[type] = await request("/api/v1/entries", {
      type,
      title: `Offline seeded ${type}`,
      content: `Original ${type} body`,
      project: "offline-demo",
      status: "draft",
      ...(type === "automation"
        ? { action: { type: "script", command: "echo seeded" } }
        : {}),
    });
  const extras = [];
  for (let i = 0; i < 205; i++)
    extras.push(
      await request("/api/v1/entries", {
        type: "scratch",
        title: `Seed padding ${i}`,
        content: "searchable albatross " + i,
        project: "offline-demo",
        status: "draft",
      }),
    );
  const initial = await allChanges();
  assert.equal(initial.changes.length, 208);
  assert.equal(
    (await allChanges(initial.cursor, initial.epoch)).changes.length,
    0,
  );
  pass(
    "208 seeded records bootstrap across pages; unchanged feed transfers zero records",
  );
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const wasm = [];
  page.on("response", (r) => {
    if (r.url().endsWith(".wasm")) wasm.push(r.headers()["content-type"]);
  });
  await page.goto(origin);
  await expect(
    page.getByRole("button", { name: /Offline sync · Ready/ }),
  ).toBeVisible({ timeout: 30000 });
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  const open = async (p = page) => {
    await p.getByRole("button", { name: /Offline sync ·/ }).click();
    await expect(
      p.getByRole("heading", { name: "Offline sync", exact: true }),
    ).toBeVisible();
  };
  const select = async (path, p = page) => {
    await p.getByLabel("Entry", { exact: true }).selectOption(path);
  };
  await open();
  await select(seeds.automation.path);
  await expect(page.getByLabel("Entry draft")).toContainText("echo seeded");
  assert.ok(
    wasm.length && wasm.every((ct) => ct === "application/wasm"),
    JSON.stringify(wasm),
  );
  pass(
    "production WASM loads with correct MIME; full automation definition cached",
  );
  await context.setOffline(true);
  await page.reload();
  await open();
  await select(seeds.scratch.path);
  await expect(page.getByLabel("Entry draft")).toContainText(
    "Original scratch body",
  );
  pass("installed PWA and persistent SQLite reopen with network disconnected");
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Entries", exact: true }).click();
  await page.getByPlaceholder("Search entries…  ( / )").fill("albatross");
  await expect(page.locator(".entry-row")).toHaveCount(50);
  pass("existing Entries view performs local full-text search while offline");
  await page.getByPlaceholder("Search entries…  ( / )").fill("");
  await open();

  for (const type of ["scratch", "task", "automation"]) {
    await select(seeds[type].path);
    const editor = page.getByLabel("Entry draft");
    let raw = await editor.inputValue();
    raw = raw.replace(`Original ${type} body`, `Offline edited ${type} body`);
    if (type === "automation") raw = raw.replace("echo seeded", "echo edited");
    await editor.fill(raw);
    await page
      .getByRole("button", { name: "Save locally", exact: true })
      .click();
    await expect(page.getByRole("status").last()).toContainText(
      "Saved on this device",
    );
  }
  await expect(
    page.getByRole("button", { name: /Offline sync ·.*3 pending/ }),
  ).toBeVisible();
  await page.reload();
  await open();
  await select(seeds.task.path);
  await expect(page.getByLabel("Entry draft")).toContainText(
    "Offline edited task body",
  );
  pass("knowledge, task, and automation offline edits survive reload");
  if (!(await page.getByLabel("Title", { exact: true }).isVisible()))
    await page.getByText("Create an entry offline", { exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill("Created while offline");
  await page.getByLabel("Project", { exact: true }).fill("offline-demo");
  await page
    .getByLabel("Content", { exact: true })
    .fill("offline created content");
  await page
    .getByRole("button", { name: "Create locally", exact: true })
    .click();
  await expect(page.getByRole("button", { name: /4 pending/ })).toBeVisible();
  await request(
    "/api/v1/entries/" + seeds.scratch.path,
    { content: "Concurrent server body" },
    "PATCH",
  );
  await request(
    "/api/v1/entries/" + extras[0].path + "?confirm=true",
    undefined,
    "DELETE",
  );
  await request("/api/v1/entries/" + extras[1].path + "/move", {
    project: "moved-demo",
  });
  const delta = await allChanges(initial.cursor, initial.epoch);
  assert.equal(delta.changes.length, 4);
  assert.equal(delta.changes.filter((c) => c.deleted).length, 2);
  pass("delta feed returns only update, delete, and both sides of a move");
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /Offline sync · Ready · 1 pending/ }),
  ).toBeVisible({ timeout: 30000 });
  for (const type of ["task", "automation"]) {
    const e = await request("/api/v1/entries/" + seeds[type].path);
    assert.equal(e.content.trim(), `Offline edited ${type} body`);
    if (type === "automation") assert.equal(e.action.command, "echo edited");
  }
  const created = await request(
    "/api/v1/entries?project=offline-demo&type=scratch&limit=500",
  );
  assert.equal(
    created.entries.filter((e) => e.title === "Created while offline").length,
    1,
  );
  assert.equal(
    (await request("/api/v1/entries/" + seeds.scratch.path)).content,
    "Concurrent server body",
  );
  await expect(page.getByText("Needs review", { exact: false })).toBeVisible();
  pass(
    "reconnect applies task and automation edits and one create; concurrent knowledge edit conflicts without overwrite",
  );
  await page.getByRole("button", { name: "Review draft", exact: true }).click();
  await expect(page.getByLabel("Entry draft")).toContainText(
    "Offline edited scratch body",
  );
  await expect(page.getByLabel("Current server version")).toContainText(
    "Concurrent server body",
  );
  page.once("dialog", (d) => d.accept());
  await page
    .getByRole("button", {
      name: "Apply draft to current version",
      exact: true,
    })
    .click();
  await expect(
    page.getByRole("button", { name: "Offline sync · Ready", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  assert.equal(
    (await request("/api/v1/entries/" + seeds.scratch.path)).content.trim(),
    "Offline edited scratch body",
  );
  pass("conflict review shows both versions and explicit resolution converges");
  // Two browser tabs contend for the same OPFS files through Web Locks.
  const second = await context.newPage();
  second.on("pageerror", (e) => errors.push(e.message));
  await second.goto(origin);
  await expect(
    second.getByRole("button", { name: "Offline sync · Ready", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await open(second);
  await select(seeds.task.path, second);
  await expect(second.getByLabel("Entry draft")).toContainText(
    "Offline edited task body",
  );
  await context.setOffline(true);
  await select(seeds.task.path);
  await page
    .getByLabel("Entry draft")
    .fill(
      (await page.getByLabel("Entry draft").inputValue()).replace(
        "Offline edited task body",
        "Two tab offline edit",
      ),
    );
  await page.getByRole("button", { name: "Save locally", exact: true }).click();
  await expect(second.getByRole("button", { name: /1 pending/ })).toBeVisible({
    timeout: 10000,
  });

  await second
    .getByLabel("Entry draft")
    .fill(
      (await second.getByLabel("Entry draft").inputValue()).replace(
        "Offline edited task body",
        "Stale second tab",
      ),
    );
  await second
    .getByRole("button", { name: "Save locally", exact: true })
    .click();
  await expect(second.getByRole("status").last()).toContainText(
    "another editor",
  );
  pass("stale second-tab editor cannot overwrite a newer local draft");
  await select(seeds.automation.path, second);
  await select(seeds.task.path, second);
  await expect(second.getByLabel("Entry draft")).toContainText(
    "Two tab offline edit",
  );
  pass(
    "two tabs share durable database and observe pending edits without lock errors",
  );
  await context.setOffline(false);
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Offline sync · Ready", exact: true }),
  ).toBeVisible({ timeout: 30000 });

  await second.close();
  // Simulate the response being lost only AFTER the real server commits it.
  await page.evaluate(() => {
    const original = window.fetch.bind(window);
    let drop = true;
    window.fetch = async (...args) => {
      const response = await original(...args);
      if (
        drop &&
        String(args[0]).includes("/api/v1/sync/entries") &&
        args[1]?.method === "POST"
      ) {
        drop = false;
        window.__lostSyncResponse = true;
        throw new TypeError("Lost response after server commit");
      }
      return response;
    };
  });
  if (!(await page.getByLabel("Title", { exact: true }).isVisible()))
    await page.getByText("Create an entry offline", { exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill("Lost response create");
  await page.getByLabel("Project", { exact: true }).fill("offline-demo");
  await page
    .getByLabel("Content", { exact: true })
    .fill("Must be created exactly once");
  await page
    .getByRole("button", { name: "Create locally", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => window.__lostSyncResponse))
    .toBe(true);
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Offline sync · Ready", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  const afterRetry = await request(
    "/api/v1/entries?project=offline-demo&type=scratch&limit=500",
  );
  assert.equal(
    afterRetry.entries.filter((e) => e.title === "Lost response create").length,
    1,
  );
  pass(
    "lost response after real server commit replays the same operation after reload without duplicate creation",
  );
  await open();
  await select(seeds.scratch.path);
  const beforeServerChange = await page.getByLabel("Entry draft").inputValue();
  await request(
    "/api/v1/entries/" + seeds.scratch.path,
    { content: "Newer while editor open" },
    "PATCH",
  );
  await page.getByRole("button", { name: "Sync now", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Offline sync · Ready", exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("Entry draft")
    .fill(
      beforeServerChange.replace(
        "Offline edited scratch body",
        "Stale open editor",
      ),
    );
  await page.getByRole("button", { name: "Save locally", exact: true }).click();
  await expect(page.getByText("Needs review", { exact: false })).toBeVisible({
    timeout: 15000,
  });
  assert.equal(
    (await request("/api/v1/entries/" + seeds.scratch.path)).content,
    "Newer while editor open",
  );
  pass(
    "open editor retains its original revision even when background sync downloads a newer server version",
  );
  page.once("dialog", (d) => d.accept());
  await page
    .getByRole("button", { name: "Discard local edit", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Offline sync · Ready", exact: true }),
  ).toBeVisible();
  assert.equal(errors.length, 0, errors.join("\n"));
  await select(seeds.task.path);
  await page.locator(".offline-sync-dialog").evaluate((el) => {
    el.scrollTop = 0;
  });
  await page.screenshot({ path: join(root, "verified.png"), fullPage: false });
  await page.setViewportSize({ width: 390, height: 844 });
  const dimensions = await page
    .locator(".offline-sync-dialog")
    .evaluate((el) => ({ scroll: el.scrollWidth, width: el.clientWidth }));
  assert.ok(
    dimensions.scroll <= dimensions.width + 1,
    JSON.stringify(dimensions),
  );
  await page.screenshot({
    path: join(root, "verified-mobile.png"),
    fullPage: false,
  });
  pass("no uncaught browser errors; editor fits a 390px viewport");
  await writeFile(
    join(root, "results.json"),
    JSON.stringify({ origin, results }, null, 2),
  );
  console.log("Evidence:", root);
} catch (e) {
  if (browser) {
    const pages = browser.contexts().flatMap((c) => c.pages());
    for (let i = 0; i < pages.length; i++) {
      await pages[i]
        .screenshot({ path: join(root, `failure-${i}.png`), fullPage: true })
        .catch(() => {});
      console.log(
        (
          await pages[i]
            .locator("body")
            .innerText()
            .catch(() => "")
        ).slice(-4500),
      );
    }
  }
  console.error("Evidence:", root);
  throw e;
} finally {
  await browser?.close();
  child.kill("SIGTERM");
  await writeFile(join(root, "server.log"), logs);
}
