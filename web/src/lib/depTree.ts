/**
 * lib/depTree — pure dependency-forest construction.
 *
 * Both the feature list and the task list are flat arrays that carry
 * dependency edges as id references (`feature_depends_on` for features,
 * `resolved_deps` / `parent_id` for tasks). This module turns either one
 * into a forest so the UI can render "B depends on A" as A ▸ B.
 *
 * The semantics deliberately mirror `internal/tui/tasktree.go BuildTree`
 * so the PWA and the TUI agree on what a dependency tree looks like:
 *
 *   Edge direction — if B depends on A, A is the PARENT and B is the
 *     child. Roots are therefore the items nothing else waits on, and
 *     reading top-down follows execution order.
 *
 *   parent_id wins — an explicit hard parent outranks a dependency edge
 *     for *placement*. The dependency edge is still recorded on the node
 *     as an `extraDep` so the UI can hint at it.
 *
 *   Diamonds render once — an item reachable from several parents is
 *     placed under the first parent visited; the other incoming edges
 *     land in `extraDeps`. This keeps the row count equal to the item
 *     count, so counts in headers stay honest.
 *
 *   Cycles become roots — members of a dependency cycle are flagged
 *     `inCycle` and their intra-cycle edges are dropped, so a cycle
 *     surfaces as sibling roots marked "↺" rather than hanging the
 *     renderer.
 *
 *   Dangling refs are ignored — a dep pointing outside `items` (filtered
 *     out, different project, deleted) does not create an edge, so the
 *     dependent stays a root instead of vanishing.
 *
 * Pure: no react, no hooks, no fetch. Unit-tested from `depTree.test.ts`.
 */

export interface DepNode<T> {
  id: string;
  item: T;
  children: DepNode<T>[];
  /** Member of a dependency cycle — render with a "↺" marker. */
  inCycle: boolean;
  /**
   * Ids of other in-set dependencies that did NOT place this node:
   * the extra edges of a diamond, plus the dep edge that lost to a
   * `parent_id`. Empty for the common single-parent case.
   */
  extraDeps: string[];
}

export interface DepForestOptions<T> {
  /** Stable identity of an item. */
  id: (item: T) => string;
  /** Ids this item depends on. Each becomes a candidate parent. */
  deps: (item: T) => readonly string[] | undefined | null;
  /** Optional hard parent id, outranking `deps` for placement. */
  parent?: (item: T) => string | undefined | null;
  /**
   * Sibling / root ordering weight — lower sorts first. Defaults to the
   * item's position in `items`, i.e. the caller's existing order is
   * preserved. Topological order among siblings always wins over rank.
   */
  rank?: (item: T, index: number) => number;
}

/**
 * Build a dependency forest from a flat list. See the module docstring
 * for edge direction, diamond, and cycle rules.
 */
