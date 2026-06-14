import { useState } from "react";
import { useProjects } from "../../hooks/useProjects";
import { useIsMobile } from "../../hooks/useIsMobile";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import { BottomSheet } from "./BottomSheet";
import { Modal } from "../common/Modal";

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "All projects";
  return id.split(/[/\\]/).pop() || id;
}

// Project picker with a search box. Clicking the status-bar project name opens
// it: a bottom sheet on mobile, a centered modal on desktop. Type to filter,
// Enter picks the top match.
export function ProjectSheet() {
  const open = useUI((s) => s.projectSheetOpen);
  const setOpen = useUI((s) => s.setProjectSheetOpen);
  const active = useUI((s) => s.activeProject);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const isMobile = useIsMobile();
  const projectsQ = useProjects();
  const [q, setQ] = useState("");

  if (!open) return null;

  const all = [ALL_PROJECTS, ...(projectsQ.data ?? [])];
  const needle = q.trim().toLowerCase();
  const filtered = needle ? all.filter((p) => shortName(p).toLowerCase().includes(needle)) : all;

  function close() {
    setOpen(false);
    setQ("");
  }
  function pick(p: string) {
    setActiveProject(p);
    close();
  }

  const body = (
    <div className="proj-picker">
      <input
        className="proj-search"
        autoFocus
        placeholder="Search projects…"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && filtered.length) pick(filtered[0]);
          else if (e.key === "Escape") close();
        }}
      />
      <div className="proj-list">
        {filtered.map((p) => (
          <button key={p} className={`proj-item ${p === active ? "on" : ""}`} onClick={() => pick(p)}>
            <span className="proj-dot">{p === active ? "●" : "○"}</span>
            <span className="truncate">{shortName(p)}</span>
          </button>
        ))}
        {filtered.length === 0 && <div className="faint" style={{ padding: 10 }}>No match for “{q}”.</div>}
        {projectsQ.isLoading && <div className="faint" style={{ padding: 10 }}>Loading projects…</div>}
      </div>
    </div>
  );

  return isMobile ? (
    <BottomSheet title="Project" onClose={close}>
      {body}
    </BottomSheet>
  ) : (
    <Modal title="Switch project" onClose={close}>
      {body}
    </Modal>
  );
}
