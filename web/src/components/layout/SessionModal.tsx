// In-place session inspection: a modal wrapping TaskSessionPane so Enter on
// a task (Tasks list, Automations run-tasks) reviews the work — live stream
// or recorded transcript — WITHOUT leaving the current tab. Jumping to the
// Runners/Control tab (with resume and a composer) stays available via the
// explicit o key / review links.

import { Modal } from "../common/Modal";
import { TaskSessionPane } from "./TaskSessionPane";

export interface SessionModalTarget {
  taskId: string;
  projectId?: string;
  taskPath?: string;
  title?: string;
}

export function SessionModal({ target, onClose }: { target: SessionModalTarget; onClose: () => void }) {
  return (
    <Modal
      title={`Session — ${target.title || target.taskId}`}
      onClose={onClose}
      className="sheet-wide session-modal"
      storageKey="session-inspect"
    >
      <TaskSessionPane
        taskId={target.taskId}
        projectId={target.projectId}
        taskPath={target.taskPath}
      />
    </Modal>
  );
}
