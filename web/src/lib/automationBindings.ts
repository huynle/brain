/**
 * lib/automationBindings — the pure side of per-project customization.
 *
 * A binding is a project-owned automation entry with `extends` set to a
 * global automation's id. It overrides some fields for one project, or
 * opts that project out. The server (internal/service/automation_binding.go)
 * decides what a binding does; this module only reads and writes the shape.
 *
 * Rules mirrored from the server, so the PWA cannot write a binding the
 * scheduler would read differently:
 *   - A field overrides only when it is present. Empty means inherit.
 *   - Timing is one unit: a schedule, or every + at. Writing one replaces
 *     the other.
 *   - PATCH replaces trigger and action wholesale, so a save always sends
 *     both in full. A status-only change sends only status, so it cannot
 *     wipe the overrides.
 *   - PATCH does not remove top-level fields it omits. Removal sends "" for
 *     timezone, starts_at and expires_at, and 0 for max_runs (0 inherits).
 *   - Opt-out is `archived`. `inactive` is not an entry status the API
 *     accepts, and only `active` opts a project in.
 *
 * Bindings never appear as rows of their own. They show through their
 * parent's row in the project that owns them.
 */
import type {
  AutomationAction,
  BrainEntry,
  CalendarEventFilter,
  TriggerConfig,
} from "./types";

/** Status a binding carries when it opts its project in. */
export const OPT_IN_STATUS = "active";
/** Status a binding carries when it opts its project out. */
export const OPT_OUT_STATUS = "archived";

/** The effective view, as GET /automations/{id}/effective returns it. */
export interface AutomationEffectiveView {
  id: string;
  project: string;
  automation?: {
    id: string;
    trigger?: TriggerConfig;
    action?: AutomationAction;
    timezone?: string;
    starts_at?: string;
    expires_at?: string;
    max_runs?: number;
  };
  fields: Record<string, string>;
  binding_id?: string;
  binding_status?: string;
  targeted: boolean;
  broken: boolean;
  broken_reason?: string;
}

/** Override state a binding carries, in the shape a binding is written. */
export interface BindingOverrides {
  trigger: TriggerConfig;
  action: AutomationAction;
  timezone?: string;
  starts_at?: string;
  expires_at?: string;
  max_runs?: number;
}

export type FieldGroupId =
  | "timing"
  | "stagger"
  | "catch_up"
  | "calendar"
  | "skip_if_event"
  | "only_if_event"
  | "timezone"
  | "starts_at"
  | "expires_at"
  | "max_runs"
  | "agent"
  | "model"
  | "executor"
  | "target_workdir"
  | "execution_mode"
  | "timeout"
  | "prompt_append";

export interface FieldGroup {
  id: FieldGroupId;
  label: string;
  /** Offered in the customize form. The rest are shown and can be reset. */
  editable: boolean;
  /** Keys in the effective view's `fields` map that this group covers. */
  fieldKeys: string[];
  /** Where the override lives on a binding. */
  slots: Array<{ at: "trigger" | "action" | "top"; name: string }>;
}

export interface FieldRow {
  id: FieldGroupId;
  label: string;
  editable: boolean;
  state: "inherited" | "overridden";
  /** The value this project runs under, or "—". */
  value: string;
}

/** Customize-form state. Every value is text, and "" means inherit. */
export interface BindingDraft {
  schedule: string;
  every: string;
  at: string;
  stagger: string;
  catch_up: string;
  calendar: string;
  timezone: string;
  starts_at: string;
  expires_at: string;
  max_runs: string;
  agent: string;
  model: string;
  executor: string;
  prompt_append: string;
}

/** What a toggle must write. Only `patch` and `delete` name a binding path. */
export type BindingPlan =
  | { kind: "none" }
  | { kind: "create"; status: string }
  | { kind: "patch"; path: string; status: string }
  | { kind: "delete"; path: string };

const TRIGGER_OVERRIDE_KEYS = [
  "schedule",
  "every",
  "at",
  "stagger",
  "catch_up",
  "calendar",
  "skip_if_event",
  "only_if_event",
] as const;

