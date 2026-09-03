/**
 * panes-v2 TaskDetailLeaf.
 *
 * Renders the same information as `TaskModal` (read-only KV grid),
 * plus recorded Sessions and the Content body — the same three pieces
 * the old single-item `FeatureDrawer` (now `SidebarDock.tsx`) showed
 * for its task view — embedded inside a pane instead of a modal shell.
 *
 * NOTE: We currently duplicate the KV-pair builder from
 * `Modal/TaskModal.tsx` rather than extracting a shared
 * `TaskDetailView` component. Phase 7 focuses on the docking
 * infrastructure — the shared-view refactor is a low-risk follow-up
 * and is tracked in `docs/panes-v2-followups.md`. `SessionsSection` and
 * the Content `<pre>` ARE already shared pieces (with TaskModal), so
 * those are reused verbatim rather than re-duplicated here.
 */
import React, { useMemo } from "react";

import { KV } from "../../common/KV";
import { Chip } from "../../common/Chip";
import { Dot, type DotVariant } from "../../common/Dot";
import { ErrorState } from "../../common/ErrorState";
import { ActionBar } from "../../common/ActionBar";
import { SessionsSection } from "../../Modal/SessionsSection";
import { DispatchAttemptsSection } from "../../Modal/DispatchAttemptsSection";

import { useModal } from "../../../store/modal";
import { useLive } from "../../../lib/sse";
import { useActionRunner } from "../../../hooks/useActionRunner";
import { useTaskActionContext } from "../../../hooks/useTaskActionContext";
import { usePauseState } from "../../../hooks/usePauseState";
import { buildTaskActions } from "../../../lib/actions/taskActions";
import { taskHoldReason } from "../../../lib/pause";
import { TaskScheduleSection } from "../../Modal/TaskScheduleSection";
import type { Task, TaskStatus } from "../../../lib/types";

function taskDotVariant(status: TaskStatus): DotVariant {
  switch (status) {
    case "in_progress":
    case "active":
      return "busy";
    case "completed":
    case "validated":
      return "on";
    case "blocked":
      return "err";
    default:
      return "stale";
  }
}

export function TaskDetailLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const openModal = useModal((s) => s.open);
  const taskId =
    (target.taskId as string | undefined) ??
    (target.id as string | undefined) ??
    "";
  const projectId = (target.projectId as string | undefined) ?? "";

  const task = useLive((s) =>
    s.projects[projectId]?.tasks.find((t) => t.id === taskId),
  );
  const taskCtx = useTaskActionContext(projectId);
  const runner = useActionRunner();
  const { pause } = usePauseState();

  const pairs = useMemo(
    () => (task ? buildKvPairs(task, projectId, openModal) : []),
    [task, projectId, openModal],
  );

  // Built unconditionally (before the not-found early return) so hook
  // order stays stable. buildTaskActions surfaces the shared "Resume
  // task" verb whenever the task is resumable (is_abandoned, stuck
  // pending, etc.) — the same registry the row context menu and TaskModal
  // footer use, so this docked pane can't drift from them.
  const actions = useMemo(
    () => (task ? buildTaskActions(task, taskCtx) : []),
    [task, taskCtx],
  );

  if (!task) {
    return (
      <ErrorState
        error={`Task "${taskId}" not found in project "${projectId}".`}
        title="Task not found"
      />
    );
  }

  const hold = taskHoldReason(task, { pause, projectId });

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: "var(--p2-space-3)",
          fontSize: 13,
          color: "var(--p2-fg)",
        }}
      >
        <Dot variant={taskDotVariant(task.status)} title={task.status} />
        <strong>{task.title || `Task: ${task.id}`}</strong>
        {task.is_abandoned && (
          <span
            className="life-badge abandoned"
            style={{ marginLeft: 4 }}
            title={
              task.abandon_reason
                ? `Abandoned (${task.abandon_reason})`
                : "Abandoned"
            }
          >
            abandoned
          </span>
        )}
      </div>

      {hold && (
        <div className={`hold-banner ${hold.code}`} style={{ marginBottom: 10 }}>
          <b>{hold.glyph} Held — not dispatching.</b> {hold.detail}
        </div>
      )}

      {/* Action row — the same shared registry the row context menu and
          TaskModal footer use. Without this the docked task pane had no
          Resume affordance at all: an abandoned task (e.g. after the runner
          was rebuilt/restarted mid-run) could only be recovered from the
          row menu or the feature-level batch resume, never from the pane
          the user was already looking at. Resume is promoted beside Run
          whenever it applies. */}
      <div style={{ marginBottom: "var(--p2-space-3)" }}>
        <ActionBar
          actions={actions}
          onRun={runner.run}
          primary={["run", "resume"]}
        />
        {runner.dialog}
      </div>

      <KV pairs={pairs} />

      {/* "View" opens the session INLINE in the sidebar dock (not the
          full-page session view) — same verb the task's context menu
          offers as "Open session in sidebar". */}
      <div style={{ marginTop: "var(--p2-space-3)" }}>
        <SessionsSection
          task={task}
          projectId={projectId}
          onView={(t, ref) => taskCtx.openSessionInDrawer(t, ref)}
        />
      </div>

      {/* Above dispatch attempts: "when does this run again" is the
          question a recurring task raises, and it should not sit below a
          section that is absent for the happy path. */}
      <div style={{ marginTop: "var(--p2-space-3)" }}>
        <TaskScheduleSection task={task} />
      </div>

      <div style={{ marginTop: "var(--p2-space-3)" }}>
        <DispatchAttemptsSection task={task} />
      </div>

      {task.content && (
        <div style={{ marginTop: "var(--p2-space-3)" }}>
          <h4 className="modal-content-heading">Content</h4>
          <pre className="modal-content-pre">{task.content}</pre>
        </div>
      )}
    </div>
  );
}

