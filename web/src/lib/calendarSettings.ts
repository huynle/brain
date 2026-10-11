/**
 * Logic behind the Settings → Calendars section.
 *
 * Client-side checks mirror the server rules (name pattern, https-only
 * addresses, poll minimum) so typos are caught before a request. The server
 * stays the authority: its 400/409/422 answers are shown as-is.
 *
 * Secret handling: the address is passed in once, sent once, and the caller's
 * `clear` runs on every submit attempt (success, failure or local rejection).
 * Nothing returned from these flows contains the address, and no validation
 * message echoes it.
 */
import { ApiError } from "./api";
import type {
  CalendarReference,
  CalendarRemoval,
  CalendarStatus,
  CreateCalendarInput,
  UpdateCalendarInput,
} from "./calendars";

/** Same rule as the server's config and runtime source names. */
const NAME_PATTERN = /^[a-z0-9][a-z0-9_-]{0,63}$/;
/** Longest accepted address, in characters. */
export const ADDRESS_MAX_LENGTH = 2048;
/** Minimum poll interval, in milliseconds. */
const POLL_MIN_MS = 60_000;
export const ADMIN_ONLY_MESSAGE = "Only admins can manage calendars";
const GENERIC_MESSAGE = "Something went wrong. Try again.";

const UNIT_MS: Record<string, number> = { ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
const DURATION_PATTERN = /^(\d+(?:\.\d+)?(?:ms|h|m|s))+$/;
const DURATION_PART = /(\d+(?:\.\d+)?)(ms|h|m|s)/g;

/** Total milliseconds of a Go-style duration such as "5m" or "1h30m", or null. */
function durationMs(value: string): number | null {
  if (!DURATION_PATTERN.test(value)) return null;
  let total = 0;
  for (const [, amount, unit] of value.matchAll(DURATION_PART)) {
    total += Number(amount) * UNIT_MS[unit];
  }
  return total;
}

/** Null when the name is valid. */
export function validateCalendarName(name: string): string | null {
  return NAME_PATTERN.test(name)
    ? null
    : "Use 1–64 lowercase letters, digits, '-' or '_', starting with a letter or digit.";
}

/** Null when the poll interval is empty (server default) or a valid duration of at least 1m. */
export function validatePollInterval(poll: string): string | null {
  const value = poll.trim();
  if (value === "") return null;
  const ms = durationMs(value);
  if (ms === null) return "Use a duration such as 5m or 1h30m.";
  if (ms < POLL_MIN_MS) return "The poll interval must be at least 1m.";
  return null;
}

/**
 * Null when the address looks acceptable. Messages are generic on purpose:
 * they never repeat the address back.
 */
export function validateCalendarAddress(url: string): string | null {
  const value = url.trim();
  if (value === "") return "Paste the secret address in iCal format.";
  if (value.length > ADDRESS_MAX_LENGTH) {
    return `The address is longer than ${ADDRESS_MAX_LENGTH} characters.`;
  }
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return "The address is not a valid URL.";
  }
  if (parsed.protocol !== "https:") return "The address must start with https://.";
  if (parsed.username || parsed.password) {
    return "The address must not contain a user name or password.";
  }
  return null;
}

export type CalendarTone = "ok" | "warn" | "error" | "muted";

export interface CalendarStatusView {
  tone: CalendarTone;
  label: string;
  /** The server's last error message, shown only for error status. */
  detail?: string;
}

/** How a source's health reads. Built-in sources have no fetch state. */
export function calendarStatusView(calendar: CalendarStatus): CalendarStatusView {
  if (calendar.kind !== "ics") return { tone: "muted", label: "Built in" };
  if (calendar.last_error) {
    return { tone: "error", label: "Error", detail: calendar.last_error };
  }
  if (!calendar.last_fetch) return { tone: "muted", label: "Never fetched" };
  if (calendar.stale) return { tone: "warn", label: "Stale" };
  return { tone: "ok", label: "Healthy" };
}

