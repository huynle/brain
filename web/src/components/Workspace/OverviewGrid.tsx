/**
 * OverviewGrid — wireframe-parity port of `renderOverviewGrid`.
 *
 * Panels in order:
 *   1. `.workflow-center` — command center for feature execution
 *   2. `.review-queue` (Needs attention) — only when items exist
 *   3. `.flow-board` — lifecycle lanes (Active / Blocked / Finished / MR / Merged)
 *   4. `.entries-preview` — Brain notes carousel
 *   5. `.pcard` × N — project cards
 *   6. `.empty-state` — fallback
 */
import { useEffect, useMemo, useRef } from "react";
import { useProjects } from "../../hooks/useProjects";
import { useLive } from "../../lib/sse";
import { useRunners } from "../../hooks/useRunners";
import { useWorkspace } from "../../store/workspace";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import {
  deriveFeatures,
  type DerivedFeature,
  type FeatureLifecycle,
} from "../../lib/features";
import { ProjectCard } from "./ProjectCard";
import { projectMatchesStatusFilter } from "../../lib/statusFilter";
import type { Task } from "../../lib/types";

// Map internal lifecycle key → wireframe tone/label.
const LIFECYCLE_TONE: Record<
  FeatureLifecycle,
  { tone: string; label: string }
> = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
};

