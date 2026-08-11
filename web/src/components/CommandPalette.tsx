/**
 * CommandPalette — wireframe-parity port of `renderCommandPalette`.
 *
 * ⌘K / Ctrl+K opens; Esc closes. Renders a scrim + centered search
 * input + command list. Commands map to workspace/modal store
 * actions. Type-filter narrows the list.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useModal } from "../store/modal";
import { useProjects } from "../hooks/useProjects";
import { useLive } from "../lib/sse";
import { useRunners } from "../hooks/useRunners";
import { useActionRunner } from "../hooks/useActionRunner";
import { useTaskActionContextFactory } from "../hooks/useTaskActionContext";
import { useFeatureActionContextFactory } from "../hooks/useFeatureActionContext";
import { buildTaskActions } from "../lib/actions/taskActions";
import { buildFeatureActions } from "../lib/actions/featureActions";
import { deriveFeatures } from "../lib/features";
import type { Task } from "../lib/types";

interface Command {
  id: string;
  label: string;
  hint?: string;
  action: () => void;
}

/**
 * Which verbs earn a palette entry.
 *
 * Not all of them: the palette lists every task and feature, so each id
 * added here multiplies the command count. These are the ones worth
 * reaching without touching the mouse. Navigation verbs are excluded —
 * the plain "Task: X" entry already goes there.
 */
const PALETTE_VERBS = new Set(["run", "resume", "cancel", "status", "delete"]);

