package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// -----------------------------------------------------------------------------
// builtInDeliveryFilter (Phase 3)
// -----------------------------------------------------------------------------

func TestBuiltInDeliveryFilter(t *testing.T) {
	f := builtInDeliveryFilter()
	if f == nil {
		t.Fatal("builtInDeliveryFilter returned nil")
	}
	if f["delivery_mode"] != "in:mr,local_merge" {
		t.Errorf("filter delivery_mode = %q, want %q", f["delivery_mode"], "in:mr,local_merge")
	}
	if f["project"] != "*" {
		t.Errorf("filter project = %q, want %q", f["project"], "*")
	}
	if len(f) != 2 {
		t.Errorf("filter has %d keys, want 2", len(f))
	}
}

// -----------------------------------------------------------------------------
// triggerNeedsDeliveryMigration truth table (Phase 3)
// -----------------------------------------------------------------------------

func TestTriggerNeedsDeliveryMigration(t *testing.T) {
	tests := []struct {
		name    string
		trigger *types.TriggerConfig
		want    bool
	}{
		{
			name:    "nil trigger",
			trigger: nil,
			want:    true,
		},
		{
			name:    "nil filter",
			trigger: &types.TriggerConfig{Type: "event", Event: types.EventFeatureCompleted},
			want:    true,
		},
		{
			name: "wrong delivery_mode",
			trigger: &types.TriggerConfig{Filter: map[string]string{
				"delivery_mode": "none",
				"project":       "*",
			}},
			want: true,
		},
		{
			name: "missing project wildcard",
			trigger: &types.TriggerConfig{Filter: map[string]string{
				"delivery_mode": "in:mr,local_merge",
			}},
			want: true,
		},
		{
			name: "correct shape",
			trigger: &types.TriggerConfig{Filter: map[string]string{
				"delivery_mode": "in:mr,local_merge",
				"project":       "*",
			}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := triggerNeedsDeliveryMigration(tt.trigger)
			if got != tt.want {
				t.Errorf("triggerNeedsDeliveryMigration(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// buildFeatureDeliveryScript / renderFeatureDeliveryScript invariants (Phase 3)
// -----------------------------------------------------------------------------

func TestBuildFeatureDeliveryScript_Invariants(t *testing.T) {
	script := buildFeatureDeliveryScript(BuiltInFeatureDeliveryConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
	})

	required := []string{
		"set -euo pipefail",
		"git -c merge.ff=true merge --squash",
		"git push -u origin",
		"glab mr create",
		"mr)",
		"local_merge)",
		"{{.FeatureID}}",
		"{{.ProjectID}}",
		"{{.DeliveryMode}}",
		// protected-branch refusal (local_merge guard)
		"is protected",
		"local_merge refused",
		// github not implemented
		"GitHub delivery not yet implemented",
		// phase 4: merge_request write-back (MR URL + status lifecycle)
		"brain_patch_merge_request",
		"/metadata",
		"mr_url",
		`brain_patch_merge_request "${MR_URL}" "mr_open"`,
		`brain_patch_merge_request "" "merged"`,
		"BRAIN_API_URL",
		"BRAIN_API_TOKEN",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Errorf("delivery script missing invariant substring %q", needle)
		}
	}

	// The phase-4 write-back replaced the phase-3 placeholder; the marker must
	// be gone so a future reader does not think the write-back is still a TODO.
	if strings.Contains(script, "TODO(phase4)") {
		t.Error("delivery script still contains the phase-3 TODO(phase4) marker; the write-back should have replaced it")
	}
	// fmt.Sprintf must not have left any unfilled/mis-escaped verbs in the
	// rendered script (a stray %s would surface as %!s(MISSING) etc.).
	if strings.Contains(script, "%!") {
		t.Errorf("delivery script contains a malformed fmt verb (%%! sequence); check %%%% escaping in the template")
	}
}

func TestBuildFeatureDeliveryScript_DefaultTargetBranch(t *testing.T) {
	script := buildFeatureDeliveryScript(BuiltInFeatureDeliveryConfig{
		Enabled: true,
		// no MergeTargetBranch -> should default to main
	})
	if !strings.Contains(script, "TARGET_BRANCH='main'") {
		t.Errorf("expected default TARGET_BRANCH='main' in script")
	}
}

// -----------------------------------------------------------------------------
// EnsureBuiltInFeatureDeliveryAutomation (Phase 3)
// -----------------------------------------------------------------------------

func TestEnsureBuiltInFeatureDeliveryAutomation_CreatesEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
	})
	if err != nil {
		t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Status: "active", Limit: 100})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}

	var found *types.BrainEntry
	for i := range resp.Entries {
		if resp.Entries[i].GeneratedBy == BuiltInFeatureDeliveryGeneratedBy {
			found = &resp.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected delivery built-in automation, not found; entries=%d", len(resp.Entries))
	}

	if found.Trigger == nil {
		t.Fatalf("delivery automation missing trigger")
	}
	if found.Trigger.Type != "event" {
		t.Errorf("trigger type = %q, want %q", found.Trigger.Type, "event")
	}
	if found.Trigger.Event != types.EventFeatureCompleted {
		t.Errorf("trigger event = %q, want %q", found.Trigger.Event, types.EventFeatureCompleted)
	}
	if found.Trigger.OncePer != "feature_id" {
		t.Errorf("trigger once_per = %q, want %q", found.Trigger.OncePer, "feature_id")
	}
	if found.Trigger.Filter["delivery_mode"] != "in:mr,local_merge" {
		t.Errorf("trigger filter delivery_mode = %q, want %q", found.Trigger.Filter["delivery_mode"], "in:mr,local_merge")
	}
	if found.Trigger.Filter["project"] != "*" {
		t.Errorf("trigger filter project = %q, want %q", found.Trigger.Filter["project"], "*")
	}

	if found.Action == nil {
		t.Fatalf("delivery automation missing action")
	}
	if found.Action.Type != types.AutomationActionScript {
		t.Errorf("action type = %q, want %q", found.Action.Type, types.AutomationActionScript)
	}
	if found.Action.Command == "" {
		t.Fatalf("action command empty")
	}
	if found.Action.ExecutionMode != "current_branch" {
		t.Errorf("action execution_mode = %q, want %q", found.Action.ExecutionMode, "current_branch")
	}
	if found.Action.TargetWorkdir != "/repo/brain" {
		t.Errorf("action target_workdir = %q, want %q", found.Action.TargetWorkdir, "/repo/brain")
	}
}

