// List-with-detail/logs layout shared by the Brain and Automations tabs,
// matching the Tasks tab: the list fills the top, and a bottom row splits in a
// Detail pane (T) and/or a Logs pane (z) for the highlighted entry.
//
// Pane focus + keyboard navigation: the consuming view is expected to call
// `usePaneNavigation()` and pass the returned object as `paneNav`. The view
// must also forward `paneNav.handleKey(e)` from inside its useViewKeyboard
// handler so Tab/Shift-Tab and vim-style scroll keys work. We can't install
// our own useViewKeyboard here because that hook's `activeHandler` is a
// module-level singleton — the last-mounted handler wins, which would race
// with the inner view's handler. Making paneNav an explicit prop keeps the
// wiring testable and obvious.

import { useUI } from "../../store/ui";
import type { PaneNavigation } from "../../lib/usePaneNavigation";
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
  paneNav,
}: {
  children: React.ReactNode;
  detailPath: string | null;
  logTarget?: LogTarget | null;
  // Required for keyboard pane focus + scroll. Get one from
  // `usePaneNavigation()` in the consuming view.
  paneNav: PaneNavigation;
}) {
  const detailVisible = useUI((s) => s.detailVisible);
  const logsVisible = useUI((s) => s.logsVisible);

  return (
    <>
      <Panel {...paneNav.tasksPaneProps} style={{ flex: 1 }}>
        {children}
      </Panel>
      {(detailVisible || logsVisible) && (
        <div className="tui-bottom" style={{ height: "34vh" }}>
          {detailVisible && (
            <Panel title="Detail" {...paneNav.detailPaneProps} style={{ flex: 1 }}>
              <EntryDetailPane path={detailPath} />
            </Panel>
          )}
          {logsVisible && (
            <Panel
              title="Logs"
              meta={logTarget?.taskId}
              {...paneNav.logsPaneProps}
              style={{ flex: 1 }}
            >
              <EntryLogsPane taskId={logTarget?.taskId} projectId={logTarget?.projectId} />
            </Panel>
          )}
        </div>
      )}
    </>
  );
}
