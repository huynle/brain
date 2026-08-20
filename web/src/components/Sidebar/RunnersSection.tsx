/**
 * Sidebar — Runners section (wireframe-parity port).
 *
 * DOM (matches wireframe renderSidebar → runner rows):
 *   .sb-section
 *     .sb-head (▾ Runners · online/total)
 *     .sb-list
 *       .runner-row × N
 *         .dot (on/err)
 *         .runner-body
 *           .runner-name
 *           .runner-meta (running/capacity · os · executor)
 *           .runner-assign (chip.mini × N assigned features)
 *
 * Preserves the Phase 8 assignment logic (real backend + optimistic
 * rollback via feature→runner assign/clear APIs) but restyles the
 * row with wireframe classnames.
 */
import { useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { useRunners } from "../../hooks/useRunners";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { useContextMenu } from "../common/ContextMenu";
import {
  readDragPayload,
  type DragPayload,
} from "../../hooks/useDragDrop";
import {
  assignFeatureToRunner,
  clearFeatureAssignment,
  ApiError,
} from "../../lib/api";
import type { RunnerInfo, FeatureAssignment } from "../../lib/types";

export function combineRunnerAssignments(
  runner: RunnerInfo,
  optimistic: Record<string, string>,
): Array<{ featureId: string; projectId?: string }> {
  const seen = new Set<string>();
  const out: Array<{ featureId: string; projectId?: string }> = [];
  for (const [featureId, runnerId] of Object.entries(optimistic)) {
    if (runnerId !== runner.runner_id) continue;
    if (seen.has(featureId)) continue;
    seen.add(featureId);
    out.push({ featureId });
  }
  for (const a of runner.feature_assignments ?? []) {
    if (
      optimistic[a.feature_id] &&
      optimistic[a.feature_id] !== runner.runner_id
    ) {
      continue;
    }
    if (seen.has(a.feature_id)) continue;
    seen.add(a.feature_id);
    out.push({ featureId: a.feature_id, projectId: a.project_id });
  }
  return out;
}

function runnerDot(status: RunnerInfo["status"]): "on" | "err" | "" {
  if (status === "online") return "on";
  if (status === "stale") return "err";
  return "";
}

export function RunnersSection(): JSX.Element {
  const expanded = useWorkspace((s) => s.sidebarSection.runners);
  const toggle = useWorkspace((s) => s.toggleSidebarSection);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const ctx = useContextMenu();
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

  const doClearAll = async (runner: RunnerInfo): Promise<void> => {
    const assignments = combineRunnerAssignments(runner, featureAssignments);
    if (assignments.length === 0) return;
    const clearsWithProject = assignments.filter((a) => a.projectId);
    const clearsMissingProject = assignments.filter((a) => !a.projectId);
    if (clearsMissingProject.length > 0) {
      toast(
        `Skipped ${clearsMissingProject.length} assignments with unknown project`,
        "info",
      );
    }
    const results = await Promise.allSettled(
      clearsWithProject.map((a) => {
        unassignFeature(a.featureId);
        return clearFeatureAssignment(a.projectId as string, a.featureId).then(
          () => a,
          (err) => {
            assignFeature(a.featureId, runner.runner_id);
            throw err;
          },
        );
      }),
    );
    const failed = results.filter((r) => r.status === "rejected").length;
    if (failed > 0) {
      toast(
        `Cleared ${results.length - failed}/${results.length}; ${failed} failed`,
        "error",
      );
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
      const dot = runnerDot(r.status);
      const isDrop = dropTarget === r.runner_id;
      const running = r.active_tasks ?? 0;
      const capacity = r.max_parallel ?? 0;
      const os =
        (r.labels && (r.labels.os || r.labels.OS)) || r.status;
      const executor =
        (r.executors && r.executors[0]) || "opencode";

      return (
        <div
          key={r.runner_id}
          className={`runner-row${isDrop ? " drop-target" : ""}`}
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
            setDropTarget(null);
            void doAssign(payload, r.runner_id);
          }}
          onContextMenu={(e) => {
            e.preventDefault();
            const hasAssignments = assignments.length > 0;
            ctx.open(e.clientX, e.clientY, [
              {
                id: "open",
                label: "Open runner shell",
                onClick: () =>
                  openModal("runner", { id: r.runner_id }, "shell"),
              },
              {
                id: "details",
                label: "View details",
                onClick: () =>
                  openModal("runner", { id: r.runner_id }, "overview"),
              },
              {
                id: "processes",
                label: "View processes",
                onClick: () =>
                  openModal("runner", { id: r.runner_id }, "processes"),
              },
              { id: "sep-1", separator: true },
              {
                id: "clear",
                label: hasAssignments
                  ? assignments.length === 1
                    ? "Clear assignment"
                    : `Clear all ${assignments.length} assignments`
                  : "Clear assignment",
                disabled: !hasAssignments,
                danger: hasAssignments,
                onClick: () => void doClearAll(r),
              },
            ]);
          }}
        >
          <span className={`dot ${dot}`} />
          <div className="runner-body">
            <div className="runner-name">{r.runner_id}</div>
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

  const onlineCount = runners.filter((r) => r.status === "online").length;

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
        </span>
      </div>
      {expanded && <div className="sb-list">{body}</div>}
      {ctx.menu}
    </div>
  );
}

export type { FeatureAssignment };
