package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// BuiltInFeatureDeliveryGeneratedBy is the GeneratedBy marker used to identify
// the built-in git-delivery automation and keep the ensure function
// idempotent. Like the two feature-checkout markers, an entry carrying this
// marker is owned by config+code: it is migrated in place on every boot and
// never hand-edited (edits do not survive a restart).
const BuiltInFeatureDeliveryGeneratedBy = "brain:builtin-feature-delivery"

// builtInDeliveryFilter is the trigger filter for the git-delivery automation.
//
// Like builtInCheckoutFilter, "project":"*" is load-bearing — a global
// automation is inert for project-scoped events without it. The delivery_mode
// "in:mr,local_merge" ensures ONLY opted-in features fire (default "none" does
// not match), so nothing pushes or merges unless a feature explicitly opts in.
func builtInDeliveryFilter() map[string]string {
	return map[string]string{
		"delivery_mode": "in:mr,local_merge",
		"project":       "*",
	}
}

// BuiltInFeatureDeliveryConfig controls the built-in per-feature git-delivery
// automation. It fires on feature.completed events whose folded delivery_mode
// is "mr" or "local_merge" and runs a deterministic git script that either
// pushes the feature branch and opens a merge request (mr) or squash-merges it
// into the target branch locally and pushes (local_merge).
//
// The automation is registered ONCE, globally, at server startup. Like the
// simple checkout automation, its script is generated wholly from this config
// and code — never authored by the user — so the ensure function migrates a
// stored entry's action toward the current rendering on every boot.
type BuiltInFeatureDeliveryConfig struct {
	// Enabled toggles the automation on/off at startup. Sourced from
	// config.FeatureDelivery.Enabled (default OFF). While off, no delivery
	// automation is registered and no feature is ever pushed or merged by
	// this path.
	Enabled bool

	// MergeTargetBranch is the branch a local_merge squash-merges into and
	// the target-branch argument for the mr's merge request. Defaults to
	// "main" in the rendered script if empty.
	MergeTargetBranch string

	// MergeStrategy is retained on the automation entry for parity with the
	// checkout automations. The local_merge path always squash-merges (the
	// Finding-7 invariant), so this is informational for now.
	MergeStrategy string

	// RemoteBranchPolicy controls remote source-branch cleanup after a
	// successful local_merge: "delete" runs `git push origin --delete`;
	// anything else keeps the branch. It is IGNORED in mr mode — the source
	// branch must survive for review.
	RemoteBranchPolicy string

	// TargetWorkdir is the absolute path to the main repo the script runs
	// in. Falls back to the runner-default handling in
	// automation_service.createTask when empty.
	TargetWorkdir string
}

