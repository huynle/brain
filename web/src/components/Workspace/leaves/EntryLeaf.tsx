/**
 * panes-v2 EntryLeaf — a Brain entry reader docked as a Focus pane.
 *
 * Target: `{ path: string }` (an entry path or 8-char short id).
 * In-pane entry links open as *new* leaves (tabbed onto the last
 * focused pane) so a side-by-side layout built by the user survives
 * navigation.
 */
import { EntryReader } from "../EntryReader";
import { ErrorState } from "../../common/ErrorState";
import { useWorkspace } from "../../../store/workspace";
import { entryBasename } from "../../../lib/entries";

export function EntryLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const path = (target.path as string | undefined) ?? "";

  if (!path) {
    return (
      <ErrorState
        error="This pane has no entry path."
        title="No entry"
      />
    );
  }

  return (
    <EntryReader
      path={path}
      onOpenEntry={(ref) =>
        openInFocus("entry", { path: ref }, entryBasename(ref))
      }
    />
  );
}
