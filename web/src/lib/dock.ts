/**
 * panes-v2 dock tree — pure tree operations.
 *
 * Backs the Focus workspace. See `dock.test.ts` for the contract.
 *
 * A `DockNode` is one of:
 *   • leaf   — the actual content pane
 *   • split  — two-or-more children arranged in a row or column
 *   • tabs   — a stack of leaves with an active index
 *
 * All operations are pure and return a new tree — nothing is mutated
 * in place. That keeps zustand's reference-equality change-detection
 * honest so React only re-renders the panes that actually changed.
 *
 * Constraints:
 *   • splits always contain ≥ 1 child; a 1-child split is collapsed
 *     into the child (`removeNode` handles this).
 *   • tabs always contain ≥ 1 leaf; a 0-child tabs is removed.
 *   • ratios are clamped to [0.1, 0.9] so a pane can never collapse
 *     entirely via splitter drag.
 *   • moves that would create a cycle (leaf into itself) are no-ops.
 */

// ─── types ────────────────────────────────────────────────────────────

export interface DockLeaf {
  kind:
    | "task-detail"
    | "feature-detail"
    | "logs"
    | "session"
    | "runners"
    | "browser"
    | "entry"
    | "automation-runs"
    | "automation-detail";
  target: Record<string, unknown>;
  title: string;
}

export type DockNode =
  | { type: "leaf"; id: string; leaf: DockLeaf }
  | {
      type: "split";
      id: string;
      dir: "row" | "col";
      ratio: number;
      children: DockNode[];
    }
  | {
      type: "tabs";
      id: string;
      activeIdx: number;
      children: Array<{ type: "leaf"; id: string; leaf: DockLeaf }>;
    };

export type Edge = "left" | "right" | "top" | "bottom" | "center";

/** A `DockNode` narrowed to the leaf arm. Callers that mint their own
 *  node (so they can keep its id) hand one of these to `addNodeAtEdge`. */
export type DockLeafNode = Extract<DockNode, { type: "leaf" }>;

/**
 * Runtime predicate for `DockLeaf["kind"]`.
 *
 * Drop targets use it to decide whether a drag payload can become a
 * pane at all — the feature→runner "assign" payload cannot. Exported so
 * `PaneLeaf`, `PaneTabs`, `FocusPanes` and `SidebarDock` share ONE list
 * instead of hand-maintained copies, which had already drifted:
 * FocusPanes' copy was missing "feature-detail" and "entry", so those
 * panes could be dropped onto a populated Focus tab but not an empty one.
 */
export function isDockLeafKind(kind: string): kind is DockLeaf["kind"] {
  return (
    kind === "task-detail" ||
    kind === "feature-detail" ||
    kind === "logs" ||
    kind === "session" ||
    kind === "runners" ||
    kind === "browser" ||
    kind === "entry" ||
    kind === "automation-runs" ||
    kind === "automation-detail"
  );
}

// ─── ids ──────────────────────────────────────────────────────────────

/**
 * Monotonically-increasing id counter. IDs are opaque strings; the
 * only guarantee is uniqueness within a single browser tab session.
 * We don't need crypto-strong ids here — the tree is client-only.
 */
let idCounter = 0;

function nextId(prefix: string): string {
  idCounter += 1;
  // Include a random suffix so ids are still distinct if two tabs
  // happen to serialize a persisted tree that collides on counters.
  const rand = Math.random().toString(36).slice(2, 8);
  return `${prefix}_${idCounter}_${rand}`;
}

// ─── constructors ─────────────────────────────────────────────────────

export function newLeafNode(leaf: DockLeaf): DockLeafNode {
  return { type: "leaf", id: nextId("leaf"), leaf };
}

function newSplit(
  dir: "row" | "col",
  children: DockNode[],
  ratio = 0.5,
): DockNode {
  return {
    type: "split",
    id: nextId("split"),
    dir,
    ratio: clampRatio(ratio),
    children,
  };
}

function newTabs(
  children: Array<{ type: "leaf"; id: string; leaf: DockLeaf }>,
  activeIdx: number,
): DockNode {
  return {
    type: "tabs",
    id: nextId("tabs"),
    activeIdx,
    children,
  };
}

