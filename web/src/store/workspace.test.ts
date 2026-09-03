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
  clampSidebarWidth,
} from "./workspace";
import { countLeaves, walkLeaves, type DockNode } from "../lib/dock";
import {
  installNavPush,
  leafIdentity,
  withoutNav,
  type NavEntry,
} from "../lib/navBridge";

/** Assert a node is a leaf carrying `title`. */
function assertLeafTitle(node: DockNode, title: string): void {
  assert.equal(node.type, "leaf");
  if (node.type === "leaf") assert.equal(node.leaf.title, title);
}

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
    docks: { focus: null, sidebar: null },
    lastFocusLeafId: null,
    lastSidebarLeafId: null,
    sidebarSection: { ...INITIAL.sidebarSection },
    mobile: INITIAL.mobile,
    streaming: INITIAL.streaming,
    featureAssignments: { ...INITIAL.featureAssignments },
    featureCollapsed: {},
    hiddenProjects: [],
    statusFilter: "all",
    sidebarDockOpen: false,
    drawerWidth: 430,
    sidebarWidth: 250,
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
  assert.equal(useWorkspace.getState().featureAssignments["panes-v2"], "r-b");
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

// ─── group folds ─────────────────────────────────────────────────────
// The archive fold used to be its own `archivedExpanded` map. It now
// shares `featureCollapsed` under sentinel keys, so there is ONE fold
// mechanism on the card: the archive section, each archived bucket, the
// ungrouped bucket and every feature all read and write the same map.

test("workspace: the archive fold shares the feature fold map", () => {
  resetStore();
  const w = () => useWorkspace.getState();

  // The sidebar's Archived chip supplies the DEFAULT (open), and the
  // click still wins — the old implementation removed the toggle from
  // the DOM in that mode instead, leaving no way to fold at all.
  w().toggleFeatureCollapsed("p1", "__archived__", false);
  assert.equal(w().featureCollapsed["p1"]["__archived__"], true);
  w().toggleFeatureCollapsed("p1", "__archived__", false);
  assert.equal(w().featureCollapsed["p1"]["__archived__"], false);
});

// An archived bucket and its live sibling are the SAME feature id on two
// sides of the fold; the archived side is prefixed so folding one cannot
// fold the other.
test("workspace: an archived bucket folds independently of its live feature", () => {
  resetStore();
  const w = () => useWorkspace.getState();

  w().toggleFeatureCollapsed("p1", "auth", false);
  assert.equal(w().featureCollapsed["p1"]["auth"], true);
  assert.equal(w().featureCollapsed["p1"]["__archived__:auth"], undefined);

  w().toggleFeatureCollapsed("p1", "__archived__:auth", false);
  assert.equal(w().featureCollapsed["p1"]["auth"], true);
  assert.equal(w().featureCollapsed["p1"]["__archived__:auth"], true);
});

// ─── per-feature fold ────────────────────────────────────────────────

test("workspace: featureCollapsed starts empty (no stored opinions)", () => {
  resetStore();
  assert.deepEqual(useWorkspace.getState().featureCollapsed, {});
});

// The default is passed IN because the view derives it from the feature's
// lifecycle. A toggle that flipped `!undefined` instead would make the
// first click on an already-folded finished feature a no-op on screen.
test("workspace: the first toggle inverts the DERIVED default, not undefined", () => {
  resetStore();
  const w = () => useWorkspace.getState();

  w().toggleFeatureCollapsed("p1", "open-feat", false);
  assert.equal(w().featureCollapsed["p1"]["open-feat"], true);

  w().toggleFeatureCollapsed("p1", "done-feat", true);
  assert.equal(w().featureCollapsed["p1"]["done-feat"], false);
});

// Once stored, the user's choice outranks the default in both directions —
// a feature they expanded must not silently re-fold when it merges.
test("workspace: a stored fold outranks the default on later toggles", () => {
  resetStore();
  const w = () => useWorkspace.getState();

  w().toggleFeatureCollapsed("p1", "f", true); // stored false
  w().toggleFeatureCollapsed("p1", "f", true); // ignores the default
  assert.equal(w().featureCollapsed["p1"]["f"], true);
});

test("workspace: featureCollapsed keys are scoped per project", () => {
  resetStore();
  const w = () => useWorkspace.getState();

  w().toggleFeatureCollapsed("p1", "shared-id", false);
  assert.equal(w().featureCollapsed["p1"]["shared-id"], true);
  assert.equal(w().featureCollapsed["p2"], undefined);

  w().toggleFeatureCollapsed("p2", "shared-id", false);
  assert.equal(w().featureCollapsed["p1"]["shared-id"], true);
  assert.equal(w().featureCollapsed["p2"]["shared-id"], true);
});

test("workspace: toggling one feature leaves its siblings untouched", () => {
  resetStore();
  const w = () => useWorkspace.getState();

  w().toggleFeatureCollapsed("p1", "a", false);
  w().toggleFeatureCollapsed("p1", "b", false);
  w().toggleFeatureCollapsed("p1", "a", false);

  assert.equal(w().featureCollapsed["p1"]["a"], false);
  assert.equal(w().featureCollapsed["p1"]["b"], true);
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
  assert.ok(s.docks.focus);
  if (s.docks.focus && s.docks.focus.type === "leaf") {
    assert.equal(s.docks.focus.leaf.title, "Ex");
    assert.equal(s.docks.focus.leaf.kind, "browser");
  } else {
    assert.fail("expected a leaf root");
  }
  assert.equal(s.lastFocusLeafId, s.docks.focus?.id ?? null);
});

