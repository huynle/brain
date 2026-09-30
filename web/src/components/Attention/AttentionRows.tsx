import type { Attention, AttentionSeverity } from "../../lib/types";

export function formatAttentionTime(iso?: string): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

const SEVERITY_LABEL: Record<AttentionSeverity, string> = {
  info: "Info",
  warning: "Warning",
  critical: "Critical",
};

export function severityLabel(severity: AttentionSeverity): string {
  return SEVERITY_LABEL[severity] ?? severity;
}

export interface AttentionRowActions {
  onRead: (item: Attention) => void;
  onResolve: (item: Attention) => void;
  onDismiss: (item: Attention) => void;
  onSnooze: (item: Attention) => void;
  onOpen: (item: Attention) => void;
  /** True when the item links to something openable (task/feature/session). */
  hasLink: (item: Attention) => boolean;
}

export function AttentionRows({
  items,
  actions,
}: {
  items: Attention[];
  actions: AttentionRowActions;
}): JSX.Element {
  return (
    <div className="attention-items">
      {items.map((item) => (
        <AttentionItem key={item.id} item={item} actions={actions} />
      ))}
    </div>
  );
}

function AttentionItem({
  item,
  actions,
}: {
  item: Attention;
  actions: AttentionRowActions;
}): JSX.Element {
  const openable = actions.hasLink(item);
  return (
    <article
      className={`attention-item attention-item--${item.severity}${
        openable ? " attention-item--openable" : ""
      }`}
    >
      <div className="attention-item__row">
        <button
          type="button"
          className="attention-item__open"
          aria-label={
            openable
              ? `Open ${item.title}`
              : item.title
          }
          disabled={!openable}
          onClick={() => actions.onOpen(item)}
        >
          <div className="attention-item__title">
            <span
              className={`attention-sev attention-sev--${item.severity}`}
              title={severityLabel(item.severity)}
              aria-hidden="true"
            />
            {item.title}
          </div>
          {item.body ? (
            <div className="attention-item__body">{item.body}</div>
          ) : null}
          <div className="attention-item__meta">
            {formatAttentionTime(item.created_at)}
            {` · ${item.project || "global"}`}
            {item.kind ? ` · ${item.kind}` : ""}
            {item.state === "snoozed" && item.snoozed_until
              ? ` · snoozed until ${formatAttentionTime(item.snoozed_until)}`
              : ""}
          </div>
        </button>
        <div className="attention-item__acts">
          {item.state === "unread" && (
            <>
              <button
                title="Snooze for one hour"
                aria-label={`Snooze ${item.title} for one hour`}
                onClick={() => actions.onSnooze(item)}
              >
                1h
              </button>
              <button
                title="Mark read"
                aria-label={`Mark ${item.title} read`}
                onClick={() => actions.onRead(item)}
              >
                Read
              </button>
              <button
                className="primary"
                title="Resolve"
                aria-label={`Resolve ${item.title}`}
                onClick={() => actions.onResolve(item)}
              >
                Resolve
              </button>
            </>
          )}
          {(item.state === "read" || item.state === "snoozed") && (
            <button
              className="primary"
              title="Resolve"
              aria-label={`Resolve ${item.title}`}
              onClick={() => actions.onResolve(item)}
            >
              Resolve
            </button>
          )}
          {item.state !== "dismissed" && item.state !== "resolved" && (
            <button
              title="Dismiss"
              aria-label={`Dismiss ${item.title}`}
              onClick={() => actions.onDismiss(item)}
            >
              Dismiss
            </button>
          )}
        </div>
      </div>
    </article>
  );
}
