/**
 * panes-v2 ModalHost.
 *
 * Global modal renderer. Reads `useModal().kind` and mounts the
 * matching modal component. Only one modal can be open at a time
 * (the modal store enforces this — see `store/modal.ts`).
 *
 * Mount this ONCE, near the top of the tree — the modals themselves
 * use `Modal`, which portals into `document.body`, so ModalHost's
 * position in the DOM tree is largely cosmetic.
 *
 * Adding a new modal:
 *   1. Add the kind to `ModalKind` in `store/modal.ts`
 *   2. Add a matching `case` here that renders the component
 *   3. Ensure the component reads its target from `useModal(...)`
 */
import { useModal, type ModalKind } from "../../store/modal";
import { RunnerModal } from "./RunnerModal";
import { TaskModal } from "./TaskModal";
import { TaskActionsModal } from "./TaskActionsModal";
import { FeatureModal } from "./FeatureModal";
import { FeatureActionsModal } from "./FeatureActionsModal";
import { FeatureAssignModal } from "./FeatureAssignModal";
import { AutomationModal } from "./AutomationModal";
import { GoalModal } from "./GoalModal";
import { GoalCreateModal } from "./GoalCreateModal";
import { SettingsModal } from "./SettingsModal";
import { StatusPickerModal } from "./StatusPickerModal";
import { MetadataModal } from "./MetadataModal";
import { ForceConfirmHost } from "./ForceConfirmHost";

export function ModalHost(): JSX.Element | null {
  const kind = useModal((s) => s.kind);
  // The force-confirm dialog is NOT a modal-store citizen: it answers a
  // promise parked by an in-flight effect, and must be able to appear on
  // top of (or without) whatever modal is open. Always mounted.
  return (
    <>
      {renderKind(kind)}
      <ForceConfirmHost />
    </>
  );
}

function renderKind(kind: ModalKind): JSX.Element | null {
  if (kind === null) return null;
  switch (kind) {
    case "runner":
      return <RunnerModal />;
    case "task":
      return <TaskModal />;
    case "task-actions":
      return <TaskActionsModal />;
    case "task-status":
      return <StatusPickerModal mode="task" />;
    case "task-metadata":
      return <MetadataModal mode="task" />;
    case "feature":
      return <FeatureModal />;
    case "feature-actions":
      return <FeatureActionsModal />;
    case "feature-assign":
      return <FeatureAssignModal />;
    case "feature-status":
      return <StatusPickerModal mode="feature" />;
    case "feature-metadata":
      return <MetadataModal mode="feature" />;
    case "automation":
      return <AutomationModal />;
    case "goal":
      return <GoalModal />;
    case "goal-create":
      return <GoalCreateModal />;
    case "settings":
      return <SettingsModal />;
    default: {
      // Exhaustiveness guard — a new kind added to the store but not
      // wired here should be caught by tsc.
      const _exhaustive: never = kind;
      void _exhaustive;
      return null;
    }
  }
}
