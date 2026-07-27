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
import { useModal } from "../../store/modal";
import { RunnerModal } from "./RunnerModal";
import { TaskModal } from "./TaskModal";
import { FeatureModal } from "./FeatureModal";
import { FeatureActionsModal } from "./FeatureActionsModal";
import { AutomationModal } from "./AutomationModal";
import { SettingsModal } from "./SettingsModal";

export function ModalHost(): JSX.Element | null {
  const kind = useModal((s) => s.kind);
  if (kind === null) return null;
  switch (kind) {
    case "runner":
      return <RunnerModal />;
    case "task":
      return <TaskModal />;
    case "feature":
      return <FeatureModal />;
    case "feature-actions":
      return <FeatureActionsModal />;
    case "automation":
      return <AutomationModal />;
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
