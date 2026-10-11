/**
 * Settings → Calendars. Lists every calendar source and lets an admin add a
 * runtime source, replace its address, change its poll interval, or remove it.
 * Config sources are shown read-only.
 *
 * Secret handling: the address lives only in an uncontrolled password input.
 * It is read once on submit, and the input is cleared after every submit or
 * cancel. It never enters React state, the query cache or local storage, and
 * it is not passed as React Query mutation variables (those are retained).
 */
import * as React from "react";
import { useRef, useState, type FormEvent, type RefObject } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { useUI } from "../../store/ui";
import { relativeTime } from "../../lib/format";
import {
  createCalendar,
  deleteCalendar,
  listCalendars,
  updateCalendar,
  type CalendarReference,
  type CalendarStatus,
} from "../../lib/calendars";
import {
  calendarStatusView,
  changePollInterval,
  errorMessageFor,
  isRuntimeCalendar,
  removeCalendarFlow,
  replaceCalendarAddress,
  submitNewCalendar,
  type CalendarStatusView,
  type CalendarTone,
} from "../../lib/calendarSettings";

const CALENDARS_KEY = ["settings", "calendars"] as const;
const ADDRESS_INPUT_ID = "calendar-address-input";

const FIELD_LABELS: Record<CalendarReference["field"], string> = {
  "trigger.calendar": "Calendar trigger",
  "trigger.skip_if_event": "Skip if event",
  "trigger.only_if_event": "Only if event",
};

const TONE_COLOR: Record<CalendarTone, string> = {
  ok: "#6fca7d",
  warn: "#f4b23a",
  error: "#e57878",
  muted: "#6b757e",
};

export interface CalendarRowActions {
  onReplace: () => void;
  onChangePoll: () => void;
  onRemove: () => void;
}

// ─── presentational pieces ───────────────────────────────────────

function StatusBadge({ status }: { status: CalendarStatusView }): JSX.Element {
  return (
    <span className="calendar-status" style={{ color: TONE_COLOR[status.tone] }}>
      {status.label}
      {status.detail && (
        <span className="calendar-status-detail" style={{ display: "block" }}>
          {status.detail}
        </span>
      )}
    </span>
  );
}

/** One source. Only runtime sources get edit controls; config sources never do. */
export function CalendarRow({
  calendar,
  actions,
  busy = false,
}: {
  calendar: CalendarStatus;
  actions?: CalendarRowActions;
  busy?: boolean;
}): JSX.Element {
  const runtime = isRuntimeCalendar(calendar);
  const isIcs = calendar.kind === "ics";
  const count = calendar.event_count;

  return (
    <li className="calendar-row">
      <div className="calendar-row-head">
        <strong className="calendar-row-name">{calendar.name}</strong>
        <StatusBadge status={calendarStatusView(calendar)} />
      </div>
      <dl className="calendar-facts">
        <div>
          <dt>Source</dt>
          <dd>{isIcs ? "ICS feed" : "Built in"}</dd>
        </div>
        {isIcs && calendar.poll && (
          <div>
            <dt>Poll</dt>
            <dd>Every {calendar.poll}</dd>
          </div>
        )}
        {isIcs && (
          <div>
            <dt>Last success</dt>
            <dd>{calendar.last_success ? relativeTime(calendar.last_success) : "never"}</dd>
          </div>
        )}
        {count !== undefined && (
          <div>
            <dt>Events</dt>
            <dd>{`${count} ${count === 1 ? "event" : "events"}`}</dd>
          </div>
        )}
      </dl>
      {runtime && actions ? (
        <div className="settings-actions">
          <button type="button" onClick={actions.onReplace} disabled={busy}>
            Replace address
          </button>
          <button type="button" onClick={actions.onChangePoll} disabled={busy}>
            Change poll interval
          </button>
          <button type="button" onClick={actions.onRemove} disabled={busy}>
            Remove
          </button>
        </div>
      ) : (
        <div className="settings-note">Defined in server config and read-only here.</div>
      )}
    </li>
  );
}

