/**
 * panes-v2 sidebar section header — collapsible title bar.
 *
 * Extracted as a shared piece because the three sections
 * (Projects / Sessions / Runners) all render an identical head. The
 * head is a <button> so keyboard users can toggle sections with
 * Enter/Space; screen readers announce it as expanded/collapsed via
 * aria-expanded.
 *
 * See web/src/styles/sidebar.css `.p2-side-section__head`.
 */

export interface SectionHeadProps {
  title: string;
  count?: number;
  expanded: boolean;
  onToggle: () => void;
}

export function SectionHead({
  title,
  count,
  expanded,
  onToggle,
}: SectionHeadProps): JSX.Element {
  return (
    <button
      type="button"
      className="p2-side-section__head"
      onClick={onToggle}
      aria-expanded={expanded}
      style={{
        background: "transparent",
        border: 0,
        width: "100%",
        cursor: "pointer",
        textAlign: "left",
        font: "inherit",
        color: "inherit",
      }}
    >
      <span className="p2-side-section__toggle" aria-hidden="true">
        ▾
      </span>
      <span className="p2-side-section__title">{title}</span>
      {count !== undefined && (
        <span className="p2-side-section__count">{count}</span>
      )}
    </button>
  );
}
