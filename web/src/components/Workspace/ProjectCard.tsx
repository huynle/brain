/**
 * ProjectCard — wireframe-parity port of `renderProjectCard`.
 *
 * DOM:
 *   .pcard[data-project=pid]
 *     .pcard-head (dot · name · env · health · autos-paused · stats · close)
 *     .hold-strip (why the last scheduler pass dispatched nothing)
 *     .flow-strip (lifecycle pills)
 *     .pcard-tabs (Tasks | Features | More▾ | Focus icon)
 *     .pcard-body → CardTasks | CardFeatures | CardAutomations | CardSession | CardLogs
 */
import { useState, useMemo } from "react";
import { useLive } from "../../lib/sse";
import { useWorkspace } from "../../store/workspace";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { deriveFeatures, type DerivedFeature } from "../../lib/features";
import { useMergeRequests } from "../../hooks/useMergeRequests";
import { usePauseState } from "../../hooks/usePauseState";
import { useSchedulerStatus } from "../../hooks/useSchedulerStatus";
import { useRowActions } from "../../hooks/useRowActions";
import { runProject, summarizeRunProjectResult } from "../../lib/api";
import { buildProjectActions } from "../../lib/actions/projectActions";
import {
  forceDispatchNote,
  projectPauseBadges,
  schedulerHoldNote,
  withForceNote,
} from "../../lib/pause";
import { CardTasks } from "./CardTasks";
import { CardFeatures } from "./CardFeatures";
import { CardAutomations } from "./CardAutomations";
import { CardGoals } from "./CardGoals";
import type { Task } from "../../lib/types";

type TabKey = "tasks" | "features" | "goals" | "automations";

interface ProjectStats {
  active: number;
  ready: number;
  blocked: number;
  completed: number;
  total: number;
}

function statsFor(tasks: readonly Task[]): ProjectStats {
  const s: ProjectStats = {
    active: 0,
    ready: 0,
    blocked: 0,
    completed: 0,
    total: tasks.length,
  };
  for (const t of tasks) {
    if (t.status === "in_progress") s.active++;
    else if (t.status === "pending") s.ready++;
    else if (t.status === "blocked") s.blocked++;
    else if (t.status === "completed" || t.status === "validated") s.completed++;
  }
  return s;
}

// Health describes whether the project's WORK is in trouble. Pause describes
// whether work can move at all, and it outranks every label below: a paused
// project with clean tasks used to render "healthy" in green, which is the
// single most misleading thing this card could say. `paused` is passed in
// rather than read here so the function stays pure and testable.
function healthFor(
  stats: ProjectStats,
  features: DerivedFeature[],
  paused: boolean,
): { label: string; tone: string } {
  if (paused) return { label: "paused", tone: "paused" };
  const mr = features.filter((f) => f.lifecycle === "mr-open").length;
  const blocked = features.filter((f) => f.lifecycle === "blocked").length;
  if (blocked > 0 || stats.blocked > 0) return { label: "blocked", tone: "blocked" };
  if (mr > 0) return { label: "reviewing", tone: "mr" };
  if (stats.active > 0) return { label: "active", tone: "active" };
  return { label: "healthy", tone: "merged" };
}

export interface ProjectCardProps {
  projectId: string;
}

