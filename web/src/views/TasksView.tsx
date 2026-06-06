import { lazy, Suspense, useMemo, useState } from "react";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useLiveTasks } from "../hooks/useLiveTasks";
import { filterTasks, groupByFeature, UNGROUPED } from "./tasks/grouping";
import { TaskCard } from "./tasks/TaskCard";
import { TaskDetail } from "./tasks/TaskDetail";
import { EmptyState } from "../components/common/states";
import type { Task } from "../lib/types";

const ComposeModal = lazy(() =>
  import("../components/compose/ComposeModal").then((m) => ({
    default: m.ComposeModal,
  })),
);

export function TasksView() {
  const activeProject = useUI((s) => s.activeProject);
  const { tasks, connected } = useLiveTasks(activeProject);
  const [query, setQuery] = useState("");
  const [showDone, setShowDone] = useState(false);
  const [selected, setSelected] = useState<Task | null>(null);
  const [composing, setComposing] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const groups = useMemo(() => {
    let list = filterTasks(tasks, query);
    if (!showDone) {
      list = list.filter(
        (t) =>
          !["completed", "cancelled", "archived", "superseded"].includes(
            t.status,
          ),
      );
    }
    return groupByFeature(list);
  }, [tasks, query, showDone]);

  const total = groups.reduce((n, g) => n + g.tasks.length, 0);
  const showProject = activeProject === ALL_PROJECTS;

  return (
    <div>
      <div className="search-bar">
        <input
          placeholder="Filter tasks…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button
          className={`btn sm ${showDone ? "primary" : ""}`}
          onClick={() => setShowDone((v) => !v)}
          title="Toggle completed/cancelled tasks"
        >
          {showDone ? "All" : "Active"}
        </button>
        <button
          className="btn sm primary"
          onClick={() => setComposing(true)}
          title="New task"
        >
          + New
        </button>
      </div>

      {total === 0 ? (
        tasks.length === 0 && !connected ? (
          <EmptyState
            glyph="⧗"
            title="Waiting for live data…"
            hint="Connecting to the task stream."
          />
        ) : (
          <EmptyState
            glyph="✓"
            title={query ? "No matches" : "No active tasks"}
            hint={
              query
                ? "Try a different filter."
                : showDone
                  ? "This project has no tasks."
                  : "Tap “All” to include completed tasks."
            }
          />
        )
      ) : (
        <div className="section-pad">
          {groups.map((g) => {
            const isCollapsed = collapsed[g.feature];
            return (
              <div key={g.feature} style={{ marginBottom: "0.8rem" }}>
                {g.feature !== UNGROUPED || groups.length > 1 ? (
                  <button
                    onClick={() =>
                      setCollapsed((c) => ({
                        ...c,
                        [g.feature]: !c[g.feature],
                      }))
                    }
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: "0.4rem",
                      width: "100%",
                      background: "none",
                      border: "none",
                      padding: "0.3rem 0",
                      color: "var(--fg-dim)",
                      fontWeight: 600,
                      fontSize: 13,
                    }}
                  >
                    <span style={{ color: "var(--purple)" }}>
                      {isCollapsed ? "▸" : "▾"}
                    </span>
                    {g.label}
                    <span className="faint">({g.tasks.length})</span>
                  </button>
                ) : null}
                {!isCollapsed &&
                  g.tasks.map((t) => (
                    <TaskCard
                      key={`${t.projectId}:${t.id}`}
                      task={t}
                      showProject={showProject}
                      onClick={() => setSelected(t)}
                    />
                  ))}
              </div>
            );
          })}
        </div>
      )}

      {selected && (
        <TaskDetail task={selected} onClose={() => setSelected(null)} />
      )}
      {composing && (
        <Suspense fallback={null}>
          <ComposeModal kind="task" onClose={() => setComposing(false)} />
        </Suspense>
      )}
    </div>
  );
}
