/**
 * LifecycleBadge — the one place a feature lifecycle becomes a chip.
 *
 * Four surfaces (CardTasks, OverviewGrid, FeatureModal, FeatureDetailLeaf)
 * kept their own private copy of the tone/label map, so adding a lifecycle
 * meant editing four literals and a missed one rendered `undefined` with no
 * type error — `LIFECYCLE_TONE[f.lifecycle]` is an index into a Record the
 * copies each declared `as const`, not an exhaustive switch. One export now.
 *
 * ─── Why the badge is no longer ever a link ──────────────────────────
 *
 * It used to be, for the `mr-open` lifecycle. That lifecycle is gone (see
 * `lib/features` for why: a forge URL is an artifact attached to a feature,
 * not the state of its work, and it outranked both `finished` and `blocked`
 * on the strength of a regex over task prose). The link survives as
 * {@link MergeRequestLink}, a SIBLING chip rendered beside the badge on
 * whatever lifecycle is actually true.
 *
 * Splitting them also fixes what the chip claimed. Nothing in this system
 * ever contacts a git server, so "MR open" was never checkable; the link
 * now says only what is true — this URL appears in a task of this feature.
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
  "ready-to-merge": { tone: "ready", label: "ready to merge" },
  validated: { tone: "validated", label: "validated" },
};

/** Hover text for the state users confuse with a real merge request. Long
 *  on purpose: this is the only place the distinction is explained. */
const TITLES: Partial<Record<FeatureLifecycle, string>> = {
  validated:
    "Every task is done and every non-generated task is marked validated — " +
    "the checkout agent verified the work. This does NOT mean the branch was " +
    "merged: nothing in Brain observes that.",
  "ready-to-merge":
    "Checkout produced a Brain merge request for this feature and it is still pending — " +
    "the work is validated and waiting to be merged. Nothing has been opened on a git server.",
};

export interface LifecycleBadgeProps {
  lifecycle: FeatureLifecycle;
  className?: string;
  style?: CSSProperties;
}

export function LifecycleBadge({
  lifecycle,
  className,
  style,
}: LifecycleBadgeProps): JSX.Element {
  const tone = LIFECYCLE_TONE[lifecycle];
  const cls = `life-badge lifecycle-chip ${tone.tone}${className ? ` ${className}` : ""}`;
  return (
    <span className={cls} style={style} title={TITLES[lifecycle]}>
      {tone.label}
    </span>
  );
}

/**
 * MergeRequestLink — `DerivedFeature.prUrl` as a chip you can follow.
 *
 * Orthogonal to the lifecycle badge: a feature can be blocked, active or
 * finished AND carry a merge-request URL, and before the split the URL
 * silently replaced whichever of those was true.
 *
 * ─── The event guards, and why each one is load-bearing ──────────────
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
export interface MergeRequestLinkProps {
  /** `DerivedFeature.prUrl`. */
  href: string;
  className?: string;
  style?: CSSProperties;
}

export function MergeRequestLink({
  href,
  className,
  style,
}: MergeRequestLinkProps): JSX.Element {
  return (
    <a
      className={`life-badge lifecycle-chip mr link${className ? ` ${className}` : ""}`}
      style={style}
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      draggable={false}
      aria-label="Open the merge request in a new tab"
      // Deliberately NOT "a merge request is open": nothing in this system
      // ever asks the git server, so the only checkable claim is that the
      // URL appears in this feature's tasks.
      title={`A task in this feature links to this merge request. Brain does not track whether it is still open.\n${href}`}
      onClick={(e) => e.stopPropagation()}
      // The Overview's attention rows pin a feature on double-click, and
      // their guard only skips `closest("button")` — an anchor is neither.
      // Stopping it here keeps that responsibility with the chip, where
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
      MR ↗
    </a>
  );
}
