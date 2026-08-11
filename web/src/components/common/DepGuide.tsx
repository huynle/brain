/**
 * DepGuide — the leading box-drawing guide for a dependency-tree row,
 * plus the two markers that ride alongside it.
 *
 * Rendered inside the row's name cell so the surrounding grid columns
 * (glyph / status / id) stay aligned no matter how deep the row sits.
 *
 *   prefix    "│ └─" — from `flattenDepForest`, monospace and dimmed
 *   ↺         this node is in a dependency cycle
 *   +N        N further dependencies exist that did not place this
 *             node (the other edges of a diamond)
 *
 * Renders nothing at depth 0 with no markers, so a dependency-free list
 * looks exactly as it did before the tree landed.
 */

export interface DepGuideProps {
  /** Box-drawing prefix from `flattenDepForest`. Empty at depth 0. */
  prefix: string;
  /** Node is part of a dependency cycle. */
  inCycle?: boolean;
  /** Ids of extra dependencies not used for placement. */
  extraDeps?: readonly string[];
  /** What the ids refer to, for the +N tooltip. */
  extraLabel?: string;
}

export function DepGuide({
  prefix,
  inCycle = false,
  extraDeps,
  extraLabel = "dependencies",
}: DepGuideProps): JSX.Element | null {
  const extras = extraDeps ?? [];
  if (!prefix && !inCycle && extras.length === 0) return null;

  return (
    <>
      {prefix && (
        <span className="dep-guide" aria-hidden="true">
          {prefix}
        </span>
      )}
      {inCycle && (
        <span
          className="dep-cycle"
          title="Circular dependency — this item and its dependency wait on each other"
        >
          ↺
        </span>
      )}
      {extras.length > 0 && (
        <span
          className="dep-extra"
          title={`Also depends on ${extras.length} other ${extraLabel}: ${extras.join(", ")}`}
        >
          +{extras.length}
        </span>
      )}
    </>
  );
}
