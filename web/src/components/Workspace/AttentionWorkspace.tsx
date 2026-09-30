import { useEffect, useMemo, useState } from "react";

import { useProjects } from "../../hooks/useProjects";
import { useAttention, useAttentionCounts } from "../../hooks/useAttention";
import { useAttentionRowActions } from "../../hooks/useAttentionRowActions";
import { useVisibleProjects } from "../../hooks/useVisibleProjects";
import {
  PROJECT_FILTER_ALL,
  PROJECT_FILTER_GLOBAL,
  PROJECT_FILTER_SIDEBAR,
  SEVERITY_FILTER_ALL,
  attentionCounts,
  filterAttention,
  resolveAttentionScope,
  sortAttention,
  type AttentionLifecycle,
  type SeverityFilter,
} from "../../lib/attention";
import {
  disablePushNotifications,
  enablePushNotifications,
  pushSupported,
} from "../../lib/push";
import { getVapidPublicKey } from "../../lib/api";
import { useUI } from "../../store/ui";
import { ErrorState } from "../common/ErrorState";
import { Loading } from "../common/Loading";
import { AttentionRows } from "../Attention/AttentionRows";

const FILTERS: Array<{
  id: AttentionLifecycle;
  label: string;
  hint: string;
}> = [
  { id: "all", label: "All", hint: "Every notification in scope" },
  { id: "unread", label: "Unread", hint: "Awaiting you" },
  { id: "snoozed", label: "Snoozed", hint: "Hidden until their snooze passes" },
  { id: "resolved", label: "Resolved", hint: "Dealt with" },
  { id: "dismissed", label: "Dismissed", hint: "Cleared without action" },
];

const SEVERITIES: Array<{ id: SeverityFilter; label: string }> = [
  { id: SEVERITY_FILTER_ALL, label: "All severities" },
  { id: "critical", label: "Critical" },
  { id: "warning", label: "Warning" },
  { id: "info", label: "Info" },
];