test("workspace: openInFocus on non-empty tree merges into tabs at the last-focus leaf", () => {
  resetStore();
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  useWorkspace.getState().openInFocus("browser", { url: "https://b" }, "B");
  const tree = useWorkspace.getState().docks.focus;
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
  const tree = useWorkspace.getState().docks.focus;
  assert.ok(tree);
  const leafId = tree!.id;

  useWorkspace.getState().closeLeaf(leafId);
  assert.equal(useWorkspace.getState().docks.focus, null);
  assert.equal(useWorkspace.getState().lastFocusLeafId, null);
});

test("workspace: setSplitRatio and setActiveTab flow through dock helpers", () => {
  resetStore();
  // Build: leaf A, then add B at right → row-split [A, B] ratio 0.5.
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  const rootLeaf = useWorkspace.getState().docks.focus;
  assert.ok(rootLeaf);
  // Force a right-edge split via moveLeaf-like effect: use dock helper
  // via the store's own move; simplest is a fresh direct set for test.
  useWorkspace.setState({
    docks: {
      focus: {
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
      sidebar: null,
    },
  });

  useWorkspace.getState().setSplitRatio("sp1", 0.3);
  const tree = useWorkspace.getState().docks.focus;
  if (tree && tree.type === "split") {
    assert.equal(tree.ratio, 0.3);
  } else {
    assert.fail("expected split root");
  }

  // Clamp path.
  useWorkspace.getState().setSplitRatio("sp1", 0.99);
  const clamped = useWorkspace.getState().docks.focus;
  if (clamped && clamped.type === "split") {
    assert.equal(clamped.ratio, 0.9);
  }
});

test("workspace: moveLeaf on empty tree is a no-op", () => {
  resetStore();
  useWorkspace.getState().moveLeaf("nope", "also-nope", "center");
  assert.equal(useWorkspace.getState().docks.focus, null);
});

// ─── target-aware insertion (the drop path) ───────────────────────────
//
// `openIn*` merges into the LAST-TOUCHED pane, which is right for menus
// and the command palette and wrong for a drop, where the user picked
// the pane and the edge by hand. `openIn*At` is the drop path: one tree
// write at an explicit target. It replaced a broken round-trip in
// PaneLeaf (openIn*, then moveLeaf using `lastXLeafId` as the new leaf's
// id — an id openIn* never set, so it dragged a pre-existing pane to the
// chosen edge or no-opped entirely).

/** Titles of every leaf in a dock, in DFS order. */
function focusTitles(): string[] {
  const tree = useWorkspace.getState().docks.focus;
  if (!tree) return [];
  const out: string[] = [];
  walkLeaves(tree, (l) => out.push(l.title));
  return out;
}

/** Seed the Focus dock with `split(row)[A, B]` and return both leaf ids. */
function seedFocusSplit(): { aId: string; bId: string } {
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  const aId = useWorkspace.getState().docks.focus!.id;
  useWorkspace
    .getState()
    .openInFocusAt("browser", { url: "https://b" }, "B", aId, "right");
  const tree = useWorkspace.getState().docks.focus;
  if (!tree || tree.type !== "split") throw new Error("expected a split root");
  return { aId, bId: tree.children[1].id };
}

test("workspace: openInFocusAt seeds the root leaf on an empty dock", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInFocusAt(
      "browser",
      { url: "https://a" },
      "A",
      "no-such-leaf",
      "left",
    );
  const s = useWorkspace.getState();
  assert.equal(s.view, "focus");
  assert.ok(s.docks.focus && s.docks.focus.type === "leaf");
  assert.equal(s.lastFocusLeafId, s.docks.focus!.id);
});

test("workspace: openInFocusAt places the NEW leaf at the requested edge of the requested pane", () => {
  // Drop on B's LEFT: the new pane must land between A and B, and A
  // must not move. The old round-trip moved A here instead.
  resetStore();
  const { aId, bId } = seedFocusSplit();
  useWorkspace
    .getState()
    .openInFocusAt("browser", { url: "https://n" }, "NEW", bId, "left");

  assert.deepEqual(focusTitles(), ["A", "NEW", "B"]);
  const tree = useWorkspace.getState().docks.focus!;
  if (tree.type !== "split") return assert.fail("expected split root");
  // A keeps its identity and its slot; only B's slot was subdivided.
  assert.equal(tree.children[0].id, aId);
  const inner = tree.children[1];
  assert.equal(inner.type, "split");
  if (inner.type !== "split") return;
  assert.equal(inner.dir, "row");
  assert.equal(inner.children[1].id, bId);
});

test("workspace: openInFocusAt honours each of the four split edges", () => {
  const cases = [
    { edge: "left" as const, dir: "row", order: ["A", "NEW", "B"] },
    { edge: "right" as const, dir: "row", order: ["A", "B", "NEW"] },
    { edge: "top" as const, dir: "col", order: ["A", "NEW", "B"] },
    { edge: "bottom" as const, dir: "col", order: ["A", "B", "NEW"] },
  ];
  for (const c of cases) {
    resetStore();
    const { bId } = seedFocusSplit();
    useWorkspace
      .getState()
      .openInFocusAt("browser", { url: "https://n" }, "NEW", bId, c.edge);

    assert.deepEqual(focusTitles(), c.order, `edge ${c.edge}`);
    const tree = useWorkspace.getState().docks.focus!;
    if (tree.type !== "split") return assert.fail("expected split root");
    const inner = tree.children[1];
    assert.equal(inner.type, "split", `edge ${c.edge}: B's slot should split`);
    if (inner.type === "split")
      assert.equal(inner.dir, c.dir, `edge ${c.edge}`);
  }
});