// EnsureBuiltInFeatureDeliveryAutomation registers the built-in git-delivery
// automation. Idempotent by GeneratedBy.
//
// Fires only on feature.completed events whose folded delivery_mode is "mr" or
// "local_merge" (filter delivery_mode:"in:mr,local_merge"). The action is an
// Action.Type == "script" that shells out to git and, for mr mode, to the
// provider CLI (glab). The local_merge path reuses the same Finding-7-invariant
// merge command `git -c merge.ff=true merge --squash <source>` as the simple
// checkout script so it survives users' global gitconfig merge.ff=no.
func EnsureBuiltInFeatureDeliveryAutomation(ctx context.Context, brain *BrainServiceImpl, cfg BuiltInFeatureDeliveryConfig) error {
	if !cfg.Enabled || brain == nil {
		return nil
	}

	desiredTrigger := &types.TriggerConfig{
		Type:    "event",
		Event:   types.EventFeatureCompleted,
		OncePer: "feature_id",
		Filter:  builtInDeliveryFilter(),
	}

	scriptCommand := buildFeatureDeliveryScript(cfg)

	existing, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}
	for _, entry := range existing.Entries {
		if entry.GeneratedBy != BuiltInFeatureDeliveryGeneratedBy {
			continue
		}
		// Migrate the stored entry toward the shape this build wants. Both
		// halves matter, for the same reason they do in the simple checkout
		// ensure: the trigger migration revives entries written before the
		// project wildcard / delivery filter, and the ACTION migration keeps
		// the generated script in lockstep with the code — an entry created
		// by an older build otherwise keeps running an older script forever.
		update := types.UpdateEntryRequest{}
		changed := false
		if triggerNeedsDeliveryMigration(entry.Trigger) {
			update.Trigger = desiredTrigger
			changed = true
		}
		if entry.Action == nil || entry.Action.Command != scriptCommand {
			update.Action = &types.AutomationAction{
				Type:          types.AutomationActionScript,
				Command:       scriptCommand,
				ExecutionMode: "current_branch",
				TargetWorkdir: cfg.TargetWorkdir,
			}
			changed = true
		}
		if changed {
			if _, err := brain.Update(ctx, entry.Path, update); err != nil {
				return fmt.Errorf("migrate built-in feature delivery automation: %w", err)
			}
		}
		return nil
	}

	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "automation",
		Title:       "Built-in feature git delivery (mr/local_merge)",
		Content:     "Deterministic git delivery for opted-in features. On feature.completed with delivery_mode mr or local_merge, pushes the feature branch and opens a merge request (mr) or squash-merges into the target branch and pushes (local_merge). Runs a scripted git sequence — no LLM.",
		Status:      "active",
		Global:      serviceBoolPtr(true),
		Generated:   serviceBoolPtr(true),
		GeneratedBy: BuiltInFeatureDeliveryGeneratedBy,
		Trigger:     desiredTrigger,
		Action: &types.AutomationAction{
			Type:          types.AutomationActionScript,
			Command:       scriptCommand,
			ExecutionMode: "current_branch",
			TargetWorkdir: cfg.TargetWorkdir,
		},
		MergeTargetBranch:  cfg.MergeTargetBranch,
		MergeStrategy:      cfg.MergeStrategy,
		RemoteBranchPolicy: cfg.RemoteBranchPolicy,
		ExecutionMode:      "current_branch",
		TargetWorkdir:      cfg.TargetWorkdir,
	})
	if err != nil {
		return fmt.Errorf("create built-in feature delivery automation: %w", err)
	}
	return nil
}

// triggerNeedsDeliveryMigration reports whether an existing built-in delivery
// automation trigger differs from the shape we now require: the delivery_mode
// membership filter AND the project wildcard that lets a global automation see
// project events at all.
//
// A nil trigger, a nil filter, a wrong delivery_mode, or a missing wildcard
// all count as "needs migration".
func triggerNeedsDeliveryMigration(trigger *types.TriggerConfig) bool {
	if trigger == nil || trigger.Filter == nil {
		return true
	}
	if trigger.Filter["delivery_mode"] != "in:mr,local_merge" {
		return true
	}
	return trigger.Filter["project"] != "*"
}

// featureDeliveryScriptParams are the values baked into one rendering of the
// delivery script. Only one caller (the automation) renders it, but the trio
// mirrors the simple-checkout structure for consistency and testability.
type featureDeliveryScriptParams struct {
	// FeatureExpr and ProjectExpr are single-quoted shell literals as they
	// appear in the script — Brain placeholders ({{.FeatureID}} /
	// {{.ProjectID}}) that renderAutomationTemplate expands at dispatch.
	FeatureExpr string
	ProjectExpr string

	// DeliveryModeExpr is the COMPLETE right-hand side of the DELIVERY_MODE
	// assignment. For the automation it is the {{.DeliveryMode}} placeholder,
	// which renderAutomationTemplate fills from the event's folded
	// metadata["delivery_mode"] — the same in-band path FeatureID/ProjectID
	// flow through. The script also allows an env override as a fallback.
	DeliveryModeExpr string

	// SourceBranchExpr is the COMPLETE right-hand side of the SOURCE_BRANCH
	// assignment. The feature id IS the source branch name; the automation
	// uses "${FEATURE_ID}" (double-quoted so the placeholder value expands
	// at dispatch).
	SourceBranchExpr string

	// TargetBranchExpr is the COMPLETE right-hand side of the per-feature
	// TARGET_BRANCH override. For the automation it is the
	// {{.MergeTargetBranch}} placeholder, which renderAutomationTemplate fills
	// from the event's folded metadata["merge_target_branch"] — the same
	// in-band path DeliveryMode flows through. When it renders empty (no
	// per-feature target, or the feature disagreed), TARGET_BRANCH falls back
	// to TargetBranch (the config default), then "main".
	TargetBranchExpr string

	// TargetBranch is the branch a local_merge squash-merges into and the
	// mr's target branch.
	TargetBranch string

	// MergeStrategy is baked for informational logging.
	MergeStrategy string

	// RemoteDelete deletes the remote source branch after a pushed
	// local_merge (RemoteBranchPolicy=delete). Ignored in mr mode.
	RemoteDelete bool
}

