import { useQuery } from "@tanstack/react-query";
import {
  getFeatureRunnerCandidates,
  getProposedTaskRunnerCandidates,
  getTaskRunnerCandidates,
} from "../lib/api";

export function useFeatureRunnerCandidates(projectId: string, featureId: string) {
  return useQuery({
    queryKey: ["v2", "runner-candidates", projectId, featureId],
    queryFn: () => getFeatureRunnerCandidates(projectId, featureId),
    enabled: Boolean(projectId && featureId),
    staleTime: 10_000,
  });
}

export function useTaskRunnerCandidates(projectId: string, taskId: string, enabled = true) {
  return useQuery({
    queryKey: ["v2", "runner-candidates", projectId, taskId],
    queryFn: () => getTaskRunnerCandidates(projectId, taskId),
    enabled: enabled && Boolean(projectId && taskId),
    staleTime: 10_000,
  });
}

export function useProposedTaskRunnerCandidates(
  projectId: string,
  spec: Record<string, unknown>,
  enabled = true,
) {
  return useQuery({
    queryKey: ["v2", "runner-candidates", "proposed", projectId, spec],
    queryFn: () => getProposedTaskRunnerCandidates(projectId, spec),
    enabled: enabled && Boolean(projectId),
    staleTime: 10_000,
  });
}