func TestEnsureBuiltInFeatureDeliveryAutomation_Idempotent(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	cfg := BuiltInFeatureDeliveryConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
	}
	for i := 0; i < 3; i++ {
		if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, cfg); err != nil {
			t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation (call %d) failed: %v", i, err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	count := 0
	for i := range resp.Entries {
		if resp.Entries[i].GeneratedBy == BuiltInFeatureDeliveryGeneratedBy {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 delivery automation after 3 calls, got %d", count)
	}
}

func TestEnsureBuiltInFeatureDeliveryAutomation_DisabledDoesNothing(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{Enabled: false}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation (disabled) failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	for i := range resp.Entries {
		if resp.Entries[i].GeneratedBy == BuiltInFeatureDeliveryGeneratedBy {
			t.Fatalf("disabled config should not create a delivery automation")
		}
	}
}

// -----------------------------------------------------------------------------
// Mode-branch semantics (dn9gs12t): assert per-mode behavior, not just global
// substring presence. The rendered script contains BOTH modes behind a runtime
// `case "${DELIVERY_MODE}"` switch, so these tests slice out each branch and
// assert what must (and must NOT) appear inside it.
// -----------------------------------------------------------------------------

// sliceDeliveryBranch returns the body of one `case` arm of the delivery
// script's `case "${DELIVERY_MODE}" in ... esac`. label is "mr)" or
// "local_merge)".
//
// The outer case arms are indented two spaces (`\n  mr)`), and their
// terminating `;;` is indented four (`\n    ;;`) — distinct from the EIGHT-space
// `;;` of the nested `case "${PROVIDER}" in` switch inside the mr arm. Anchoring
// on those exact indentations is what keeps the mr-arm slice from stopping early
// at the first nested provider `;;` and losing the write-back at the arm's end.
func sliceDeliveryBranch(t *testing.T, script, label string) string {
	t.Helper()
	anchor := "\n  " + label
	start := strings.Index(script, anchor)
	if start < 0 {
		t.Fatalf("delivery script has no %q case arm (anchor %q)", label, anchor)
	}
	rest := script[start+len(anchor):]
	end := strings.Index(rest, "\n    ;;")
	if end < 0 {
		t.Fatalf("delivery script %q arm has no terminating \\n    ;;", label)
	}
	return rest[:end]
}

func deliveryScriptForTest() string {
	return buildFeatureDeliveryScript(BuiltInFeatureDeliveryConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
	})
}

