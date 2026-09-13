# Per-feature git delivery (`delivery_mode`)

Brain can automatically **deliver** a completed feature's branch via git: either
open a merge request into the target branch, or squash-merge it into the target
locally and push. This is separate from the review-oriented
`builtin-feature-checkout` automations — delivery actually moves the code.

It is **opt-in and OFF by default**. Nothing is pushed or merged unless (1) the
master switch is on AND (2) a feature explicitly opts a task into a delivery
mode.

## The `delivery_mode` field

`delivery_mode` is a task-level field (also settable in `task_defaults`) with
three values:

| `delivery_mode` | Effect on `feature.completed`                                          |
|-----------------|------------------------------------------------------------------------|
| `none` (default) | Nothing. No push, no merge. (This is the safe default.)               |
| `mr`            | Push the feature branch and open a merge request into the target.       |
| `local_merge`   | Squash-merge the feature branch into the target locally, then push.     |

### Backward-compat bridge (no new field required)

Legacy tasks that only set `merge_policy` are bridged automatically by
`EffectiveDeliveryMode`:

- `merge_policy: auto_pr`   → behaves as `delivery_mode: mr`
- `merge_policy: auto_merge` → behaves as `delivery_mode: local_merge`

An explicit `delivery_mode` always wins over the `merge_policy` bridge.

## How to opt a feature in

Set `delivery_mode` on the feature's task(s). Any of these opts the feature in:

```yaml
# in a task's frontmatter / create request
delivery_mode: mr            # open an MR into merge_target_branch
merge_target_branch: dev
```

```yaml
delivery_mode: local_merge   # squash-merge into an UNPROTECTED target + push
merge_target_branch: staging
remote_branch_policy: delete # delete the remote source branch after merge
```

The mode is **folded across all of a feature's non-generated tasks**
(`foldDeliveryMode`): any `mr` → `mr`; any `local_merge` → `local_merge`; both
present in the same feature → **conflict** (the delivery task blocks with an
actionable message rather than guessing). Missing/`none` on every task → the
feature is not opted in and nothing fires.

`ResolveFeatureDelivery` (`internal/service/feature_delivery.go`) is the
authoritative resolver for the remote, source branch, target branch, strategy,
and remote-branch policy: disagreeing values across a feature's tasks (e.g. a
github remote on one task and a gitlab remote on another, or two different
target branches) are **errors, never a silent pick**.

## Enabling the automation (operator)

The built-in automation `brain:builtin-feature-delivery` is registered at
startup **only when the master switch is on**:

```yaml
# config.yaml
server:
  feature_delivery:
    enabled: true
```

or

```bash
BRAIN_FEATURE_DELIVERY_ENABLED=true
```

While the switch is off, the automation is not registered at all — even an
opted-in `feature.completed` does nothing. Its trigger filter is
`delivery_mode: "in:mr,local_merge"` + `project: "*"`, so only opted-in
features match.

## MR mode vs local-merge mode

### `mr`
- `git push -u origin <feature>` (idempotent, no force).
- `glab mr create --source-branch <feature> --target-branch <target>
  --squash-before-merge` (GitLab). The MR is **opened, not merged**, and the
  **source branch is preserved** for review. `remote_branch_policy` is ignored
  in mr mode.
- On success, `mr_url` + `status: mr_open` are written back onto the feature's
  Brain-native `merge_request` entry (best-effort, non-fatal).
- Opening an MR into a **protected** target is fine and expected — that is the
  whole point of mr mode. There is no protected-branch guard on this path.
- GitHub is **not implemented** (a clear-error stub); unknown providers error.

> The GitLab CLI flag is `--squash-before-merge` (sets the MR to squash when
> accepted). It is **not** `--squash` — older/newer `glab` reject `--squash`,
> and the delivery script fails loudly if that flag is ever used.

### `local_merge`
- Runs a **fail-closed protected-branch guard first** (see below).
- `git -c merge.ff=true merge --squash <feature>` (the `-c merge.ff=true`
  invariant overrides a user's global `merge.ff=no`), commit if anything is
  staged, `git push origin <target>`.
- Idempotent: a re-run where the source branch is already gone locally and
  remotely finishes quietly (`already delivered`).
- Cleanup: remove the worktree, delete the local source branch, and — only if
  the target actually reached the remote and `remote_branch_policy: delete` —
  delete the remote source branch.
- On a successful push, `status: merged` is written back onto the
  `merge_request` entry.

## The protected-branch rule (fail-closed)

The design invariant is **never direct-push a protected target**. In
`local_merge` mode the guard therefore **fails closed**:

1. Query `glab api projects/:id/protected_branches/<target>`.
2. A JSON body naming the branch → **protected → REFUSE** with
   `local_merge refused — set delivery_mode: mr`.
3. A `404` → **not protected → proceed**.
4. **Anything else** — a glab error, glab not installed, or a non-gitlab remote
   that still has a real origin — is **"could not determine" → REFUSE**. An
   indeterminate check never proceeds "best-effort".

The single allowed-through case with no protection oracle is a purely **local
repo with no remote** (there is no protected forge branch to endanger).

`mr` mode has **no** such guard — opening an MR against a protected branch is
the correct move.

## What gets written back

The write-back resolves the feature's `merge_request` entry via
`GET /api/v1/entries?type=merge_request&project=..&feature_id=..` and PATCHes
`/api/v1/entries/{path}/metadata`:

- `mr_url` — audit-only DB-mirror field (in `AllowedMetadataUpdateFields`;
  never written to frontmatter).
- `status` — free-form for `merge_request` entries, so `mr_open` / `merged` are
  accepted with no enum change.

It authenticates with `BRAIN_API_URL` / `BRAIN_API_TOKEN` when set, and is
**best-effort / non-fatal**: a missing tool or failed PATCH only warns, because
the MR/merge is the primary success criterion.

## Constraints

- All git operations run **runner-side** in the feature's worktree — the Brain
  API host has no working copy. Only the create-MR API call is provider-side.
- The GitLab path is the only exercised provider. GitHub delivery is a stub.
