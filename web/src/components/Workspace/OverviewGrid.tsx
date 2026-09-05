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
import { useEffect, useMemo, useRef, useState } from "react";
import { useProjects } from "../../hooks/useProjects";
import { useLive } from "../../lib/sse";
import { useRunners } from "../../hooks/useRunners";
import { useMergeRequests } from "../../hooks/useMergeRequests";
import { useRowActions } from "../../hooks/useRowActions";
import { useFeatureActionContextFactory } from "../../hooks/useFeatureActionContext";
import { useWorkspace } from "../../store/workspace";
import { useFeatureAssignments } from "../../hooks/useFeatureAssignments";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { buildFeatureActions } from "../../lib/actions/featureActions";
import {
  deriveFeatures,
  type DerivedFeature,
  type FeatureLifecycle,
} from "../../lib/features";
import { laneVisible } from "../../lib/lane";
import { useDeferredPreview } from "../../hooks/useDeferredPreview";
import {
  LIFECYCLE_TONE,
  LifecycleBadge,
  MergeRequestLink,
} from "../common/LifecycleBadge";
import { ProjectTiles } from "./ProjectTiles";
import { EntriesPreview } from "./EntriesPreview";
import { projectMatchesStatusFilter } from "../../lib/statusFilter";
import type { Task } from "../../lib/types";

