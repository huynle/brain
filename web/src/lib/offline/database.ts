import type { Database } from "@sqlite.org/sqlite-wasm";
import { makeDraft, matches, rawParts } from "./model";
import type { CachedEntry, ChangePage, Mutation, SyncState } from "./model";

// This class is shared by the browser worker and real SQLite unit tests.
export class EntryDatabase {
  private db: Database;
  constructor(db: Database) {
    this.db = db;
    db.exec(`CREATE TABLE IF NOT EXISTS entries(path TEXT PRIMARY KEY, data TEXT NOT NULL);
   CREATE INDEX IF NOT EXISTS entries_type ON entries(json_extract(data,'$.type'));
   CREATE TABLE IF NOT EXISTS state(key TEXT PRIMARY KEY, value TEXT NOT NULL);
   CREATE TABLE IF NOT EXISTS outbox(path TEXT PRIMARY KEY, data TEXT NOT NULL);
   CREATE VIRTUAL TABLE IF NOT EXISTS entry_search USING fts5(path UNINDEXED,title,content);`);
  }
  private rows(
    sql: string,
    bind: (string | number)[] = [],
  ): Record<string, unknown>[] {
    return this.db.exec({
      sql,
      bind,
      rowMode: "object",
      returnValue: "resultRows",
    }) as Record<string, unknown>[];
  }
  private set(key: string, value: unknown) {
    this.db.exec({
      sql: "INSERT OR REPLACE INTO state VALUES (?,?)",
      bind: [key, JSON.stringify(value)],
    });
  }
  private transaction<T>(f: () => T): T {
    this.db.exec("BEGIN IMMEDIATE");
    try {
      const result = f();
      this.db.exec("COMMIT");
      return result;
    } catch (e) {
      this.db.exec("ROLLBACK");
      throw e;
    }
  }
  state(): SyncState {
    const meta = Object.fromEntries(
      this.rows("SELECT * FROM state").map((r) => [
        r.key,
        JSON.parse(String(r.value)),
      ]),
    );
    return {
      epoch: meta.epoch ?? "",
      cursor: meta.cursor ?? 0,
      ready: meta.ready ?? false,
      pending: this.rows("SELECT data FROM outbox ORDER BY rowid").map((r) =>
        JSON.parse(String(r.data)),
      ),
    };
  }
  private store(entry: CachedEntry) {
    this.db.exec({
      sql: "INSERT OR REPLACE INTO entries VALUES (?,?)",
      bind: [entry.path, JSON.stringify(entry)],
    });
    this.db.exec({
      sql: "DELETE FROM entry_search WHERE path=?",
      bind: [entry.path],
    });
    this.db.exec({
      sql: "INSERT INTO entry_search VALUES (?,?,?)",
      bind: [entry.path, entry.title, entry.content],
    });
  }
  apply(page: ChangePage) {
    return this.transaction(() => {
      const old = this.state();
      if (old.epoch && old.epoch !== page.epoch)
        throw new Error(
          "Reset required before applying a different database epoch",
        );
      for (const c of page.changes) {
        if (c.deleted) {
          this.db.exec({
            sql: "DELETE FROM entries WHERE path=?",
            bind: [c.path],
          });
          this.db.exec({
            sql: "DELETE FROM entry_search WHERE path=?",
            bind: [c.path],
          });
        } else if (c.entry)
          this.store({
            ...c.entry,
            raw: c.raw ?? "",
            revision: c.entry.revision ?? "",
          });
      }
      this.set("epoch", page.epoch);
      this.set("cursor", page.cursor);
      if (!page.more) this.set("ready", true);
    });
  }
  reset() {
    this.transaction(() => {
      const device = this.deviceInfo();
      delete device.last_sync;
      this.set("device", device);
      // A rebuilt server database may have lost mutation receipts. Never
      // replay a possibly committed create merely because its epoch changed.
      for (const op of this.state().pending) {
        if (op.sent)
          this.mark(
            op.id,
            "Server database changed while this edit was unconfirmed. Compare with the server before retrying.",
            "uncertain",
          );
      }
      this.db.exec(
        "DELETE FROM entries; DELETE FROM entry_search; DELETE FROM state WHERE key != 'device';",
      );
    });
  }
  get(path: string, server = false): CachedEntry | null {
    if (!server) {
      const pending = this.state().pending.find(
        (o) => o.path === path || o.draft.id === path,
      );
      if (pending) return pending.draft;
    }
    const alias = this.rows("SELECT value FROM state WHERE key=?", [
      "alias:" + path,
    ])[0];
    if (alias) path = JSON.parse(String(alias.value));
    const row = this.rows(
      "SELECT data FROM entries WHERE path=? OR json_extract(data,'$.id')=? LIMIT 1",
      [path, path],
    )[0];
    return row ? JSON.parse(String(row.data)) : null;
  }
  list(q: Record<string, unknown> = {}): CachedEntry[] {
    let rows: Record<string, unknown>[];
    const term = String(q.query ?? "").trim();
    if (term) {
      // Quote user tokens; never interpret user input as FTS operators.
      const expression = term
        .split(/\s+/)
        .map((t) => '"' + t.replaceAll('"', '""') + '"*')
        .join(" AND ");
      rows = this.rows(
        "SELECT data FROM entries WHERE path IN (SELECT path FROM entry_search WHERE entry_search MATCH ?) OR json_extract(data,'$.id')=? OR path=?",
        [expression, term, term],
      );
    } else
      rows = q.type
        ? this.rows(
            "SELECT data FROM entries WHERE json_extract(data,'$.type')=?",
            [String(q.type)],
          )
        : this.rows("SELECT data FROM entries");
    const byPath = new Map<string, CachedEntry>(
      rows.map((r) => {
        const e = JSON.parse(String(r.data));
        return [e.path, e];
      }),
    );
    for (const op of this.state().pending) {
      byPath.delete(op.path);
      if (
        !term ||
        op.draft.id === term ||
        op.path === term ||
        term
          .toLowerCase()
          .split(/\s+/)
          .every((t) =>
            (op.draft.title + " " + op.draft.content).toLowerCase().includes(t),
          )
      )
        byPath.set(op.path, op.draft);
    }
    const entries = [...byPath.values()].filter(
      (e) =>
        matches(e, q) &&
        (!q.editorFilter ||
          (e.title + " " + e.type + " " + e.project_id)
            .toLowerCase()
            .includes(String(q.editorFilter).toLowerCase())),
    );
    const key = String(q.sortBy ?? "modified");
    const direction = q.sortOrder === "asc" ? 1 : -1;
    const sortValue = (entry: CachedEntry) => {
      const fields = entry as unknown as Record<string, unknown>;
      return String(
        key === "completed"
          ? fields.completed_at || entry.modified || ""
          : (fields[key] ?? ""),
      );
    };
    entries.sort(
      (a, b) =>
        sortValue(a).localeCompare(sortValue(b)) * direction ||
        a.path.localeCompare(b.path),
    );
    return q.editorLimit ? entries.slice(0, Number(q.editorLimit)) : entries;
  }
  queue(op: Mutation) {
    return this.transaction(() => {
      const existing = this.state().pending.find((p) => p.path === op.path);
      if (
        existing &&
        op.baseLocalID !== undefined &&
        op.baseLocalID !== existing.id
      ) {
        throw new Error(
          "This draft changed in another editor. Reopen the entry before saving.",
        );
      }
      if (
        existing?.sent &&
        existing.failure !== "rejected" &&
        existing.failure !== "conflict"
      )
        throw new Error(
          "This entry has an unconfirmed edit. Resolve it in Offline sync before saving again.",
        );
      if (existing) {
        // Coalesce only edits never sent. Rebase against the original revision.
        op.revision = existing.revision;
        op.draft.revision = existing.revision;
        op.method = existing.method;
        if (op.method === "PATCH") {
          op.raw = op.draft.raw;
          delete op.body;
        } else {
          const parts = rawParts(op.draft.raw);
          op.body = { ...parts.fields, content: parts.content };
          delete op.raw;
        }
      }
      op.draft.local_revision = op.id;
      this.db.exec({
        sql: "INSERT OR REPLACE INTO outbox VALUES (?,?)",
        bind: [op.path, JSON.stringify(op)],
      });
      return op.draft;
    });
  }
  mark(id: string, error?: string, failure?: Mutation["failure"]) {
    const op = this.state().pending.find((p) => p.id === id);
    if (!op) return;
    op.sent = true;
    op.error = error;
    op.failure = failure;
    this.db.exec({
      sql: "UPDATE outbox SET data=? WHERE path=?",
      bind: [JSON.stringify(op), op.path],
    });
    return op;
  }
  discard(id: string) {
    this.db.exec({
      sql: "DELETE FROM outbox WHERE json_extract(data,'$.id')=?",
      bind: [id],
    });
  }
  acknowledge(id: string, entry?: CachedEntry) {
    this.transaction(() => {
      if (entry) {
        this.store(entry);
        const op = this.state().pending.find((p) => p.id === id);
        if (op && op.path !== entry.path)
          this.set("alias:" + op.path, entry.path);
      }
      this.discard(id);
    });
  }
  deviceInfo() {
    let row = this.rows("SELECT value FROM state WHERE key='device'")[0];
    if (!row) {
      this.set("device", { id: crypto.randomUUID() });
      row = this.rows("SELECT value FROM state WHERE key='device'")[0];
    }
    return JSON.parse(String(row.value)) as {
      id: string;
      ack_id?: string;
      ack_outcome?: string;
      last_sync?: string;
    };
  }
  syncedAt() {
    this.set("device", {
      ...this.deviceInfo(),
      last_sync: new Date().toISOString(),
    });
  }
  reconcile(command: {
    id: string;
    operation_id: string;
    action: string;
    expected_raw: string;
    server_revision: string;
    raw?: string;
  }) {
    return this.transaction(() => {
      const device = this.deviceInfo();
      if (device.ack_id === command.id) return device.ack_outcome;
      const op = this.state().pending.find(
        (p) => p.id === command.operation_id,
      );
      const current = op ? this.get(op.path, true) : null;
      let outcome = "stale";
      if (
        op?.error &&
        op.draft.raw === command.expected_raw &&
        (current?.revision ?? "") === command.server_revision
      ) {
        if (command.action === "discard") {
          this.discard(op.id);
          outcome = "applied_locally";
        } else if (
          (command.action === "merge" || command.action === "rebase") &&
          op.method === "PATCH" &&
          op.failure !== "uncertain" &&
          current
        ) {
          // Validate and replace in the same transaction as the durable command receipt.
          op.id = crypto.randomUUID();
          op.revision = current.revision;
          op.raw = command.action === "merge" ? command.raw : op.draft.raw;
          if (op.raw === undefined)
            throw new Error("Merged definition required");
          try {
            op.draft = makeDraft(current, undefined, op.raw);
          } catch {
            this.set("device", {
              ...device,
              ack_id: command.id,
              ack_outcome: "stale",
            });
            return "stale";
          }
          op.draft.local_revision = op.id;
          delete op.body;
          delete op.error;
          delete op.failure;
          op.sent = false;
          this.db.exec({
            sql: "UPDATE outbox SET data=? WHERE path=?",
            bind: [JSON.stringify(op), op.path],
          });
          outcome = "applied_locally";
        }
      }
      this.set("device", {
        ...device,
        ack_id: command.id,
        ack_outcome: outcome,
      });
      return outcome;
    });
  }
  rebase(id: string) {
    return this.transaction(() => {
      const op = this.state().pending.find((p) => p.id === id);
      if (!op) return;
      const current = this.get(op.path, true);
      if (!current)
        throw new Error(
          "Server entry was deleted. Export your draft before discarding it.",
        );
      op.id = crypto.randomUUID();
      op.revision = current.revision;
      op.sent = false;
      delete op.error;
      delete op.failure;
      op.draft = makeDraft(current, undefined, op.draft.raw);
      op.draft.local_revision = op.id;
      op.raw = op.draft.raw;
      delete op.body;
      this.db.exec({
        sql: "UPDATE outbox SET data=? WHERE path=?",
        bind: [JSON.stringify(op), op.path],
      });
    });
  }
}