// TestDeliveryScript_MRModePushesAndOpensMRButNeverMerges asserts the MR branch
// pushes the source branch and calls the provider CLI to open an MR, but does
// NOT run any local merge and does NOT delete the source branch (review needs
// it). This is the task's "MR mode ... does NOT merge" requirement, scoped to
// the mr branch only so a merge command hiding in local_merge can't mask a
// regression here.
func TestDeliveryScript_MRModePushesAndOpensMRButNeverMerges(t *testing.T) {
	mr := sliceDeliveryBranch(t, deliveryScriptForTest(), "mr)")

	if !strings.Contains(mr, "git push -u origin") {
		t.Error("mr branch must push the source branch (git push -u origin)")
	}
	if !strings.Contains(mr, "glab mr create") {
		t.Error("mr branch must open a merge request via glab")
	}
	if !strings.Contains(mr, "--target-branch") || !strings.Contains(mr, "--squash-before-merge") {
		t.Error("mr branch must pass source->target and --squash-before-merge to glab mr create")
	}
	// Must NOT merge in mr mode.
	if strings.Contains(mr, "merge --squash") {
		t.Error("mr branch must NOT run a local squash-merge; mr mode opens an MR instead")
	}
	// Must NOT delete the source branch in mr mode (review needs it).
	if strings.Contains(mr, "push origin --delete") {
		t.Error("mr branch must NOT delete the source branch; review needs it")
	}
	// MR-URL write-back must be wired on the mr path.
	if !strings.Contains(mr, `brain_patch_merge_request "${MR_URL}" "mr_open"`) {
		t.Error("mr branch must write the captured MR URL back with status mr_open")
	}
}

// TestDeliveryScript_MRModeProceedsIntoProtectedTarget is the asymmetric half of
// the protected-branch guard: mr mode has NO protected-branch check, because
// opening an MR against a protected branch is exactly the right move. The guard
// text must live ONLY in the local_merge branch.
func TestDeliveryScript_MRModeProceedsIntoProtectedTarget(t *testing.T) {
	mr := sliceDeliveryBranch(t, deliveryScriptForTest(), "mr)")
	if strings.Contains(mr, "is protected") || strings.Contains(mr, "local_merge refused") {
		t.Error("mr branch must NOT contain a protected-branch guard; mr into a protected target is allowed")
	}
}

// TestDeliveryScript_LocalMergeUsesFindingSevenInvariant asserts the local_merge
// branch squash-merges with the -c merge.ff=true invariant so it survives a
// user's global gitconfig merge.ff=no, commits, and pushes the target.
func TestDeliveryScript_LocalMergeUsesFindingSevenInvariant(t *testing.T) {
	lm := sliceDeliveryBranch(t, deliveryScriptForTest(), "local_merge)")

	if !strings.Contains(lm, "git -c merge.ff=true merge --squash") {
		t.Error("local_merge must use git -c merge.ff=true merge --squash (Finding-7 invariant)")
	}
	if !strings.Contains(lm, "git commit -m") {
		t.Error("local_merge must commit the squashed result")
	}
	if !strings.Contains(lm, `git push origin "${TARGET_BRANCH}"`) {
		t.Error("local_merge must push the target branch")
	}
}

// TestDeliveryScript_LocalMergeIsIdempotent asserts the re-run guards that make
// a second run a no-op instead of an error: when the source branch is gone both
// locally and remotely a prior run already delivered, and a merge that stages
// nothing is reported as already-merged rather than committed again.
func TestDeliveryScript_LocalMergeIsIdempotent(t *testing.T) {
	lm := sliceDeliveryBranch(t, deliveryScriptForTest(), "local_merge)")

	if !strings.Contains(lm, "already delivered") {
		t.Error("local_merge must treat an already-gone source branch as already delivered (idempotent no-op)")
	}
	if !strings.Contains(lm, "git diff --cached --quiet") {
		t.Error("local_merge must detect a no-op squash (nothing staged) instead of committing empty")
	}
	if !strings.Contains(lm, "already merged into") {
		t.Error("local_merge must report an already-merged source as a no-op, not an error")
	}
}

