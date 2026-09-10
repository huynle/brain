import { create } from "zustand";
import { stringify } from "yaml";
import { api, ApiError } from "../api";
import { useAuth } from "../auth";
import { cacheScope, database, databaseFor } from "./client";
import { makeDraft } from "./model";
import type { CachedEntry, ChangePage, Mutation, SyncState } from "./model";

export const useOffline = create<{
  editPath: string | null;
  ready: boolean;
  syncing: boolean;
  online: boolean;
  error: string | null;
  reportingError: string | null;
  pending: Mutation[];
  generation: number;
}>(() => ({
  editPath: null,
  ready: false,
  syncing: false,
  online: true,
  error: null,
  reportingError: null,
  pending: [],
  generation: 0,
}));
let active: Promise<void> | undefined;
let lastStateFingerprint = "";
const channel =
  typeof window !== "undefined" && typeof BroadcastChannel !== "undefined"
    ? new BroadcastChannel("brain-entry-sync")
    : null;
channel?.addEventListener("message", () => {
  void refreshState().catch((e) => useOffline.setState({ error: String(e) }));
});
export const offlineAvailable = () =>
  typeof window !== "undefined" &&
  typeof Worker !== "undefined" &&
  typeof navigator !== "undefined" &&
  !!navigator.locks &&
  !!navigator.storage?.getDirectory &&
  ["authenticated", "anonymous"].includes(useAuth.getState().status);
