import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  attachmentDisplayLabel,
  attachmentDisplayUrl,
  isImageAttachment,
} from "./attachments";
import type { AttachmentReference } from "./types";

test("isImageAttachment detects images from content_type only", () => {
  assert.equal(isImageAttachment({ id: "image-1", content_type: "image/png" }), true);
  assert.equal(isImageAttachment({ id: "file-1", content_type: "application/pdf", filename: "scan.png" }), false);
  assert.equal(isImageAttachment({ id: "file-2", type: "image/jpeg" }), false);
});

test("attachmentDisplayLabel prefers caption, then filename, then id", () => {
  assert.equal(attachmentDisplayLabel({ id: "att-1", caption: "Whiteboard", filename: "board.png" }), "Whiteboard");
  assert.equal(attachmentDisplayLabel({ id: "att-2", filename: "notes.pdf" }), "notes.pdf");
  assert.equal(attachmentDisplayLabel({ id: "att-3" }), "att-3");
});

test("attachmentDisplayUrl prefers download_url and preserves existing query params when adding token", () => {
  const attachment: AttachmentReference = {
    id: "att-1",
    download_url: "/api/v1/attachments/att-1/content?project_id=brain-api",
  };

  assert.equal(
    attachmentDisplayUrl(attachment, { token: "tok en" }),
    "/api/v1/attachments/att-1/content?project_id=brain-api&token=tok+en",
  );
});

test("attachmentDisplayUrl does not duplicate an existing token query parameter", () => {
  const attachment: AttachmentReference = {
    id: "att-1",
    download_url: "/api/v1/attachments/att-1/content?token=from-backend&project_id=brain-api",
  };

  assert.equal(
    attachmentDisplayUrl(attachment, { token: "from-frontend" }),
    "/api/v1/attachments/att-1/content?token=from-backend&project_id=brain-api",
  );
});

test("attachmentDisplayUrl builds fallback only when enough information exists", () => {
  assert.equal(
    attachmentDisplayUrl({ id: "att/1" }, { token: "secret" }),
    "/api/v1/attachments/att%2F1/content?token=secret",
  );
  assert.equal(attachmentDisplayUrl({ id: "" }, { token: "secret" }), undefined);
  assert.equal(attachmentDisplayUrl(undefined, { token: "secret" }), undefined);
});
