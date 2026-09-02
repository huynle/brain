/**
 * CardArchived — the project card's Archived tab.
 *
 * Archived work used to be a fold at the bottom of the Tasks tab, which
 * put the rows you are finished with in the same scroll region as the
 * rows you are not. On a project with hundreds of them that is exactly
 * backwards: the fold was there to get them out of the way, and it could
 * not, because it still had to live inside the list.
 *
 * Everything here is the SAME machinery the Tasks tab uses — the group
 * blocks, their verb menus, and `useTaskRowRenderer` — so a right-click
 * on an archived row offers what a right-click on a live row offers, and
 * cannot drift from it. The two differences are deliberate:
 *
 *   - Groups are keyed off the RAW `feature_id`. `deriveFeatures` skips
 *     archived tasks, so an all-archived feature has no DerivedFeature
 *     and no lifecycle; the header is a name and a count, not a badge.
 *   - `orderedTaskIds` is rebuilt from THIS tab's visible rows. A
 *     shift-click range walks it, and handing in the Tasks tab's list
 *     would let a range reach rows nobody can see here.
 *
 * The bulk delete is scoped by FILTER, not by the rows on screen —
 * `{project, status: "archived", type: "task"}` — so "delete all
 * archived" means all of them, not all the ones this snapshot happens to
 * carry. It runs through the same action runner as every other verb, so
 * it inherits the confirm dialog, the disabled-with-reason rule and the
 * error toast.
 */
import { useMemo } from "react";

import { useUI } from "../../store/ui";
import { useSelection } from "../../store/selection";
import { useWorkspace } from "../../store/workspace";
import { useRowActions } from "../../hooks/useRowActions";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useTaskGroupActionContext } from "../../hooks/useTaskGroupActionContext";
import { buildSelectionActions } from "../../lib/actions/selectionActions";
import { runBulkBaton, summarizeBatonOutcome } from "../../lib/actions/bulkBaton";
import { forceConfirmFor } from "../../lib/actions/forceConfirm";
import { withForceRetry } from "../../lib/actions/forceRetry";
import { deleteArchivedTasks } from "../../lib/api";
import type { ActionDescriptor } from "../../lib/actions/types";
import type { Task } from "../../lib/types";
import { archivedKey, bucketArchived } from "../../lib/taskGroups";
import { TaskGroupBlock } from "./TaskGroupBlock";
import { useTaskRowRenderer } from "./TaskRow";

export interface CardArchivedProps {
  projectId: string;
  /** The project's full task list — the same prop CardTasks receives.
   *  The SSE snapshot is unfiltered, so every archived task is already
   *  here and this tab needs no fetch of its own. */
  tasks: readonly Task[];
}

export function CardArchived({
  projectId,
  tasks,
}: CardArchivedProps): JSX.Element {
  const toast = useUI((s) => s.toast);
  const featureCollapsed = useWorkspace(
    (s) => s.featureCollapsed[projectId] ?? EMPTY_COLLAPSE,
  );
  const toggleFeatureCollapsed = useWorkspace((s) => s.toggleFeatureCollapsed);
  const { rowProps, overlays } = useRowActions();
  const runner = useActionRunner();

  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const requestVerb = useSelection((s) => s.requestVerb);
  const clearSel = useSelection((s) => s.clear);
  const selScoped = selProjectId === projectId;
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

  const archivedTasks = useMemo(
    () => tasks.filter((t) => t.status === "archived"),
    [tasks],
  );
  const groups = useMemo(() => bucketArchived(archivedTasks), [archivedTasks]);

  const groupCtx = useTaskGroupActionContext(projectId, {
    toggleCollapsed: (g) => toggleFeatureCollapsed(projectId, g.key, false),
  });

  // Mirrors the render exactly: groups in order, skipping folded ones.
  const orderedTaskIds = useMemo(() => {
    const ids: string[] = [];
    for (const g of groups) {
      if (featureCollapsed[archivedKey(g.key)] ?? false) continue;
      for (const row of g.rows) ids.push(row.node.item.id);
    }
    return ids;
  }, [groups, featureCollapsed]);

  const renderTaskRow = useTaskRowRenderer({
    projectId,
    orderedTaskIds,
    rowProps,
    selectionActions,
  });

  const n = archivedTasks.length;

  // A descriptor rather than a bare onClick, so the button obeys the same
  // three rules every other verb does: a disabled verb never runs, a
  // verb with `confirm` always asks, and a throw becomes an error toast.
  const purge: ActionDescriptor = {
    id: "delete-archived",
    label: `Delete all archived (${n})`,
    group: "danger",
    danger: true,
    disabledReason: n === 0 ? "Nothing is archived" : "",
    confirm: {
      title: `Delete every archived task in ${projectId}?`,
      body:
        `This permanently removes the project's archived tasks and their ` +
        `history. It cannot be undone. It is scoped by filter, not by what ` +
        `is on screen, so it clears every archived task in ${projectId} — ` +
        `including any this view has not loaded.`,
      typeToConfirm: projectId,
      confirmLabel: "Delete permanently",
    },
    run: async () => {
      // The server caps a bulk delete at 100 per call. Deletes make
      // progress with a bare filter — a deleted entry cannot match
      // again — so the plain baton drains it without the per-status
      // dance a bulk UPDATE needs.
      const outcome = await withForceRetry(
        (force) =>
          runBulkBaton(
            () => deleteArchivedTasks(projectId, { force }),
            (r) => r.deleted,
          ),
        forceConfirmFor({
          title: "Runner online — force delete?",
          body:
            "A runner reports it is executing one of these. Force deletes " +
            "them anyway; its in-flight work will have nowhere to land.",
          confirmLabel: "Force delete",
          danger: true,
          typeToConfirm: projectId,
        }),
      );
      const { message, kind } = summarizeBatonOutcome(outcome, "deleted");
      toast(`${projectId} archive: ${message}`, kind);
    },
  };

  if (n === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
        Nothing archived yet.
      </div>
    );
  }

  return (
    <div>
      {groups.map((g) => (
        <TaskGroupBlock
          key={g.key}
          group={{
            projectId,
            key: archivedKey(g.key),
            label: g.label,
            tasks: g.tasks,
          }}
          rows={g.rows}
          collapsed={featureCollapsed[archivedKey(g.key)] ?? false}
          renderRow={renderTaskRow}
          rowProps={rowProps}
          ctx={groupCtx}
        />
      ))}

      <button
        className="id"
        style={{ marginTop: 8, padding: "2px 8px", fontSize: 10 }}
        onClick={() => runner.run(purge)}
        disabled={runner.busy}
      >
        Delete all archived ({n})
      </button>

      {overlays}
      {runner.dialog}
    </div>
  );
}

const EMPTY_COLLAPSE: Record<string, boolean> = {};
