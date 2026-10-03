import assert from "node:assert/strict";
import test from "node:test";

import {
  categoryForSection,
  dirtySettingsCategories,
  nextSettingsCategory,
} from "./settingsNavigation";

test("categoryForSection groups the settings into five focused destinations", () => {
  assert.equal(categoryForSection("assistant"), "assistant");
  assert.equal(categoryForSection("task_defaults"), "tasks");
  assert.equal(categoryForSection("runner.opencode"), "runner");
  assert.equal(categoryForSection("server"), "advanced");
  assert.equal(categoryForSection("future_server_section"), "advanced");
});

test("dirtySettingsCategories marks only categories containing changed fields", () => {
  const before = {
    server: {
      assistant: { model: "old", jobs: { enabled: false, max_parallel: 3 } },
      port: 3333,
    },
    runner: { max_parallel: 3 },
    mcp: { api_url: "http://old" },
  };
  const after = {
    server: {
      assistant: { model: "new", jobs: { enabled: false, max_parallel: 3 } },
      port: 3333,
    },
    runner: { max_parallel: 5 },
    mcp: { api_url: "http://new" },
  };

  assert.deepEqual(
    [...dirtySettingsCategories(before, after)].sort(),
    ["advanced", "assistant", "runner"],
  );
});

test("nextSettingsCategory supports standard tab-list keyboard navigation", () => {
  const tabs = ["general", "assistant", "tasks", "runner", "advanced"] as const;
  assert.equal(nextSettingsCategory(tabs, "general", "ArrowRight"), "assistant");
  assert.equal(nextSettingsCategory(tabs, "general", "ArrowLeft"), "advanced");
  assert.equal(nextSettingsCategory(tabs, "runner", "Home"), "general");
  assert.equal(nextSettingsCategory(tabs, "assistant", "End"), "advanced");
  assert.equal(nextSettingsCategory(tabs, "tasks", "Enter"), null);
});
