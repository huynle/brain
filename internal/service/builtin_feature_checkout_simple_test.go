package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// -----------------------------------------------------------------------------
// EnsureBuiltInFeatureCheckoutSimpleAutomation (Phase 3.3)
// -----------------------------------------------------------------------------

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_CreatesEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
	})
	if err != nil {
		t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Status: "active", Limit: 100})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}

	// Find our entry
	var simple *types.BrainEntry
	for i := range resp.Entries {
		if resp.Entries[i].GeneratedBy == BuiltInFeatureCheckoutSimpleGeneratedBy {
			simple = &resp.Entries[i]
			break
		}
	}
	if simple == nil {
		t.Fatalf("expected simple built-in automation, not found; entries=%d", len(resp.Entries))
	}

	// Trigger shape
	if simple.Trigger == nil {
		t.Fatalf("simple automation missing trigger")
	}
	if simple.Trigger.Type != "event" {
		t.Errorf("trigger type = %q, want %q", simple.Trigger.Type, "event")
	}
	if simple.Trigger.Event != types.EventFeatureCompleted {
		t.Errorf("trigger event = %q, want %q", simple.Trigger.Event, types.EventFeatureCompleted)
	}
	if simple.Trigger.OncePer != "feature_id" {
		t.Errorf("trigger once_per = %q, want %q", simple.Trigger.OncePer, "feature_id")
	}
	if simple.Trigger.Filter["checkout_mode"] != "simple" {
		t.Errorf("trigger filter checkout_mode = %q, want %q", simple.Trigger.Filter["checkout_mode"], "simple")
	}

	// Action shape
	if simple.Action == nil {
		t.Fatalf("simple automation missing action")
	}
	if simple.Action.Type != types.AutomationActionScript {
		t.Errorf("action type = %q, want %q", simple.Action.Type, types.AutomationActionScript)
	}
	if simple.Action.Command == "" {
		t.Fatalf("action command empty")
	}

	// Required script substrings (invariants)
	required := []string{
		"git -c merge.ff=true merge --squash",
		"git worktree remove",
		"git branch -D",
		"{{.FeatureID}}",
	}
	for _, needle := range required {
		if !strings.Contains(simple.Action.Command, needle) {
			t.Errorf("script command missing invariant substring %q", needle)
		}
	}
}

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_Idempotent(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	cfg := BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
	}
	for i := 0; i < 3; i++ {
		if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, cfg); err != nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 100})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	count := 0
	for _, e := range resp.Entries {
		if e.GeneratedBy == BuiltInFeatureCheckoutSimpleGeneratedBy {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 simple built-in automation, got %d", count)
	}
}

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_DisabledDoesNothing(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{Enabled: false}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation (disabled) failed: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected no automations when disabled, got %d", len(resp.Entries))
	}
}

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_UsesConfiguredMergeTarget(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "develop",
	}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	entry := resp.Entries[0]
	if !strings.Contains(entry.Action.Command, "develop") {
		t.Errorf("expected script to reference configured merge target 'develop', got: %s", entry.Action.Command)
	}
}

// -----------------------------------------------------------------------------
// End-to-end dispatch routing (Phase 3.5 integration test)
// -----------------------------------------------------------------------------

// TestBuiltinFeatureCheckoutDispatch_SimpleEventOnlyMatchesSimpleAutomation
// verifies that with both automations registered, a feature.completed event
// carrying checkout_mode=simple in metadata generates exactly one script
// task, coming from the simple automation.
func TestBuiltinFeatureCheckoutDispatch_SimpleEventOnlyMatchesSimpleAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Register both built-ins.
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
		ExecutionMode:     "worktree",
	}); err != nil {
		t.Fatalf("ensure AI automation: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
	}); err != nil {
		t.Fatalf("ensure simple automation: %v", err)
	}

	// Deliberately NO per-project copies: the server registers these two
	// built-ins once, globally, and nothing materializes per-project
	// versions. Copying them here is what previously masked the fact that
	// the real global entries matched no event at all.

	automationSvc := NewAutomationService(brain)
	err := automationSvc.HandleEvent(ctx, types.Event{
		ID:        "evt-simple",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "brain",
		FeatureID: "feat-x",
		Metadata:  map[string]string{"checkout_mode": "simple"},
	})
	if err != nil {
		t.Fatalf("HandleEvent (simple): %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("expected exactly 1 generated task (simple only), got %d", len(tasks.Entries))
	}
	task := tasks.Entries[0]
	if task.Executor != "script" {
		t.Errorf("generated task executor = %q, want %q (must be script)", task.Executor, "script")
	}
	if !strings.Contains(task.Content, "git -c merge.ff=true merge --squash") {
		t.Errorf("generated task content missing invariant substring; got: %s", task.Content)
	}
	if !strings.Contains(task.Content, "feat-x") {
		t.Errorf("generated task content did not template FeatureID; got: %s", task.Content)
	}
}

