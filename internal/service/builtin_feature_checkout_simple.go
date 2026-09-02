package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// BuiltInFeatureCheckoutSimpleGeneratedBy is the GeneratedBy marker used to
// identify the deterministic script-based feature-checkout automation and
// keep the ensure function idempotent.
const BuiltInFeatureCheckoutSimpleGeneratedBy = "brain:builtin-feature-checkout-simple"

// BuiltInFeatureCheckoutSimpleConfig controls the deterministic script-based
// feature checkout automation registered alongside the AI one.
//
// The AI automation carries checkout_mode:"ai" as its trigger filter; this
// automation carries checkout_mode:"simple". CheckFeatureCompletion folds
// checkout_mode across all feature tasks and sets it in the event metadata,
// so only one of the two automations fires per feature completion.
type BuiltInFeatureCheckoutSimpleConfig struct {
	// Enabled toggles the automation on/off at startup. The AI enable flag
	// controls this in practice (see apiserver/server.go) — if the user
	// wants to disable ONLY the simple path they can archive the
	// automation entry via the existing UX.
	Enabled bool

	// MergeTargetBranch is the branch that the feature branch is squash-
	// merged into. Defaults to "main" in the rendered script if empty.
	MergeTargetBranch string

	// RemoteBranchPolicy controls remote cleanup after a successful merge:
	// "delete" runs `git push origin --delete <branch>`; anything else
	// (including "keep") skips remote deletion.
	RemoteBranchPolicy string

	// TargetWorkdir is the absolute path to the main repo that the script
	// executes in. Falls back to the runner-default handling in
	// automation_service.createTask when empty (which uses /tmp — the
	// simple automation is not useful in that state and callers should
	// pass a real repo path).
	TargetWorkdir string
}

// EnsureBuiltInFeatureCheckoutSimpleAutomation registers the deterministic
// squash-merge feature-checkout automation. Idempotent by GeneratedBy.
//
// Fires only on feature.completed events whose folded checkout_mode is
// "simple". The action is an Action.Type == "script" that shells out to git
// using the Finding-7-invariant merge command
// `git -c merge.ff=true merge --squash <source>` so it survives users'
// global gitconfig `merge.ff = no` setting.
func EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx context.Context, brain *BrainServiceImpl, cfg BuiltInFeatureCheckoutSimpleConfig) error {
	if !cfg.Enabled || brain == nil {
		return nil
	}

	desiredTrigger := &types.TriggerConfig{
		Type:    "event",
		Event:   types.EventFeatureCompleted,
		OncePer: "feature_id",
		Filter:  builtInCheckoutFilter("simple"),
	}

	scriptCommand := buildSimpleFeatureCheckoutScript(cfg)

	existing, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}
	for _, entry := range existing.Entries {
		if entry.GeneratedBy != BuiltInFeatureCheckoutSimpleGeneratedBy {
			continue
		}
		// Migrate the stored entry toward the shape this build wants.
		//
		// Both halves matter. The trigger migration revives entries written
		// before the project wildcard. The ACTION migration matters just as
		// much: the script is generated wholly from config and code, never
		// authored by the user, so an entry created by an older build keeps
		// running an older script forever — a fix to the script would never
		// reach any existing install. That is exactly how the missing
		// `git push` survived: the code was fixed, the stored automation was
		// not.
		update := types.UpdateEntryRequest{}
		changed := false
		if triggerNeedsCheckoutMigration(entry.Trigger, "simple") {
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
				return fmt.Errorf("migrate built-in feature checkout simple automation: %w", err)
			}
		}
		return nil
	}

	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "automation",
		Title:       "Built-in feature checkout (simple/script)",
		Content:     "Deterministic squash-merge feature checkout. Runs a scripted git sequence — no LLM.",
		Status:      "active",
		Global:      serviceBoolPtr(true),
		Generated:   serviceBoolPtr(true),
		GeneratedBy: BuiltInFeatureCheckoutSimpleGeneratedBy,
		Trigger:     desiredTrigger,
		Action: &types.AutomationAction{
			Type:          types.AutomationActionScript,
			Command:       scriptCommand,
			ExecutionMode: "current_branch",
			TargetWorkdir: cfg.TargetWorkdir,
		},
		MergeTargetBranch:  cfg.MergeTargetBranch,
		RemoteBranchPolicy: cfg.RemoteBranchPolicy,
		ExecutionMode:      "current_branch",
		TargetWorkdir:      cfg.TargetWorkdir,
	})
	if err != nil {
		return fmt.Errorf("create built-in feature checkout simple automation: %w", err)
	}
	return nil
}

