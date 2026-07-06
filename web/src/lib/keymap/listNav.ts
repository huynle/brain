// Shared list-navigation specs/handlers for migrated views — the registry
// counterpart of the legacy handleListNavKey helper. j/k accept vim counts.

import { useNav } from "../../store/nav";
import type { ActionHandlers, ActionSpec, HelpGroupId } from "./types";

export function listNavSpecs(group: HelpGroupId, desc = "Move cursor"): ActionSpec[] {
  return [
    { id: `${group}.list.down`, keys: ["j", "ArrowDown"], desc: `${desc} — down (counts: 5j)`, hint: "Nav", group, countable: true },
    { id: `${group}.list.up`, keys: ["k", "ArrowUp"], desc: `${desc} — up`, group, countable: true },
    { id: `${group}.list.top`, keys: ["g"], desc: "Jump to top", group },
    { id: `${group}.list.bottom`, keys: ["G"], desc: "Jump to bottom", group },
  ];
}

export function listNavHandlers(
  group: HelpGroupId,
  opts: { scope: () => string; count: () => number },
): ActionHandlers {
  const nav = () => useNav.getState();
  return {
    [`${group}.list.down`]: ({ count }) => nav().moveCursor(opts.scope(), count, opts.count()),
    [`${group}.list.up`]: ({ count }) => nav().moveCursor(opts.scope(), -count, opts.count()),
    [`${group}.list.top`]: () => nav().top(opts.scope()),
    [`${group}.list.bottom`]: () => nav().bottom(opts.scope(), opts.count()),
  };
}
