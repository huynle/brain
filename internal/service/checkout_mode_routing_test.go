package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// The manual "Review & merge…" endpoint used to write checkout_mode into
// frontmatter and never read it, so picking "simple" produced the same LLM
// prose task as "ai" — no executor, no script. These tests pin the routing.

func TestCheckoutFeature_SimpleModeEmitsRunnableScriptTask(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	result, err := svc.CheckoutFeature(ctx, "brain", "feat-alpha", &types.FeatureCheckoutOptions{
		CheckoutMode:       "simple",
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
	})
	if err != nil {
		t.Fatalf("CheckoutFeature: %v", err)
	}
	if !result.Created {
		t.Fatalf("expected creation")
	}

	raw := readCheckoutTaskFile(t, brainDir, "brain")

	// The three fields that make the task actually run as a script. Without
	// executor+direct_prompt the runner resolves it to opencode and hands an
	// LLM a bash script as a prompt.
	for _, want := range []string{
		"executor: script",
		"direct_prompt:",
		"execution_mode: current_branch",
		"checkout_mode: simple",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in:\n%s", want, raw)
		}
	}
	// And the script body itself, with the merge invariant intact.
	for _, want := range []string{
		"git -c merge.ff=true merge --squash",
		"FEATURE_ID='feat-alpha'",
		"TARGET_BRANCH='main'",
		"git push origin --delete",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing script line %q in:\n%s", want, raw)
		}
	}
	// A Brain template placeholder here would never be expanded: the manual
	// endpoint publishes no event, so nothing runs renderAutomationTemplate.
	if strings.Contains(raw, "{{.FeatureID}}") || strings.Contains(raw, "{{.ProjectID}}") {
		t.Errorf("unexpanded template placeholder in manual-path script:\n%s", raw)
	}
}

func TestCheckoutFeature_AIModeStaysAPromptTask(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	if _, err := svc.CheckoutFeature(ctx, "brain", "feat-beta", &types.FeatureCheckoutOptions{
		CheckoutMode:      "ai",
		MergeTargetBranch: "main",
	}); err != nil {
		t.Fatalf("CheckoutFeature: %v", err)
	}

	raw := readCheckoutTaskFile(t, brainDir, "brain")
	if strings.Contains(raw, "executor: script") {
		t.Errorf("ai mode must not become a script task:\n%s", raw)
	}
	if strings.Contains(raw, "git -c merge.ff=true") {
		t.Errorf("ai mode must not carry the deterministic script:\n%s", raw)
	}
}

// A second checkout with a DIFFERENT mode used to hit the hardcoded
// round-1 idempotency key, return the old task, and silently discard the
// mode the user had just chosen.
func TestCheckoutFeature_ModeChangeSupersedesPendingTask(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	first, err := svc.CheckoutFeature(ctx, "brain", "feat-gamma", &types.FeatureCheckoutOptions{
		CheckoutMode: "ai", MergeTargetBranch: "main",
	})
	if err != nil {
		t.Fatalf("first CheckoutFeature: %v", err)
	}

	second, err := svc.CheckoutFeature(ctx, "brain", "feat-gamma", &types.FeatureCheckoutOptions{
		CheckoutMode: "simple", MergeTargetBranch: "main",
	})
	if err != nil {
		t.Fatalf("second CheckoutFeature: %v", err)
	}
	if !second.Created {
		t.Fatalf("mode change must create a new task, got Created=false")
	}
	if !second.Superseded || second.SupersededTaskID != first.Task.ID {
		t.Errorf("expected supersede of %s, got superseded=%v id=%q",
			first.Task.ID, second.Superseded, second.SupersededTaskID)
	}

	raw := readCheckoutTaskFile(t, brainDir, "brain")
	if !strings.Contains(raw, "executor: script") {
		t.Errorf("superseding task should be the simple one:\n%s", raw)
	}
	// The old file must be gone, not left beside the new one.
	dir := filepath.Join(brainDir, "projects", "brain", "task")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected exactly one checkout task after supersede, got %d", len(entries))
	}
}

// Same mode twice stays idempotent — that behavior was correct and the
// supersede path must not regress it.
func TestCheckoutFeature_SameModeIsIdempotent(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	opts := &types.FeatureCheckoutOptions{CheckoutMode: "simple", MergeTargetBranch: "main"}
	first, err := svc.CheckoutFeature(ctx, "brain", "feat-delta", opts)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.CheckoutFeature(ctx, "brain", "feat-delta", opts)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Created {
		t.Errorf("same mode must not re-create")
	}
	if second.Task.ID != first.Task.ID {
		t.Errorf("expected the original task back, got %s want %s", second.Task.ID, first.Task.ID)
	}
	// The already-exists branch used to drop the "/task/" path segment, so
	// the path it returned resolved to nothing.
	if !strings.Contains(second.Task.Path, "/task/") {
		t.Errorf("path missing /task/ segment: %q", second.Task.Path)
	}
}

