/**
 * Tests for lib/actions/selectionActions — the whole-selection verbs a
 * marked row offers instead of its own menu.
 *
 * Worth pinning: the verbs post requests / clear rather than acting
 * directly (SelectionBar owns the real ladders), none carry a confirm
 * (the bar's dialogs would double-prompt), and delete renders as
 * destructive.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { buildSelectionActions } from "./selectionActions";

function record() {
  const verbs: string[] = [];
  const ctx = {
    count: 3,
    requestVerb: (v: "archive" | "delete") => verbs.push(v),
    clearSelection: () => verbs.push("clear"),
  };
  return { verbs, ctx };
}

test("verbs post requests instead of acting directly", async () => {
  const { verbs, ctx } = record();
  const actions = buildSelectionActions(ctx);
  for (const a of actions) await a.run();
  assert.deepEqual(verbs.sort(), ["archive", "clear", "delete"]);
});

test("labels carry the count; delete is danger; none confirm", () => {
  const actions = buildSelectionActions(record().ctx);
  const byId = new Map(actions.map((a) => [a.id, a]));
  assert.match(byId.get("selection-archive")!.label, /\(3\)/);
  assert.match(byId.get("selection-delete")!.label, /\(3\)/);
  assert.equal(byId.get("selection-delete")!.danger, true);
  for (const a of actions) assert.equal(a.confirm, undefined);
});
