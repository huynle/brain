import {
  priorityColor,
  priorityGlyph,
  statusColor,
  statusGlyph,
  statusLabel,
} from "../../lib/format";
import type { Priority } from "../../lib/types";

export function StatusBadge({ status }: { status: string }) {
  const color = statusColor(status);
  return (
    <span
      className="badge"
      style={{
        color,
        background: `color-mix(in srgb, ${color} 16%, var(--bg-2))`,
      }}
    >
      {statusGlyph(status)} {statusLabel(status)}
    </span>
  );
}

export function PriorityTag({ priority }: { priority: Priority }) {
  if (!priority) return null;
  return (
    <span className="pill" style={{ color: priorityColor(priority) }}>
      {priorityGlyph(priority)} {priority}
    </span>
  );
}

export function Pill({
  children,
  color,
  title,
  className,
}: {
  children: React.ReactNode;
  color?: string;
  title?: string;
  className?: string;
}) {
  return (
    <span
      className={className ? `pill ${className}` : "pill"}
      style={color ? { color } : undefined}
      title={title}
    >
      {children}
    </span>
  );
}
