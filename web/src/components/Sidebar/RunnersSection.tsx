/**
 * Sidebar — Runners section (wireframe-parity port).
 *
 * DOM (matches wireframe renderSidebar → runner rows):
 *   .sb-section
 *     .sb-head (▾ Runners · online/total)
 *     .sb-list
 *       .runner-row × N
 *         .dot (on/busy/paused/err)
 *         .runner-body
 *           .runner-name (+ .pause-tag when the runner dial is off)
 *           .runner-meta (running/capacity · os · executor)
 *           .runner-assign (chip.mini × N assigned features)
 *
 * Preserves the Phase 8 assignment logic (real backend + optimistic
 * rollback via feature→runner assign/clear APIs) but restyles the
 * row with wireframe classnames.
 *
 * The dot is derived by `lib/pause.runnerDotState`, not from `r.status`
 * alone: a runner paused via PUT /runners/{id}/pause keeps reporting status
 * "online" — it heartbeats normally, it just refuses placement — so the old
 * status-only derivation painted it the identical green as one doing work.
 *
 * Verbs come from `lib/actions/runnerActions` via `useRowActions`, so
 * right-click, long-press and keyboard offer the identical set — same
 * registry pattern as tasks, features, goals and automations. The
 * per-chip click (clear one assignment) stays local: it is a targeted
 * shortcut the menu's bulk clear does not replace.
 */
import { useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { useRunners } from "../../hooks/useRunners";
import { useRunnerActionContext } from "../../hooks/useRunnerActionContext";
import { useRowActions } from "../../hooks/useRowActions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import {
  endDrag,
  readDragPayload,
  type DragPayload,
} from "../../hooks/useDragDrop";
import {
  buildRunnerActions,
  combineRunnerAssignments,
} from "../../lib/actions/runnerActions";
import {
  assignFeatureToRunner,
  clearFeatureAssignment,
  ApiError,
} from "../../lib/api";
import { runnerDotState, runnerDotTitle } from "../../lib/pause";
import type { FeatureAssignment } from "../../lib/types";

