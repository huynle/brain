/**
 * ConfirmDialog — the single confirmation surface for destructive actions.
 *
 * Before this existed, FeatureActionsModal hand-rolled its own
 * `confirmForce` view, so every new destructive verb would have reinvented
 * one and drifted. An ActionDescriptor carrying `confirm` routes here
 * automatically.
 *
 * Two deliberate choices:
 *
 * - **The body states a consequence, not a question.** "All 4 tasks will be
 *   set to cancelled" tells the user what they are about to cause; "Are you
 *   sure?" tells them nothing they did not already know.
 * - **`typeToConfirm` is rationed.** It is friction that only works while it
 *   is rare — applied to reversible actions it just trains people to type
 *   through the irreversible ones. Reserved for feature deletion.
 */
import { useEffect, useRef, useState } from "react";

import { Modal } from "./Modal";
import type { ActionConfirm } from "../../lib/actions/types";

export interface ConfirmDialogProps {
  confirm: ActionConfirm;
  /** Rendered in the destructive tone when true. */
  danger?: boolean;
  onCancel: () => void;
  /** Resolve to close; reject to surface an error and stay open. */
  onConfirm: () => Promise<void>;
}

export function ConfirmDialog({
  confirm,
  danger = false,
  onCancel,
  onConfirm,
}: ConfirmDialogProps): JSX.Element {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const needsTyping = !!confirm.typeToConfirm;
  const typingSatisfied = !needsTyping || typed === confirm.typeToConfirm;
  const canConfirm = typingSatisfied && !busy;

  useEffect(() => {
    if (needsTyping) inputRef.current?.focus();
  }, [needsTyping]);

  const submit = async () => {
    if (!canConfirm) return;
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  return (
    <Modal
      title={confirm.title}
      onClose={busy ? () => {} : onCancel}
      closeOnScrimClick={!busy}
      closeOnEscape={!busy}
      footer={
        <>
          <button onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className={danger ? "danger" : "primary"}
            disabled={!canConfirm}
            // Autofocus the confirm button only when there is no field to
            // fill — otherwise focus belongs in the input.
            data-autofocus={needsTyping ? undefined : "true"}
            onClick={() => void submit()}
          >
            {busy ? "Working…" : (confirm.confirmLabel ?? "Confirm")}
          </button>
        </>
      }
    >
      <p style={{ fontSize: 12, lineHeight: 1.6, color: "#c8cdd2", margin: 0 }}>
        {confirm.body}
      </p>

      {needsTyping && (
        <div style={{ marginTop: 14 }}>
          <label
            style={{
              display: "block",
              fontSize: 11,
              color: "#9098a1",
              marginBottom: 5,
            }}
            htmlFor="confirm-type"
          >
            Type <code style={{ color: "#f4b23a" }}>{confirm.typeToConfirm}</code> to
            confirm
          </label>
          <input
            id="confirm-type"
            ref={inputRef}
            value={typed}
            disabled={busy}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && canConfirm) {
                e.preventDefault();
                void submit();
              }
            }}
            style={{
              width: "100%",
              padding: "6px 8px",
              fontSize: 12,
              fontFamily: "inherit",
              background: "#0a0c0e",
              border: `1px solid ${typingSatisfied ? "#6fca7d55" : "#2a2f35"}`,
              borderRadius: 3,
              color: "#eaedef",
            }}
          />
        </div>
      )}

      {error && (
        <div
          role="alert"
          style={{
            marginTop: 12,
            padding: "6px 8px",
            fontSize: 11,
            color: "#d96060",
            border: "1px solid #d9606055",
            background: "#d9606011",
            borderRadius: 3,
          }}
        >
          {error}
        </div>
      )}
    </Modal>
  );
}
