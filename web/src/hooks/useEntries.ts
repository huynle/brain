/**
 * Entries browser data hooks.
 *
 * Entry data doesn't ride SSE (only tasks do), so these follow the
 * react-query polling + focus-refetch pattern from `useProjects` /
 * `useGoals`. The list hook executes the fan-out plan from
 * `lib/entries.ts` (one request per type for the "knowledge"/"all"
 * modes) and merges client-side.
 */
import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getBacklinks,
  getBrainStats,
  getEntry,
  getRelated,
  listEntries,
  search,
} from "../lib/api";
import { MIN_ENTRY_QUERY, rankEntryHits } from "../lib/paletteEntries";
import type { BrainEntry, SearchResult, SearchStrategy } from "../lib/types";
import {
  buildListPlan,
  mergeEntryLists,
  scopeKey,
  scopeProjectsParam,
  KNOWLEDGE_TYPES,
  type EntryListFilters,
  type ProjectScope,
} from "../lib/entries";

const EMPTY_ENTRIES: BrainEntry[] = [];
const EMPTY_HITS: SearchResult[] = [];

export function useEntryList(filters: EntryListFilters) {
  const q = useQuery({
    queryKey: [
      "entries",
      "list",
      filters.typeFilter,
      scopeKey(filters.scope),
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
  /** Already resolved against the sidebar — see resolveProjectScope. */
  scope: ProjectScope;
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
      scopeKey(filters.scope),
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
        ...(filters.scope.kind === "global"
          ? { global: true }
          : filters.scope.kind === "project"
            ? { project: filters.scope.project }
            : filters.scope.kind === "set"
              ? { projects: filters.scope.projects }
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

/**
 * Entry lookup for the command palette: one debounced, unscoped search.
 *
 * Three things separate it from `useEntrySearch`:
 *
 *  - **Unscoped by design.** The browser follows the sidebar's project
 *    selection; the palette is a jump-anywhere surface, so looking up a
 *    short id has to find the entry whether or not its project happens
 *    to be visible right now.
 *  - **`fts`, not the user's saved strategy.** A palette wants a fast,
 *    predictable answer, and an id match is exact — `hybrid` would add
 *    an embedding round trip to buy nothing here.
 *  - **Debounced internally.** The browser debounces in the component
 *    because its input is the one the user filters with; the palette's
 *    input is shared with the local command filter, which must stay
 *    instant, so only the request half is delayed.
 *
 * `active` gates the request so a closed palette never polls.
 */
export function usePaletteEntrySearch(query: string, active: boolean) {
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    if (!active) {
      setDebounced("");
      return;
    }
    const t = window.setTimeout(() => setDebounced(query.trim()), 180);
    return () => window.clearTimeout(t);
  }, [query, active]);

  const enabled = active && debounced.length >= MIN_ENTRY_QUERY;
  const q = useQuery({
    queryKey: ["entries", "palette-search", debounced],
    queryFn: () =>
      search({ query: debounced, strategy: "fts", limit: 25 }).then(
        (r) => r.results || [],
      ),
    enabled,
    staleTime: 30_000,
  });

  const results = useMemo(
    () => (enabled ? rankEntryHits(debounced, q.data ?? EMPTY_HITS) : EMPTY_HITS),
    [enabled, debounced, q.data],
  );

  return {
    results,
    searching: enabled && q.isPending && q.fetchStatus !== "idle",
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

/** Entry counts by type, for the type-filter chips. Scoped exactly like
 *  the list below them — chip counts that span projects the list can't
 *  show would send the user hunting for entries that never appear. */
export function useBrainStats(scope: ProjectScope) {
  const q = useQuery({
    queryKey: ["entries", "stats", scopeKey(scope)],
    queryFn: () =>
      getBrainStats(
        scope.kind === "project" ? scope.project : undefined,
        scope.kind === "global",
        scopeProjectsParam(scope),
      ),
    staleTime: 60_000,
    refetchOnWindowFocus: true,
  });
  return { stats: q.data ?? null };
}
