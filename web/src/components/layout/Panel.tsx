import { forwardRef } from "react";

/** A bordered terminal-style panel with a title overlaid on the top border. */
export const Panel = forwardRef<
  HTMLDivElement,
  {
    title: string;
    meta?: string;
    focused?: boolean;
    onFocus?: () => void;
    children: React.ReactNode;
    style?: React.CSSProperties;
    className?: string;
    bodyRef?: React.Ref<HTMLDivElement>;
  }
>(function Panel(
  { title, meta, focused, onFocus, children, style, className, bodyRef },
  ref,
) {
  return (
    <div
      ref={ref}
      className={`panel ${focused ? "focused" : ""} ${className ?? ""}`}
      style={style}
      onMouseDown={onFocus}
    >
      <span className="panel-title">
        {title}
        {meta && <span className="ttl-meta">{meta}</span>}
      </span>
      <div className="panel-body" ref={bodyRef}>
        {children}
      </div>
    </div>
  );
});