// TestDeliveryScript_LocalMergeRefusesProtectedTarget asserts the protected-
// branch guard lives in the local_merge branch and refuses with an actionable
// message (points the user at delivery_mode: mr).
func TestDeliveryScript_LocalMergeRefusesProtectedTarget(t *testing.T) {
	lm := sliceDeliveryBranch(t, deliveryScriptForTest(), "local_merge)")

	if !strings.Contains(lm, "is protected") {
		t.Error("local_merge must check whether the target branch is protected")
	}
	if !strings.Contains(lm, "local_merge refused") {
		t.Error("local_merge must refuse a protected target")
	}
	if !strings.Contains(lm, "delivery_mode: mr") {
		t.Error("local_merge refusal must be actionable — point the user at delivery_mode: mr")
	}
}

// TestDeliveryScript_LocalMergeGuardIsFailClosed asserts the protected-branch
// guard REFUSES when it cannot positively determine that the target is
// unprotected (glab error / missing glab / non-gitlab remote). The design
// invariant is "never direct-push a protected target", so an indeterminate
// check must fail closed rather than proceed "best-effort". This is a
// regression guard for the fail-open bug found during live verification
// against orion/ai/canis (task akzdsp8n): a glab api error let local_merge
// proceed toward a protected branch.
func TestDeliveryScript_LocalMergeGuardIsFailClosed(t *testing.T) {
	lm := sliceDeliveryBranch(t, deliveryScriptForTest(), "local_merge)")

	// The old fail-open phrasing must be gone.
	if strings.Contains(lm, "proceeding best-effort") {
		t.Error("local_merge guard must NOT proceed best-effort when protection cannot be verified (fail-open regression)")
	}
	// The fail-closed refusal must be present and capture the glab exit code
	// so an error (not just a positive 'protected') triggers refusal.
	if !strings.Contains(lm, "fail-closed") {
		t.Error("local_merge guard must refuse fail-closed when protection is indeterminate")
	}
	if !strings.Contains(lm, "PB_RC") {
		t.Error("local_merge guard must capture the glab api exit status to distinguish protected/not-protected/error")
	}
	// A 404 (branch genuinely not protected) is the ONLY non-refusing gitlab
	// outcome; assert that path exists so we don't accidentally refuse every
	// unprotected target.
	if !strings.Contains(lm, "404") {
		t.Error("local_merge guard must treat a 404 as 'not protected' and proceed")
	}
}

// TestDeliveryScript_LocalMergeRemoteDeleteHonorsPolicy asserts RemoteBranchPolicy
// wiring: delete emits a guarded remote delete, keep does not.
func TestDeliveryScript_LocalMergeRemoteDeleteHonorsPolicy(t *testing.T) {
	del := sliceDeliveryBranch(t, buildFeatureDeliveryScript(BuiltInFeatureDeliveryConfig{
		Enabled: true, MergeTargetBranch: "main", RemoteBranchPolicy: "delete",
	}), "local_merge)")
	if !strings.Contains(del, "git push origin --delete") {
		t.Error("RemoteBranchPolicy=delete must emit a remote source-branch delete in local_merge")
	}

	keep := sliceDeliveryBranch(t, buildFeatureDeliveryScript(BuiltInFeatureDeliveryConfig{
		Enabled: true, MergeTargetBranch: "main", RemoteBranchPolicy: "keep",
	}), "local_merge)")
	if strings.Contains(keep, "git push origin --delete") {
		t.Error("RemoteBranchPolicy=keep must NOT delete the remote source branch")
	}
}

// TestDeliveryScript_GitHubIsAStubNotALiveCall enforces the hard GitLab-only
// constraint at the script level: the github provider arm exits with a clear
// "not yet implemented" message and never shells out to gh or github.com.
func TestDeliveryScript_GitHubIsAStubNotALiveCall(t *testing.T) {
	script := deliveryScriptForTest()
	if !strings.Contains(script, "GitHub delivery not yet implemented") {
		t.Error("github provider must be a clear stub, not a live path")
	}
	// The script must never invoke the gh CLI nor hardcode a github.com URL.
	if strings.Contains(script, "gh pr create") || strings.Contains(script, "gh mr") {
		t.Error("delivery script must not shell out to the gh CLI (GitLab-only constraint)")
	}
	if strings.Contains(script, "github.com/huynle/brain") {
		t.Error("delivery script must not hardcode the github.com brain remote (GitLab-only constraint)")
	}
}

