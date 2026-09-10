import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
const origin = process.env.BRAIN_READER_TEST_URL ?? "http://localhost:3333";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
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
const project = "attachment-preview-" + Date.now();
const evidence = join(tmpdir(), project);
await mkdir(evidence);
const results = [];
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
const entry = await request("/api/v1/entries", {
  project,
  type: "scratch",
  status: "draft",
  title: "In-page attachment preview",
  content:
    "Click the diagram to enlarge it here.\n\n![Preview diagram](diagram.svg)\n\n[Read the PDF](sample.pdf) · [View text](notes.txt)",
});
function pdf() {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 250] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  const stream = "BT /F1 20 Tf 30 180 Td (Inline PDF preview) Tj ET";
  objects.push(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`);
  objects.push(
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 250] /Resources << /Font << /F1 4 0 R >> >> /Contents 7 0 R >>",
  );
  const second = "BT /F1 20 Tf 30 180 Td (Second PDF page) Tj ET";
  objects.push(`<< /Length ${second.length} >>\nstream\n${second}\nendstream`);
  let out = "%PDF-1.4\n";
  const offsets = [0];
  for (let i = 0; i < objects.length; i++) {
    offsets.push(out.length);
    out += `${i + 1} 0 obj\n${objects[i]}\nendobj\n`;
  }
  const xref = out.length;
  out += `xref\n0 ${offsets.length}\n0000000000 65535 f \n`;
  for (const offset of offsets.slice(1))
    out += `${String(offset).padStart(10, "0")} 00000 n \n`;
  return (
    out +
    `trailer\n<< /Size ${offsets.length} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  );
}
const attachments = {};
for (const [filename, type, content] of [
  [
    "diagram.svg",
    "image/svg+xml",
    '<svg xmlns="http://www.w3.org/2000/svg" width="800" height="420"><rect width="800" height="420" rx="20" fill="#142f46"/><text x="60" y="160" fill="#b9e8ff" font-size="42">Attachment preview</text><text x="60" y="230" fill="white" font-size="25">Stay in the document. Close to continue reading.</text><script>window.attachmentUnsafe=true</script></svg>',
  ],
  ["sample.pdf", "application/pdf", pdf()],
  ["notes.txt", "text/plain", "Attachment text displayed in the page."],
  [
    "markup.html",
    "text/html",
    "<h1>Inert HTML</h1><script>window.attachmentUnsafe=true</script>",
  ],
  [
    "archive.bin",
    "application/octet-stream",
    new Uint8Array([0, 255, 0, 255, 5, 30]),
  ],
]) {
  const form = new FormData();
  form.set("project_id", project);
  form.set(
    "file",
    new Blob(
      [
        content,
        type.includes("svg") || type.includes("html")
          ? `\n<!-- ${project} -->`
          : type === "application/pdf"
            ? `\n% ${project}`
            : `\n${project}`,
      ],
      { type },
    ),
    filename,
  );
  const response = await fetch(origin + "/api/v1/attachments", {
    method: "POST",
    body: form,
  });
  assert.ok(response.ok, await response.clone().text());
  const a = (await response.json()).attachment;
  attachments[filename] = a;
  await request(
    `/api/v1/entries/${entry.id}/attachments?project_id=${project}`,
    { attachment: { id: a.id, role: "inline" } },
  );
}
const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: 1100, height: 850 },
});
const page = await context.newPage();
const errors = [];
const requests = [];
page.on("pageerror", (e) => errors.push(e.message));
page.on("request", (r) => requests.push(r.url()));
const url = origin + "/read.html?entry=" + encodeURIComponent(entry.path);
try {
  await page.goto(url);
  await expect(
    page.getByRole("heading", { name: entry.title, exact: true }),
  ).toBeVisible();
  const image = page.getByRole("button", {
    name: "Preview diagram",
    exact: true,
  });
  await expect(image).toBeVisible();
  assert.ok(
    !requests.some((u) =>
      new URL(u).pathname.includes(
        `/attachments/${attachments["sample.pdf"].id}/content`,
      ),
    ),
    "PDF eagerly fetched",
  );
  await image.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByRole("dialog").getByRole("img")).toBeVisible();
  assert.equal(context.pages().length, 1);
  assert.equal(await page.evaluate(() => window.attachmentUnsafe), undefined);
  await page.screenshot({ path: join(evidence, "image.png") });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(image).toBeFocused();
  pass(
    "Inline image opens in-page with keyboard support, safe SVG rendering, Escape close, and focus restoration",
  );
  await page.getByRole("button", { name: "Read the PDF", exact: true }).click();
  await expect(
    page.getByRole("dialog").locator('canvas[data-rendered="true"]'),
  ).toBeVisible();
  await expect(page.getByRole("dialog").getByRole("img")).toHaveAccessibleName(
    /Inline PDF preview/,
  );
  assert.ok(
    await page.locator("canvas").evaluate((c) => {
      const data = c
        .getContext("2d")
        .getImageData(0, 0, c.width, c.height).data;
      let dark = 0;
      for (let i = 0; i < data.length; i += 4)
        if (data[i] < 100 && data[i + 3] > 0) dark++;
      return dark > 100;
    }),
    "PDF canvas must contain drawn text",
  );
  assert.equal(context.pages().length, 1);
  await page.screenshot({ path: join(evidence, "pdf.png") });
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("img")).toHaveAccessibleName(
    /Second PDF page/,
  );
  await expect(page.getByRole("dialog")).toContainText("Page 2 of 2");
  await page
    .getByRole("button", { name: "Previous page", exact: true })
    .click();
  await expect(page.getByRole("dialog").getByRole("img")).toHaveAccessibleName(
    /Inline PDF preview/,
  );
  await page.getByRole("button", { name: "Close attachment preview" }).click();
  pass("Markdown PDF link opens an embedded PDF without creating a tab");
  await page.getByRole("button", { name: "View text", exact: true }).click();
  await expect(page.getByRole("dialog").locator("pre")).toContainText(
    "Attachment text displayed",
  );
  await page.getByRole("button", { name: "Close attachment preview" }).click();
  const htmlRow = page.locator(".att-file").filter({ hasText: "markup.html" });
  await htmlRow.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("dialog").locator("pre")).toContainText(
    "<h1>Inert HTML</h1>",
  );
  assert.equal(await page.evaluate(() => window.attachmentUnsafe), undefined);
  await page.keyboard.press("Escape");
  pass(
    "Text and HTML attachment previews display literal text without script execution",
  );
  const binRow = page.locator(".att-file").filter({ hasText: "archive.bin" });
  await binRow.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText(
    "preview is not available",
  );
  const download = page.waitForEvent("download");
  await page
    .getByRole("dialog")
    .getByRole("link", { name: "Download attachment" })
    .click();
  assert.equal((await download).suggestedFilename(), "archive.bin");
  await page.keyboard.press("Escape");
  pass("Unsupported files provide an in-page fallback and working download");
  await page.setViewportSize({ width: 390, height: 844 });
  await image.click();
  await expect(page.getByRole("dialog")).toBeVisible();
  assert.ok(
    await page
      .getByRole("dialog")
      .evaluate((d) => d.getBoundingClientRect().width <= innerWidth),
  );
  await page.screenshot({ path: join(evidence, "mobile.png") });
  await page.keyboard.press("Escape");
  pass("Preview fits a 390px viewport");
  await page.goto(origin + "/?entry=" + encodeURIComponent(entry.path));
  await expect(image).toBeVisible({ timeout: 30000 });
  await image.click();
  await expect(page.getByRole("dialog")).toBeVisible();
  assert.equal(context.pages().length, 1);
  await page.keyboard.press("Escape");
  pass("Dashboard entry reader uses the same in-page preview");
  assert.deepEqual(errors, []);
  await writeFile(
    join(evidence, "results.json"),
    JSON.stringify({ url, entry: entry.path, results }, null, 2),
  );
  console.log("Reader URL:", url);
  console.log("Evidence:", evidence);
} finally {
  await browser.close();
}
