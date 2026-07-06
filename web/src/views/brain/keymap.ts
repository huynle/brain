// Brain view action table. Static specs — help derives from these; handlers
// close over view state in BrainView.
//
// Note: b/B (embed) shadow the global autos-pause keys here — documented
// divergence; use :pause autos or the StatusBar chip on this tab.

import { listNavSpecs } from "../../lib/keymap/listNav";
import type { ActionSpec } from "../../lib/keymap/types";

const listOnly = { focus: ["tasks" as const] };

export const BRAIN_SPECS: ActionSpec[] = [
  ...listNavSpecs("brain", "Move through entries").map((s) => ({ ...s, when: listOnly })),
  { id: "brain.toggleDetail", keys: ["T"], desc: "Toggle detail pane", hint: "Detail", group: "brain" },
  { id: "brain.toggleLogs", keys: ["z"], desc: "Toggle logs pane", hint: "Logs", group: "brain" },
  { id: "brain.open", keys: ["Enter"], desc: "Open entry", hint: "Open", group: "brain", when: listOnly },
  { id: "brain.edit", keys: ["e"], desc: "Edit entry", hint: "Edit", group: "brain", when: listOnly },
  { id: "brain.search", keys: ["/"], desc: "Search entries", hint: "Search", group: "brain" },
  { id: "brain.new", keys: ["n"], desc: "New entry", hint: "New", group: "brain" },
  { id: "brain.embedProject", keys: ["b"], desc: "Embed project", hint: "Embed", group: "brain" },
  { id: "brain.embedAll", keys: ["B"], desc: "Embed all projects", group: "brain" },
  { id: "brain.reembedProject", keys: ["F"], desc: "Re-embed project (force)", group: "brain" },
  { id: "brain.reembedAll", keys: ["A"], desc: "Re-embed all (force)", group: "brain" },
];
