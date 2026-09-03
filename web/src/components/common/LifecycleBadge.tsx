/**
 * LifecycleBadge — the one place a feature lifecycle becomes a chip.
 *
 * Four surfaces (CardTasks, OverviewGrid, FeatureModal, FeatureDetailLeaf)
 * kept their own private copy of the tone/label map, so adding a lifecycle
 * meant editing four literals and a missed one rendered `undefined` with no
 * type error — `LIFECYCLE_TONE[f.lifecycle]` is an index into a Record the
 * copies each declared `as const`, not an exhaustive switch. One export now.
 *
 * ─── Why this can be a link ───────────────────────────────────────────
 *
 * `mr-open` is the ONLY lifecycle with somewhere to go: a real merge
 * request on a git server, addressed by `DerivedFeature.prUrl`. Pass that
 * url as `href` and the chip renders as an anchor that opens the MR in the
 * user's default browser. `ready-to-merge` — a parked Brain-native merge
 * intent — deliberately has no href: there is nothing on any git server to
 * open, and a dead link would recreate exactly the confusion the split of
 * these two states was meant to end.
 *
 * The anchor stops its own click from propagating: every surface that
 * renders this chip sits inside a row whose click opens the feature modal,
 * and a link that also opened a modal behind the new tab would be a bug on
 * every one of them. It is likewise `draggable={false}`, because the
 * feature rows in CardTasks ARE drag sources and a native link-drag would
 * shadow the row's own drag payload.
 *
 * It must stop Enter and Space too, and for a sharper reason than tidiness.
 * Those rows come from `useRowActions.rowProps`, which is `role="button"`
 * with `tabIndex 0` and an `onKeyDown` that calls `e.preventDefault()` on
 * Enter/Space before running the row's activate verb. A keydown bubbling
 * out of this anchor hits that handler, the preventDefault cancels the
 * link's OWN default activation, and the feature modal opens instead of
 * the merge request — leaving the link reachable by mouse only. Space is
 * activated explicitly here because a link has no Space default to keep.
 *
 * Known limitation: an `<a>` inside `role="button"` is invalid ARIA
 * nesting, and some screen readers will not announce it as a separate
 * control. Fixing that properly means the rows stop being role="button",
 * which is a much larger change; the link carries its own `aria-label` so
 * that where it IS exposed, it says what it does.
 */
import type { CSSProperties } from "react";

import type { FeatureLifecycle } from "../../lib/features";

export interface LifecycleTone {
  /** CSS modifier appended to `.life-badge` (and reused by `.flow-lane`,
   *  `.flow-pill`, `.pcard-head .health`). */
  tone: string;
  /** Chip text. Rendered uppercase by CSS; written lowercase here so the
   *  same string reads correctly in prose contexts (AssistantPanel). */
  label: string;
}

export const LIFECYCLE_TONE: Record<FeatureLifecycle, LifecycleTone> = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  "ready-to-merge": { tone: "ready", label: "ready to merge" },
  merged: { tone: "merged", label: "merged" },
};

/** Hover text for the two states users confuse. Long on purpose: this is
 *  the only place the distinction is explained in the UI. */
const TITLES: Partial<Record<FeatureLifecycle, string>> = {
  "mr-open":
    "A merge request is open on the git server. Click to open it in your browser.",
  "ready-to-merge":
    "Checkout produced a Brain merge request for this feature and it is still pending — " +
    "the work is validated and waiting to be merged. Nothing has been opened on a git server.",
};

export interface LifecycleBadgeProps {
  lifecycle: FeatureLifecycle;
  /** `DerivedFeature.prUrl`. Makes the chip a link when present; ignored
   *  for every lifecycle but `mr-open`, since only that one means the url
   *  points at something still open. */
  href?: string;
  className?: string;
  style?: CSSProperties;
}

export function LifecycleBadge({
  lifecycle,
  href,
  className,
  style,
}: LifecycleBadgeProps): JSX.Element {
  const tone = LIFECYCLE_TONE[lifecycle];
  const cls = `life-badge lifecycle-chip ${tone.tone}${className ? ` ${className}` : ""}`;
  const title = TITLES[lifecycle];

  if (href && lifecycle === "mr-open") {
    return (
      <a
        className={`${cls} link`}
        style={style}
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        draggable={false}
        aria-label="Open the merge request in a new tab"
        title={`${title}\n${href}`}
        onClick={(e) => e.stopPropagation()}
        // The Overview's attention rows pin a feature on double-click, and
        // their guard only skips `closest("button")` — an anchor is neither.
        // Stopping it here keeps that responsibility with the badge, where
        // the click and keydown guards already live.
        onDoubleClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key !== "Enter" && e.key !== " ") return;
          // Enter: let the browser follow the link, just keep the row's
          // handler from cancelling it. Space: a link has no activation
          // default, so drive it here rather than leaving a dead key.
          e.stopPropagation();
          if (e.key === " ") {
            e.preventDefault();
            e.currentTarget.click();
          }
        }}
      >
        {tone.label} ↗
      </a>
    );
  }

  return (
    <span className={cls} style={style} title={title}>
      {tone.label}
    </span>
  );
}