function PushControl(): JSX.Element | null {
  const toast = useUI((s) => s.toast);
  const [available, setAvailable] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);

  // Hide the control unless the browser can do Web Push AND the server has a
  // configured VAPID key (empty key ⇒ Web Push not configured).
  useEffect(() => {
    let cancelled = false;
    if (!pushSupported()) {
      setAvailable(false);
      return;
    }
    getVapidPublicKey()
      .then((key) => {
        if (!cancelled) setAvailable(Boolean(key));
      })
      .catch(() => {
        if (!cancelled) setAvailable(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (available !== true) return null;

  const enable = async () => {
    setBusy(true);
    try {
      const result = await enablePushNotifications();
      if (result.ok) {
        toast("Browser notifications enabled", "success");
      } else if (result.reason === "denied") {
        toast("Notification permission was denied", "error");
      } else if (result.reason === "not-configured") {
        toast("Web Push is not configured on this server", "error");
      } else if (result.reason === "unsupported") {
        toast("This browser does not support Web Push", "error");
      } else {
        toast(`Could not enable notifications: ${result.message ?? ""}`, "error");
      }
    } finally {
      setBusy(false);
    }
  };

  const disable = async () => {
    setBusy(true);
    try {
      const result = await disablePushNotifications();
      if (result.ok) toast("Browser notifications disabled", "success");
      else toast(`Could not disable notifications: ${result.message ?? ""}`, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="attention-push">
      <button type="button" disabled={busy} onClick={() => void enable()}>
        Enable browser notifications
      </button>
      <button
        type="button"
        className="attention-push__off"
        disabled={busy}
        onClick={() => void disable()}
      >
        Disable
      </button>
    </div>
  );
}

export function AttentionWorkspace(): JSX.Element {
  const attentionQuery = useAttention();
  const actions = useAttentionRowActions(attentionQuery);
  const { counts: totals } = useAttentionCounts();
  const sidebar = useVisibleProjects();
  const { data: projects } = useProjects();
  const [lifecycle, setLifecycle] = useState<AttentionLifecycle>("unread");
  const [severity, setSeverity] = useState<SeverityFilter>(SEVERITY_FILTER_ALL);
  const [projectFilter, setProjectFilter] = useState(PROJECT_FILTER_SIDEBAR);
  const [query, setQuery] = useState("");

  const projectScope = useMemo(
    () =>
      resolveAttentionScope(projectFilter, {
        projects: sidebar.visible,
        unfiltered: sidebar.unfiltered,
        loading: sidebar.loading,
      }),
    [projectFilter, sidebar.visible, sidebar.unfiltered, sidebar.loading],
  );
  const scoped = useMemo(
    () =>
      filterAttention(attentionQuery.attention, {
        lifecycle: "all",
        severity,
        projectScope,
        query,
      }),
    [attentionQuery.attention, severity, projectScope, query],
  );
  const counts = useMemo(() => attentionCounts(scoped), [scoped]);
  const visible = useMemo(
    () =>
      sortAttention(
        filterAttention(scoped, {
          lifecycle,
          severity: SEVERITY_FILTER_ALL,
          projectScope: { kind: "all" },
          query: "",
        }),
      ),
    [scoped, lifecycle],
  );

  if (attentionQuery.isLoading) {
    return <Loading label="Loading notifications…" />;
  }
  if (attentionQuery.error) {
    return (
      <ErrorState
        error={attentionQuery.error}
        onRetry={attentionQuery.refetch}
      />
    );
  }

  return (
    <div className="attention-workspace">
      <header className="attention-workspace__hero">
        <div>
          <div className="attention-workspace__eyebrow">Attention inbox</div>
          <h1>What needs you—and what you have already handled.</h1>
          <p>
            Durable, actionable notifications addressed to you: blocked tasks,
            idle runners, failed deliveries, and anything else the system
            wants to surface.
          </p>
          <PushControl />
        </div>
        <div className="attention-workspace__total">
          <strong>{totals?.unread ?? counts.unread}</strong>
          <span>unread</span>
        </div>
      </header>

      <div className="attention-toolbar">
        <input
          type="search"
          placeholder="Search title, body, or project…"
          aria-label="Search notifications"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <select
          aria-label="Severity"
          value={severity}
          onChange={(event) =>
            setSeverity(event.target.value as SeverityFilter)
          }
        >
          {SEVERITIES.map((s) => (
            <option key={s.id} value={s.id}>
              {s.label}
            </option>
          ))}
        </select>
        <select
          aria-label="Notification project scope"
          value={projectFilter}
          title={
            projectFilter === PROJECT_FILTER_SIDEBAR
              ? `Following the sidebar: ${sidebar.visible.length} of ${sidebar.all.length} projects, plus global`
              : "Project"
          }
          onChange={(event) => setProjectFilter(event.target.value)}
        >
          <option value={PROJECT_FILTER_SIDEBAR}>
            {sidebar.unfiltered
              ? "Sidebar projects"
              : `Sidebar projects (${sidebar.visible.length})`}
          </option>
          <option value={PROJECT_FILTER_ALL}>All projects</option>
          <option value={PROJECT_FILTER_GLOBAL}>Global</option>
          {(projects ?? []).map((project) => (
            <option key={project} value={project}>
              {project}
            </option>
          ))}
        </select>
      </div>

      <nav className="attention-filters" aria-label="Notification lifecycle">
        {FILTERS.map((filter) => (
          <button
            key={filter.id}
            type="button"
            className={lifecycle === filter.id ? "active" : ""}
            aria-pressed={lifecycle === filter.id}
            title={filter.hint}
            onClick={() => setLifecycle(filter.id)}
          >
            <span>{filter.label}</span>
            <strong>{counts[filter.id]}</strong>
          </button>
        ))}
      </nav>

      <main className="attention-workspace__list">
        <div className="attention-workspace__list-head">
          <span>{FILTERS.find((filter) => filter.id === lifecycle)?.label}</span>
          <span>
            {visible.length} notification{visible.length === 1 ? "" : "s"}
          </span>
        </div>
        {visible.length > 0 ? (
          <AttentionRows items={visible} actions={actions} />
        ) : (
          <div className="attention-workspace__empty">
            <strong>Nothing here.</strong>
            <span>Try another tab, severity, project, or search.</span>
          </div>
        )}
      </main>
    </div>
  );
}
