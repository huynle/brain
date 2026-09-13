/**
 * EntryAttachments — the attachment strip below an entry's body.
 *
 * Pictures (including SVG) render inline as a figure with its caption;
 * everything else becomes a file row with an in-page preview and download. Attachments the
 * body already displays inline via markdown are skipped, so a figure
 * referenced in prose doesn't also appear again in the strip.
 *
 * Extracted text (`derived_text`) is folded away behind a disclosure —
 * it's how the PDF/image extractor makes a binary searchable, and it is
 * usually long enough to bury the entry if shown by default.
 */
import { AttachmentLink } from "./AttachmentPreview";
import { useState } from "react";
import { AttachmentImage } from "./AttachmentImage";
import { useAttachmentBlob } from "../../hooks/useAttachmentBlob";
import {
  attachmentKindLabel,
  attachmentLabel,
  formatBytes,
  isImageAttachment,
} from "../../lib/attachments";
import type { AttachmentReference } from "../../lib/types";

export function EntryAttachments({
  attachments,
  /** Ids already rendered inline in the markdown body — skipped here. */
  inlinedIds,
}: {
  attachments: readonly AttachmentReference[];
  inlinedIds?: ReadonlySet<string>;
}): JSX.Element | null {
  const shown = attachments.filter((a) => !inlinedIds?.has(a.id));
  if (shown.length === 0) return null;

  const images = shown.filter(isImageAttachment);
  const files = shown.filter((a) => !isImageAttachment(a));

  return (
    <div className="entry-attachments">
      <div className="entry-attachments-label">
        Attachments ({shown.length})
      </div>
      {images.length > 0 && (
        <div className="att-gallery">
          {images.map((a) => (
            <figure key={a.id} className="att-figure">
              <AttachmentImage attachment={a} />
              <figcaption className="att-figcaption">
                {a.caption || a.filename}
                <span className="att-figmeta">
                  {attachmentKindLabel(a)}
                  {a.size ? ` · ${formatBytes(a.size)}` : ""}
                </span>
              </figcaption>
            </figure>
          ))}
        </div>
      )}
      {files.map((a) => (
        <AttachmentFileRow key={a.id} attachment={a} />
      ))}
    </div>
  );
}

function AttachmentFileRow({
  attachment,
}: {
  attachment: AttachmentReference;
}): JSX.Element {
  const [textOpen, setTextOpen] = useState(false);
  const text = attachment.derived_text;
  const hasText = text?.status === "ready" && !!text.text;

  return (
    <div className="att-file">
      <div className="att-file-row">
        <span className="entry-type">{attachmentKindLabel(attachment)}</span>
        <span className="att-file-name" title={attachment.filename}>
          {attachmentLabel(attachment)}
        </span>
        <span className="att-file-size">{formatBytes(attachment.size)}</span>
        {hasText && (
          <button
            className={`entry-act ${textOpen ? "active" : ""}`}
            onClick={() => setTextOpen((v) => !v)}
            title="Extracted text"
          >
            Text
          </button>
        )}
        <AttachmentLink attachment={attachment}>Preview</AttachmentLink>
        <AttachmentDownload attachment={attachment} />
      </div>
      {textOpen && hasText && <pre className="att-file-text">{text!.text}</pre>}
    </div>
  );
}

/** Download button. The bytes come through the authed fetch, so this is a
 *  blob link rather than an href straight at the API. */
function AttachmentDownload({
  attachment,
}: {
  attachment: AttachmentReference;
}): JSX.Element | null {
  const { url, load } = useAttachmentBlob(attachment.download_url, false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  if (!attachment.download_url) return null;
  return (
    <>
      <button
        type="button"
        className="entry-act"
        disabled={busy}
        title="Download"
        aria-label={`Download ${attachment.filename || "attachment"}`}
        onClick={async () => {
          setBusy(true);
          setError(false);
          try {
            const href = url ?? (await load());
            if (!href) throw new Error("Missing download");
            const a = document.createElement("a");
            a.href = href;
            a.download = attachment.filename || `attachment-${attachment.id}`;
            document.body.append(a);
            a.click();
            a.remove();
          } catch {
            setError(true);
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? "…" : "↓"}
      </button>
      {error && <span role="alert">Download failed. Try again.</span>}
    </>
  );
}
