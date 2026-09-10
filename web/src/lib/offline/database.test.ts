import test from "node:test";
import assert from "node:assert/strict";
import sqlite3InitModule from "@sqlite.org/sqlite-wasm";
import { EntryDatabase } from "./database";
import { makeDraft } from "./model";
import type { CachedEntry, Mutation } from "./model";
const sqlite = await sqlite3InitModule();
const seed: CachedEntry = {
  id: "a",
  path: "projects/demo/note/a.md",
  type: "note",
  status: "active",
  title: "Albatross",
  content: "ocean bird",
  revision: "r1",
  raw: "---\ntitle: Albatross\ntype: note\nstatus: active\n---\nocean bird",
  project_id: "demo",
};
function setup(t: { after: (f: () => void) => void }) {
  const db = new sqlite.oo1.DB(":memory:");
  t.after(() => db.close());
  const s = new EntryDatabase(db);
  s.apply({
    epoch: "e",
    cursor: 1,
    more: false,
    changes: [{ path: seed.path, entry: seed, raw: seed.raw }],
  });
  return s;
}
function op(raw: string): Mutation {
  return {
    id: crypto.randomUUID(),
    path: seed.path,
    method: "PATCH",
    revision: seed.revision,
    raw,
    draft: makeDraft(seed, undefined, raw),
  };
}
test("SQLite cache persists a page atomically and searches locally", (t) => {
  const s = setup(t);
  assert.equal(s.state().cursor, 1);
  assert.equal(s.state().ready, true);
  assert.equal(s.list({ query: "ocean" }).length, 1);
  assert.equal(s.list({ query: "missing" }).length, 0);
  assert.equal(s.list({ query: seed.id })[0]?.path, seed.path);
  assert.equal(s.list({ query: seed.path })[0]?.id, seed.id);
  assert.equal(s.list({ project: "other" }).length, 0);
  assert.throws(() =>
    s.apply({ epoch: "other", cursor: 2, more: false, changes: [] }),
  );
  assert.equal(s.state().cursor, 1);
});
test("pending edits survive server updates, resets and deletion", (t) => {
  const s = setup(t);
  const local = op(seed.raw.replace("ocean bird", "offline bird"));
  s.queue(local);
  s.apply({
    epoch: "e",
    cursor: 2,
    more: false,
    changes: [{ path: seed.path, deleted: true }],
  });
  assert.equal(s.get(seed.path)?.content, "offline bird");
  assert.equal(s.get(seed.path, true), null);
  s.reset();
  assert.equal(s.state().pending.length, 1);
  assert.equal(s.state().ready, false);
  assert.equal(s.get(seed.path)?.content, "offline bird");
});
test("unsent edits coalesce but ambiguous sent edits cannot change payload", (t) => {
  const s = setup(t);
  const one = op(seed.raw.replace("ocean bird", "first"));
  s.queue(one);
  const two = op(seed.raw.replace("ocean bird", "second"));
  s.queue(two);
  assert.equal(s.state().pending.length, 1);
  assert.equal(s.get(seed.path)?.content, "second");
  s.mark(two.id);
  assert.throws(() => s.queue(op(seed.raw)), /unconfirmed/);
  assert.equal(s.state().pending[0].id, two.id);
});
test("conflict resolution uses the current server revision and new operation id", (t) => {
  const s = setup(t);
  const local = op(seed.raw.replace("ocean bird", "offline"));
  s.queue(local);
  s.mark(local.id, "conflict");
  s.apply({
    epoch: "e",
    cursor: 2,
    more: false,
    changes: [
      {
        path: seed.path,
        entry: { ...seed, revision: "r2", content: "server" },
        raw: seed.raw.replace("ocean bird", "server"),
      },
    ],
  });
  s.rebase(local.id);
  const rebased = s.state().pending[0];
  assert.equal(rebased.revision, "r2");
  assert.notEqual(rebased.id, local.id);
  assert.equal(rebased.draft.content, "offline");
  assert.equal(rebased.error, undefined);
});
test("acknowledged creates remain addressable by their local path", (t) => {
  const s = setup(t);
  const local = {
    ...op(seed.raw),
    method: "POST" as const,
    path: "local/create",
    draft: { ...seed, path: "local/create" },
  };
  s.queue(local);
  s.acknowledge(local.id, seed);
  assert.equal(s.state().pending.length, 0);
  assert.equal(s.get("local/create")?.path, seed.path);
});

test("a stale editor cannot overwrite another local draft", (t) => {
  const s = setup(t);
  const first = {
    ...op(seed.raw.replace("ocean bird", "first editor")),
    baseLocalID: "",
  };
  s.queue(first);
  const stale = {
    ...op(seed.raw.replace("ocean bird", "stale editor")),
    baseLocalID: "",
  };
  assert.throws(() => s.queue(stale), /another editor/);
  assert.equal(s.get(seed.path)?.content, "first editor");
  const fresh = { ...stale, baseLocalID: first.id };
  s.queue(fresh);
  assert.equal(s.get(seed.path)?.content, "stale editor");
});
test("a rejected create can be corrected without dropping its definition", (t) => {
  const s = setup(t);
  const first = {
    ...op(seed.raw),
    method: "POST" as const,
    path: "local/new",
    draft: { ...seed, path: "local/new" },
  };
  s.queue(first);
  s.mark(first.id, "invalid title", "rejected");
  const corrected = {
    ...first,
    id: crypto.randomUUID(),
    draft: {
      ...first.draft,
      raw: first.draft.raw.replace("Albatross", "Corrected"),
    },
  };
  s.queue(corrected);
  const pending = s.state().pending[0];
  assert.equal(pending.method, "POST");
  assert.equal(pending.body?.title, "Corrected");
  assert.equal(pending.raw, undefined);
  assert.equal(pending.sent, undefined);
});