// -----------------------------------------------------------------------------
// Opt-in gating (dn9gs12t): the KEY end-to-end guarantee. A feature.completed
// event only triggers a git-delivery task when its folded delivery_mode opted
// in (mr | local_merge). A NON-opted-in feature (delivery_mode none/missing)
// must NOT generate any delivery task — nothing is ever pushed or merged for a
// feature that did not ask for it.
//
// These drive the real matcher + generator (AutomationService.HandleEvent)
// against the real built-in automation registered by
// EnsureBuiltInFeatureDeliveryAutomation, so they cover the trigger filter
// delivery_mode:"in:mr,local_merge" wiring, not just the folder in isolation.
// GitLab-only constraint: no git is executed here at all — HandleEvent only
// GENERATES the task entry; the script never runs, so no remote is contacted.
// -----------------------------------------------------------------------------

// deliveryTaskCount counts automation-generated tasks in a project. In these
// isolated tests the built-in delivery automation is the ONLY automation
// registered, so an automation-generated task (GeneratedBy prefixed
// "automation:") is necessarily its delivery task. It is counted this way
// rather than by BuiltInFeatureDeliveryGeneratedBy because the generated TASK
// carries the automation's id as its GeneratedBy, not the automation's own
// code-owner marker.
func deliveryTaskCount(t *testing.T, brain *BrainServiceImpl, project string) int {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	n := 0
	for _, e := range resp.Entries {
		if strings.HasPrefix(e.GeneratedBy, "automation:") {
			n++
		}
	}
	return n
}

func TestFeatureDeliveryAutomation_OptedInModeGeneratesTask(t *testing.T) {
	for _, mode := range []string{"mr", "local_merge"} {
		t.Run(mode, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()

			if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{
				Enabled:           true,
				MergeTargetBranch: "main",
			}); err != nil {
				t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation failed: %v", err)
			}

			automation := NewAutomationService(brain)
			// A folded feature.completed event carrying an opted-in delivery_mode,
			// exactly as CheckFeatureCompletion stamps it.
			if err := automation.HandleEvent(ctx, types.Event{
				ID:        "evt-delivery-" + mode,
				Type:      types.EventFeatureCompleted,
				Source:    types.EventSourceAPI,
				ProjectID: "delivery-optin",
				FeatureID: "feat-ship",
				Metadata:  map[string]string{"delivery_mode": mode},
			}); err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			if got := deliveryTaskCount(t, brain, "delivery-optin"); got != 1 {
				t.Fatalf("delivery_mode=%q: expected exactly 1 generated delivery task, got %d", mode, got)
			}
		})
	}
}

func TestFeatureDeliveryAutomation_NonOptedInDoesNotGenerateTask(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
	}{
		{name: "delivery_mode none", metadata: map[string]string{"delivery_mode": "none"}},
		{name: "delivery_mode missing", metadata: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()

			if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{
				Enabled:           true,
				MergeTargetBranch: "main",
			}); err != nil {
				t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation failed: %v", err)
			}

			automation := NewAutomationService(brain)
			if err := automation.HandleEvent(ctx, types.Event{
				ID:        "evt-delivery-noopt",
				Type:      types.EventFeatureCompleted,
				Source:    types.EventSourceAPI,
				ProjectID: "delivery-noopt",
				FeatureID: "feat-quiet",
				Metadata:  tt.metadata,
			}); err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			if got := deliveryTaskCount(t, brain, "delivery-noopt"); got != 0 {
				t.Fatalf("%s: expected NO delivery task (feature did not opt in), got %d", tt.name, got)
			}
		})
	}
}

// TestFeatureDeliveryAutomation_DisabledConfigNeverDelivers is the config-level
// off switch (BRAIN_FEATURE_DELIVERY_ENABLED / cfg.FeatureDelivery.Enabled): with
// the automation disabled, even an explicitly opted-in feature.completed event
// generates nothing, because no automation was ever registered.
func TestFeatureDeliveryAutomation_DisabledConfigNeverDelivers(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{
		Enabled: false, // master switch OFF
	}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation (disabled) failed: %v", err)
	}

	automation := NewAutomationService(brain)
	if err := automation.HandleEvent(ctx, types.Event{
		ID:        "evt-delivery-disabled",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "delivery-off",
		FeatureID: "feat-ship",
		Metadata:  map[string]string{"delivery_mode": "mr"},
	}); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if got := deliveryTaskCount(t, brain, "delivery-off"); got != 0 {
		t.Fatalf("disabled delivery automation must generate no task, got %d", got)
	}
}

