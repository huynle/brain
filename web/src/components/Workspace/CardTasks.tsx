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
import { useDependentChains } from "../../hooks/useDependentChains";
import { useRowActions } from "../../hooks/useRowActions";
import { useTaskRowRenderer } from "./TaskRow";
import { DepGuide } from "../common/DepGuide";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { useFeatureActionContext } from "../../hooks/useFeatureActionContext";
import { useTaskGroupActionContext } from "../../hooks/useTaskGroupActionContext";
import { useAutoArchive } from "../../hooks/useAutoArchive";
import { useActionRunner } from "../../hooks/useActionRunner";
import { buildFeatureActions } from "../../lib/actions/featureActions";
import { buildSelectionActions } from "../../lib/actions/selectionActions";
import {
  buildTaskGroupActions,
  type TaskGroup,
  type TaskGroupActionContext,
} from "../../lib/actions/taskGroupActions";
import { isRangeKey } from "../../lib/selection";
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

function featStateClass(f: DerivedFeature): string {
  if (f.lifecycle === "blocked") return "block";
  if (f.lifecycle === "merged" || f.lifecycle === "finished") return "done";
  return "busy";
}

/** Fold/verb keys for the groups that are NOT features. None is a legal
 *  feature id, so none can collide with a real one in the per-project
 *  `featureCollapsed` map. Archived buckets are prefixed because the SAME
 *  feature can hold both live and archived tasks, and folding one must not
 *  fold the other. */
export const NO_FEATURE = "__nofeat__";
export const archivedKey = (featureId: string) => `__archived__:${featureId}`;

/**
 * Bucket archived tasks by feature, preserving dependency order inside
 * each bucket.
 *
 * `deriveFeatures` skips archived tasks, so an all-archived feature has
 * no DerivedFeature to hang rows under — but the raw `feature_id`
 * survives archiving, and that is all a header needs. Real features
 * first in id order, the ungrouped bucket last, matching the live list.
 */
export function bucketArchived(archivedTasks: readonly Task[]) {
  const buckets = new Map<string, Task[]>();
  for (const t of archivedTasks) {
    const key = t.feature_id ?? NO_FEATURE;
    const arr = buckets.get(key);
    if (arr) arr.push(t);
    else buckets.set(key, [t]);
  }
  const keys = [...buckets.keys()]
    .filter((k) => k !== NO_FEATURE)
    .sort((a, b) => a.localeCompare(b));
  if (buckets.has(NO_FEATURE)) keys.push(NO_FEATURE);
  return keys.map((key) => {
    const bucket = buckets.get(key)!;
    return {
      key,
      label: key === NO_FEATURE ? "No feature" : key,
      tasks: bucket,
      rows: flattenDepForest(buildTaskForest(bucket)),
    };
  });
}

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

/**
 * A group of tasks that is NOT a feature: the ungrouped bucket, and each
 * bucket inside the archive fold.
 *
 * One component for both, because they are the same object — a header, a
 * fold, and a verb list over an explicit set of tasks. The header answers
 * right-click, long-press and the keyboard through `useRowActions` like
 * every other row on the card; it is NOT draggable and carries no
 * selection checkbox, because there is no feature entity behind it to
 * assign to a runner or to mark.
 */
export function TaskGroupBlock({
  group,
  rows,
  collapsed,
  renderRow,
  rowProps,
  ctx,
  nested = false,
}: {
  group: TaskGroup;
  rows: DepRow<Task>[];
  collapsed: boolean;
  renderRow: (row: DepRow<Task>) => JSX.Element;
  rowProps: ReturnType<typeof useRowActions>["rowProps"];
  ctx: TaskGroupActionContext;
  /** Rendered inside the archive fold — indent it under that header. */
  nested?: boolean;
}): JSX.Element {
  const n = group.tasks.length;
  const actions = buildTaskGroupActions(group, ctx, { collapsed });
  const rp = rowProps(actions, group.label, () => ctx.toggleCollapsed(group));
  const word = `task${n === 1 ? "" : "s"}`;

  return (
    <div
      className={`feat${collapsed ? " collapsed" : ""}`}
      style={nested ? { marginLeft: 12 } : undefined}
    >
      <div
        className="feat-head"
        {...rp}
        onKeyDown={(e) => {
          // Same tree convention as a feature head, same modifier guard:
          // ⌘←/Alt+← belongs to the browser, not to a fold.
          if (
            !e.metaKey &&
            !e.ctrlKey &&
            !e.altKey &&
            (e.key === "ArrowLeft" || e.key === "ArrowRight")
          ) {
            e.preventDefault();
            if (collapsed !== (e.key === "ArrowLeft")) ctx.toggleCollapsed(group);
            return;
          }
          rp.onKeyDown(e);
        }}
        onClick={(e) => {
          if ((e.target as HTMLElement).closest("button, .caret")) return;
          ctx.toggleCollapsed(group);
        }}
      >
        <span className="glyph-slot">
          {/* A real <button>, unlike a feature caret: this header has no
              selection checkbox competing for the slot, so nothing hides
              it, and `.feat-head` here is not itself role="button". */}
          <button
            type="button"
            className="caret"
            aria-expanded={!collapsed}
            aria-label={`${collapsed ? "Show" : "Hide"} ${n} ${word} in ${group.label}`}
            title={collapsed ? `Show ${n} ${word}` : `Hide ${n} ${word}`}
            onClick={(e) => {
              e.stopPropagation();
              ctx.toggleCollapsed(group);
            }}
          >
            {collapsed ? "▸" : "▾"}
          </button>
        </span>
        <span className="name" style={{ color: "#6b757e" }}>
          {group.label}
        </span>
        <span className="age">
          {n} {word}
        </span>
      </div>
      {!collapsed && rows.map(renderRow)}
    </div>
  );
}

