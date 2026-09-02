/**
 * CardTasks — the project card's one list of work.
 *
 * Groups tasks by feature. Each feature shows:
 *   .feat[state]
 *     .feat-head (caret · name · life-badge · chain-chip · assign-chip ·
 *                 progress bar · % text)
 *     .trow × N (glyph · name · status [+ .hold-chip] · id)
 *
 * ─── Two dependency trees, one view ──────────────────────────────
 *
 * WITHIN a feature the task rows form a dependency tree: a task that
 * depends on another renders indented beneath it, so a plan reads
 * top-down in execution order (`lib/taskTree`).
 *
 * ACROSS features the feature headers do the same, nested by
 * `feature_depends_on` (`buildFeatureForest`). That used to live on a
 * separate Features tab, which meant the two halves of one dependency
 * graph were never on screen together — and it was the justification
 * for dropping cross-feature task edges here. Both trees now render in
 * one list, so a task gated by another feature sits visibly beneath the
 * feature it waits on.
 *
 * ─── Folding ─────────────────────────────────────────────────────
 *
 * A feature's rows collapse to just the header. Finished and merged
 * features start folded (`isFeatureDone`), because a completed feature
 * is a one-line fact, not a list to scroll past — the store only records
 * the folds a user explicitly clicked (`featureCollapsed`).
 *
 * Every row's verbs come from `lib/actions` via `useRowActions`, so
 * right-click, long-press and keyboard all offer the identical set —
 * including the ungrouped "No feature" rows, which previously had no
 * action affordance at all.
 */
import { useMemo } from "react";
import { useSelection } from "../../store/selection";
import { useWorkspace } from "../../store/workspace";
import { useRunners } from "../../hooks/useRunners";
import { usePauseState } from "../../hooks/usePauseState";
import { useDependentChains } from "../../hooks/useDependentChains";
import { useRowActions } from "../../hooks/useRowActions";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { useFeatureActionContext } from "../../hooks/useFeatureActionContext";
import { DepGuide } from "../common/DepGuide";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { buildTaskActions } from "../../lib/actions/taskActions";
import { buildFeatureActions } from "../../lib/actions/featureActions";
import { buildSelectionActions } from "../../lib/actions/selectionActions";
import { isRangeKey } from "../../lib/selection";
import { featureDepWarning, taskHoldReason } from "../../lib/pause";
import { CHAIN_QUEUED_TITLE, chainRootTitle } from "../../lib/chains";
import { buildTaskForest } from "../../lib/taskTree";
import { flattenDepForest, type DepRow } from "../../lib/depTree";
import type { Task } from "../../lib/types";
import {
  buildFeatureForest,
  isFeatureDone,
  type DerivedFeature,
} from "../../lib/features";

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

function taskGlyph(
  status: Task["status"],
  isAbandoned = false,
): {
  glyph: string;
  cls: string;
} {
  // Abandoned tasks override the status glyph with a "⟳" (recovery) marker
  // in amber. Distinct from ✕ (blocked) and ▸ (in-progress) so a user
  // scanning the row can spot resumable work without opening the modal.
  if (isAbandoned) {
    return { glyph: "⟳", cls: "abandoned" };
  }
  switch (status) {
    case "in_progress":
      return { glyph: "▸", cls: "busy" };
    case "blocked":
      return { glyph: "✕", cls: "blk" };
    case "completed":
    case "validated":
      return { glyph: "✓", cls: "ok" };
    case "pending":
      return { glyph: "▪", cls: "" };
    default:
      return { glyph: "○", cls: "" };
  }
}

function featStateClass(f: DerivedFeature): string {
  if (f.lifecycle === "blocked") return "block";
  if (f.lifecycle === "merged" || f.lifecycle === "finished") return "done";
  return "busy";
}

/** Bucket key for tasks with no feature_id. Not a legal feature id, so
 *  it cannot collide with a real one — including as a key in the
 *  per-project `featureCollapsed` map, where it holds this bucket's fold. */