export function buildDepForest<T>(
  items: readonly T[],
  opts: DepForestOptions<T>,
): DepNode<T>[] {
  if (items.length === 0) return [];

  const { id: getId, deps: getDeps, parent: getParent, rank: getRank } = opts;

  // ─── Identity + rank ─────────────────────────────────────────────
  // Later duplicates of an id are dropped rather than overwriting, so a
  // malformed list can't produce two rows claiming the same identity.
  const byId = new Map<string, T>();
  const rankOf = new Map<string, number>();
  const order: string[] = [];
  items.forEach((item, i) => {
    const id = getId(item);
    if (!id || byId.has(id)) return;
    byId.set(id, item);
    rankOf.set(id, getRank ? getRank(item, i) : i);
    order.push(id);
  });

  // ─── Edges ───────────────────────────────────────────────────────
  // `placement` is the single parent that will physically hold each
  // node; `extraDeps` collects every other in-set incoming edge.
  const placement = new Map<string, string>();
  const extraDeps = new Map<string, string[]>();

  for (const id of order) {
    const item = byId.get(id) as T;

    // A hard parent claims placement outright — but only if it is in
    // the set and is not the node itself.
    const hardParent = getParent?.(item);
    let placed: string | undefined;
    if (hardParent && hardParent !== id && byId.has(hardParent)) {
      placed = hardParent;
    }

    const extras: string[] = [];
    for (const dep of getDeps(item) ?? []) {
      if (!dep || dep === id || !byId.has(dep)) continue; // dangling / self
      if (placed === undefined) placed = dep;
      else if (dep !== placed && !extras.includes(dep)) extras.push(dep);
    }

    if (placed !== undefined) placement.set(id, placed);
    if (extras.length > 0) extraDeps.set(id, extras);
  }

  // ─── Cycle detection ─────────────────────────────────────────────
  // DFS over the placement edges. Any node on the recursion stack when
  // re-encountered marks the whole active chain as in-cycle.
  const childrenOf = new Map<string, string[]>();
  for (const [child, parent] of placement) {
    const arr = childrenOf.get(parent);
    if (arr) arr.push(child);
    else childrenOf.set(parent, [child]);
  }

  const inCycle = new Set<string>();
  {
    const WHITE = 0,
      GREY = 1,
      BLACK = 2;
    const color = new Map<string, number>();
    for (const id of order) color.set(id, WHITE);

    // Iterative DFS — a deep dependency chain must not blow the JS stack.
    for (const root of order) {
      if (color.get(root) !== WHITE) continue;
      const stack: Array<{ id: string; next: number }> = [
        { id: root, next: 0 },
      ];
      color.set(root, GREY);
      while (stack.length > 0) {
        const top = stack[stack.length - 1];
        const kids = childrenOf.get(top.id) ?? [];
        if (top.next >= kids.length) {
          color.set(top.id, BLACK);
          stack.pop();
          continue;
        }
        const kid = kids[top.next++];
        const c = color.get(kid);
        if (c === GREY) {
          // Back edge — everything from `kid` up to the stack top is
          // on the cycle.
          let i = stack.length - 1;
          while (i >= 0) {
            inCycle.add(stack[i].id);
            if (stack[i].id === kid) break;
            i--;
          }
          inCycle.add(kid);
        } else if (c === WHITE) {
          color.set(kid, GREY);
          stack.push({ id: kid, next: 0 });
        }
      }
    }
  }

  // Drop intra-cycle edges so cycle members surface as sibling roots.
  const effectiveParent = new Map<string, string>();
  for (const [child, parent] of placement) {
    if (inCycle.has(child) && inCycle.has(parent)) continue;
    effectiveParent.set(child, parent);
  }

  const effectiveChildren = new Map<string, string[]>();
  for (const [child, parent] of effectiveParent) {
    const arr = effectiveChildren.get(parent);
    if (arr) arr.push(child);
    else effectiveChildren.set(parent, [child]);
  }

  // ─── Sibling ordering ────────────────────────────────────────────
  // Rank order, then a Kahn pass so any sibling that depends on another
  // sibling (only reachable via parent_id, which outranks dep edges)
  // still renders after it.
  const sortSiblings = (ids: string[]): string[] => {
    const ranked = [...ids].sort(
      (a, b) => (rankOf.get(a) ?? 0) - (rankOf.get(b) ?? 0),
    );
    if (ranked.length < 2) return ranked;

    const set = new Set(ranked);
    const indeg = new Map<string, number>();
    const out = new Map<string, string[]>();
    for (const id of ranked) indeg.set(id, 0);
    for (const id of ranked) {
      const item = byId.get(id) as T;
      for (const dep of getDeps(item) ?? []) {
        if (!set.has(dep) || dep === id) continue;
        const arr = out.get(dep);
        if (arr) arr.push(id);
        else out.set(dep, [id]);
        indeg.set(id, (indeg.get(id) ?? 0) + 1);
      }
    }

    const ready = ranked.filter((id) => (indeg.get(id) ?? 0) === 0);
    const result: string[] = [];
    while (ready.length > 0) {
      const id = ready.shift() as string;
      result.push(id);
      for (const next of out.get(id) ?? []) {
        const d = (indeg.get(next) ?? 0) - 1;
        indeg.set(next, d);
        if (d === 0) {
          // Keep the ready queue in rank order.
          const r = rankOf.get(next) ?? 0;
          const at = ready.findIndex((x) => (rankOf.get(x) ?? 0) > r);
          if (at === -1) ready.push(next);
          else ready.splice(at, 0, next);
        }
      }
    }
    // A sibling cycle leaves nodes unemitted — append them in rank
    // order rather than dropping rows.
    if (result.length < ranked.length) {
      const seen = new Set(result);
      for (const id of ranked) if (!seen.has(id)) result.push(id);
    }
    return result;
  };

  // ─── Materialize ─────────────────────────────────────────────────
  const rendered = new Set<string>();

  const mkNode = (id: string, item: T): DepNode<T> => ({
    id,
    item,
    children: [],
    inCycle: inCycle.has(id),
    extraDeps: extraDeps.get(id) ?? [],
  });

  // Iterative pre-order expansion. A pathological dep chain (agent-
  // generated graphs can be thousands deep) must not blow the JS stack
  // and white-screen the dashboard.
  const build = (rootId: string): DepNode<T> | null => {
    if (rendered.has(rootId)) return null;
    const rootItem = byId.get(rootId);
    if (rootItem === undefined) return null;
    rendered.add(rootId);

    const rootNode = mkNode(rootId, rootItem);
    const stack: Array<{ node: DepNode<T>; kids: string[]; next: number }> = [
      { node: rootNode, kids: sortSiblings(effectiveChildren.get(rootId) ?? []), next: 0 },
    ];

    while (stack.length > 0) {
      const frame = stack[stack.length - 1];
      if (frame.next >= frame.kids.length) {
        stack.pop();
        continue;
      }
      const childId = frame.kids[frame.next++];
      if (rendered.has(childId)) continue; // diamond — placed already
      const childItem = byId.get(childId);
      if (childItem === undefined) continue;
      rendered.add(childId);

      const childNode = mkNode(childId, childItem);
      frame.node.children.push(childNode);
      stack.push({
        node: childNode,
        kids: sortSiblings(effectiveChildren.get(childId) ?? []),
        next: 0,
      });
    }

    return rootNode;
  };

  const rootIds = sortSiblings(order.filter((id) => !effectiveParent.has(id)));
  const roots: DepNode<T>[] = [];
  for (const id of rootIds) {
    const node = build(id);
    if (node) roots.push(node);
  }
  // Anything still unrendered (reachable only through a dropped
  // intra-cycle edge) is promoted to a root so no item disappears.
  for (const id of order) {
    if (rendered.has(id)) continue;
    const node = build(id);
    if (node) roots.push(node);
  }

  return roots;
}

