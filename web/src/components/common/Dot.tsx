/**
 * Dot — status dot matching wireframe `.dot .on/.busy/.err`.
 *
 * The wireframe scopes `.dot` under `.proj-row`, `.pcard-head`,
 * `.runner-row`, and `.sess-row`. Consumers usually render the dot
 * inside one of those wrappers so the CSS applies. When used
 * standalone, we fall back to inline styles that match the wireframe
 * palette.
 */

export type DotVariant = "on" | "busy" | "err" | "stale";
export type DotSize = "sm" | "md" | "lg";

export interface DotProps {
  variant: DotVariant;
  size?: DotSize;
  className?: string;
  title?: string;
}

export function classForDot(
  variant: DotVariant,
  _size: DotSize = "md",
  extra?: string,
): string {
  const parts = ["dot"];
  if (variant === "on" || variant === "busy" || variant === "err") {
    parts.push(variant);
  }
  // "stale" has no wireframe class — falls back to default grey.
  if (extra) parts.push(extra);
  return parts.join(" ");
}

const COLOR: Record<DotVariant, string> = {
  on: "#6fca7d",
  busy: "#f4b23a",
  err: "#d96060",
  stale: "#4b545c",
};
const SIZE = { sm: 6, md: 8, lg: 10 } as const;

export function Dot({
  variant,
  size = "md",
  className,
  title,
}: DotProps): JSX.Element {
  const d = SIZE[size];
  return (
    <span
      className={classForDot(variant, size, className)}
      role="img"
      aria-label={title ?? variant}
      style={{
        display: "inline-block",
        width: d,
        height: d,
        borderRadius: "50%",
        background: COLOR[variant],
        boxShadow: variant === "on" ? `0 0 6px ${COLOR.on}99` : undefined,
      }}
    />
  );
}
