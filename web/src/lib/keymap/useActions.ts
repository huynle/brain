// React registration hook for keymap scopes — the evolved useViewKeyboard.
// Views declare a static ActionSpec[] (in their keymap.ts) plus handler
// closures over view state; registration follows the component lifetime.

import { useEffect, useSyncExternalStore } from "react";
import { keymapVersion, registerScope, subscribeKeymap, type ScopeTier } from "./registry";
import type { ActionHandlers, ActionSpec } from "./types";

export function useActions(
  scopeId: string,
  tier: ScopeTier,
  specs: ActionSpec[],
  handlers: ActionHandlers,
  deps: unknown[],
): void {
  useEffect(() => {
    return registerScope({ scopeId, tier, specs, handlers });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}

/** Re-render when scopes register/unregister (HelpBar, HelpModal). */
export function useKeymapVersion(): number {
  return useSyncExternalStore(subscribeKeymap, keymapVersion, keymapVersion);
}