export function OverviewGrid(): JSX.Element {
  const { data: projects } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const { runners } = useRunners();
  // Server-resolved (RunnerInfo.feature_assignments), with the local
  // optimistic map layered on. Reading the local map directly is what
  // hid every auto-assignment and lied after a reload elsewhere.
  const featureAssignments = useFeatureAssignments();
  const hiddenProjects = useWorkspace((s) => s.hiddenProjects);
  const hideAllEmpty = useWorkspace((s) => s.hideAllEmpty);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const openModal = useModal((s) => s.open);
  // openOrReuseInSidebar, not openInSidebar: the latter opens a NEW tab
  // every time, so clicking down a lane of features would leave one pane
  // per click. Reuse is what makes single-click preview viable at all.
  const previewInSidebar = useWorkspace((s) => s.openOrReuseInSidebar);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const setView = useWorkspace((s) => s.setView);
  const toast = useUI((s) => s.toast);

  // Per-lane expansion for the flow-board, keyed by lifecycle. Ephemeral
  // on purpose — live buffers reset on reload per wireframe rules.
  const [expandedLanes, setExpandedLanes] = useState<Record<string, boolean>>(
    {},
  );

  // Feature rows here span projects, so the factory form supplies a
  // context per row's own project. One useRowActions instance serves the
  // whole grid — its overlays render once at the end.
  const featureCtxFor = useFeatureActionContextFactory();
  const { rowProps, overlays } = useRowActions();
  // Every feature surface on this page follows the app-wide click
  // contract: single click previews in the side panel, double click pins
  // into Focus. These used to open a MODAL instead — the one place in the
  // app where clicking a feature took over the screen.
  const preview = useDeferredPreview();
  const previewFeature = (f: DerivedFeature & { projectId: string }) =>
    previewInSidebar(
      "feature-detail",
      { projectId: f.projectId, featureId: f.id },
      f.name,
    );
  const pinFeature = (f: DerivedFeature & { projectId: string }) => {
    preview.cancel();
    openInFocus(
      "feature-detail",
      { projectId: f.projectId, featureId: f.id },
      f.name,
    );
  };
  // Enter previews immediately — the keyboard has no double-click to wait
  // out.
  const featureRowProps = (f: DerivedFeature & { projectId: string }) =>
    rowProps(buildFeatureActions(f, featureCtxFor(f.projectId)), f.name, () =>
      previewFeature(f),
    );

  const projectIds = projects ?? [];
  const hiddenSet = useMemo(() => new Set(hiddenProjects), [hiddenProjects]);

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

  // Brain-native merge requests: an auto_pr checkout produces a
  // merge_request ENTRY, not a URL on a task — without this input the
  // READY TO MERGE column never moves for AI-checked-out features.
  // `openByProject` is the query's stable data reference, so the memo
  // below only recomputes when the MR set actually changes.
  const { openByProject } = useMergeRequests();

  // Derive every feature across VISIBLE projects for the shared panels.
  const allDerived = useMemo(() => {
    const out: Array<DerivedFeature & { projectId: string }> = [];
    for (const pid of visibleProjectIds) {
      const tasks = liveProjects[pid]?.tasks ?? [];
      const feats = deriveFeatures(tasks, pid, openByProject.get(pid));
      for (const f of feats) out.push(f);
    }
    return out;
  }, [visibleProjectIds, liveProjects, openByProject]);

  // Group by lifecycle for the flow-board.
  const byLifecycle: Record<
    FeatureLifecycle,
    Array<DerivedFeature & { projectId: string }>
  > = {
    "in-progress": [],
    blocked: [],
    finished: [],
    "ready-to-merge": [],
    merged: [],
  };
  for (const f of allDerived) byLifecycle[f.lifecycle].push(f);

  // "Needs attention" = blocked + ready-to-merge + features whose runner is
  // stale/offline. Ready-to-merge qualifies because the work has stopped and
  // is waiting on the merge executor. A forge URL is NOT an attention signal:
  // nothing here tracks whether that MR is still open (see lib/features).
  const attention = useMemo(() => {
    return allDerived.filter((f) => {
      if (f.lifecycle === "blocked" || f.lifecycle === "ready-to-merge")
        return true;
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

  // "Online" here has to mean "can take work". A paused runner heartbeats as
  // online but refuses every dispatch, so counting it made the command
  // center's headline metric read 1/1 on a fleet that would run nothing.
  const onlineRunners = runners.filter(
    (r) => r.status === "online" && !r.paused,
  ).length;
  const pausedRunners = runners.filter((r) => r.paused).length;

  return (
    <div className="overview">
      {/* Workflow center */}
      <div className="workflow-center">
        <div className="wc-head">
          <div>
            <div className="wc-title">
              Workflow command center · all projects
            </div>
            <div className="wc-sub">
              Execute features, track automation consequences, and update Brain
              memory from one control surface.
            </div>
          </div>
          <button
            className="primary"
            onClick={() => {
              if (executable[0]) toast(`Dispatch ${executable[0].id}`, "info");
              else toast("No executable features", "info");
            }}
          >
            Run next ready feature
          </button>
          <button onClick={() => setView("entries")}>Open Brain entries</button>
        </div>
        <div className="wc-metrics">
          <div>
            <b>{executable.length}</b>
            <span> executable features</span>
          </div>
          <div>
            <b className={pausedRunners > 0 ? "wc-metric-warn" : undefined}>
              {onlineRunners}/{runners.length}
            </b>
            <span>
              {" "}
              runners online
              {pausedRunners > 0 && (
                <span
                  className="wc-metric-note"
                  title={`${pausedRunners} runner${pausedRunners === 1 ? " is" : "s are"} paused and will not accept dispatches`}
                >
                  {" "}
                  · {pausedRunners} paused
                </span>
              )}
            </span>
          </div>
          <div>
            <b>{allDerived.filter((f) => f.prUrl).length}</b>
            <span> with MR links</span>
          </div>
          <div>
            <b>{byLifecycle["ready-to-merge"].length}</b>
            <span> ready to merge</span>
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
            return (
              <div
                key={`${f.projectId}:${f.id}`}
                className="wc-row"
                {...featureRowProps(f)}
              >
                <span className="chip-pair">
                  <LifecycleBadge lifecycle={f.lifecycle} />
                  {f.prUrl && <MergeRequestLink href={f.prUrl} />}
                </span>
                <span className="wc-feature">{f.name}</span>
                <span className="wc-meta">
                  {f.projectId} · {runner ? runner.runner_id : "unassigned"}
                </span>
                <button onClick={() => previewFeature(f)}>Plan</button>
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
                  {...featureRowProps(f)}
                  onClick={(e) => {
                    if ((e.target as HTMLElement).closest("button")) return;
                    preview.schedule(() => previewFeature(f));
                  }}
                  onDoubleClick={(e) => {
                    if ((e.target as HTMLElement).closest("button")) return;
                    pinFeature(f);
                  }}
                >
                  <span className="chip-pair">
                    <LifecycleBadge lifecycle={f.lifecycle} />
                    {f.prUrl && <MergeRequestLink href={f.prUrl} />}
                  </span>
                  <span className="rq-name">{f.name}</span>
                  <span className="rq-meta">
                    {f.projectId}
                    {runnerIssue ? ` · ${runnerIssue}` : ""}
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
            ["ready-to-merge", "ready"],
            ["merged", "merged"],
          ] as Array<[FeatureLifecycle, string]>
        ).map(([key, laneClass]) => {
          const items = byLifecycle[key];
          const tone = LIFECYCLE_TONE[key];
          const expanded = !!expandedLanes[key];
          const { visible, hiddenCount } = laneVisible(items, expanded);
          return (
            <div key={key} className={`flow-lane ${laneClass}`}>
              <div className="lane-head">
                <span>{tone.label}</span>
                <b>{items.length}</b>
              </div>
              <div className={`lane-items${expanded ? " expanded" : ""}`}>
                {visible.map((f) => (
                  <button
                    key={`${f.projectId}:${f.id}`}
                    className="lane-card"
                    {...featureRowProps(f)}
                    onClick={() => preview.schedule(() => previewFeature(f))}
                    onDoubleClick={() => pinFeature(f)}
                  >
                    <span className="lane-name">{f.name}</span>
                    <span className="lane-meta">
                      {f.projectId} · {formatProgress(f)}
                    </span>
                  </button>
                ))}
                {(hiddenCount > 0 || expanded) && (
                  <button
                    type="button"
                    className="lane-more"
                    onClick={() =>
                      setExpandedLanes((prev) => ({
                        ...prev,
                        [key]: !prev[key],
                      }))
                    }
                  >
                    {expanded ? "Show less" : `+${hiddenCount} more`}
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* Brain memory carousel */}
      <EntriesPreview />

      {/* Project cards (visible only), tiled — see ProjectTiles for why
          they are not just more children of this grid. */}
      <ProjectTiles projectIds={visibleProjectIds} />

      {projectIds.length === 0 && (
        <div className="empty-state">
          <div>No projects yet.</div>
        </div>
      )}

      {projectIds.length > 0 && visibleProjectIds.length === 0 && (
        <div className="empty-state">
          <div>
            All projects are hidden. Click a project in the sidebar's Hidden
            group to bring one back.
          </div>
        </div>
      )}

      {overlays}
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