// buildFeatureDeliveryScript returns the automation's rendering: Brain template
// placeholders that renderAutomationTemplate expands at task dispatch time.
// Available placeholders (see renderAutomationTemplate):
//
//	{{.FeatureID}}          — the feature that completed (also the source branch name)
//	{{.ProjectID}}          — automation-owner project ID
//	{{.DeliveryMode}}       — folded delivery mode from event metadata ("mr"/"local_merge")
//	{{.MergeTargetBranch}}  — folded merge target branch from event metadata (may be empty)
func buildFeatureDeliveryScript(cfg BuiltInFeatureDeliveryConfig) string {
	return renderFeatureDeliveryScript(featureDeliveryScriptParams{
		FeatureExpr:      "{{.FeatureID}}",
		ProjectExpr:      "{{.ProjectID}}",
		DeliveryModeExpr: `"{{.DeliveryMode}}"`,
		SourceBranchExpr: `"${FEATURE_ID}"`,
		TargetBranchExpr: `"{{.MergeTargetBranch}}"`,
		TargetBranch:     cfg.MergeTargetBranch,
		MergeStrategy:    cfg.MergeStrategy,
		RemoteDelete:     cfg.RemoteBranchPolicy == "delete",
	})
}

// renderFeatureDeliveryScript renders the delivery script. It implements BOTH
// delivery modes with a runtime switch on DELIVERY_MODE, plus a provider switch
// (gitlab/github/unknown) derived from the origin remote URL. Invariants:
//
//   - local_merge reuses the Finding-7 squash sequence verbatim:
//     `git -c merge.ff=true merge --squash`, re-run guards, worktree cleanup.
//   - local_merge REFUSES a protected target branch (gitlab check via glab).
//   - mr pushes the source branch and opens a merge request; it never merges
//     and never deletes the source branch (review needs it).
//   - github mr is not yet implemented and exits 1 with a clear message.
func renderFeatureDeliveryScript(p featureDeliveryScriptParams) string {
	// Escaped, not filtered — mirrors the simple script's hardening of the
	// caller-supplied target branch.
	target := shellSingleQuoted(strings.TrimSpace(p.TargetBranch))
	if strings.TrimSpace(p.TargetBranch) == "" {
		target = "main"
	}
	strategy := shellSingleQuoted(strings.TrimSpace(p.MergeStrategy))
	if strings.TrimSpace(p.MergeStrategy) == "" {
		strategy = "squash"
	}
	source := strings.TrimSpace(p.SourceBranchExpr)
	if source == "" {
		source = `"${FEATURE_ID}"`
	}
	deliveryMode := strings.TrimSpace(p.DeliveryModeExpr)
	if deliveryMode == "" {
		deliveryMode = `"{{.DeliveryMode}}"`
	}
	targetExpr := strings.TrimSpace(p.TargetBranchExpr)
	if targetExpr == "" {
		targetExpr = `"{{.MergeTargetBranch}}"`
	}

	remoteBlock := "  # Remote source-branch deletion skipped (RemoteBranchPolicy != delete).\n"
	if p.RemoteDelete {
		remoteBlock = `  # Remote source-branch deletion (RemoteBranchPolicy=delete). Same guard as
  # the simple checkout script: never delete the target/default branches, and
  # never delete anything unless the merge actually reached the remote.
  if [ "${PUSHED_TARGET}" != "yes" ]; then
    echo "[feature-delivery] target not pushed; keeping remote ${SOURCE_BRANCH} so the work is not orphaned"
  elif [ "${SOURCE_BRANCH}" != "${TARGET_BRANCH}" ] && [ "${SOURCE_BRANCH}" != "main" ] && [ "${SOURCE_BRANCH}" != "master" ]; then
    echo "[feature-delivery] deleting remote origin/${SOURCE_BRANCH} (best-effort)"
    git push origin --delete "${SOURCE_BRANCH}" || echo "[feature-delivery] remote delete failed or branch already gone (non-fatal)"
  fi
`
	}

	return fmt.Sprintf(
		featureDeliveryScriptTemplate,
		p.FeatureExpr, // 1 FEATURE_ID
		p.ProjectExpr, // 2 PROJECT_ID
		deliveryMode,  // 3 DELIVERY_MODE default expr
		source,        // 4 SOURCE_BRANCH expr
		targetExpr,    // 5 per-feature TARGET_BRANCH override expr
		target,        // 6 TARGET_BRANCH config default
		strategy,      // 7 MERGE_STRATEGY
		remoteBlock,   // 8 remote deletion block (local_merge)
	)
}

