/**
 * AutomationDetailLeaf — one automation, docked.
 *
 * This is the automation row's activation target: clicking (or Enter,
 * or double-click) an automation opens it HERE, in the side panel,
 * instead of the modal it used to open. See AutomationDetail for why
 * the surface changed.
 *
 * Drill-downs out of the run list go to FOCUS, not to this dock: the
 * whole point of docking the automation is that it stays visible while
 * you read what one of its runs produced. Opening the transcript into
 * this same panel would replace the thing you are driving.
 *
 * target shape: { projectId: string; automationId: string }
 */
import { useWorkspace } from "../../../store/workspace";
import { AutomationDetail } from "../../Automations/AutomationDetail";

export function AutomationDetailLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const projectId = (target.projectId as string | undefined) ?? "";
  const automationId = (target.automationId as string | undefined) ?? "";
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const openSessionRef = useWorkspace((s) => s.openSessionRef);

  if (!projectId || !automationId) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        No automation selected — open one from a project card's Automations tab.
      </div>
    );
  }

  return (
    <AutomationDetail
      projectId={projectId}
      automationId={automationId}
      onOpenSession={(_task, ref) => openSessionRef(ref)}
      onOpenLog={(task) =>
        openInFocus(
          "logs",
          { projectId, taskId: task.id },
          `Logs ${task.id.slice(0, 8)}`,
        )
      }
      onOpenTask={(task) =>
        openInFocus(
          "task-detail",
          { projectId, taskId: task.id },
          task.title || task.id,
        )
      }
      onOpenRunsPane={() =>
        openInFocus(
          "automation-runs",
          { projectId, automationId },
          `${projectId} runs`,
        )
      }
    />
  );
}
