/**
 * Loading primitive.
 *
 * Small spinner + optional label. No CSS class dependency — inline
 * styles keep it simple.
 */

export type LoadingSize = "sm" | "md" | "lg";

export interface LoadingProps {
  label?: string;
  size?: LoadingSize;
  className?: string;
}

const SIZE = { sm: 12, md: 18, lg: 24 } as const;

export function Loading({
  label,
  size = "md",
  className,
}: LoadingProps): JSX.Element {
  const d = SIZE[size];
  return (
    <div
      className={className}
      role="status"
      aria-live="polite"
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 8,
        padding: "6px 10px",
        color: "#9098a1",
        fontSize: 11,
      }}
    >
      <span
        style={{
          width: d,
          height: d,
          borderRadius: "50%",
          border: `2px solid #2a2f35`,
          borderTopColor: "#f4b23a",
          display: "inline-block",
          animation: "p2-spin 800ms linear infinite",
        }}
      />
      {label && <span>{label}</span>}
      <style>{`@keyframes p2-spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );
}
