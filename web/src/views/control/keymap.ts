// Runners view action table (the view lives at views/RunnersView.tsx; this
// folder already holds its helpers). Static specs — help derives from these.

import { listNavSpecs } from "../../lib/keymap/listNav";
import type { ActionSpec } from "../../lib/keymap/types";

export const RUNNERS_SPECS: ActionSpec[] = [
  ...listNavSpecs("runners", "Move through runners and instances"),
  { id: "runners.open", keys: ["Enter", "o"], desc: "Open instance in Control", hint: "Open", group: "runners" },
  { id: "runners.spawn", keys: ["n", "+"], desc: "Spawn new ad-hoc instance", hint: "New", group: "runners" },
  { id: "runners.shutdown", keys: ["s"], desc: "Shut down cursored runner", hint: "Shutdown", group: "runners" },
  { id: "runners.killInstance", keys: ["K"], desc: "Kill cursored instance", group: "runners" },
  { id: "runners.pause", keys: ["p", "P"], desc: "Pause/resume tasks for the active scope", hint: "Pause", group: "runners" },
  { id: "runners.pauseAutos", keys: ["a", "A"], desc: "Pause/resume automations", hint: "Autos", group: "runners" },
];
