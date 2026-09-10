import React, { useEffect } from "react";
import { createRoot } from "react-dom/client";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { api, getBacklinks, getOutlinks } from "./lib/api";
import { useAuth } from "./lib/auth";
import { Login } from "./pages/Login";
import { EntryMarkdown } from "./components/Workspace/EntryMarkdown";
import type { BrainEntry } from "./lib/types";
import { readerHref, readerLink } from "./lib/readerLinks";
import "./styles/reader.css";

const ref = new URLSearchParams(window.location.search).get("entry")?.trim();
function Reader() {
  const status = useAuth((s) => s.status);
  const init = useAuth((s) => s.init);
  useEffect(() => {
    void init(true);
  }, [init]);
  if (!ref)
    return (
      <main>
        <h1>Read an entry</h1>
        <p>
          Add <code>?entry=projects/your-project/scratch/entry-id.md</code> or
          an entry ID to this URL.
        </p>
      </main>
    );
  if (status === "loading")
    return (
      <main>
        <p role="status">Connecting…</p>
      </main>
    );
  if (status === "needs-login") return <Login />;
  return <Document entryRef={ref} />;
}
function Document({ entryRef }: { entryRef: string }) {
  // Direct single-entry request: never bootstrap the offline database here.
  const entry = useQuery({
    queryKey: ["reader", entryRef],
    queryFn: () =>
      api<BrainEntry>(
        `/api/v1/entries/${entryRef.split("/").map(encodeURIComponent).join("/")}`,
        { query: { include: "attachments" } },
      ),
  });
  const back = useQuery({
    queryKey: ["reader-backlinks", entry.data?.id],
    enabled: !!entry.data,
    queryFn: () => getBacklinks(entry.data!.id),
  });
  const forward = useQuery({
    queryKey: ["reader-outlinks", entry.data?.id],
    enabled: !!entry.data,
    queryFn: () => getOutlinks(entry.data!.id),
  });
  useEffect(() => {
    if (!entry.data) return;
    document.title = entry.data.title + " · Brain";
    if (location.hash) {
      let id = location.hash.slice(1);
      try {
        id = decodeURIComponent(id);
      } catch {
        /* retain literal */
      }
      document.getElementById(id)?.scrollIntoView();
    }
  }, [entry.data]);
  if (entry.isPending)
    return (
      <main>
        <p role="status">Loading entry…</p>
      </main>
    );
  if (entry.error)
    return (
      <main>
        <h1>Unable to load entry</h1>
        <p role="alert">{entry.error.message}</p>
        <button onClick={() => void entry.refetch()}>Retry</button>
      </main>
    );
  const e = entry.data;
  return (
    <main>
      <article>
        <header>
          <h1>{e.title}</h1>
        </header>
        <EntryMarkdown
          content={e.content || ""}
          attachments={e.attachments}
          standalone
          linkHref={(href) => readerLink(href, e.path, location.origin)}
        />
      </article>
      <footer aria-label="Entry connections">
        <Connections
          title="Links from this entry"
          entries={forward.data}
          error={forward.error}
        />
        <Connections title="Backlinks" entries={back.data} error={back.error} />
        <a
          className="open-brain"
          href={`/?entry=${encodeURIComponent(e.path)}`}
        >
          Open in Brain
        </a>
      </footer>
    </main>
  );
}
function Connections({
  title,
  entries,
  error,
}: {
  title: string;
  entries?: BrainEntry[];
  error: Error | null;
}) {
  return (
    <section>
      <h2>{title}</h2>
      {error ? (
        <p>Could not load connections.</p>
      ) : !entries ? (
        <p>Loading…</p>
      ) : entries.length ? (
        <ul>
          {entries.map((e) => (
            <li key={e.path}>
              <a href={readerHref(e.path)}>{e.title || e.path}</a>
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">None.</p>
      )}
    </section>
  );
}
const client = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 30000, refetchOnWindowFocus: false },
  },
});
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <Reader />
    </QueryClientProvider>
  </React.StrictMode>,
);
