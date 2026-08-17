/**
 * lib/actions/forceRetry — the 409 live-claim recovery path.
 *
 * The server refuses (409) to delete, resume or bulk-mutate a task that an
 * online runner is actively executing. Before this helper, that 409 was a
 * dead end: the error toast said "runner online" and the UI offered no way
 * to say "I mean it". Every mutating context effect now runs through
 * `withForceRetry`, which turns the 409 into a second confirmation and — if
 * the user accepts — one retry with `force: true`.
 *
 * Pure by the same rule as the action builders: no react, no store, no
 * fetch. The caller supplies both the attempt (a function of `force`) and
 * the "ask the user" step, so the whole recovery ladder is testable with
 * two stubs. The UI half lives in `forceConfirm.ts` + `ForceConfirmHost`.
 */
import { ApiError } from "../api";

/** True when an error is the server's live-claim refusal (HTTP 409). */
export function isLiveClaimConflict(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status === 409;
}

/**
 * Thrown when the user declines the force confirmation. Callers that route
 * errors to toasts should treat this as a cancellation, not a failure —
 * `useActionRunner` recognises it by name and stays quiet.
 */
export class ForceDeclinedError extends Error {
  constructor(serverMessage: string) {
    super(`Cancelled — ${serverMessage}`);
    this.name = "ForceDeclinedError";
  }
}

/**
 * Run `attempt(false)`; on a 409 live-claim conflict ask the user via
 * `confirmForce` (which receives the server's message so the dialog can
 * quote the actual refusal) and retry once with `attempt(true)`.
 *
 * Non-409 errors pass through untouched — this helper only knows about the
 * one recoverable conflict. A 409 on the forced retry also propagates:
 * some gates (resume's live-claim safety) are deliberately force-proof,
 * and pretending otherwise would loop forever.
 *
 * Some endpoints report the recoverable conflict IN-BAND instead of via
 * 409: /run answers 200 with `reasonCode: "already_leased"` and /resume
 * answers 200 with per-task skip reasons. `needsForce` covers those — it
 * inspects the successful result and returns the refusal message when a
 * forced retry could change the outcome (null otherwise). Declining an
 * in-band escalation returns the ORIGINAL result rather than throwing:
 * the server did complete the request, and the caller's summary toast
 * should describe what actually happened.
 */
export async function withForceRetry<T>(
  attempt: (force: boolean) => Promise<T>,
  confirmForce: (serverMessage: string) => Promise<boolean>,
  needsForce?: (result: T) => string | null,
): Promise<T> {
  let first: T;
  try {
    first = await attempt(false);
  } catch (err) {
    if (!isLiveClaimConflict(err)) throw err;
    const ok = await confirmForce(err.message);
    if (!ok) throw new ForceDeclinedError(err.message);
    return attempt(true);
  }
  const inBand = needsForce?.(first) ?? null;
  if (inBand === null) return first;
  const ok = await confirmForce(inBand);
  if (!ok) return first;
  return attempt(true);
}
