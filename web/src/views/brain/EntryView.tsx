import { lazy, Suspense, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Modal } from "../../components/common/Modal";
import { AttachmentGallery } from "../../components/layout/AttachmentGallery";
import { Pill } from "../../components/common/Badge";
import { Loading, ErrorState } from "../../components/common/states";
import { getEntry } from "../../lib/api";
import { relativeTime } from "../../lib/format";

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
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
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
