// List-with-detail/logs layout shared by the Brain and Automations tabs,
// matching the Tasks tab: the list fills the top, and a bottom row splits in a
// Detail pane (T) and/or a Logs pane (z) for the highlighted entry.

import { useUI } from "../../store/ui";
import { Panel } from "./Panel";
import { EntryDetailPane } from "./EntryDetailPane";
import { EntryLogsPane } from "./EntryLogsPane";

export interface LogTarget {
  taskId?: string;
  projectId?: string;
}

export function ListDetail({
  children,
  detailPath,
  logTarget,
}: {
  children: React.ReactNode;
  detailPath: string | null;
  logTarget?: LogTarget | null;
}) {
  const detailVisible = useUI((s) => s.detailVisible);
  const logsVisible = useUI((s) => s.logsVisible);
  return (
    <>
      <Panel focused style={{ flex: 1 }}>
        {children}
      </Panel>
      {(detailVisible || logsVisible) && (
        <div className="tui-bottom" style={{ height: "34vh" }}>
          {detailVisible && (
            <Panel title="Detail" style={{ flex: 1 }}>
              <EntryDetailPane path={detailPath} />
            </Panel>
          )}
          {logsVisible && (
            <Panel title="Logs" meta={logTarget?.taskId} style={{ flex: 1 }}>
              <EntryLogsPane taskId={logTarget?.taskId} projectId={logTarget?.projectId} />
            </Panel>
          )}
        </div>
      )}
    </>
  );
}
