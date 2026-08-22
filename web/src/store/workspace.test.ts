/**
 * panes-v2 workspace store — unit tests.
 *
 * Runs under `node --test` (see web/package.json) — no DOM, no React.
 * We exercise the pure reducer logic that drives the v2 shell:
 *   - view switch (overview / focus / session)
 *   - sidebar section collapse toggle
 *   - mobile flag toggle
 *   - streaming flag setter
 *   - feature-assignment map put/clear
 *   - focus-session setter (linked to setView("session"))
 *
 * Persistence to localStorage is provided by zustand/middleware/persist
 * — we don't test the middleware itself (that's zustand's job), only
 * that the store constructor doesn't crash in a Node environment
 * where localStorage is absent.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  useWorkspace,
  WORKSPACE_STORAGE_KEY,
  clampDrawerWidth,
} from "./workspace";

// ─── setup ────────────────────────────────────────────────────────────

/**
 * Reset the store between tests. Zustand exposes `setState` for exactly
 * this — we snapshot the initial state once on module load, then restore
 * just the data fields before each test.
 *
 * IMPORTANT: pass `false` (or omit) for the replace flag so that action
 * functions survive. `setState(x, true)` blows away everything including
 * the action closures.
 */
const INITIAL = useWorkspace.getState();

function resetStore() {
  useWorkspace.setState({
    view: INITIAL.view,
    focusSessionId: INITIAL.focusSessionId,
    dockTree: INITIAL.dockTree,
    lastFocusLeafId: null,
    sidebarSection: { ...INITIAL.sidebarSection },
    mobile: INITIAL.mobile,
    streaming: INITIAL.streaming,
    featureAssignments: { ...INITIAL.featureAssignments },
    mergedExpanded: {},
    archivedExpanded: {},
    statusFilter: "all",
    drawer: null,
    drawerWidth: 430,
  });
}

// ─── storage key ──────────────────────────────────────────────────────

test("workspace: persisted storage key is versioned", () => {
  assert.equal(WORKSPACE_STORAGE_KEY, "panes-v2:workspace:v1");
});

// ─── initial state ────────────────────────────────────────────────────

test("workspace: default view is overview", () => {
  resetStore();
  assert.equal(useWorkspace.getState().view, "overview");
});

test("workspace: sidebar sections all default to open", () => {
  resetStore();
  const s = useWorkspace.getState().sidebarSection;
  assert.equal(s.projects, true);
  assert.equal(s.sessions, true);
  assert.equal(s.runners, true);
});

test("workspace: feature-assignments starts empty", () => {
  resetStore();
  assert.deepEqual(useWorkspace.getState().featureAssignments, {});
});

// ─── view switching ───────────────────────────────────────────────────

test("workspace: setView updates the view", () => {
  resetStore();
  useWorkspace.getState().setView("focus");
  assert.equal(useWorkspace.getState().view, "focus");
  useWorkspace.getState().setView("session");
  assert.equal(useWorkspace.getState().view, "session");
  useWorkspace.getState().setView("overview");
  assert.equal(useWorkspace.getState().view, "overview");
});

test("workspace: setFocusSession switches view to session and stores id", () => {
  resetStore();
  useWorkspace.getState().setFocusSession("s-42");
  assert.equal(useWorkspace.getState().view, "session");
  assert.equal(useWorkspace.getState().focusSessionId, "s-42");
});

test("workspace: setFocusSession(undefined) clears session id but keeps view", () => {
  resetStore();
  useWorkspace.getState().setFocusSession("s-42");
  useWorkspace.getState().setFocusSession(undefined);
  assert.equal(useWorkspace.getState().focusSessionId, undefined);
  // View intentionally stays "session" so an empty session pane can
  // render; setView is the one place that changes the view field.
  assert.equal(useWorkspace.getState().view, "session");
});

// ─── sidebar section toggle ───────────────────────────────────────────

test("workspace: toggleSidebarSection flips one key without touching others", () => {
  resetStore();
  useWorkspace.getState().toggleSidebarSection("projects");
  let s = useWorkspace.getState().sidebarSection;
  assert.equal(s.projects, false);
  assert.equal(s.sessions, true);
  assert.equal(s.runners, true);

  useWorkspace.getState().toggleSidebarSection("projects");
  s = useWorkspace.getState().sidebarSection;
  assert.equal(s.projects, true);
});

test("workspace: toggleSidebarSection works for each key independently", () => {
  resetStore();
  useWorkspace.getState().toggleSidebarSection("sessions");
  useWorkspace.getState().toggleSidebarSection("runners");
  const s = useWorkspace.getState().sidebarSection;
  assert.equal(s.projects, true);
  assert.equal(s.sessions, false);
  assert.equal(s.runners, false);
});

// ─── flags ────────────────────────────────────────────────────────────

