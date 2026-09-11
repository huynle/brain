import { useIsMobile } from "../../hooks/useIsMobile";
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuth } from "../../lib/auth";
import { useLive } from "../../lib/sse";
import type { Task } from "../../lib/types";
import {
  offlineAvailable,
  cachedEntry,
  cachedList,
  queueCreate,
  queueEdit,
  refreshState,
  resolveEdit,
  syncNow,
  useOffline,
} from "../../lib/offline/sync";
import { database } from "../../lib/offline/client";
import type { CachedEntry } from "../../lib/offline/model";

export function OfflineSync() {
  const mobile = useIsMobile();
  const auth = useAuth((s) => s.status);
  const token = useAuth((s) => s.token);
  const previousScope = useRef(localStorage.getItem("brain.offline.scope"));
  const state = useOffline();
  const query = useQueryClient();
  const dialog = useRef<HTMLDialogElement>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [entries, setEntries] = useState<CachedEntry[]>([]);
  const [path, setPath] = useState("");
  const [raw, setRaw] = useState("");
  const [editingRevision, setEditingRevision] = useState("");
  const [editingLocalID, setEditingLocalID] = useState("");
  const [message, setMessage] = useState("");
  const [filter, setFilter] = useState("");
  const [server, setServer] = useState("");
  useEffect(() => {
    const scope = localStorage.getItem("brain.offline.scope");
    if (previousScope.current !== scope || auth === "needs-login") {
      previousScope.current = scope;
      query.clear();
      setEntries([]);
      setPath("");
      setRaw("");
      setServer("");
      useOffline.setState({
        ready: false,
        pending: [],
        syncing: false,
        error: null,
      });
      for (const project of Object.keys(useLive.getState().projects))
        useLive.getState().reset(project);
    }
    if (auth !== "authenticated" && auth !== "anonymous") return;
    if (!offlineAvailable()) {
      useOffline.setState({
        error:
          "This browser cannot provide persistent offline storage. The app will use the server directly.",
      });
      return;
    }
    void refreshState().catch((e) => useOffline.setState({ error: String(e) }));
    void syncNow();
    const online = () => {
      void syncNow();
    };
    const offline = () => useOffline.setState({ online: false });
    const timer = setInterval(online, 10_000);
    window.addEventListener("online", online);
    window.addEventListener("offline", offline);
    return () => {
      clearInterval(timer);
      window.removeEventListener("online", online);
      window.removeEventListener("offline", offline);
    };
  }, [auth, token, query]);
  useEffect(() => {
    if (!state.ready && offlineAvailable()) return;
    for (const key of ["entries", "projects", "automations"])
      void query.invalidateQueries({ queryKey: [key] });
    if (!offlineAvailable()) return;
    const scope = localStorage.getItem("brain.offline.scope");
    void database<CachedEntry[]>("list", { type: "task" })
      .then(async (all) => {
        if (
          !offlineAvailable() ||
          localStorage.getItem("brain.offline.scope") !== scope
        )
          return;
        // Offline task views use last indexed definitions. Runtime readiness is
        // intentionally unknown: only the server may declare a task runnable.
        const live = useLive.getState();
        const projects = new Set([
          ...Object.keys(live.projects),
          ...all.map((e) => e.project_id).filter((p): p is string => !!p),
        ]);
        for (const project of projects) {
          if (
            live.projects[project]?.connected &&
            live.projects[project]?.snapshotReceived &&
            state.online
          )
            continue;
          const saved = await database<Task[] | null>(
            "queryGet",
            "tasks:" + project,
          );
          if (localStorage.getItem("brain.offline.scope") !== scope) return;
          const definitions = new Map((saved ?? []).map((e) => [e.path, e]));
          for (const e of all.filter((e) => e.project_id === project))
            definitions.set(e.path, e as unknown as Task);
          const tasks = [...definitions.values()]
            .filter(
              (e) =>
                (e as unknown as CachedEntry).project_id === project ||
                (e as Task & { projectId?: string }).projectId === project,
            )
            .map((e) => ({
              ...e,
              projectId: project,
              classification: "unknown",
              blocked_by_reason: "Offline: execution state unavailable",
            })) as unknown as Task[];
          live.setProject(project, {
            tasks,
            snapshotReceived: false,
            connected: live.projects[project]?.connected ?? false,
            stats: undefined,
            cycles: undefined,
          });
        }
      })
      .catch((e) => setMessage(String(e)));
  }, [state.generation, state.ready, state.online, query]);
  useEffect(() => {
    if (!editorOpen || (!state.ready && offlineAvailable())) return;
    let cancelled = false;
    void cachedList({
      editorFilter: filter,
      editorLimit: 250,
      sortBy: "title",
      sortOrder: "asc",
    })
      .then((all) => {
        if (!cancelled) setEntries(all);
      })
      .catch((e) => setMessage(String(e)));
    return () => {
      cancelled = true;
    };
  }, [editorOpen, state.generation, state.ready, filter]);
  useEffect(() => {
    if (!state.editPath) return;
    void cachedEntry(state.editPath)
      .then((entry) => {
        setPath(entry.path);
        setRaw(entry.raw);
        setEditingRevision(entry.revision);
        setEditingLocalID(entry.local_revision ?? "");
        setServer("");
        setMessage("");
        setEditorOpen(true);
        dialog.current?.showModal();
        useOffline.setState({ editPath: null });
      })
      .catch((e) => setMessage(String(e)));
  }, [state.editPath]);
  if (auth !== "authenticated" && auth !== "anonymous") return null;
  const edit = (entry: CachedEntry) => {
    setPath(entry.path);
    setRaw(entry.raw);
    setEditingRevision(entry.revision);
    setEditingLocalID(entry.local_revision ?? "");
    setServer("");
    setMessage("");
  };
  const exportDraft = () => {
    const url = URL.createObjectURL(new Blob([raw], { type: "text/markdown" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "brain-draft.md";
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <>
      <button
        className="btn sm offline-sync-toggle"
        onClick={() => {
          setEditorOpen(true);
          dialog.current?.showModal();
        }}
      >
        {mobile ? "Sync · " : "Offline sync · "}
        {!offlineAvailable()
          ? "Online only"
          : state.syncing
            ? state.ready
              ? "Syncing recent entries"
              : "Preparing cache"
            : !state.online
              ? "Offline"
              : state.ready
                ? `Ready · ${state.cachedCount} cached`
                : "Downloading"}
        {state.pending.length ? ` · ${state.pending.length} pending` : ""}
      </button>
      <dialog
        ref={dialog}
        className="offline-sync-dialog"
        aria-labelledby="offline-sync-title"
        onClose={() => setEditorOpen(false)}
      >
        <div className="offline-sync-heading">
          <h2 id="offline-sync-title">Offline sync</h2>
          <button className="btn" onClick={() => dialog.current?.close()}>
            Close
          </button>
        </div>
        <p role="status">
          {!offlineAvailable()
            ? "Online mode: changes save directly to the server. Offline editing requires persistent browser storage."
            : state.ready
              ? "Only recently opened entries are stored on this device: up to 200 used within 30 days, plus pending edits. Lists load in pages; online search covers the whole library."
              : "Entries are cached when you open them. Unopened entries require a connection."}{" "}
          {offlineAvailable()
            ? `${state.pending.length} pending edits.`
            : "Local pending edits cannot be checked in this mode."}
        </p>
        <p>
          Runner controls, execution, moves, and deletion require a connection.
          Cached task execution state may be stale. This server’s administrators
          can inspect reported pending definitions and request reconciliation
          through MCP.
        </p>
        {state.error && <p role="alert">{state.error}</p>}
        {state.reportingError && (
          <p role="status">Agent sync reporting: {state.reportingError}</p>
        )}
        <button
          className="btn"
          disabled={state.syncing || !offlineAvailable()}
          onClick={() => void syncNow()}
        >
          Sync now
        </button>{" "}
        <button
          className="btn"
          onClick={() =>
            void navigator.storage
              .persist()
              .then((ok) =>
                setMessage(
                  ok
                    ? "Persistent storage granted."
                    : "Browser did not grant persistent storage. Export important unsynced drafts.",
                ),
              )
          }
        >
          Keep data on device
        </button>
        {state.pending.map((op) => (
          <section key={op.id} className="offline-pending">
            <strong>{op.draft.title}</strong> —{" "}
            {op.error
              ? "Needs review"
              : op.sent
                ? "Awaiting confirmation"
                : "Saved locally"}
            {op.error && <p role="alert">{op.error}</p>}
            <button
              className="btn sm"
              onClick={() => {
                edit(op.draft);
                void database<CachedEntry | null>("get", op.path, true).then(
                  (e) => setServer(e?.raw ?? "Server entry is absent."),
                );
              }}
            >
              Review draft
            </button>{" "}
            {op.error &&
              op.method === "PATCH" &&
              op.failure !== "uncertain" && (
                <button
                  className="btn sm"
                  disabled={path !== op.path || !server}
                  onClick={() => {
                    if (
                      window.confirm(
                        "Submit your draft over the current server version? Review both versions first.",
                      )
                    )
                      void resolveEdit(op.id, "rebase").catch((e) =>
                        setMessage(String(e)),
                      );
                  }}
                >
                  Apply draft to current version
                </button>
              )}{" "}
            <button
              className="btn sm"
              onClick={() => {
                if (
                  window.confirm(
                    "Discard this local edit? Export the draft first if you need it.",
                  )
                )
                  void resolveEdit(op.id, "discard")
                    .then(() => {
                      if (path === op.path) {
                        setPath("");
                        setRaw("");
                        setServer("");
                      }
                      setMessage("Local edit discarded.");
                    })
                    .catch((e) => setMessage(String(e)));
              }}
            >
              Discard local edit
            </button>
          </section>
        ))}
        <h3>Edit an entry</h3>
        <p>
          Find a title, type, or project. Up to 250 matching entries are shown.
        </p>
        <label>
          Find entry
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Title, type, or project"
          />
        </label>
        <label>
          Entry
          <select
            aria-label="Entry"
            value={path}
            onChange={(e) => {
              const item = entries.find((x) => x.path === e.target.value);
              if (item) edit(item);
            }}
          >
            <option value="">Choose an entry</option>
            {entries
              .filter((e) =>
                (e.title + " " + e.type + " " + e.project_id)
                  .toLowerCase()
                  .includes(filter.toLowerCase()),
              )
              .map((e) => (
                <option key={e.path} value={e.path}>
                  {e.title} ({e.type})
                </option>
              ))}
          </select>
        </label>
        {path && (
          <>
            <label>
              Draft — YAML definition and Markdown content
              <textarea
                aria-label="Entry draft"
                rows={15}
                value={raw}
                onChange={(e) => setRaw(e.target.value)}
              />
            </label>
            <button
              className="btn primary"
              onClick={() =>
                void queueEdit(
                  path,
                  undefined,
                  raw,
                  editingRevision,
                  editingLocalID,
                )
                  .then((saved) => {
                    setEditingRevision(saved.revision);
                    setEditingLocalID(saved.local_revision ?? "");
                    setMessage(
                      offlineAvailable()
                        ? "Saved on this device; awaiting server validation and sync."
                        : "Saved to the server.",
                    );
                  })
                  .catch((e) => setMessage(String(e)))
              }
            >
              {offlineAvailable() ? "Save locally" : "Save to server"}
            </button>{" "}
            <button className="btn" onClick={exportDraft}>
              Export draft
            </button>
          </>
        )}
        {server && (
          <label>
            Current server version
            <textarea
              aria-label="Current server version"
              readOnly
              rows={10}
              value={server}
            />
          </label>
        )}
        <details>
          <summary>Create an entry offline</summary>
          <CreateOffline onSaved={setMessage} />
        </details>
        {message && <p role="status">{message}</p>}
      </dialog>
    </>
  );
}
function CreateOffline({ onSaved }: { onSaved: (s: string) => void }) {
  const [type, setType] = useState("scratch");
  const [title, setTitle] = useState("");
  const [project, setProject] = useState("");
  const [content, setContent] = useState("");
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void queueCreate({
          type,
          title,
          project: project || undefined,
          content,
          status: type === "scratch" ? "active" : "draft",
        })
          .then(() =>
            onSaved(
              "Created locally. Edit its full definition above before activating it.",
            ),
          )
          .catch((e) => onSaved(String(e)));
      }}
    >
      <label>
        Type
        <select
          aria-label="Type"
          value={type}
          onChange={(e) => setType(e.target.value)}
        >
          <option>scratch</option>
          <option>task</option>
          <option>automation</option>
        </select>
      </label>
      <label>
        Title
        <input
          required
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </label>
      <label>
        Project
        <input value={project} onChange={(e) => setProject(e.target.value)} />
      </label>
      <label>
        Content
        <textarea
          aria-label="Content"
          required
          value={content}
          onChange={(e) => setContent(e.target.value)}
        />
      </label>
      <button className="btn" type="submit">
        Create locally
      </button>
    </form>
  );
}