test("workspace: openInFocusAt center merges into the pane that was dropped on, not the last-touched one", () => {
  // The whole point of the target-aware action: `lastFocusLeafId` here
  // is A (it opened last), but the drop was on B.
  resetStore();
  const { aId, bId } = seedFocusSplit();
  useWorkspace.getState().setLastFocusLeaf(aId);
  useWorkspace
    .getState()
    .openInFocusAt("browser", { url: "https://n" }, "NEW", bId, "center");

  const tree = useWorkspace.getState().docks.focus!;
  if (tree.type !== "split") return assert.fail("expected split root");
  assert.equal(tree.children[0].id, aId, "A must stay a plain leaf");
  assert.equal(tree.children[0].type, "leaf");
  const tabs = tree.children[1];
  assert.equal(tabs.type, "tabs", "B is the pane that gained a tab");
  if (tabs.type === "tabs") {
    assert.equal(tabs.children[0].id, bId);
    assert.equal(tabs.children[1].leaf.title, "NEW");
    assert.equal(tabs.activeIdx, 1);
  }
});

test("workspace: openInFocusAt records the new leaf id as the drop-target hint", () => {
  resetStore();
  const { bId } = seedFocusSplit();
  useWorkspace
    .getState()
    .openInFocusAt("browser", { url: "https://n" }, "NEW", bId, "right");

  const s = useWorkspace.getState();
  const tree = s.docks.focus!;
  if (tree.type !== "split") return assert.fail("expected split root");
  const inner = tree.children[1];
  if (inner.type !== "split") return assert.fail("expected inner split");
  assert.equal(s.lastFocusLeafId, inner.children[1].id);
});

test("workspace: openInFocusAt falls back to the last-touched rule when the target is gone", () => {
  // A close can race a drop. The item must still land somewhere, and
  // the existing tree must survive intact.
  resetStore();
  const { aId } = seedFocusSplit();
  useWorkspace.getState().setLastFocusLeaf(aId);
  useWorkspace
    .getState()
    .openInFocusAt(
      "browser",
      { url: "https://n" },
      "NEW",
      "leaf_closed",
      "left",
    );

  assert.deepEqual(focusTitles().sort(), ["A", "B", "NEW"]);
  const tree = useWorkspace.getState().docks.focus!;
  if (tree.type !== "split") return assert.fail("expected split root");
  // Fell back to "merge as a tab at the last-touched pane" (A), and did
  // NOT replace the whole tree with a bare root leaf.
  assert.equal(tree.children[0].type, "tabs");
});

test("workspace: openInSidebarAt targets the sidebar dock and opens the panel", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar("task-detail", { projectId: "p1", taskId: "t1" }, "T1");
  const t1 = useWorkspace.getState().docks.sidebar!.id;
  useWorkspace
    .getState()
    .openInSidebarAt(
      "feature-detail",
      { projectId: "p1", featureId: "f1" },
      "F1",
      t1,
      "bottom",
    );

  const s = useWorkspace.getState();
  assert.equal(s.sidebarDockOpen, true);
  const tree = s.docks.sidebar!;
  assert.equal(tree.type, "split");
  if (tree.type === "split") {
    assert.equal(tree.dir, "col");
    assert.equal(tree.children[0].id, t1);
    assert.equal(tree.children[1].type, "leaf");
  }
  // The Focus dock is a separate tree.
  assert.equal(s.docks.focus, null);
});

test("workspace: openInSidebarAt lands on a pane that lives inside a tab group", () => {
  // The side panel's normal state after item two is a tab strip, and
  // drop zones there carry the ACTIVE TAB's leaf id. Every drop was
  // silently discarded before the dock-layer fix.
  resetStore();
  const open = useWorkspace.getState().openInSidebar;
  open("task-detail", { projectId: "p", taskId: "t1" }, "T1");
  open("task-detail", { projectId: "p", taskId: "t2" }, "T2");
  const strip = useWorkspace.getState().docks.sidebar!;
  assert.equal(strip.type, "tabs");
  if (strip.type !== "tabs") return;
  const activeTabId = strip.children[strip.activeIdx].id;

  useWorkspace
    .getState()
    .openInSidebarAt("logs", { taskId: "t3" }, "L3", activeTabId, "bottom");

  const tree = useWorkspace.getState().docks.sidebar!;
  const out: string[] = [];
  walkLeaves(tree, (l) => out.push(l.title));
  assert.deepEqual(out, ["T1", "T2", "L3"]);
  assert.equal(tree.type, "split");
  if (tree.type === "split") {
    assert.equal(tree.dir, "col");
    // The whole strip is one side of the split; the tabs survive.
    assert.equal(tree.children[0].id, strip.id);
    assertLeafTitle(tree.children[1], "L3");
  }
});

// ─── regression: openIn* must record the leaf it just created ─────────

test("workspace: openInFocus records the newly-created leaf as the drop-target hint", () => {
  // Only the empty-tree branches used to set this. On a populated dock
  // the hint stayed pinned to whatever pane was last moused down, which
  // is what made PaneLeaf's open-then-move round-trip grab the wrong id.
  resetStore();
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  useWorkspace.getState().openInFocus("browser", { url: "https://b" }, "B");

  const s = useWorkspace.getState();
  const tree = s.docks.focus!;
  assert.equal(tree.type, "tabs");
  if (tree.type !== "tabs") return;
  assert.equal(
    s.lastFocusLeafId,
    tree.children[1].id,
    "the hint should name B, the leaf just opened",
  );
});

test("workspace: a third and fourth openInSidebar still land once the dock is a tab strip", () => {
  // Regression: `pickDropTarget` returned a leaf INSIDE the strip and
  // the dock layer discarded the insertion, so opening anything into a
  // populated side panel silently did nothing from the third item on.
  resetStore();
  const open = useWorkspace.getState().openInSidebar;
  open("task-detail", { projectId: "p", taskId: "t1" }, "T1");
  open("task-detail", { projectId: "p", taskId: "t2" }, "T2");
  open("task-detail", { projectId: "p", taskId: "t3" }, "T3");
  open("task-detail", { projectId: "p", taskId: "t4" }, "T4");

  const tree = useWorkspace.getState().docks.sidebar!;
  const out: string[] = [];
  walkLeaves(tree, (l) => out.push(l.title));
  assert.deepEqual(out, ["T1", "T2", "T3", "T4"]);
});

