import { submitBulkJob } from "../store/bulkJobs";
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useModal } from "../store/modal";
import { useSelection } from "../store/selection";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import {
  clearFeatureAssignment,
  cancelDependentChain as apiCancelDependentChain,
  resumeFeature as apiResumeFeature,
  runFeature,
  summarizeDependentQueue,
  summarizeRunFeatureResult,
} from "../lib/api";
import {
  summarizeResumeOutcome,
  type FeatureActionContext,
} from "../lib/actions/featureActions";
import { withForceRetry } from "../lib/actions/forceRetry";
import { forceConfirmFor } from "../lib/actions/forceConfirm";
import {
  pauseFeatureDispatch as apiPauseFeatureDispatch,
  resumeFeatureDispatch as apiResumeFeatureDispatch,
} from "../lib/api";
import { useInvalidateRunnerStatus } from "./useRunnerStatus";
import { forceDispatchNote, withForceNote } from "../lib/pause";
import { usePauseState } from "./usePauseState";
import type { DerivedFeature } from "../lib/features";
import type { DependentChain } from "../lib/api";
import { dependentChainsKey } from "./useDependentChains";
import { type TaskStatus } from "../lib/types";
import type { OpencodeInstance, SessionRef, Task } from "../lib/types";
import { useLive } from "../lib/sse";
import { liveSessionRef } from "../lib/sessionRef";

/** Stable empty list, so a project with no live tasks never churns. */
const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

/**
 * How many session panes "Watch tasks in Focus" will open at once.
 * Past four, each transcript is narrower than the code it is printing.
 */
const MAX_WATCH_PANES = 4;

/**
 * Factory form, for callers that span several projects at once (the
 * command palette). A hook cannot be called per project inside a loop, so
 * the project id becomes an argument instead.
 */
