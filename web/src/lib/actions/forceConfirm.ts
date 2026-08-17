/**
 * lib/actions/forceConfirm — promise-bridged store behind the force dialog.
 *
 * `withForceRetry` needs an answer to "runner online — force?" from inside
 * a context effect, which is plain async code with no render tree. This
 * store is the bridge: `request(spec)` parks a promise and exposes the spec;
 * `ForceConfirmHost` (mounted once, in ModalHost) renders it as a
 * ConfirmDialog and settles the promise with the user's answer.
 *
 * One request at a time, matching the single-modal rule everywhere else.
 * A second `request` while one is pending auto-declines the first rather
 * than queueing — two stacked force prompts would be a UX bug, not a
 * feature worth supporting.
 */
import { create } from "zustand";

export interface ForceConfirmSpec {
  title: string;
  /** Consequence copy. Callers should include the server's 409 message. */
  body: string;
  /** Label on the confirming button, e.g. "Force delete". */
  confirmLabel: string;
  /**
   * When set, the user must retype this to enable the button. Feature
   * delete sets it to the feature name so the force pass keeps the same
   * friction as the first pass.
   */
  typeToConfirm?: string;
  danger?: boolean;
}

interface ForceConfirmState {
  /** The spec the host should render, or null when idle. */
  pending: ForceConfirmSpec | null;
  /** Internal: resolver for the parked promise. */
  _resolve: ((ok: boolean) => void) | null;

  /** Park a request; resolves true/false when the user answers. */
  request(spec: ForceConfirmSpec): Promise<boolean>;
  /** Answer the pending request. No-op when nothing is pending. */
  settle(ok: boolean): void;
}

export const useForceConfirm = create<ForceConfirmState>((set, get) => ({
  pending: null,
  _resolve: null,

  request: (spec) =>
    new Promise<boolean>((resolve) => {
      // Auto-decline any request already showing — see module docstring.
      get()._resolve?.(false);
      set({ pending: spec, _resolve: resolve });
    }),

  settle: (ok) => {
    const resolve = get()._resolve;
    set({ pending: null, _resolve: null });
    resolve?.(ok);
  },
}));

/**
 * Adapt a spec into the `confirmForce` callback `withForceRetry` expects.
 * The server's 409 message is prepended to the body so the dialog quotes
 * the actual refusal instead of a paraphrase.
 */
export function forceConfirmFor(
  spec: Omit<ForceConfirmSpec, "body"> & { body: string },
): (serverMessage: string) => Promise<boolean> {
  return (serverMessage) =>
    useForceConfirm.getState().request({
      ...spec,
      body: serverMessage ? `${serverMessage}. ${spec.body}` : spec.body,
    });
}
