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
  kind: "task-detail" | "logs" | "session" | "runners" | "browser" | "entry";
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

export function newLeafNode(leaf: DockLeaf): DockNode {
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
function isAncestor(tree: DockNode, ancestorId: string, descendantId: string): boolean {
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
  // tabs: children are strictly leaves. If the replacement is not a
  // leaf, we can't slot it into the tabs list — bail with the
  // original tree. (Callers shouldn't hit this branch; they use
  // moveLeaf which decomposes moves into remove+add.)
  const replaced = tree.children.map((c) => {
    if (c.id === oldNodeId && newNode.type === "leaf") return newNode;
    return c;
  });
  return { ...tree, children: replaced };
}

/**
 * Insert `newLeaf` at the requested edge of the node with id
 * `targetId`. All edges wrap the target into a split except `center`,
 * which merges into (or creates) a tabs stack.
 */
export function addLeafAtEdge(
  tree: DockNode,
  targetId: string,
  edge: Edge,
  newLeaf: DockLeaf,
): DockNode {
  const info = findNodeInfo(tree, targetId);
  if (!info) return tree; // target vanished — no-op

  const target = info.node;
  const leafNode = newLeafNode(newLeaf) as {
    type: "leaf";
    id: string;
    leaf: DockLeaf;
  };

  if (edge === "center") {
    // Merge into (or create) tabs.
    if (target.type === "tabs") {
      const nextChildren = [...target.children, leafNode];
      const nextTabs: DockNode = {
        ...target,
        children: nextChildren,
        activeIdx: nextChildren.length - 1,
      };
      return replaceNode(tree, targetId, nextTabs);
    }
    if (target.type === "leaf") {
      const tabsNode = newTabs([target, leafNode], 1);
      return replaceNode(tree, targetId, tabsNode);
    }
    // Split: fall through — dropping "center" on a split isn't
    // meaningful in the current UI. Do nothing.
    return tree;
  }

  // Edge = left | right | top | bottom → wrap target in a new split.
  const dir: "row" | "col" =
    edge === "left" || edge === "right" ? "row" : "col";
  const newFirst = edge === "left" || edge === "top";
  const children = newFirst ? [leafNode, target] : [target, leafNode];
  const split = newSplit(dir, children, 0.5);
  return replaceNode(tree, targetId, split);
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
 * Move a leaf from its current location to a new one. This is
 * remove-then-add; it never creates duplicate nodes.
 *
 * No-ops (returns the tree unchanged):
 *   • source == target
 *   • center-drop on self (leaf into itself)
 *   • source not found
 *   • target not found after removal
 */
export function moveLeaf(
  tree: DockNode,
  sourceLeafId: string,
  targetId: string,
  edge: Edge,
): DockNode {
  if (sourceLeafId === targetId) return tree;
  if (edge === "center" && sourceLeafId === targetId) return tree;

  const src = findNodeInfo(tree, sourceLeafId);
  if (!src || src.node.type !== "leaf") return tree;

  // Guard: if the source is an ancestor of the target we'd create a
  // cycle. In the current schema leaves have no children, so this
  // is impossible in practice — but we check for robustness.
  if (isAncestor(tree, sourceLeafId, targetId) && sourceLeafId !== targetId) {
    return tree;
  }

  const leafPayload = src.node.leaf;
  const withoutSource = removeNode(tree, sourceLeafId);
  if (withoutSource === null) {
    // We just removed the last node; nothing to attach to.
    return tree;
  }

  // The target may have collapsed as a side-effect of removal
  // (e.g. its sibling was removed and the parent split folded up).
  // If it's still findable, use it; otherwise fall back to the root.
  const targetInfo = findNodeInfo(withoutSource, targetId);
  const finalTargetId = targetInfo ? targetId : withoutSource.id;

  return addLeafAtEdge(withoutSource, finalTargetId, edge, leafPayload);
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
