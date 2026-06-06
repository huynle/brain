import { StatusBadge, PriorityTag } from "../../components/common/Badge";
import { relativeTime } from "../../lib/format";
import type { Task } from "../../lib/types";

export function TaskCard({
  task,
  showProject,
  onClick,
}: {
  task: Task;
  showProject?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      style={{
        display: "block",
        width: "100%",
        textAlign: "left",
        background: "var(--bg-1)",
        border: "1px solid var(--border)",
        borderLeft: `3px solid ${task.in_cycle ? "var(--red)" : "var(--border-strong)"}`,
        borderRadius: "var(--radius-sm)",
        padding: "0.6rem 0.7rem",
        marginBottom: "0.4rem",
      }}
    >
      <div style={{ display: "flex", gap: "0.5rem", alignItems: "flex-start" }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontWeight: 500,
              lineHeight: 1.3,
              overflow: "hidden",
              textOverflow: "ellipsis",
              display: "-webkit-box",
              WebkitLineClamp: 2,
              WebkitBoxOrient: "vertical",
            }}
          >
            {task.in_cycle && (
              <span title="dependency cycle" style={{ color: "var(--red)" }}>
                ↺{" "}
              </span>
            )}
            {task.title || task.id}
          </div>
          <div
            className="row wrap"
            style={{ marginTop: "0.4rem", gap: "0.35rem" }}
          >
            <PriorityTag priority={task.priority} />
            {showProject && task.projectId && (
              <span className="pill mono" style={{ color: "var(--cyan)" }}>
                {task.projectId.split(/[/\\]/).pop()}
              </span>
            )}
            {task.agent && (
              <span className="pill faint">{task.agent}</span>
            )}
            {task.created && (
              <span className="pill faint">{relativeTime(task.created)}</span>
            )}
          </div>
        </div>
        <StatusBadge status={task.status} />
      </div>
    </button>
  );
}
