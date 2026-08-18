/**
 * lib/lane — pure flow-board lane visibility math.
 *
 * A collapsed lane shows the first {@link LANE_COLLAPSED_CAP} items
 * and reports how many are hidden so the lane can render a
 * "+N more" toggle. Expanded lanes show everything.
 *
 * Intentionally pure: no react, no hooks. Tested from `lane.test.ts`.
 */

export const LANE_COLLAPSED_CAP = 4;

export function laneVisible<T>(
  items: T[],
  expanded: boolean,
): { visible: T[]; hiddenCount: number } {
  if (expanded || items.length <= LANE_COLLAPSED_CAP) {
    return { visible: items, hiddenCount: 0 };
  }
  return {
    visible: items.slice(0, LANE_COLLAPSED_CAP),
    hiddenCount: items.length - LANE_COLLAPSED_CAP,
  };
}