const ACTION_OVERRIDE_KEYS = [
  "agent",
  "model",
  "executor",
  "target_workdir",
  "execution_mode",
  "timeout",
  "prompt_append",
] as const;

const triggerSlot = (name: string) => ({ at: "trigger" as const, name });
const actionSlot = (name: string) => ({ at: "action" as const, name });
const topSlot = (name: string) => ({ at: "top" as const, name });

export const FIELD_GROUPS: FieldGroup[] = [
  {
    id: "timing",
    label: "Schedule (cron, or every + at)",
    editable: true,
    fieldKeys: ["trigger.schedule", "trigger.every", "trigger.at"],
    slots: [triggerSlot("schedule"), triggerSlot("every"), triggerSlot("at")],
  },
  {
    id: "stagger",
    label: "Stagger",
    editable: true,
    fieldKeys: ["trigger.stagger"],
    slots: [triggerSlot("stagger")],
  },
  {
    id: "catch_up",
    label: "Catch-up",
    editable: true,
    fieldKeys: ["trigger.catch_up"],
    slots: [triggerSlot("catch_up")],
  },
  {
    id: "calendar",
    label: "Calendar",
    editable: true,
    fieldKeys: ["trigger.calendar"],
    slots: [triggerSlot("calendar")],
  },
  {
    id: "skip_if_event",
    label: "Skip if event",
    editable: false,
    fieldKeys: ["trigger.skip_if_event"],
    slots: [triggerSlot("skip_if_event")],
  },
  {
    id: "only_if_event",
    label: "Only if event",
    editable: false,
    fieldKeys: ["trigger.only_if_event"],
    slots: [triggerSlot("only_if_event")],
  },
  {
    id: "timezone",
    label: "Timezone",
    editable: true,
    fieldKeys: ["timezone"],
    slots: [topSlot("timezone")],
  },
  {
    id: "starts_at",
    label: "Starts",
    editable: true,
    fieldKeys: ["starts_at"],
    slots: [topSlot("starts_at")],
  },
  {
    id: "expires_at",
    label: "Expires",
    editable: true,
    fieldKeys: ["expires_at"],
    slots: [topSlot("expires_at")],
  },
  {
    id: "max_runs",
    label: "Max runs",
    editable: true,
    fieldKeys: ["max_runs"],
    slots: [topSlot("max_runs")],
  },
  {
    id: "agent",
    label: "Agent",
    editable: true,
    fieldKeys: ["action.agent"],
    slots: [actionSlot("agent")],
  },
  {
    id: "model",
    label: "Model",
    editable: true,
    fieldKeys: ["action.model"],
    slots: [actionSlot("model")],
  },
  {
    id: "executor",
    label: "Executor",
    editable: true,
    fieldKeys: ["action.executor"],
    slots: [actionSlot("executor")],
  },
  {
    id: "target_workdir",
    label: "Workdir",
    editable: false,
    fieldKeys: ["action.target_workdir"],
    slots: [actionSlot("target_workdir")],
  },
  {
    id: "execution_mode",
    label: "Execution mode",
    editable: false,
    fieldKeys: ["action.execution_mode"],
    slots: [actionSlot("execution_mode")],
  },
  {
    id: "timeout",
    label: "Timeout",
    editable: false,
    fieldKeys: ["action.timeout"],
    slots: [actionSlot("timeout")],
  },
  {
    id: "prompt_append",
    label: "Prompt appended",
    editable: true,
    fieldKeys: ["action.prompt_append"],
    slots: [actionSlot("prompt_append")],
  },
];

const isBinding = (entry: BrainEntry): boolean => !!entry.extends;

/**
 * Split a merged automation list into rows and bindings. A binding is never
 * a row. An entry seen twice (scoped and global) is one row.
 */
export function mergeAutomationRows(
  scoped: BrainEntry[],
  global: BrainEntry[],
): { rows: BrainEntry[]; bindings: BrainEntry[] } {
  const byId = new Map<string, BrainEntry>();
  for (const entry of [...global, ...scoped]) byId.set(entry.id, entry);
  const rows: BrainEntry[] = [];
  const bindings: BrainEntry[] = [];
  for (const entry of byId.values()) {
    (isBinding(entry) ? bindings : rows).push(entry);
  }
  return { rows, bindings };
}

