/**
 * MetadataModal — field editor for a task or a whole feature.
 *
 * The `s` (metadata) modal, including its tab split. See
 * `lib/actions/metadataFields` for the schema and the diffing rules.
 *
 * Two behaviours worth knowing:
 *
 * - **Only changed fields are sent.** A save PATCHes a diff, not the form.
 *   Rewriting every field would clobber values a runner updated while the
 *   modal was open.
 * - **Feature mode respects mixed values.** A field whose tasks disagree
 *   starts blank and marked "mixed"; leaving it alone leaves every task
 *   alone. Only touching it applies one value across the feature.
 */
import { useMemo, useState } from "react";

import { Modal } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { useLive } from "../../lib/sse";
import { bulkUpdate, updateEntry } from "../../lib/api";
import {
  buildPatch,
  fieldsForTab,
  initialFeatureValues,
  initialTaskValues,
  TAB_LABELS,
  tabsForMode,
  type FormValues,
  type MetadataField,
  type MetadataTab,
} from "../../lib/actions/metadataFields";
import { deriveFeatures } from "../../lib/features";
import type { Task } from "../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export interface MetadataModalProps {
  mode: "task" | "feature";
}

export function MetadataModal({ mode }: MetadataModalProps): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);

  const projectId = (target?.projectId as string | undefined) ?? "";
  const taskId = (target?.taskId as string | undefined) ?? "";
  const featureId = (target?.featureId as string | undefined) ?? "";

  const tasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;

  const task = useMemo(
    () => tasks.find((t) => t.id === taskId),
    [tasks, taskId],
  );
  const feature = useMemo(
    () => deriveFeatures(tasks, projectId).find((f) => f.id === featureId),
    [tasks, projectId, featureId],
  );

  // Snapshot the starting values once. Recomputing them from live SSE
  // updates would wipe the user's in-progress edits every time a runner
  // touched anything in the project.
  const [initial] = useState<{ values: FormValues; mixed: Set<string> }>(() => {
    if (mode === "task" && task) {
      return { values: initialTaskValues(task), mixed: new Set<string>() };
    }
    if (mode === "feature" && feature) {
      return initialFeatureValues(feature, tasks);
    }
    return { values: {}, mixed: new Set<string>() };
  });

  const [values, setValues] = useState<FormValues>(initial.values);
  // A mixed field stays skipped until the user actually edits it.
  const [untouchedMixed, setUntouchedMixed] = useState<Set<string>>(
    () => new Set(initial.mixed),
  );
  const [tab, setTab] = useState<MetadataTab>(() => tabsForMode(mode)[0]);
  const [busy, setBusy] = useState(false);

  const patch = useMemo(
    () => buildPatch(initial.values, values, mode, untouchedMixed),
    [initial.values, values, mode, untouchedMixed],
  );
  const dirtyCount = Object.keys(patch).length;

  if (mode === "task" && !task) {
    return (
      <Modal title="Task not found" onClose={close}>
        <div style={{ color: "#9098a1" }}>
          No matching task in <code>{projectId}</code>.
        </div>
      </Modal>
    );
  }
  if (mode === "feature" && !feature) {
    return (
      <Modal title="Feature not found" onClose={close}>
        <div style={{ color: "#9098a1" }}>
          No matching feature in <code>{projectId}</code>.
        </div>
      </Modal>
    );
  }

  const setField = (key: string, value: string | boolean) => {
    setValues((v) => ({ ...v, [key]: value }));
    // Editing a mixed field opts it into the save.
    setUntouchedMixed((s) => {
      if (!s.has(key)) return s;
      const next = new Set(s);
      next.delete(key);
      return next;
    });
  };

  const save = async () => {
    if (dirtyCount === 0) {
      close();
      return;
    }
    setBusy(true);
    try {
      if (mode === "task" && task) {
        await updateEntry(task.path, patch);
        toast(`Updated ${dirtyCount} field${dirtyCount === 1 ? "" : "s"}`, "success");
      } else if (mode === "feature" && feature) {
        const preview = await bulkUpdate(
          { project: projectId, feature_id: feature.id, type: "task" },
          patch,
          { dryRun: true },
        );
        if (preview.truncated) {
          throw new Error(
            `This feature has ${preview.matched_total ?? preview.total} tasks — ` +
              `more than the ${preview.total}-task limit for a single update.`,
          );
        }
        const result = await bulkUpdate(
          { project: projectId, feature_id: feature.id, type: "task" },
          patch,
        );
        toast(
          result.failed > 0
            ? `${result.updated} of ${result.total} tasks updated; ${result.failed} failed`
            : `Updated ${dirtyCount} field${dirtyCount === 1 ? "" : "s"} on ${result.updated} tasks`,
          result.failed > 0 ? "warning" : "success",
        );
      }
      close();
    } catch (err) {
      toast(
        `Save failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
      setBusy(false);
    }
  };

  const title =
    mode === "task"
      ? `Edit: ${task!.title || task!.id}`
      : `Edit feature: ${feature!.name}`;

  return (
    <Modal
      title={title}
      onClose={close}
      tabs={tabsForMode(mode).map((t) => ({ id: t, label: TAB_LABELS[t] }))}
      activeTab={tab}
      onTabChange={(id) => setTab(id as MetadataTab)}
      footer={
        <>
          <span className="faint" style={{ marginRight: "auto", fontSize: 11 }}>
            {dirtyCount === 0
              ? "No changes"
              : `${dirtyCount} field${dirtyCount === 1 ? "" : "s"} changed`}
          </span>
          <button onClick={close} disabled={busy}>
            Cancel
          </button>
          <button
            className="primary"
            data-autofocus="true"
            disabled={busy || dirtyCount === 0}
            onClick={() => void save()}
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </>
      }
    >
      {mode === "feature" && (
        <p style={{ fontSize: 11, color: "#9098a1", margin: "0 0 10px" }}>
          Changes apply to all {feature!.taskCount.total}{" "}
          {feature!.taskCount.total === 1 ? "task" : "tasks"} in this feature.
          Fields marked <em>mixed</em> differ between tasks and are left
          untouched unless you edit them.
        </p>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {fieldsForTab(tab, mode).map((field) => (
          <FieldRow
            key={field.key}
            field={field}
            value={values[field.key]}
            mixed={untouchedMixed.has(field.key)}
            disabled={busy}
            onChange={(v) => setField(field.key, v)}
          />
        ))}
      </div>
    </Modal>
  );
}

const inputStyle: React.CSSProperties = {
  width: "100%",
  padding: "5px 7px",
  fontSize: 12,
  fontFamily: "inherit",
  background: "#0a0c0e",
  border: "1px solid #22272c",
  borderRadius: 3,
  color: "#eaedef",
};

function FieldRow({
  field,
  value,
  mixed,
  disabled,
  onChange,
}: {
  field: MetadataField;
  value: string | boolean | undefined;
  mixed: boolean;
  disabled: boolean;
  onChange: (v: string | boolean) => void;
}): JSX.Element {
  return (
    <div>
      <label
        htmlFor={`meta-${field.key}`}
        style={{
          display: "block",
          fontSize: 11,
          color: "#9098a1",
          marginBottom: 4,
        }}
      >
        {field.label}
        {mixed && (
          <em style={{ color: "#d29922", marginLeft: 6, fontSize: 10 }}>
            mixed
          </em>
        )}
      </label>

      {field.kind === "boolean" ? (
        <input
          id={`meta-${field.key}`}
          type="checkbox"
          disabled={disabled}
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
        />
      ) : field.kind === "select" ? (
        <select
          id={`meta-${field.key}`}
          disabled={disabled}
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
          style={inputStyle}
        >
          {/* Empty option means "unset" — without it a field can only ever
              be changed, never cleared. */}
          <option value="">— unset —</option>
          {field.options?.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      ) : (
        <input
          id={`meta-${field.key}`}
          type="text"
          disabled={disabled}
          value={String(value ?? "")}
          placeholder={mixed ? "(mixed — edit to apply to all)" : field.placeholder}
          autoComplete="off"
          spellCheck={false}
          onChange={(e) => onChange(e.target.value)}
          style={inputStyle}
        />
      )}

      {field.help && (
        <div style={{ fontSize: 10, color: "#6b757e", marginTop: 3 }}>
          {field.help}
        </div>
      )}
    </div>
  );
}
