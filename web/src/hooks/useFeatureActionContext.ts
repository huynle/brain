/**
 * useFeatureActionContext — binds the pure feature-action builders to real
 * effects.
 *
 * The two fan-out verbs (set-status-for-all, delete) both **preview before
 * they commit**. The bulk endpoints accept `dry_run`, and the preview is
 * what catches the case the server used to swallow: a filter matching more
 * than the 100-entry cap, where a live run would mutate the first 100 and
 * report success. Rather than half-apply, we refuse and tell the user.
 */
import { useMemo } from "react";

import { useModal } from "../store/modal";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import {
  deleteFeatureTasks,
  runFeature,
  setFeatureStatus,
  summarizeRunFeatureResult,
} from "../lib/api";
import {
  summarizeBulkResult,
  type FeatureActionContext,
} from "../lib/actions/featureActions";
import { STATUS_LABELS } from "../lib/actions/taskActions";
import type { DerivedFeature } from "../lib/features";
import type { TaskStatus } from "../lib/types";

/**
 * Error thrown when a dry run reports the safety cap would truncate the
 * operation. Deliberately a hard stop rather than a warning: partially
 * applying a feature-wide change is the outcome this whole preview exists
 * to prevent.
 */
export class TruncatedOperationError extends Error {
  constructor(matched: number, cap: number, verb: string) {
    super(
      `This feature has ${matched} tasks — more than the ${cap}-task limit for a single ${verb}. ` +
        `Narrow the operation or ${verb} in batches.`,
    );
    this.name = "TruncatedOperationError";
  }
}

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
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const toast = useUI((s) => s.toast);

  return useMemo(
    () => (projectId: string) => ({
      runFeature: async (feature: DerivedFeature) => {
        const r = await runFeature(projectId, feature.id, false);
        const { message, kind } = summarizeRunFeatureResult(r);
        toast(message, kind);
      },

      setStatusForAll: async (
        feature: DerivedFeature,
        status: TaskStatus,
      ) => {
        const preview = await setFeatureStatus(projectId, feature.id, status, {
          dryRun: true,
        });
        if (preview.truncated) {
          throw new TruncatedOperationError(
            preview.matched_total ?? preview.total,
            preview.total,
            "update",
          );
        }
        if (preview.total === 0) {
          toast("No tasks matched — nothing changed", "warning");
          return;
        }

        const result = await setFeatureStatus(projectId, feature.id, status);
        const { message, kind } = summarizeBulkResult({
          ok: result.updated,
          failed: result.failed,
          total: result.total,
          truncated: result.truncated,
          matchedTotal: result.matched_total,
        });
        toast(
          `${feature.name} → ${STATUS_LABELS[status] ?? status}: ${message}`,
          kind,
        );
      },

      deleteFeature: async (feature: DerivedFeature) => {
        const preview = await deleteFeatureTasks(projectId, feature.id, {
          dryRun: true,
        });
        if (preview.truncated) {
          throw new TruncatedOperationError(
            preview.matched_total ?? preview.total,
            preview.total,
            "delete",
          );
        }
        if (preview.total === 0) {
          toast("No tasks matched — nothing deleted", "warning");
          return;
        }

        const result = await deleteFeatureTasks(projectId, feature.id);
        closeModal();

        // Partial failure has to be legible. A bare "deleted" after 2 of 9
        // failed would leave orphan tasks with no signal.
        const { message, kind } = summarizeBulkResult({
          ok: result.deleted,
          failed: result.failed,
          total: result.total,
        });
        const failedTitles = result.results
          .filter((r) => r.status !== "ok")
          .map((r) => r.title || r.id)
          .slice(0, 3);
        toast(
          failedTitles.length > 0
            ? `${feature.name}: ${message} (${failedTitles.join(", ")})`
            : `Deleted ${feature.name}: ${message}`,
          kind,
        );
      },

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
      openPlan: (feature: DerivedFeature) =>
        openFeatureDrawer(projectId, feature.id),
    }),
    [openModal, closeModal, openFeatureDrawer, toast],
  );
}

export function useFeatureActionContext(
  projectId: string,
): FeatureActionContext {
  const factory = useFeatureActionContextFactory();
  return useMemo(() => factory(projectId), [factory, projectId]);
}
