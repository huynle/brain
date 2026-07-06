import { useQuery } from "@tanstack/react-query";
import { getProjects } from "../lib/api";

// useProjects fetches the list of project IDs known to the Brain API.
//
// The API surfaces new project namespaces as soon as the first entry is saved
// to them. To keep the picker / status bar / dashboard honest in long-lived
// PWA sessions where the user may have created a project from another tab or
// agent, the query refetches on a modest interval and on window focus. The
// ProjectSheet additionally invalidates this query when it opens for an
// immediate refresh.
export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: () => getProjects(),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
  });
}
