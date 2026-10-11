/**
 * Runtime calendar sources: types and HTTP client.
 *
 * Contract: docs/plans/2026-10-11-runtime-calendar-sources-design.md
 * ("HTTP contract"). Server handlers live in internal/api/calendars.go.
 *
 * The address (`url`) is a secret. It is sent only in a request body, is
 * never part of a URL or query string, and the server never returns it. Do
 * not route it through React Query mutation variables, which retain them in
 * the mutation cache; callers invoke these functions directly.
 */
import { api } from "./api";

export type CalendarKind = "builtin" | "ics";
export type CalendarOrigin = "config" | "runtime";

/** One source as GET /api/v1/calendars reports it. Never carries an address. */
export interface CalendarStatus {
  name: string;
  kind: CalendarKind;
  /** "config" sources are defined in server config and are read-only here. */
  origin?: CalendarOrigin;
  /** Go duration string, ICS sources only, e.g. "5m". */
  poll?: string;
  last_fetch?: string;
  last_success?: string;
  event_count?: number;
  last_error?: string;
  stale?: boolean;
}

export type CalendarReferenceField =
  | "trigger.calendar"
  | "trigger.skip_if_event"
  | "trigger.only_if_event";

/** An automation that refers to a calendar by name. */
export interface CalendarReference {
  id: string;
  path: string;
  title: string;
  field: CalendarReferenceField;
}

/** Response of DELETE /api/v1/calendars/{name}. */
export interface CalendarRemoval {
  name: string;
  removed: boolean;
  references: CalendarReference[];
}

export interface CreateCalendarInput {
  name: string;
  /** Secret iCal address. Sent once; never stored by the client. */
  url: string;
  /** Optional Go duration, minimum 1m; the server defaults to 5m. */
  poll?: string;
}

export interface UpdateCalendarInput {
  /** Replacement secret address. */
  url?: string;
  /** New poll interval. At least one of url or poll is required. */
  poll?: string;
}

export const listCalendars = async (): Promise<CalendarStatus[]> => {
  const res = await api<{ calendars: CalendarStatus[] | null }>(
    "/api/v1/calendars",
  );
  return res.calendars ?? [];
};

export const createCalendar = (input: CreateCalendarInput) =>
  api<CalendarStatus>("/api/v1/calendars", { method: "POST", body: input });

export const updateCalendar = (name: string, input: UpdateCalendarInput) =>
  api<CalendarStatus>(`/api/v1/calendars/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: input,
  });

export const deleteCalendar = (name: string, opts: { force?: boolean } = {}) =>
  api<CalendarRemoval>(`/api/v1/calendars/${encodeURIComponent(name)}`, {
    method: "DELETE",
    query: { force: opts.force ? "true" : undefined },
  });