// deliveryTaskContent returns the rendered script (task Content) of the single
// generated delivery task for a project, failing if there is not exactly one.
// Unlike deliveryTaskCount it returns the body, so a test can assert what the
// automation actually rendered — the field→event-metadata→rendered-script wiring
// that unit-testing the fold/resolver in isolation cannot cover.
func deliveryTaskContent(t *testing.T, brain *BrainServiceImpl, project string) string {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	var content string
	n := 0
	for _, e := range resp.Entries {
		if strings.HasPrefix(e.GeneratedBy, "automation:") {
			n++
			content = e.Content
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 generated delivery task, got %d", n)
	}
	return content
}

// TestFeatureDeliveryAutomation_ThreadsDeliveryModeAndTargetBranchIntoScript is
// the regression for the mr-mode E2E bug: a feature that opted into
// delivery_mode:mr with merge_target_branch:main previously rendered a script
// with the HARDCODED defaults DELIVERY_MODE=local_merge / TARGET_BRANCH='dev'
// because (1) DeliveryMode was dropped in brainEntryToResolvedTask and
// (2) merge_target_branch was never threaded onto the event metadata or into
// the template. This asserts the folded delivery_mode + merge_target_branch
// reach the rendered script, so mr mode targets main and takes the push+glab-mr
// path — NOT a local_merge into dev.
func TestFeatureDeliveryAutomation_ThreadsDeliveryModeAndTargetBranchIntoScript(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Config default target is deliberately "dev" — the WRONG value the bug
	// rendered. The per-feature merge_target_branch:main must override it.
	if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{
		Enabled:           true,
		MergeTargetBranch: "dev",
	}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	// A folded feature.completed event exactly as CheckFeatureCompletion now
	// stamps it: delivery_mode + merge_target_branch on the metadata.
	if err := automation.HandleEvent(ctx, types.Event{
		ID:        "evt-delivery-thread",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "delivery-thread",
		FeatureID: "delivery-smoke",
		Metadata: map[string]string{
			"delivery_mode":       "mr",
			"merge_target_branch": "main",
		},
	}); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	script := deliveryTaskContent(t, brain, "delivery-thread")

	// The folded delivery_mode:mr must be rendered as the DELIVERY_MODE
	// default (not the hardcoded local_merge).
	if !strings.Contains(script, `DELIVERY_MODE="mr"`) {
		t.Errorf("rendered script did not thread delivery_mode=mr; got:\n%s", script)
	}
	if strings.Contains(script, `DELIVERY_MODE="local_merge"`) {
		t.Errorf("rendered script still defaults DELIVERY_MODE to local_merge (bug); got:\n%s", script)
	}
	// The per-feature merge_target_branch:main must override the config
	// default "dev".
	if !strings.Contains(script, `TARGET_BRANCH="main"`) {
		t.Errorf("rendered script did not thread merge_target_branch=main into TARGET_BRANCH; got:\n%s", script)
	}
	if strings.Contains(script, `TARGET_BRANCH='dev'`) && !strings.Contains(script, `TARGET_BRANCH="main"`) {
		t.Errorf("rendered script used the hardcoded config default TARGET_BRANCH='dev'; got:\n%s", script)
	}
	// mr mode must take the push + glab-mr path, never the local_merge branch.
	for _, needle := range []string{
		`git push -u origin`,
		`glab mr create`,
		`--target-branch "${TARGET_BRANCH}"`,
	} {
		if !strings.Contains(script, needle) {
			t.Errorf("rendered script missing mr-path substring %q", needle)
		}
	}

	// The DELIVERY_MODE placeholder still routes on ${DELIVERY_MODE} at
	// runtime, and with mode=mr the "mr)" arm is taken; the local_merge
	// squash into the wrong target must not be what this feature runs.
	// (The runtime switch itself is covered by the mode-slicing tests; here
	// we only assert the resolved constants that drive it.)
}