// ─── sidebar dock tree actions (mirror the Focus pair) ─────────────────

test("workspace: openInSidebar on empty tree seeds the root leaf and opens the panel", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar("task-detail", { projectId: "p1", taskId: "t1" }, "T1");

  const s = useWorkspace.getState();
  assert.equal(s.sidebarDockOpen, true);
  assert.ok(s.docks.sidebar);
  if (s.docks.sidebar && s.docks.sidebar.type === "leaf") {
    assert.equal(s.docks.sidebar.leaf.title, "T1");
    assert.equal(s.docks.sidebar.leaf.kind, "task-detail");
  } else {
    assert.fail("expected a leaf root");
  }
  assert.equal(s.lastSidebarLeafId, s.docks.sidebar?.id ?? null);
  // The Focus dock is untouched.
  assert.equal(s.docks.focus, null);
});

test("workspace: openInSidebar on non-empty tree merges into tabs", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar("task-detail", { projectId: "p1", taskId: "t1" }, "T1");
  useWorkspace
    .getState()
    .openInSidebar(
      "feature-detail",
      { projectId: "p1", featureId: "f1" },
      "F1",
    );
  const tree = useWorkspace.getState().docks.sidebar;
  assert.ok(tree);
  assert.equal(tree!.type, "tabs");
  if (tree!.type === "tabs") {
    assert.equal(tree.children.length, 2);
    assert.equal(tree.children[0].leaf.title, "T1");
    assert.equal(tree.children[1].leaf.title, "F1");
  }
});

test("workspace: closeSidebarLeaf removes a leaf, clears lastSidebarLeaf, does not touch Focus", () => {
  resetStore();
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  useWorkspace
    .getState()
    .openInSidebar("task-detail", { projectId: "p1", taskId: "t1" }, "T1");
  const sidebarLeafId = useWorkspace.getState().docks.sidebar!.id;

  useWorkspace.getState().closeSidebarLeaf(sidebarLeafId);
  const s = useWorkspace.getState();
  assert.equal(s.docks.sidebar, null);
  assert.equal(s.lastSidebarLeafId, null);
  // Focus dock survives untouched.
  assert.ok(s.docks.focus);
});

test("workspace: moveSidebarLeaf on empty tree is a no-op", () => {
  resetStore();
  useWorkspace.getState().moveSidebarLeaf("nope", "also-nope", "center");
  assert.equal(useWorkspace.getState().docks.sidebar, null);
});

test("workspace: setSidebarSplitRatio flows through the dock helper and does not touch Focus", () => {
  resetStore();
  useWorkspace.getState().openInFocus("browser", { url: "https://a" }, "A");
  const focusBefore = useWorkspace.getState().docks.focus;
  useWorkspace
    .getState()
    .openInSidebar("task-detail", { projectId: "p1", taskId: "t1" }, "T1");
  const rootLeaf = useWorkspace.getState().docks.sidebar;
  assert.ok(rootLeaf);
  useWorkspace.setState((s) => ({
    docks: {
      ...s.docks,
      sidebar: {
        type: "split",
        id: "sp-sidebar",
        dir: "row",
        ratio: 0.5,
        children: [
          rootLeaf!,
          {
            type: "leaf",
            id: "l-sidebar-b",
            leaf: { kind: "task-detail", target: {}, title: "B" },
          },
        ],
      },
    },
  }));

  useWorkspace.getState().setSidebarSplitRatio("sp-sidebar", 0.3);
  const tree = useWorkspace.getState().docks.sidebar;
  if (tree && tree.type === "split") {
    assert.equal(tree.ratio, 0.3);
  } else {
    assert.fail("expected split root");
  }
  // Focus dock is a completely separate tree.
  assert.equal(useWorkspace.getState().docks.focus, focusBefore);
});

// ─── sidebarDockOpen: manual pin/collapse ──────────────────────────────

test("workspace: sidebarDockOpen defaults to false", () => {
  resetStore();
  assert.equal(useWorkspace.getState().sidebarDockOpen, false);
});

test("workspace: toggleSidebarDockOpen flips the flag", () => {
  resetStore();
  useWorkspace.getState().toggleSidebarDockOpen();
  assert.equal(useWorkspace.getState().sidebarDockOpen, true);
  useWorkspace.getState().toggleSidebarDockOpen();
  assert.equal(useWorkspace.getState().sidebarDockOpen, false);
});

test("workspace: closing the sidebar dock manually preserves docks.sidebar", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar("task-detail", { projectId: "p1", taskId: "t1" }, "T1");
  assert.equal(useWorkspace.getState().sidebarDockOpen, true);
  const treeBefore = useWorkspace.getState().docks.sidebar;

  useWorkspace.getState().setSidebarDockOpen(false);
  const s = useWorkspace.getState();
  assert.equal(s.sidebarDockOpen, false);
  // The layout survives the close — reopening shows the same tree.
  assert.equal(s.docks.sidebar, treeBefore);

  useWorkspace.getState().setSidebarDockOpen(true);
  assert.equal(useWorkspace.getState().docks.sidebar, treeBefore);
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

// ─── sidebar dock: openInSidebar covers every leaf kind ────────────────
// The old single-item `DrawerState` union (`openFeatureDrawer` /
// `openTaskDrawer` / `openSessionDrawer` / `openDrawerFromDrag`) is
// retired — every one of its call sites now opens a leaf into
// `docks.sidebar` via `openInSidebar`, exercised generically above and
// per-kind below.

test("workspace: openInSidebar(task-detail) opens a task-detail leaf", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar(
      "task-detail",
      { projectId: "proj-a", taskId: "task-9" },
      "Task 9",
    );
  const tree = useWorkspace.getState().docks.sidebar;
  assert.ok(tree && tree.type === "leaf");
  if (tree && tree.type === "leaf") {
    assert.equal(tree.leaf.kind, "task-detail");
    assert.deepEqual(tree.leaf.target, {
      projectId: "proj-a",
      taskId: "task-9",
    });
  }
});

