import type { TimelineSource } from "./timeline";

export const seededTimelineSource: TimelineSource = {
  mode: "seeded",
  events: [
    { id: "seed-feature-brain", type: "feature.completed", source: "api", timestamp: "2026-09-29T15:42:00Z", project_id: "brain-api", feature_id: "password-session-lifetime", summary: "Password sessions can now be configured to never expire." },
    { id: "seed-entry-brain", type: "entry.created", source: "api", timestamp: "2026-09-29T15:15:00Z", project_id: "brain-api", task_path: "projects/brain-api/plan/93kmh2c0.md", summary: "Timeline workspace wireframe design captured." },
    { id: "seed-task-brain", type: "task.completed", source: "runner", timestamp: "2026-09-29T14:58:00Z", project_id: "brain-api", task_id: "timeline-wireframe", task_title: "Build seeded Timeline wireframe", feature_id: "timeline-workspace", summary: "Frontend source adapter and timeline destinations verified." },
    { id: "seed-session-productivity", type: "session.activity", source: "runner", timestamp: "2026-09-29T14:31:00Z", project_id: "personal-productivity", runner_id: "runner_944ee1fb", summary: "Mouse movement automation completed on the local runner." },
    { id: "seed-feature-orion", type: "feature.completed", source: "api", timestamp: "2026-09-29T12:08:00Z", project_id: "orion-ai", feature_id: "assistant-model-routing", summary: "Jedi Brain Assistant switched to GPT 5.6 Luna." },
    { id: "seed-runner-orion", type: "runner.started", source: "runner", timestamp: "2026-09-28T22:47:00Z", project_id: "orion-ai", runner_id: "jedi-ui-local-mm-test", summary: "Runner connected with script and control capabilities." },
    { id: "seed-entry-orion", type: "entry.updated", source: "api", timestamp: "2026-09-28T20:14:00Z", project_id: "orion-ai", task_path: "projects/orion-ai/decision/model-routing.md", summary: "Model routing decision updated with Luna availability." },
    { id: "seed-project-productivity", type: "project.resumed", source: "api", timestamp: "2026-09-27T16:30:00Z", project_id: "personal-productivity", summary: "Scheduled productivity automations resumed." },
    { id: "seed-entry-deleted", type: "entry.deleted", source: "api", timestamp: "2026-09-27T11:12:00Z", project_id: "brain-api", task_path: "projects/brain-api/scratch/old-spike.md", summary: "Expired prototype notes were removed." },
  ],
};
