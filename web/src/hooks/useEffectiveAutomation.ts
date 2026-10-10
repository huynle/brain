/**
 * useEffectiveAutomation — the config one project runs a global automation under.
 *
 * Wraps GET /automations/{id}/effective with react-query. Each field reports
 * whether it is inherited from the global automation or overridden by this
 * project's binding, and `broken` says the server cannot vouch for the view.
 *
 * An empty id or project disables the query, so a caller can call the hook
 * before the automation is known. Binding writes invalidate the
 * ["v2", "automation-effective", project] prefix (see useAutomationActionContext).
 */
import { useQuery } from "@tanstack/react-query";

import { getAutomationEffective } from "../lib/api";
import type { AutomationEffectiveView } from "../lib/automationBindings";

export interface UseEffectiveAutomationResult {
  effective: AutomationEffectiveView | null;
  isLoading: boolean;
  error: unknown;
  refresh: () => void;
}

export function useEffectiveAutomation(
  automationId: string,
  projectId: string,
): UseEffectiveAutomationResult {
  const enabled = !!automationId && !!projectId;
  const q = useQuery({
    queryKey: ["v2", "automation-effective", projectId, automationId],
    queryFn: () => getAutomationEffective(automationId, projectId),
    enabled,
    staleTime: 15_000,
    // One retry rides out a blip. A 400 or 404 is an answer, not a blip.
    retry: 1,
  });

  return {
    effective: q.data ?? null,
    isLoading: enabled && q.isPending && q.fetchStatus !== "idle",
    error: q.error,
    refresh: () => void q.refetch(),
  };
}
