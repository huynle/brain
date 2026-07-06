// Keymap core types: normalized chords + declarative action specs.
//
// This module is the single source of truth's vocabulary: views declare
// ActionSpec tables (static, importable without mounting the view), the
// registry dispatches by Chord, and HelpBar/HelpModal render from the same
// specs — so a binding can no longer drift from its documentation.
//
// Pure module: no React, no stores (node:test-able).

import type { Panel } from "../paneNav";

/**
 * Normalized chord string.
 *
 * - Modifiers in fixed order: "C-" (ctrl), "M-" (meta/cmd), "A-" (alt/option).
 * - Shift folds into printable characters ("G", "?", "{"); explicit "S-" only
 *   for non-printables ("S-Tab").
 * - Alt chords normalize the key from e.code for letters (macOS Option+J
 *   yields key "∆" but code "KeyJ" → "A-j").
 * - Two-key sequences are space-separated ("g g"); chordOf always returns a
 *   single chord — sequences are assembled by the dispatcher.
 */
export type Chord = string;

export type HelpGroupId =
  | "tasks"
  | "brain"
  | "automations"
  | "runners"
  | "control"
  | "logs"
  | "global"
  | "lists"
  | "panes"
  | "popups";

/** Declarative availability — data, not code, so help UIs can filter it. */
export interface WhenClause {
  /** Only when one of these panes has focus. */
  focus?: Panel[];
  /** View-defined sub-mode, e.g. "tasks" | "schedules" | "done". */
  mode?: string;
  /** Only when the selection is (non-)empty. */
  hasSelection?: boolean;
}

export interface ActionSpec {
  /** Stable id, "view.verb": "tasks.complete", "global.command". */
  id: string;
  keys: Chord[];
  /** Full sentence for the help modal. */
  desc: string;
  /** Short HelpBar label ("Done"); omitted = not shown in the HelpBar. */
  hint?: string;
  group: HelpGroupId;
  when?: WhenClause;
  /** Accepts a vim count prefix (5j). */
  countable?: boolean;
}

export interface ActionCtx {
  event: KeyboardEvent;
  /** Vim count prefix, default 1. */
  count: number;
}

/**
 * Dynamic handlers, keyed by ActionSpec.id — closures over view state,
 * supplied at mount. Returning false means "not applicable right now, fall
 * through" (runtime escape hatch beyond the declarative `when`).
 */
export type ActionHandlers = Record<string, (ctx: ActionCtx) => boolean | void>;

export interface WhenEnv {
  focus: Panel;
  mode?: string;
  hasSelection: boolean;
  isMobile: boolean;
}

/** Keys whose shift-ness is expressed by the printable character itself. */
function isPrintable(key: string): boolean {
  return key.length === 1;
}

/** Map KeyboardEvent.code to a base character for Alt-chord normalization. */
function codeToChar(code: string): string | null {
  if (/^Key[A-Z]$/.test(code)) return code.slice(3).toLowerCase();
  if (/^Digit[0-9]$/.test(code)) return code.slice(5);
  return null;
}

/**
 * chordOf normalizes a KeyboardEvent into a Chord. Returns null for pure
 * modifier presses (Shift, Control, ...).
 */
export function chordOf(e: Pick<KeyboardEvent, "key" | "code" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey">): Chord | null {
  if (e.key === "Shift" || e.key === "Control" || e.key === "Meta" || e.key === "Alt") return null;

  let key = e.key;
  // Alt composes characters on macOS (Option+J → "∆"); recover the physical
  // key from e.code so "A-j" means the same thing on every platform.
  if (e.altKey) {
    const fromCode = codeToChar(e.code);
    if (fromCode) key = e.shiftKey ? fromCode.toUpperCase() : fromCode;
  }

  let chord = "";
  if (e.ctrlKey) chord += "C-";
  if (e.metaKey) chord += "M-";
  if (e.altKey) chord += "A-";
  if (e.shiftKey && !isPrintable(key)) chord += "S-";
  // Ctrl/Meta chords fold letters to lowercase so "C-d" matches with or
  // without caps-lock interference; bare printables keep their case (shift
  // folding: "G" vs "g").
  if ((e.ctrlKey || e.metaKey) && isPrintable(key)) key = key.toLowerCase();
  // The space key is spelled out — a literal " " would collide with the
  // sequence separator ("g g").
  if (key === " ") key = "Space";
  return chord + key;
}

export function matchesWhen(w: WhenClause | undefined, env: WhenEnv): boolean {
  if (!w) return true;
  if (w.focus && !w.focus.includes(env.focus)) return false;
  if (w.mode !== undefined && w.mode !== env.mode) return false;
  if (w.hasSelection !== undefined && w.hasSelection !== env.hasSelection) return false;
  return true;
}

const PRETTY_KEY: Record<string, string> = {
  ArrowUp: "↑",
  ArrowDown: "↓",
  ArrowLeft: "←",
  ArrowRight: "→",
  Backspace: "⌫",
  Escape: "Esc",
};

/** Human form for help UIs: "C-d" → "Ctrl-D", "g g" → "gg", "M-." → "⌘.". */
export function prettyChord(chord: Chord): string {
  const parts = chord.split(" ");
  if (parts.length > 1 && parts.every((p) => p.length === 1)) return parts.join("");
  return parts
    .map((part) => {
      let rest = part;
      let out = "";
      for (;;) {
        if (rest.startsWith("C-")) {
          out += "Ctrl-";
          rest = rest.slice(2);
        } else if (rest.startsWith("M-")) {
          out += "⌘";
          rest = rest.slice(2);
        } else if (rest.startsWith("A-")) {
          out += "Alt-";
          rest = rest.slice(2);
        } else if (rest.startsWith("S-")) {
          out += "Shift-";
          rest = rest.slice(2);
        } else {
          break;
        }
      }
      const key = PRETTY_KEY[rest] ?? (out && rest.length === 1 ? rest.toUpperCase() : rest);
      return out + key;
    })
    .join(" ");
}
