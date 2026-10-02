/**
 * AttentionBell — the in-app inbox surface for unread attention items.
 *
 * Sits beside ReminderBell in the topbar. Like the reminder bell, it only
 * appears when something is unread: chrome that is always present but almost
 * always empty trains people to stop looking at it. An unread attention item
 * survives a reload because its state lives on the server record, not in a
 * client-side notification list.
 */
import { useEffect, useRef, useState } from "react";

import { useAttention, snoozeUntil } from "../hooks/useAttention";
import { useAttentionRowActions } from "../hooks/useAttentionRowActions";
import { sortAttention } from "../lib/attention";
import { severityLabel } from "./Attention/AttentionRows";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";

/** How long ago, in words. */
function createdAgo(iso?: string): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "just now";
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function AttentionBell(): JSX.Element | null {
  const attentionQuery = useAttention({ state: "unread" });
  const { unread } = attentionQuery;
  const actions = useAttentionRowActions(attentionQuery);
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const toast = useUI((s) => s.toast);
  const setView = useWorkspace((s) => s.setView);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  useEffect(() => {
    if (open && unread.length === 0) setOpen(false);
  }, [open, unread.length]);

  if (unread.length === 0) return null;

  const recent = sortAttention(unread).slice(0, 8);

  const run = async (label: string, fn: () => Promise<void>) => {
    try {
      await fn();
    } catch (err) {
      toast(
        `${label} failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    }
  };

  return (
    <div className="attention-bell" ref={wrapRef}>
      <button
        className={"icon-btn" + (open ? " active" : "")}
        title={`${unread.length} unread notification${unread.length === 1 ? "" : "s"}`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        📥<span className="dock-count">{unread.length}</span>
      </button>

      {open && (
        <div
          className="attention-panel"
          role="dialog"
          aria-label="Notifications"
        >
          <div className="attention-panel__head">
            <span>
              {unread.length} unread notification
              {unread.length === 1 ? "" : "s"}
            </span>
            <button
              className="attention-panel__all"
              onClick={() => {
                setOpen(false);
                setView("attention");
              }}
            >
              Open inbox →
            </button>
          </div>
          <div className="attention-panel__list">
            {recent.map((item) => (
              <div
                key={item.id}
                className={`attention-row attention-row--${item.severity}`}
              >
                <div className="attention-row__main">
                  <div className="attention-row__title">
                    <span
                      className={`attention-sev attention-sev--${item.severity}`}
                      title={severityLabel(item.severity)}
                      aria-hidden="true"
                    />
                    {item.title}
                  </div>
                  <div className="attention-row__meta">
                    {createdAgo(item.created_at)}
                    {item.project ? ` · ${item.project}` : ""}
                    {item.kind ? ` · ${item.kind}` : ""}
                  </div>
                </div>
                <div className="attention-row__acts">
                  {actions.hasLink(item) && (
                    <button
                      title="Open the linked task, feature, or session"
                      onClick={() => {
                        setOpen(false);
                        actions.onOpen(item);
                      }}
                    >
                      Open
                    </button>
                  )}
                  <button
                    title="Remind me again in an hour"
                    onClick={() =>
                      void run("Snooze", () =>
                        attentionQuery.snooze(item.id, snoozeUntil(60)),
                      )
                    }
                  >
                    1h
                  </button>
                  <button
                    title="Mark read"
                    onClick={() =>
                      void run("Mark read", () =>
                        attentionQuery.markRead(item.id),
                      )
                    }
                  >
                    Read
                  </button>
                  <button
                    className="primary"
                    title="Resolve"
                    onClick={() =>
                      void run("Resolve", () =>
                        attentionQuery.resolve(item.id),
                      )
                    }
                  >
                    Resolve
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
