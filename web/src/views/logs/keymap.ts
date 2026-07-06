// Logs view action table. Static specs — HelpBar hints and the HelpModal
// group derive from these; handlers close over view state in LogsView.

import type { ActionSpec } from "../../lib/keymap/types";

export const LOGS_SPECS: ActionSpec[] = [
  { id: "logs.filter", keys: ["/"], desc: "Filter requests (path, method, actor, status)", hint: "Filter", group: "logs" },
  { id: "logs.down", keys: ["j", "ArrowDown"], desc: "Scroll down", hint: "Scroll", group: "logs", countable: true },
  { id: "logs.up", keys: ["k", "ArrowUp"], desc: "Scroll up", group: "logs", countable: true },
  { id: "logs.top", keys: ["g"], desc: "Jump to top", group: "logs" },
  { id: "logs.bottom", keys: ["G"], desc: "Jump to bottom (enables follow)", group: "logs" },
  { id: "logs.follow", keys: ["f"], desc: "Toggle follow / live tail", hint: "Follow", group: "logs" },
];
