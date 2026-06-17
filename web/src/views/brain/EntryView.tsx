import { lazy, Suspense, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Modal } from "../../components/common/Modal";
import { Pill } from "../../components/common/Badge";
import { Loading, ErrorState } from "../../components/common/states";
import { getEntry } from "../../lib/api";
import { useAuth } from "../../lib/auth";
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
  const token = useAuth((s) => s.token);
  const q = useQuery({
    queryKey: ["entry", path],
    queryFn: () => getEntry(path),
  });

  const attachmentUrl = (id: string) => {
    const base = `/api/v1/attachments/${encodeURIComponent(id)}/content`;
    return token ? `${base}?token=${encodeURIComponent(token)}` : base;
  };

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
            {q.data.attachments && q.data.attachments.length > 0 && (
              <div className="field">
                <label>Attachments</label>
                <div className="col" style={{ gap: "0.35rem" }}>
                  {q.data.attachments.map((a) => (
                    <a
                      key={a.id}
                      className="btn sm"
                      href={attachmentUrl(a.id)}
                      target="_blank"
                      rel="noopener noreferrer"
                      style={{ justifyContent: "flex-start", gap: "0.5rem" }}
                    >
                      📎 {a.filename || a.path || a.id}
                      {a.type && <span className="faint">· {a.type}</span>}
                    </a>
                  ))}
                </div>
              </div>
            )}
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
