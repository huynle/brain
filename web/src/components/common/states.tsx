export function Spinner() {
  return <div className="spinner" aria-label="loading" />;
}

export function Loading({ label }: { label?: string }) {
  return (
    <div className="center-state">
      <Spinner />
      {label && <div className="muted">{label}</div>}
    </div>
  );
}

export function EmptyState({
  glyph = "∅",
  title,
  hint,
  children,
}: {
  glyph?: string;
  title: string;
  hint?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="center-state">
      <div className="big">{glyph}</div>
      <div style={{ fontWeight: 600, color: "var(--fg)" }}>{title}</div>
      {hint && <div className="muted">{hint}</div>}
      {children}
    </div>
  );
}

export function ErrorState({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: () => void;
}) {
  const message = error instanceof Error ? error.message : String(error);
  return (
    <div className="center-state">
      <div className="big" style={{ color: "var(--red)" }}>
        ⚠
      </div>
      <div style={{ fontWeight: 600, color: "var(--fg)" }}>Something broke</div>
      <div className="muted" style={{ wordBreak: "break-word" }}>
        {message}
      </div>
      {onRetry && (
        <button className="btn" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  );
}