export function OverviewGrid(): JSX.Element {
  const { data: projects } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const { runners } = useRunners();
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const hiddenProjects = useWorkspace((s) => s.hiddenProjects);
  const hideAllEmpty = useWorkspace((s) => s.hideAllEmpty);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const openModal = useModal((s) => s.open);
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const toast = useUI((s) => s.toast);

  const projectIds = projects ?? [];
  const hiddenSet = useMemo(
    () => new Set(hiddenProjects),
    [hiddenProjects],
  );

  // On first load — if user hasn't curated a hidden list yet, auto-hide
  // projects with zero tasks so the grid isn't overwhelmed. Runs once
  // when EVERY project's initial live-tasks snapshot has arrived so we
  // never hide a project whose SSE stream just hasn't delivered yet.
  const didAutoHide = useRef(false);
  useEffect(() => {
    if (didAutoHide.current) return;
    if (hiddenProjects.length > 0) {
      didAutoHide.current = true;
      return;
    }
    if (projectIds.length === 0) return;
    // Wait until every project has at least a first snapshot (tasks field
    // present, even if empty). Otherwise we could auto-hide `brain` in
    // the ~1s window between initial fetch and first SSE frame.
    const allSnapshotsIn = projectIds.every(
      (pid) => liveProjects[pid]?.tasks !== undefined,
    );
    if (!allSnapshotsIn) return;
    const nonEmpty = projectIds.filter(
      (pid) => (liveProjects[pid]?.tasks?.length ?? 0) > 0,
    );
    if (nonEmpty.length > 0 && nonEmpty.length < projectIds.length) {
      hideAllEmpty(projectIds, nonEmpty);
    }
    didAutoHide.current = true;
  }, [projectIds, liveProjects, hiddenProjects, hideAllEmpty]);

  const visibleProjectIds = useMemo(
    () =>
      projectIds.filter(
        (p) =>
          !hiddenSet.has(p) &&
          projectMatchesStatusFilter(
            liveProjects[p]?.tasks ?? [],
            statusFilter,
          ),
      ),
    [projectIds, hiddenSet, liveProjects, statusFilter],
  );

  // Derive every feature across VISIBLE projects for the shared panels.
  const allDerived = useMemo(() => {
    const out: Array<DerivedFeature & { projectId: string }> = [];
    for (const pid of visibleProjectIds) {
      const tasks = liveProjects[pid]?.tasks ?? [];
      const feats = deriveFeatures(tasks, pid);
      for (const f of feats) out.push(f);
    }
    return out;
  }, [visibleProjectIds, liveProjects]);

  // Group by lifecycle for the flow-board.
  const byLifecycle: Record<
    FeatureLifecycle,
    Array<DerivedFeature & { projectId: string }>
  > = {
    "in-progress": [],
    blocked: [],
    finished: [],
    "mr-open": [],
    merged: [],
  };
  for (const f of allDerived) byLifecycle[f.lifecycle].push(f);

  // "Needs attention" = blocked + mr-open + features whose runner is stale/offline
  const attention = useMemo(() => {
    return allDerived.filter((f) => {
      if (f.lifecycle === "blocked" || f.lifecycle === "mr-open") return true;
      const runnerId = featureAssignments[f.id];
      if (!runnerId) return false;
      const runner = runners.find((r) => r.runner_id === runnerId);
      return runner && runner.status !== "online";
    });
  }, [allDerived, featureAssignments, runners]);

  // Executable feature queue = in-progress features ordered by unblocked task count
  const executable = useMemo(
    () =>
      allDerived
        .filter((f) => f.lifecycle === "in-progress" && f.taskCount.total > 0)
        .sort((a, b) => b.taskCount.active - a.taskCount.active),
    [allDerived],
  );

  const onlineRunners = runners.filter((r) => r.status === "online").length;

  return (
    <div className="overview">
      {/* Workflow center */}
      <div className="workflow-center">
        <div className="wc-head">
          <div>
            <div className="wc-title">Workflow command center · all projects</div>
            <div className="wc-sub">
              Execute features, track automation consequences, and update Brain
              memory from one control surface.
            </div>
          </div>
          <button
            className="primary"
            onClick={() => {
              if (executable[0])
                toast(`Dispatch ${executable[0].id}`, "info");
              else toast("No executable features", "info");
            }}
          >
            Run next ready feature
          </button>
          <button onClick={() => openInFocus("browser", { url: "" }, "Brain")}>
            Open Brain entries
          </button>
        </div>
        <div className="wc-metrics">
          <div>
            <b>{executable.length}</b>
            <span> executable features</span>
          </div>
          <div>
            <b>
              {onlineRunners}/{runners.length}
            </b>
            <span> runners online</span>
          </div>
          <div>
            <b>{byLifecycle["mr-open"].length}</b>
            <span> MRs open</span>
          </div>
          <div>
            <b>{byLifecycle.merged.length}</b>
            <span> merged</span>
          </div>
        </div>
        <div className="wc-queue">
          {executable.slice(0, 5).map((f) => {
            const runnerId = featureAssignments[f.id];
            const runner = runners.find((r) => r.runner_id === runnerId);
            const tone = LIFECYCLE_TONE[f.lifecycle];
            return (
              <div key={`${f.projectId}:${f.id}`} className="wc-row">
                <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
                <span className="wc-feature">{f.name}</span>
                <span className="wc-meta">
                  {f.projectId} · {runner ? runner.runner_id : "unassigned"}
                </span>
                <button
                  onClick={() =>
                    openFeatureDrawer(f.projectId, f.id)
                  }
                >
                  Plan
                </button>
                <button
                  onClick={() =>
                    openModal("feature", {
                      projectId: f.projectId,
                      featureId: f.id,
                    })
                  }
                >
                  Details
                </button>
              </div>
            );
          })}
          {executable.length === 0 && (
            <div
              className="wc-row"
              style={{ color: "#6b757e", justifyContent: "center" }}
            >
              No executable features right now.
            </div>
          )}
        </div>
      </div>

      {/* Attention queue */}
      {attention.length > 0 && (
        <div className="review-queue">
          <div className="rq-head">
            <span>Needs attention</span>
            <span className="rq-count">
              {attention.length} feature{attention.length === 1 ? "" : "s"}
            </span>
          </div>
          <div className="rq-list">
            {attention.map((f) => {
              const tone = LIFECYCLE_TONE[f.lifecycle];
              const runnerId = featureAssignments[f.id];
              const runner = runners.find((r) => r.runner_id === runnerId);
              const runnerIssue =
                runner && runner.status !== "online"
                  ? `${runner.runner_id} ${runner.status}`
                  : null;
              return (
                <div
                  key={`${f.projectId}:${f.id}`}
                  className="rq-item"
                  onClick={() =>
                    openModal("feature", {
                      projectId: f.projectId,
                      featureId: f.id,
                    })
                  }
                >
                  <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
                  <span className="rq-name">{f.name}</span>
                  <span className="rq-meta">
                    {f.projectId}
                    {runnerIssue ? ` · ${runnerIssue}` : ""}
                    {f.prUrl && !runnerIssue ? ` · MR open` : ""}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Lifecycle board */}
      <div className="flow-board">
        {(
          [
            ["in-progress", "active"],
            ["blocked", "blocked"],
            ["finished", "finished"],
            ["mr-open", "mr"],
            ["merged", "merged"],
          ] as Array<[FeatureLifecycle, string]>
        ).map(([key, laneClass]) => {
          const items = byLifecycle[key];
          const tone = LIFECYCLE_TONE[key];
          return (
            <div key={key} className={`flow-lane ${laneClass}`}>
              <div className="lane-head">
                <span>{tone.label}</span>
                <b>{items.length}</b>
              </div>
              <div className="lane-items">
                {items.slice(0, 4).map((f) => (
                  <button
                    key={`${f.projectId}:${f.id}`}
                    className="lane-card"
                    onClick={() =>
                      openModal("feature", {
                        projectId: f.projectId,
                        featureId: f.id,
                      })
                    }
                  >
                    <span className="lane-name">{f.name}</span>
                    <span className="lane-meta">
                      {f.projectId} · {formatProgress(f)}
                    </span>
                  </button>
                ))}
                {items.length > 4 && (
                  <span className="lane-more">
                    +{items.length - 4} more
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* Project cards (visible only) */}
      {visibleProjectIds.map((pid) => (
        <ProjectCard key={pid} projectId={pid} />
      ))}

      {projectIds.length === 0 && (
        <div className="empty-state">
          <div>No projects yet.</div>
        </div>
      )}

      {projectIds.length > 0 && visibleProjectIds.length === 0 && (
        <div className="empty-state">
          <div>
            All projects are hidden. Click a project in the sidebar's
            Hidden group to bring one back.
          </div>
        </div>
      )}
    </div>
  );
}

function formatProgress(f: DerivedFeature): string {
  const total = f.taskCount.total;
  if (total === 0) return "no tasks";
  const done = f.taskCount.completed;
  const pct = Math.round((done / total) * 100);
  return `${pct}%`;
}

// helper re-export for tests
export type { Task };