/** The list with loading, error and empty states. */
export function CalendarsBody({
  calendars,
  loading,
  error,
  onRetry,
  actionsFor,
  busy = false,
}: {
  calendars: CalendarStatus[] | undefined;
  loading: boolean;
  error: unknown;
  onRetry: () => void;
  actionsFor: (calendar: CalendarStatus) => CalendarRowActions | undefined;
  busy?: boolean;
}): JSX.Element {
  if (loading) {
    return (
      <div role="status" aria-live="polite" className="settings-note">
        Loading calendars…
      </div>
    );
  }
  if (error) {
    return (
      <div role="alert" className="settings-banner settings-banner-error">
        <div>Couldn't load calendars: {errorMessageFor(error)}</div>
        <button type="button" onClick={onRetry} style={{ marginTop: 6 }}>
          Retry
        </button>
      </div>
    );
  }
  if (!calendars || calendars.length === 0) {
    return (
      <div className="settings-note">
        No calendar sources yet. Add one to use it in automations.
      </div>
    );
  }
  return (
    <ul className="calendar-list" style={{ listStyle: "none", padding: 0, margin: 0 }}>
      {calendars.map((calendar) => (
        <React.Fragment key={calendar.name}>
          <CalendarRow
            calendar={calendar}
            actions={actionsFor(calendar)}
            busy={busy}
          />
        </React.Fragment>
      ))}
    </ul>
  );
}

/** Shown before a removal that automations still reference. */
export function RemovalPrompt({
  name,
  references,
  busy,
  error,
  onRemoveAnyway,
  onCancel,
}: {
  name: string;
  references: CalendarReference[];
  busy: boolean;
  error: string | null;
  onRemoveAnyway: () => void;
  onCancel: () => void;
}): JSX.Element {
  const headingId = `calendar-removal-${name}`;
  const count = references.length;
  return (
    <div
      role="alertdialog"
      aria-labelledby={`${headingId}-title`}
      aria-describedby={`${headingId}-desc`}
      className="calendar-removal"
    >
      <h5 id={`${headingId}-title`}>Remove “{name}”?</h5>
      <p id={`${headingId}-desc`} className="settings-note">
        {`${count} ${count === 1 ? "automation refers" : "automations refer"} to this calendar. `}
        Skip-if-event filters will fail open, only-if-event filters will fail closed,
        and calendar triggers will stop firing.
      </p>
      <ul>
        {references.map((ref) => (
          <li key={`${ref.id}:${ref.field}`}>
            <strong>{ref.title}</strong> <span>({FIELD_LABELS[ref.field] ?? ref.field})</span>
          </li>
        ))}
      </ul>
      {error && (
        <div role="alert" className="settings-banner settings-banner-error">
          {error}
        </div>
      )}
      <div className="settings-actions">
        <button type="button" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="button" className="danger" onClick={onRemoveAnyway} disabled={busy}>
          {busy ? "Removing…" : "Remove anyway"}
        </button>
      </div>
    </div>
  );
}

/** Masked address input. It is uncontrolled, so no value is ever rendered or held in state. */
export function CalendarAddressInput({
  id,
  inputRef,
  label = "Secret address",
}: {
  id: string;
  inputRef: RefObject<HTMLInputElement>;
  label?: string;
}): JSX.Element {
  return (
    <div className="setting-field" style={{ flexWrap: "wrap" }}>
      <label htmlFor={id} style={{ minWidth: 180 }}>
        {label}
      </label>
      <input
        id={id}
        ref={inputRef}
        type="password"
        name="calendar-address"
        autoComplete="off"
        autoCapitalize="off"
        autoCorrect="off"
        spellCheck={false}
        required
        aria-describedby={`${id}-help`}
        style={{ flex: 1, minWidth: 200 }}
      />
      <div id={`${id}-help`} className="setting-help">
        In Google Calendar, open the calendar's settings and copy the “Secret address in
        iCal format”. The address is stored only on the server and cannot be viewed again.
        To change it, reset it in Google and replace it here.
      </div>
    </div>
  );
}

// ─── container ───────────────────────────────────────────────────

