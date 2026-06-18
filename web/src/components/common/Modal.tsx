import { useEffect, useRef, useState } from "react";
import { isEditableTarget } from "../../lib/keyboard";

export function Modal({
  title,
  onClose,
  children,
  footer,
  className,
  onEdit,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  /** Extra class on the dialog, e.g. "sheet-wide" for a larger editor modal. */
  className?: string;
  /** Optional action wired to the modal-level edit shortcut (`e`). */
  onEdit?: () => void;
}) {
  const backdropRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    const focusables = getFocusableElements(dialog);
    const preferred = dialog?.querySelector<HTMLElement>('[data-autofocus="true"]');
    const target = preferred || focusables[0] || dialog;
    target?.focus({ preventScroll: true });
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
        setExpanded((v) => !v);
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
  }, [onClose, onEdit]);

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
            onClick={() => setExpanded((v) => !v)}
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
