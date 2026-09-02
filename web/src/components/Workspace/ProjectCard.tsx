/**
 * ProjectCard — wireframe-parity port of `renderProjectCard`.
 *
 * DOM:
 *   .pcard[data-project=pid]
 *     .pcard-head (dial button · name · health · autos-paused · stats · close)
 *     .hold-strip (why the last scheduler pass dispatched nothing)
 *     .flow-strip (lifecycle pills)
 *     .pcard-tabs (Tasks | Goals | Automations | Focus icon)
 *     .pcard-body → CardTasks | CardGoals | CardAutomations
 *
 * There is no Features tab. Features are not a separate list — every one
 * of them is a group header in the Tasks tab, nested by
 * `feature_depends_on` and foldable, so the old tab was the same features
 * shown twice with the tasks removed. `CardFeatures` was deleted when its
 * three unique affordances (the dependency forest, the chain chips, and
 * the merged fold) moved into `CardTasks`.
 */
import { useState, useMemo } from "react";
import { useLive } from "../../lib/sse";
import { useWorkspace } from "../../store/workspace";
import {
  deriveFeatures,
  sortFeatures,
  type DerivedFeature,
} from "../../lib/features";
import { useMergeRequests } from "../../hooks/useMergeRequests";
import { usePauseState } from "../../hooks/usePauseState";
import { useSchedulerStatus } from "../../hooks/useSchedulerStatus";
import { useRowActions } from "../../hooks/useRowActions";
import {
  buildProjectActions,
  isProjectAutomationsPaused,
  isProjectTasksPaused,
} from "../../lib/actions/projectActions";
import { useProjectActionContext } from "../../hooks/useProjectActionContext";
import {
  projectPauseBadges,
  projectRunIndicator,
  schedulerHoldNote,
} from "../../lib/pause";
import { ProjectPauseButton } from "../common/ProjectPauseButton";
import { CardTasks } from "./CardTasks";
import { useDependentChainsSync } from "../../hooks/useDependentChains";
import { CardAutomations } from "./CardAutomations";
import { CardGoals } from "./CardGoals";
import type { Task } from "../../lib/types";

type TabKey = "tasks" | "goals" | "automations";

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
    else if (t.status === "completed" || t.status === "validated")
      s.completed++;
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
  if (blocked > 0 || stats.blocked > 0)
    return { label: "blocked", tone: "blocked" };
  if (mr > 0) return { label: "reviewing", tone: "mr" };
  if (stats.active > 0) return { label: "active", tone: "active" };
  return { label: "healthy", tone: "merged" };
}

export interface ProjectCardProps {
  projectId: string;
}

export function ProjectCard({ projectId }: ProjectCardProps): JSX.Element {
  const [tab, setTab] = useState<TabKey>("tasks");
  // Poll chain state for the whole card, not only for the tab that draws
  // the chips.
  //
  // The verbs that read it — "Cancel queued dependents" above all — are
  // built on every surface, including the overview. Polling inside the tab
  // body meant the cancel verb was silently absent everywhere except the
  // one view that happened to observe the query, with no error and no
  // disabled entry.
  useDependentChainsSync(projectId);
  const projectLive = useLive((s) => s.projects[projectId]);
  const tasks = projectLive?.tasks ?? EMPTY_TASKS;
  const connected = projectLive?.connected ?? false;
  const hasSnapshot =
    projectLive !== undefined && projectLive.tasks !== undefined;
  const { rowProps, overlays } = useRowActions();
  const { pause, isLoading: pauseLoading } = usePauseState();
  const { resultFor } = useSchedulerStatus();
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const hideProject = useWorkspace((s) => s.hideProject);

  const stats = useMemo(() => statsFor(tasks), [tasks]);
  // Brain-native MRs fold into lifecycle (see lib/mergeRequests).
  const { openByProject } = useMergeRequests();
  // Sorted into the canonical blocked → in-progress → mr-open → finished
  // → merged order. `sortFeatures` had no caller at all while a second,
  // flat feature list existed alongside this one; now that the Tasks tab
  // is the only feature list, the order it imposes IS the reading order —
  // and it is the one that puts what needs attention at the top and the
  // folded, finished work at the bottom.
  const features = useMemo(
    () =>
      sortFeatures(
        deriveFeatures(tasks, projectId, openByProject.get(projectId)),
      ),
    [tasks, projectId, openByProject],
  );
  const badges = projectPauseBadges(pause, projectId);
  const indicator = projectRunIndicator(tasks, {
    paused: badges.tasks,
    projectId,
  });
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
    if (tab === "automations")
      openInFocus("browser", { url: "" }, `${projectId} automations`);
    else openInFocus("task-detail", { projectId }, projectId);
  };

  const projectCtx = useProjectActionContext();

  // Project-level verbs come from the registry like everything else, so
  // the card header answers right-click, long-press AND the keyboard —
  // the old hand-rolled ContextMenu covered only the first.
  const projectActions = useMemo(
    () =>
      buildProjectActions(projectId, projectCtx, {
        taskCount: tasks.length,
        // undefined while loading: unknown must not disable the verb.
        tasksPaused: pauseLoading
          ? undefined
          : isProjectTasksPaused(pause, projectId),
        automationsPaused: pauseLoading
          ? undefined
          : isProjectAutomationsPaused(pause, projectId),
      }),
    [projectId, tasks.length, projectCtx, pause, pauseLoading],
  );

  return (
    <div className="pcard" data-project={projectId} style={{ maxHeight: 460 }}>
      <div className="pcard-head" {...rowProps(projectActions, projectId)}>
        {/* The dial replaces a dot that only ever reported SSE liveness
            — it never showed pause, so `.pcard-head .dot.paused` sat in
            the stylesheet unreachable. Connection state moves onto the
            name's tooltip, which is where "connecting…" belongs: it is a
            transient of the card, not a state of the project. */}
        <ProjectPauseButton
          projectId={projectId}
          indicator={indicator}
          taskCount={tasks.length}
          pauseLoading={pauseLoading}
        />
        <span
          className="name"
          title={
            !hasSnapshot ? "connecting…" : connected ? "live" : "reconnecting"
          }
        >
          {projectId}
        </span>
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
          <span className="health autos-paused" title={badges.automationsTitle}>
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
        <div className={`hold-strip ${holdNote.tone}`} title={holdNote.detail}>
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
        {tab === "goals" && <CardGoals projectId={projectId} />}
        {tab === "automations" && <CardAutomations projectId={projectId} />}
      </div>

      {overlays}
    </div>
  );
}

const EMPTY_TASKS: Task[] = [];
