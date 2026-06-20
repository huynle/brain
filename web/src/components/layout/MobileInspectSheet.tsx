import { lazy, Suspense, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useUI } from "../../store/ui";
import { useOpenInControl } from "../../hooks/useOpenInControl";
import { BottomSheet } from "./BottomSheet";
import { EntryDetailPane } from "./EntryDetailPane";
import { EntryLogsPane } from "./EntryLogsPane";

// CodeMirror is heavy — load the editor only when the user edits.
const EntryEditModal = lazy(() =>
  import("../../views/brain/EntryEditModal").then((m) => ({ default: m.EntryEditModal })),
);

// Mobile Detail/Logs viewer — the touch replacement for the desktop split
// panes (T/z). Opened by tapping a row; a segmented toggle switches between the
// entry's Detail and its task Logs. Swipe down / tap backdrop to dismiss.
export function MobileInspectSheet() {
  const target = useUI((s) => s.inspect);
  const close = useUI((s) => s.closeInspect);
  const openInControl = useOpenInControl();
  const [tab, setTab] = useState<"detail" | "logs">("detail");
  const [editing, setEditing] = useState(false);
  const qc = useQueryClient();

  // Reset to Detail whenever a new entry is inspected.
  useEffect(() => {
    setTab("detail");
    setEditing(false);
  }, [target?.path]);

  if (!target) return null;

  const hasLogs = !!target.taskId;

  // The full-file editor renders its own modal over the sheet.
  if (editing) {
    return (
      <Suspense fallback={null}>
        <EntryEditModal
          path={target.path}
          title={target.title}
          onClose={() => setEditing(false)}
          onSaved={() => {
            void qc.invalidateQueries({ queryKey: ["entry-detail", target.path] });
            void qc.invalidateQueries({ queryKey: ["entries"] });
            void qc.invalidateQueries({ queryKey: ["automation-data"] });
          }}
        />
      </Suspense>
    );
  }

  return (
    <BottomSheet
      onClose={close}
      title={
        <div className="insp-tabs">
          <button className={`insp-tab ${tab === "detail" ? "on" : ""}`} onClick={() => setTab("detail")}>
            Detail
          </button>
          <button
            className={`insp-tab ${tab === "logs" ? "on" : ""}`}
            onClick={() => hasLogs && setTab("logs")}
            disabled={!hasLogs}
            title={hasLogs ? "" : "Logs are available for task entries"}
          >
            Logs
          </button>
        </div>
      }
      footer={
        <div className="btn-row" style={{ width: "100%", gap: 8 }}>
          {hasLogs && (
            <button
              className="btn"
              style={{ flex: 1 }}
              onClick={() => {
                const t = target;
                close();
                void openInControl({ taskId: t.taskId, path: t.path, title: t.title });
              }}
            >
              ⊙ Open in Control
            </button>
          )}
          <button
            className="btn primary"
            style={{ flex: 1 }}
            onClick={() => setEditing(true)}
          >
            ✎ Edit
          </button>
        </div>
      }
    >
      {tab === "detail" ? (
        <EntryDetailPane path={target.path} />
      ) : (
        <EntryLogsPane taskId={target.taskId} projectId={target.projectId} />
      )}
    </BottomSheet>
  );
}
