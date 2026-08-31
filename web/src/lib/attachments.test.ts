import { test } from "node:test";
import assert from "node:assert/strict";
import {
  attachmentKindLabel,
  attachmentLabel,
  collectInlinedAttachmentIds,
  formatBytes,
  isImageAttachment,
  isSvgAttachment,
  resolveAttachmentSrc,
} from "./attachments";
import type { AttachmentReference } from "./types";

function att(over: Partial<AttachmentReference>): AttachmentReference {
  return { id: "1", filename: "gradient.png", content_type: "image/png", ...over };
}

const PNG = att({ id: "1", filename: "gradient.png" });
const SVG = att({
  id: "2",
  filename: "architecture.svg",
  content_type: "image/svg+xml",
});
const PDF = att({
  id: "3",
  filename: "spec.pdf",
  content_type: "application/pdf",
});
const LIST = [PNG, SVG, PDF];

// ─── classification ───────────────────────────────────────────────────

test("attachments: images are classified by content type, SVG included", () => {
  assert.equal(isImageAttachment(PNG), true);
  assert.equal(isImageAttachment(SVG), true);
  assert.equal(isImageAttachment(PDF), false);
  assert.equal(isSvgAttachment(SVG), true);
  assert.equal(isSvgAttachment(PNG), false);
  // A missing content_type must not be guessed into an <img>.
  assert.equal(isImageAttachment(att({ content_type: undefined })), false);
});

test("attachments: kind label is the readable half of the content type", () => {
  assert.equal(attachmentKindLabel(SVG), "svg");
  assert.equal(attachmentKindLabel(PDF), "pdf");
  assert.equal(attachmentKindLabel(att({ content_type: "text/csv" })), "csv");
  assert.equal(attachmentKindLabel(att({ content_type: undefined })), "file");
});

test("attachments: label prefers the caption, then the filename", () => {
  assert.equal(attachmentLabel(att({ caption: "Figure 1" })), "Figure 1");
  assert.equal(attachmentLabel(PNG), "gradient.png");
  assert.equal(
    attachmentLabel(att({ id: "9", filename: undefined })),
    "attachment 9",
  );
});

test("attachments: byte sizes round to a readable unit", () => {
  assert.equal(formatBytes(0), "0 B");
  assert.equal(formatBytes(900), "900 B");
  // One decimal only below 10 — "79 KB" reads better than "79.5 KB".
  assert.equal(formatBytes(81379), "79 KB");
  assert.equal(formatBytes(3 * 1024 + 512), "3.5 KB");
  assert.equal(formatBytes(5 * 1024 * 1024), "5.0 MB");
  assert.equal(formatBytes(undefined), "");
});

// ─── markdown src resolution ──────────────────────────────────────────

test("attachments: a bare filename resolves to its attachment", () => {
  const r = resolveAttachmentSrc("gradient.png", LIST);
  assert.ok(r && "attachment" in r && r.attachment.id === "1");
});

test("attachments: an attachment: id ref resolves", () => {
  const r = resolveAttachmentSrc("attachment:2", LIST);
  assert.ok(r && "attachment" in r && r.attachment.id === "2");
  // An id that names nothing is a miss, not a wrong picture.
  assert.equal(resolveAttachmentSrc("attachment:99", LIST), null);
});

test("attachments: a relative path matches on its basename", () => {
  const r = resolveAttachmentSrc("./figures/architecture.svg", LIST);
  assert.ok(r && "attachment" in r && r.attachment.id === "2");
});

test("attachments: absolute and data URLs pass through untouched", () => {
  const http = resolveAttachmentSrc("https://example.com/x.png", LIST);
  assert.deepEqual(http, { url: "https://example.com/x.png", external: true });
  const data = resolveAttachmentSrc("data:image/png;base64,AAAA", []);
  assert.ok(data && "external" in data);
});

test("attachments: an unmatched relative src is a miss, not a broken img", () => {
  // Returning null lets the renderer say "missing image" instead of
  // pointing <img> at the SPA origin and showing a broken-image icon.
  assert.equal(resolveAttachmentSrc("nope.png", LIST), null);
  assert.equal(resolveAttachmentSrc("", LIST), null);
  assert.equal(resolveAttachmentSrc(undefined, LIST), null);
});

// ─── inline detection ─────────────────────────────────────────────────

test("attachments: ids used inline are detected so the strip can skip them", () => {
  const md = [
    "# Report",
    "",
    "![The gradient](gradient.png)",
    "",
    'Some prose, then ![Arch](attachment:2 "titled").',
  ].join("\n");
  const ids = collectInlinedAttachmentIds(md, LIST);
  assert.deepEqual([...ids].sort(), ["1", "2"]);
  // The PDF is never shown inline, so it stays in the strip.
  assert.equal(ids.has("3"), false);
});

test("attachments: a body with no images inlines nothing", () => {
  assert.equal(collectInlinedAttachmentIds("plain text", LIST).size, 0);
  assert.equal(collectInlinedAttachmentIds("![x](gradient.png)", []).size, 0);
});