// featureDeliveryScriptTemplate is the delivery script body. Eight fmt verbs:
//  1. feature id expression     2. project id expression
//  3. delivery mode default     4. source branch expression
//  5. per-feature target expr   6. target branch config default
//  7. merge strategy            8. remote deletion block (local_merge)
const featureDeliveryScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

# Built-in feature git delivery (mr/local_merge).
# Deterministic — no LLM. Fires only for features that opted into a
# non-"none" delivery_mode (the automation trigger filters on
# delivery_mode:"in:mr,local_merge").

FEATURE_ID='%s'
PROJECT_ID='%s'
# DELIVERY_MODE is resolved in-band from the event's folded
# metadata["delivery_mode"] via the {{.DeliveryMode}} placeholder. An explicit
# ${DELIVERY_MODE} env var (if set non-empty) overrides it, purely as an escape
# hatch for manual re-runs.
DELIVERY_MODE="${DELIVERY_MODE:-}"
if [ -z "${DELIVERY_MODE}" ]; then
  DELIVERY_MODE=%s
fi
SOURCE_BRANCH=%s
# TARGET_BRANCH is resolved in-band from the event's folded
# metadata["merge_target_branch"] via the {{.MergeTargetBranch}} placeholder.
# When the feature carried no target (or disagreed on one) the placeholder
# renders empty and we fall back to the automation's configured default, then
# "main". An explicit ${TARGET_BRANCH} env var (if set non-empty) overrides
# everything, as an escape hatch for manual re-runs.
TARGET_BRANCH="${TARGET_BRANCH:-}"
if [ -z "${TARGET_BRANCH}" ]; then
  TARGET_BRANCH=%s
fi
if [ -z "${TARGET_BRANCH}" ]; then
  TARGET_BRANCH='%s'
fi
MERGE_STRATEGY='%s'

echo "[feature-delivery] project=${PROJECT_ID} feature=${FEATURE_ID}"
echo "[feature-delivery] mode=${DELIVERY_MODE} source=${SOURCE_BRANCH} target=${TARGET_BRANCH} strategy=${MERGE_STRATEGY}"