/** The project an entry belongs to: project_id, else its path. "" for global. */
export function bindingProjectOf(entry: BrainEntry): string {
  if (entry.project_id) return entry.project_id;
  const match = /^projects\/([^/]+)\//.exec(entry.path ?? "");
  return match ? match[1] : "";
}

/**
 * The binding that governs `project` for `parentId`. The oldest wins, then
 * the smaller id, which is the server's own tie-break. Other projects'
 * bindings are never returned.
 */
export function bindingFor(
  bindings: BrainEntry[],
  parentId: string,
  project: string,
): BrainEntry | null {
  const matches = bindings.filter(
    (b) => b.extends === parentId && bindingProjectOf(b) === project,
  );
  matches.sort(
    (a, b) =>
      (a.created ?? "").localeCompare(b.created ?? "") ||
      a.id.localeCompare(b.id),
  );
  return matches[0] ?? null;
}

export function emptyOverrides(): BindingOverrides {
  return { trigger: {}, action: {} };
}

const hasValue = (v: unknown): boolean => {
  if (v === undefined || v === null || v === "") return false;
  if (typeof v === "object" && Object.keys(v as object).length === 0) return false;
  return true;
};

/** What a binding overrides. Anything it does not set is absent here. */
export function overridesOf(binding: BrainEntry | null): BindingOverrides {
  const out = emptyOverrides();
  if (!binding) return out;

  const bt = (binding.trigger ?? {}) as Record<string, unknown>;
  for (const key of TRIGGER_OVERRIDE_KEYS) {
    if (hasValue(bt[key])) (out.trigger as Record<string, unknown>)[key] = bt[key];
  }
  const ba = (binding.action ?? {}) as Record<string, unknown>;
  for (const key of ACTION_OVERRIDE_KEYS) {
    if (hasValue(ba[key])) (out.action as Record<string, unknown>)[key] = ba[key];
  }
  const tz = (bt.timezone as string | undefined) || binding.timezone;
  if (tz) out.timezone = tz;
  if (binding.starts_at) out.starts_at = binding.starts_at;
  if (binding.expires_at) out.expires_at = binding.expires_at;
  if (binding.max_runs !== undefined && binding.max_runs !== null && binding.max_runs !== 0) {
    out.max_runs = binding.max_runs;
  }
  return out;
}

export function hasOverrides(o: BindingOverrides): boolean {
  return (
    Object.keys(o.trigger).length > 0 ||
    Object.keys(o.action).length > 0 ||
    o.timezone !== undefined ||
    o.starts_at !== undefined ||
    o.expires_at !== undefined ||
    o.max_runs !== undefined
  );
}

/** Remove one field group's override and keep everything else. Does not mutate. */
export function resetGroup(o: BindingOverrides, id: FieldGroupId): BindingOverrides {
  const group = FIELD_GROUPS.find((g) => g.id === id);
  const next: BindingOverrides = { ...o, trigger: { ...o.trigger }, action: { ...o.action } };
  if (!group) return next;
  for (const slot of group.slots) {
    if (slot.at === "trigger") delete (next.trigger as Record<string, unknown>)[slot.name];
    else if (slot.at === "action") delete (next.action as Record<string, unknown>)[slot.name];
    else delete (next as unknown as Record<string, unknown>)[slot.name];
  }
  return next;
}

export function draftFromOverrides(o: BindingOverrides): BindingDraft {
  return {
    schedule: o.trigger.schedule ?? "",
    every: o.trigger.every ?? "",
    at: o.trigger.at ?? "",
    stagger: o.trigger.stagger ?? "",
    catch_up: o.trigger.catch_up ?? "",
    calendar: o.trigger.calendar ?? "",
    timezone: o.timezone ?? "",
    starts_at: o.starts_at ?? "",
    expires_at: o.expires_at ?? "",
    max_runs: o.max_runs === undefined ? "" : String(o.max_runs),
    agent: o.action.agent ?? "",
    model: o.action.model ?? "",
    executor: o.action.executor ?? "",
    prompt_append: o.action.prompt_append ?? "",
  };
}