/** Only runtime sources can be changed or removed through the API. */
export function isRuntimeCalendar(calendar: CalendarStatus): boolean {
  return calendar.origin === "runtime";
}

/** A user-facing message for any thrown value. */
export function errorMessageFor(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 403) return ADMIN_ONLY_MESSAGE;
    return err.message || GENERIC_MESSAGE;
  }
  if (err instanceof Error) return err.message || GENERIC_MESSAGE;
  return GENERIC_MESSAGE;
}

/** The automations that block a removal, read from a 409 body. Empty otherwise. */
export function referencesFrom(err: unknown): CalendarReference[] {
  if (!(err instanceof ApiError) || err.status !== 409) return [];
  try {
    const parsed = JSON.parse(err.body) as { references?: unknown };
    return Array.isArray(parsed.references)
      ? (parsed.references as CalendarReference[])
      : [];
  } catch {
    return [];
  }
}

export type RemoveOutcome =
  | { kind: "removed" }
  | { kind: "references"; references: CalendarReference[] }
  | { kind: "error"; message: string };

export interface RemoveDeps {
  remove: (name: string, opts?: { force?: boolean }) => Promise<CalendarRemoval>;
}

/**
 * Removes a runtime source. A 409 that lists automations comes back as
 * `references` so the caller can ask before retrying with `force: true`.
 */
export async function removeCalendarFlow(
  name: string,
  deps: RemoveDeps,
  opts: { force?: boolean } = {},
): Promise<RemoveOutcome> {
  try {
    await deps.remove(name, { force: opts.force === true });
    return { kind: "removed" };
  } catch (err) {
    const references = referencesFrom(err);
    if (references.length > 0) return { kind: "references", references };
    return { kind: "error", message: errorMessageFor(err) };
  }
}

export type FlowResult =
  | { ok: true; calendar: CalendarStatus }
  | { ok: false; message: string };

export interface CreateDeps {
  create: (input: CreateCalendarInput) => Promise<CalendarStatus>;
  clear: () => void;
}

/**
 * Adds a runtime source. `clear` runs on every attempt, including local
 * rejections, so the secret never lingers in the form after submit.
 */
export async function submitNewCalendar(
  draft: { name: string; poll: string },
  url: string,
  deps: CreateDeps,
): Promise<FlowResult> {
  try {
    const problem =
      validateCalendarName(draft.name) ??
      validatePollInterval(draft.poll) ??
      validateCalendarAddress(url);
    if (problem) return { ok: false, message: problem };
    const poll = draft.poll.trim();
    const calendar = await deps.create({
      name: draft.name,
      url: url.trim(),
      ...(poll ? { poll } : {}),
    });
    return { ok: true, calendar };
  } catch (err) {
    return { ok: false, message: errorMessageFor(err) };
  } finally {
    deps.clear();
  }
}

export interface UpdateDeps {
  update: (name: string, input: UpdateCalendarInput) => Promise<CalendarStatus>;
}

/** Replaces a runtime source's address. `clear` runs on every attempt. */
export async function replaceCalendarAddress(
  name: string,
  url: string,
  deps: UpdateDeps & { clear: () => void },
): Promise<FlowResult> {
  try {
    const problem = validateCalendarAddress(url);
    if (problem) return { ok: false, message: problem };
    const calendar = await deps.update(name, { url: url.trim() });
    return { ok: true, calendar };
  } catch (err) {
    return { ok: false, message: errorMessageFor(err) };
  } finally {
    deps.clear();
  }
}

/** Changes a runtime source's poll interval. Sends only the poll. */
export async function changePollInterval(
  name: string,
  poll: string,
  deps: UpdateDeps,
): Promise<FlowResult> {
  try {
    const problem =
      poll.trim() === ""
        ? "Enter a poll interval such as 5m."
        : validatePollInterval(poll);
    if (problem) return { ok: false, message: problem };
    const calendar = await deps.update(name, { poll: poll.trim() });
    return { ok: true, calendar };
  } catch (err) {
    return { ok: false, message: errorMessageFor(err) };
  }
}