function clampRatio(r: number): number {
  if (!Number.isFinite(r)) return 0.5;
  if (r < 0.1) return 0.1;
  if (r > 0.9) return 0.9;
  return r;
}

// ─── traversal ────────────────────────────────────────────────────────

export function walkLeaves(
  tree: DockNode,
  fn: (leaf: DockLeaf, id: string) => void,
): void {
  if (tree.type === "leaf") {
    fn(tree.leaf, tree.id);
    return;
  }
  for (const child of tree.children) walkLeaves(child, fn);
}

export function firstLeaf(tree: DockNode): DockLeaf | null {
  let found: DockLeaf | null = null;
  walkLeaves(tree, (leaf) => {
    if (found === null) found = leaf;
  });
  return found;
}

/**
 * The first leaf of a given kind, with its id — for surfaces that are
 * VIEWERS rather than collections.
 *
 * A detail pane (one automation, say) is a single window onto whichever
 * row you activated: opening five automations should move one pane five
 * times, not leave five tabs to close. Callers pair this with
 * `retargetLeaf` and `enclosingTabsId` to reuse the pane they already
 * have. Panes the user may legitimately want several of at once —
 * sessions, logs — deliberately do NOT use it.
 */
export function findLeafOfKind(
  tree: DockNode,
  kind: DockLeaf["kind"],
): { id: string; leaf: DockLeaf } | null {
  let found: { id: string; leaf: DockLeaf } | null = null;
  walkLeaves(tree, (leaf, id) => {
    if (found === null && leaf.kind === kind) found = { id, leaf };
  });
  return found;
}

/** Point an existing leaf at new content, keeping its id and position. */
export function retargetLeaf(
  tree: DockNode,
  leafId: string,
  leaf: DockLeaf,
): DockNode {
  const info = findNodeInfo(tree, leafId);
  if (!info || info.node.type !== "leaf") return tree;
  return replaceNode(tree, leafId, { ...info.node, leaf });
}

/** The id of the tabs strip holding `leafId`, when it is in one — the
 *  reused pane has to be brought to the front of its strip, or it is
 *  updated invisibly behind whatever tab is showing. */
export function enclosingTabsId(tree: DockNode, leafId: string): string | null {
  const info = findNodeInfo(tree, leafId);
  if (!info || !info.parent || info.parent.type !== "tabs") return null;
  return info.parent.id;
}

export function findNodeInfo(
  tree: DockNode,
  nodeId: string,
): { node: DockNode; parent: DockNode | null; index: number } | null {
  if (tree.id === nodeId) {
    return { node: tree, parent: null, index: 0 };
  }
  if (tree.type === "leaf") return null;
  for (let i = 0; i < tree.children.length; i++) {
    const child = tree.children[i];
    if (child.id === nodeId) {
      return { node: child, parent: tree, index: i };
    }
    // Recurse.
    const nested = findNodeInfo(child, nodeId);
    if (nested) return nested;
  }
  return null;
}

/** Returns true if `ancestorId` contains a node with id `descendantId`
 *  anywhere in its subtree (including itself). */
function isAncestor(
  tree: DockNode,
  ancestorId: string,
  descendantId: string,
): boolean {
  const anc = findNodeInfo(tree, ancestorId);
  if (!anc) return false;
  if (ancestorId === descendantId) return true;
  const inner = findNodeInfo(anc.node, descendantId);
  return inner !== null;
}

// ─── mutation (all return a new tree) ────────────────────────────────

/**
 * Replace the node with id `oldNodeId` with `newNode`. Returns the
 * new root. If the root itself is being replaced, returns `newNode`
 * directly.
 */
export function replaceNode(
  tree: DockNode,
  oldNodeId: string,
  newNode: DockNode,
): DockNode {
  if (tree.id === oldNodeId) return newNode;
  if (tree.type === "leaf") return tree;

  // Splits and tabs both have `children`. Walk and rebuild.
  if (tree.type === "split") {
    return {
      ...tree,
      children: tree.children.map((c) => replaceNode(c, oldNodeId, newNode)),
    };
  }
  // tabs: children are strictly leaves, so a non-leaf replacement has
  // nowhere to go and is dropped. That silent discard used to be a
  // data-loss path: `addLeafAtEdge` always builds a container (tabs or
  // split) and handed it straight here whenever its target was a leaf
  // inside a strip, so every insertion into a tabbed pane vanished.
  // `addNodeAtEdge` now retargets to the strip itself and never asks
  // for the impossible; this branch is a type-level backstop only.
  const replaced = tree.children.map((c) => {
    if (c.id === oldNodeId && newNode.type === "leaf") return newNode;
    return c;
  });
  return { ...tree, children: replaced };
}

