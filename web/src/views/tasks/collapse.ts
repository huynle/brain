// Feature-group collapse state for the Tasks view.
//
// Keyed per project scope (a project id or the ALL_PROJECTS sentinel) so each
// project tab keeps its own expand/collapse map — mirroring the TUI's
// persisted featureCollapsed settings. Persisted to localStorage so the state
// survives view switches (the Tasks view unmounts) and page reloads.
//
// Each scope stores a baseline (`default`) plus per-feature overrides. `{`/`}`
// (collapse/expand all) set the baseline and clear overrides so features that
// appear later follow the same choice; toggling one header records an
// override against the baseline.

export interface ScopeCollapse {
  /** Baseline for features without an explicit override. */
  default?: boolean;
  /** Per-feature overrides of the baseline. */
  overrides: Record<string, boolean>;
}

export type CollapseState = Record<string, ScopeCollapse>;

/**
 * Whether a feature group is collapsed. `scopeDefault` is the caller's
 * fallback for scopes with no recorded baseline (ALL_PROJECTS starts
 * collapsed, single projects expanded).
 */
export function isFeatureCollapsed(
  state: CollapseState,
  scope: string,
  feature: string,
  scopeDefault: boolean,
): boolean {
  const s = state[scope];
  return s?.overrides[feature] ?? s?.default ?? scopeDefault;
}

/** Toggle one feature group, recording an override against the baseline. */
export function toggleFeatureCollapsed(
  state: CollapseState,
  scope: string,
  feature: string,
  scopeDefault: boolean,
): CollapseState {
  const next = !isFeatureCollapsed(state, scope, feature, scopeDefault);
  const s = state[scope] ?? { overrides: {} };
  return { ...state, [scope]: { ...s, overrides: { ...s.overrides, [feature]: next } } };
}

/** Collapse or expand every feature group in a scope (the `{`/`}` keys). */
export function setAllCollapsed(
  state: CollapseState,
  scope: string,
  value: boolean,
): CollapseState {
  return { ...state, [scope]: { default: value, overrides: {} } };
}

export function serializeCollapseState(state: CollapseState): string {
  return JSON.stringify(state);
}

/** Defensive parse: anything malformed degrades to empty state, not a crash. */
export function parseCollapseState(raw: string | null): CollapseState {
  if (!raw) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return {};
  const state: CollapseState = {};
  for (const [scope, value] of Object.entries(parsed as Record<string, unknown>)) {
    if (typeof value !== "object" || value === null || Array.isArray(value)) continue;
    const v = value as { default?: unknown; overrides?: unknown };
    const overrides: Record<string, boolean> = {};
    if (typeof v.overrides === "object" && v.overrides !== null && !Array.isArray(v.overrides)) {
      for (const [feature, collapsed] of Object.entries(v.overrides as Record<string, unknown>)) {
        if (typeof collapsed === "boolean") overrides[feature] = collapsed;
      }
    }
    state[scope] = {
      ...(typeof v.default === "boolean" ? { default: v.default } : {}),
      overrides,
    };
  }
  return state;
}

const STORAGE_KEY = "brain.tasks_view.feature_collapse";

export function loadCollapseState(): CollapseState {
  try {
    return parseCollapseState(localStorage.getItem(STORAGE_KEY));
  } catch {
    return {};
  }
}

export function saveCollapseState(state: CollapseState) {
  try {
    localStorage.setItem(STORAGE_KEY, serializeCollapseState(state));
  } catch {
    // Ignore private-mode/quota errors; collapse still works for this session.
  }
}