test("workspace: setMobile flips the mobile flag", () => {
  resetStore();
  assert.equal(useWorkspace.getState().mobile, false);
  useWorkspace.getState().setMobile(true);
  assert.equal(useWorkspace.getState().mobile, true);
  useWorkspace.getState().setMobile(false);
  assert.equal(useWorkspace.getState().mobile, false);
});

test("workspace: setStreaming updates streaming flag", () => {
  resetStore();
  useWorkspace.getState().setStreaming(true);
  assert.equal(useWorkspace.getState().streaming, true);
});

// ─── feature assignments ──────────────────────────────────────────────

test("workspace: assignFeature stores feature_id → runner_id", () => {
  resetStore();
  useWorkspace.getState().assignFeature("panes-v2", "r-macbook");
  assert.equal(
    useWorkspace.getState().featureAssignments["panes-v2"],
    "r-macbook",
  );
});

test("workspace: assignFeature overwrites existing mapping", () => {
  resetStore();
  useWorkspace.getState().assignFeature("panes-v2", "r-a");
  useWorkspace.getState().assignFeature("panes-v2", "r-b");
  assert.equal(
    useWorkspace.getState().featureAssignments["panes-v2"],
    "r-b",
  );
});

test("workspace: unassignFeature removes the mapping", () => {
  resetStore();
  useWorkspace.getState().assignFeature("panes-v2", "r-macbook");
  useWorkspace.getState().unassignFeature("panes-v2");
  assert.equal(
    useWorkspace.getState().featureAssignments["panes-v2"],
    undefined,
  );
});

test("workspace: unassignFeature on a missing key is a no-op", () => {
  resetStore();
  useWorkspace.getState().unassignFeature("does-not-exist");
  assert.deepEqual(useWorkspace.getState().featureAssignments, {});
});

// ─── archived fold + filter ──────────────────────────────────────────

test("workspace: archivedExpanded starts empty (collapsed by default)", () => {
  resetStore();
  assert.deepEqual(useWorkspace.getState().archivedExpanded, {});
});

test("workspace: toggleArchivedExpanded flips one project's fold", () => {
  resetStore();
  useWorkspace.getState().toggleArchivedExpanded("p1");
  assert.equal(useWorkspace.getState().archivedExpanded["p1"], true);
  useWorkspace.getState().toggleArchivedExpanded("p1");
  assert.equal(useWorkspace.getState().archivedExpanded["p1"], false);
});

test("workspace: toggleArchivedExpanded leaves other projects untouched", () => {
  resetStore();
  useWorkspace.getState().toggleArchivedExpanded("p1");
  useWorkspace.getState().toggleArchivedExpanded("p2");
  useWorkspace.getState().toggleArchivedExpanded("p1");
  const m = useWorkspace.getState().archivedExpanded;
  assert.equal(m["p1"], false);
  assert.equal(m["p2"], true);
});

test("workspace: archived and merged folds are independent maps", () => {
  resetStore();
  useWorkspace.getState().toggleArchivedExpanded("p1");
  assert.deepEqual(useWorkspace.getState().mergedExpanded, {});
  useWorkspace.getState().toggleMergedExpanded("p1");
  assert.equal(useWorkspace.getState().archivedExpanded["p1"], true);
  assert.equal(useWorkspace.getState().mergedExpanded["p1"], true);
});

test("workspace: setStatusFilter accepts archived", () => {
  resetStore();
  useWorkspace.getState().setStatusFilter("archived");
  assert.equal(useWorkspace.getState().statusFilter, "archived");
});

// ─── dock tree actions (Phase 7) ──────────────────────────────────────

test("workspace: openInFocus on empty tree seeds the root leaf and switches view", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInFocus("browser", { url: "https://example.com" }, "Ex");

  const s = useWorkspace.getState();
  assert.equal(s.view, "focus");
  assert.ok(s.dockTree);
  if (s.dockTree && s.dockTree.type === "leaf") {
    assert.equal(s.dockTree.leaf.title, "Ex");
    assert.equal(s.dockTree.leaf.kind, "browser");
  } else {
    assert.fail("expected a leaf root");
  }
  assert.equal(s.lastFocusLeafId, s.dockTree?.id ?? null);
});

test("workspace: openInFocus on non-empty tree merges into tabs at the last-focus leaf", () => {
  resetStore();
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  useWorkspace.getState().openInFocus("browser", { url: "https://b" }, "B");
  const tree = useWorkspace.getState().dockTree;
  assert.ok(tree);
  assert.equal(tree!.type, "tabs");
  if (tree!.type === "tabs") {
    assert.equal(tree.children.length, 2);
    assert.equal(tree.children[0].leaf.title, "A");
    assert.equal(tree.children[1].leaf.title, "B");
    assert.equal(tree.activeIdx, 1);
  }
});

test("workspace: closeLeaf removes a leaf and clears lastFocus when it matches", () => {
  resetStore();
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  const tree = useWorkspace.getState().dockTree;
  assert.ok(tree);
  const leafId = tree!.id;

  useWorkspace.getState().closeLeaf(leafId);
  assert.equal(useWorkspace.getState().dockTree, null);
  assert.equal(useWorkspace.getState().lastFocusLeafId, null);
});

