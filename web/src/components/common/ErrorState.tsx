/**
 * ErrorState primitive.
 *
 * Centered error message + retry button. Inline styles.
 */

export interface ErrorStateProps {
  error: unknown;
  onRetry?: () => void;
  title?: string;
  className?: string;
}

export function messageOf(error: unknown): string {
  if (error instanceof Error) return error.message || String(error);
  if (typeof error === "string") return error;
  try {
    return JSON.stringify(error);
  } catch {
    return String(error);
  }
}

export function ErrorState({
  error,
  onRetry,
  title = "Something broke",
  className,
}: ErrorStateProps): JSX.Element {
  return (
    <div
      className={className}
      role="alert"
      style={{
        padding: "12px 14px",
        border: "1px solid #d9606055",
        borderRadius: 6,
        background: "#d9606011",
        color: "#eaedef",
        fontSize: 11,
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
    >
      <div style={{ color: "#d96060", fontSize: 16 }}>⚠ {title}</div>
      <div style={{ color: "#9098a1" }}>{messageOf(error)}</div>
      {onRetry && (
        <div>
          <button onClick={onRetry} style={{ marginTop: 6 }}>
            Retry
          </button>
        </div>
      )}
    </div>
  );
}
