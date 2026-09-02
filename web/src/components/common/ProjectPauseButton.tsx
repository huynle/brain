/**
 * ProjectPauseButton — the project task dial, as one clickable glyph.
 *
 * Replaces the inert status dot that used to lead the sidebar project row
 * and the project card header. Until now the dials were reachable ONLY
 * through right-click / long-press / the `p`+`r` accelerators, so the most
 * common thing a user does to a project took three steps and a menu.
 *
 * ─── What the glyph and the colour each mean ─────────────────────
 *
 * The dot it replaces carried the WORK state (busy / blocked / idle) and,
 * in the sidebar, the dial as well — with `busy` outranking `paused`, so a
 * project someone had deliberately isolated looked exactly like one
 * running freely. Splitting the two channels fixes that without losing
 * either signal:
 *
 *   glyph   the dial     ▶ dispatching · ⏸ held
 *   colour  the work     green ready · amber running · red blocked ·
 *                        grey empty · steel-blue held
 *
 * The one composite state is `override`: the dial is off and work is
 * running anyway (a manual "Run now" is defined to force past the dial).
 * That renders as a ⏸ glyph in amber with the running pulse — held, hot —
 * which is precisely what it is.
 *
 * Glyph reads as STATE, not as the action, because it occupies the status
 * column the dot held: a ▶ where a green dot used to sit must not mean
 * "press to play". The action is in the tooltip and the aria-label.
 *
 * ─── Wiring ─────────────────────────────────────────────────────
 *
 * Verbs come from `buildProjectActions` and run through `useActionRunner`
 * rather than calling the API directly, so the button inherits the error
 * toast, the disabled-with-reason guard and the confirm plumbing that the
 * context menu already has — and can never disagree with the menu item
 * sitting one right-click away.
 */
import { useMemo } from "react";

import { useActionRunner } from "../../hooks/useActionRunner";
import { useProjectActionContext } from "../../hooks/useProjectActionContext";
import { buildProjectActions } from "../../lib/actions/projectActions";
import type { ProjectRunIndicator } from "../../lib/pause";

export interface ProjectPauseButtonProps {
  projectId: string;
  /** From `projectRunIndicator` — state, glyph choice and the tooltips. */
  indicator: ProjectRunIndicator;
  /** Task count, so the run verb's disabled reason stays accurate. */
  taskCount: number;
  /** True while the dial state is still loading. The button stays live:
   *  both endpoints are idempotent, and unknown must not disable a verb. */
  pauseLoading?: boolean;
}

export function ProjectPauseButton({
  projectId,
  indicator,
  taskCount,
  pauseLoading = false,
}: ProjectPauseButtonProps): JSX.Element {
  const ctx = useProjectActionContext();
  // Instantiated HERE rather than hoisted to the list: `busy` is one flag
  // per runner instance, so a shared one would grey out every project row
  // the moment any single one was clicked.
  const runner = useActionRunner();

  const action = useMemo(() => {
    const actions = buildProjectActions(projectId, ctx, {
      taskCount,
      tasksPaused: pauseLoading ? undefined : indicator.paused,
    });
    const want = indicator.paused ? "resume" : "pause";
    return actions.find((a) => a.id === want);
  }, [projectId, ctx, taskCount, pauseLoading, indicator.paused]);

  return (
    <>
      <button
        type="button"
        // Surface-specific rules, if ever needed, hang off the ancestor
        // (`.proj-row .dial` / `.pcard-head .dial`) rather than a marker
        // class that looks like a styling hook and matches no rule.
        className={`dial ${indicator.state}`.trim()}
        title={indicator.title}
        aria-label={indicator.actionLabel}
        aria-pressed={indicator.paused}
        disabled={runner.busy}
        onClick={(e) => {
          // The row underneath navigates on click, and the card header
          // starts a drag — both have to be left alone.
          e.stopPropagation();
          if (action) runner.run(action);
        }}
        // The real trap: `useRowActions` puts an onKeyDown on the row that
        // preventDefault()s Enter/Space (running the ROW's activate) and
        // claims bare `p` / `r` / `x` as accelerators. `isTypingTarget`
        // does not exclude a <button>, so without this the control is
        // keyboard-dead and typing in it fires row verbs.
        //
        // Modified keys pass straight through, matching the guard the row
        // handler itself opens with. React delegates from the root, so a
        // blanket stop here is a NATIVE stop below every window-level
        // listener — it would have swallowed ⌘K, ⌘B and the view chords
        // for as long as this button held focus.
        onKeyDown={(e) => {
          if (e.metaKey || e.ctrlKey || e.altKey) return;
          e.stopPropagation();
        }}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {indicator.paused ? "⏸" : "▶"}
      </button>
      {runner.dialog}
    </>
  );
}
