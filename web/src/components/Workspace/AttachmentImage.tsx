/**
 * AttachmentImage — one attachment rendered as a picture.
 *
 * Bytes arrive as an object URL (auth-gated fetch, see useAttachmentBlob),
 * so this handles the three states a plain <img> cannot: still fetching,
 * failed, and loaded. Clicking opens the full-size image in a new tab.
 *
 * SVG goes through <img> like everything else rather than being inlined
 * into the DOM. An entry body is agent-written content, and an inlined
 * <svg> would run its scripts and load its external refs inside the app's
 * origin; <img> renders it inert.
 *
 * The placeholder states are <span>s, not <div>s: markdown wraps an image
 * in a <p>, and a block element there is invalid nesting that makes the
 * browser close the paragraph early and wreck the layout around it.
 */
import { useAttachmentBlob } from "../../hooks/useAttachmentBlob";
import { attachmentLabel } from "../../lib/attachments";
import type { AttachmentReference } from "../../lib/types";

export function AttachmentImage({
  attachment,
  className = "",
  onOpen,
}: {
  attachment: AttachmentReference;
  className?: string;
  onOpen?: (url: string) => void;
}): JSX.Element {
  const { url, loading, error } = useAttachmentBlob(attachment.download_url);
  const label = attachmentLabel(attachment);

  if (error) {
    return (
      <span className={`att-image att-image--error ${className}`}>
        <span className="att-image-icon">⚠</span>
        <span>Couldn't load {attachment.filename || label}</span>
      </span>
    );
  }
  if (loading || !url) {
    return (
      <span className={`att-image att-image--loading ${className}`}>
        <span className="att-image-icon">▦</span>
        <span>{label}</span>
      </span>
    );
  }
  return (
    <img
      className={`att-image ${className}`}
      src={url}
      alt={label}
      title={`${label} — click to open full size`}
      loading="lazy"
      onClick={() => (onOpen ? onOpen(url) : window.open(url, "_blank"))}
    />
  );
}
