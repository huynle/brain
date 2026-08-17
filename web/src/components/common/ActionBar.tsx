/**
 * ActionBar — renders an ActionDescriptor list as a modal footer.
 *
 * The fourth surface over the same registry (menu, sheet, palette being
 * the others). Before this, TaskModal's footer hand-built its own buttons,
 * which is how "Run now" ended up labelled differently from the context
 * menu's "Run task now" and why Resume was buried behind a generic
 * "Actions…" button.
 *
 * Footers have limited room, so only the primary groups render as
 * buttons; the rest stay one click away behind "More…". Disabled actions
 * keep their reason as a tooltip rather than disappearing.
 *
 * "More…" is device-aware: on desktop it opens a compact context menu
 * anchored to the button (the same menu right-click shows on rows); the
 * full-width bottom sheet — correct for thumbs, comically wide on a
 * monitor — is reserved for touch.
 */
import { useRef, useState } from "react";

import { ActionSheet } from "./ActionSheet";
import { useContextMenu } from "./ContextMenu";
import { useIsMobile } from "../../hooks/useIsMobile";
import { toMenuItems } from "../../hooks/useRowActions";
import { isEnabled, type ActionDescriptor } from "../../lib/actions/types";

export interface ActionBarProps {
  actions: readonly ActionDescriptor[];
  onRun: (action: ActionDescriptor) => void;
  /**
   * Ids to surface as buttons, in order. Anything else goes under
   * "More…". Ids that do not exist in `actions` are skipped, so callers
   * can name a conditional action (like resume) unconditionally.
   */
  primary?: readonly string[];
}

export function ActionBar({
  actions,
  onRun,
  primary = ["run", "resume"],
}: ActionBarProps): JSX.Element {
  const [showSheet, setShowSheet] = useState(false);
  const moreRef = useRef<HTMLButtonElement | null>(null);
  const ctx = useContextMenu();
  const isMobile = useIsMobile();

  const byId = new Map(actions.map((a) => [a.id, a]));
  const primaryActions = primary
    .map((id) => byId.get(id))
    .filter((a): a is ActionDescriptor => !!a);
  const primaryIds = new Set(primaryActions.map((a) => a.id));
  const rest = actions.filter((a) => !primaryIds.has(a.id));

  const openMore = () => {
    if (isMobile) {
      setShowSheet(true);
      return;
    }
    const rect = moreRef.current?.getBoundingClientRect();
    // Anchor above-left of the button; ContextMenu clamps to viewport.
    ctx.open(rect?.left ?? 0, rect?.top ?? 0, toMenuItems(rest, onRun));
  };

  return (
    <>
      {rest.length > 0 && (
        <button ref={moreRef} onClick={openMore}>
          More…
        </button>
      )}
      {primaryActions.map((a) => {
        const enabled = isEnabled(a);
        return (
          <button
            key={a.id}
            disabled={!enabled}
            title={a.disabledReason || undefined}
            className={a.danger ? "danger" : undefined}
            onClick={() => onRun(a)}
          >
            {a.label}
          </button>
        );
      })}

      {ctx.menu}
      {showSheet && (
        <ActionSheet
          title="More actions"
          actions={rest}
          onSelect={onRun}
          onClose={() => setShowSheet(false)}
        />
      )}
    </>
  );
}
