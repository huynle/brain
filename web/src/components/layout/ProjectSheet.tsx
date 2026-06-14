import { useProjects } from "../../hooks/useProjects";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import { BottomSheet } from "./BottomSheet";

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "All projects";
  return id.split(/[/\\]/).pop() || id;
}

// Mobile project picker — replaces the desktop H/L project switch.
export function ProjectSheet() {
  const open = useUI((s) => s.projectSheetOpen);
  const setOpen = useUI((s) => s.setProjectSheetOpen);
  const active = useUI((s) => s.activeProject);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const projectsQ = useProjects();

  if (!open) return null;

  const projects = [ALL_PROJECTS, ...(projectsQ.data ?? [])];

  function pick(p: string) {
    setActiveProject(p);
    setOpen(false);
  }

  return (
    <BottomSheet title="Project" onClose={() => setOpen(false)}>
      <div className="proj-list">
        {projects.map((p) => (
          <button
            key={p}
            className={`proj-item ${p === active ? "on" : ""}`}
            onClick={() => pick(p)}
          >
            <span className="proj-dot">{p === active ? "●" : "○"}</span>
            <span className="truncate">{shortName(p)}</span>
          </button>
        ))}
        {projectsQ.isLoading && <div className="faint" style={{ padding: 10 }}>Loading projects…</div>}
      </div>
    </BottomSheet>
  );
}
