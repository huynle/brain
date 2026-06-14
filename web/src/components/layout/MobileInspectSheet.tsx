import { useEffect, useState } from "react";
import { useUI } from "../../store/ui";
import { BottomSheet } from "./BottomSheet";
import { EntryDetailPane } from "./EntryDetailPane";
import { EntryLogsPane } from "./EntryLogsPane";

// Mobile Detail/Logs viewer — the touch replacement for the desktop split
// panes (T/z). Opened by tapping a row; a segmented toggle switches between the
// entry's Detail and its task Logs. Swipe down / tap backdrop to dismiss.
export function MobileInspectSheet() {
  const target = useUI((s) => s.inspect);
  const close = useUI((s) => s.closeInspect);
  const [tab, setTab] = useState<"detail" | "logs">("detail");

  // Reset to Detail whenever a new entry is inspected.
  useEffect(() => {
    setTab("detail");
  }, [target?.path]);

  if (!target) return null;

  const hasLogs = !!target.taskId;

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
    >
      {tab === "detail" ? (
        <EntryDetailPane path={target.path} />
      ) : (
        <EntryLogsPane taskId={target.taskId} projectId={target.projectId} />
      )}
    </BottomSheet>
  );
}