test("MCP reconciliation is guarded, durable, and idempotent", (t) => {
  const s = setup(t);
  const draft = op(seed.raw.replace("ocean bird", "local edit"));
  s.queue(draft);
  s.mark(draft.id, "Conflict", "conflict");
  const cmd = {
    id: "command",
    operation_id: draft.id,
    action: "merge",
    expected_raw: draft.draft.raw,
    server_revision: "r1",
    raw: seed.raw.replace("ocean bird", "merged edit"),
  };
  assert.equal(
    s.reconcile({ ...cmd, id: "stale", server_revision: "old" }),
    "stale",
  );
  assert.equal(s.state().pending[0].id, draft.id);
  assert.equal(s.reconcile(cmd), "applied_locally");
  const pending = s.state().pending[0];
  assert.notEqual(pending.id, draft.id);
  assert.equal(pending.draft.content, "merged edit");
  assert.equal(pending.error, undefined);
  assert.equal(s.reconcile(cmd), "applied_locally");
  assert.equal(s.state().pending[0].id, pending.id);
  assert.equal(s.deviceInfo().ack_id, "command");
});
test("MCP discard cannot erase a changed draft and uncertain edits cannot rebase", (t) => {
  const s = setup(t);
  const draft = op(seed.raw);
  s.queue(draft);
  s.mark(draft.id, "Uncertain", "uncertain");
  const cmd = {
    id: "command",
    operation_id: draft.id,
    action: "rebase",
    expected_raw: draft.draft.raw,
    server_revision: "r1",
  };
  assert.equal(s.reconcile(cmd), "stale");
  assert.equal(s.state().pending.length, 1);
  assert.equal(
    s.reconcile({
      ...cmd,
      id: "changed",
      action: "discard",
      expected_raw: "old",
    }),
    "stale",
  );
  assert.equal(
    s.reconcile({ ...cmd, id: "discard", action: "discard" }),
    "applied_locally",
  );
  assert.equal(s.state().pending.length, 0);
});

test("MCP rebase preserves the draft and reset preserves device acknowledgement", (t) => {
  const s = setup(t);
  const draft = op(seed.raw.replace("ocean bird", "keep me"));
  s.queue(draft);
  s.mark(draft.id, "Conflict", "conflict");
  const id = s.deviceInfo().id;
  assert.equal(
    s.reconcile({
      id: "keep",
      operation_id: draft.id,
      action: "rebase",
      expected_raw: draft.draft.raw,
      server_revision: "r1",
    }),
    "applied_locally",
  );
  assert.equal(s.state().pending[0].draft.content, "keep me");
  s.syncedAt();
  s.reset();
  assert.equal(s.deviceInfo().id, id);
  assert.equal(s.deviceInfo().ack_id, "keep");
  assert.equal(s.deviceInfo().last_sync, undefined);
});
test("Invalid MCP merged YAML leaves the pending draft intact", (t) => {
  const s = setup(t);
  const draft = op(seed.raw);
  s.queue(draft);
  s.mark(draft.id, "Conflict", "conflict");
  assert.equal(
    s.reconcile({
      id: "invalid",
      operation_id: draft.id,
      action: "merge",
      expected_raw: draft.draft.raw,
      server_revision: "r1",
      raw: "---\ntitle: [\n---\ninvalid",
    }),
    "stale",
  );
  assert.equal(s.state().pending[0].id, draft.id);
  assert.equal(s.get(seed.path)?.raw, seed.raw);
});

test("metadata summary matches scoped lists and includes pending drafts without returning bodies", (t) => {
  const s = setup(t);
  s.apply({
    epoch: "e",
    cursor: 2,
    more: false,
    changes: [
      {
        path: "global/note/g.md",
        entry: {
          ...seed,
          id: "g",
          path: "global/note/g.md",
          project_id: undefined,
          type: "scratch",
        },
        raw: seed.raw,
      },
    ],
  });
  const local = op(seed.raw);
  local.draft = { ...local.draft, type: "task", project_id: "changed" };
  s.queue(local);
  s.queue({
    ...op(seed.raw),
    id: "new",
    method: "POST",
    path: "local/new",
    draft: { ...seed, path: "local/new", id: "new", project_id: "new" },
  });
  for (const q of [
    {},
    { project: "demo" },
    { project: "changed" },
    { global: true },
    { projects: "changed,global" },
  ]) {
    const entries = s.list(q);
    const summary = s.summary(q);
    assert.equal(summary.totalEntries, entries.length);
    assert.deepEqual(
      summary.projects,
      [...new Set(entries.map((e) => e.project_id).filter(Boolean))].sort(),
    );
    assert.deepEqual(
      summary.byType,
      entries.reduce<Record<string, number>>((counts, e) => {
        counts[e.type] = (counts[e.type] ?? 0) + 1;
        return counts;
      }, {}),
    );
    assert.ok(!JSON.stringify(summary).includes("ocean bird"));
  }
});