test("workspace: setSplitRatio and setActiveTab flow through dock helpers", () => {
  resetStore();
  // Build: leaf A, then add B at right → row-split [A, B] ratio 0.5.
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  const rootLeaf = useWorkspace.getState().dockTree;
  assert.ok(rootLeaf);
  // Force a right-edge split via moveLeaf-like effect: use dock helper
  // via the store's own move; simplest is a fresh direct set for test.
  useWorkspace.setState({
    dockTree: {
      type: "split",
      id: "sp1",
      dir: "row",
      ratio: 0.5,
      children: [
        rootLeaf!,
        {
          type: "leaf",
          id: "l-b",
          leaf: {
            kind: "browser",
            target: { url: "https://b" },
            title: "B",
          },
        },
      ],
    },
  });

  useWorkspace.getState().setSplitRatio("sp1", 0.3);
  const tree = useWorkspace.getState().dockTree;
  if (tree && tree.type === "split") {
    assert.equal(tree.ratio, 0.3);
  } else {
    assert.fail("expected split root");
  }

  // Clamp path.
  useWorkspace.getState().setSplitRatio("sp1", 0.99);
  const clamped = useWorkspace.getState().dockTree;
  if (clamped && clamped.type === "split") {
    assert.equal(clamped.ratio, 0.9);
  }
});

test("workspace: moveLeaf on empty tree is a no-op", () => {
  resetStore();
  useWorkspace.getState().moveLeaf("nope", "also-nope", "center");
  assert.equal(useWorkspace.getState().dockTree, null);
});

// ─── drawer: clampDrawerWidth (pure helper) ────────────────────────────

test("workspace: clampDrawerWidth passes through an in-range value", () => {
  assert.equal(clampDrawerWidth(500), 500);
});

test("workspace: clampDrawerWidth floors below the minimum (300)", () => {
  assert.equal(clampDrawerWidth(120), 300);
  assert.equal(clampDrawerWidth(-40), 300);
});

test("workspace: clampDrawerWidth caps above the maximum (900)", () => {
  assert.equal(clampDrawerWidth(2000), 900);
});

test("workspace: clampDrawerWidth honors explicit bounds", () => {
  assert.equal(clampDrawerWidth(50, 100, 200), 100);
  assert.equal(clampDrawerWidth(999, 100, 200), 200);
  assert.equal(clampDrawerWidth(150, 100, 200), 150);
});

// ─── drawer: open/close union state ────────────────────────────────────

test("workspace: drawer is null initially", () => {
  resetStore();
  assert.equal(useWorkspace.getState().drawer, null);
});

test("workspace: openFeatureDrawer sets a feature-kind drawer", () => {
  resetStore();
  useWorkspace.getState().openFeatureDrawer("proj-a", "feat-1");
  assert.deepEqual(useWorkspace.getState().drawer, {
    kind: "feature",
    projectId: "proj-a",
    featureId: "feat-1",
  });
});

test("workspace: openTaskDrawer sets a task-kind drawer", () => {
  resetStore();
  useWorkspace.getState().openTaskDrawer("proj-a", "task-9");
  assert.deepEqual(useWorkspace.getState().drawer, {
    kind: "task",
    projectId: "proj-a",
    taskId: "task-9",
  });
});

test("workspace: openTaskDrawer replaces an open feature drawer", () => {
  resetStore();
  useWorkspace.getState().openFeatureDrawer("proj-a", "feat-1");
  useWorkspace.getState().openTaskDrawer("proj-b", "task-2");
  assert.deepEqual(useWorkspace.getState().drawer, {
    kind: "task",
    projectId: "proj-b",
    taskId: "task-2",
  });
});

test("workspace: closeFeatureDrawer clears the drawer regardless of kind", () => {
  resetStore();
  useWorkspace.getState().openTaskDrawer("proj-a", "task-9");
  useWorkspace.getState().closeFeatureDrawer();
  assert.equal(useWorkspace.getState().drawer, null);
});

// ─── drawer: width state + setter clamp ────────────────────────────────

test("workspace: drawerWidth defaults to 430", () => {
  resetStore();
  assert.equal(useWorkspace.getState().drawerWidth, 430);
});

test("workspace: setDrawerWidth stores an in-range width", () => {
  resetStore();
  useWorkspace.getState().setDrawerWidth(560);
  assert.equal(useWorkspace.getState().drawerWidth, 560);
});

test("workspace: setDrawerWidth clamps to the [300, 900] range", () => {
  resetStore();
  useWorkspace.getState().setDrawerWidth(50);
  assert.equal(useWorkspace.getState().drawerWidth, 300);
  useWorkspace.getState().setDrawerWidth(5000);
  assert.equal(useWorkspace.getState().drawerWidth, 900);
});