export function useFeatureActionContextFactory(): (
  projectId: string,
) => FeatureActionContext {
  const openModal = useModal((s) => s.open);
  const closeModal = useModal((s) => s.close);
  const openInSidebar = useWorkspace((s) => s.openInSidebar);
  const openInFocusGroup = useWorkspace((s) => s.openInFocusGroup);
  const assignFeatureLocal = useWorkspace((s) => s.assignFeature);
  const unassignFeatureLocal = useWorkspace((s) => s.unassignFeature);
  const toast = useUI((s) => s.toast);
  const invalidateStatus = useInvalidateRunnerStatus();
  // Pause dials — "Run feature now" bypasses the project ones by design,
  // and cannot bypass runner pause at all. Both are worth saying out loud.
  const { pause } = usePauseState();
  const queryClient = useQueryClient();

  return useMemo(
    () => (projectId: string) => {
      // Sync cache reads rather than a hook: this factory is called per
      // project inside a loop (the command palette), where hooks cannot go.
      // Mirrors how assignedRunner reads useWorkspace.getState().
      const activeChainRoots = (): Set<string> => {
        const data = queryClient.getQueryData<{ chains: DependentChain[] }>(
          dependentChainsKey(projectId),
        );
        return new Set((data?.chains ?? []).map((c) => c.rootFeatureId));
      };
      const refreshChains = () =>
        queryClient.invalidateQueries({
          queryKey: dependentChainsKey(projectId),
        });

      return {
        // getState() (not a hook subscription): builders run on every row
        // render, and the row components subscribe to the selection store
        // themselves — the label stays fresh without a stale closure.
        toggleSelect: (feature: DerivedFeature) =>
          useSelection.getState().toggleFeature(projectId, feature.id),
        isSelected: (feature: DerivedFeature) => {
          const s = useSelection.getState();
          return s.projectId === projectId && s.featureIds.has(feature.id);
        },

        hasActiveChain: (feature: DerivedFeature) =>
          activeChainRoots().has(feature.id),

        runFeatureWithDependents: async (feature: DerivedFeature) => {
          const r = await runFeature(projectId, feature.id, false, true);
          const { message, kind } = summarizeRunFeatureResult(r);
          // Three things the user needs from one line: what dispatched,
          // what got queued behind it, and that the pause was crossed on
          // purpose. Dropping the middle one is how a chain becomes
          // invisible the moment the toast fades.
          const chain = summarizeDependentQueue(r.dependents);
          const note = forceDispatchNote(pause, { projectId });
          toast(
            withForceNote(chain ? `${message} · ${chain}` : message, note),
            kind,
          );
          // Seed the cache, do not merely invalidate.
          //
          // invalidateQueries does not create a query that has no observer,
          // so on a surface that has not polled chains the entry stays
          // undefined and hasActiveChain reads "no chain" — leaving the user
          // who just started one with no way to cancel it. Writing the row we
          // just learned about makes the cancel verb available immediately,
          // everywhere, and the next poll reconciles it.
          if (r.dependents) {
            queryClient.setQueryData<{ chains: DependentChain[] }>(
              dependentChainsKey(projectId),
              (prev) => {
                const others = (prev?.chains ?? []).filter(
                  (c) => c.rootFeatureId !== feature.id,
                );
                return {
                  chains: [
                    ...others,
                    {
                      projectId,
                      rootFeatureId: feature.id,
                      requestedAt: Date.now(),
                      pausedAtRequest: false,
                      queued: r.dependents!.queued ?? [],
                      skipped: r.dependents!.skipped,
                      waitsOnExternal: r.dependents!.waitsOnExternal,
                      truncated: r.dependents!.truncated,
                    },
                  ],
                };
              },
            );
          }
          void refreshChains();
        },

        cancelDependentChain: async (feature: DerivedFeature) => {
          const r = await apiCancelDependentChain(projectId, feature.id);
          // The server distinguishes "stopped a chain" from "there was
          // nothing to stop"; reporting both as success is how a user
          // concludes cancel is broken.
          toast(r.detail, r.cancelled ? "success" : "info");
          void refreshChains();
        },

        // The FEATURE dial. Reversible and status-free, unlike cancel: it

        // holds this feature's tasks out of automatic dispatch and leaves

        // the rest of the project running. The dials do not ride SSE, so

        // both effects invalidate runner-status after writing.

        pauseFeatureDispatch: async (feature: DerivedFeature) => {
          await apiPauseFeatureDispatch(projectId, feature.id);

          invalidateStatus();
          toast(
            `${feature.name} paused — no new dispatch for this feature. ` +
              `Work already running finishes.`,
            "success",
          );
        },

        resumeFeatureDispatch: async (feature: DerivedFeature) => {
          await apiResumeFeatureDispatch(projectId, feature.id);
          invalidateStatus();
          toast(`${feature.name} resumed`, "success");
        },

        runFeature: async (feature: DerivedFeature) => {
          const r = await withForceRetry(
            (force) => runFeature(projectId, feature.id, force),
            forceConfirmFor({
              title: "Runner conflict — force dispatch?",
              body: "Force pushes the dispatch even though a runner already holds a lease on part of this feature.",
              confirmLabel: "Force dispatch",
              danger: true,
            }),
          );
          const { message, kind } = summarizeRunFeatureResult(r);
          const note = forceDispatchNote(pause, { projectId });
          toast(withForceNote(message, note), kind);
        },

        resumeFeature: async (feature: DerivedFeature) => {
          const result = await withForceRetry(
            (force) =>
              apiResumeFeature(
                projectId,
                feature.id,
                force ? { force: true } : undefined,
              ),
            forceConfirmFor({
              title: "Force resume the whole feature?",
              body:
                "Force re-opens terminal tasks and stamps a resume request on " +
                "every remaining task in the feature. It does NOT bypass the " +
                "live-claim safety — tasks claimed by an online runner stay refused.",
              confirmLabel: "Force resume",
              danger: true,
            }),
            // Batch resume reports refusals in-band (200 + per-task skip
            // reasons), never as a 409. Escalate only for skips a forced
            // retry can actually change: the batch terminal exclusion and
            // the not-abandoned gate. Live-claim refusals are force-proof.
            (res) => {
              const recoverable = res.results.filter(
                (row) =>
                  !row.resumed &&
                  ((row.reason ?? "").startsWith(
                    "terminal_status_excluded_from_batch",
                  ) ||
                    (row.reason ?? "").startsWith("task is not abandoned")),
              ).length;
              return recoverable > 0
                ? `${recoverable} task(s) were skipped (terminal or not abandoned). Force can resume them.`
                : null;
            },
          );
          const { message, kind } = summarizeResumeOutcome(result);
          toast(`${feature.name}: ${message}`, kind);
        },

        setStatusForAll: async (feature: DerivedFeature, status: TaskStatus) => {
          await submitBulkJob({ operation: "set_status", status, filters: [{ project: projectId, type: "task", feature_id: feature.id }] });
        },
        deleteFeature: async (feature: DerivedFeature) => {
          await submitBulkJob({ operation: "delete", filters: [{ project: projectId, type: "task", feature_id: feature.id }] });
          closeModal();
        },

        clearRunnerAssignment: async (feature: DerivedFeature) => {
          const previous =
            useWorkspace.getState().featureAssignments[feature.id];
          // Optimistic clear; the server's runners_update SSE reconciles,
          // and a failure rolls the mirror back.
          unassignFeatureLocal(feature.id);
          try {
            await clearFeatureAssignment(projectId, feature.id);
            toast(`Cleared runner assignment for ${feature.name}`, "success");
          } catch (err) {
            if (previous) assignFeatureLocal(feature.id, previous);
            throw err;
          }
        },

        assignedRunner: (feature: DerivedFeature) =>
          useWorkspace.getState().featureAssignments[feature.id],

        openAssignRunner: (feature: DerivedFeature) =>
          openModal("feature-assign", { projectId, featureId: feature.id }),
        openGoalCreate: (feature: DerivedFeature) =>
          openModal("goal-create", {
            project: projectId,
            featureId: feature.id,
          }),
        openStatusPicker: (feature: DerivedFeature) =>
          openModal("feature-status", { projectId, featureId: feature.id }),
        openMetadata: (feature: DerivedFeature) =>
          openModal("feature-metadata", { projectId, featureId: feature.id }),
        openCheckout: (feature: DerivedFeature) =>
          openModal("feature-actions", { projectId, featureId: feature.id }),
        openResume: (feature: DerivedFeature) =>
          openModal("feature-actions", { projectId, featureId: feature.id }),
        openDetails: (feature: DerivedFeature) =>
          openModal("feature", { projectId, featureId: feature.id }),

        /*
         * One session pane per running task, side by side.
         *
         * Reads both caches synchronously rather than through hooks —
         * this factory is called per project inside a loop (the command
         * palette), where hooks cannot go. Same pattern as
         * activeChainRoots above.
         *
         * "Active" on the feature is a task-status rollup; being
         * addressable is a stronger condition (the runner must have
         * discovered an instance). So the count returned is what was
         * actually opened, and the toast reports the gap instead of
         * quietly showing fewer panes than there are running tasks.
         */
        watchInFocus: (feature: DerivedFeature): number => {
          const instances =
            queryClient.getQueryData<OpencodeInstance[]>(["v2", "sessions"]) ??
            [];
          const tasks =
            useLive.getState().projects[projectId]?.tasks ?? EMPTY_TASKS;

          const running = tasks.filter(
            (t) => t.feature_id === feature.id && t.status === "in_progress",
          );
          const addressable = running
            .map((t) => ({
              task: t,
              ref: liveSessionRef({ id: t.id, projectId }, instances),
            }))
            .filter((x): x is { task: Task; ref: SessionRef } => !!x.ref);

          if (addressable.length === 0) {
            toast(
              running.length === 0
                ? "No tasks are running in this feature right now"
                : `${running.length} task${running.length === 1 ? " is" : "s are"} in progress but no session is addressable yet — the runner may still be starting them`,
              "warning",
            );
            return 0;
          }

          // Past four panes each transcript is too narrow to read, which
          // defeats the point of watching them. Cap, and say so — a
          // silent truncation would read as "this is all that's
          // running".
          const shown = addressable.slice(0, MAX_WATCH_PANES);
          closeModal();
          openInFocusGroup(
            shown.map(({ task, ref }) => ({
              kind: "session" as const,
              target: { ref },
              title: task.title || task.id,
            })),
          );
          if (addressable.length > shown.length) {
            toast(
              `Watching ${shown.length} of ${addressable.length} running tasks — open the rest from the sidebar`,
              "info",
            );
          }
          return shown.length;
        },
        openPlan: (feature: DerivedFeature) =>
          openInSidebar(
            "feature-detail",
            { projectId, featureId: feature.id },
            feature.name,
          ),
        // The row's primary Open verb: the sidebar dock. Same target as
        // openPlan, named plainly ("Open" rather than "Open plan drawer")
        // so the top-of-menu verb reads as the row's default action.
        openDrawer: (feature: DerivedFeature) =>
          openInSidebar(
            "feature-detail",
            { projectId, featureId: feature.id },
            feature.name,
          ),
      };
    },
    [
      openModal,
      closeModal,
      openInSidebar,
      openInFocusGroup,
      assignFeatureLocal,
      unassignFeatureLocal,
      toast,
      pause,
    ],
  );
}

export function useFeatureActionContext(
  projectId: string,
): FeatureActionContext {
  const factory = useFeatureActionContextFactory();
  return useMemo(() => factory(projectId), [factory, projectId]);
}
