import { StatusBadge, PriorityTag } from "../../components/common/Badge";
import { relativeTime } from "../../lib/format";
import type { Task } from "../../lib/types";

export function TaskCard({
  task,
  showProject,
  cursored,
  selected,
  showCheck,
  wrap,
  onClick,
  onToggleSelect,
}: {
  task: Task;
  showProject?: boolean;
  cursored?: boolean;
  selected?: boolean;
  showCheck?: boolean;
  wrap?: boolean;
  onClick: () => void;
  onToggleSelect?: () => void;
}) {
  return (
    <div
      className={cursored ? "kbd-cursor" : ""}
      data-cursor={cursored ? "1" : undefined}
      style={{
        display: "flex",
        alignItems: "stretch",
        gap: "0.5rem",
        background: "var(--bg-1)",
        border: "1px solid var(--border)",
        borderLeft: `3px solid ${task.in_cycle ? "var(--red)" : selected ? "var(--accent)" : "var(--border-strong)"}`,
        borderRadius: "var(--radius-sm)",
        padding: "0.6rem 0.7rem",
        marginBottom: "0.4rem",
      }}
    >
      {showCheck && (
        <button
          className={`sel-check ${selected ? "on" : ""}`}
          onClick={(e) => {
            e.stopPropagation();
            onToggleSelect?.();
          }}
          aria-label={selected ? "deselect" : "select"}
          style={{ alignSelf: "center" }}
        >
          {selected ? "✓" : ""}
        </button>
      )}
      <button
        onClick={onClick}
        style={{
          flex: 1,
          minWidth: 0,
          textAlign: "left",
          background: "none",
          border: "none",
          padding: 0,
          color: "inherit",
        }}
      >
        <div style={{ display: "flex", gap: "0.5rem", alignItems: "flex-start" }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div
              style={{
                fontWeight: 500,
                lineHeight: 1.3,
                ...(wrap
                  ? {}
                  : {
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      display: "-webkit-box",
                      WebkitLineClamp: 2,
                      WebkitBoxOrient: "vertical" as const,
                    }),
              }}
            >
              {task.in_cycle && (
                <span title="dependency cycle" style={{ color: "var(--red)" }}>
                  ↺{" "}
                </span>
              )}
              {task.title || task.id}
            </div>
            <div className="row wrap" style={{ marginTop: "0.4rem", gap: "0.35rem" }}>
              <PriorityTag priority={task.priority} />
              {showProject && task.projectId && (
                <span className="pill mono" style={{ color: "var(--cyan)" }}>
                  {task.projectId.split(/[/\\]/).pop()}
                </span>
              )}
              {task.agent && <span className="pill faint">{task.agent}</span>}
              {task.created && (
                <span className="pill faint">{relativeTime(task.created)}</span>
              )}
            </div>
          </div>
          <StatusBadge status={task.status} />
        </div>
      </button>
    </div>
  );
}