/**
 * Insert an already-minted `leafNode` at the requested edge of the node
 * with id `targetId`. All edges wrap the target into a split except
 * `center`, which merges into (or creates) a tabs stack.
 *
 * The caller owns the node, and therefore knows its id. That matters
 * for the store: `openInFocusAt`/`openInSidebarAt` have to record the
 * new leaf in `lastFocusLeafId`/`lastSidebarLeafId`, and the only other
 * way to learn an id minted inside this module would be to diff the
 * before/after trees. `addLeafAtEdge` is the id-blind wrapper over this.
 */
export function addNodeAtEdge(
  tree: DockNode,
  targetId: string,
  edge: Edge,
  leafNode: DockLeafNode,
): DockNode {
  const info = findNodeInfo(tree, targetId);
  if (!info) return tree; // target vanished — no-op

  // A leaf that lives inside a tabs strip cannot be substituted in
  // place: `replaceNode`'s tabs branch only accepts another LEAF, and
  // every shape built below is a container. Handing one over anyway
  // rebuilt the tree with the insertion silently dropped — which is
  // what made EVERY drop zone stop working once a dock held two items
  // (two items is a tab strip, and `PaneTabs` renders the drop zones
  // against the active tab's LEAF id, never the strip's).
  //
  // Retarget to the strip. That is also the semantically right answer:
  // a strip holds leaves, so "put this beside the tab I'm looking at"
  // means splitting the whole strip, and "put this in the middle of it"
  // means appending a tab.
  const target =
    info.parent !== null && info.parent.type === "tabs"
      ? info.parent
      : info.node;
  const anchorId = target.id;

  if (edge === "center") {
    // Merge into (or create) tabs.
    if (target.type === "tabs") {
      const nextChildren = [...target.children, leafNode];
      const nextTabs: DockNode = {
        ...target,
        children: nextChildren,
        activeIdx: nextChildren.length - 1,
      };
      return replaceNode(tree, anchorId, nextTabs);
    }
    if (target.type === "leaf") {
      const tabsNode = newTabs([target, leafNode], 1);
      return replaceNode(tree, anchorId, tabsNode);
    }
    // Split: fall through — dropping "center" on a split isn't
    // meaningful in the current UI. Do nothing. `moveLeaf` guards
    // against this no-op eating the pane it already removed.
    return tree;
  }

  // Edge = left | right | top | bottom → wrap target in a new split.
  return addSubtreeAtEdge(tree, anchorId, edge, leafNode);
}

/**
 * Dock an arbitrary SUBTREE (not just a leaf) at one edge of a node.
 *
 * `addNodeAtEdge` delegates its edge branch here, so the two cannot
 * drift. The extra generality exists for layouts opened as a group — a
 * "watch this run" pane set arrives as a pre-built split, and must land
 * beside whatever the user already had rather than replacing it.
 *
 * "center" is deliberately not accepted: merging into a tab strip needs
 * a leaf, and a subtree is not one.
 */
export function addSubtreeAtEdge(
  tree: DockNode,
  targetId: string,
  edge: Exclude<Edge, "center">,
  subtree: DockNode,
): DockNode {
  const info = findNodeInfo(tree, targetId);
  if (!info) return tree; // target vanished — no-op
  // Same tab-strip retarget as addNodeAtEdge: "beside the tab I am
  // looking at" means beside the whole strip.
  const target =
    info.parent !== null && info.parent.type === "tabs"
      ? info.parent
      : info.node;
  const dir: "row" | "col" =
    edge === "left" || edge === "right" ? "row" : "col";
  const newFirst = edge === "left" || edge === "top";
  const children = newFirst ? [subtree, target] : [target, subtree];
  return replaceNode(tree, target.id, newSplit(dir, children, 0.5));
}

