import { lazy, Suspense, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Modal } from "../../components/common/Modal";
import { AttachmentGallery } from "../../components/layout/AttachmentGallery";
import { attachmentDisplayLabel, attachmentDisplayUrl, isImageAttachment } from "../../lib/attachments";
import { Pill } from "../../components/common/Badge";
import { Loading, ErrorState } from "../../components/common/states";
import { getEntry } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import { relativeTime } from "../../lib/format";
import type { AttachmentReference } from "../../lib/types";


const BRAIN_ATTACHMENT_IMAGE_PREFIX = "brain-attachment://";

function markdownUrlTransform(url: string): string {
  if (url.startsWith(BRAIN_ATTACHMENT_IMAGE_PREFIX)) return url;
  return defaultUrlTransform(url);
}

function brainAttachmentIDFromMarkdownUrl(src: string | undefined): string | undefined {
  if (!src?.startsWith(BRAIN_ATTACHMENT_IMAGE_PREFIX)) return undefined;
  const rawID = src.slice(BRAIN_ATTACHMENT_IMAGE_PREFIX.length);
  if (!rawID) return undefined;
  try {
    return decodeURIComponent(rawID);
  } catch {
    return rawID;
  }
}

function InlineMarkdownImage({
  src,
  alt,
  attachments,
  token,
}: {
  src?: string;
  alt?: string;
  attachments?: AttachmentReference[];
  token?: string | null;
}) {
  const attachmentID = brainAttachmentIDFromMarkdownUrl(src);
  if (!attachmentID) {
    return <img src={src} alt={alt || ""} loading="lazy" />;
  }

  const attachment = attachments?.find((a) => a.id === attachmentID);
  const url = attachment && isImageAttachment(attachment)
    ? attachmentDisplayUrl(attachment, { token })
    : undefined;

  if (!attachment || !url) {
    return (
      <span className="entry-inline-attachment-missing">
        Image unavailable: {alt || attachmentID}
      </span>
    );
  }

  const label = alt || attachmentDisplayLabel(attachment);
  return (
    <a
      className="entry-inline-attachment-image-link"
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={`Open image ${label}`}
    >
      <img
        className="entry-inline-attachment-image"
        src={url}
        alt={label}
        loading="lazy"
      />
    </a>
  );
}

// The CodeMirror editor is heavy; load it only when the user edits.
const EntryEditModal = lazy(() =>
  import("./EntryEditModal").then((m) => ({ default: m.EntryEditModal })),
);

export function EntryView({
  path,
  onClose,
}: {
  path: string;
  onClose: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const token = useAuth((s) => s.token);
  const q = useQuery({
    queryKey: ["entry", path],
    queryFn: () => getEntry(path),
  });

  return (
    <>
      <Modal
        title="Entry"
        onClose={onClose}
        onEdit={q.data ? () => setEditing(true) : undefined}
        footer={
          q.data ? (
            <button
              className="btn primary sm"
              style={{ marginLeft: "auto" }}
              onClick={() => setEditing(true)}
            >
              ✎ Edit
            </button>
          ) : undefined
        }
      >
        {q.isLoading ? (
          <Loading />
        ) : q.error ? (
          <ErrorState error={q.error} onRetry={() => void q.refetch()} />
        ) : q.data ? (
          <>
            <h2 style={{ margin: "0 0 0.5rem", fontSize: 17 }}>
              {q.data.title}
            </h2>
            <div
              className="row wrap"
              style={{ gap: "0.35rem", marginBottom: "0.7rem" }}
            >
              <Pill color="var(--purple)">{q.data.type}</Pill>
              {q.data.status && <Pill>{q.data.status}</Pill>}
              {q.data.project_id && (
                <Pill color="var(--cyan)">{q.data.project_id}</Pill>
              )}
              {q.data.modified && (
                <Pill className="faint">
                  updated {relativeTime(q.data.modified)}
                </Pill>
              )}
            </div>
            <div className="mono faint" style={{ fontSize: 11.5, marginBottom: "0.7rem", wordBreak: "break-all" }}>
              {q.data.path}
            </div>
            {q.data.tags && q.data.tags.length > 0 && (
              <div className="row wrap" style={{ gap: "0.3rem", marginBottom: "0.7rem" }}>
                {q.data.tags.map((t) => (
                  <Pill key={t} color="var(--teal)">
                    #{t}
                  </Pill>
                ))}
              </div>
            )}
            <AttachmentGallery attachments={q.data.attachments} />
            <div className="markdown">
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                urlTransform={markdownUrlTransform}
                components={{
                  img: ({ src, alt }) => (
                    <InlineMarkdownImage
                      src={src}
                      alt={alt}
                      attachments={q.data.attachments}
                      token={token}
                    />
                  ),
                }}
              >
                {q.data.content || "_(empty)_"}
              </ReactMarkdown>
            </div>
          </>
        ) : null}
      </Modal>

      {editing && q.data && (
        <Suspense fallback={null}>
          <EntryEditModal
            path={q.data.path}
            title={q.data.title}
            onClose={() => setEditing(false)}
            onSaved={() => void q.refetch()}
          />
        </Suspense>
      )}
    </>
  );
}