// A checkout task written by an older build RECORDED checkout_mode without
// acting on it, so a stored "simple" task can carry no executor at all. It
// matches on mode, so mode comparison alone hands that dead task back
// forever — the fix would never reach a feature that already had one.
func TestCheckoutFeature_StaleSimpleTaskWithoutExecutorIsReplaced(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	// Hand-write the pre-fix shape: simple mode, no executor, prose body.
	dir := filepath.Join(brainDir, "projects", "brain", "task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := "---\ntype: task\ntitle: 'Feature checkout: feat-legacy'\nstatus: pending\n" +
		"feature_id: feat-legacy\ncheckout_mode: simple\n" +
		"generated_key: 'feature-checkout:feat-legacy:round-1'\n---\n\nMerge intent: prose.\n"
	if err := os.WriteFile(filepath.Join(dir, "legacy01.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := svc.CheckoutFeature(ctx, "brain", "feat-legacy", &types.FeatureCheckoutOptions{
		CheckoutMode: "simple", MergeTargetBranch: "main",
	})
	if err != nil {
		t.Fatalf("CheckoutFeature: %v", err)
	}
	if !result.Created {
		t.Fatalf("a stale simple task must be replaced, got Created=false")
	}
	if result.SupersededTaskID != "legacy01" {
		t.Errorf("expected supersede of legacy01, got %q", result.SupersededTaskID)
	}
	raw := readCheckoutTaskFile(t, brainDir, "brain")
	if !strings.Contains(raw, "executor: script") {
		t.Errorf("replacement must be runnable:\n%s", raw)
	}
}

// The manual endpoint has no automation entry to inherit a workdir from, so
// it resolves one from the feature's own tasks. getFeatureTasksFromFilesystem
// used to drop every frontmatter field but four, so that resolution always
// saw empty and the generated git script ran wherever the runner happened to
// be — which is how a well-formed checkout task still failed with git's
// "not a git repository".
func TestCheckoutFeature_ResolvesWorkdirFromFeatureTasks(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	dir := filepath.Join(brainDir, "projects", "brain", "task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	work := "---\ntype: task\ntitle: real work\nstatus: completed\n" +
		"feature_id: feat-wd\ntarget_workdir: /repos/thing\n---\n\nDone.\n"
	if err := os.WriteFile(filepath.Join(dir, "work0001.md"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CheckoutFeature(ctx, "brain", "feat-wd", &types.FeatureCheckoutOptions{
		CheckoutMode: "simple", MergeTargetBranch: "main",
	}); err != nil {
		t.Fatalf("CheckoutFeature: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var raw string
	for _, e := range entries {
		if e.Name() == "work0001.md" {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		raw = string(b)
	}
	if !strings.Contains(raw, "target_workdir: /repos/thing") {
		t.Errorf("checkout task must inherit the feature's repo, got:\n%s", raw)
	}
}

// "" and "ai" are the same mode. The write side deliberately omits
// checkout_mode rather than persisting "ai", and the two front doors disagree
// by construction — the PWA always sends "ai", the MCP tool passes "" through
// when the caller omits it. Comparing raw strings made an ordinary
// agent-creates-then-human-confirms flow delete and recreate a byte-identical
// task, losing its id and resetting its retry attempt_count, forever.
func TestCheckoutFeature_EmptyAndAIAreTheSameMode(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// The MCP shape: no checkout_mode at all.
	first, err := svc.CheckoutFeature(ctx, "brain", "feat-fold", &types.FeatureCheckoutOptions{})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// The PWA shape: explicit "ai".
	second, err := svc.CheckoutFeature(ctx, "brain", "feat-fold",
		&types.FeatureCheckoutOptions{CheckoutMode: "ai"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Created || second.Superseded {
		t.Errorf(`"" then "ai" must be idempotent, got created=%v superseded=%v`,
			second.Created, second.Superseded)
	}
	if second.Task.ID != first.Task.ID {
		t.Errorf("task id churned: %s -> %s", first.Task.ID, second.Task.ID)
	}
	// And back the other way — alternating front doors must still converge.
	third, err := svc.CheckoutFeature(ctx, "brain", "feat-fold", &types.FeatureCheckoutOptions{})
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if third.Created || third.Task.ID != first.Task.ID {
		t.Errorf("alternating front doors churn the task: %+v", third)
	}
}
