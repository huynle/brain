# P4.9 bounded performance follow-up

P4.9 measures graph construction/cache behavior, **not release load readiness**.
Keep runtime multi activation closed. Preserve the complete measurements and
matched single-mode comparison in [the evidence](p4-tenant-graph-evidence.md).

## Attribution: measured boundary versus source hypothesis

At 300 roots, capacity-32 cold acquisition took 826.692 ms, with 826.666 ms
inside the factory: only 25,138 ns per operation lies outside construction.
Each miss allocated 140,703,904 bytes in 3,386,060 allocations, while retaining
254,826 bytes per resident graph. Retained heap is not peak heap, RSS, or
allocation traffic. Independent verification reproduced approximately 834 ms
and 140.45 MB per miss.

The strongest source-backed suspect is repeated full-registry root validation:

- `newTenantGraph` calls `Resolver.Lookup`.
- `NewTenantFilesystemStore` calls `Lookup` again, then resolves `.staging`,
  which calls `Root.check` and takes another validated snapshot.
- Each `Resolver.snapshot` reads all registrations, validates lexical and
  canonical root identities, and compares cross-tenant root pairs.
- `tenantfs.validate` uses `within`, which calls `filepath.Rel`. At 300
  registrations, three validations imply 2,152,800 base containment calls and
  1,800 root identity calls per construction (source-derived counts, plus
  legacy-exception checks). The SQL snapshots read 900 mapping rows in total.

This is algorithmic attribution, **not profile-confirmed allocation percentages**.
CPU and allocation-stack attribution remains an explicit follow-up gate.
Whole-process benchmark profiles include expensive provisioning, warmups,
calibration and cleanup even when benchmark timers are stopped. Filter stacks
through `newTenantGraph`/manager construction to distinguish provisioning, or
use a measured-region profiling harness; do not divide whole-process profile
totals by the reported timed iteration count.

## Bounded optimization sequence

1. Profile CPU, `alloc_space`, and `alloc_objects` through construction stacks.
   First experiment: an allocation-free containment predicate for proven clean
   absolute paths, retaining a conservative `filepath.Rel` fallback. Differential
   tests must establish identical behavior. Keep every fresh snapshot and full
   registry/filesystem validation; this changes computation, not authority.
2. Separately replace pairwise overlap validation with a sorted component index
   or trie rebuilt from each fresh complete snapshot. Target O(N log N + path
   bytes) work and O(N) temporary memory. Preserve lexical/canonical comparisons,
   ownership, same-tenant nesting and the exact legacy-local `tenants/` exception.
3. Re-measure 100/300/500 roots, both serial and simultaneous misses, before
   selecting production concurrency/memory admission limits. Capacity 32 bounds
   graph count, not transient allocation traffic. Do not cache validated authority
   snapshots or skip other registrations to obtain a faster benchmark.

## Required verification and phase ownership

- Tenantfs/storage optimization: differential/property tests for sibling prefixes,
  aliases, filesystem drift, foreign/dangling symlinks, missing suffixes,
  permission/ENOTDIR errors, destructive ancestor exclusion and legacy roots.
  Preserve the SQLite writer reservation before complete-registry validation
  during registration; test conflicting independent connections/processes and
  rollback without replacing mappings.
- Cache lifecycle: retain single-flight, pinned/retiring/closing capacity,
  suspension during construction/requests, stale-generation rejection and
  shared-pool shutdown ordering tests.
- P10/load gate: measure peak heap/RSS, GC CPU, allocation rate and sustained
  churn, including populated hubs, blocked leases and concurrent misses. Run
  actual HTTP/search with concurrent writers at 100/500 tenants, warm graph
  filesystem admission, fairness/cancellation and the approved search latency
  target. Synthetic hit rates alone cannot satisfy this gate.
- P6/P8: online authority, output/commit fencing, and durable revocation remain
  required; request cancellation cannot retract previously delivered output.

The matched single-mode baseline comparison is complete: fresh-empty median
10.358542 ms before versus 10.343000 ms after; initialized-empty 3.181687 ms
before versus 3.117375 ms after. Thirty independent paired observations per
fixture show no meaningful regression within the declared tolerance; they do
not measure network readiness or indexing completion.