// simpleCheckoutScriptParams are the values baked into one rendering of the
// deterministic checkout script.
//
// Two callers render the same body from different sources, and they MUST NOT
// drift: the built-in automation renders it once at ensure time with Brain
// template placeholders that renderAutomationTemplate expands at dispatch,
// while TaskServiceImpl.CheckoutFeature renders it with concrete ids because
// the manual endpoint has no automation to expand anything. Sharing one
// template is the only reason a fix to the merge sequence reaches both paths.
type simpleCheckoutScriptParams struct {
	// FeatureExpr and ProjectExpr are single-quoted shell literals as they
	// appear in the script — either a Brain placeholder ({{.FeatureID}})
	// for the automation, or a real id for the manual path.
	FeatureExpr string
	ProjectExpr string

	// SourceBranch is a shell expression evaluated inside the script. It is
	// "${FEATURE_ID}" unless a caller pinned an explicit execution branch.
	SourceBranch string

	// TargetBranch is the branch the feature is squash-merged into.
	TargetBranch string

	// RemoteDelete deletes the remote source branch after a pushed merge.
	RemoteDelete bool
}

// buildSimpleFeatureCheckoutScript returns the automation's rendering: Brain
// template placeholders that renderAutomationTemplate expands at task
// dispatch time. Available placeholders:
//
//	{{.Project}}     — automation-owner project ID
//	{{.ProjectID}}   — same as .Project
//	{{.FeatureID}}   — the feature that completed
//	{{.TaskID}}      — the task whose status change triggered completion
//	{{.EventProjectID}} — source-event project (differs for cross-project)
func buildSimpleFeatureCheckoutScript(cfg BuiltInFeatureCheckoutSimpleConfig) string {
	return renderSimpleFeatureCheckoutScript(simpleCheckoutScriptParams{
		FeatureExpr:  "{{.FeatureID}}",
		ProjectExpr:  "{{.ProjectID}}",
		SourceBranch: "${FEATURE_ID}",
		TargetBranch: cfg.MergeTargetBranch,
		RemoteDelete: cfg.RemoteBranchPolicy == "delete",
	})
}

// renderSimpleFeatureCheckoutScript renders the deterministic squash-merge
// sequence. It honors the following invariants:
//
//   - Finding 7: `git -c merge.ff=true merge --squash` (never bare
//     `git merge --squash`, which collides with global merge.ff=no).
//   - Re-runnable: a second run after a successful merge exits 0 rather
//     than dying on "nothing to commit". This matters because the runner
//     retries up to 3 times, and every failure AFTER the merge — a push
//     hiccup, a branch still checked out in a worktree — would otherwise
//     burn all three attempts on work that had already landed.
//   - Fails loudly on merge conflicts (no auto-resolution — that's the AI
//     path's job).
//   - Never deletes the merge target branch.
func renderSimpleFeatureCheckoutScript(p simpleCheckoutScriptParams) string {
	target := strings.TrimSpace(p.TargetBranch)
	if target == "" {
		target = "main"
	}
	source := strings.TrimSpace(p.SourceBranch)
	if source == "" {
		source = "${FEATURE_ID}"
	}

	remoteBlock := "# Remote branch deletion skipped (RemoteBranchPolicy != delete).\n"
	if p.RemoteDelete {
		// PUSHED_TARGET gates this block. Deleting the remote source branch
		// while the merge exists only locally destroys the only shared copy
		// of the work: the remote loses the feature branch and never gains
		// the commit. Only delete once the target is safely on the remote.
		remoteBlock = `# Remote branch deletion (RemoteBranchPolicy=delete).
# Guardrails: never delete the merge target or default branches, and never
# delete anything unless the merge has actually reached the remote.
if [ "${PUSHED_TARGET}" != "yes" ]; then
  echo "[feature-checkout-simple] target not pushed; keeping remote ${SOURCE_BRANCH} so the work is not orphaned"
elif [ "${SOURCE_BRANCH}" != "${TARGET_BRANCH}" ] && [ "${SOURCE_BRANCH}" != "main" ] && [ "${SOURCE_BRANCH}" != "master" ]; then
  echo "[feature-checkout-simple] deleting remote origin/${SOURCE_BRANCH} (best-effort)"
  git push origin --delete "${SOURCE_BRANCH}" || echo "[feature-checkout-simple] remote delete failed or branch already gone (non-fatal)"
fi
`
	}

	return fmt.Sprintf(
		simpleFeatureCheckoutScriptTemplate,
		p.FeatureExpr, p.ProjectExpr, target, source, remoteBlock,
	)
}

// simpleFeatureCheckoutScriptTemplate is the deterministic squash-merge
// script body. It uses five fmt verbs, all embedded at render time:
//  1. feature id expression   2. project id expression
//  3. target branch           4. source branch expression
//  5. remote deletion block
const simpleFeatureCheckoutScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

# Built-in feature checkout (simple/script).
# Deterministic squash-merge — no LLM.

FEATURE_ID='%s'
PROJECT_ID='%s'
TARGET_BRANCH='%s'
SOURCE_BRANCH="%s"

# The runner names worktree directories with a SANITIZED branch name
# (runner.sanitizeBranchName: "/" becomes "-", everything outside
# [A-Za-z0-9-_] is stripped), so a feature id like "feat/x" lives in
# .worktrees/feat-x. Testing the raw name here missed the directory
# entirely, skipped cleanup, and then died on ` + "`git branch -D`" + ` with
# "cannot delete branch used by worktree" — AFTER the merge and push had
# already landed.
SAFE_BRANCH="$(printf '%%s' "${SOURCE_BRANCH}" | tr '/' '-' | tr -cd 'A-Za-z0-9-_')"
WORKTREE_PATH=".worktrees/${SAFE_BRANCH}"

