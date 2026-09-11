import { create } from "zustand";
import { stringify } from "yaml";
import { api, ApiError } from "../api";
import { useAuth } from "../auth";
import {
  cacheScope,
  database,
  databaseFor,
  offlineStorageUnavailable,
} from "./client";
import { makeDraft, matches } from "./model";
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
  cachedCount: number;
}>(() => ({
  editPath: null,
  ready: false,
  syncing: false,
  online: true,
  error: null,
  reportingError: null,
  pending: [],
  generation: 0,
  cachedCount: 0,
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
// A loopback origin (localhost / 127.0.0.1 / [::1]) is, by definition, the
// server running on this very machine — it is always reachable, so there is no
// "offline" state to cache against. Routing reads through the OPFS SQLite-WASM
// cache there only forces a large first-sync download that blocks the dashboard
// ("Loading projects…") for no benefit. On loopback we always talk to the
// server directly; offline sync stays enabled for real remote deployments.
export const onLoopbackHost = () => {
  if (typeof window === "undefined") return false;
  const h = window.location.hostname;
  return (
    h === "localhost" ||
    h === "127.0.0.1" ||
    h === "::1" ||
    h === "[::1]" ||
    h.endsWith(".localhost")
  );
};

export const offlineAvailable = () =>
  !onLoopbackHost() &&
  !offlineStorageUnavailable() &&
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
    cachedCount: state.cachedCount ?? 0,
    pending: state.pending,
    generation: s.generation + (changed ? 1 : 0),
  }));
}
async function selected(
  entries: Record<string, string>,
  db: typeof database,
  scope: string,
  remember = false,
) {
  const fetchPage = () =>
    api<ChangePage>("/api/v1/sync/entries/selected", {
      method: "POST",
      body: { entries },
      signal: AbortSignal.timeout(15000),
    });
  let page = await fetchPage();
  if (scope !== cacheScope() || !offlineAvailable())
    throw new Error("Account changed during sync");
  const state = await db<SyncState>("state");
  if (state.epoch && state.epoch !== page.epoch) {
    await db("reset"); // Preserve drafts; uncertain receipts must never replay automatically.
    entries = Object.fromEntries(
      Object.keys(entries).map((path) => [path, ""]),
    );
    page = await fetchPage();
    if (scope !== cacheScope() || !offlineAvailable())
      throw new Error("Account changed during sync");
  }
  await db("applySelected", page, remember);
  return page;
}
async function pull(db: typeof database = database, scope = cacheScope()) {
  const entries = Object.entries(await db<Record<string, string>>("selection"));
  // Pending edits are pinned and can exceed the ordinary 200-entry working set.
  for (let i = 0; i < Math.max(entries.length, 1); i += 200) {
    if (scope !== cacheScope() || !offlineAvailable())
      throw new Error("Account changed during sync");
    await selected(Object.fromEntries(entries.slice(i, i + 200)), db, scope);
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
        cache_mode: "recent",
        cached_entries: state.cachedCount ?? 0,
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
            const savedPath = result.path || op.path;
            await selected({ [savedPath]: "" }, db, scope, true);
            const entry = await db<CachedEntry | null>("get", savedPath, true);
            if (!entry)
              throw new Error(
                "Saved entry could not be confirmed; retry sync to recover the receipt.",
              );
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
// Persist only query pages the user requested. Warm reads return immediately;
// background refreshes notify React only when the result actually changes.
const queryRefreshes = new Map<string, Promise<unknown>>();
async function cachedQuery<T>(
  key: string,
  fetcher: () => Promise<T>,
): Promise<T> {
  if (!offlineAvailable()) return fetcher();
  const scope = cacheScope();
  const db: typeof database = (method, ...args) =>
    databaseFor(scope, method, ...args);
  try {
    await db("prepareSelective");
  } catch (e) {
    if (offlineStorageUnavailable()) return fetcher();
    throw e;
  }
  const cached = await db<T | null>("queryGet", key);
  const load = () => {
    const id = scope + key;
    let job = queryRefreshes.get(id) as Promise<T> | undefined;
    if (!job) {
      job = fetcher()
        .then(async (value) => {
          if (cacheScope() !== scope || !offlineAvailable())
            throw new Error("Account changed during loading");
          const changed = await db<boolean>("queryPut", key, value);
          if (changed)
            useOffline.setState((s) => ({ generation: s.generation + 1 }));
          return value;
        })
        .finally(() => queryRefreshes.delete(id));
      queryRefreshes.set(id, job);
    }
    return job;
  };
  if (scope !== cacheScope()) throw new Error("Account changed during loading");
  if (cached !== null) {
    if (navigator.onLine) void load().catch(() => {});
    return cached;
  }
  return load();
}
export async function cachedList(q: Record<string, unknown> = {}) {
  if (
    q.editorFilter !== undefined ||
    (!navigator.onLine && offlineAvailable())
  ) {
    await database("prepareSelective");
    const entries = await database<CachedEntry[]>("list", q);
    return q.editorFilter !== undefined
      ? entries
      : entries.slice(
          Number(q.offset ?? 0),
          Number(q.offset ?? 0) + Number(q.limit ?? 50),
        );
  }
  let result: { entries: CachedEntry[] };
  try {
    result = await cachedQuery("list:" + JSON.stringify(q), async () => {
      const page = await api<{ entries: CachedEntry[] }>("/api/v1/entries", {
        query: { ...q, preview: true, limit: Number(q.limit ?? 50) },
      });
      // List previews are not full offline documents and do not enter the sync set.
      return {
        ...page,
        entries: (page.entries ?? []).map((e) => ({
          ...e,
          content: e.content?.slice(0, 500) ?? "",
          raw: "",
        })),
      };
    });
  } catch (e) {
    if (!offlineAvailable() || (e instanceof ApiError && e.status < 500))
      throw e;
    const entries = await database<CachedEntry[]>("list", q);
    return q.editorFilter !== undefined
      ? entries
      : entries.slice(
          Number(q.offset ?? 0),
          Number(q.offset ?? 0) + Number(q.limit ?? 50),
        );
  }
  const entries = new Map((result.entries ?? []).map((e) => [e.path, e]));
  if (offlineAvailable())
    for (const op of (await database<SyncState>("state")).pending) {
      entries.delete(op.path);
      if (matches(op.draft, q))
        entries.set(op.path, { ...op.draft, local_revision: op.id });
    }
  return [...entries.values()];
}
export async function cachedSummary(
  q: { project?: string; global?: boolean; projects?: string } = {},
) {
  return cachedQuery("summary:" + JSON.stringify(q), async () => {
    const [projects, stats] = await Promise.all([
      api<{ projects: string[] }>("/api/v1/tasks"),
      api<{ totalEntries: number; byType: Record<string, number> }>(
        "/api/v1/stats",
        { query: q },
      ),
    ]);
    return { ...stats, projects: projects.projects ?? [] };
  });
}
export async function cachedEntry(path: string): Promise<CachedEntry> {
  if (offlineAvailable()) {
    const scope = cacheScope();
    const db: typeof database = (method, ...args) =>
      databaseFor(scope, method, ...args);
    try {
      // Touch before pruning so an opened entry from an older full cache survives migration.
      const entry = await db<CachedEntry | null>("get", path);
      await db("prepareSelective");
      if (scope !== cacheScope())
        throw new Error("Account changed during loading");
      if (entry) return entry;
      if (!navigator.onLine)
        throw new Error(
          "This entry is not stored on this device. Connect to open it.",
        );
      // Resolve short IDs through the existing entry API before selecting the canonical path.
      let canonical = path;
      if (!path.includes("/"))
        canonical = (
          await api<CachedEntry>(`/api/v1/entries/${encodeURIComponent(path)}`)
        ).path;
      const page = await selected({ [canonical]: "" }, db, scope, true);
      const found = page.changes.find((c) => c.entry)?.entry;
      if (!found)
        throw new Error(
          "This entry is not cached on this device or no longer exists.",
        );
      const saved = await db<CachedEntry | null>("get", found.path, true);
      if (!saved) throw new Error("Entry is not available");
      await db("remember", saved);
      await refreshState();
      return saved;
    } catch (e) {
      if (!offlineStorageUnavailable()) throw e;
    }
  }
  const endpoint = `/api/v1/entries/${path.split("/").map(encodeURIComponent).join("/")}`;
  const entry = await api<CachedEntry>(endpoint, {
    query: { include: "attachments" },
  });
  const raw = await api<Response>(endpoint, {
    headers: { Accept: "text/x-brain-full" },
    raw: true,
  });
  return { ...entry, raw: await raw.text() };
}
export async function queueEdit(
  path: string,
  body?: Record<string, unknown>,
  raw?: string,
  expectedRevision?: string,
  expectedLocalID?: string,
) {
  if (!offlineAvailable()) {
    await api(
      `/api/v1/entries/${path.split("/").map(encodeURIComponent).join("/")}`,
      {
        method: "PATCH",
        headers: {
          ...(raw === undefined ? {} : { "Content-Type": "text/x-brain-full" }),
          ...(expectedRevision
            ? { "X-Brain-Expected-Revision": expectedRevision }
            : {}),
        },
        ...(raw === undefined ? { body } : { rawBody: raw }),
      },
    );
    useOffline.setState((s) => ({ generation: s.generation + 1 }));
    return cachedEntry(path);
  }
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
  if (!offlineAvailable()) {
    const result = await api<{ path: string }>("/api/v1/entries", {
      method: "POST",
      body,
    });
    useOffline.setState((s) => ({ generation: s.generation + 1 }));
    return cachedEntry(result.path);
  }
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
