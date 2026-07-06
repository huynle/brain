import { useEffect, useRef, useState } from "react";
import { isEditableTarget } from "../../lib/keyboard";

export function Modal({
  title,
  onClose,
  children,
  footer,
  className,
  onEdit,
  storageKey,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  /** Extra class on the dialog, e.g. "sheet-wide" for a larger editor modal. */
  className?: string;
  /** Optional action wired to the modal-level edit shortcut (`e`). */
  onEdit?: () => void;
  /** Stable preference key for remembering modal size on this browser. */
  storageKey?: string;
}) {
  const backdropRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const modalStorageKey = storageKey ?? modalPreferenceKey(title, className);
  const [expanded, setExpanded] = useState(() => readExpandedPreference(modalStorageKey));

  useEffect(() => {
    // Remember what had focus so it can be restored on close — without this
    // a keyboard-driven flow (j/k to a row, open a modal, q to close) dumps
    // focus on <body> and the next keystroke goes nowhere.
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = dialogRef.current;
    const focusables = getFocusableElements(dialog);
    const preferred = dialog?.querySelector<HTMLElement>('[data-autofocus="true"]');
    const target = preferred || focusables[0] || dialog;
    target?.focus({ preventScroll: true });
    return () => {
      if (opener && opener.isConnected) opener.focus({ preventScroll: true });
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const modals = Array.from(document.querySelectorAll(".modal-backdrop"));
      if (modals[modals.length - 1] !== backdropRef.current) return;

      if (e.key === "Tab") {
        const focusables = getFocusableElements(dialogRef.current);
        if (focusables.length === 0) {
          e.preventDefault();
          return;
        }
        const first = focusables[0];
        const last = focusables[focusables.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
          return;
        }
        if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
          return;
        }
        return;
      }

      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
        return;
      }

      if (isEditableTarget(e.target) || e.metaKey || e.altKey) return;

      if (e.key === "q") {
        e.preventDefault();
        onClose();
        return;
      }

      if (e.key === "m") {
        e.preventDefault();
        toggleExpanded();
        return;
      }

      if (e.key === "e" && onEdit) {
        e.preventDefault();
        onEdit();
        return;
      }

      const body = bodyRef.current;
      if (!body) return;

      const line = 48;
      const page = Math.max(line, body.clientHeight * 0.75);
      let top: number | null = null;

      switch (e.key) {
        case "j":
        case "ArrowDown":
          top = body.scrollTop + line;
          break;
        case "k":
        case "ArrowUp":
          top = body.scrollTop - line;
          break;
        case "d":
          if (e.ctrlKey) top = body.scrollTop + page;
          break;
        case "u":
          if (e.ctrlKey) top = body.scrollTop - page;
          break;
        case "g":
          top = 0;
          break;
        case "G":
          top = body.scrollHeight;
          break;
        default:
          return;
      }

      if (top === null) return;
      e.preventDefault();
      body.scrollTo({ top });
    };
    window.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [modalStorageKey, onClose, onEdit]);

  function toggleExpanded() {
    setExpanded((v) => {
      const next = !v;
      writeExpandedPreference(modalStorageKey, next);
      return next;
    });
  }

  const sheetClass = ["sheet", className, expanded ? "sheet-expanded" : ""]
    .filter(Boolean)
    .join(" ");

  return (
    <div
      ref={backdropRef}
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div ref={dialogRef} className={sheetClass} role="dialog" aria-modal="true" aria-label={title} tabIndex={-1}>
        <div className="sheet-header">
          <h2>{title}</h2>
          <button
            className="icon-btn"
            onClick={toggleExpanded}
            aria-label={expanded ? "restore window" : "expand window"}
            title={expanded ? "Restore (m)" : "Expand (m)"}
          >
            {expanded ? "▢" : "□"}
          </button>
          <button className="icon-btn" onClick={onClose} aria-label="close" title="Close (q)">
            ✕
          </button>
        </div>
        <div ref={bodyRef} className="sheet-body">{children}</div>
        {footer && <div className="sheet-footer">{footer}</div>}
      </div>
    </div>
  );
}

const MODAL_SIZE_PREF_PREFIX = "brain.modal.expanded.";

function modalPreferenceKey(title: string, className?: string): string {
  const stableTitle = title.replace(/ · .*/, "");
  const stableClass = className?.trim().split(/\s+/).sort().join(".") || "default";
  return stableClass + "." + stableTitle.toLowerCase().replace(/[^a-z0-9]+/g, "-");
}

function readExpandedPreference(key: string): boolean {
  try {
    return localStorage.getItem(MODAL_SIZE_PREF_PREFIX + key) === "1";
  } catch {
    return false;
  }
}

function writeExpandedPreference(key: string, expanded: boolean) {
  try {
    localStorage.setItem(MODAL_SIZE_PREF_PREFIX + key, expanded ? "1" : "0");
  } catch {
    // Storage can be unavailable in private/restricted contexts; the modal still works.
  }
}

function getFocusableElements(root: HTMLElement | null): HTMLElement[] {
  if (!root) return [];
  return Array.from(
    root.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ).filter((el) => el.offsetParent !== null && !el.hasAttribute("disabled") && el.tabIndex !== -1);
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel = "Confirm",
  danger,
  onConfirm,
  onClose,
  busy,
}: {
  title: string;
  message: React.ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
  onClose: () => void;
  busy?: boolean;
}) {
  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button
            className={`btn ${danger ? "danger" : "primary"}`}
            style={{ marginLeft: "auto" }}
            onClick={onConfirm}
            disabled={busy}
            data-autofocus="true"
          >
            {busy ? "Working…" : confirmLabel}
          </button>
        </>
      }
    >
      <div style={{ color: "var(--fg-dim)" }}>{message}</div>
    </Modal>
  );
}