# brain_patch_merge_request records the delivery outcome onto the feature's
# Brain-native merge_request entry (ADR D6). It is BEST-EFFORT and NON-FATAL:
# the MR/merge is the primary success criterion, so a missing tool or a failed
# write-back only warns — it never fails the task.
#
#   arg1: mr_url  (may be empty; omitted from the PATCH body when empty)
#   arg2: status  (mr_open | merged)
#
# The script knows FEATURE_ID + PROJECT_ID but not the merge_request entry's
# path, so it resolves the path via the Brain list API
# (GET /api/v1/entries?type=merge_request&project=..&feature_id=..), then
# PATCHes metadata at /api/v1/entries/{path}/metadata. Both mr_url (audit-only
# DB mirror) and status (file-syncable) are in AllowedMetadataUpdateFields, so
# the write goes through the metadata endpoint without a full-frontmatter PATCH.
brain_patch_merge_request() {
  local mr_url="${1:-}"
  local mr_status="${2:-}"

  if [ -z "${BRAIN_API_URL:-}" ]; then
    echo "[feature-delivery] BRAIN_API_URL unset; skipping merge_request write-back (non-fatal)"
    return 0
  fi
  if ! command -v curl >/dev/null 2>&1; then
    echo "[feature-delivery] curl not found; skipping merge_request write-back (non-fatal)"
    return 0
  fi

  local auth_args=()
  if [ -n "${BRAIN_API_TOKEN:-}" ]; then
    auth_args=(-H "Authorization: Bearer ${BRAIN_API_TOKEN}")
  fi

  # Resolve the merge_request entry path for this feature+project.
  local list_url="${BRAIN_API_URL%%/}/api/v1/entries?type=merge_request&project=${PROJECT_ID}&feature_id=${FEATURE_ID}&limit=1000"
  local list_body
  list_body="$(curl -fsS ${auth_args[@]+"${auth_args[@]}"} "${list_url}" 2>/dev/null)" || {
    echo "[feature-delivery] merge_request lookup failed (GET ${list_url}); skipping write-back (non-fatal)"
    return 0
  }

  local mr_path=""
  if command -v jq >/dev/null 2>&1; then
    mr_path="$(printf '%%s' "${list_body}" | jq -r '.entries[0].path // empty' 2>/dev/null)"
  else
    # jq-less fallback: pull the first "path":"..." value out of the JSON.
    mr_path="$(printf '%%s' "${list_body}" | grep -Eo '"path"[[:space:]]*:[[:space:]]*"[^"]+"' | head -n1 | sed -E 's/.*"path"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
  fi

  if [ -z "${mr_path}" ]; then
    echo "[feature-delivery] no merge_request entry found for feature=${FEATURE_ID}; skipping write-back (non-fatal)"
    return 0
  fi

  # Build the PATCH body. mr_url is omitted when empty so we never overwrite a
  # previously-captured URL with a blank.
  local body
  if [ -n "${mr_url}" ]; then
    body="$(printf '{"mr_url":"%%s","status":"%%s"}' "${mr_url}" "${mr_status}")"
  else
    body="$(printf '{"status":"%%s"}' "${mr_status}")"
  fi

  local patch_url="${BRAIN_API_URL%%/}/api/v1/entries/${mr_path}/metadata"
  echo "[feature-delivery] recording merge_request status=${mr_status}${mr_url:+ mr_url=${mr_url}} onto ${mr_path}"
  if curl -fsS -X PATCH ${auth_args[@]+"${auth_args[@]}"} -H "Content-Type: application/json" -d "${body}" "${patch_url}" >/dev/null 2>&1; then
    echo "[feature-delivery] merge_request write-back ok"
  else
    echo "[feature-delivery] merge_request write-back failed (PATCH ${patch_url}); non-fatal, MR/merge already done"
  fi
}

# Resolve the origin remote and derive a provider. Everything downstream that
# talks to a forge keys off PROVIDER.
REMOTE_URL="$(git remote get-url origin 2>/dev/null || true)"
PROVIDER=unknown
case "${REMOTE_URL}" in
  *gitlab*) PROVIDER=gitlab ;;
  *github*) PROVIDER=github ;;
esac
echo "[feature-delivery] remote=${REMOTE_URL:-<none>} provider=${PROVIDER}"

# The runner names worktree directories with a SANITIZED branch name
# (runner.sanitizeBranchName), so cleanup must test the sanitized path.
SAFE_BRANCH="$(printf '%%s' "${SOURCE_BRANCH}" | tr '/' '-' | tr -cd 'A-Za-z0-9-_')"
WORKTREE_PATH=".worktrees/${SAFE_BRANCH}"