/**
 * Build a BALANCED binary split over `nodes`, weighted so every pane
 * ends up the same size.
 *
 * Splits are strictly binary here — `PaneNode` renders exactly two
 * children plus a splitter — so an N-pane row has to be nested. Nesting
 * naively off the last pane (a | (b | (c | d))) at the default 0.5
 * leaves the first pane with half the dock and the last with an eighth.
 * Halving the LIST instead, and setting each split's ratio to the share
 * of leaves on its left, gives an even row at any N.
 *
 * Returns null for an empty list, the node itself for one.
 */
export function evenSplitTree(
  nodes: DockNode[],
  dir: "row" | "col",
): DockNode | null {
  if (nodes.length === 0) return null;
  if (nodes.length === 1) return nodes[0];
  const half = Math.ceil(nodes.length / 2);
  const left = evenSplitTree(nodes.slice(0, half), dir) as DockNode;
  const right = evenSplitTree(nodes.slice(half), dir) as DockNode;
  return newSplit(dir, [left, right], half / nodes.length);
}

/**
 * Insert `newLeaf` at the requested edge of the node with id
 * `targetId`, minting the node internally. Convenience wrapper over
 * `addNodeAtEdge` for callers that don't need the new leaf's id.
 */
export function addLeafAtEdge(
  tree: DockNode,
  targetId: string,
  edge: Edge,
  newLeaf: DockLeaf,
): DockNode {
  return addNodeAtEdge(tree, targetId, edge, newLeafNode(newLeaf));
}

/**
 * Remove the node with id `nodeId`. Returns the new root, or `null`
 * if the whole tree was removed (i.e. the removed node was the root).
 *
 * Collapses:
 *   • split with 1 child → replaced with that child
 *   • split with 0 children → removed (shouldn't happen but handled)
 *   • tabs with 1 child → replaced with that leaf (matches the mental
 *     model: tabs of one is just a leaf)
 *   • tabs with 0 children → removed
 */
export function removeNode(tree: DockNode, nodeId: string): DockNode | null {
  if (tree.id === nodeId) return null;

  if (tree.type === "leaf") return tree;

  // Rebuild children with the target excised. Recurse for nested trees.
  if (tree.type === "split") {
    const nextChildren: DockNode[] = [];
    for (const child of tree.children) {
      if (child.id === nodeId) continue; // drop it
      // Recurse to remove nested matches. If the recursion returns
      // null (whole subtree gone), skip it entirely.
      const recursed = removeNode(child, nodeId);
      if (recursed !== null) nextChildren.push(recursed);
    }
    return collapseContainer({ ...tree, children: nextChildren });
  }

  // tabs: children are leaves. Remove any matching id and clamp activeIdx.
  const nextTabChildren = tree.children.filter((c) => c.id !== nodeId);
  const clampedActive =
    nextTabChildren.length === 0
      ? 0
      : Math.min(tree.activeIdx, nextTabChildren.length - 1);
  return collapseContainer({
    ...tree,
    children: nextTabChildren,
    activeIdx: clampedActive,
  });
}

/**
 * Post-mutation cleanup: collapse containers that ended up too small
 * to render usefully. Returns `null` when the container itself
 * should be removed.
 */
function collapseContainer(node: DockNode): DockNode | null {
  if (node.type === "leaf") return node;
  if (node.type === "split") {
    if (node.children.length === 0) return null;
    if (node.children.length === 1) return node.children[0];
    return node;
  }
  // tabs
  if (node.children.length === 0) return null;
  if (node.children.length === 1) return node.children[0];
  return node;
}

/**
 * The container `targetId` named is gone from `next` because removing
 * the source collapsed it (`collapseContainer` folds a 1-child split or
 * tabs into that child, and the child keeps its own id, not the
 * container's). Return the id of whatever took its place: the first
 * descendant of the ORIGINAL target that survived the removal.
 *
 * Returns null when the target's whole subtree went with the source, in
 * which case the caller has nothing better than the root.
 */
function resolveCollapsedTarget(
  original: DockNode,
  targetId: string,
  next: DockNode,
): string | null {
  const before = findNodeInfo(original, targetId);
  if (!before) return null;
  let survivor: string | null = null;
  walkLeaves(before.node, (_leaf, id) => {
    if (survivor === null && findNodeInfo(next, id) !== null) survivor = id;
  });
  return survivor;
}

