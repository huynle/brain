import type { AttachmentReference } from "./types";

const ATTACHMENT_CONTENT_BASE = "/api/v1/attachments";

export function isImageAttachment(attachment: AttachmentReference): boolean {
  return Boolean(attachment.content_type?.startsWith("image/"));
}

export function attachmentDisplayLabel(attachment: AttachmentReference): string {
  return attachment.caption || attachment.filename || attachment.id;
}

export function attachmentDisplayUrl(
  attachment: AttachmentReference | undefined,
  opts: { token?: string | null } = {},
): string | undefined {
  const source = attachment?.download_url || fallbackAttachmentUrl(attachment);
  if (!source) return undefined;
  if (!opts.token) return source;

  const url = new URL(source, "http://brain.local");
  if (!url.searchParams.has("token")) {
    url.searchParams.set("token", opts.token);
  }

  return url.pathname + url.search + url.hash;
}

function fallbackAttachmentUrl(attachment: AttachmentReference | undefined): string | undefined {
  if (!attachment?.id) return undefined;
  return `${ATTACHMENT_CONTENT_BASE}/${encodeURIComponent(attachment.id)}/content`;
}