case "${DELIVERY_MODE}" in
  mr)
    # ---- MR mode: push the source branch and open a merge request. -------
    # Never merge here; never delete the source branch (review needs it).
    # RemoteBranchPolicy is intentionally IGNORED in mr mode.
    if [ -z "${REMOTE_URL}" ]; then
      echo "[feature-delivery] no origin remote; cannot open a merge request"
      exit 1
    fi

    echo "[feature-delivery] pushing ${SOURCE_BRANCH} to origin (idempotent, no force)"
    git push -u origin "${SOURCE_BRANCH}"

    MR_URL=""
    case "${PROVIDER}" in
      gitlab)
        if command -v glab >/dev/null 2>&1; then
          echo "[feature-delivery] opening GitLab MR via glab"
          MR_OUT="$(glab mr create --source-branch "${SOURCE_BRANCH}" --target-branch "${TARGET_BRANCH}" --squash-before-merge --title "feat(${FEATURE_ID}): ${SOURCE_BRANCH} -> ${TARGET_BRANCH}" --description "Automated delivery for feature ${FEATURE_ID}." --yes 2>&1)" || {
            echo "[feature-delivery] glab mr create failed:"
            echo "${MR_OUT}"
            exit 1
          }
          echo "${MR_OUT}"
          MR_URL="$(printf '%%s\n' "${MR_OUT}" | grep -Eo 'https?://[^[:space:]]+' | tail -n1 || true)"
        else
          # Best-effort GitLab REST fallback when glab is not installed.
          echo "[feature-delivery] glab not found; attempting GitLab REST API fallback"
          GL_TOKEN="${GITLAB_TOKEN:-${CI_JOB_TOKEN:-}}"
          if [ -z "${GL_TOKEN}" ]; then
            echo "[feature-delivery] no GITLAB_TOKEN or CI_JOB_TOKEN set; cannot open MR via REST. Install glab or set a token."
            exit 1
          fi
          echo "[feature-delivery] GitLab REST MR fallback requires project id resolution; set up glab for reliable MR creation."
          # Minimal, documented best-effort: rely on glab in practice.
          exit 1
        fi
        ;;
      github)
        echo "[feature-delivery] GitHub delivery not yet implemented — use local_merge on an unprotected branch or open the PR manually"
        exit 1
        ;;
      *)
        echo "[feature-delivery] unknown provider for remote ${REMOTE_URL}; cannot open MR"
        exit 1
        ;;
    esac

    if [ -n "${MR_URL}" ]; then
      echo "[feature-delivery] MR opened: ${MR_URL}"
      # Record the MR URL + status onto the feature's merge_request entry
      # (ADR D6). Best-effort/non-fatal — the MR is already open, which is the
      # primary success criterion. status "mr_open" is accepted by the metadata
      # PATCH allowlist (merge_request statuses are free-form; no enum rejects it).
      brain_patch_merge_request "${MR_URL}" "mr_open"
    else
      echo "[feature-delivery] MR created but could not capture its URL from output"
      # Still advance the merge_request lifecycle to mr_open even without a URL.
      brain_patch_merge_request "" "mr_open"
    fi
    echo "[feature-delivery] done (mr)"
    ;;

  local_merge)
    # ---- local_merge mode: squash-merge into target locally, then push. --
    # Protected-branch guard FIRST — refuse to merge into a protected target.
    # The guard is FAIL-CLOSED for local_merge: the design invariant is
    # "never direct-push a protected target", so if protection status cannot
    # be positively determined we REFUSE rather than proceed. glab is queried
    # with output+status captured (not >/dev/null), because a bare boolean
    # cannot tell "protected" (branch in the protected list) from "not
    # protected" (404) from "could not check" (auth/host/network error) — and
    # those three demand different answers.
    if [ "${PROVIDER}" = "gitlab" ]; then
      if command -v glab >/dev/null 2>&1; then
        PB_OUT="$(glab api "projects/:id/protected_branches/${TARGET_BRANCH}" 2>&1)" && PB_RC=0 || PB_RC=$?
        if [ "${PB_RC}" -eq 0 ] && printf '%%s' "${PB_OUT}" | grep -q '"name"'; then
          echo "[feature-delivery] target ${TARGET_BRANCH} is protected on ${REMOTE_URL}; local_merge refused — set delivery_mode: mr"
          exit 1
        elif printf '%%s' "${PB_OUT}" | grep -Eq '404|Not Found|not found'; then
          echo "[feature-delivery] target ${TARGET_BRANCH} is not protected; proceeding with local_merge"
        else
          echo "[feature-delivery] could not determine protected-branch status for ${TARGET_BRANCH} (glab rc=${PB_RC}): ${PB_OUT}"
          echo "[feature-delivery] refusing local_merge (fail-closed) — verify the branch is unprotected, or use delivery_mode: mr"
          exit 1
        fi
      else
        echo "[feature-delivery] provider is gitlab but glab is not installed; cannot verify protected-branch status"
        echo "[feature-delivery] refusing local_merge (fail-closed) — install glab, or use delivery_mode: mr"
        exit 1
      fi
    else
      # Non-gitlab provider (github stub / unknown / no remote): we have no
      # protection oracle. For a real remote this is fail-closed; a purely
      # local repo with no remote is allowed through (nothing gets pushed to
      # a protected forge branch in that case).
      if [ -n "${REMOTE_URL}" ]; then
        echo "[feature-delivery] cannot verify protected-branch status for provider=${PROVIDER} on ${REMOTE_URL}"
        echo "[feature-delivery] refusing local_merge (fail-closed) — use delivery_mode: mr, or verify the target is unprotected"
        exit 1
      fi
      echo "[feature-delivery] no remote; proceeding with local-only merge (no protected forge branch to endanger)"
    fi

    # Guardrail: never merge the target into itself.
    if [ "${SOURCE_BRANCH}" = "${TARGET_BRANCH}" ]; then
      echo "[feature-delivery] source and target are identical (${TARGET_BRANCH}); nothing to do"
      exit 0
    fi

    git checkout "${TARGET_BRANCH}"

    # Re-run guard: if the source is gone locally and remotely, a prior run
    # already merged and cleaned up; finish quietly.
    if ! git rev-parse --verify --quiet "${SOURCE_BRANCH}" >/dev/null 2>&1 &&
       ! git rev-parse --verify --quiet "origin/${SOURCE_BRANCH}" >/dev/null 2>&1; then
      echo "[feature-delivery] source branch ${SOURCE_BRANCH} no longer exists; already delivered"
      exit 0
    fi

    # Finding-7 invariant: -c merge.ff=true overrides user gitconfig merge.ff=no.
    echo "[feature-delivery] squash-merging ${SOURCE_BRANCH} into ${TARGET_BRANCH}"
    git -c merge.ff=true merge --squash "${SOURCE_BRANCH}"

    if git diff --cached --quiet; then
      echo "[feature-delivery] nothing staged; ${SOURCE_BRANCH} is already merged into ${TARGET_BRANCH}"
    else
      git commit -m "feat(${FEATURE_ID}): squash merge from ${SOURCE_BRANCH}"
    fi

    PUSHED_TARGET=no
    if [ -n "${REMOTE_URL}" ]; then
      echo "[feature-delivery] pushing ${TARGET_BRANCH} to origin"
      git push origin "${TARGET_BRANCH}"
      PUSHED_TARGET=yes
    else
      echo "[feature-delivery] no origin remote; leaving ${TARGET_BRANCH} local-only"
    fi

    # Idempotent worktree cleanup.
    if [ -d "${WORKTREE_PATH}" ]; then
      echo "[feature-delivery] removing worktree ${WORKTREE_PATH}"
      git worktree remove --force "${WORKTREE_PATH}" || true
    else
      echo "[feature-delivery] no worktree at ${WORKTREE_PATH}; skipping"
    fi

    # Local branch deletion (idempotent, best-effort).
    if git rev-parse --verify --quiet "${SOURCE_BRANCH}" >/dev/null; then
      echo "[feature-delivery] deleting local branch ${SOURCE_BRANCH}"
      git branch -D "${SOURCE_BRANCH}" || echo "[feature-delivery] could not delete ${SOURCE_BRANCH} (non-fatal)"
    else
      echo "[feature-delivery] local branch ${SOURCE_BRANCH} already gone; skipping"
    fi

%s
    # Record the merged status onto the feature's merge_request entry (ADR D6)
    # only if the target actually reached the remote. Best-effort/non-fatal.
    if [ "${PUSHED_TARGET}" = "yes" ]; then
      brain_patch_merge_request "" "merged"
    fi
    echo "[feature-delivery] done (local_merge)"
    ;;

  *)
    # Safety net — the automation filter should keep this from ever firing.
    echo "[feature-delivery] delivery_mode='${DELIVERY_MODE}' not mr|local_merge; nothing to do"
    exit 0
    ;;
esac
`
