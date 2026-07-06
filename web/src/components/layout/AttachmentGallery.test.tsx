import React from "react";
import { strict as assert } from "node:assert";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AttachmentGallery } from "./AttachmentGallery";
import type { AttachmentReference } from "../../lib/types";

(globalThis as typeof globalThis & { React: typeof React }).React = React;

test("renders image attachments lazily and file attachments with metadata", () => {
  const attachments: AttachmentReference[] = [
    {
      id: "img-1",
      filename: "diagram.png",
      caption: "System diagram",
      content_type: "image/png",
      size: 1536,
      download_url: "/files/diagram.png",
    },
    {
      id: "file-1",
      filename: "notes.pdf",
      content_type: "application/pdf",
      size: 2048,
      download_url: "/files/notes.pdf",
    },
  ];

  const html = renderToStaticMarkup(<AttachmentGallery attachments={attachments} />);

  assert.match(html, /class="attachment-gallery__grid"/);
  assert.match(html, /loading="lazy"/);
  assert.match(html, /alt="System diagram"/);
  assert.match(html, /href="\/files\/notes\.pdf"/);
  assert.match(html, /aria-label="Download attachment notes\.pdf"/);
  assert.match(html, /notes\.pdf/);
  assert.match(html, /application\/pdf/);
  assert.match(html, /2 KB/);
});


test("renders image buttons with accessible lightbox labels", () => {
  const attachments: AttachmentReference[] = [
    { id: "img-1", filename: "one.png", content_type: "image/png", download_url: "/files/one.png" },
    { id: "img-2", filename: "two.png", content_type: "image/png", download_url: "/files/two.png" },
  ];

  const html = renderToStaticMarkup(<AttachmentGallery attachments={attachments} />);

  assert.match(html, /<button type="button"/);
  assert.match(html, /aria-label="Open image one\.png"/);
  assert.match(html, /aria-label="Open image two\.png"/);
});


test("renders nothing for empty attachment lists", () => {
  assert.equal(renderToStaticMarkup(<AttachmentGallery attachments={[]} />), "");
});

test("renders URL-less image attachments as download fallbacks without empty image sources", () => {
  const attachments: AttachmentReference[] = [
    { id: "", filename: "missing.png", content_type: "image/png" },
    { id: "file-1", filename: "notes.txt", content_type: "text/plain", download_url: "/files/notes.txt" },
  ];

  const html = renderToStaticMarkup(<AttachmentGallery attachments={attachments} />);

  assert.doesNotMatch(html, /<img[^>]+src=/);
  assert.match(html, /Image unavailable/);
  assert.match(html, /missing\.png/);
  assert.match(html, /href="\/files\/notes\.txt"/);
});