function setOrDelete(obj: Record<string, unknown>, key: string, value: string): void {
  if (value) obj[key] = value;
  else delete obj[key];
}

/**
 * Apply the form to the overrides it started from. Fields the form does not
 * edit (skip/only event, workdir, execution mode, timeout) are kept as they
 * were. Throws when max_runs is not a whole number.
 */
export function overridesFromDraft(base: BindingOverrides, draft: BindingDraft): BindingOverrides {
  const next: BindingOverrides = { ...base, trigger: { ...base.trigger }, action: { ...base.action } };
  const trigger = next.trigger as Record<string, unknown>;
  const action = next.action as Record<string, unknown>;
  const top = next as unknown as Record<string, unknown>;

  const schedule = draft.schedule.trim();
  const every = draft.every.trim();
  const at = draft.at.trim();
  setOrDelete(trigger, "schedule", schedule);
  if (schedule) {
    delete trigger.every;
    delete trigger.at;
  } else {
    setOrDelete(trigger, "every", every);
    setOrDelete(trigger, "at", at);
  }
  setOrDelete(trigger, "stagger", draft.stagger.trim());
  setOrDelete(trigger, "catch_up", draft.catch_up.trim());
  setOrDelete(trigger, "calendar", draft.calendar.trim());

  setOrDelete(top, "timezone", draft.timezone.trim());
  setOrDelete(top, "starts_at", draft.starts_at.trim());
  setOrDelete(top, "expires_at", draft.expires_at.trim());

  const maxRuns = draft.max_runs.trim();
  if (maxRuns === "") {
    delete next.max_runs;
  } else {
    if (!/^-?\d+$/.test(maxRuns)) {
      throw new Error("Max runs must be a whole number (-1 for unlimited, empty to inherit).");
    }
    const n = Number(maxRuns);
    if (n === 0) delete next.max_runs;
    else next.max_runs = n;
  }

  setOrDelete(action, "agent", draft.agent.trim());
  setOrDelete(action, "model", draft.model.trim());
  setOrDelete(action, "executor", draft.executor.trim());
  setOrDelete(action, "prompt_append", draft.prompt_append.trim());
  return next;
}

/**
 * Body for a PATCH that saves the overrides. It sends trigger and action in
 * full, because the server replaces them. It sends "" or 0 for the removed
 * top-level fields, because a PATCH that omits them leaves them unchanged.
 */
export function bindingPatchBody(o: BindingOverrides, status: string): Record<string, unknown> {
  return {
    status,
    trigger: { ...o.trigger },
    action: { ...o.action },
    timezone: o.timezone ?? "",
    starts_at: o.starts_at ?? "",
    expires_at: o.expires_at ?? "",
    max_runs: o.max_runs ?? 0,
  };
}

/** Body for a status-only change. It must not carry trigger or action. */
export function bindingStatusPatch(status: string): Record<string, unknown> {
  return { status };
}

const hasKeys = (obj: object): boolean => Object.keys(obj).length > 0;

/**
 * Body for POST /entries that creates a binding. It names the parent and the
 * project and sets no global flag, so it lands under the project. Overrides
 * that are not set are left out, so the create carries only what it means.
 */
export function bindingCreateBody(
  parent: BrainEntry,
  project: string,
  o: BindingOverrides,
  status: string,
): Record<string, unknown> {
  const body: Record<string, unknown> = {
    type: "automation",
    title: parent.title || parent.id,
    // The API requires content on create. A binding has no body of its own.
    content: `Per-project binding of ${parent.id}.`,
    project,
    extends: parent.id,
    status,
  };
  if (hasKeys(o.trigger)) body.trigger = { ...o.trigger };
  if (hasKeys(o.action)) body.action = { ...o.action };
  if (o.timezone !== undefined) body.timezone = o.timezone;
  if (o.starts_at !== undefined) body.starts_at = o.starts_at;
  if (o.expires_at !== undefined) body.expires_at = o.expires_at;
  if (o.max_runs !== undefined) body.max_runs = o.max_runs;
  return body;
}

