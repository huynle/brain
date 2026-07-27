/**
 * panes-v2 recursive dock-tree renderer.
 *
 * Given a `DockNode`, renders it appropriately:
 *   • leaf  → `<PaneLeaf/>`
 *   • split → row/col flexbox with `<Splitter/>` between children
 *   • tabs  → `<PaneTabs/>` with strip on top and active leaf below
 *
 * All layout math lives here so `<PaneLeaf/>` doesn't need to know
 * about its container.
 */
import type { DockNode } from "../../lib/dock";
import { PaneLeaf } from "./PaneLeaf";
import { PaneTabs } from "./PaneTabs";
import { Splitter } from "./Splitter";

export function PaneNode({ node }: { node: DockNode }): JSX.Element {
  if (node.type === "leaf") {
    return <PaneLeaf id={node.id} leaf={node.leaf} />;
  }

  if (node.type === "tabs") {
    return <PaneTabs node={node} />;
  }

  // split
  const dirClass = node.dir === "row" ? "row" : "col";
  const first = node.children[0];
  const second = node.children[1];

  // For nested splits (>2 children shouldn't happen given addLeafAtEdge
  // always creates two-child splits, but be defensive) we fall back to
  // rendering children directly with no splitter.
  if (!first || !second) {
    return (
      <div className={`p2-dock-split ${dirClass}`}>
        {node.children.map((c) => (
          <div className="p2-dock-child" key={c.id} style={{ flex: 1 }}>
            <PaneNode node={c} />
          </div>
        ))}
      </div>
    );
  }

  const ratio = node.ratio;
  const firstBasis = `${ratio * 100}%`;
  const secondBasis = `${(1 - ratio) * 100}%`;

  return (
    <div className={`p2-dock-split ${dirClass}`}>
      <div className="p2-dock-child" style={{ flexBasis: firstBasis }}>
        <PaneNode node={first} />
      </div>
      <Splitter dir={node.dir} splitId={node.id} />
      <div className="p2-dock-child" style={{ flexBasis: secondBasis }}>
        <PaneNode node={second} />
      </div>
    </div>
  );
}
