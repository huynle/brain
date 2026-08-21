import { toPlainText } from "./ansi";
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

/**
 * Plain-text form of a log line: carriage-return overwrites resolved,
 * every escape sequence removed.
 *
 * Delegates to lib/ansi. The hand-rolled version this replaced ran a
 * second, UNANCHORED pass — `/\[(?:\d{1,3};)*\d{1,3}[A-Za-z]/` — which
 * matched any bracketed digits followed by a letter and so deleted real
 * content (`sleep[5s]`, `v[1beta]`, `[404m]`). Every pattern in lib/ansi
 * starts at a real ESC, so ordinary bracketed text is left alone.
 *
 * Prefer `parseAnsi` when the surface can render styled spans — colour
 * in agent output is information, not noise.
 */
export function cleanLogContent(content?: string): string {
  return toPlainText(content);
}

/**
 * Log-line clock: fixed-width 24-hour HH:MM:SS, local time.
 *
 * Deliberately NOT `toLocaleTimeString()`. A 12-hour locale renders
 * "12:04:31 PM" — around 69px at the log grids' 10.5px monospace, wider
 * than their fixed 54px (46px on mobile) timestamp track, so it
 * overpaints the level column beside it. Every log surface shares this
 * one formatter so the column width holds in every locale.
 */
export function clockTime(ts?: string | number): string {
  if (ts === undefined || ts === null || ts === "") return "";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
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