export function CardTasks({
  projectId,
  tasks,
  features,
}: CardTasksProps): JSX.Element {
  const openInSidebar = useWorkspace((s) => s.openInSidebar);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  // Whole map for this project, not a per-feature selector: the feature
  // list is built inside a render, so there is no stable place to call one
  // hook per feature. It changes only when a fold is clicked.
  const featureCollapsed = useWorkspace(
    (s) => s.featureCollapsed[projectId] ?? EMPTY_COLLAPSE,
  );
  const toggleFeatureCollapsed = useWorkspace((s) => s.toggleFeatureCollapsed);
  const { runners } = useRunners();
  // A standing "run feature + dependents" request is a queue the server
  // drains on its own. Nothing else on screen would mention the features
  // it enrolled, so the chips below are the only place they exist.
  // The POLL lives in ProjectCard; this is a cache reader.
  const chains = useDependentChains(projectId);

  const featureCtx = useFeatureActionContext(projectId);
  // The groups that answer to no feature_id — the ungrouped bucket and
  // every bucket inside the archive — get their own verb matrix. It is
  // deliberately NOT buildFeatureActions on a synthetic feature: half
  // those verbs put a feature id in a URL or re-derive it in a modal, and
  // would dead-end on an id `deriveFeatures` can never produce.
  const groupCtx = useTaskGroupActionContext(projectId, {
    toggleCollapsed: (g) => toggleFeatureCollapsed(projectId, g.key, false),
  });
  const { rowProps, overlays } = useRowActions();
  const autoArchive = useAutoArchive(projectId);
  // The toggle writes to the server (it creates or deletes an
  // automation), so it runs through the action runner like every other
  // mutating verb — a failure becomes a toast rather than an unhandled
  // rejection and a checkbox that silently springs back.
  const autoArchiveRunner = useActionRunner();

  // Subscribed (not getState) so checkboxes and the "marked" tint react
  // to every toggle, from any surface — checkbox, `v` key, or menu.
  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleFeatureSel = useSelection((s) => s.toggleFeature);
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

  // Archived tasks are NOT here. They have their own tab now: a fold at
  // the bottom of this list meant the rows you were done with shared a
  // scroll region with the rows you are not, which is the opposite of
  // what a fold is for on a project with hundreds of them.
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
  // The bucket's raw tasks, in render order — what its verbs operate on.
  const orphanTasks = useMemo(
    () => orphanRows.map((r) => r.node.item),
    [orphanRows],
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
    return ids;
  }, [featureRows, featureCollapsed, rowsByFeat]);

  // Feature headers range over the headers only, in their render order.
  const orderedFeatureIds = useMemo(
    () => featureRows.map((row) => row.node.item.id),
    [featureRows],
  );

  // One row renderer, shared with the Archived tab. `orderedTaskIds` is
  // this surface's own visible order — see the hook's docstring.
  const renderTaskRow = useTaskRowRenderer({
    projectId,
    orderedTaskIds,
    rowProps,
    selectionActions,
  });


  return (
    <div>
      {/* Auto-archive is a property of the PROJECT, but it belongs on this
          tab because this is the list it keeps short — the checkbox and
          its effect are one glance apart. It creates a real automation on
          the server (visible in the Automations tab), so it keeps working
          with every browser closed. */}
      <label
        className="auto-archive"
        title={
          autoArchive.enabled
            ? "On — a feature's tasks move to Archived as soon as it " +
              "completes. Untick to remove the automation."
            : "Off — completed features stay in this list. Tick to archive " +
              "them automatically as they finish."
        }
      >
        <input
          type="checkbox"
          checked={autoArchive.enabled}
          disabled={autoArchive.isLoading || autoArchiveRunner.busy}
          onChange={() =>
            autoArchiveRunner.run({
              id: "toggle-auto-archive",
              label: "Auto-archive",
              group: "state",
              run: autoArchive.toggle,
            })
          }
        />
        Auto-archive completed features
      </label>
      {autoArchiveRunner.dialog}

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

      {/* Every group that is NOT a feature renders through one component:
          the ungrouped bucket here, and each bucket inside the archive
          fold below. They behave identically because they ARE the same
          thing — a set of tasks with a header, a fold and a verb list. */}
      {orphanRows.length > 0 && (
        <TaskGroupBlock
          group={{
            projectId,
            key: NO_FEATURE,
            label: "No feature",
            tasks: orphanTasks,
          }}
          rows={orphanRows}
          collapsed={orphanCollapsed}
          renderRow={renderTaskRow}
          rowProps={rowProps}
          ctx={groupCtx}
        />
      )}

      {features.length === 0 && orphanRows.length === 0 && (
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
