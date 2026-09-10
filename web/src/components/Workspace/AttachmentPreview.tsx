import { lazy, Suspense, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useAttachmentBlob } from "../../hooks/useAttachmentBlob";
import { attachmentLabel, attachmentPreviewKind } from "../../lib/attachments";
import type { AttachmentReference } from "../../lib/types";
import "../../styles/attachment-preview.css";

const PdfPreview = lazy(() => import("./PdfPreview"));

export function AttachmentPreview({
  attachment,
  onClose,
}: {
  attachment: AttachmentReference;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const title = useId();
  const restoreFocus = useRef(document.activeElement);
  const { url, loading, error } = useAttachmentBlob(attachment.download_url);
  const kind = attachmentPreviewKind(attachment);
  const label = attachmentLabel(attachment);
  useEffect(() => {
    const previous = restoreFocus.current;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    dialog.current?.showModal();
    return () => {
      document.body.style.overflow = overflow;
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus();
    };
  }, []);
  return createPortal(
    <dialog
      ref={dialog}
      className="attachment-preview"
      aria-labelledby={title}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          const r = e.currentTarget.getBoundingClientRect();
          if (
            e.clientX < r.left ||
            e.clientX > r.right ||
            e.clientY < r.top ||
            e.clientY > r.bottom
          )
            onClose();
        }
      }}
    >
      <header>
        <h2 id={title}>{label}</h2>
        <button
          type="button"
          autoFocus
          onClick={onClose}
          aria-label="Close attachment preview"
        >
          Close
        </button>
      </header>
      <div className="attachment-preview-body">
        {loading ? (
          <p role="status">Loading attachment…</p>
        ) : error || !url ? (
          <p role="alert">Unable to load this attachment.</p>
        ) : kind === "image" ? (
          <img
            src={url}
            alt={label}
            onError={(e) => {
              e.currentTarget.alt = "Unable to display " + label;
            }}
          />
        ) : kind === "video" ? (
          <video src={url} controls preload="metadata" aria-label={label} />
        ) : kind === "audio" ? (
          <audio src={url} controls preload="metadata" aria-label={label} />
        ) : kind === "pdf" ? (
          <Suspense fallback={<p role="status">Preparing PDF…</p>}>
            <PdfPreview url={url} label={label} />
          </Suspense>
        ) : kind === "text" ? (
          <DocumentPreview url={url} />
        ) : (
          <p>
            A preview is not available for this file type. You can download it
            below.
          </p>
        )}
      </div>
      <footer>
        {url && (
          <a
            href={url}
            download={attachment.filename || `attachment-${attachment.id}`}
          >
            Download attachment
          </a>
        )}
      </footer>
    </dialog>,
    document.body,
  );
}
function DocumentPreview({ url }: { url: string }) {
  const [content, setContent] = useState<string | null>(null);
  const [error, setError] = useState(false);
  useEffect(() => {
    let live = true;
    const controller = new AbortController();
    void fetch(url, { signal: controller.signal })
      .then((r) => r.blob())
      .then(async (blob) => {
        const value = await blob.slice(0, 1024 * 1024).text();
        if (live)
          setContent(
            value +
              (blob.size > 1024 * 1024
                ? "\n\n[Preview limited to 1 MiB. Download for the full file.]"
                : ""),
          );
      })
      .catch(() => {
        if (live) setError(true);
      });
    return () => {
      live = false;
      controller.abort();
    };
  }, [url]);
  if (error)
    return (
      <p role="alert">
        Unable to preview this document. Download it to view it.
      </p>
    );
  if (content === null) return <p role="status">Preparing preview…</p>;
  return <pre>{content}</pre>;
}
export function AttachmentLink({
  attachment,
  children,
}: {
  attachment: AttachmentReference;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        className="attachment-inline-link"
        onClick={() => setOpen(true)}
      >
        {children}
      </button>
      {open && (
        <AttachmentPreview
          attachment={attachment}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}
