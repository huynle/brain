import { lazy, Suspense, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal } from "../../components/common/Modal";
import { Loading, ErrorState } from "../../components/common/states";
import { getEntryRaw } from "../../lib/api";

const EntryEditModal = lazy(() =>
  import("./EntryEditModal").then((m) => ({ default: m.EntryEditModal })),
);

export function EntryRawViewModal({
  path,
  title,
  onClose,
}: {
  path: string;
  title?: string;
  onClose: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const q = useQuery({
    queryKey: ["entry-raw", path],
    queryFn: () => getEntryRaw(path),
  });

  return (
    <>
      <Modal
        title={title ? `Entry · ${title}` : "Entry"}
        onClose={onClose}
        onEdit={q.data ? () => setEditing(true) : undefined}
        footer={
          q.data ? (
            <button
              className="btn primary sm"
              style={{ marginLeft: "auto" }}
              onClick={() => setEditing(true)}
            >
              e · Edit
            </button>
          ) : undefined
        }
      >
        {q.isLoading ? (
          <Loading label="Loading entry…" />
        ) : q.error ? (
          <ErrorState error={q.error} onRetry={() => void q.refetch()} />
        ) : (
          <pre className="entry-raw-view">{q.data || ""}</pre>
        )}
      </Modal>

      {editing && (
        <Suspense fallback={null}>
          <EntryEditModal
            path={path}
            title={title ?? path}
            onClose={() => setEditing(false)}
            onSaved={() => void q.refetch()}
          />
        </Suspense>
      )}
    </>
  );
}
