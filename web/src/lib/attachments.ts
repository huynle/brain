/**
 * Attachment helpers for the entry reader — pure, unit-tested in
 * `attachments.test.ts`.
 *
 * The API hands every attachment a `download_url` (and a `text_url` for
 * extracted text), so the client never builds those paths itself. What it
 * does have to decide is *how* to present each one: inline as a picture,
 * or as a file row you can download.
 */
import type { AttachmentReference } from "./types";

/** Content types we render inline as a picture. */
export function isImageAttachment(a: AttachmentReference): boolean {
  return (a.content_type || "").toLowerCase().startsWith("image/");
}

/**
 * SVG is an image, but it is also a document that can carry script and
 * external references. It is rendered through the same sandboxed <img>
 * path as a raster image — never inlined into the DOM — so callers that
 * need to treat it specially can ask.
 */
export function isSvgAttachment(a: AttachmentReference): boolean {
  return (a.content_type || "").toLowerCase().startsWith("image/svg");
}

/** Human byte size for the file rows. */
export function formatBytes(n: number | undefined): string {
  if (n === undefined || n < 0 || !Number.isFinite(n)) return "";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

/** Short type label for a file row ("image/svg+xml" → "svg"). */
export function attachmentKindLabel(a: AttachmentReference): string {
  const ct = (a.content_type || "").toLowerCase();
  if (!ct) return "file";
  const sub = ct.split(";")[0].split("/")[1] || ct;
  // "svg+xml" → "svg", "vnd.openxmlformats-…document" → the last segment.
  return sub.split("+")[0].split(".").pop() || sub;
}

/** Display name: the caption if there is one, else the filename. */
export function attachmentLabel(a: AttachmentReference): string {
  return a.caption || a.filename || a.path || `attachment ${a.id}`;
}

/**
 * Resolve a markdown image `src` against an entry's attachments.
 *
 * Entry bodies are written by agents and by hand, so the same picture gets
 * referenced three different ways. All three land on the attachment's
 * `download_url`:
 *
 *   ![x](attachment:12)        — explicit attachment id
 *   ![x](gradient.png)         — bare filename
 *   ![x](./figures/gradient.png) — a path whose basename matches
 *
 * Absolute URLs and data: URIs pass through untouched. Anything that
 * matches no attachment returns null, so the caller can render a
 * placeholder instead of a broken image icon.
 */
export function resolveAttachmentSrc(
  src: string | undefined,
  attachments: readonly AttachmentReference[] | undefined,
): { url: string; external: true } | { attachment: AttachmentReference } | null {
  const raw = (src || "").trim();
  if (!raw) return null;
  if (/^(https?:|data:|blob:)/i.test(raw)) return { url: raw, external: true };

  const list = attachments ?? [];
  const idMatch = raw.match(/^attachment:(.+)$/i);
  if (idMatch) {
    const id = idMatch[1].trim();
    const hit = list.find((a) => a.id === id);
    return hit ? { attachment: hit } : null;
  }

  // Compare on the basename so "./figures/x.png" and "x.png" agree.
  const base = decodeURIComponent(raw.split("?")[0].split("#")[0])
    .split("/")
    .pop();
  if (!base) return null;
  const hit = list.find((a) => a.filename === base || a.path?.endsWith(base));
  return hit ? { attachment: hit } : null;
}

/**
 * Ids of the attachments an entry body already renders inline.
 *
 * The reader shows an attachment strip under the body, but a figure the
 * prose already displays shouldn't appear twice. Scanning the markdown for
 * image refs and resolving each one is cheaper and more honest than asking
 * the renderer to report back what it drew.
 */
export function collectInlinedAttachmentIds(
  content: string,
  attachments: readonly AttachmentReference[],
): Set<string> {
  const ids = new Set<string>();
  if (!content || attachments.length === 0) return ids;
  // ![alt](src "title") — src runs to the first space or closing paren.
  const re = /!\[[^\]]*\]\(\s*([^)\s]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(content)) !== null) {
    const hit = resolveAttachmentSrc(m[1], attachments);
    if (hit && "attachment" in hit) ids.add(hit.attachment.id);
  }
  return ids;
}
