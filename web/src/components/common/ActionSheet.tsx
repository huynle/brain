/**
 * ActionSheet — the touch rendering of an ActionDescriptor list.
 *
 * Same descriptors the desktop context menu renders, presented as a bottom
 * sheet: large tap targets, grouped with separators, disabled entries kept
 * visible with their reason as a subtitle rather than a tooltip (there is
 * no hover on touch, so a tooltip would be invisible).
 */
import { useEffect } from "react";
import { createPortal } from "react-dom";

import { groupActions, isEnabled, type ActionDescriptor } from "../../lib/actions/types";

export interface ActionSheetProps {
  title: string;
  actions: readonly ActionDescriptor[];
  onSelect: (action: ActionDescriptor) => void;
  onClose: () => void;
}

export function ActionSheet({
  title,
  actions,
  onSelect,
  onClose,
}: ActionSheetProps): JSX.Element {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    // Lock the page behind the sheet — otherwise the list scrolls under it.
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [onClose]);

  const groups = groupActions(actions);

  return createPortal(
    <div
      className="sheet-scrim"
      onClick={onClose}
      role="presentation"
    >
      <div
        className="sheet"
        role="dialog"
        aria-label={title}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="sheet-handle" aria-hidden="true" />
        <div className="sheet-title">{title}</div>

        {groups.map((group, gi) => (
          <div key={gi} className="sheet-group">
            {group.map((action) => {
              const enabled = isEnabled(action);
              return (
                <button
                  key={action.id}
                  className={`sheet-item${action.danger ? " danger" : ""}`}
                  disabled={!enabled}
                  onClick={() => {
                    onClose();
                    onSelect(action);
                  }}
                >
                  <span className="sheet-item-label">{action.label}</span>
                  {!enabled && (
                    // Subtitle, not tooltip: touch has no hover, so a
                    // title attribute would be dead weight here.
                    <span className="sheet-item-reason">
                      {action.disabledReason}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        ))}

        <button className="sheet-cancel" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>,
    document.body,
  );
}
