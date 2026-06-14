// List-with-detail layout shared by the Brain and Automations tabs, matching
// the Tasks tab: the list fills the top, and a detail pane splits in along the
// bottom when enabled. Toggle the detail pane with T (handled in each view).

import { useUI } from "../../store/ui";
import { Panel } from "./Panel";
import { EntryDetailPane } from "./EntryDetailPane";

export function ListDetail({
  children,
  detailPath,
}: {
  children: React.ReactNode;
  detailPath: string | null;
}) {
  const detailVisible = useUI((s) => s.detailVisible);
  return (
    <>
      <Panel focused style={{ flex: 1 }}>
        {children}
      </Panel>
      {detailVisible && (
        <div className="tui-bottom" style={{ height: "34vh" }}>
          <Panel title="Detail" style={{ flex: 1 }}>
            <EntryDetailPane path={detailPath} />
          </Panel>
        </div>
      )}
    </>
  );
}
