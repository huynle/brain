/**
 * Tests for lib/actions/entryActions.
 *
 * The things worth pinning: archive/unarchive render as one status-aware
 * slot, delete demands typing the entry's short id, the "link" surface
 * (reader graph footer) carries only the reference verbs, and the pin
 * verb never takes the id "select" that useRowActions special-cases on
 * long-press.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildEntryActions,
  entryName,
  entrySlug,
  type EntryActionContext,
  type EntryActionOptions,
  type EntryActionTarget,
} from "./entryActions";

function mkEntry(over: Partial<EntryActionTarget> = {}): EntryActionTarget {
  return {
    path: "projects/brain-api/plan/ab12cd34.md",
    id: "ab12cd34",
    title: "Auth rollout plan",
    status: "active",
    ...over,
  };
}

function recorder() {
  const calls: string[] = [];
  const ctx: EntryActionContext = {
    openEntry: (e) => void calls.push(`open:${e.path}`),
    togglePin: (e) => void calls.push(`pin:${e.path}`),
    copyPath: async (e) => void calls.push(`copy:${e.path}`),
    setEntryStatus: async (e, status) =>
      void calls.push(`status:${e.path}:${status}`),
    deleteEntry: async (e) => void calls.push(`delete:${e.path}`),
  };
  return { calls, ctx };
}

function byId(
  entry: EntryActionTarget,
  ctx: EntryActionContext,
  opts: EntryActionOptions = { pinned: false },
) {
  return new Map(buildEntryActions(entry, opts, ctx).map((a) => [a.id, a]));
}

// ─── presence ──────────────────────────────────────────────────────

test("browser surface carries the full verb set", () => {
  const { ctx } = recorder();
  const ids = buildEntryActions(mkEntry(), { pinned: false }, ctx).map(
    (a) => a.id,
  );
  assert.deepEqual(ids, ["pin", "archive", "open", "copy-path", "delete"]);
});

test("link surface carries only open + pin + copy", () => {
  const { ctx } = recorder();
  const ids = buildEntryActions(
    mkEntry(),
    { pinned: false, surface: "link" },
    ctx,
  ).map((a) => a.id);
  assert.deepEqual(ids, ["pin", "open", "copy-path"]);
});

test("no verb takes the id 'select' useRowActions long-press special-cases", () => {
  const { ctx } = recorder();
  for (const surface of [undefined, "link"] as const) {
    const ids = buildEntryActions(
      mkEntry(),
      { pinned: false, surface },
      ctx,
    ).map((a) => a.id);
    assert.ok(!ids.includes("select"), `surface=${surface ?? "browser"}`);
  }
});

// ─── archive/unarchive slot ────────────────────────────────────────

test("non-archived entry shows Archive, not Unarchive", () => {
  const { ctx } = recorder();
  for (const status of ["active", "draft", "completed", "superseded"]) {
    const actions = byId(mkEntry({ status }), ctx);
    assert.ok(actions.has("archive"), `status=${status}`);
    assert.ok(!actions.has("unarchive"), `status=${status}`);
  }
});

test("archived entry shows Unarchive, not Archive", () => {
  const { ctx } = recorder();
  const actions = byId(mkEntry({ status: "archived" }), ctx);
  assert.ok(actions.has("unarchive"));
  assert.ok(!actions.has("archive"));
});

test("archive flips to archived; unarchive restores active", async () => {
  const { calls, ctx } = recorder();
  await byId(mkEntry({ status: "active" }), ctx).get("archive")!.run();
  await byId(mkEntry({ status: "archived" }), ctx).get("unarchive")!.run();
  assert.deepEqual(calls, [
    "status:projects/brain-api/plan/ab12cd34.md:archived",
    "status:projects/brain-api/plan/ab12cd34.md:active",
  ]);
});

test("archive is a plain flip — no confirm dialog", () => {
  const { ctx } = recorder();
  assert.equal(byId(mkEntry(), ctx).get("archive")!.confirm, undefined);
});

// ─── pin label ─────────────────────────────────────────────────────

test("pin label reflects the pinned state", () => {
  const { ctx } = recorder();
  assert.equal(
    byId(mkEntry(), ctx, { pinned: false }).get("pin")!.label,
    "Pin for compare",
  );
  assert.equal(
    byId(mkEntry(), ctx, { pinned: true }).get("pin")!.label,
    "Unpin from compare",
  );
});

// ─── delete confirm ────────────────────────────────────────────────

test("delete requires typing the entry id", () => {
  const { ctx } = recorder();
  const del = byId(mkEntry(), ctx).get("delete")!;
  assert.ok(del.danger);
  assert.equal(del.confirm?.typeToConfirm, "ab12cd34");
  assert.equal(del.confirm?.confirmLabel, "Delete permanently");
});

test("delete falls back to the path basename when the surface has no id", () => {
  const { ctx } = recorder();
  const del = byId(
    mkEntry({ id: undefined, path: "global/quirk/ff00aa11.md" }),
    ctx,
  ).get("delete")!;
  assert.equal(del.confirm?.typeToConfirm, "ff00aa11");
});

// ─── identity helpers ──────────────────────────────────────────────

test("entrySlug prefers the id, falls back to the basename", () => {
  assert.equal(entrySlug(mkEntry()), "ab12cd34");
  assert.equal(
    entrySlug({ path: "projects/x/report/deadbeef.md" }),
    "deadbeef",
  );
});

test("entryName prefers the title, falls back to the slug", () => {
  assert.equal(entryName(mkEntry()), "Auth rollout plan");
  assert.equal(entryName(mkEntry({ title: undefined })), "ab12cd34");
});

// ─── routing ───────────────────────────────────────────────────────

test("each verb routes to its context effect", async () => {
  const { calls, ctx } = recorder();
  const actions = byId(mkEntry(), ctx);
  await actions.get("open")!.run();
  await actions.get("pin")!.run();
  await actions.get("copy-path")!.run();
  await actions.get("delete")!.run();
  assert.deepEqual(calls, [
    "open:projects/brain-api/plan/ab12cd34.md",
    "pin:projects/brain-api/plan/ab12cd34.md",
    "copy:projects/brain-api/plan/ab12cd34.md",
    "delete:projects/brain-api/plan/ab12cd34.md",
  ]);
});
