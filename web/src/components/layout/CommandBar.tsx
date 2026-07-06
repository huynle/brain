// The ":" command bar: k9s-style command grammar with fuzzy suggestions.
// Parsing/resolution is pure (lib/commands.ts); this component owns the
// suggestion UI (↑/↓/Ctrl-N/Ctrl-P to highlight, Tab to complete, Enter to
// run) and executes the typed outcomes against the stores/API.

import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useProjects } from "../../hooks/useProjects";
import {
  pauseAll,
  pauseAutomations,
  pauseProject,
  resumeAll,
  resumeAutomations,
  resumeProject,
} from "../../lib/api";
import { resolveCommand, suggest, type CommandOutcome } from "../../lib/commands";
import { useScope } from "../../store/scope";
import { ALL_PROJECTS, useUI } from "../../store/ui";

export function CommandBar({ onClose }: { onClose: () => void }) {
  const [value, setValue] = useState("");
  const [highlight, setHighlight] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);
  const setView = useUI((s) => s.setView);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const activeProject = useUI((s) => s.activeProject);
  const setProjectSheetOpen = useUI((s) => s.setProjectSheetOpen);
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const { data: projects } = useProjects();

  const ctx = useMemo(() => ({ projects: projects ?? [] }), [projects]);
  const suggestions = useMemo(() => suggest(value, ctx), [value, ctx]);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Keep the highlight inside the suggestion list as it changes.
  useEffect(() => {
    setHighlight((h) => (h >= suggestions.length ? suggestions.length - 1 : h));
  }, [suggestions.length]);

  async function runPauseResume(o: Extract<CommandOutcome, { type: "pauseResume" }>) {
    const projectForActive = activeProject === ALL_PROJECTS ? undefined : activeProject;
    const scopeAll = o.scope === "all" || (o.scope === "active" && !projectForActive);
    const project = o.scope === "project" ? o.project : projectForActive;
    const label = `${o.verb === "pause" ? "Paused" : "Resumed"} ${o.kind}${scopeAll ? " (all)" : ` — ${project}`}`;
    try {
      if (o.kind === "autos") {
        await (o.verb === "pause"
          ? pauseAutomations(scopeAll ? undefined : project)
          : resumeAutomations(scopeAll ? undefined : project));
      } else if (scopeAll) {
        await (o.verb === "pause" ? pauseAll() : resumeAll());
      } else {
        await (o.verb === "pause" ? pauseProject(project!) : resumeProject(project!));
      }
      toast(label, "success");
      void qc.invalidateQueries({ queryKey: ["runner-status"] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Pause action failed", "error");
    }
  }

  function execute(text: string) {
    const outcome = resolveCommand(text, ctx);
    switch (outcome.type) {
      case "navigate":
        if (outcome.project) setActiveProject(outcome.project);
        setView(outcome.view);
        onClose();
        return;
      case "projectPicker":
        setProjectSheetOpen(true);
        onClose();
        return;
      case "projectSwitch":
        setActiveProject(outcome.project);
        toast(`Project: ${outcome.project}`);
        onClose();
        return;
      case "preset": {
        if (outcome.project) setActiveProject(outcome.project);
        setView("tasks");
        const ui = useUI.getState();
        if (outcome.preset === "done") {
          ui.setTasksMode("done");
          ui.setDoneMergeOnly(false);
        } else if (outcome.preset === "merge-ready") {
          ui.setTasksMode("done");
          ui.setDoneMergeOnly(true);
        } else if (outcome.preset === "ready") {
          ui.setTasksMode("tasks");
          useScope.getState().setFilter("tasks", "status:ready");
        }
        onClose();
        return;
      }
      case "pauseResume":
        void runPauseResume(outcome);
        onClose();
        return;
      case "suggest":
        if (outcome.suggestions.length) setValue(outcome.suggestions[0].insert);
        else toast(`Unknown command :${text}`, "error");
        return;
      case "error":
        toast(outcome.message, "error");
        if (outcome.suggestions.length) setHighlight(0);
        return;
    }
  }

  function completeHighlighted() {
    const pick = suggestions[highlight >= 0 ? highlight : 0];
    if (pick) {
      setValue(pick.insert + " ");
      setHighlight(-1);
    }
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      if (highlight >= 0 && suggestions[highlight]) execute(suggestions[highlight].insert);
      else execute(value);
      return;
    }
    if (e.key === "Tab") {
      e.preventDefault();
      completeHighlighted();
      return;
    }
    if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) {
      e.preventDefault();
      setHighlight((h) => Math.min(h + 1, suggestions.length - 1));
      return;
    }
    if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) {
      e.preventDefault();
      setHighlight((h) => Math.max(h - 1, -1));
      return;
    }
  }

  return (
    <div className="command-bar" role="dialog" aria-label="Command">
      <span className="command-prompt">:</span>
      <input
        ref={inputRef}
        value={value}
        onChange={(e) => {
          setValue(e.currentTarget.value);
          setHighlight(-1);
        }}
        onKeyDown={onKeyDown}
        placeholder="tasks [project] · proj <name> · pause [tasks|autos] [all|project] …"
      />
      {suggestions.length > 0 && (
        <div className="command-suggestions command-suggestions-list">
          {suggestions.map((s, i) => (
            <button
              key={s.insert + i}
              type="button"
              className={i === highlight ? "on" : ""}
              onMouseEnter={() => setHighlight(i)}
              onClick={() => execute(s.insert)}
            >
              <span className="command-suggestion-label">{s.label}</span>
              {s.detail && <span className="command-suggestion-detail">{s.detail}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