/** What turning the automation on in this project writes. */
export function planTurnOn(binding: BrainEntry | null): BindingPlan {
  if (!binding) return { kind: "create", status: OPT_IN_STATUS };
  if (binding.status === OPT_IN_STATUS) return { kind: "none" };
  if (!hasOverrides(overridesOf(binding))) return { kind: "delete", path: binding.path };
  return { kind: "patch", path: binding.path, status: OPT_IN_STATUS };
}

/** What turning the automation off in this project writes. Never the global entry. */
export function planTurnOff(binding: BrainEntry | null): BindingPlan {
  if (!binding) return { kind: "create", status: OPT_OUT_STATUS };
  if (binding.status !== OPT_IN_STATUS) return { kind: "none" };
  return { kind: "patch", path: binding.path, status: OPT_OUT_STATUS };
}

function formatFilter(f: CalendarEventFilter | undefined): string {
  if (!f) return "—";
  const parts = Object.entries(f)
    .filter(([, v]) => !!v)
    .map(([k, v]) => `${k}=${v}`);
  return parts.length ? parts.join(", ") : "—";
}

function valueOf(
  id: FieldGroupId,
  cfg: AutomationEffectiveView["automation"],
  o: BindingOverrides,
): string {
  const t = cfg?.trigger;
  const a = cfg?.action;
  let v: string | undefined;
  switch (id) {
    case "timing":
      if (t?.schedule) v = t.schedule;
      else if (t?.every) v = `every ${t.every}${t.at ? ` at ${t.at}` : ""}`;
      break;
    case "stagger": v = t?.stagger; break;
    case "catch_up": v = t?.catch_up; break;
    case "calendar": v = t?.calendar; break;
    case "skip_if_event": v = formatFilter(t?.skip_if_event); break;
    case "only_if_event": v = formatFilter(t?.only_if_event); break;
    case "timezone": v = cfg?.timezone; break;
    case "starts_at": v = cfg?.starts_at; break;
    case "expires_at": v = cfg?.expires_at; break;
    case "max_runs":
      v = cfg?.max_runs === undefined ? undefined : cfg.max_runs === -1 ? "unlimited" : String(cfg.max_runs);
      break;
    case "agent": v = a?.agent; break;
    case "model": v = a?.model; break;
    case "executor": v = a?.executor; break;
    case "target_workdir": v = a?.target_workdir; break;
    case "execution_mode": v = a?.execution_mode; break;
    case "timeout": v = a?.timeout; break;
    case "prompt_append": v = o.action.prompt_append; break;
  }
  return v && v !== "" ? v : "—";
}

/**
 * One row per field group: its value in this project, and whether a binding
 * overrides it. The state comes from the server's view, not from local guesses.
 */
export function describeFields(effective: AutomationEffectiveView, o: BindingOverrides): FieldRow[] {
  return FIELD_GROUPS.map((g) => {
    const overridden = g.fieldKeys.some((k) => effective.fields?.[k] === "overridden");
    return {
      id: g.id,
      label: g.label,
      editable: g.editable,
      state: overridden ? "overridden" : "inherited",
      value: valueOf(g.id, effective.automation, o),
    };
  });
}

/** Plain-language reason a broken view cannot be trusted. */
export function brokenMessage(reason: string | undefined): string {
  switch (reason) {
    case "duplicate_binding":
      return "This project has more than one binding for this automation. The oldest one applies; remove the others.";
    case "parent_missing":
      return "The global automation this binding extends no longer exists. Delete this binding.";
    case "parent_not_automation":
      return "The entry this binding extends is no longer a global automation. Delete this binding.";
    default:
      return "This view cannot be trusted as the one the scheduler uses. Check the bindings for this automation.";
  }
}

/** Path and query for GET /automations/{id}/effective. */
export function automationEffectivePath(
  id: string,
  project: string,
): { path: string; query: { project: string } } {
  return {
    path: `/api/v1/automations/${encodeURIComponent(id)}/effective`,
    query: { project },
  };
}
