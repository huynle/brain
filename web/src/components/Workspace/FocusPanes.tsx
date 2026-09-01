/**
 * panes-v2 Focus workspace root.
 *
 * Renders the current `dockTree` as a recursive `<PaneNode/>` tree.
 * When the tree is null, shows a friendly empty state with a drop
 * zone so users can drag their first pane in from the sidebar.
 *
 * Both branches are drop targets. The empty state is the only place a
 * first pane can land; the populated branch carries a catch-all on the
 * `.p2-dock` wrapper so a drop that misses a pane's zones — the splitter
 * between panes, the 3px gutters, the padding around the dock — still
 * goes somewhere instead of evaporating. Pane zones `stopPropagation`,
 * so the catch-all only ever sees genuine misses. All of it routes
 * through `useDockDrop`, the one reducer every dock surface shares.
 */
import React, { useCallback } from "react";
import { useWorkspace } from "../../store/workspace";
import { PaneNode } from "./PaneNode";
import { useDockDrop } from "./useDockDrop";

export function FocusPanes(): JSX.Element {
  const dockTree = useWorkspace((s) => s.docks.focus);
  const { dragActive, drop } = useDockDrop("focus");

  const [dragover, setDragover] = React.useState(false);

  const handleDragOver = useCallback(
    (e: React.DragEvent) => {
      // Preventing default is REQUIRED to enable drop targets — but
      // only for a payload we'll actually take, so an "assign" drag
      // doesn't get a drop cursor it can't cash in.
      if (!dragActive) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      setDragover(true);
    },
    [dragActive],
  );

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    // Only clear when leaving the element itself, not a bubbling child.
    if (e.currentTarget === e.target) setDragover(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      setDragover(false);
      // `null` target: no pane was aimed at, so the store's last-touched
      // rule places it. A "pane-leaf" payload from the sidebar dock is
      // moved here rather than duplicated — see useDockDrop.
      drop(null, "center", e);
    },
    [drop],
  );

  // A drag abandoned over the dock (Escape, or a drop on a pane zone,
  // which stops propagation before `dragleave` reaches this wrapper)
  // never sends the falling-edge event that would clear this.
  React.useEffect(() => {
    if (!dragActive) setDragover(false);
  }, [dragActive]);

  if (dockTree === null) {
    return (
      <div
        className={"p2-dock-empty" + (dragover ? " dragover" : "")}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        aria-label="Focus workspace empty state"
      >
        <div className="p2-dock-empty__icon" aria-hidden="true">
          ⌗
        </div>
        <div>Focus workspace is empty.</div>
        {/*
          The old copy advertised only the drag, which is the one route
          nobody finds and the one that does not exist on a touch
          screen. Lead with the verb that builds a layout for you —
          Focus is for watching several things at once, and saying so is
          most of what it needed.
        */}
        <div style={{ fontSize: 11, maxWidth: 380, lineHeight: 1.6 }}>
          This is where you watch work run — several panes side by side,
          at a width where each is actually readable.
          <br />
          <b>Watch in Focus</b> on a task opens its transcript beside its
          raw log. <b>Watch tasks in Focus</b> on a feature opens one
          live session per running task.
          <br />
          You can also drag any pane or sidebar row in here, or send one
          over from the side panel with its <b>⤢</b> button.
        </div>
      </div>
    );
  }

  return (
    <div
      className="p2-dock"
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <PaneNode dockId="focus" node={dockTree} />
    </div>
  );
}
