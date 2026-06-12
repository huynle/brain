import { useQuery } from "@tanstack/react-query";
import { getProjects } from "../lib/api";

export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: () => getProjects(),
    staleTime: 30_000,
  });
}
