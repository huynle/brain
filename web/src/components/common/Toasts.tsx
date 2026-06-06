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
          {t.message}
        </div>
      ))}
    </div>
  );
}
