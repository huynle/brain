/**
 * Chip — matches wireframe `.chip .mini/.active`.
 */
import React from "react";

export type ChipVariant = "default" | "mini" | "active";

export interface ChipProps {
  variant?: ChipVariant;
  children?: React.ReactNode;
  onClick?: (e: React.MouseEvent) => void;
  className?: string;
  title?: string;
  disabled?: boolean;
  pressed?: boolean;
}

export function classForChip(
  variant: ChipVariant = "default",
  extra?: string,
): string {
  const parts = ["chip"];
  if (variant !== "default") parts.push(variant);
  if (extra) parts.push(extra);
  return parts.join(" ");
}

export function Chip({
  variant = "default",
  children,
  onClick,
  className,
  title,
  disabled,
  pressed,
}: ChipProps): JSX.Element {
  const cls = classForChip(variant, className);
  if (onClick) {
    return (
      <button
        type="button"
        className={cls}
        onClick={onClick}
        title={title}
        disabled={disabled}
        aria-pressed={pressed}
        style={{ border: "1px solid #2a2f35", background: "transparent" }}
      >
        {children}
      </button>
    );
  }
  return (
    <span className={cls} title={title}>
      {children}
    </span>
  );
}