export function RunnersSection(): JSX.Element {
  const expanded = useWorkspace((s) => s.sidebarSection.runners);
  const toggle = useWorkspace((s) => s.toggleSidebarSection);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const runnerCtx = useRunnerActionContext();
  const { rowProps, overlays } = useRowActions();
  const { runners, isLoading, error, refetch } = useRunners();
  const [dropTarget, setDropTarget] = useState<string | null>(null);

  const doAssign = async (
    payload: DragPayload,
    targetRunnerId: string,
  ): Promise<void> => {
    const projectId = payload.target.projectId as string | undefined;
    const featureId = payload.target.featureId as string | undefined;
    const currentRunnerId = payload.target.currentRunnerId as
      | string
      | undefined;
    if (!projectId || !featureId) {
      toast("Invalid drop (missing project/feature)", "error");
      return;
    }
    if (currentRunnerId === targetRunnerId) return;
    assignFeature(featureId, targetRunnerId);
    try {
      await assignFeatureToRunner(projectId, featureId, targetRunnerId, {
        intent: currentRunnerId ? "reassign" : "assign",
      });
    } catch (err) {
      if (currentRunnerId) assignFeature(featureId, currentRunnerId);
      else unassignFeature(featureId);
      const msg =
        err instanceof ApiError
          ? `Assign failed: ${err.message}`
          : `Assign failed: ${(err as Error).message ?? "unknown"}`;
      toast(msg, "error");
    }
  };

  const doClearOne = async (
    projectId: string,
    featureId: string,
    previousRunnerId: string,
  ): Promise<void> => {
    unassignFeature(featureId);
    try {
      await clearFeatureAssignment(projectId, featureId);
    } catch (err) {
      assignFeature(featureId, previousRunnerId);
      const msg =
        err instanceof ApiError
          ? `Clear failed: ${err.message}`
          : `Clear failed: ${(err as Error).message ?? "unknown"}`;
      toast(msg, "error");
    }
  };

  const body = (() => {
    if (isLoading) return <Loading size="sm" label="Loading…" />;
    if (error) return <ErrorState error={error} onRetry={refetch} />;
    if (runners.length === 0) {
      return (
        <div style={{ padding: "6px 10px", color: "#6b757e", fontSize: 11 }}>
          No runners registered.
        </div>
      );
    }
    return runners.map((r) => {
      const assignments = combineRunnerAssignments(r, featureAssignments);
      const dot = runnerDotState(r);
      const isDrop = dropTarget === r.runner_id;
      const running = r.active_tasks ?? 0;
      const capacity = r.max_parallel ?? 0;
      const os =
        (r.labels && (r.labels.os || r.labels.OS)) || r.status;
      const executor =
        (r.executors && r.executors[0]) || "opencode";
      const actions = buildRunnerActions(r, runnerCtx, {
        assignmentCount: assignments.length,
      });

      return (
        <div
          key={r.runner_id}
          className={`runner-row${isDrop ? " drop-target" : ""}`}
          {...rowProps(actions, r.runner_id, () =>
            openModal("runner", { id: r.runner_id }),
          )}
          onClick={() => openModal("runner", { id: r.runner_id })}
          onDragOver={(e) => {
            const payload = readDragPayload(e);
            if (payload?.source !== "feature-header") return;
            e.preventDefault();
            e.stopPropagation();
            if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
            if (dropTarget !== r.runner_id) setDropTarget(r.runner_id);
          }}
          onDragLeave={() => {
            if (dropTarget === r.runner_id) setDropTarget(null);
          }}
          onDrop={(e) => {
            const payload = readDragPayload(e);
            if (payload?.source !== "feature-header") return;
            e.preventDefault();
            e.stopPropagation();
            // Clear the shadow payload here like every other drop
            // target does. Relying on the source row's `onDragEnd` is a
            // coincidence: React delegates listeners to the root, so if
            // the feature row unmounts during the drop (an SSE re-sort,
            // a re-bucketing assignment) its `dragend` never lands and
            // the payload sticks — leaving an armed, invisible
            // `pointer-events: auto` overlay on every pane in both docks.
            endDrag();
            setDropTarget(null);
            void doAssign(payload, r.runner_id);
          }}
        >
          <span className={`dot ${dot}`} title={runnerDotTitle(r)} />
          <div className="runner-body">
            <div className="runner-name">
              {/* The id truncates; the pause tag never does. Without the
                  inner span the tag rode inside the ellipsised text and a
                  long runner id would have hidden it entirely. */}
              <span className="runner-name__id">{r.runner_id}</span>
              {r.paused && (
                <span className="pause-tag" title={runnerDotTitle(r)}>
                  paused
                </span>
              )}
            </div>
            <div className="runner-meta">
              <span>
                {running}/{capacity}
              </span>
              <span>{os}</span>
              <span>{executor}</span>
            </div>
            {assignments.length > 0 && (
              <div className="runner-assign">
                {assignments.map((a) => (
                  <span
                    key={a.featureId}
                    className="chip mini"
                    title={`Unassign ${a.featureId}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      if (!a.projectId) {
                        toast(
                          `Can't clear ${a.featureId}: unknown project`,
                          "info",
                        );
                        return;
                      }
                      void doClearOne(a.projectId, a.featureId, r.runner_id);
                    }}
                  >
                    {a.featureId}
                  </span>
                ))}
              </div>
            )}
          </div>
        </div>
      );
    });
  })();

  // "online" here means online AND able to take work. A paused runner
  // inflating this count is how "1/1 runners" reassured a user whose only
  // runner would never accept a dispatch.
  const onlineCount = runners.filter(
    (r) => r.status === "online" && !r.paused,
  ).length;
  const pausedCount = runners.filter((r) => r.paused).length;

  return (
    <div className="sb-section">
      <div
        className={`sb-head ${!expanded ? "collapsed" : ""}`}
        onClick={() => toggle("runners")}
      >
        <span className="caret">▾</span>
        Runners
        <span className="count">
          {onlineCount}/{runners.length}
          {pausedCount > 0 && (
            <span
              className="count-paused"
              title={`${pausedCount} runner${pausedCount === 1 ? " is" : "s are"} paused and will not accept dispatches`}
            >
              {" "}
              · {pausedCount} paused
            </span>
          )}
        </span>
      </div>
      {expanded && <div className="sb-list">{body}</div>}
      {overlays}
    </div>
  );
}

// Re-exported from the actions module where the pure helper now lives,
// so existing importers keep working.
export { combineRunnerAssignments };
export type { FeatureAssignment };
