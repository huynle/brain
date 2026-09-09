# Runner-scoped OpenCode listener discovery (P2.5 phase 1)

The session-history HTTP fallback discovers only listeners whose PID belongs
to this runner's tracked OpenCode process trees. Roots are task drivers
(including tmux shell parents), their separately tracked executor `ServePID`
processes, and bridge ad-hoc OpenCode processes. A tracked serve process can
remain eligible after its driver exits. Reservations and instance metadata
without a tracked process confer no ownership.

Discovery requires an explicit owned-PID set. Before using cached candidates,
the bridge rebuilds ownership from current tracking and a fresh process-table
snapshot, filtering **before HTTP**. Missing tracking, absent processes, and
inspection errors grant no ownership. `ExistingSessionIDs` is a discovery
baseline, never authorization. Session creation and `--attach --session`
pinning are unchanged.

This is a PID/process-snapshot boundary, **not a sandbox**. Process exit,
reparenting, PID/port reuse and concurrent changes between snapshots and HTTP
remain possible; cached listener bindings are short-lived snapshots, not
authenticated endpoint identities. P7 principal-to-runner credential identity
is still required. Runner ownership also does not authorize every transcript
an OpenCode server's store-wide API can return. On-disk SQLite/JSON history
scope is deliberately unchanged: this phase does not provide full transcript
authorization.
