/**
 * lib/actions/metadataFields — the editable field schema for tasks and
 * features.
 *
 * Keeps the metadata field set and its tab split in one place, so the PWA
 * and the TUI expose the same fields under the same headings. Where the
 * TUI has a field the PWA lacks, the two surfaces disagree about what a
 * task *is*, which is worse than either being incomplete.
 *
 * Pure data + pure diffing. The modal renders from this; the tests assert
 * against it. Enum options are duplicated from the server's validation
 * lists rather than fetched — they change with a release, not at runtime,
 * and a wrong value is rejected by the API with a field-level error we
 * surface anyway.
 */
import { ALL_STATUSES, type Task } from "../types";
import type { DerivedFeature } from "../features";

export type FieldKind = "text" | "select" | "boolean" | "list";

export interface MetadataField {
  /** Frontmatter key. This is what gets PATCHed. */
  key: string;
  label: string;
  kind: FieldKind;
  /** For `select`. Empty string is always prepended as "unset". */
  options?: readonly string[];
  /** Shown under the input. Say what the field does, not what it is. */
  help?: string;
  placeholder?: string;
}

export type MetadataTab = "task" | "execution" | "git" | "feature";

export const TAB_LABELS: Record<MetadataTab, string> = {
  task: "Task",
  execution: "Execution",
  git: "Git & Merge",
  feature: "Feature",
};

export const PRIORITIES = ["high", "medium", "low"] as const;
export const EXECUTORS = ["opencode", "pi"] as const;
export const EXECUTION_MODES = ["worktree", "current_branch"] as const;
export const MERGE_POLICIES = ["prompt_only", "auto_pr", "auto_merge"] as const;
export const MERGE_STRATEGIES = ["squash", "merge", "rebase"] as const;
export const REMOTE_BRANCH_POLICIES = ["keep", "delete"] as const;
export const CHECKOUT_MODES = ["ai", "simple"] as const;

const TASK_FIELDS: readonly MetadataField[] = [
  {
    key: "status",
    label: "Status",
    kind: "select",
    options: ALL_STATUSES,
  },
  { key: "priority", label: "Priority", kind: "select", options: PRIORITIES },
  {
    key: "feature_id",
    label: "Feature",
    kind: "text",
    help: "Tasks sharing a feature id are grouped and run together.",
    placeholder: "e.g. checkout-flow",
  },
  {
    key: "depends_on",
    label: "Depends on",
    kind: "list",
    help: "Task ids or titles. This task waits until all of them finish.",
    placeholder: "task-id, another task",
  },
];

const EXECUTION_FIELDS: readonly MetadataField[] = [
  {
    key: "agent",
    label: "Agent",
    kind: "text",
    placeholder: "e.g. tdd-dev",
  },
  {
    key: "model",
    label: "Model",
    kind: "text",
    placeholder: "e.g. anthropic/claude-sonnet-4-20250514",
  },
  { key: "executor", label: "Executor", kind: "select", options: EXECUTORS },
  {
    key: "execution_mode",
    label: "Execution mode",
    kind: "select",
    options: EXECUTION_MODES,
    help: "worktree runs in an isolated checkout; current_branch runs in place.",
  },
  {
    key: "target_workdir",
    label: "Target workdir",
    kind: "text",
    placeholder: "/path/to/repo",
  },
  {
    key: "complete_on_idle",
    label: "Complete on idle",
    kind: "boolean",
    help: "Mark the task done when the executor goes idle.",
  },
];

const GIT_FIELDS: readonly MetadataField[] = [
  { key: "git_branch", label: "Branch", kind: "text" },
  { key: "merge_target_branch", label: "Merge target", kind: "text", placeholder: "main" },
  {
    key: "merge_policy",
    label: "Merge policy",
    kind: "select",
    options: MERGE_POLICIES,
  },
  {
    key: "merge_strategy",
    label: "Merge strategy",
    kind: "select",
    options: MERGE_STRATEGIES,
  },
  {
    key: "remote_branch_policy",
    label: "Remote branch",
    kind: "select",
    options: REMOTE_BRANCH_POLICIES,
  },
  { key: "open_pr_before_merge", label: "Open PR before merge", kind: "boolean" },
  {
    key: "checkout_mode",
    label: "Checkout mode",
    kind: "select",
    options: CHECKOUT_MODES,
    help: "ai runs the review agent; simple does a deterministic squash-merge.",
  },
];