// TestBuiltinFeatureCheckoutDispatch_AIEventOnlyMatchesAIAutomation
// verifies the AI automation fires (not the simple one) for AI events.
func TestBuiltinFeatureCheckoutDispatch_AIEventOnlyMatchesAIAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
		ExecutionMode:     "worktree",
	}); err != nil {
		t.Fatalf("ensure AI automation: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
	}); err != nil {
		t.Fatalf("ensure simple automation: %v", err)
	}

	// No per-project copies — see the note in the simple-dispatch test above.

	automationSvc := NewAutomationService(brain)
	err := automationSvc.HandleEvent(ctx, types.Event{
		ID:        "evt-ai",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "brain",
		FeatureID: "feat-y",
		Metadata:  map[string]string{"checkout_mode": "ai"},
	})
	if err != nil {
		t.Fatalf("HandleEvent (ai): %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("expected exactly 1 generated task (AI only), got %d", len(tasks.Entries))
	}
	task := tasks.Entries[0]
	if task.Executor == "script" {
		t.Errorf("AI event should not create a script task, got executor=%q", task.Executor)
	}
	if !strings.Contains(task.Content, "feature-checkout skill") {
		t.Errorf("AI task content missing AI-skill prompt; got: %s", task.Content)
	}
}

// TestBuiltinFeatureCheckoutDispatch_MissingCheckoutModeMatchesAI verifies
// the belt-and-suspenders normalizer: events without checkout_mode in
// metadata still match the AI automation (backward-compat).
func TestBuiltinFeatureCheckoutDispatch_MissingCheckoutModeMatchesAI(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
		ExecutionMode:     "worktree",
	}); err != nil {
		t.Fatalf("ensure AI automation: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
	}); err != nil {
		t.Fatalf("ensure simple automation: %v", err)
	}

	// No per-project copies — see the note in the simple-dispatch test above.

	automationSvc := NewAutomationService(brain)
	// Event with NO checkout_mode metadata (simulates a raw event from a
	// pre-Phase-3 source or a code path that bypasses CheckFeatureCompletion).
	err := automationSvc.HandleEvent(ctx, types.Event{
		ID:        "evt-nometa",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "brain",
		FeatureID: "feat-z",
	})
	if err != nil {
		t.Fatalf("HandleEvent (no metadata): %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("expected exactly 1 generated task (AI only, via defaulting), got %d", len(tasks.Entries))
	}
	if tasks.Entries[0].Executor == "script" {
		t.Errorf("missing checkout_mode should default to AI (not script), got executor=%q", tasks.Entries[0].Executor)
	}
}

// ─── Publish-before-delete ─────────────────────────────────────────
//
// Deleting the remote source branch while the merge exists only locally
// destroys the only shared copy of the work: the remote loses the feature
// branch and never gains the commit. Verified live against a real GitHub
// repo before the fix — remote main stayed at the initial commit while the
// feature branch was deleted.

func simpleScript(t *testing.T, cfg BuiltInFeatureCheckoutSimpleConfig) string {
	t.Helper()
	return buildSimpleFeatureCheckoutScript(cfg)
}

func TestSimpleCheckoutScript_PushesTargetBranch(t *testing.T) {
	script := simpleScript(t, BuiltInFeatureCheckoutSimpleConfig{MergeTargetBranch: "main"})
	if !strings.Contains(script, `git push origin "${TARGET_BRANCH}"`) {
		t.Errorf("script never pushes the target branch:\n%s", script)
	}
}

// The push must happen after the merge commit; pushing first would publish
// nothing.
func TestSimpleCheckoutScript_PushesAfterCommit(t *testing.T) {
	script := simpleScript(t, BuiltInFeatureCheckoutSimpleConfig{MergeTargetBranch: "main"})
	commitAt := strings.Index(script, "git commit -m")
	pushAt := strings.Index(script, `git push origin "${TARGET_BRANCH}"`)
	if commitAt < 0 || pushAt < 0 {
		t.Fatal("script missing commit or push")
	}
	if pushAt < commitAt {
		t.Error("push happens before the merge commit")
	}
}

// A repo with no remote must still work — the local-only case is valid.
func TestSimpleCheckoutScript_TolerantOfMissingRemote(t *testing.T) {
	script := simpleScript(t, BuiltInFeatureCheckoutSimpleConfig{MergeTargetBranch: "main"})
	if !strings.Contains(script, "git remote get-url origin") {
		t.Errorf("script does not check for a remote before pushing:\n%s", script)
	}
	if !strings.Contains(script, "leaving ${TARGET_BRANCH} local-only") {
		t.Errorf("script has no local-only branch:\n%s", script)
	}
}

// The whole point: remote deletion is gated on a successful push.
func TestSimpleCheckoutScript_RemoteDeleteGatedOnPush(t *testing.T) {
	script := simpleScript(t, BuiltInFeatureCheckoutSimpleConfig{
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
	})
	if !strings.Contains(script, `if [ "${PUSHED_TARGET}" != "yes" ]`) {
		t.Errorf("remote deletion is not gated on a successful push:\n%s", script)
	}
	gateAt := strings.Index(script, `PUSHED_TARGET`)
	deleteAt := strings.Index(script, "git push origin --delete")
	if gateAt < 0 || deleteAt < 0 {
		t.Fatal("script missing gate or delete")
	}
	if deleteAt < gateAt {
		t.Error("remote delete appears before the push gate is established")
	}
}

