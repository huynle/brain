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

	existing, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}
	for _, entry := range existing.Entries {
		if entry.GeneratedBy == BuiltInFeatureCheckoutSimpleGeneratedBy {
			return nil
		}
	}

	scriptCommand := buildSimpleFeatureCheckoutScript(cfg)

	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "automation",
		Title:       "Built-in feature checkout (simple/script)",
		Content:     "Deterministic squash-merge feature checkout. Runs a scripted git sequence — no LLM.",
		Status:      "active",
		Global:      serviceBoolPtr(true),
		Generated:   serviceBoolPtr(true),
		GeneratedBy: BuiltInFeatureCheckoutSimpleGeneratedBy,
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventFeatureCompleted,
			OncePer: "feature_id",
			Filter:  map[string]string{"checkout_mode": "simple"},
		},
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

// buildSimpleFeatureCheckoutScript returns a bash script that performs the
// deterministic squash-merge sequence. The script uses Go template
// placeholders that automation_service.renderAutomationTemplate expands at
// task dispatch time. Available placeholders:
//
//	{{.Project}}     — automation-owner project ID
//	{{.ProjectID}}   — same as .Project
//	{{.FeatureID}}   — the feature that completed
//	{{.TaskID}}      — the task whose status change triggered completion
//	{{.EventProjectID}} — source-event project (differs for cross-project)
//
// The script honors the following invariants:
//   - Finding 7: `git -c merge.ff=true merge --squash` (never bare
//     `git merge --squash`, which collides with global merge.ff=no).
//   - Idempotency for retry: worktree/branch removal treats "not found"
//     as success.
//   - Fails loudly on merge conflicts (no auto-resolution — that's the AI
//     path's job).
//   - Never deletes the merge target branch.
func buildSimpleFeatureCheckoutScript(cfg BuiltInFeatureCheckoutSimpleConfig) string {
	target := strings.TrimSpace(cfg.MergeTargetBranch)
	if target == "" {
		target = "main"
	}

	remoteBlock := "# Remote branch deletion skipped (RemoteBranchPolicy != delete).\n"
	if cfg.RemoteBranchPolicy == "delete" {
		remoteBlock = `# Remote branch deletion (RemoteBranchPolicy=delete).
# Guardrail: never delete the merge target or default branches.
if [ "${SOURCE_BRANCH}" != "${TARGET_BRANCH}" ] && [ "${SOURCE_BRANCH}" != "main" ] && [ "${SOURCE_BRANCH}" != "master" ]; then
  echo "[feature-checkout-simple] deleting remote origin/${SOURCE_BRANCH} (best-effort)"
  git push origin --delete "${SOURCE_BRANCH}" || echo "[feature-checkout-simple] remote delete failed or branch already gone (non-fatal)"
fi
`
	}

	return fmt.Sprintf(simpleFeatureCheckoutScriptTemplate, target, remoteBlock)
}

// simpleFeatureCheckoutScriptTemplate is the deterministic squash-merge
// script emitted by the simple built-in automation. It uses two fmt verbs:
//  1. target branch (a literal, embedded at ensure time)
//  2. remote deletion block (also a literal, embedded at ensure time)
//
// It also uses Go/Brain template placeholders like {{.FeatureID}} that
// automation_service.renderAutomationTemplate expands at task dispatch time.
const simpleFeatureCheckoutScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

# Built-in feature checkout (simple/script) — Phase 3.3.
# Deterministic squash-merge for feature {{.FeatureID}} in project {{.ProjectID}}.

FEATURE_ID='{{.FeatureID}}'
PROJECT_ID='{{.ProjectID}}'
TARGET_BRANCH='%s'
SOURCE_BRANCH="${FEATURE_ID}"  # runner convention: branch defaults to feature_id
WORKTREE_PATH=".worktrees/${SOURCE_BRANCH}"

echo "[feature-checkout-simple] project=${PROJECT_ID} feature=${FEATURE_ID}"
echo "[feature-checkout-simple] source=${SOURCE_BRANCH} target=${TARGET_BRANCH}"

# Guardrail: never merge the target into itself.
if [ "${SOURCE_BRANCH}" = "${TARGET_BRANCH}" ]; then
  echo "[feature-checkout-simple] source and target are identical (${TARGET_BRANCH}); nothing to do"
  exit 0
fi

# Switch to the target branch. If unable, abort loudly.
git checkout "${TARGET_BRANCH}"

# Squash-merge with the Finding-7 invariant: ` + "`-c merge.ff=true`" + `
# overrides any user gitconfig ` + "`merge.ff=no`" + `, which otherwise conflicts
# with ` + "`--squash`" + `. This invariant is documented in the feature-checkout
# skill markdown as well; both paths MUST agree.
echo "[feature-checkout-simple] squash-merging ${SOURCE_BRANCH} into ${TARGET_BRANCH}"
git -c merge.ff=true merge --squash "${SOURCE_BRANCH}"
git commit -m "feat(${FEATURE_ID}): squash merge from ${SOURCE_BRANCH}"

# Idempotent worktree cleanup: only attempt removal if it exists.
if [ -d "${WORKTREE_PATH}" ]; then
  echo "[feature-checkout-simple] removing worktree ${WORKTREE_PATH}"
  git worktree remove --force "${WORKTREE_PATH}" || true
else
  echo "[feature-checkout-simple] no worktree at ${WORKTREE_PATH}; skipping"
fi

# Local branch deletion (idempotent — ignore if already gone).
if git rev-parse --verify --quiet "${SOURCE_BRANCH}" >/dev/null; then
  echo "[feature-checkout-simple] deleting local branch ${SOURCE_BRANCH}"
  git branch -D "${SOURCE_BRANCH}"
else
  echo "[feature-checkout-simple] local branch ${SOURCE_BRANCH} already gone; skipping"
fi

%s
echo "[feature-checkout-simple] done"
`
