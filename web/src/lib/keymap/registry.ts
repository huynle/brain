// Keymap registry: the single source of truth for keyboard bindings.
//
// Scopes register {specs, handlers} at mount (via useActions). The dispatcher
// resolves a Chord against scopes in tier order (pane → view → global), and
// the help surfaces (HelpBar hints, HelpModal groups) derive from the same
// spec tables — a binding cannot drift from its documentation, and a unit
// test asserts no two active specs claim the same chord.
//
// Two-key sequences ("g g") are handled here: when a chord is a prefix of a
// registered sequence, it arms a ~500ms buffer. If the same chord is ALSO a
// complete binding ("g" bound and "g g" bound), the single-chord action fires
// on timeout — vim semantics.
//
// Pure-ish module: no React imports; a tiny subscription surface
// (subscribe/getVersion) lets components re-render on registration changes
// via useSyncExternalStore.

import {
  matchesWhen,
  type ActionCtx,
  type ActionHandlers,
  type ActionSpec,
  type Chord,
  type HelpGroupId,
  type WhenEnv,
} from "./types";

export type ScopeTier = "pane" | "view" | "global";

export interface RegisteredScope {
  scopeId: string;
  tier: ScopeTier;
  specs: ActionSpec[];
  /** Absent for help-only scopes (static documentation groups). */
  handlers?: ActionHandlers;
}

const TIER_ORDER: ScopeTier[] = ["pane", "view", "global"];
const SEQUENCE_TIMEOUT_MS = 500;

let scopes: RegisteredScope[] = [];
let version = 0;
const listeners = new Set<() => void>();

function bump() {
  version += 1;
  for (const fn of listeners) fn();
}

