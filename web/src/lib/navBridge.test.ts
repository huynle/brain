import { strict as assert } from "node:assert";
import { test } from "node:test";

import { canonicalNavEntry } from "./navBridge";

test("legacy reminder pane history opens the top-level Reminders view", () => {
  assert.deepEqual(
    canonicalNavEntry({
      view: "focus",
      leaf: { dock: "focus", kind: "reminders", target: {}, title: "Reminders" },
    }),
    { view: "reminders" },
  );
});

test("ordinary pane history is unchanged", () => {
  const entry = {
    view: "focus" as const,
    leaf: {
      dock: "focus" as const,
      kind: "entry",
      target: { path: "projects/canis/report/example.md" },
      title: "Example",
    },
  };
  assert.equal(canonicalNavEntry(entry), entry);
});
