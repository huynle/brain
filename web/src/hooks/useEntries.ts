/**
 * Entries browser data hooks.
 *
 * Entry data doesn't ride SSE (only tasks do), so these follow the
 * react-query polling + focus-refetch pattern from `useProjects` /
 * `useGoals`. The list hook executes the fan-out plan from
 * `lib/entries.ts` (one request per type for the "knowledge"/"all"
 * modes) and merges client-side.
 */
import { useQuery } from "@tanstack/react-query";
import {
  getBacklinks,
  getBrainStats,
  getEntry,
  getRelated,
  listEntries,
  search,
} from "../lib/api";
import type { BrainEntry, SearchStrategy } from "../lib/types";
import {
  buildListPlan,
  mergeEntryLists,
  KNOWLEDGE_TYPES,
  type EntryListFilters,
} from "../lib/entries";

const EMPTY_ENTRIES: BrainEntry[] = [];

export function useEntryList(filters: EntryListFilters) {
  const q = useQuery({
    queryKey: [
      "entries",
      "list",
      filters.typeFilter,
      filters.projectFilter,
      filters.statusFilter,
      filters.sortBy,
      filters.sortOrder,
    ],
    queryFn: async () => {
      const plan = buildListPlan(filters);
      let failures = 0;
      let lastError: unknown = null;
      const lists = await Promise.all(
        plan.map((call) =>
          listEntries(call)
            .then((r) => r.entries || [])
            // One type failing (e.g. nothing indexed yet) shouldn't
            // blank the whole browser…
            .catch((err) => {
              failures++;
              lastError = err;
              return [] as BrainEntry[];
            }),
        ),
      );
      // …but if EVERY call failed (server down, auth expired), surface
      // the error instead of rendering an empty brain.
      if (plan.length > 0 && failures === plan.length) {
        throw lastError instanceof Error
          ? lastError
          : new Error("All entry list requests failed");
      }
      return mergeEntryLists(lists, filters.sortBy, filters.sortOrder);
    },
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    placeholderData: (prev) => prev,
  });
  return {
    entries: q.data ?? EMPTY_ENTRIES,
    loading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
    refetch: q.refetch,
  };
}

export function useEntry(path: string | null) {
  const q = useQuery({
    queryKey: ["entries", "entry", path],
    queryFn: () => getEntry(path!),
    enabled: !!path,
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });
  return {
    entry: q.data ?? null,
    loading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
  };
}

export interface EntrySearchFilters {
  /** "knowledge" filters results client-side; a concrete type is passed
   *  to the API; "all" searches everything. */
  typeFilter: string;
  projectFilter: string;
  statusFilter: string;
}

export function useEntrySearch(
  query: string,
  strategy: SearchStrategy,
  filters: EntrySearchFilters,
) {
  const trimmed = query.trim();
  const enabled = trimmed.length >= 2;
  const q = useQuery({
    queryKey: [
      "entries",
      "search",
      trimmed,
      strategy,
      filters.typeFilter,
      filters.projectFilter,
      filters.statusFilter,
    ],
    queryFn: async () => {
      const concreteType =
        filters.typeFilter !== "knowledge" && filters.typeFilter !== "all"
          ? filters.typeFilter
          : undefined;
      const res = await search({
        query: trimmed,
        strategy,
        limit: 50,
        ...(concreteType ? { type: concreteType } : {}),
        ...(filters.statusFilter ? { status: filters.statusFilter } : {}),
        ...(filters.projectFilter === "global"
          ? { global: true }
          : filters.projectFilter
            ? { project: filters.projectFilter }
            : {}),
      });
      let results = res.results || [];
      if (filters.typeFilter === "knowledge") {
        const knowledge = new Set<string>(KNOWLEDGE_TYPES);
        results = results.filter((r) => knowledge.has(r.type));
      }
      return results;
    },
    enabled,
    staleTime: 30_000,
    placeholderData: (prev) => prev,
  });
  return {
    results: q.data ?? [],
    searching: enabled && q.isPending && q.fetchStatus !== "idle",
    error: q.error,
    enabled,
  };
}

/** Backlinks + related entries for the reader footer. */
export function useEntryGraph(id: string | undefined) {
  const backlinks = useQuery({
    queryKey: ["entries", "backlinks", id],
    queryFn: () => getBacklinks(id!),
    enabled: !!id,
    staleTime: 60_000,
  });
  const related = useQuery({
    queryKey: ["entries", "related", id],
    queryFn: () => getRelated(id!, 8),
    enabled: !!id,
    staleTime: 60_000,
  });
  return {
    backlinks: backlinks.data ?? EMPTY_ENTRIES,
    related: related.data ?? EMPTY_ENTRIES,
  };
}

/** Entry counts by type, for the type-filter chips. */
export function useBrainStats(project?: string) {
  const q = useQuery({
    queryKey: ["entries", "stats", project || ""],
    queryFn: () =>
      getBrainStats(
        project && project !== "global" ? project : undefined,
        project === "global",
      ),
    staleTime: 60_000,
    refetchOnWindowFocus: true,
  });
  return { stats: q.data ?? null };
}