echo "[feature-checkout-simple] project=${PROJECT_ID} feature=${FEATURE_ID}"
echo "[feature-checkout-simple] source=${SOURCE_BRANCH} target=${TARGET_BRANCH}"

# Guardrail: never merge the target into itself.
if [ "${SOURCE_BRANCH}" = "${TARGET_BRANCH}" ]; then
  echo "[feature-checkout-simple] source and target are identical (${TARGET_BRANCH}); nothing to do"
  exit 0
fi

# Switch to the target branch. If unable, abort loudly.
git checkout "${TARGET_BRANCH}"

# Re-run guard. If the source branch is gone both locally and on the remote,
# a previous run already merged it and cleaned up; finishing quietly beats
# failing the task for work that is done.
if ! git rev-parse --verify --quiet "${SOURCE_BRANCH}" >/dev/null 2>&1 &&
   ! git rev-parse --verify --quiet "origin/${SOURCE_BRANCH}" >/dev/null 2>&1; then
  echo "[feature-checkout-simple] source branch ${SOURCE_BRANCH} no longer exists; already checked out"
  exit 0
fi

# Squash-merge with the Finding-7 invariant: ` + "`-c merge.ff=true`" + `
# overrides any user gitconfig ` + "`merge.ff=no`" + `, which otherwise conflicts
# with ` + "`--squash`" + `. This invariant is documented in the feature-checkout
# skill markdown as well; both paths MUST agree.
echo "[feature-checkout-simple] squash-merging ${SOURCE_BRANCH} into ${TARGET_BRANCH}"
git -c merge.ff=true merge --squash "${SOURCE_BRANCH}"

# Commit only if the squash actually staged something. A retry after a
# successful merge stages nothing, and ` + "`git commit`" + ` would exit 1 under
# ` + "`set -e`" + ` — failing a task whose merge had already landed.
if git diff --cached --quiet; then
  echo "[feature-checkout-simple] nothing staged; ${SOURCE_BRANCH} is already merged into ${TARGET_BRANCH}"
else
  git commit -m "feat(${FEATURE_ID}): squash merge from ${SOURCE_BRANCH}"
fi

# Publish the merge. Without this the feature only ever landed in one local
# clone: another runner, another machine, or a fresh checkout would never see
# it, and orchestration that assumes the target branch moved would build on
# work that is not there. A push failure is fatal — continuing would delete
# the source branch below and strand the commit locally.
PUSHED_TARGET=no
if git remote get-url origin >/dev/null 2>&1; then
  echo "[feature-checkout-simple] pushing ${TARGET_BRANCH} to origin"
  git push origin "${TARGET_BRANCH}"
  PUSHED_TARGET=yes
else
  echo "[feature-checkout-simple] no origin remote; leaving ${TARGET_BRANCH} local-only"
fi

# Idempotent worktree cleanup: only attempt removal if it exists.
if [ -d "${WORKTREE_PATH}" ]; then
  echo "[feature-checkout-simple] removing worktree ${WORKTREE_PATH}"
  git worktree remove --force "${WORKTREE_PATH}" || true
else
  echo "[feature-checkout-simple] no worktree at ${WORKTREE_PATH}; skipping"
fi

# Local branch deletion (idempotent — ignore if already gone). Best-effort:
# a branch still held by a worktree we could not remove must not fail a
# checkout whose merge and push already succeeded.
if git rev-parse --verify --quiet "${SOURCE_BRANCH}" >/dev/null; then
  echo "[feature-checkout-simple] deleting local branch ${SOURCE_BRANCH}"
  git branch -D "${SOURCE_BRANCH}" || echo "[feature-checkout-simple] could not delete ${SOURCE_BRANCH} (non-fatal)"
else
  echo "[feature-checkout-simple] local branch ${SOURCE_BRANCH} already gone; skipping"
fi

%s
echo "[feature-checkout-simple] done"
`

// shellSingleQuoted escapes s for use inside a single-quoted shell literal.
//
// The manual checkout path bakes real project and feature ids into the
// script, and neither is validated anywhere upstream — CheckoutFeature only
// trims whitespace. An id containing an apostrophe would otherwise close the
// literal and hand the rest of the value to bash as code, on a runner host.
func shellSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// safeBranchLiteral reduces s to the characters git actually allows in a
// branch name, so it can be embedded in the script's double-quoted
// SOURCE_BRANCH assignment without carrying `$`, a backtick or a backslash
// into a context bash would expand.
//
// This is a narrowing filter, not a validator: a name that loses characters
// here would not have resolved as a branch anyway, and the script's own
// "source branch no longer exists" guard reports that cleanly.
func safeBranchLiteral(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-' || r == '/':
			b.WriteRune(r)
		}
	}
	return b.String()
}