// With no deletion policy there is nothing to gate, but the push must
// still happen.
func TestSimpleCheckoutScript_KeepPolicyStillPushes(t *testing.T) {
	script := simpleScript(t, BuiltInFeatureCheckoutSimpleConfig{
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "keep",
	})
	if !strings.Contains(script, `git push origin "${TARGET_BRANCH}"`) {
		t.Error("keep policy dropped the target push")
	}
	if strings.Contains(script, "git push origin --delete") {
		t.Error("keep policy still emits a remote delete")
	}
}

// The existing guardrails must survive the restructure.
func TestSimpleCheckoutScript_KeepsExistingGuardrails(t *testing.T) {
	script := simpleScript(t, BuiltInFeatureCheckoutSimpleConfig{
		MergeTargetBranch:  "release",
		RemoteBranchPolicy: "delete",
	})
	// Finding-7 invariant: survives a user gitconfig of merge.ff=no.
	if !strings.Contains(script, "git -c merge.ff=true merge --squash") {
		t.Error("lost the merge.ff=true invariant")
	}
	// Never delete the target or a default branch.
	if !strings.Contains(script, `[ "${SOURCE_BRANCH}" != "${TARGET_BRANCH}" ]`) {
		t.Error("lost the target-branch deletion guardrail")
	}
	if !strings.Contains(script, `[ "${SOURCE_BRANCH}" != "main" ]`) {
		t.Error("lost the main-branch deletion guardrail")
	}
	if !strings.Contains(script, "set -euo pipefail") {
		t.Error("lost fail-fast; a conflict would no longer stop the script")
	}
}

// ─── Action migration ──────────────────────────────────────────────
//
// The script is generated wholly from config and code, never authored by
// the user. An entry written by an older build keeps running that older
// script forever unless Ensure rewrites it — which is exactly how the
// missing `git push` survived a code fix.

func TestEnsureBuiltInFeatureCheckoutSimple_MigratesStaleScript(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// An entry carrying a script from an older build.
	global := true
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Built-in feature checkout (simple/script)",
		Content: "legacy", Status: "active", Global: &global,
		GeneratedBy: BuiltInFeatureCheckoutSimpleGeneratedBy,
		Trigger: &types.TriggerConfig{
			Type: "event", Event: types.EventFeatureCompleted,
			OncePer: "feature_id",
			Filter:  builtInCheckoutFilter("simple"),
		},
		Action: &types.AutomationAction{
			Type:    types.AutomationActionScript,
			Command: "#!/usr/bin/env bash\necho old script with no push\n",
		},
	}); err != nil {
		t.Fatalf("save legacy: %v", err)
	}

	// Restart-equivalent.
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, e := range resp.Entries {
		if e.GeneratedBy != BuiltInFeatureCheckoutSimpleGeneratedBy {
			continue
		}
		found = true
		if e.Action == nil {
			t.Fatal("action missing after migration")
		}
		if !strings.Contains(e.Action.Command, `git push origin "${TARGET_BRANCH}"`) {
			t.Errorf("stale script survived the restart; it will never publish a merge:\n%s", e.Action.Command)
		}
		if strings.Contains(e.Action.Command, "old script with no push") {
			t.Error("legacy script was not replaced")
		}
	}
	if !found {
		t.Fatal("built-in entry disappeared")
	}
}

// Migration must not duplicate the entry.
func TestEnsureBuiltInFeatureCheckoutSimple_MigrationDoesNotDuplicate(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	cfg := BuiltInFeatureCheckoutSimpleConfig{
		Enabled: true, MergeTargetBranch: "main", RemoteBranchPolicy: "delete",
	}
	for i := 0; i < 3; i++ {
		if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, cfg); err != nil {
			t.Fatalf("ensure #%d: %v", i, err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	count := 0
	for _, e := range resp.Entries {
		if e.GeneratedBy == BuiltInFeatureCheckoutSimpleGeneratedBy {
			count++
		}
	}
	if count != 1 {
		t.Errorf("simple built-ins = %d, want 1", count)
	}
}

// Changing the configured merge target must reach the stored script, or the
// automation keeps merging into the old branch.
func TestEnsureBuiltInFeatureCheckoutSimple_MigratesOnTargetBranchChange(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled: true, MergeTargetBranch: "main",
	}); err != nil {
		t.Fatalf("ensure v1: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled: true, MergeTargetBranch: "release/v2",
	}); err != nil {
		t.Fatalf("ensure v2: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, e := range resp.Entries {
		if e.GeneratedBy != BuiltInFeatureCheckoutSimpleGeneratedBy {
			continue
		}
		if !strings.Contains(e.Action.Command, "release/v2") {
			t.Errorf("stored script still targets the old branch:\n%s", e.Action.Command)
		}
	}
}