export async function refreshState() {
  if (!offlineAvailable()) return;
  const scope = cacheScope();
  const state = await databaseFor<SyncState>(scope, "state");
  if (!offlineAvailable() || cacheScope() !== scope) return;
  const fingerprint = JSON.stringify([scope, state]);
  const changed = fingerprint !== lastStateFingerprint;
  lastStateFingerprint = fingerprint;
  useOffline.setState((s) => ({
    ready: state.ready,
    pending: state.pending,
    generation: s.generation + (changed ? 1 : 0),
  }));
}
async function pull(db: typeof database = database, scope = cacheScope()) {
  let more = true;
  while (more) {
    if (scope !== cacheScope() || !offlineAvailable())
      throw new Error("Account changed during sync");
    const s = await db<SyncState>("state");
    let page: ChangePage;
    try {
      page = await api<ChangePage>("/api/v1/sync/entries", {
        query: { epoch: s.epoch, cursor: s.cursor },
        signal: AbortSignal.timeout(15_000),
      });
    } catch (e) {
      if (e instanceof ApiError && e.status === 410) {
        await db("reset");
        continue;
      }
      throw e;
    }
    await db("apply", page);
    more = page.more;
  }
}
// Drafts are shared with this server's administrators for MCP conflict review.
// Reports are best effort: reporting failure must never prevent normal entry sync.
async function reportDevice(
  db: typeof database,
  scope: string,
  applyCommands: boolean,
) {
  if (scope !== cacheScope() || !offlineAvailable()) return;
  try {
    const state = await db<SyncState>("state");
    const device = await db<{
      id: string;
      ack_id?: string;
      ack_outcome?: string;
      last_sync?: string;
    }>("deviceInfo");
    const ui = useOffline.getState();
    const result = await api<{
      command: {
        id: string;
        operation_id: string;
        action: string;
        expected_raw: string;
        server_revision: string;
        raw?: string;
        outcome?: string;
      } | null;
    }>(`/api/v1/sync/devices/${device.id}/report`, {
      method: "POST",
      signal: AbortSignal.timeout(5000),
      body: {
        reported_online: ui.online,
        syncing: ui.syncing,
        ready: state.ready,
        cursor: state.cursor,
        epoch: state.epoch,
        error: ui.error ?? "",
        last_successful_sync: device.last_sync,
        ack_id: device.ack_id,
        ack_outcome: device.ack_outcome,
        pending: state.pending.map((p) => ({
          id: p.id,
          path: p.path,
          method: p.method,
          revision: p.revision,
          raw: p.draft.raw,
          error: p.error,
          failure: p.failure,
        })),
      },
    });
    if (scope === cacheScope()) useOffline.setState({ reportingError: null });
    if (
      applyCommands &&
      result.command &&
      !result.command.outcome &&
      scope === cacheScope() &&
      offlineAvailable()
    ) {
      try {
        await db("reconcile", result.command);
      } catch {
        /* Invalid merged YAML leaves the original draft untouched. */
      }
    }
  } catch (e) {
    if (scope === cacheScope())
      useOffline.setState({
        reportingError: e instanceof Error ? e.message : String(e),
      });
  }
}
export async function syncNow(): Promise<void> {
  if (!offlineAvailable()) return;
  if (active) return active;
  const scope = cacheScope();
  const db: typeof database = (method, ...args) =>
    databaseFor(scope, method, ...args);
  active = (async () => {
    await navigator.locks.request("brain-entry-sync-" + scope, async () => {
      useOffline.setState({ syncing: true, error: null });
      try {
        await pull(db, scope);
        useOffline.setState({ online: true });
        await reportDevice(db, scope, true);
        const { pending } = await db<SyncState>("state");
        for (const op of pending) {
          if (cacheScope() !== scope || !offlineAvailable()) return;
          if (op.error) continue;
          const claimed = await db<Mutation | undefined>("mark", op.id);
          if (!claimed) continue;
          const {
            draft: _draft,
            sent: _sent,
            error: _error,
            failure: _failure,
            baseLocalID: _baseLocalID,
            ...wire
          } = claimed;
          try {
            const result = await api<{ path?: string }>(
              "/api/v1/sync/entries",
              {
                method: "POST",
                signal: AbortSignal.timeout(15_000),
                body: { ...wire, path: op.method === "POST" ? "" : wire.path },
              },
            );
            // Pull before removing the overlay, so reads never flash the old content.
            await pull(db, scope);
            const entry = result.path
              ? await db<CachedEntry | null>("get", result.path, true)
              : undefined;
            await db("acknowledge", op.id, entry);
          } catch (e) {
            if (e instanceof ApiError && e.status >= 500) {
              await db(
                "mark",
                op.id,
                "The server could not confirm this edit: " + e.message,
                "uncertain",
              );
              continue;
            }
            if (
              e instanceof ApiError &&
              e.status >= 400 &&
              e.status < 500 &&
              e.status !== 401 &&
              e.status !== 429
            ) {
              await db(
                "mark",
                op.id,
                e.message,
                e.status === 409
                  ? e.message.includes("interrupted sync")
                    ? "uncertain"
                    : "conflict"
                  : "rejected",
              );
              continue;
            }
            throw e; // Preserve the exact operation ID on ambiguous network failures.
          }
        }
        useOffline.setState({ online: true });
        if ((await db<SyncState>("state")).pending.length === 0)
          await db("syncedAt");
      } catch (e) {
        useOffline.setState({
          online: false,
          error: e instanceof Error ? e.message : String(e),
        });
      } finally {
        if (cacheScope() === scope && offlineAvailable()) {
          useOffline.setState({ syncing: false });
          await refreshState();
          await reportDevice(db, scope, false);
          channel?.postMessage("changed");
        }
      }
    });
  })()
    .catch((e) => {
      useOffline.setState({ syncing: false, online: false, error: String(e) });
    })
    .finally(() => {
      active = undefined;
    });
  return active;
}
async function ensureCacheReady() {
  let state = await database<SyncState>("state");
  if (!state.ready) {
    await syncNow();
    state = await database<SyncState>("state");
  }
  if (!state.ready)
    throw new Error("Connect once to download entries for offline use.");
}
export async function cachedList(q: Record<string, unknown> = {}) {
  await ensureCacheReady();
  return database<CachedEntry[]>("list", q);
}
export async function cachedSummary(
  q: { project?: string; global?: boolean; projects?: string } = {},
) {
  await ensureCacheReady();
  return database<{
    projects: string[];
    totalEntries: number;
    byType: Record<string, number>;
  }>("summary", q);
}
export async function cachedEntry(path: string): Promise<CachedEntry> {
  const entry = await database<CachedEntry | null>("get", path);
  if (entry) return entry;
  await syncNow();
  const found = await database<CachedEntry | null>("get", path);
  if (!found) throw new Error("Entry is not available in the local cache.");
  return found;
}
export async function queueEdit(
  path: string,
  body?: Record<string, unknown>,
  raw?: string,
  expectedRevision?: string,
  expectedLocalID?: string,
) {
  const base = await cachedEntry(path);
  if (!base.revision)
    throw new Error("Download this entry before editing offline.");
  const op: Mutation = {
    id: crypto.randomUUID(),
    path: base.path,
    method: "PATCH",
    baseLocalID:
      expectedRevision !== undefined
        ? (expectedLocalID ?? "")
        : (base.local_revision ?? ""),
    revision:
      expectedRevision ??
      (typeof body?.expected_revision === "string"
        ? body.expected_revision
        : base.revision),
    body,
    raw,
    draft: makeDraft(base, body, raw),
  };
  op.draft.revision = op.revision;
  const entry = await database<CachedEntry>("queue", op);
  await refreshState();
  channel?.postMessage("changed");
  void syncNow();
  return entry;
}
export async function queueCreate(body: Record<string, unknown>) {
  const id = crypto.randomUUID();
  const path = "local/" + id;
  const fields = { ...body };
  delete fields.content;
  const raw =
    "---\n" + stringify(fields) + "---\n" + String(body.content ?? "");
  const draft = {
    ...body,
    id,
    path,
    project_id: body.project,
    raw,
    revision: "local",
    local_revision: id,
    content: String(body.content ?? ""),
  } as CachedEntry;
  await database("queue", {
    id,
    path,
    method: "POST",
    revision: "",
    body,
    draft,
  });
  await refreshState();
  channel?.postMessage("changed");
  void syncNow();
  return draft;
}
export async function resolveEdit(id: string, action: "discard" | "rebase") {
  await database(action, id);
  await refreshState();
  channel?.postMessage("changed");
  void syncNow();
}
