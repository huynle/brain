/**
 * ForceConfirmHost — renders the pending force-confirmation, if any.
 *
 * The other half of `lib/actions/forceConfirm`: context effects park a
 * promise in that store when the server 409s a mutation; this host turns
 * the parked spec into a ConfirmDialog and settles the promise with the
 * user's answer. Mounted once, unconditionally, from ModalHost — it must
 * outlive whatever modal or menu triggered the original action, because
 * the 409 arrives after those surfaces may have closed.
 */
import { ConfirmDialog } from "../common/ConfirmDialog";
import { useForceConfirm } from "../../lib/actions/forceConfirm";

export function ForceConfirmHost(): JSX.Element | null {
  const pending = useForceConfirm((s) => s.pending);
  const settle = useForceConfirm((s) => s.settle);

  if (!pending) return null;

  return (
    <ConfirmDialog
      confirm={{
        title: pending.title,
        body: pending.body,
        confirmLabel: pending.confirmLabel,
        typeToConfirm: pending.typeToConfirm,
      }}
      danger={pending.danger ?? true}
      onCancel={() => settle(false)}
      onConfirm={async () => {
        // The forced retry runs back in the original effect after the
        // promise resolves; its outcome surfaces there (toast, or inline
        // in the first dialog). This dialog's only job is the answer.
        settle(true);
      }}
    />
  );
}