test("workspace: openInSidebar(feature-detail) opens a feature-detail leaf", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar(
      "feature-detail",
      { projectId: "proj-a", featureId: "feat-1" },
      "Feat 1",
    );
  const tree = useWorkspace.getState().docks.sidebar;
  assert.ok(tree && tree.type === "leaf");
  if (tree && tree.type === "leaf") {
    assert.equal(tree.leaf.kind, "feature-detail");
    assert.deepEqual(tree.leaf.target, {
      projectId: "proj-a",
      featureId: "feat-1",
    });
  }
});

test("workspace: openInSidebar(entry) opens an entry leaf with the raw target", () => {
  resetStore();
  useWorkspace.getState().openInSidebar("entry", { path: "notes/x.md" });
  const tree = useWorkspace.getState().docks.sidebar;
  assert.ok(tree && tree.type === "leaf");
  if (tree && tree.type === "leaf") {
    assert.equal(tree.leaf.kind, "entry");
    assert.deepEqual(tree.leaf.target, { path: "notes/x.md" });
  }
});

test("workspace: openInSidebar(session) opens a session leaf wrapping the ref", () => {
  resetStore();
  const ref = {
    mode: "live" as const,
    runner_id: "r1",
    instance_id: "i1",
    session_id: "s1",
  };
  useWorkspace.getState().openInSidebar("session", { ref });
  const tree = useWorkspace.getState().docks.sidebar;
  assert.ok(tree && tree.type === "leaf");
  if (tree && tree.type === "leaf") {
    assert.equal(tree.leaf.kind, "session");
    assert.deepEqual(tree.leaf.target, { ref });
  }
});