const FEATURE_FIELDS: readonly MetadataField[] = [
  {
    key: "feature_priority",
    label: "Feature priority",
    kind: "select",
    options: PRIORITIES,
  },
  {
    key: "feature_depends_on",
    label: "Waits on features",
    kind: "list",
    help: "Feature ids this feature depends on. Drives the dependency tree.",
    placeholder: "catalog-schema, search-revamp",
  },
];

/** Fields for a tab, in the mode being edited. */
export function fieldsForTab(
  tab: MetadataTab,
  mode: "task" | "feature",
): readonly MetadataField[] {
  switch (tab) {
    case "task":
      // In feature mode every value is applied to every task, so
      // per-task identity fields (feature_id, depends_on) are excluded —
      // setting them in bulk would collapse the whole feature into one
      // dependency or move all of it to another feature at once.
      return mode === "feature"
        ? TASK_FIELDS.filter((f) => f.key === "status" || f.key === "priority")
        : TASK_FIELDS;
    case "execution":
      return EXECUTION_FIELDS;
    case "git":
      return GIT_FIELDS;
    case "feature":
      return FEATURE_FIELDS;
  }
}

/** Tabs available in a mode. Feature-level fields only appear for features. */
export function tabsForMode(mode: "task" | "feature"): MetadataTab[] {
  return mode === "feature"
    ? ["feature", "task", "execution", "git"]
    : ["task", "execution", "git", "feature"];
}

/** Every field across every tab, for a mode. */
export function allFields(mode: "task" | "feature"): MetadataField[] {
  return tabsForMode(mode).flatMap((tab) => [...fieldsForTab(tab, mode)]);
}

/** Form value shape: everything is edited as a string or boolean. */
export type FormValues = Record<string, string | boolean>;

/** Read the initial form state for a task. */
export function initialTaskValues(task: Task): FormValues {
  const raw = task as unknown as Record<string, unknown>;
  const values: FormValues = {};
  for (const field of allFields("task")) {
    const v = raw[field.key];
    values[field.key] = toFormValue(field, v);
  }
  return values;
}

/**
 * Read the initial form state for a feature.
 *
 * Only fields where every task already agrees get a value; anything mixed
 * starts blank, so a blind save cannot silently flatten a deliberate
 * per-task difference.
 */
export function initialFeatureValues(
  feature: DerivedFeature,
  tasks: readonly Task[],
): { values: FormValues; mixed: Set<string> } {
  const members = tasks.filter((t) => t.feature_id === feature.id);
  const values: FormValues = {};
  const mixed = new Set<string>();

  for (const field of allFields("feature")) {
    const seen = new Set<string>();
    for (const t of members) {
      const raw = t as unknown as Record<string, unknown>;
      seen.add(String(toFormValue(field, raw[field.key])));
    }
    if (seen.size <= 1) {
      const only = [...seen][0] ?? "";
      values[field.key] = field.kind === "boolean" ? only === "true" : only;
    } else {
      values[field.key] = field.kind === "boolean" ? false : "";
      mixed.add(field.key);
    }
  }
  return { values, mixed };
}

function toFormValue(field: MetadataField, v: unknown): string | boolean {
  if (field.kind === "boolean") return v === true;
  if (field.kind === "list") {
    return Array.isArray(v) ? v.join(", ") : "";
  }
  if (v === undefined || v === null) return "";
  return String(v);
}

/**
 * Diff edited values against the originals and produce a PATCH body.
 *
 * Only changed keys are included: sending the whole form would rewrite
 * every field on every save, clobbering anything a runner updated
 * concurrently and generating pointless `entry.updated` churn.
 *
 * `skip` names fields to leave alone regardless — used in feature mode for
 * values that started mixed and were never touched.
 */
export function buildPatch(
  initial: FormValues,
  current: FormValues,
  mode: "task" | "feature",
  skip: ReadonlySet<string> = new Set(),
): Record<string, unknown> {
  const patch: Record<string, unknown> = {};

  for (const field of allFields(mode)) {
    if (skip.has(field.key)) continue;
    const before = initial[field.key];
    const after = current[field.key];
    if (before === after) continue;

    if (field.kind === "boolean") {
      patch[field.key] = after === true;
      continue;
    }
    if (field.kind === "list") {
      patch[field.key] = splitList(String(after ?? ""));
      continue;
    }
    patch[field.key] = String(after ?? "");
  }

  return patch;
}

/** Split a comma/newline separated list, dropping blanks. */
export function splitList(raw: string): string[] {
  return raw
    .split(/[,\n]/)
    .map((s) => s.trim())
    .filter(Boolean);
}
