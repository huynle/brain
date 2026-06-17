import React from "react";
import { strict as assert } from "node:assert";
import { test } from "node:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { EntryView } from "./EntryView";
import type { BrainEntry } from "../../lib/types";

(globalThis as typeof globalThis & { React: typeof React }).React = React;

test("renders entry attachments with the shared gallery and preserves markdown", () => {
  const path = "projects/brain-api/report/example.md";
  const entry: BrainEntry = {
    id: "entry-1",
    path,
    title: "Entry with attachments",
    type: "report",
    status: "active",
    content: "## Notes\n\n- Markdown remains intact",
    project_id: "brain-api",
    attachments: [
      {
        id: "img-1",
        filename: "diagram.png",
        content_type: "image/png",
        download_url: "/files/diagram.png",
      },
      {
        id: "file-1",
        filename: "notes.pdf",
        content_type: "application/pdf",
        download_url: "/files/notes.pdf",
      },
    ],
  };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(["entry", path], entry);

  const html = renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <EntryView path={path} onClose={() => undefined} />
    </QueryClientProvider>,
  );

  assert.match(html, /class="attachment-gallery__grid"/);
  assert.match(html, /aria-label="Open image diagram\.png"/);
  assert.match(html, /class="attachment-gallery__file"/);
  assert.match(html, /href="\/files\/notes\.pdf"/);
  assert.match(html, /<h2>Notes<\/h2>/);
  assert.match(html, /<li>Markdown remains intact<\/li>/);
});


test("resolves inline brain attachment image references to authenticated display URLs", () => {
  const path = "projects/brain-api/report/inline-image.md";
  const entry: BrainEntry = {
    id: "entry-inline-image",
    path,
    title: "Entry with inline image",
    type: "report",
    status: "active",
    content: "Inline image: ![System diagram](brain-attachment://img-1)",
    project_id: "brain-api",
    attachments: [
      {
        id: "img-1",
        filename: "diagram.png",
        content_type: "image/png",
        download_url: "/files/diagram.png?project_id=brain-api",
      },
    ],
  };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(["entry", path], entry);

  const html = renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <EntryView path={path} onClose={() => undefined} />
    </QueryClientProvider>,
  );

  assert.match(html, /class="entry-inline-attachment-image-link"/);
  assert.match(html, /href="\/files\/diagram\.png\?project_id=brain-api"/);
  assert.match(html, /src="\/files\/diagram\.png\?project_id=brain-api"/);
  assert.match(html, /alt="System diagram"/);
  assert.match(html, /loading="lazy"/);
  assert.doesNotMatch(html, /src="brain-attachment:\/\/img-1"/);
});

test("renders unresolved inline brain attachment images as graceful fallback text", () => {
  const path = "projects/brain-api/report/missing-inline-image.md";
  const entry: BrainEntry = {
    id: "entry-missing-inline-image",
    path,
    title: "Entry with missing inline image",
    type: "report",
    status: "active",
    content: "Missing image: ![Missing diagram](brain-attachment://missing)",
    project_id: "brain-api",
    attachments: [
      {
        id: "img-1",
        filename: "diagram.png",
        content_type: "image/png",
        download_url: "/files/diagram.png",
      },
    ],
  };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(["entry", path], entry);

  const html = renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <EntryView path={path} onClose={() => undefined} />
    </QueryClientProvider>,
  );

  assert.match(html, /class="entry-inline-attachment-missing"/);
  assert.match(html, /Image unavailable: Missing diagram/);
  assert.doesNotMatch(html, /src="brain-attachment:\/\/missing"/);
});