export function CommandPalette(): JSX.Element | null {
  const open = useWorkspace((s) => s.commandOpen);
  const close = () => useWorkspace.getState().setCommandOpen(false);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [selected, setSelected] = useState(0);

  const { data: projects } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const { runners } = useRunners();
  const setView = useWorkspace((s) => s.setView);
  const toggleAssistant = useWorkspace((s) => s.toggleAssistant);
  const toggleSidebar = useWorkspace((s) => s.toggleSidebarCollapsed);
  const cycleTheme = useWorkspace((s) => s.cycleTheme);
  const openModal = useModal((s) => s.open);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const taskCtxFor = useTaskActionContextFactory();
  const featureCtxFor = useFeatureActionContextFactory();
  const runner = useActionRunner();
  const runAction = runner.run;

  useEffect(() => {
    if (open) {
      setQuery("");
      setSelected(0);
      window.setTimeout(() => inputRef.current?.focus(), 10);
    }
  }, [open]);

  const commands: Command[] = useMemo(() => {
    if (!open) return [];
    const cmds: Command[] = [
      {
        id: "view-overview",
        label: "Go to Overview",
        hint: "Enter",
        action: () => setView("overview"),
      },
      {
        id: "view-focus",
        label: "Go to Focus workspace",
        hint: "Enter",
        action: () => setView("focus"),
      },
      {
        id: "toggle-sidebar",
        label: "Toggle sidebar",
        action: toggleSidebar,
      },
      {
        id: "toggle-assistant",
        label: "Toggle Assistant panel",
        action: toggleAssistant,
      },
      {
        id: "cycle-theme",
        label: "Switch theme (dark / light / system)",
        action: cycleTheme,
      },
      {
        id: "settings",
        label: "Open settings",
        action: () => openModal("settings", {}),
      },
    ];

    // Add project / task / feature / runner commands from live data.
    for (const pid of projects ?? []) {
      cmds.push({
        id: `proj:${pid}`,
        label: `Go to project: ${pid}`,
        action: () => {
          setView("overview");
          window.setTimeout(() => {
            const el = document.querySelector<HTMLElement>(
              `.pcard[data-project="${CSS.escape(pid)}"]`,
            );
            el?.scrollIntoView({ block: "center", behavior: "smooth" });
          }, 30);
        },
      });
      const tasks = liveProjects[pid]?.tasks ?? ([] as Task[]);
      const feats = deriveFeatures(tasks, pid);
      const featureCtx = featureCtxFor(pid);
      const taskCtx = taskCtxFor(pid);

      for (const f of feats.slice(0, 20)) {
        cmds.push({
          id: `feat:${pid}:${f.id}`,
          label: `Feature: ${f.name} (${pid})`,
          action: () => openFeatureDrawer(pid, f.id),
        });
        // Verbs, not just navigation. Previously the palette could only
        // take you somewhere — for a keyboard-first user this is the
        // natural home for "run it" and "cancel it".
        for (const a of buildFeatureActions(f, featureCtx)) {
          if (!PALETTE_VERBS.has(a.id)) continue;
          cmds.push({
            id: `feat-act:${pid}:${f.id}:${a.id}`,
            label: `${a.label} — feature ${f.name} (${pid})`,
            hint: a.disabledReason || undefined,
            action: () => runAction(a),
          });
        }
      }

      for (const t of tasks.slice(0, 20)) {
        cmds.push({
          id: `task:${pid}:${t.id}`,
          label: `Task: ${t.title || t.id} (${pid})`,
          action: () => openModal("task", { projectId: pid, taskId: t.id }),
        });
        for (const a of buildTaskActions(t, taskCtx)) {
          if (!PALETTE_VERBS.has(a.id)) continue;
          cmds.push({
            id: `task-act:${pid}:${t.id}:${a.id}`,
            label: `${a.label} — ${t.title || t.id} (${pid})`,
            hint: a.disabledReason || undefined,
            action: () => runAction(a),
          });
        }
      }
    }

    for (const r of runners) {
      cmds.push({
        id: `runner:${r.runner_id}`,
        label: `Runner: ${r.runner_id} (${r.status})`,
        action: () => openModal("runner", { id: r.runner_id }),
      });
    }

    // Open in focus shortcuts
    cmds.push({
      id: "focus-runners",
      label: "Open runners in focus pane",
      action: () => openInFocus("runners", {}, "Runners"),
    });

    return cmds;
  }, [
    open,
    projects,
    liveProjects,
    runners,
    setView,
    toggleSidebar,
    toggleAssistant,
    cycleTheme,
    openModal,
    openFeatureDrawer,
    openInFocus,
    taskCtxFor,
    featureCtxFor,
    runAction,
  ]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return commands.slice(0, 50);
    return commands
      .filter((c) => c.label.toLowerCase().includes(q))
      .slice(0, 50);
  }, [commands, query]);

  useEffect(() => {
    setSelected(0);
  }, [query]);

  // The confirm dialog has to outlive the palette: running a destructive
  // command closes the palette, and if the dialog lived inside the `open`
  // guard it would unmount before the user could confirm.
  if (!open) return runner.dialog;
  if (typeof document === "undefined") return null;

  const run = (cmd: Command) => {
    close();
    cmd.action();
  };

  const palette = createPortal(
    <div
      className="palette-scrim"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div className="command-palette">
        <input
          ref={inputRef}
          type="search"
          placeholder="Type a command, project, feature, task, or runner…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              close();
              return;
            }
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setSelected((s) => Math.min(s + 1, filtered.length - 1));
              return;
            }
            if (e.key === "ArrowUp") {
              e.preventDefault();
              setSelected((s) => Math.max(s - 1, 0));
              return;
            }
            if (e.key === "Enter") {
              e.preventDefault();
              const cmd = filtered[selected];
              if (cmd) run(cmd);
              return;
            }
          }}
        />
        <div className="palette-list">
          {filtered.length === 0 && (
            <div style={{ padding: 12, color: "#6b757e", fontSize: 11 }}>
              No commands match.
            </div>
          )}
          {filtered.map((c, i) => (
            <button
              key={c.id}
              onClick={() => run(c)}
              onMouseEnter={() => setSelected(i)}
              style={
                i === selected
                  ? { background: "#1e2833", color: "#f4b23a" }
                  : undefined
              }
            >
              <span>{c.label}</span>
              {c.hint && <kbd>{c.hint}</kbd>}
            </button>
          ))}
        </div>
      </div>
    </div>,
    document.body,
  );

  return (
    <>
      {runner.dialog}
      {palette}
    </>
  );
}
