import { useQuery } from "@tanstack/react-query";
import { getTimeline } from "../lib/api";

export function useTimeline(from: string, to: string, project?: string) {
  return useQuery({
    queryKey: ["v2", "timeline", from, to, project || "all"],
    queryFn: ({ signal }) => getTimeline({ from, to, project }, signal),
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
}