export function ProjectCard({ projectId }: ProjectCardProps): JSX.Element {
  const [tab, setTab] = useState<TabKey>("tasks");
  const projectLive = useLive((s) => s.projects[projectId]);
  const tasks = projectLive?.tasks ?? EMPTY_TASKS;
  const connected = projectLive?.connected ?? false;
  const hasSnapshot = projectLive !== undefined && projectLive.tasks !== undefined;
  const { rowProps, overlays } = useRowActions();
  const { pause } = usePauseState();
  const { resultFor } = useSchedulerStatus();
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const openModal = useModal((s) => s.open);
  const hideProject = useWorkspace((s) => s.hideProject);
  const toast = useUI((s) => s.toast);

  const stats = useMemo(() => statsFor(tasks), [tasks]);
  // Brain-native MRs fold into lifecycle (see lib/mergeRequests).
  const { openByProject } = useMergeRequests();
  const features = useMemo(
    () => deriveFeatures(tasks, projectId, openByProject.get(projectId)),
    [tasks, projectId, openByProject],
  );
  const badges = projectPauseBadges(pause, projectId);
  const health = useMemo(
    () => healthFor(stats, features, badges.tasks),
    [stats, features, badges.tasks],
  );
  // What the last scheduler pass actually did with this project's tasks —
  // the server's own account of why nothing moved.
  const holdNote = schedulerHoldNote(resultFor(projectId));

  const lifecycleCounts = useMemo(() => {
    const c = { active: 0, blocked: 0, finished: 0, mr: 0, merged: 0 };
    for (const f of features) {
      if (f.lifecycle === "in-progress") c.active++;
      else if (f.lifecycle === "blocked") c.blocked++;
      else if (f.lifecycle === "finished") c.finished++;
      else if (f.lifecycle === "mr-open") c.mr++;
      else if (f.lifecycle === "merged") c.merged++;
    }
    return c;
  }, [features]);

  const openInFocusForTab = () => {
    if (tab === "features")
      openModal("feature", { projectId, featureId: features[0]?.id });
    else if (tab === "automations")
      openInFocus("browser", { url: "" }, `${projectId} automations`);
    else openInFocus("task-detail", { projectId }, projectId);
  };

  // Project-level verbs come from the registry like everything else, so
  // the card header answers right-click, long-press AND the keyboard —
  // the old hand-rolled ContextMenu covered only the first.
  const projectActions = useMemo(
    () =>
      buildProjectActions(
        projectId,
        {
          runProject: async (pid) => {
            const r = await runProject(pid, false);
            // Run deliberately bypasses the project pause dials (see
            // SchedulerService.RunTaskNow). Saying so keeps an intentional
            // override from reading as "pause was silently ignored" —
            // and names the one dial force CANNOT cross.
            const note = forceDispatchNote(pause, { projectId: pid });
            toast(
              withForceNote(summarizeRunProjectResult(r), note),
              r.totalTasksDispatched > 0 ? "success" : "info",
            );
          },
          openTaskList: (pid) => openInFocus("task-detail", { projectId: pid }, pid),
          hideProject: (pid) => hideProject(pid),
        },
        { taskCount: tasks.length },
      ),
    [projectId, tasks.length, toast, openInFocus, hideProject, pause],
  );

  return (
    <div
      className="pcard"
      data-project={projectId}
      style={{ maxHeight: 460 }}
    >
      <div
        className="pcard-head"
        {...rowProps(projectActions, projectId)}
      >
        <span
          className={`dot ${!hasSnapshot ? "" : stats.active ? "busy" : "on"}`}
          title={!hasSnapshot ? "connecting…" : connected ? "live" : "reconnecting"}
        />
        <span className="name">{projectId}</span>
        <span
          className={`health ${health.tone}`}
          title={badges.tasks ? badges.tasksTitle : undefined}
        >
          {health.label}
        </span>
        {/* The automations dial is a DIFFERENT switch with a different
            meaning, so it gets its own indicator rather than folding into
            the health label. Both can be on at once. */}
        {badges.automations && (
          <span
            className="health autos-paused"
            title={badges.automationsTitle}
          >
            autos paused
          </span>
        )}
        <span className="spacer" />
        {!hasSnapshot ? (
          <span
            className="stats compact"
            style={{ color: "#6b757e", fontStyle: "italic" }}
          >
            connecting…
          </span>
        ) : (
          <span className="stats compact">
            <span className="active">
              <b>{stats.active}</b> active
            </span>
            <span className="ready">
              <b>{stats.ready}</b> ready
            </span>
            <span className="blocked">
              <b>{stats.blocked}</b> blocked
            </span>
            <span>
              <b>{stats.completed}</b> done
            </span>
          </span>
        )}
        <button
          className="close"
          title="Hide card"
          onClick={(e) => {
            e.stopPropagation();
            hideProject(projectId);
          }}
        >
          ×
        </button>
      </div>

      {/* The scheduler's own account of the last pass. A project whose tasks
          sit at ready with nothing running is the case this answers: the
          server already knew why, it just had no surface here. */}
      {holdNote && (
        <div
          className={`hold-strip ${holdNote.tone}`}
          title={holdNote.detail}
        >
          {holdNote.glyph} {holdNote.short}
        </div>
      )}

      {(lifecycleCounts.active > 0 ||
        lifecycleCounts.blocked > 0 ||
        lifecycleCounts.finished > 0 ||
        lifecycleCounts.mr > 0 ||
        lifecycleCounts.merged > 0) && (
        <div className="flow-strip">
          {lifecycleCounts.active > 0 && (
            <span className="flow-pill active">
              <b>{lifecycleCounts.active}</b> active
            </span>
          )}
          {lifecycleCounts.blocked > 0 && (
            <span className="flow-pill blocked">
              <b>{lifecycleCounts.blocked}</b> blocked
            </span>
          )}
          {lifecycleCounts.finished > 0 && (
            <span className="flow-pill finished">
              <b>{lifecycleCounts.finished}</b> finished
            </span>
          )}
          {lifecycleCounts.mr > 0 && (
            <span className="flow-pill mr">
              <b>{lifecycleCounts.mr}</b> MR
            </span>
          )}
          {lifecycleCounts.merged > 0 && (
            <span className="flow-pill merged">
              <b>{lifecycleCounts.merged}</b> merged
            </span>
          )}
        </div>
      )}

      <div className="pcard-tabs">
        <button
          className={tab === "tasks" ? "active" : ""}
          onClick={() => setTab("tasks")}
        >
          Tasks
        </button>
        <button
          className={tab === "features" ? "active" : ""}
          onClick={() => setTab("features")}
        >
          Features
        </button>
        <button
          className={tab === "goals" ? "active" : ""}
          onClick={() => setTab("goals")}
        >
          Goals
        </button>
        <button
          className={tab === "automations" ? "active" : ""}
          onClick={() => setTab("automations")}
        >
          Automations
        </button>
        <span className="spacer" />
        <button
          className="icon"
          title="Open in focus"
          onClick={openInFocusForTab}
        >
          ⤢
        </button>
      </div>

      <div className="pcard-body">
        {tab === "tasks" && (
          <CardTasks projectId={projectId} tasks={tasks} features={features} />
        )}
        {tab === "features" && (
          <CardFeatures projectId={projectId} features={features} />
        )}
        {tab === "goals" && <CardGoals projectId={projectId} />}
        {tab === "automations" && <CardAutomations projectId={projectId} />}
      </div>

      {overlays}
    </div>
  );
}

const EMPTY_TASKS: Task[] = [];
