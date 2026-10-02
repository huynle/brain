/** Pure attention grouping, scope, filter, and sort tests (node --test, no DOM). */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  PROJECT_FILTER_ALL,
  PROJECT_FILTER_GLOBAL,
  PROJECT_FILTER_SIDEBAR,
  SEVERITY_FILTER_ALL,
  attentionCounts,
  filterAttention,
  groupAttention,
  resolveAttentionScope,
  sortAttention,
  type AttentionFilterState,
} from "./attention";
import type { Attention, AttentionSeverity, AttentionState } from "./types";

function item(
  id: string,
  state: AttentionState,
  over: Partial<Attention> = {},
): Attention {
  return {
    id,
    recipient: "me",
    kind: "task_blocked",
    severity: "info" as AttentionSeverity,
    title: `Attention ${id}`,
    state,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    revision: 1,
    ...over,
  };
}

const sample = [
  item("unread", "unread", { project: "alpha", severity: "critical" }),
  item("read", "read", { project: "beta" }),
  item("snoozed", "snoozed", { project: "alpha" }),
  item("resolved", "resolved", { project: "gamma", severity: "warning" }),
  item("dismissed", "dismissed"),
];

test("grouping: lifecycle states stay distinct and read has no group", () => {
  const groups = groupAttention(sample);
  assert.deepEqual(groups.unread.map((r) => r.id), ["unread"]);
  assert.deepEqual(groups.snoozed.map((r) => r.id), ["snoozed"]);
  assert.deepEqual(groups.resolved.map((r) => r.id), ["resolved"]);
  assert.deepEqual(groups.dismissed.map((r) => r.id), ["dismissed"]);
});

test("counts: All counts every row while tabs count their own state", () => {
  assert.deepEqual(attentionCounts(sample), {
    all: 5,
    unread: 1,
    snoozed: 1,
    resolved: 1,
    dismissed: 1,
  });
});

test("scope: sidebar projects include global and loading/unfiltered widens to all", () => {
  assert.deepEqual(
    resolveAttentionScope(PROJECT_FILTER_SIDEBAR, {
      projects: ["alpha", "beta"],
      unfiltered: false,
      loading: false,
    }),
    { kind: "set", projects: ["alpha", "beta", "global"] },
  );
  assert.deepEqual(
    resolveAttentionScope(PROJECT_FILTER_SIDEBAR, {
      projects: [],
      unfiltered: false,
      loading: true,
    }),
    { kind: "all" },
  );
  assert.deepEqual(
    resolveAttentionScope(PROJECT_FILTER_SIDEBAR, {
      projects: ["alpha"],
      unfiltered: true,
      loading: false,
    }),
    { kind: "all" },
  );
});

test("scope: all, global, and explicit project override the sidebar", () => {
  const sidebar = { projects: ["alpha"], unfiltered: false, loading: false };
  assert.deepEqual(resolveAttentionScope(PROJECT_FILTER_ALL, sidebar), {
    kind: "all",
  });
  assert.deepEqual(resolveAttentionScope(PROJECT_FILTER_GLOBAL, sidebar), {
    kind: "global",
  });
  assert.deepEqual(resolveAttentionScope("beta", sidebar), {
    kind: "project",
    project: "beta",
  });
});

function apply(filter: Partial<AttentionFilterState>) {
  return filterAttention(sample, {
    lifecycle: "all",
    severity: SEVERITY_FILTER_ALL,
    projectScope: { kind: "all" },
    query: "",
    ...filter,
  }).map((r) => r.id);
}

test("filters: lifecycle selects exact states", () => {
  assert.deepEqual(apply({ lifecycle: "unread" }), ["unread"]);
  assert.deepEqual(apply({ lifecycle: "snoozed" }), ["snoozed"]);
  assert.deepEqual(apply({ lifecycle: "resolved" }), ["resolved"]);
  assert.deepEqual(apply({ lifecycle: "dismissed" }), ["dismissed"]);
});

test("filters: severity narrows to a single level and composes with lifecycle", () => {
  assert.deepEqual(apply({ severity: "critical" }), ["unread"]);
  assert.deepEqual(apply({ severity: "warning" }), ["resolved"]);
  assert.deepEqual(
    apply({ severity: "critical", lifecycle: "resolved" }),
    [],
  );
});

test("filters: project scope treats absent project as global", () => {
  assert.deepEqual(apply({ projectScope: { kind: "global" } }), ["dismissed"]);
  assert.deepEqual(
    apply({ projectScope: { kind: "project", project: "alpha" } }),
    ["unread", "snoozed"],
  );
  assert.deepEqual(
    apply({ projectScope: { kind: "set", projects: ["gamma", "global"] } }),
    ["resolved", "dismissed"],
  );
});

test("filters: search matches title, body, or project case-insensitively", () => {
  const rows = [
    item("a", "unread", { title: "Deploy failed", project: "alpha" }),
    item("b", "unread", { title: "Review logs", body: "deploy step" }),
    item("c", "unread", { title: "Idle runner", project: "Platform-Ops" }),
  ];
  assert.deepEqual(
    filterAttention(rows, {
      lifecycle: "all",
      severity: SEVERITY_FILTER_ALL,
      projectScope: { kind: "all" },
      query: "DEPLOY",
    }).map((r) => r.id),
    ["a", "b"],
  );
  assert.deepEqual(
    filterAttention(rows, {
      lifecycle: "all",
      severity: SEVERITY_FILTER_ALL,
      projectScope: { kind: "all" },
      query: "platform-ops",
    }).map((r) => r.id),
    ["c"],
  );
});

test("sort: critical before warning before info, newest first within a level", () => {
  const rows = [
    item("i-old", "unread", {
      severity: "info",
      created_at: "2026-01-01T00:00:00Z",
    }),
    item("c1", "unread", {
      severity: "critical",
      created_at: "2026-01-01T00:00:00Z",
    }),
    item("c2", "unread", {
      severity: "critical",
      created_at: "2026-01-02T00:00:00Z",
    }),
    item("w", "unread", {
      severity: "warning",
      created_at: "2026-01-05T00:00:00Z",
    }),
  ];
  assert.deepEqual(sortAttention(rows).map((r) => r.id), [
    "c2",
    "c1",
    "w",
    "i-old",
  ]);
});