test("workspace: opening a second item into the sidebar adds it alongside the first, not replacing it", () => {
  // The whole point of the generalization: two dropped/opened items
  // coexist (as tabs, here) instead of one clobbering the other like
  // the old single-item drawer did.
  resetStore();
  useWorkspace
    .getState()
    .openInSidebar(
      "feature-detail",
      { projectId: "proj-a", featureId: "feat-1" },
      "Feat 1",
    );
  useWorkspace
    .getState()
    .openInSidebar(
      "task-detail",
      { projectId: "proj-b", taskId: "task-2" },
      "Task 2",
    );
  const tree = useWorkspace.getState().docks.sidebar;
  assert.ok(tree);
  assert.equal(tree!.type, "tabs");
  if (tree!.type === "tabs") {
    assert.equal(tree.children.length, 2);
    assert.equal(tree.children[0].leaf.kind, "feature-detail");
    assert.equal(tree.children[1].leaf.kind, "task-detail");
  }
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

// ─── sidebar: clampSidebarWidth (pure helper) ──────────────────────────

test("workspace: clampSidebarWidth passes through an in-range value", () => {
  assert.equal(clampSidebarWidth(300), 300);
});

test("workspace: clampSidebarWidth floors below the minimum (180)", () => {
  assert.equal(clampSidebarWidth(50), 180);
  assert.equal(clampSidebarWidth(-40), 180);
});

test("workspace: clampSidebarWidth caps above the maximum (480)", () => {
  assert.equal(clampSidebarWidth(900), 480);
});

test("workspace: clampSidebarWidth honors explicit bounds", () => {
  assert.equal(clampSidebarWidth(50, 100, 200), 100);
  assert.equal(clampSidebarWidth(999, 100, 200), 200);
  assert.equal(clampSidebarWidth(150, 100, 200), 150);
});

// ─── sidebar: width state + setter clamp ───────────────────────────────

test("workspace: sidebarWidth defaults to 250", () => {
  resetStore();
  assert.equal(useWorkspace.getState().sidebarWidth, 250);
});

test("workspace: setSidebarWidth stores an in-range width", () => {
  resetStore();
  useWorkspace.getState().setSidebarWidth(320);
  assert.equal(useWorkspace.getState().sidebarWidth, 320);
});

test("workspace: setSidebarWidth clamps to the [180, 480] range", () => {
  resetStore();
  useWorkspace.getState().setSidebarWidth(50);
  assert.equal(useWorkspace.getState().sidebarWidth, 180);
  useWorkspace.getState().setSidebarWidth(5000);
  assert.equal(useWorkspace.getState().sidebarWidth, 480);
});

// ─── forgetProject: what a DELETED project leaves behind ──────────────
//
// hideProject is a view preference about something that still exists;
// forgetProject runs after the project is gone from the server. The
// difference that matters is the dock: a leaf pointed at a deleted project
// renders an error state forever, and the dock tree is persisted, so it
// survives a reload.

test("workspace: forgetProject closes focus panes targeting the project", () => {
  resetStore();
  const w = () => useWorkspace.getState();
  w().openInFocus("task-detail", { projectId: "shop" }, "shop");
  w().openInFocus("task-detail", { projectId: "warehouse" }, "warehouse");

  w().forgetProject("shop");

  const titles: string[] = [];
  const tree = w().docks.focus;
  if (tree) walkLeaves(tree, (leaf) => void titles.push(leaf.title));
  assert.deepEqual(titles, ["warehouse"]);
});

test("workspace: forgetProject sweeps the sidebar dock too", () => {
  // A leaf can be dragged between docks, so sweeping only `focus` would
  // leave the dead pane alive in the other column.
  resetStore();
  const w = () => useWorkspace.getState();
  w().openInSidebar("task-detail", { projectId: "shop" }, "shop");

  w().forgetProject("shop");

  assert.equal(w().docks.sidebar, null);
});

test("workspace: forgetProject leaves unrelated panes untouched", () => {
  resetStore();
  const w = () => useWorkspace.getState();
  w().openInFocus("runners", {}, "Runners");

  w().forgetProject("shop");

  const tree = w().docks.focus;
  assert.ok(tree);
  assertLeafTitle(tree, "Runners");
});

test("workspace: forgetProject drops the project from hiddenProjects", () => {
  // Otherwise a project recreated under the same name comes back
  // invisible, which reads as the delete having half-failed.
  resetStore();
  const w = () => useWorkspace.getState();
  w().hideProject("shop");
  w().hideProject("warehouse");

  w().forgetProject("shop");

  assert.deepEqual(w().hiddenProjects, ["warehouse"]);
});

// The nesting is what makes this a one-liner in forgetProject: a
// `${projectId}:${featureId}` composite key would survive `omitKey` and
// leak every fold of a deleted project into a project recreated under the
// same name.
test("workspace: forgetProject drops the project's expansion state", () => {
  resetStore();
  const w = () => useWorkspace.getState();
  w().toggleFeatureCollapsed("shop", "feat-a", false);
  w().toggleFeatureCollapsed("shop", "feat-b", false);
  w().toggleFeatureCollapsed("shop", "__archived__", false);
  w().toggleFeatureCollapsed("warehouse", "feat-a", false);

  w().forgetProject("shop");

  assert.deepEqual(Object.keys(w().featureCollapsed), ["warehouse"]);
});

test("workspace: forgetProject on an unknown project is a no-op", () => {
  resetStore();
  const w = () => useWorkspace.getState();
  w().openInFocus("task-detail", { projectId: "shop" }, "shop");
  const before = w().docks.focus;

  w().forgetProject("nonexistent");

  assert.equal(w().docks.focus, before);
});

// ─── openInFocusGroup ─────────────────────────────────────────────────

/** Titles of every leaf in a dock, in tree order. */
function dockTitles(tree: DockNode | null): string[] {
  if (!tree) return [];
  const out: string[] = [];
  walkLeaves(tree, (l) => out.push(l.title));
  return out;
}

test("openInFocusGroup: an empty dock becomes exactly the group", () => {
  resetStore();
  useWorkspace.getState().openInFocusGroup([
    { kind: "session", target: { ref: { mode: "live" } }, title: "Session" },
    { kind: "logs", target: { projectId: "p", taskId: "t1" }, title: "Logs" },
  ]);
  const s = useWorkspace.getState();
  assert.deepEqual(dockTitles(s.docks.focus), ["Session", "Logs"]);
  assert.equal(s.view, "focus");
});

test("openInFocusGroup: panes land SIDE BY SIDE, not stacked as tabs", () => {
  // This is the whole reason the action exists — two openInFocus calls
  // merge into one pane as tabs, which is the opposite of a watch
  // layout.
  resetStore();
  useWorkspace.getState().openInFocusGroup([
    { kind: "session", target: {}, title: "Session" },
    { kind: "logs", target: {}, title: "Logs" },
  ]);
  const tree = useWorkspace.getState().docks.focus!;
  assert.equal(tree.type, "split");
});

test("openInFocusGroup: adds beside an existing layout instead of replacing it", () => {
  resetStore();
  useWorkspace.getState().openInFocus("runners", {}, "Runners");
  useWorkspace.getState().openInFocusGroup([
    { kind: "session", target: {}, title: "Session" },
    { kind: "logs", target: {}, title: "Logs" },
  ]);
  const titles = dockTitles(useWorkspace.getState().docks.focus);
  // The pane the user already had is still there.
  assert.ok(titles.includes("Runners"), titles.join(","));
  assert.equal(titles.length, 3);
});

test("openInFocusGroup: a single item still works, and an empty list is a no-op", () => {
  resetStore();
  useWorkspace
    .getState()
    .openInFocusGroup([{ kind: "logs", target: {}, title: "Logs" }]);
  assert.equal(countLeaves(useWorkspace.getState().docks.focus), 1);

  const before = useWorkspace.getState().docks.focus;
  useWorkspace.getState().openInFocusGroup([]);
  assert.equal(useWorkspace.getState().docks.focus, before);
});

test("openInFocusGroup: points the drop hint at the group it just made", () => {
  resetStore();
  useWorkspace.getState().openInFocusGroup([
    { kind: "session", target: {}, title: "Session" },
    { kind: "logs", target: {}, title: "Logs" },
  ]);
  const { docks, lastFocusLeafId } = useWorkspace.getState();
  const ids: string[] = [];
  walkLeaves(docks.focus!, (_l, id) => ids.push(id));
  assert.ok(lastFocusLeafId && ids.includes(lastFocusLeafId));
});

// ─── sendLeafToOtherDock ──────────────────────────────────────────────

/** The id of the first leaf in a dock. */
function firstLeafId(tree: DockNode | null): string {
  let id = "";
  if (tree)
    walkLeaves(tree, (_l, leafId) => {
      if (!id) id = leafId;
    });
  return id;
}

test("sendLeafToOtherDock: focus → sidebar moves the pane, keeping its content", () => {
  resetStore();
  useWorkspace.getState().openInFocus("logs", { taskId: "t1" }, "Logs t1");
  const id = firstLeafId(useWorkspace.getState().docks.focus);

  useWorkspace.getState().sendLeafToOtherDock(id, "focus");

  const s = useWorkspace.getState();
  assert.equal(s.docks.focus, null, "left the source dock");
  assert.deepEqual(dockTitles(s.docks.sidebar), ["Logs t1"]);
  // The destination column is opened, or the move would be invisible.
  assert.equal(s.sidebarDockOpen, true);
});

test("sendLeafToOtherDock: sidebar → focus takes the user to Focus", () => {
  resetStore();
  useWorkspace.getState().openInSidebar("session", {}, "Session");
  const id = firstLeafId(useWorkspace.getState().docks.sidebar);

  useWorkspace.getState().sendLeafToOtherDock(id, "sidebar");

  const s = useWorkspace.getState();
  assert.equal(s.docks.sidebar, null);
  assert.deepEqual(dockTitles(s.docks.focus), ["Session"]);
  assert.equal(s.view, "focus");
});

test("sendLeafToOtherDock: never duplicates or loses a pane", () => {
  resetStore();
  const ws = useWorkspace.getState();
  ws.openInFocus("logs", {}, "A");
  ws.openInFocus("logs", {}, "B");
  assert.equal(countLeaves(useWorkspace.getState().docks.focus), 2);

  const id = firstLeafId(useWorkspace.getState().docks.focus);
  useWorkspace.getState().sendLeafToOtherDock(id, "focus");

  const s = useWorkspace.getState();
  assert.equal(
    countLeaves(s.docks.focus) + countLeaves(s.docks.sidebar),
    2,
    "total pane count is conserved",
  );
  assert.equal(countLeaves(s.docks.sidebar), 1);
});

test("sendLeafToOtherDock: an unknown leaf id changes nothing", () => {
  resetStore();
  useWorkspace.getState().openInFocus("logs", {}, "A");
  const before = useWorkspace.getState().docks;
  useWorkspace.getState().sendLeafToOtherDock("leaf_nope", "focus");
  assert.equal(useWorkspace.getState().docks, before);
});

// ─── closeCurrentLeaf (the ⌃W / ⇧⌘X target) ──────────────────────────

const w = () => useWorkspace.getState();

test("workspace: closeCurrentLeaf closes the pane in the dock on screen", () => {
  resetStore();
  w().openInFocus("browser", { url: "https://a" }, "A");
  w().setView("focus");
  assert.ok(w().docks.focus);

  w().closeCurrentLeaf();
  assert.equal(w().docks.focus, null, "the focus pane should be gone");
});

// The gate that stops a chord from deleting something invisible. `docks`
// is persisted, so an ungated shortcut pressed in Overview would remove a
// Focus pane the user cannot see — and it would still be gone on reload.
test("workspace: closeCurrentLeaf refuses a dock that is not on screen", () => {
  resetStore();
  w().openInFocus("browser", { url: "https://a" }, "A");
  w().setView("overview"); // Focus dock is not visible here
  const before = w().docks.focus;
  assert.ok(before);

  w().closeCurrentLeaf();
  assert.equal(
    w().docks.focus,
    before,
    "a pane the user cannot see must not be closable by a chord",
  );
});

test("workspace: closeCurrentLeaf closes a sidebar pane when the sidebar is open", () => {
  resetStore();
  w().openInSidebar("browser", { url: "https://s" }, "S");
  assert.ok(w().docks.sidebar);
  assert.equal(w().sidebarDockOpen, true);

  w().closeCurrentLeaf();
  assert.equal(w().docks.sidebar, null);
  // Emptying the sidebar collapses the column, so it cannot linger as an
  // empty strip the shortcut can no longer clear.
  assert.equal(w().sidebarDockOpen, false);
});

// Both docks populated: the one the user touched last wins.
test("workspace: closeCurrentLeaf prefers the dock touched most recently", () => {
  resetStore();
  w().openInFocus("browser", { url: "https://f" }, "F");
  w().setView("focus");
  w().openInSidebar("browser", { url: "https://s" }, "S");
  // openInSidebar was last, so lastActiveDock is the sidebar.
  assert.equal(w().lastActiveDock, "sidebar");

  w().closeCurrentLeaf();
  assert.equal(w().docks.sidebar, null, "the sidebar pane should close");
  assert.ok(w().docks.focus, "the focus pane must be untouched");
});

// A stale hint is reachable in normal use. It must close the frontmost
// pane rather than silently doing nothing.
test("workspace: closeCurrentLeaf falls back when the hint is stale", () => {
  resetStore();
  w().openInFocus("browser", { url: "https://a" }, "A");
  w().setView("focus");
  useWorkspace.setState({ lastFocusLeafId: "no-such-leaf" });

  w().closeCurrentLeaf();
  assert.equal(w().docks.focus, null, "a stale hint must not wedge the verb");
});

test("workspace: closeCurrentLeaf is a no-op with nothing open", () => {
  resetStore();
  w().setView("focus");
  w().closeCurrentLeaf();
  assert.equal(w().docks.focus, null);
  assert.equal(w().docks.sidebar, null);
});

// Clicking a tab is the clearest statement of "this is the pane I am in",
// and it updated nothing before — so the shortcut closed the tab the user
// had just clicked AWAY from.
test("workspace: clicking a tab makes it the close target", () => {
  resetStore();
  w().openInFocus("browser", { url: "https://a" }, "A");
  w().setView("focus");
  const first = w().docks.focus!.id;
  // Stack a second pane as a tab in the same group.
  w().openInFocusAt("browser", { url: "https://b" }, "B", first, "center");

  const tree = w().docks.focus!;
  assert.equal(tree.type, "tabs", "expected a tabs group");
  if (tree.type !== "tabs") return;

  // Select the FIRST tab explicitly.
  w().setActiveTab(tree.id, 0);
  assert.equal(
    w().lastFocusLeafId,
    tree.children[0].id,
    "selecting a tab must become the close target",
  );

  w().closeCurrentLeaf();
  const after = w().docks.focus;
  assert.ok(after);
  // The tab we selected is gone; the other one survives.
  const ids: string[] = [];
  walkLeaves(after!, (_l, id) => ids.push(id));
  assert.ok(
    !ids.includes(tree.children[0].id),
    "the selected tab should have closed",
  );
  assert.ok(ids.includes(tree.children[1].id), "the other tab should remain");
});

// ─── navigation history pushes ───────────────────────────────────────

// Opening a pane used to be invisible to the browser, so Back had nothing
// to pop. The push happens at ONE chokepoint in the store rather than at
// the 43 call sites that open panes.
test("workspace: opening a pane records a navigation", () => {
  resetStore();
  const seen: NavEntry[] = [];
  installNavPush((e) => seen.push(e));

  w().openInFocus("task-detail", { projectId: "p", taskId: "t1" }, "T1");

  assert.equal(seen.length, 1, "opening a pane should record one navigation");
  assert.equal(seen[0].view, "focus");
  assert.equal(seen[0].leaf?.dock, "focus");
  assert.equal(seen[0].leaf?.kind, "task-detail");
  assert.deepEqual(seen[0].leaf?.target, { projectId: "p", taskId: "t1" });
  installNavPush(null);
});

// The single-click preview path retargets an existing pane instead of
// minting one, so it never reaches the main chokepoint and needs its own.
test("workspace: reusing the sidebar pane still records a navigation", () => {
  resetStore();
  w().openOrReuseInSidebar("task-detail", { projectId: "p", taskId: "a" }, "A");
  const seen: NavEntry[] = [];
  installNavPush((e) => seen.push(e));

  w().openOrReuseInSidebar("task-detail", { projectId: "p", taskId: "b" }, "B");

  assert.equal(seen.length, 1, "the reuse branch must record too");
  assert.deepEqual(seen[0].leaf?.target, { projectId: "p", taskId: "b" });
  installNavPush(null);
});

test("workspace: switching view records a navigation, staying put does not", () => {
  resetStore();
  const seen: NavEntry[] = [];
  installNavPush((e) => seen.push(e));

  w().setView("entries");
  w().setView("entries"); // no change — must not push
  w().setView("focus");

  assert.deepEqual(
    seen.map((e) => e.view),
    ["entries", "focus"],
    "only real view changes are navigations",
  );
  installNavPush(null);
});

// Applying a popped entry calls the same store actions that push. Without
// the guard one Back would append an entry and Forward would be lost.
test("workspace: withoutNav suppresses pushes while a pop is applied", () => {
  resetStore();
  const seen: NavEntry[] = [];
  installNavPush((e) => seen.push(e));

  withoutNav(() => {
    w().setView("focus");
    w().openInFocus("browser", { url: "https://x" }, "X");
  });

  assert.equal(seen.length, 0, "re-applying a popped entry must not push");
  // And the mutations still happened.
  assert.equal(w().view, "focus");
  assert.ok(w().docks.focus);
  installNavPush(null);
});

// The identity function is what lets a popped intent find its pane again.
// It must be order-insensitive, since target objects are built ad hoc.
test("workspace: leafIdentity is stable across key order and kinds", () => {
  assert.equal(
    leafIdentity("task-detail", { projectId: "p", taskId: "t" }),
    leafIdentity("task-detail", { taskId: "t", projectId: "p" }),
    "key order must not change identity",
  );
  assert.notEqual(
    leafIdentity("task-detail", { projectId: "p", taskId: "t" }),
    leafIdentity("feature-detail", { projectId: "p", taskId: "t" }),
    "different kinds are different panes",
  );
  // Must never throw on the arbitrary JSON coerceDockTree lets through.
  assert.ok(leafIdentity("browser", null));
  assert.ok(leafIdentity("browser", undefined));
});

// ─── closing the last sidebar pane collapses the column ──────────────

// Every close path shares this now — the pane ×, the tab menu's Close and
// the keyboard shortcut all route through makeCloseLeaf — so they cannot
// disagree about what closing the last pane means.
test("workspace: closeSidebarLeaf collapses the column when it empties it", () => {
  resetStore();
  w().openInSidebar("browser", { url: "https://only" }, "Only");
  const leafId = w().docks.sidebar!.id;
  assert.equal(w().sidebarDockOpen, true);

  w().closeSidebarLeaf(leafId);
  assert.equal(w().docks.sidebar, null);
  assert.equal(
    w().sidebarDockOpen,
    false,
    "an empty panel is a blank strip, not a preserved layout",
  );
});

// ...but only when it was the LAST one.
test("workspace: closing one of two sidebar panes leaves the column open", () => {
  resetStore();
  w().openInSidebar("browser", { url: "https://a" }, "A");
  const firstId = w().docks.sidebar!.id;
  w().openInSidebarAt("browser", { url: "https://b" }, "B", firstId, "bottom");
  assert.equal(countLeaves(w().docks.sidebar), 2);

  w().closeSidebarLeaf(firstId);
  assert.equal(countLeaves(w().docks.sidebar), 1);
  assert.equal(
    w().sidebarDockOpen,
    true,
    "the column must stay open while a pane remains",
  );
});

// Emptying the FOCUS dock must not touch the sidebar's visibility gate.
test("workspace: emptying the focus dock does not collapse the sidebar", () => {
  resetStore();
  w().openInSidebar("browser", { url: "https://s" }, "S");
  w().openInFocus("browser", { url: "https://f" }, "F");
  const focusLeaf = w().docks.focus!.id;

  w().closeLeaf(focusLeaf);
  assert.equal(w().docks.focus, null);
  assert.equal(w().sidebarDockOpen, true, "the sidebar is unrelated");
  assert.ok(w().docks.sidebar, "and still has its pane");
});

// Collapsing the column by hand still preserves the layout — that is the
// other direction and must not have changed.
test("workspace: manually collapsing the column keeps its panes", () => {
  resetStore();
  w().openInSidebar("browser", { url: "https://keep" }, "Keep");
  w().setSidebarDockOpen(false);
  assert.equal(w().sidebarDockOpen, false);
  assert.ok(
    w().docks.sidebar,
    "a manual collapse preserves the layout for when it reopens",
  );
});
