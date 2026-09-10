import { chromium, webkit, expect } from "@playwright/test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
const origin = process.env.BRAIN_MOBILE_TEST_URL ?? "http://127.0.0.1:5181";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const project = "mobile-" + Date.now();
const evidence = join(tmpdir(), project);
await mkdir(evidence);
async function req(path, body, method = "POST") {
  const r = await fetch(origin + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  assert.ok(r.ok, await r.clone().text());
  return r.json();
}
const note = await req("/api/v1/entries", {
  project,
  type: "scratch",
  status: "draft",
  title: "Mobile reading fixture",
  content:
    "![Mobile image](mobile.svg)\n\n# Read comfortably\n\nA note for touch navigation and editing.\n\n| Column | Long value |\n|---|---|\n| Sample | " +
    "long-value-".repeat(50) +
    " |",
});
const second = await req("/api/v1/entries", {
  project,
  type: "scratch",
  status: "draft",
  title: "Mobile second note",
  content: "A second document for comparing and opening panes.",
});
const task = await req("/api/v1/entries", {
  project,
  type: "task",
  status: "pending",
  title: "Mobile task fixture",
  content: "Review this task on a phone.",
});
await req("/api/v1/entries", {
  project,
  type: "automation",
  status: "draft",
  title: "Mobile automation fixture",
  content: "Disabled test definition.",
  action: { type: "script", command: "echo mobile-test" },
});
const upload = new FormData();
upload.set("project_id", project);
upload.set(
  "file",
  new Blob(
    [
      `<svg xmlns="http://www.w3.org/2000/svg" width="600" height="300"><rect width="600" height="300" fill="#193a51"/><text x="25" y="130" fill="white" font-size="30">Mobile attachment</text><!-- ${project} --></svg>`,
    ],
    { type: "image/svg+xml" },
  ),
  "mobile.svg",
);
const uploaded = await fetch(origin + "/api/v1/attachments", {
  method: "POST",
  body: upload,
});
assert.ok(uploaded.ok);
const attachment = (await uploaded.json()).attachment;
await req(`/api/v1/entries/${note.id}/attachments?project_id=${project}`, {
  attachment: { id: attachment.id, role: "inline" },
});
const b = await (
  process.env.BRAIN_MOBILE_BROWSER === "webkit" ? webkit : chromium
).launch();
const context = await b.newContext({
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
  serviceWorkers: "block",
});
const p = await context.newPage();
p.setDefaultTimeout(10000);
const errors = [];
p.on("pageerror", (e) => errors.push(e.message));
const checks = [];
const pass = (s) => {
  checks.push(s);
  console.log("PASS", s);
};
async function fits(locator) {
  const box = await locator.boundingBox();
  assert.ok(
    box && box.x >= -1 && box.x + box.width <= p.viewportSize().width + 1,
    JSON.stringify(box),
  );
}
async function shot(name) {
  await p.screenshot({ path: join(evidence, name + ".png") });
}
try {
  await p.goto(origin);
  await expect(
    p.getByRole("button", { name: "Open workspace navigation" }),
  ).toBeVisible({ timeout: 30000 });
  for (const [width, height] of [
    [320, 740],
    [390, 844],
    [820, 1180],
    [844, 390],
  ]) {
    await p.setViewportSize({ width, height });
    await expect(p.locator("body")).toHaveClass(/mobile/);
    await fits(p.locator(".topbar"));
    await fits(p.locator(".mobile-nav"));
    for (const button of await p
      .locator(".topbar button:visible, .mobile-nav button:visible")
      .all()) {
      await fits(button);
      const box = await button.boundingBox();
      assert.ok(box.height >= 44);
    }
    assert.ok(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    );
  }
  await p.setViewportSize({ width: 390, height: 844 });
  await shot("overview");
  pass("Phone, tablet, and landscape navigation fits with 44px touch controls");
  await p.getByRole("button", { name: "Open workspace navigation" }).click();
  await expect(
    p.getByRole("dialog", { name: "Workspace navigation" }),
  ).toBeVisible();
  await p.getByRole("searchbox", { name: "Find a project" }).fill(project);
  await expect(
    p.locator(".mobile-navigation .proj-row").filter({ hasText: project }),
  ).toBeVisible();
  await p
    .locator(".mobile-navigation .proj-row")
    .filter({ hasText: project })
    .tap();
  await shot("navigation");
  await p.getByRole("button", { name: "Settings", exact: true }).click();
  await expect(p.getByRole("dialog", { name: /Settings/ })).toBeVisible();
  await expect(p.locator(".mobile-navigation")).toHaveCount(0);
  await fits(p.locator(".modal"));
  await shot("settings");
  await p.locator(".modal-close").click();
  pass(
    "Workspace navigation exposes projects and settings without trapping a second dialog",
  );
  await p.getByRole("button", { name: "More tools" }).click();
  await expect(p.getByRole("dialog", { name: "Tools" })).toBeVisible();
  await p.getByRole("button", { name: "Assistant", exact: true }).click();
  await expect(p.locator(".assistant-panel")).toBeVisible();
  await fits(p.locator(".assistant-panel"));
  await shot("assistant");
  await p
    .locator(".assistant-panel")
    .getByRole("button", { name: /close/i })
    .click();
  pass("Assistant remains reachable and uses the full mobile screen");
  await p.getByRole("button", { name: "Search and commands" }).click();
  await expect(p.locator(".command-palette")).toBeVisible();
  await fits(p.locator(".command-palette"));
  await p.keyboard.press("Escape");
  pass("Search and command palette remain reachable");
  await p.goto(origin + "/?entry=" + encodeURIComponent(note.path));
  await expect(
    p.locator(".entry-reader").getByText(note.title, { exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await fits(p.locator(".entry-reader"));
  await shot("entry");
  await p.getByRole("button", { name: "Mobile image", exact: true }).tap();
  await expect(p.getByRole("dialog").getByRole("img")).toBeVisible();
  await fits(p.getByRole("dialog"));
  await shot("attachment");
  await p.waitForTimeout(11000);
  await expect(p.getByRole("dialog").getByRole("img")).toBeVisible();
  await p.getByRole("button", { name: "Close attachment preview" }).tap();
  assert.equal(context.pages().length, 1);
  pass("Attachment preview stays in the same tab");
  await p.getByRole("button", { name: "Edit definition", exact: true }).click();
  await expect(p.locator(".offline-sync-dialog")).toBeVisible();
  await fits(p.locator(".offline-sync-dialog"));
  await shot("editor");
  const draft = p.getByRole("textbox", { name: "Entry draft" });
  await expect(draft).toHaveValue(/Mobile reading fixture/);
  const original = await draft.inputValue();
  await draft.fill(original + "\nEdited from mobile.");
  await p.setViewportSize({ width: 390, height: 420 });
  await expect(draft).toHaveValue(/Edited from mobile/);
  await fits(p.locator(".offline-sync-dialog"));
  await shot("editor-small-viewport");
  await p.getByRole("button", { name: /^Save (locally|to server)$/ }).tap();
  await expect(
    p.getByText(
      /^(Saved on this device; awaiting server validation and sync\.|Saved to the server\.)$/,
    ),
  ).toBeVisible();
  await p.keyboard.press("Escape");
  await p.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(
      async () => {
        const r = await fetch(origin + "/api/v1/entries/" + note.path);
        return (await r.json()).content;
      },
      { timeout: 20000 },
    )
    .toContain("Edited from mobile.");
  pass("Entry reader and full definition editor fit the phone");
  await p.getByRole("button", { name: "Overview", exact: true }).click();
  const card = p.locator(`.pcard[data-project="${project}"]`);
  const row = card.locator(".trow").filter({ hasText: task.title });
  await expect(row).toBeVisible({ timeout: 20000 });
  await row.tap();
  await expect(p.locator(".feature-drawer")).toBeVisible();
  await fits(p.locator(".feature-drawer"));
  await shot("task");
  await p
    .locator(".feature-drawer")
    .getByRole("button", { name: "More…", exact: true })
    .tap();
  await expect(p.getByRole("dialog", { name: "More actions" })).toBeVisible();
  await fits(p.getByRole("dialog", { name: "More actions" }));
  await shot("task-actions");
  await p.getByRole("button", { name: "Change status…", exact: true }).tap();
  await expect(p.getByRole("dialog", { name: /Status:/ })).toBeVisible();
  await p.getByRole("button", { name: "Blocked", exact: true }).tap();
  await expect
    .poll(
      async () => {
        const r = await fetch(origin + "/api/v1/entries/" + task.path);
        return (await r.json()).status;
      },
      { timeout: 20000 },
    )
    .toBe("blocked");
  await p
    .getByRole("button", { name: "Close side panel", exact: true })
    .click();
  pass("Task detail, touch action sheet, and status changes work");
  await card.getByRole("button", { name: "Automations", exact: true }).tap();
  await expect(
    card.getByText("Mobile automation fixture", { exact: true }),
  ).toBeVisible();
  await shot("automations");
  await card.getByRole("button", { name: "Goals", exact: true }).tap();
  await card.getByRole("button", { name: "New goal", exact: true }).tap();
  await expect(
    p.getByRole("dialog", { name: "New goal", exact: true }),
  ).toBeVisible();
  await fits(p.getByRole("dialog", { name: "New goal", exact: true }));
  await shot("goals");
  await p.locator(".modal-close").click();
  pass("Automation and goal tabs remain reachable");
  await p.goto(origin + "/?entry=" + encodeURIComponent(note.path));
  await expect(p.locator(".entry-reader")).toBeVisible({ timeout: 20000 });
  await expect(p.locator(".feature-drawer")).not.toBeVisible();
  await expect(p.locator(".entry-reader")).toBeVisible({ timeout: 20000 });
  await p.getByTitle("Open in a Focus pane", { exact: true }).click();
  await p.goto(origin + "/?entry=" + encodeURIComponent(second.path));
  await expect(p.locator(".entry-reader")).toBeVisible({ timeout: 20000 });
  await expect(p.locator(".feature-drawer")).not.toBeVisible();
  await expect(p.locator(".entry-reader")).toBeVisible({ timeout: 20000 });
  await p.getByTitle("Open in a Focus pane", { exact: true }).click();
  await shot("focus");
  await expect(p.locator(".p2-pane-leaf").first()).toBeVisible();
  for (const pane of await p.locator(".p2-pane-leaf").all()) await fits(pane);
  await expect(p.getByRole("tab")).toHaveCount(2);
  await p.getByRole("button", { name: "Pane actions ⋯" }).tap();
  await p.getByText("Split down", { exact: true }).tap();
  await expect(p.locator(".p2-pane-leaf")).toHaveCount(2);
  const panes = await p.locator(".p2-pane-leaf").all();
  const firstBox = await panes[0].boundingBox();
  const secondBox = await panes[1].boundingBox();
  assert.ok(secondBox.y >= firstBox.y + firstBox.height - 2);
  for (const pane of panes) await fits(pane);
  await panes[1].scrollIntoViewIfNeeded();
  await shot("focus-stacked");
  pass(
    "Multiple entry panes and touch split actions remain reachable on mobile",
  );
  await p.goto(origin + "/?entry=" + encodeURIComponent(note.path));
  await p.getByTitle("Pin for compare", { exact: true }).tap();
  await p.goto(origin + "/?entry=" + encodeURIComponent(second.path));
  await p.getByTitle("Pin for compare", { exact: true }).tap();
  await p.getByRole("button", { name: "‹ Back", exact: true }).tap();
  await p.getByRole("button", { name: "⇄ Compare", exact: true }).tap();
  await expect(p.locator(".entry-compare-cell")).toHaveCount(2);
  await fits(p.locator(".entry-compare"));
  await p.getByRole("button", { name: "Diff", exact: true }).tap();
  await expect(p.locator(".entry-diff")).toBeVisible();
  await shot("compare");
  await p.getByTitle("Close and clear both pins").tap();
  pass("Comparison and document diff work with touch controls");
  await p.setViewportSize({ width: 1280, height: 900 });
  await expect(p.locator("body")).not.toHaveClass(/mobile/);
  await expect(p.locator("#app > .sidebar")).toBeVisible();
  await expect(p.locator(".topbar .viewmode")).toBeVisible();
  await shot("desktop");
  pass("Desktop navigation survives switching from mobile");
  const sessionContext = await b.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    serviceWorkers: "block",
  });
  await sessionContext.addInitScript(() =>
    localStorage.setItem(
      "panes-v2:workspace:v1",
      JSON.stringify({
        version: 1,
        state: {
          view: "session",
          focusSessionRef: {
            mode: "history",
            runner_id: "mobile-fixture-runner",
            session_id: "mobile-fixture-session",
            project_id: "mobile-fixture-project",
          },
          docks: { focus: null, sidebar: null },
        },
      }),
    ),
  );
  const sessionPage = await sessionContext.newPage();
  sessionPage.on("pageerror", (e) => errors.push(e.message));
  await sessionPage.route(
    "**/sessions/mobile-fixture-session/history",
    (route) =>
      route.fulfill({
        json: [
          {
            info: { id: "mobile-message", role: "assistant" },
            parts: [
              {
                id: "mobile-part",
                type: "text",
                text: "Mobile transcript fixture is readable.",
              },
            ],
          },
        ],
      }),
  );
  await sessionPage.goto(origin);
  await expect(
    sessionPage.getByText("Mobile transcript fixture is readable.", {
      exact: true,
    }),
  ).toBeVisible({ timeout: 30000 });
  const details = sessionPage.getByRole("button", {
    name: "Session details",
    exact: true,
  });
  await details.tap();
  await expect(details).toHaveAttribute("aria-expanded", "true");
  const mainBox = await sessionPage.locator(".session-full-main").boundingBox();
  const metadataBox = await sessionPage.locator(".sidebar-r").boundingBox();
  assert.ok(
    mainBox.width >= 380 && metadataBox.width >= 380,
    JSON.stringify({ mainBox, metadataBox }),
  );
  assert.ok(metadataBox.y >= mainBox.y + mainBox.height - 1);
  await sessionPage.screenshot({ path: join(evidence, "session-details.png") });
  await details.tap();
  await expect(sessionPage.locator(".sidebar-r")).not.toBeVisible();
  await sessionContext.close();
  pass(
    "History transcript and session metadata use full phone width (controlled history fixture)",
  );
  assert.deepEqual(errors, []);
  await writeFile(
    join(evidence, "results.json"),
    JSON.stringify({ origin, project, note: note.path, checks }, null, 2),
  );
  console.log("Evidence:", evidence);
} catch (e) {
  await shot("failure");
  console.log("FAIL UI", (await p.locator("body").innerText()).slice(-4500));
  throw e;
} finally {
  await b.close();
}