type Panel =
  | { kind: "add" }
  | { kind: "address"; name: string }
  | { kind: "poll"; name: string }
  | { kind: "remove"; name: string; references: CalendarReference[] };

export function CalendarsSettings(): JSX.Element {
  const queryClient = useQueryClient();
  const toast = useUI((s) => s.toast);
  const query = useQuery({
    queryKey: CALENDARS_KEY,
    queryFn: listCalendars,
    staleTime: 30_000,
  });

  const [panel, setPanel] = useState<Panel | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [draftName, setDraftName] = useState("");
  const [draftPoll, setDraftPoll] = useState("");
  const addressRef = useRef<HTMLInputElement>(null);

  const clearAddress = () => {
    if (addressRef.current) addressRef.current.value = "";
  };
  const refresh = () => queryClient.invalidateQueries({ queryKey: CALENDARS_KEY });
  const closePanel = () => {
    clearAddress();
    setPanel(null);
    setMessage(null);
    setDraftName("");
    setDraftPoll("");
  };
  const openPanel = (next: Panel, poll = "") => {
    clearAddress();
    setMessage(null);
    setDraftPoll(poll);
    setPanel(next);
  };
  const succeed = (text: string) => {
    closePanel();
    toast(text, "success");
    void refresh();
  };

  const submitAdd = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setBusy(true);
    setMessage(null);
    const result = await submitNewCalendar(
      { name: draftName.trim(), poll: draftPoll },
      addressRef.current?.value ?? "",
      { create: createCalendar, clear: clearAddress },
    );
    setBusy(false);
    if (result.ok) succeed(`Calendar “${result.calendar.name}” added`);
    else setMessage(result.message);
  };

  const submitAddress = async (name: string, event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setBusy(true);
    setMessage(null);
    const result = await replaceCalendarAddress(
      name,
      addressRef.current?.value ?? "",
      { update: updateCalendar, clear: clearAddress },
    );
    setBusy(false);
    if (result.ok) succeed(`Address replaced for “${name}”`);
    else setMessage(result.message);
  };

  const submitPoll = async (name: string, event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setBusy(true);
    setMessage(null);
    const result = await changePollInterval(name, draftPoll, { update: updateCalendar });
    setBusy(false);
    if (result.ok) succeed(`Poll interval for “${name}” set to ${draftPoll.trim()}`);
    else setMessage(result.message);
  };

  const remove = async (name: string, force: boolean) => {
    setBusy(true);
    setMessage(null);
    const outcome = await removeCalendarFlow(name, { remove: deleteCalendar }, { force });
    setBusy(false);
    if (outcome.kind === "removed") succeed(`Calendar “${name}” removed`);
    else if (outcome.kind === "references")
      setPanel({ kind: "remove", name, references: outcome.references });
    else setMessage(outcome.message);
  };

  const actionsFor = (calendar: CalendarStatus): CalendarRowActions | undefined =>
    isRuntimeCalendar(calendar)
      ? {
          onReplace: () => openPanel({ kind: "address", name: calendar.name }),
          onChangePoll: () =>
            openPanel({ kind: "poll", name: calendar.name }, calendar.poll ?? ""),
          onRemove: () => void remove(calendar.name, false),
        }
      : undefined;

  const banner = message && (
    <div role="alert" className="settings-banner settings-banner-error">
      {message}
    </div>
  );

  return (
    <section className="calendars-section" aria-labelledby="calendars-heading">
      <div className="mod-group">
        <h4 id="calendars-heading">Calendars</h4>
        <div className="settings-note">
          Calendar sources feed the calendar triggers and day filters in automations. Each
          Google calendar needs its own secret address.
        </div>
        {panel === null && banner}
        <CalendarsBody
          calendars={query.data}
          loading={query.isLoading}
          error={query.error}
          onRetry={() => void query.refetch()}
          actionsFor={actionsFor}
          busy={busy}
        />

        {panel?.kind === "remove" && (
          <RemovalPrompt
            name={panel.name}
            references={panel.references}
            busy={busy}
            error={message}
            onRemoveAnyway={() => void remove(panel.name, true)}
            onCancel={closePanel}
          />
        )}

        {panel === null && (
          <div className="settings-actions">
            <button type="button" className="primary" onClick={() => openPanel({ kind: "add" })}>
              Add calendar
            </button>
          </div>
        )}

        {panel?.kind === "add" && (
          <form className="calendar-form" onSubmit={submitAdd} noValidate aria-label="Add calendar">
            <h5>Add calendar</h5>
            <div className="setting-field" style={{ flexWrap: "wrap" }}>
              <label htmlFor="calendar-name-input" style={{ minWidth: 180 }}>
                Name
              </label>
              <input
                id="calendar-name-input"
                type="text"
                value={draftName}
                onChange={(e) => setDraftName(e.target.value)}
                placeholder="personal"
                autoComplete="off"
                spellCheck={false}
                aria-describedby="calendar-name-help"
                style={{ flex: 1, minWidth: 200 }}
              />
              <div id="calendar-name-help" className="setting-help">
                Lowercase letters, digits, “-” or “_”. Automations refer to the calendar by
                this name.
              </div>
            </div>
            <CalendarAddressInput id={ADDRESS_INPUT_ID} inputRef={addressRef} />
            <div className="setting-field" style={{ flexWrap: "wrap" }}>
              <label htmlFor="calendar-poll-input" style={{ minWidth: 180 }}>
                Poll interval
              </label>
              <input
                id="calendar-poll-input"
                type="text"
                value={draftPoll}
                onChange={(e) => setDraftPoll(e.target.value)}
                placeholder="5m"
                autoComplete="off"
                spellCheck={false}
                aria-describedby="calendar-poll-help"
                style={{ flex: 1, minWidth: 200 }}
              />
              <div id="calendar-poll-help" className="setting-help">
                Optional. Default 5m, minimum 1m.
              </div>
            </div>
            {banner}
            <div className="settings-actions">
              <button type="submit" className="primary" disabled={busy}>
                {busy ? "Adding…" : "Add calendar"}
              </button>
              <button type="button" onClick={closePanel} disabled={busy}>
                Cancel
              </button>
            </div>
          </form>
        )}

        {panel?.kind === "address" && (
          <form
            className="calendar-form"
            onSubmit={(e) => void submitAddress(panel.name, e)}
            noValidate
            aria-label={`Replace address for ${panel.name}`}
          >
            <h5>Replace address for “{panel.name}”</h5>
            <CalendarAddressInput
              id={ADDRESS_INPUT_ID}
              inputRef={addressRef}
              label="New secret address"
            />
            {banner}
            <div className="settings-actions">
              <button type="submit" className="primary" disabled={busy}>
                {busy ? "Replacing…" : "Replace address"}
              </button>
              <button type="button" onClick={closePanel} disabled={busy}>
                Cancel
              </button>
            </div>
          </form>
        )}

        {panel?.kind === "poll" && (
          <form
            className="calendar-form"
            onSubmit={(e) => void submitPoll(panel.name, e)}
            noValidate
            aria-label={`Change poll interval for ${panel.name}`}
          >
            <h5>Change poll interval for “{panel.name}”</h5>
            <div className="setting-field" style={{ flexWrap: "wrap" }}>
              <label htmlFor="calendar-poll-edit" style={{ minWidth: 180 }}>
                Poll interval
              </label>
              <input
                id="calendar-poll-edit"
                type="text"
                value={draftPoll}
                onChange={(e) => setDraftPoll(e.target.value)}
                placeholder="5m"
                autoComplete="off"
                spellCheck={false}
                aria-describedby="calendar-poll-edit-help"
                style={{ flex: 1, minWidth: 200 }}
              />
              <div id="calendar-poll-edit-help" className="setting-help">
                A duration such as 5m or 1h30m. Minimum 1m.
              </div>
            </div>
            {banner}
            <div className="settings-actions">
              <button type="submit" className="primary" disabled={busy}>
                {busy ? "Saving…" : "Save interval"}
              </button>
              <button type="button" onClick={closePanel} disabled={busy}>
                Cancel
              </button>
            </div>
          </form>
        )}
      </div>
    </section>
  );
}