const NO_FEATURE = "__nofeat__";

/** The tri-state fold: an explicit click outranks the lifecycle default.
 *  Module-level and pure so the render and the shift-click ordering read
 *  it through the SAME expression — they must agree exactly, or a range
 *  selection reaches rows nobody can see. */
const isCollapsed = (
  map: Record<string, boolean>,
  f: DerivedFeature,
): boolean => map[f.id] ?? isFeatureDone(f);

export interface CardTasksProps {
  projectId: string;
  tasks: readonly Task[];
  features: DerivedFeature[];
}

export function CardTasks({
  projectId,
  tasks,
  features,
}: CardTasksProps): JSX.Element {
  const openInSidebar = useWorkspace((s) => s.openInSidebar);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const archivedExpanded = useWorkspace(
    (s) => s.archivedExpanded[projectId] ?? false,
  );
  const toggleArchivedExpanded = useWorkspace((s) => s.toggleArchivedExpanded);
  // Whole map for this project, not a per-feature selector: the feature
  // list is built inside a render, so there is no stable place to call one
  // hook per feature. It changes only when a fold is clicked.
  const featureCollapsed = useWorkspace(
    (s) => s.featureCollapsed[projectId] ?? EMPTY_COLLAPSE,
  );
  const toggleFeatureCollapsed = useWorkspace((s) => s.toggleFeatureCollapsed);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const { runners } = useRunners();
  const { pause } = usePauseState();
  // A standing "run feature + dependents" request is a queue the server
  // drains on its own. Nothing else on screen would mention the features
  // it enrolled, so the chips below are the only place they exist.
  // The POLL lives in ProjectCard; this is a cache reader.
  const chains = useDependentChains(projectId);

  const taskCtx = useTaskActionContext(projectId);
  const featureCtx = useFeatureActionContext(projectId);
  const { rowProps, overlays } = useRowActions();

  // Subscribed (not getState) so checkboxes and the "marked" tint react
  // to every toggle, from any surface — checkbox, `v` key, or menu.
  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleTaskSel = useSelection((s) => s.toggleTask);
  const toggleFeatureSel = useSelection((s) => s.toggleFeature);
  const rangeTaskSel = useSelection((s) => s.rangeTask);
  const rangeFeatureSel = useSelection((s) => s.rangeFeature);
  const requestVerb = useSelection((s) => s.requestVerb);
  const clearSel = useSelection((s) => s.clear);
  // The single "active" row (plain single-click select-only highlight),
  // separate from the checkbox multi-select above. `selActiveRow` is the
  // active-row record; do NOT confuse with `selActive` (multi-select mode
  // boolean) computed below.
  const selActiveRow = useSelection((s) => s.active);
  const setActive = useSelection((s) => s.setActive);
  const selScoped = selProjectId === projectId;
  // Selection mode: once anything in this project is marked, every row
  // shows its checkbox. Until then no row shows one — hover and focus
  // deliberately do not reveal it. Entering selection mode is explicit:
  // the row menu's Select verb, `v`, shift-click, or long-press.
  const selActive =
    selScoped && (selTaskIds.size > 0 || selFeatureIds.size > 0);

  // The whole-selection verbs marked rows offer on right-click /
  // long-press / `m` instead of their own menu.
  const selCount = selScoped ? selTaskIds.size + selFeatureIds.size : 0;
  const selectionActions = useMemo(
    () =>
      selCount > 0
        ? buildSelectionActions({
            count: selCount,
            requestVerb,
            clearSelection: clearSel,
          })
        : null,
    [selCount, requestVerb, clearSel],
  );

  // Archived tasks leave the default rows entirely (mirroring the server
  // rule that they count toward nothing) and collect in a fold at the
  // bottom. The sidebar "Archived" filter forces the fold open — the user
  // asked to see exactly these rows.
  const archivedTasks = useMemo(
    () => tasks.filter((t) => t.status === "archived"),
    [tasks],
  );
  const archivedForced = statusFilter === "archived";
  const showArchived =
    archivedTasks.length > 0 && (archivedExpanded || archivedForced);

  // Group tasks by feature_id, then turn each bucket into
  // dependency-ordered rows. Dependency edges are resolved per bucket, so
  // a cross-feature dep does not drag a task out of its own feature — it
  // stays a root here, and the relationship is carried by the feature
  // forest below, which now nests in this same list.
  const rowsByFeat = useMemo(() => {
    const buckets = new Map<string, Task[]>();
    for (const t of tasks) {
      if (t.status === "archived") continue; // rendered in the fold below
      const key = t.feature_id ?? NO_FEATURE;
      const arr = buckets.get(key);
      if (arr) arr.push(t);
      else buckets.set(key, [t]);
    }
    const m = new Map<string, DepRow<Task>[]>();
    for (const [key, bucket] of buckets) {
      m.set(key, flattenDepForest(buildTaskForest(bucket)));
    }
    return m;
  }, [tasks]);

  const orphanRows = rowsByFeat.get(NO_FEATURE) ?? [];
  const orphanCollapsed = featureCollapsed[NO_FEATURE] ?? false;

  // Archived rows keep their dependency ordering but render in one flat
  // group: an all-archived feature derives no DerivedFeature (it left the
  // lanes), so there is no per-feature header to hang them under.
  const archivedRows = useMemo(
    () => flattenDepForest(buildTaskForest(archivedTasks)),
    [archivedTasks],
  );

  // Feature headers nested by `feature_depends_on`. Roots keep the caller's
  // order (ProjectCard sorts them blocked → … → merged), so the canonical
  // sequence holds at every level of the tree.
  //
  // DONE features form a SECOND forest rather than joining the first. A
  // finished dependency is still a legal placement parent, and letting it
  // act as one drags its live dependents down the list underneath it —
  // an in-progress feature rendering below a folded merged one, which
  // undoes both the sort and the point of folding finished work away.
  // Excluding them promotes those dependents back to roots, exactly as
  // filtering a hidden parent out of the tree used to.
  const featureRows = useMemo(() => {
    const active: DerivedFeature[] = [];
    const done: DerivedFeature[] = [];
    for (const f of features) (isFeatureDone(f) ? done : active).push(f);
    return [
      ...flattenDepForest(buildFeatureForest(active)),
      ...flattenDepForest(buildFeatureForest(done)),
    ];
  }, [features]);

  // Visual order of every task row on screen, for shift-click ranges.
  // Must mirror the render exactly: features in tree order and only while
  // expanded, then the "No feature" bucket, then the archived fold only
  // while it is open — a range never reaches rows the user cannot see.
  const orderedTaskIds = useMemo(() => {
    const ids: string[] = [];
    for (const row of featureRows) {
      const f = row.node.item;
      if (isCollapsed(featureCollapsed, f)) continue;
      for (const r of rowsByFeat.get(f.id) ?? []) ids.push(r.node.item.id);
    }
    if (!(featureCollapsed[NO_FEATURE] ?? false))
      for (const row of rowsByFeat.get(NO_FEATURE) ?? [])
        ids.push(row.node.item.id);
    if (showArchived)
      for (const row of archivedRows) ids.push(row.node.item.id);
    return ids;
  }, [featureRows, featureCollapsed, rowsByFeat, showArchived, archivedRows]);

  // Feature headers range over the headers only, in their render order.
  const orderedFeatureIds = useMemo(
    () => featureRows.map((row) => row.node.item.id),
    [featureRows],
  );

  /** One task row, identical whether it sits under a feature or not. */
  const renderTaskRow = (row: DepRow<Task>) => {
    const t = row.node.item;
    const { glyph, cls } = taskGlyph(t.status, !!t.is_abandoned);
    const label = t.title || t.id;
    // Why a task is not running. Null for every task that is running or
    // simply not held — the chip only appears when there is a real answer.
    // Also covers feature_depends_on gating, whose blocking party is a
    // FEATURE and so has no row in this tree to point at.
    const hold = taskHoldReason(t, { pause, projectId });
    // Orthogonal to `hold`: an unresolved feature dep gates nothing, so it
    // can sit on a task that is running perfectly well. Shown alongside
    // rather than instead, because "running" and "ordered by a typo that
    // does nothing" are both true at once.
    const depWarn = featureDepWarning(t);
    const actions = buildTaskActions(t, taskCtx);
    const marked = selScoped && selTaskIds.has(t.id);
    // Single-click select-only highlight — one active row at a time,
    // independent of the checkbox multi-select. Never toggles on
    // selActive: the active state is set directly by a plain click.
    const isActive =
      selActiveRow?.projectId === projectId &&
      selActiveRow.kind === "task" &&
      selActiveRow.id === t.id;
    const rp = rowProps(
      actions,
      label,
      // Selection mode is modal: Enter toggles like a click, it does
      // not open detail.
      selActive
        ? () => toggleTaskSel(projectId, t.id)
        : () => openInSidebar("task-detail", { projectId, taskId: t.id }, label),
      {
        selectionActions: marked ? (selectionActions ?? undefined) : undefined,
        // Long-press = the touch shift-click.
        onRangeSelect: () => rangeTaskSel(projectId, orderedTaskIds, t.id),
      },
    );

    return (
      <div
        key={t.id}
        className={`trow${marked ? " marked" : ""}${isActive ? " active" : ""}`}
        {...rp}
        onKeyDown={(e) => {
          // Shift+V ranges from the anchor — keyboard parity with
          // shift-click for rows focused via Tab.
          if (isRangeKey(e)) {
            e.preventDefault();
            rangeTaskSel(projectId, orderedTaskIds, t.id);
            return;
          }
          rp.onKeyDown(e);
        }}
        onClick={(e) => {
          if ((e.target as HTMLElement).closest(".selbox")) return;
          // Shift-click anywhere on the row is a selection gesture, not
          // an open: range from the anchor, or start a selection here.
          if (e.shiftKey) {
            rangeTaskSel(projectId, orderedTaskIds, t.id);
            return;
          }
          // Selection mode is modal: clicks toggle, they never open.
          if (selActive) {
            toggleTaskSel(projectId, t.id);
            return;
          }
          // Plain single-click: select-only highlight. Does NOT open the
          // modal — double-click / Enter do that.
          setActive(projectId, "task", t.id);
        }}
        onDoubleClick={(e) => {
          if ((e.target as HTMLElement).closest(".selbox")) return;
          // A double-click in multi-select mode must not open — mirror
          // the click guards.
          if (selActive) return;
          openInSidebar("task-detail", { projectId, taskId: t.id }, label);
        }}
        // Shift-click would otherwise extend the browser's text
        // selection across the rows before our click handler runs. The
        // explicit focus() keeps what preventDefault suppresses: row
        // accelerators must target the row that was last clicked.
        onMouseDown={(e) => {
          if (e.shiftKey) {
            e.preventDefault();
            e.currentTarget.focus();
          }
        }}
        draggable
        onDragStart={(e) =>
          beginDrag(e, {
            source: "task-row",
            kind: "task-detail",
            target: { projectId, taskId: t.id },
            title: label,
          })
        }
        onDragEnd={endDrag}
      >
        {/* Checkbox and status glyph share one grid cell. The glyph holds
            it unless `boxed` (marked, or selection mode is on) swaps the
            checkbox in. A plain click reads as colour only — `.active`.
            Keeps the 4-column row layout intact. */}
        <span className={`glyph-slot${marked || selActive ? " boxed" : ""}`}>
          <span
            className={`selbox${marked ? " on" : ""}`}
            role="checkbox"
            aria-checked={marked}
            aria-label={`Select ${label}`}
            onClick={(e) => {
              e.stopPropagation();
              if (e.shiftKey) rangeTaskSel(projectId, orderedTaskIds, t.id);
              else toggleTaskSel(projectId, t.id);
            }}
          />
          <span className={`glyph ${cls}`}>{glyph}</span>
        </span>
        <span className="name">
          <DepGuide
            prefix={row.prefix}
            inCycle={row.node.inCycle}
            extraDeps={row.node.extraDeps}
            extraLabel="tasks"
          />
          {label}
        </span>
        <span className="status">
          {t.status}
          {hold && (
            <span className={`hold-chip ${hold.code}`} title={hold.detail}>
              {hold.glyph} {hold.short}
            </span>
          )}
          {depWarn && (
            <span
              className={`hold-chip ${depWarn.code}`}
              title={depWarn.detail}
            >
              {depWarn.glyph} {depWarn.short}
            </span>
          )}
        </span>
        <span className="id">{t.id.slice(0, 6)}</span>
      </div>
    );
  };

  return (
    <div>
      {featureRows.map((frow) => {
        const f = frow.node.item;
        const collapsed = isCollapsed(featureCollapsed, f);
        const rows = collapsed ? EMPTY_ROWS : (rowsByFeat.get(f.id) ?? []);
        const taskTotal = f.taskCount.total;
        const stateClass = featStateClass(f);
        const tone = LIFECYCLE_TONE[f.lifecycle];
        const runnerId = featureAssignments[f.id];
        const runner = runners.find((r) => r.runner_id === runnerId);
        const pct = Math.round(f.progress * 100);
        const featureActions = buildFeatureActions(f, featureCtx);
        const featMarked = selScoped && selFeatureIds.has(f.id);
        // Single-click select-only highlight for the feature head.
        const featIsActive =
          selActiveRow?.projectId === projectId &&
          selActiveRow.kind === "feature" &&
          selActiveRow.id === f.id;
        const rpHead = rowProps(
          featureActions,
          f.name,
          selActive
            ? () => toggleFeatureSel(projectId, f.id)
            : () => openInSidebar("feature-detail", { projectId, featureId: f.id }, f.name),
          {
            selectionActions: featMarked
              ? (selectionActions ?? undefined)
              : undefined,
            // Long-press = the touch shift-click.
            onRangeSelect: () =>
              rangeFeatureSel(projectId, orderedFeatureIds, f.id),
          },
        );

        return (
          <div
            key={f.id}
            className={`feat ${stateClass}${collapsed ? " collapsed" : ""}`}
            // Indent nested features so the tree reads at a glance. The
            // guide glyphs carry the exact structure; this gives each
            // level a visible step.
            style={
              frow.depth > 0 ? { marginLeft: frow.depth * 12 } : undefined
            }
          >
            <div
              className={`feat-head${featMarked ? " marked" : ""}${featIsActive ? " active" : ""}`}
              {...rpHead}
              onKeyDown={(e) => {
                if (isRangeKey(e)) {
                  e.preventDefault();
                  rangeFeatureSel(projectId, orderedFeatureIds, f.id);
                  return;
                }
                // Tree convention, and deliberately NOT a letter: every
                // single-key accelerator on this row belongs to the
                // feature verb registry, and arrows collide with nothing.
                //
                // The modifier guard is the row handler's own rule ("never
                // fight browser/OS chords") applied one level earlier —
                // this branch returns before delegating, so without it
                // ⌘←/Alt+← would fold a feature instead of going back.
                if (
                  !e.metaKey &&
                  !e.ctrlKey &&
                  !e.altKey &&
                  (e.key === "ArrowLeft" || e.key === "ArrowRight")
                ) {
                  e.preventDefault();
                  if (collapsed !== (e.key === "ArrowLeft")) {
                    toggleFeatureCollapsed(projectId, f.id, isFeatureDone(f));
                  }
                  return;
                }
                rpHead.onKeyDown(e);
              }}
              draggable
              onDragStart={(e) =>
                beginDrag(e, {
                  source: "feature-header",
                  kind: "assign",
                  target: {
                    projectId,
                    featureId: f.id,
                    currentRunnerId: runnerId,
                  },
                  title: f.name,
                })
              }
              onDragEnd={endDrag}
              onClick={(e) => {
                if (
                  (e.target as HTMLElement).closest(
                    "button, .caret, .assign-chip, .selbox",
                  )
                )
                  return;
                if (e.shiftKey) {
                  rangeFeatureSel(projectId, orderedFeatureIds, f.id);
                  return;
                }
                if (selActive) {
                  toggleFeatureSel(projectId, f.id);
                  return;
                }
                // Plain single-click: select-only highlight. Double-click
                // / Enter open the drawer.
                setActive(projectId, "feature", f.id);
              }}
              onDoubleClick={(e) => {
                if (
                  (e.target as HTMLElement).closest(
                    "button, .caret, .assign-chip, .selbox",
                  )
                )
                  return;
                if (selActive) return;
                openInSidebar("feature-detail", { projectId, featureId: f.id }, f.name);
              }}
              onMouseDown={(e) => {
                if (e.shiftKey) {
                  e.preventDefault();
                  e.currentTarget.focus();
                }
              }}
            >
              {/* The caret's slot doubles as the feature checkbox once
                  selection mode is on — no extra column, no shift. The
                  caret is a real control now: the head's own click
                  handler already excluded `.caret`, so for the whole life
                  of this component it was an arrow that pointed at a fold
                  nobody could operate. Not a <button>: this row is itself
                  role="button", and a nested one both breaks nesting
                  rules and swallows the row's key accelerators. */}
              <span
                className={`glyph-slot${featMarked || selActive ? " boxed" : ""}`}
              >
                <span
                  className={`selbox${featMarked ? " on" : ""}`}
                  role="checkbox"
                  aria-checked={featMarked}
                  aria-label={`Select feature ${f.name}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    if (e.shiftKey)
                      rangeFeatureSel(projectId, orderedFeatureIds, f.id);
                    else toggleFeatureSel(projectId, f.id);
                  }}
                />
                <span
                  className="caret"
                  role="button"
                  // Out of the tab order on purpose: the row is the
                  // focusable unit, and ← / → fold it from there.
                  tabIndex={-1}
                  aria-expanded={!collapsed}
                  aria-label={`${collapsed ? "Show" : "Hide"} ${taskTotal} task${
                    taskTotal === 1 ? "" : "s"
                  } in ${f.name}`}
                  title={
                    collapsed
                      ? `Show ${taskTotal} task${taskTotal === 1 ? "" : "s"}`
                      : `Hide ${taskTotal} task${taskTotal === 1 ? "" : "s"}`
                  }
                  onClick={(e) => {
                    e.stopPropagation();
                    toggleFeatureCollapsed(projectId, f.id, isFeatureDone(f));
                  }}
                >
                  {collapsed ? "▸" : "▾"}
                </span>
              </span>
              <span className="name">
                <DepGuide
                  prefix={frow.prefix}
                  inCycle={frow.node.inCycle}
                  extraDeps={frow.node.extraDeps}
                  extraLabel="features"
                />
                {f.name}
              </span>
              <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
              {/* Collapsed features hide their rows, so the count has to
                  come back to the header or the fold loses the one number
                  it was hiding. */}
              {collapsed && taskTotal > 0 && (
                <span className="age">
                  {taskTotal} task{taskTotal === 1 ? "" : "s"}
                </span>
              )}
              {chains.byRoot.has(f.id) && (
                <span
                  className="chain-chip root"
                  title={chainRootTitle(chains.byRoot.get(f.id))}
                >
                  ⛓ chain
                </span>
              )}
              {chains.queuedMembers.has(f.id) && (
                <span className="chain-chip queued" title={CHAIN_QUEUED_TITLE}>
                  ⛓ queued
                </span>
              )}
              {runner ? (
                <span
                  className={`assign-chip ${runner.status !== "online" ? "warn" : ""}`}
                  title="Click to unassign"
                >
                  {runner.status !== "online"
                    ? `⚠ ${runner.runner_id}`
                    : `🖥 ${runner.runner_id}`}
                </span>
              ) : (
                <span
                  className="assign-chip empty"
                  title="Drag onto a runner to assign"
                >
                  · unassigned ·
                </span>
              )}
              <span className="bar">
                <i style={{ width: `${pct}%` }} />
              </span>
              <span className="prog">{pct}%</span>
            </div>
            {rows.map(renderTaskRow)}
          </div>
        );
      })}

      {/* The ungrouped bucket folds like a feature — same control, same
          store map under a sentinel key. It is the one group with no
          lifecycle to derive a default from, so it starts open; it is
          also routinely the LONGEST list on the card, which is what the
          fold is for. */}
      {orphanRows.length > 0 && (
        <div className={`feat${orphanCollapsed ? " collapsed" : ""}`}>
          <div className="feat-head">
            <span className="glyph-slot">
              {/* A real <button> here, unlike the feature carets: this head
                  carries no rowProps, so there is no focusable row to fold
                  from and no role="button" to nest inside. Same choice the
                  archived expander below already makes. */}
              <button
                type="button"
                className="caret"
                aria-expanded={!orphanCollapsed}
                aria-label={`${orphanCollapsed ? "Show" : "Hide"} ${
                  orphanRows.length
                } task${orphanRows.length === 1 ? "" : "s"} with no feature`}
                title={
                  orphanCollapsed
                    ? `Show ${orphanRows.length} task${orphanRows.length === 1 ? "" : "s"}`
                    : `Hide ${orphanRows.length} task${orphanRows.length === 1 ? "" : "s"}`
                }
                onClick={() =>
                  toggleFeatureCollapsed(projectId, NO_FEATURE, false)
                }
              >
                {orphanCollapsed ? "▸" : "▾"}
              </button>
            </span>
            <span className="name" style={{ color: "#6b757e" }}>
              No feature
            </span>
            <span className="age">{orphanRows.length} tasks</span>
          </div>
          {!orphanCollapsed && orphanRows.map(renderTaskRow)}
        </div>
      )}

      {showArchived && (
        <div className="feat done">
          <div className="feat-head">
            <span className="name" style={{ color: "#6b757e" }}>
              Archived
            </span>
            <span className="age">{archivedTasks.length} tasks</span>
          </div>
          {archivedRows.map(renderTaskRow)}
        </div>
      )}

      {/* Same dashed expander as the merged-features fold. Hidden while
          the sidebar filter forces the rows open — a toggle that cannot
          take effect would just lie. */}
      {archivedTasks.length > 0 && !archivedForced && (
        <button
          onClick={() => toggleArchivedExpanded(projectId)}
          style={{
            border: "1px dashed #22272c",
            padding: "5px 8px",
            width: "100%",
            textAlign: "left",
            color: "#6b757e",
            fontSize: 11,
            marginTop: 6,
          }}
        >
          {archivedExpanded ? "▾" : "▸"} {archivedTasks.length} archived task
          {archivedTasks.length === 1 ? "" : "s"}
        </button>
      )}

      {features.length === 0 &&
        orphanRows.length === 0 &&
        archivedTasks.length === 0 && (
          <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
            No tasks yet.
          </div>
        )}

      {overlays}
    </div>
  );
}

/** Stable empty map so the collapse selector never returns a fresh
 *  object for a project nobody has folded anything in. */
const EMPTY_COLLAPSE: Record<string, boolean> = {};

/** Shared empty row list for a collapsed feature. */
const EMPTY_ROWS: DepRow<Task>[] = [];
