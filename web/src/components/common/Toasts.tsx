import { useUI } from "../../store/ui";

export function Toasts() {
  const toasts = useUI((s) => s.toasts);
  const dismiss = useUI((s) => s.dismissToast);
  if (!toasts.length) return null;
  return (
    <div className="toast-wrap">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`toast ${t.kind}`}
          onClick={() => dismiss(t.id)}
          role="status"
        >
          <span className="toast-message">{t.message}</span>
          {t.action && (
            <button
              className="toast-action btn sm"
              onClick={(e) => {
                // Stop the click from bubbling to the toast's dismiss
                // handler so the user sees the action's own outcome
                // (success/error) instead of the toast vanishing first.
                e.stopPropagation();
                const result = t.action!.onClick();
                if (result && typeof (result as Promise<void>).then === "function") {
                  void (result as Promise<void>).finally(() => dismiss(t.id));
                } else {
                  dismiss(t.id);
                }
              }}
            >
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
