/**
 * useActionRunner — the one place an ActionDescriptor is actually invoked.
 *
 * Every surface (context menu, action sheet, modal footer, keyboard) calls
 * `run(action)` and renders `dialog`. That keeps three rules in one place
 * instead of four:
 *
 *   1. A disabled action never runs, whatever route reached it.
 *   2. An action carrying `confirm` always goes through ConfirmDialog.
 *   3. A throwing action surfaces as an error toast — so builders can let
 *      API errors propagate rather than each writing its own try/catch.
 */
import { useCallback, useState } from "react";

import { ConfirmDialog } from "../components/common/ConfirmDialog";
import { useUI } from "../store/ui";
import { isEnabled, type ActionDescriptor } from "../lib/actions/types";

export interface ActionRunner {
  /** Invoke an action, routing through confirmation when it asks for it. */
  run: (action: ActionDescriptor) => void;
  /** Render this somewhere in the tree — the confirm dialog lives here. */
  dialog: JSX.Element | null;
  /** True while a confirmed action is in flight. */
  busy: boolean;
}

export function useActionRunner(): ActionRunner {
  const toast = useUI((s) => s.toast);
  const [pending, setPending] = useState<ActionDescriptor | null>(null);
  const [busy, setBusy] = useState(false);

  const execute = useCallback(
    async (action: ActionDescriptor) => {
      setBusy(true);
      try {
        await action.run();
      } finally {
        setBusy(false);
      }
    },
    [],
  );

  const run = useCallback(
    (action: ActionDescriptor) => {
      // A disabled action must be inert on every route in. Renderers also
      // disable their controls, but the keyboard path can reach an action
      // without going through a rendered control at all.
      if (!isEnabled(action)) {
        if (action.disabledReason) toast(action.disabledReason, "warning");
        return;
      }

      if (action.confirm) {
        setPending(action);
        return;
      }

      void execute(action).catch((err) => {
        toast(
          `${action.label} failed: ${err instanceof Error ? err.message : String(err)}`,
          "error",
        );
      });
    },
    [execute, toast],
  );

  const dialog = pending ? (
    <ConfirmDialog
      confirm={pending.confirm!}
      danger={pending.danger}
      onCancel={() => setPending(null)}
      onConfirm={async () => {
        // Errors propagate to ConfirmDialog, which shows them inline and
        // keeps itself open — a failed destructive action should not
        // vanish behind a toast the user may miss.
        await execute(pending);
        setPending(null);
      }}
    />
  ) : null;

  return { run, dialog, busy };
}
