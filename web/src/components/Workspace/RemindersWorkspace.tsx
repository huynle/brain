import { useMemo, useState } from "react";

import { useProjects } from "../../hooks/useProjects";
import { useReminderRowActions } from "../../hooks/useReminderRowActions";
import { useReminders } from "../../hooks/useReminders";
import { useVisibleProjects } from "../../hooks/useVisibleProjects";
import {
  PROJECT_FILTER_ALL,
  PROJECT_FILTER_GLOBAL,
  PROJECT_FILTER_SIDEBAR,
  filterReminders,
  reminderCounts,
  resolveReminderScope,
  type ReminderLifecycle,
} from "../../lib/reminders";
import { ErrorState } from "../common/ErrorState";
import { Loading } from "../common/Loading";
import { ReminderRows } from "../Reminders/ReminderRows";

const FILTERS: Array<{
  id: ReminderLifecycle;
  label: string;
  hint: string;
}> = [
  { id: "all", label: "All", hint: "Every reminder, once" },
  { id: "waiting", label: "Waiting", hint: "Fired and awaiting you" },
  { id: "scheduled", label: "Scheduled", hint: "Armed for a future time" },
  {
    id: "stopped",
    label: "Stopped",
    hint: "Blocked, cancelled, or archived reminders",
  },
  { id: "someday", label: "Someday", hint: "Undated; never fires itself" },
  { id: "executed", label: "Executed", hint: "Done reminder history" },
  { id: "agent", label: "Agent runs", hint: "Reminders that created tasks" },
];

export function RemindersWorkspace(): JSX.Element {
  const remindersQuery = useReminders();
  const actions = useReminderRowActions(remindersQuery);
  const sidebar = useVisibleProjects();
  const { data: projects } = useProjects();
  const [lifecycle, setLifecycle] = useState<ReminderLifecycle>("all");
  const [projectFilter, setProjectFilter] = useState(PROJECT_FILTER_SIDEBAR);
  const [query, setQuery] = useState("");

  const projectScope = useMemo(
    () =>
      resolveReminderScope(projectFilter, {
        projects: sidebar.visible,
        unfiltered: sidebar.unfiltered,
        loading: sidebar.loading,
      }),
    [projectFilter, sidebar.visible, sidebar.unfiltered, sidebar.loading],
  );
  const scoped = useMemo(
    () =>
      filterReminders(remindersQuery.reminders, {
        lifecycle: "all",
        projectScope,
        query,
      }),
    [remindersQuery.reminders, projectScope, query],
  );
  const counts = useMemo(() => reminderCounts(scoped), [scoped]);
  const visible = useMemo(
    () =>
      filterReminders(scoped, {
        lifecycle,
        projectScope: { kind: "all" },
        query: "",
      }),
    [scoped, lifecycle],
  );

  if (remindersQuery.isLoading) {
    return <Loading label="Loading reminders…" />;
  }
  if (remindersQuery.error) {
    return (
      <ErrorState
        error={remindersQuery.error}
        onRetry={remindersQuery.refetch}
      />
    );
  }

  return (
    <div className="reminders-workspace">
      <header className="reminders-workspace__hero">
        <div>
          <div className="reminders-workspace__eyebrow">Reminder centre</div>
          <h1>What needs attention—and what already ran.</h1>
          <p>
            One timeline for live prompts, future schedules, someday notes, and
            the agent work reminders set in motion.
          </p>
        </div>
        <div className="reminders-workspace__total">
          <strong>{counts.all}</strong>
          <span>in view</span>
        </div>
      </header>

      <div className="reminders-toolbar">
        <input
          type="search"
          placeholder="Search title or project…"
          aria-label="Search reminders"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <select
          aria-label="Reminder project scope"
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

      <nav className="reminder-filters" aria-label="Reminder lifecycle">
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

      <main className="reminders-workspace__list">
        <div className="reminders-workspace__list-head">
          <span>{FILTERS.find((filter) => filter.id === lifecycle)?.label}</span>
          <span>
            {visible.length} reminder{visible.length === 1 ? "" : "s"}
          </span>
        </div>
        {visible.length > 0 ? (
          <ReminderRows reminders={visible} actions={actions} />
        ) : (
          <div className="reminders-workspace__empty">
            <strong>No reminders here.</strong>
            <span>Try another lifecycle, project, or search.</span>
          </div>
        )}
      </main>
    </div>
  );
}