// ─── Flattening for row-based rendering ────────────────────────────

export interface DepRow<T> {
  node: DepNode<T>;
  /** 0 for roots. */
  depth: number;
  /**
   * Box-drawing guide for this row, e.g. "│ ├─". Empty string at
   * depth 0. One guide segment per ancestor at depth 1..depth-1 —
   * "│ " while that ancestor has siblings left below it, "  " once it
   * was the last child — followed by this row's own connector.
   */
  prefix: string;
  /** True when this node is its parent's final child. */
  isLast: boolean;
}

const BRANCH = "├─";
const LAST_BRANCH = "└─";
const VERTICAL = "│ ";
const GAP = "  ";

/**
 * Depth-first flatten of a forest into render-ready rows, each carrying
 * the box-drawing prefix for its position. Mirrors the TUI's connectors
 * (`├─`, `└─`, `│ `) so both surfaces read identically.
 */
export function flattenDepForest<T>(roots: readonly DepNode<T>[]): DepRow<T>[] {
  const rows: DepRow<T>[] = [];

  interface Frame {
    node: DepNode<T>;
    depth: number;
    isLast: boolean;
    /** isLast of each ancestor at depth 1..depth-1, root excluded. */
    guides: readonly boolean[];
  }

  // Reversed so popping yields left-to-right document order.
  const stack: Frame[] = roots
    .map((node, i) => ({
      node,
      depth: 0,
      isLast: i === roots.length - 1,
      guides: [] as readonly boolean[],
    }))
    .reverse();

  while (stack.length > 0) {
    const { node, depth, isLast, guides } = stack.pop() as Frame;

    let prefix = "";
    if (depth > 0) {
      for (const last of guides) prefix += last ? GAP : VERTICAL;
      prefix += isLast ? LAST_BRANCH : BRANCH;
    }
    rows.push({ node, depth, prefix, isLast });

    // A root's own isLast never draws a guide for its descendants —
    // there is no column to its left. Every deeper level contributes.
    const nextGuides = depth === 0 ? guides : [...guides, isLast];
    for (let i = node.children.length - 1; i >= 0; i--) {
      stack.push({
        node: node.children[i],
        depth: depth + 1,
        isLast: i === node.children.length - 1,
        guides: nextGuides,
      });
    }
  }

  return rows;
}

/** Total node count in a forest — cheaper than flattening to count. */
export function countDepNodes<T>(roots: readonly DepNode<T>[]): number {
  let n = 0;
  const stack = [...roots];
  while (stack.length > 0) {
    const node = stack.pop() as DepNode<T>;
    n++;
    for (const c of node.children) stack.push(c);
  }
  return n;
}

/** True when any node in the forest sits below a root. */
export function hasDepEdges<T>(roots: readonly DepNode<T>[]): boolean {
  return roots.some((r) => r.children.length > 0);
}