/**
 * How many panes a tree holds.
 *
 * Was private, for `moveLeaf`'s "lost a pane" invariant. Now also the
 * dock badges: a dock's tree persists across reloads, so without a
 * count on the Focus tab there is no signal that anything is parked in
 * there — which is most of why the Focus workspace reads as empty and
 * pointless even when it is not. Accepts null so callers can pass a
 * dock tree straight from the store.
 */
export function countLeaves(tree: DockNode | null | undefined): number {
  if (!tree) return 0;
  let n = 0;
  walkLeaves(tree, () => {
    n += 1;
  });
  return n;
}

/**
 * Move a leaf from its current location to a new one. This is
 * remove-then-add; it never creates duplicate nodes, and — see the
 * leaf-count guard below — it never loses one either.
 *
 * No-ops (returns the tree unchanged):
 *   • source == target (any edge, center included)
 *   • source not found
 *   • the add step couldn't place the leaf anywhere
 */
export function moveLeaf(
  tree: DockNode,
  sourceLeafId: string,
  targetId: string,
  edge: Edge,
): DockNode {
  if (sourceLeafId === targetId) return tree;

  const src = findNodeInfo(tree, sourceLeafId);
  if (!src || src.node.type !== "leaf") return tree;

  // Guard: if the source is an ancestor of the target we'd create a
  // cycle. In the current schema leaves have no children, so this
  // is impossible in practice — but we check for robustness.
  if (isAncestor(tree, sourceLeafId, targetId) && sourceLeafId !== targetId) {
    return tree;
  }

  const leafNode = src.node;
  const withoutSource = removeNode(tree, sourceLeafId);
  if (withoutSource === null) {
    // We just removed the last node; nothing to attach to.
    return tree;
  }

  // The target may have collapsed as a side-effect of removal (its only
  // other child was the source, so the container folded into it). Chase
  // the collapse to the surviving node rather than falling back to the
  // root: the root fallback is what made PaneTabs' "Split right" on a
  // two-tab group fling the pane to the far edge of the entire dock and
  // reparent everything else, instead of landing it beside its former
  // tab-mate.
  const finalTargetId =
    findNodeInfo(withoutSource, targetId) !== null
      ? targetId
      : (resolveCollapsedTarget(tree, targetId, withoutSource) ??
        withoutSource.id);

  // Reuse the source node so the moved pane keeps its identity — its id
  // stays valid in `lastFocusLeafId`/`lastSidebarLeafId` and React keeps
  // the same component instance (and its scroll position) across the move.
  const moved = addNodeAtEdge(withoutSource, finalTargetId, edge, leafNode);

  // A move must never lose its payload. `addNodeAtEdge` no-ops for a
  // few shapes (a target that vanished entirely, "center" on a split);
  // because the source is already removed by this point, a no-op add
  // would DELETE the dragged pane with no error and no undo. Returning
  // the original tree makes the worst case "the drag didn't take".
  if (countLeaves(moved) !== countLeaves(tree)) return tree;
  return moved;
}

/**
 * Update the ratio on a split node. Values outside [0.1, 0.9] are
 * clamped so the splitter can't drag a pane closed. If the node
 * isn't a split or doesn't exist, the tree is returned unchanged.
 */
export function updateSplitRatio(
  tree: DockNode,
  splitId: string,
  ratio: number,
): DockNode {
  const info = findNodeInfo(tree, splitId);
  if (!info || info.node.type !== "split") return tree;
  const next: DockNode = { ...info.node, ratio: clampRatio(ratio) };
  return replaceNode(tree, splitId, next);
}

/**
 * Set the active tab index on a tabs node. Out-of-range values are
 * ignored (no-op). If the node isn't a tabs node, the tree is
 * returned unchanged.
 */
export function setActiveTab(
  tree: DockNode,
  tabsId: string,
  idx: number,
): DockNode {
  const info = findNodeInfo(tree, tabsId);
  if (!info || info.node.type !== "tabs") return tree;
  if (idx < 0 || idx >= info.node.children.length) return tree;
  const next: DockNode = { ...info.node, activeIdx: idx };
  return replaceNode(tree, tabsId, next);
}
