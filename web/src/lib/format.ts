import type { Priority, TaskStatus } from "./types";

/** CSS color variable for a task status. */
export function statusColor(status: string): string {
  switch (status) {
    case "in_progress":
    case "active":
      return "var(--blue)";
    case "completed":
    case "validated":
      return "var(--green)";
    case "blocked":
      return "var(--red)";
    case "pending":
      return "var(--yellow)";
    case "cancelled":
    case "archived":
    case "superseded":
      return "var(--fg-faint)";
    case "draft":
      return "var(--purple)";
    default:
      return "var(--fg-dim)";
  }
}

export function statusLabel(status: string): string {
  return status.replace(/_/g, " ");
}

export function statusGlyph(status: string): string {
  switch (status) {
    case "in_progress":
    case "active":
      return "▶";
    case "completed":
    case "validated":
      return "✓";
    case "blocked":
      return "✗";
    case "pending":
      return "●";
    case "cancelled":
      return "⊘";
    case "draft":
      return "✎";
    default:
      return "·";
  }
}

export function priorityColor(p: Priority): string {
  switch (p) {
    case "high":
      return "var(--red)";
    case "medium":
      return "var(--yellow)";
    case "low":
      return "var(--fg-faint)";
    default:
      return "var(--fg-dim)";
  }
}

export function priorityGlyph(p: Priority): string {
  switch (p) {
    case "high":
      return "↑";
    case "medium":
      return "→";
    case "low":
      return "↓";
    default:
      return "·";
  }
}

const ACTIVE_STATUSES = new Set<TaskStatus>(["in_progress", "active"]);
export function isActive(status: string): boolean {
  return ACTIVE_STATUSES.has(status as TaskStatus);
}

export function relativeTime(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const diff = Date.now() - t;
  const abs = Math.abs(diff);
  const fut = diff < 0;
  const s = Math.floor(abs / 1000);
  const fmt = (n: number, unit: string) =>
    fut ? `in ${n}${unit}` : `${n}${unit} ago`;
  if (s < 45) return fut ? "soon" : "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return fmt(m, "m");
  const h = Math.floor(m / 60);
  if (h < 24) return fmt(h, "h");
  const d = Math.floor(h / 24);
  if (d < 30) return fmt(d, "d");
  const mo = Math.floor(d / 30);
  if (mo < 12) return fmt(mo, "mo");
  return fmt(Math.floor(mo / 12), "y");
}

export function cleanLogContent(content?: string): string {
  if (!content) return "";
  return content
    .replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\[(?:\d{1,3};)*\d{1,3}[A-Za-z]/g, "");
}

export function clockTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function logLevelColor(level: string): string {
  switch (level.toLowerCase()) {
    case "error":
    case "err":
      return "var(--red)";
    case "warn":
    case "warning":
      return "var(--yellow)";
    case "info":
      return "var(--blue)";
    case "debug":
      return "var(--fg-faint)";
    default:
      return "var(--fg-dim)";
  }
}
