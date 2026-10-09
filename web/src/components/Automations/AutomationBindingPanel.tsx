/**
 * AutomationBindingPanel — how one global automation runs in one project.
 *
 * Shown in the automation's detail when it is viewed from a project. It lists
 * each field with the value this project runs under and whether that value is
 * inherited from the global automation or overridden by this project's
 * binding. An overridden field offers a reset. "Customize for this project"
 * opens a form for the editable fields only.
 *
 * A broken view (the server cannot vouch for the binding) is shown as a
 * warning, never as a silent answer.
 *
 * The view is presentational so its rules can be tested without a query
 * client. The container wires it to useEffectiveAutomation and the action
 * context, which writes the binding and never the global entry.
 */
import * as React from "react";
import { useId, useState, type FormEvent } from "react";

import { useEffectiveAutomation } from "../../hooks/useEffectiveAutomation";
import type { AutomationActionContext, ProjectScope } from "../../lib/actions/automationActions";
import {
  brokenMessage,
  describeFields,
  draftFromOverrides,
  overridesFromDraft,
  overridesOf,
  resetGroup,
  type AutomationEffectiveView,
  type BindingDraft,
  type BindingOverrides,
  type FieldGroupId,
} from "../../lib/automationBindings";
import type { BrainEntry } from "../../lib/types";

export interface BindingPanelViewProps {
  projectId: string;
  effective: AutomationEffectiveView | null;
  loading: boolean;
  error: unknown;
  onRetry: () => void;
  binding: BrainEntry | null;
  busy: boolean;
  saveError: string | null;
  /** Test seam: start with the customize form open. */
  defaultCustomizeOpen?: boolean;
  onSave: (next: BindingOverrides) => void;
  onReset: (group: FieldGroupId) => void;
}

/** The form's fields, in order, with the label each one carries. */
const FORM_FIELDS: Array<{ key: keyof BindingDraft; label: string }> = [
  { key: "schedule", label: "Cron schedule" },
  { key: "every", label: "Every" },
  { key: "at", label: "At (HH:MM)" },
  { key: "stagger", label: "Stagger" },
  { key: "catch_up", label: "Catch-up" },
  { key: "calendar", label: "Calendar" },
  { key: "timezone", label: "Timezone" },
  { key: "starts_at", label: "Starts" },
  { key: "expires_at", label: "Expires" },
  { key: "max_runs", label: "Max runs" },
  { key: "agent", label: "Agent" },
  { key: "model", label: "Model" },
  { key: "executor", label: "Executor" },
  { key: "prompt_append", label: "Appended prompt" },
];

const messageOf = (err: unknown): string =>
  err instanceof Error ? err.message : String(err);

export function BindingPanelView({
  projectId,
  effective,
  loading,
  error,
  onRetry,
  binding,
  busy,
  saveError,
  defaultCustomizeOpen = false,
  onSave,
  onReset,
}: BindingPanelViewProps): JSX.Element | null {
  const formId = useId();
  const [customizeOpen, setCustomizeOpen] = useState(defaultCustomizeOpen);
  const base = overridesOf(binding);
  const [draft, setDraft] = useState<BindingDraft>(() => draftFromOverrides(base));
  const [formError, setFormError] = useState<string | null>(null);

  if (loading) {
    return (
      <div className="arun-note" role="status">
        Checking this project…
      </div>
    );
  }
  if (error) {
    return (
      <div role="alert" className="arun-note">
        Could not load this project&apos;s view: {messageOf(error)}{" "}
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      </div>
    );
  }
  if (!effective) return null;

  const rows = describeFields(effective, base);
  const off = !effective.targeted;
  const message = formError ?? saveError;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setFormError(null);
    let next: BindingOverrides;
    try {
      next = overridesFromDraft(base, draft);
    } catch (err) {
      setFormError(messageOf(err));
      return;
    }
    onSave(next);
  };

  return (
    <div className="ab-panel">
      {effective.broken && (
        <div role="alert" className="arun-note ab-broken">
          ⚠ {brokenMessage(effective.broken_reason)}
        </div>
      )}

      <div className="arun-note">
        {off ? "Off" : "On"} in {projectId}.{" "}
        {binding
          ? `Binding is ${binding.status}.`
          : "No binding: this project runs the global definition."}
      </div>

      <div className="kv-grid">
        {rows.map((row) => (
          <React.Fragment key={row.id}>
            <div className="k">{row.label}</div>
            <div className="v">
              {row.value}{" "}
              <span className={`health ${row.state}`}>{row.state}</span>
              {row.state === "overridden" && (
                <button
                  type="button"
                  aria-label={`Reset ${row.label} to inherited`}
                  disabled={busy}
                  onClick={() => onReset(row.id)}
                >
                  Reset to inherited
                </button>
              )}
            </div>
          </React.Fragment>
        ))}
      </div>

      <button
        type="button"
        aria-expanded={customizeOpen}
        aria-controls={formId}
        onClick={() => setCustomizeOpen((v) => !v)}
      >
        {customizeOpen ? "▾" : "▸"} Customize for this project
      </button>

      {customizeOpen && (
        <form id={formId} onSubmit={submit}>
          <p className="arun-note">
            Leave a field empty to inherit it from the global automation.
          </p>
          {off && !binding && (
            <p className="arun-note">
              This project is off and has no binding. Saving keeps it off; use
              Turn on here to run it.
            </p>
          )}
          {FORM_FIELDS.map(({ key, label }) => {
            const inputId = `${formId}-${key}`;
            return (
              <div key={key}>
                <label htmlFor={inputId}>{label}</label>
                <input
                  id={inputId}
                  value={draft[key]}
                  disabled={busy}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, [key]: e.target.value }))
                  }
                />
              </div>
            );
          })}
          <div>
            <button type="submit" disabled={busy}>
              Save binding
            </button>{" "}
            <button
              type="button"
              disabled={busy}
              onClick={() => setCustomizeOpen(false)}
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {message && (
        <div role="alert" className="arun-note">
          {message}
        </div>
      )}
    </div>
  );
}

export interface AutomationBindingPanelProps {
  /** A global automation. Project-owned automations have no panel. */
  automation: BrainEntry;
  projectId: string;
  binding: BrainEntry | null;
  ctx: AutomationActionContext;
}

export function AutomationBindingPanel({
  automation,
  projectId,
  binding,
  ctx,
}: AutomationBindingPanelProps): JSX.Element {
  const { effective, isLoading, error, refresh } = useEffectiveAutomation(
    automation.id,
    projectId,
  );
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const scope: ProjectScope = {
    projectId,
    binding,
    targeted: effective?.targeted ?? false,
  };

  const save = async (next: BindingOverrides) => {
    setBusy(true);
    setSaveError(null);
    try {
      await ctx.saveOverrides(automation, scope, next);
    } catch (err) {
      setSaveError(messageOf(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <BindingPanelView
      // Remount when the binding changes, so the form starts from what was saved.
      key={binding ? `${binding.id}:${binding.modified ?? ""}` : "none"}
      projectId={projectId}
      effective={effective}
      loading={isLoading}
      error={error}
      onRetry={refresh}
      binding={binding}
      busy={busy}
      saveError={saveError}
      onSave={(next) => void save(next)}
      onReset={(group) => void save(resetGroup(overridesOf(binding), group))}
    />
  );
}