// ─── KV builder — mirrored from Modal/TaskModal.tsx ──────────────────

function buildKvPairs(
  task: Task,
  projectId: string,
  openModal: (
    kind: "feature",
    target: { id: string; projectId: string },
  ) => void,
): { k: React.ReactNode; v: React.ReactNode }[] {
  const rows: { k: React.ReactNode; v: React.ReactNode }[] = [];

  rows.push({
    k: "Status",
    v: (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Dot variant={taskDotVariant(task.status)} size="sm" />
        {task.status}
      </span>
    ),
  });

  rows.push({
    k: "Priority",
    v: <Chip variant="mini">{task.priority}</Chip>,
  });

  if (task.feature_id) {
    rows.push({
      k: "Feature",
      v: (
        <button
          type="button"
          onClick={() =>
            openModal("feature", { id: task.feature_id as string, projectId })
          }
          style={{
            background: "transparent",
            border: 0,
            padding: 0,
            color: "var(--p2-accent, #58a6ff)",
            cursor: "pointer",
            font: "inherit",
            textDecoration: "underline dotted",
          }}
        >
          {task.feature_id}
        </button>
      ),
    });
  }

  if (task.agent) rows.push({ k: "Agent", v: task.agent });
  if (task.executor) rows.push({ k: "Executor", v: task.executor });
  if (task.model) rows.push({ k: "Model", v: task.model });
  if (task.workdir) rows.push({ k: "Workdir", v: task.workdir });
  if (task.git_branch) rows.push({ k: "Git branch", v: task.git_branch });
  if (task.merge_target_branch)
    rows.push({ k: "Merge target", v: task.merge_target_branch });
  if (task.merge_policy) rows.push({ k: "Merge policy", v: task.merge_policy });
  if (task.merge_strategy)
    rows.push({ k: "Merge strategy", v: task.merge_strategy });

  if (task.resolved_deps && task.resolved_deps.length > 0) {
    rows.push({
      k: "Resolved deps",
      v: <ChipList items={task.resolved_deps} />,
    });
  }
  if (task.blocked_by && task.blocked_by.length > 0) {
    rows.push({
      k: "Blocked by",
      v: <ChipList items={task.blocked_by} />,
    });
  }

  if (task.tags && task.tags.length > 0) {
    rows.push({ k: "Tags", v: <ChipList items={task.tags} /> });
  }

  if (task.created) rows.push({ k: "Created", v: task.created });
  if (task.modified) rows.push({ k: "Modified", v: task.modified });

  return rows;
}

function ChipList({ items }: { items: readonly string[] }): JSX.Element {
  return (
    <span style={{ display: "inline-flex", flexWrap: "wrap", gap: 4 }}>
      {items.map((x) => (
        <Chip key={x} variant="mini" title={x}>
          {x}
        </Chip>
      ))}
    </span>
  );
}