/** Subscription surface for useSyncExternalStore. */
export function subscribeKeymap(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
export function keymapVersion(): number {
  return version;
}

export function registerScope(scope: RegisteredScope): () => void {
  // Replace an existing registration for the same scopeId (deps re-register).
  scopes = scopes.filter((s) => s.scopeId !== scope.scopeId).concat(scope);
  bump();
  let alive = true;
  return () => {
    if (!alive) return;
    alive = false;
    scopes = scopes.filter((s) => s !== scope);
    bump();
  };
}

/** Test/debug helper. */
export function activeScopes(): readonly RegisteredScope[] {
  return scopes;
}

function scopesInTierOrder(tiers: readonly ScopeTier[]): RegisteredScope[] {
  const out: RegisteredScope[] = [];
  for (const tier of TIER_ORDER) {
    if (!tiers.includes(tier)) continue;
    for (const s of scopes) if (s.tier === tier) out.push(s);
  }
  return out;
}

interface Binding {
  scope: RegisteredScope;
  spec: ActionSpec;
  key: Chord;
}

function bindingsFor(chord: Chord, env: WhenEnv, tiers: readonly ScopeTier[]): Binding[] {
  const out: Binding[] = [];
  for (const scope of scopesInTierOrder(tiers)) {
    if (!scope.handlers) continue; // help-only scopes never dispatch
    for (const spec of scope.specs) {
      if (!matchesWhen(spec.when, env)) continue;
      for (const key of spec.keys) {
        if (key === chord) out.push({ scope, spec, key });
      }
    }
  }
  return out;
}

function sequencePrefixExists(chord: Chord, env: WhenEnv, tiers: readonly ScopeTier[]): boolean {
  for (const scope of scopesInTierOrder(tiers)) {
    if (!scope.handlers) continue;
    for (const spec of scope.specs) {
      if (!matchesWhen(spec.when, env)) continue;
      for (const key of spec.keys) {
        if (key.includes(" ") && key.split(" ")[0] === chord) return true;
      }
    }
  }
  return false;
}

function runBindings(chord: Chord, ctx: ActionCtx, env: WhenEnv, tiers: readonly ScopeTier[]): boolean {
  for (const b of bindingsFor(chord, env, tiers)) {
    const handler = b.scope.handlers?.[b.spec.id];
    if (!handler) continue;
    if (handler(ctx) !== false) return true; // false = fall through
  }
  return false;
}

// ---------------------------------------------------------------------------
// Sequence buffer ("g g")
// ---------------------------------------------------------------------------

interface PendingSequence {
  prefix: Chord;
  ctx: ActionCtx;
  env: WhenEnv;
  tiers: readonly ScopeTier[];
  timer: ReturnType<typeof setTimeout>;
}

let pending: PendingSequence | null = null;

function clearPending() {
  if (pending) {
    clearTimeout(pending.timer);
    pending = null;
  }
}

/** Test helper: cancel any armed sequence. */
export function resetSequence(): void {
  clearPending();
}

/**
 * dispatchChord resolves a chord against the active scopes.
 * Returns true when consumed (including arming a sequence).
 */
export function dispatchChord(
  chord: Chord,
  ctx: ActionCtx,
  env: WhenEnv,
  tiers: readonly ScopeTier[] = TIER_ORDER,
): boolean {
  // Complete an armed sequence first.
  if (pending) {
    const seq = `${pending.prefix} ${chord}`;
    const armed = pending;
    clearPending();
    if (runBindings(seq, ctx, armed.env, armed.tiers)) return true;
    // Sequence broken: the second chord dispatches normally below (vim: "g"
    // then "x" does nothing for the g, x proceeds).
  }

  const hasDirect = bindingsFor(chord, env, tiers).length > 0;
  const isPrefix = sequencePrefixExists(chord, env, tiers);

  if (isPrefix) {
    // Arm the sequence. If the chord is also a complete binding, fire it on
    // timeout instead of dropping it (vim's 'g' vs 'gg' behavior).
    const armedCtx = ctx;
    const armedEnv = env;
    const armedTiers = tiers;
    pending = {
      prefix: chord,
      ctx: armedCtx,
      env: armedEnv,
      tiers: armedTiers,
      timer: setTimeout(() => {
        pending = null;
        if (hasDirect) runBindings(chord, armedCtx, armedEnv, armedTiers);
      }, SEQUENCE_TIMEOUT_MS),
    };
    return true;
  }

  return runBindings(chord, ctx, env, tiers);
}

/** True when the chord maps to a countable action in the current env. */
export function isCountable(chord: Chord, env: WhenEnv): boolean {
  return bindingsFor(chord, env, TIER_ORDER).some((b) => b.spec.countable);
}

// ---------------------------------------------------------------------------
// Help derivation
// ---------------------------------------------------------------------------

export interface HelpHint {
  keys: Chord[];
  hint: string;
  tier: ScopeTier;
}

/**
 * HelpBar hints: active hinted specs, pane+view first, then global. The
 * tier lets the HelpBar decide whether the active view has migrated (any
 * view-tier hints) or should fall back to its legacy static table.
 */
export function helpBarHints(env: WhenEnv): HelpHint[] {
  if (env.isMobile) return [];
  const out: HelpHint[] = [];
  const seen = new Set<string>();
  for (const scope of scopesInTierOrder(TIER_ORDER)) {
    for (const spec of scope.specs) {
      if (!spec.hint || seen.has(spec.id)) continue;
      if (!matchesWhen(spec.when, env)) continue;
      seen.add(spec.id);
      out.push({ keys: spec.keys, hint: spec.hint, tier: scope.tier });
    }
  }
  return out;
}

export interface HelpGroup {
  id: HelpGroupId | string;
  title: string;
  rows: { keys: Chord[]; desc: string }[];
}

const GROUP_TITLES: Record<string, string> = {
  tasks: "Tasks",
  brain: "Brain",
  automations: "Automations",
  runners: "Runners",
  control: "Control",
  logs: "Logs (server requests)",
  global: "Global",
  lists: "Lists (all tabs)",
  panes: "Detail / Logs panes",
  popups: "Popups / sheets",
};

/**
 * helpModalGroups collects every registered spec (help-only scopes included)
 * grouped by HelpGroupId, current view's group first.
 */
export function helpModalGroups(currentView: string): HelpGroup[] {
  const byGroup = new Map<string, HelpGroup>();
  for (const scope of scopes) {
    for (const spec of scope.specs) {
      const g = byGroup.get(spec.group) ?? {
        id: spec.group,
        title: GROUP_TITLES[spec.group] ?? spec.group,
        rows: [],
      };
      g.rows.push({ keys: spec.keys, desc: spec.desc });
      byGroup.set(spec.group, g);
    }
  }
  const ordered: HelpGroup[] = [];
  const current = byGroup.get(currentView);
  if (current) ordered.push(current);
  for (const id of ["global", "lists", "panes", "popups"]) {
    const g = byGroup.get(id);
    if (g && g !== current) ordered.push(g);
  }
  for (const [id, g] of byGroup) {
    if (!ordered.includes(g) && id !== currentView) ordered.push(g);
  }
  return ordered;
}

/**
 * findDuplicateBindings reports chords claimed by more than one dispatchable
 * spec within the same tier whose `when` clauses can overlap. Used by a CI
 * test — a nonempty result means an ambiguous binding shipped.
 */
export function findDuplicateBindings(): string[] {
  const problems: string[] = [];
  for (const tier of TIER_ORDER) {
    const claims = new Map<string, { spec: ActionSpec; scopeId: string }[]>();
    for (const scope of scopes) {
      if (scope.tier !== tier || !scope.handlers) continue;
      for (const spec of scope.specs) {
        for (const key of spec.keys) {
          const list = claims.get(key) ?? [];
          list.push({ spec, scopeId: scope.scopeId });
          claims.set(key, list);
        }
      }
    }
    for (const [key, list] of claims) {
      if (list.length < 2) continue;
      for (let i = 0; i < list.length; i++) {
        for (let j = i + 1; j < list.length; j++) {
          const a = list[i];
          const b = list[j];
          if (a.scopeId !== b.scopeId && whenCanOverlap(a.spec, b.spec)) {
            problems.push(`${tier}:"${key}" claimed by ${a.spec.id} (${a.scopeId}) and ${b.spec.id} (${b.scopeId})`);
          }
        }
      }
    }
  }
  return problems;
}

function whenCanOverlap(a: ActionSpec, b: ActionSpec): boolean {
  const wa = a.when;
  const wb = b.when;
  if (!wa || !wb) return true;
  if (wa.focus && wb.focus && !wa.focus.some((f) => wb.focus!.includes(f))) return false;
  if (wa.mode !== undefined && wb.mode !== undefined && wa.mode !== wb.mode) return false;
  if (wa.hasSelection !== undefined && wb.hasSelection !== undefined && wa.hasSelection !== wb.hasSelection) return false;
  return true;
}
