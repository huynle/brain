# Tenant filesystem policy

This package implements the filesystem mapping portion of task `w4597j7v`.
Single-mode startup binds the indexer, watcher, filesystem services and blob
store to this policy before background indexing starts. It does **not** enable
multi mode or authenticate a tenant. Shared-database row ownership remains a
separate release prerequisite.

## Composition

- Open the existing shared database in composition code, then pass its
  `storage.StorageLayer` as the narrow `Repository` to `New(repo, base)`.
  Neither resolver nor mapping has a database path. Schema v28 adds the
  authoritative `tenant_roots` table; reindexing must never clear it.
- Trusted single-mode bootstrap calls `ProvisionLocal(ctx, cfg.BrainDir,
  independentlyConfiguredBlobRoot)`. It requires no user enrollment and creates
  no files. This explicit composition call is not implicit provisioning by a
  read. Repeat calls are idempotent only for identical configuration.
- Only an **already authorized deployment operator** may call `Provision`.
  There is intentionally no boolean `authorized` parameter masquerading as an
  authorization mechanism. Never bind its overrides directly to request data.
- `Lookup` only reads existing mappings, in either mode. Promotion and restart
  retain local; they do not infer ownership from a previous schema version.
  Unknown IDs never provision. Existing nonlocal overrides survive an omitted
  override on repeated provisioning; conflicting explicit overrides fail.
- Registrations are immutable. The repository validates and inserts under one
  SQLite writer transaction, across connections/processes. A failed transaction
  cannot leave a partially registered tenant. Root migration/deletion requires a
  future, separately fenced operator lifecycle procedure, not a mapping upsert.

## Physical layouts

| Mapping | Markdown root | CAS root | Digest suffix |
|---|---|---|---|
| Legacy local | **exact original string** | **exact independent configured string** | `<digest[:2]>/<digest[2:4]>/<digest>` |
| New tenant | `<base>/tenants/<id>` | `<markdown-root>/blobs/<id>/sha256` | `<digest[:2]>/<digest>` |

New CAS storage therefore has the contract's `blobs/<id>/sha256/...` layout
inside the tenant's exclusive tree. Explicit overrides replace the entire
markdown/CAS root, respectively. An out-of-tree legacy CAS stays out of tree.
No directories are created and no bytes are moved. `BlobPath` validates a
lowercase SHA-256 digest and applies the persisted layout plus ownership policy.

Mappings also persist absolute lexical paths and canonical identities. This
anchors relative original strings rather than interpreting them under a later
working directory. Access returns clean absolute **lexical** paths (so unlinking
a final symlink unlinks that link). Both root identities are compared, including
macOS `/var` aliases. Canonical identity drift fails closed. Root configuration
containing `..` is conservatively rejected rather than silently changing its
meaning across symlinks; trailing separators and `.` spelling are retained in
the original root strings.

## Admission hooks

`Brain(id)` and `Blobs(id)` return handles, **not cached grants**:

- `Resolve(ctx, relative)` requires an existing non-root target.
- `ResolveForWrite(ctx, relative)` allows missing suffixes and missing roots.
- `AdmitTraversal(ctx, relative)` admits `.` as the walk start. Call it on
  **every** entry before ingestion/watch registration/read. Prune denied
  directories; do not treat a repository/filesystem error as permission.
- `PreflightDelete(ctx, relative)` refuses both excluded paths **and their
  ancestors**, including missing paths. Invoke before `RemoveAll`. Do not follow
  symlinks in custom recursive deletion; such operations need per-entry admission
  as well. A traversal admission is NOT permission to delete its subtree.

All checks fetch the complete current registration snapshot and validate it.
Repository errors, invalid records, loops, non-directory ancestors, and dangling
symlinks fail closed. Only ENOENT permits a missing suffix. Safe in-root symlinks
are supported; paths that transit foreign roots are rejected even if a later
symlink points back into owned storage. Link targets containing `..` are
conservatively refused.

Foreign markdown **and** blob roots are exclusions. Cross-tenant overlap is
rejected except for a legacy local ancestor with foreign roots strictly inside
its reserved `tenants/` subtree. That subtree is denied to local even before
registration. Same-tenant markdown/CAS nesting is permitted for compatibility.
Ancestors may be traversed with per-entry admission, but cannot be recursively
removed when they contain an exclusion. The trusted repository list is an
operator inventory, not a tenant-facing enumeration API; safe tenant enumeration
is a walk using `AdmitTraversal`.

## Limits, deliberately not solved here

These are path checks, **not atomic filesystem capabilities**. They assume an
operator-controlled stable filesystem. Symlink replacement, mount changes,
case-insensitive filesystem aliases beyond symlinks, hard-linked file contents,
and a registration committed between check and I/O are not prevented by a
returned string. Operators must fence provisioning/FS changes against active
operations; consumers check every traversal entry and propagate errors. An
adversarial mutable filesystem needs OS-level rooted handles/openat-style
operations and a separately reviewed design. Checks cannot replace membership,
lifecycle, or shared-DB row ownership enforcement.

The complete registry is deliberately revalidated per admission (no stale
permission cache). This favors a small auditable interface over scan
performance. Large-registry scan cost has not been benchmarked; any optimization
must retain authoritative invalidation/fail-close semantics.
