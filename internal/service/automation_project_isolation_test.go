package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// Project isolation is the contract users rely on: an automation configured
// for one project must never act on another project's work. A blocked-task
// inspector set up for "alpha" inspecting "beta" would reset tasks nobody
// asked it to touch.
//
// The rules these tests pin:
//
//   - A project-scoped automation fires ONLY for its own project's events.
//   - A global automation is inert for project events unless it explicitly
//     opts in with a project:"*" filter.
//   - Opting in with project:"*" is the ONLY way to go cross-project, and
//     it is visible in the entry's own trigger.
//
// Everything here drives AutomationService.HandleEvent / CheckScheduled
// directly — the same entry points the trigger dispatcher calls — so a
// passing test means the real dispatch path behaves this way.

// saveProjectAutomation registers an event automation scoped to one project.
func saveProjectAutomation(t *testing.T, brain *BrainServiceImpl, project, title string, filter map[string]string) string {
	t.Helper()
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   title,
		Content: "test automation",
		Status:  "active",
		Project: project,
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventTaskCompleted,
			Filter: filter,
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Inspect blocked tasks in {{.ProjectID}}.",
		},
	})
	if err != nil {
		t.Fatalf("save automation %q: %v", title, err)
	}
	return resp.ID
}

// generatedTaskCount counts tasks in a project (automations only ever create
// tasks, so this is the observable effect of a fired automation).
func generatedTaskCount(t *testing.T, brain *BrainServiceImpl, project string) int {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("list tasks in %q: %v", project, err)
	}
	return len(resp.Entries)
}

func taskCompletedEvent(id, project string) types.Event {
	return types.Event{
		ID:        id,
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: project,
		TaskID:    "src-" + id,
	}
}

// ─── Core isolation ────────────────────────────────────────────────

func TestAutomationIsolation_FiresForOwnProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "Alpha inspector", nil)
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha generated tasks = %d, want 1", got)
	}
}

func TestAutomationIsolation_DoesNotFireForOtherProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "Alpha inspector", nil)
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "beta")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "beta"); got != 0 {
		t.Errorf("beta generated tasks = %d, want 0 — alpha's automation leaked", got)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha generated tasks = %d, want 0 — event was for beta", got)
	}
}

// Two projects each running "the same" automation must fire independently:
// one event, one task, in the right project.
func TestAutomationIsolation_TwoProjectsFireIndependently(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "Blocked inspector", nil)
	saveProjectAutomation(t, brain, "beta", "Blocked inspector", nil)
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent alpha: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1", got)
	}
	if got := generatedTaskCount(t, brain, "beta"); got != 0 {
		t.Errorf("beta tasks = %d, want 0 after an alpha-only event", got)
	}

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e2", "beta")); err != nil {
		t.Fatalf("HandleEvent beta: %v", err)
	}
	if got := generatedTaskCount(t, brain, "beta"); got != 1 {
		t.Errorf("beta tasks = %d, want 1", got)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 (unchanged by the beta event)", got)
	}
}

// project:"*" is the explicit cross-project opt-in. It must work, and it
// must be the only way in.
func TestAutomationIsolation_WildcardOptsIntoCrossProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "Fleet inspector",
		map[string]string{"project": "*"})
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "beta")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	// The task lands in the automation's own project — the automation owns
	// the work it generates, wherever the triggering event came from.
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 (wildcard automation should fire)", got)
	}
}

// A generated task must land in a project, never in the void. A task with
// no project would be invisible to every project view and to the runner.
func TestAutomationIsolation_GeneratedTaskCarriesProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "Alpha inspector", nil)
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type: "task", Project: "alpha", Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("tasks = %d, want 1", len(resp.Entries))
	}
	if !strings.HasPrefix(resp.Entries[0].Path, "projects/alpha/") {
		t.Errorf("task path = %q, want it under projects/alpha/", resp.Entries[0].Path)
	}
}

// A cross-project automation has two different projects in play: the one
// that owns the automation, and the one the event came from. The template
// exposes both, and a fleet-wide inspector needs EventProjectID — telling
// the agent to inspect the automation's own project would have it sweep
// the wrong one on every trigger.
func TestAutomationIsolation_PromptCanTargetTheEventProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Fleet inspector", Content: "x",
		Status: "active", Project: "alpha",
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventTaskCompleted,
			Filter: map[string]string{"project": "*"},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Inspect blocked tasks in {{.EventProjectID}} (owned by {{.ProjectID}}).",
		},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "beta")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type: "task", Project: "alpha", Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("tasks = %d, want 1", len(resp.Entries))
	}
	got := resp.Entries[0].Content
	if !strings.Contains(got, "in beta") {
		t.Errorf("EventProjectID did not resolve to the triggering project:\n%s", got)
	}
	if !strings.Contains(got, "owned by alpha") {
		t.Errorf("ProjectID did not resolve to the owning project:\n%s", got)
	}
}

// ─── Webhook + session triggers ────────────────────────────────────

func TestAutomationIsolation_WebhookRespectsProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Alpha webhook", Content: "x",
		Status: "active", Project: "alpha",
		Trigger: &types.TriggerConfig{Type: "webhook", Webhook: "/deploy"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "run"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc := NewAutomationService(brain)

	evt := types.Event{
		ID: "wh1", Type: "webhook.received", Source: types.EventSourceAPI,
		ProjectID: "beta",
		Metadata:  map[string]string{"webhook_path": "/deploy"},
	}
	if err := svc.HandleEvent(ctx, evt); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 — webhook for beta fired alpha's automation", got)
	}
}

func TestAutomationIsolation_SessionRespectsProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Alpha session", Content: "x",
		Status: "active", Project: "alpha",
		Trigger: &types.TriggerConfig{Type: "session"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "run"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc := NewAutomationService(brain)

	evt := types.Event{
		ID: "s1", Type: types.EventRunnerSessionDiscovered,
		Source: types.EventSourceRunner, ProjectID: "beta",
	}
	if err := svc.HandleEvent(ctx, evt); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 — beta session fired alpha's automation", got)
	}
}

// ─── Cron ──────────────────────────────────────────────────────────

// A project-scoped cron automation (the shape a per-project blocked
// inspector takes) must put its task in that project, not the default.
func TestAutomationIsolation_CronTaskLandsInOwnProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Alpha cron", Content: "x",
		Status: "active", Project: "alpha",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "* * * * *"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "inspect {{.ProjectID}}"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc := NewAutomationService(brain)

	if err := svc.CheckScheduled(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1", got)
	}
	if got := generatedTaskCount(t, brain, "default"); got != 0 {
		t.Errorf("default tasks = %d, want 0 — cron task escaped its project", got)
	}
}

// Two per-project cron automations must each produce exactly one task in
// their own project on the same tick.
func TestAutomationIsolation_CronPerProjectDoesNotCrossOver(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for _, p := range []string{"alpha", "beta"} {
		if _, err := brain.Save(ctx, types.CreateEntryRequest{
			Type: "automation", Title: "Blocked inspector", Content: "x",
			Status: "active", Project: p,
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "* * * * *"},
			Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "inspect {{.ProjectID}}"},
		}); err != nil {
			t.Fatalf("save %s: %v", p, err)
		}
	}
	svc := NewAutomationService(brain)

	if err := svc.CheckScheduled(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	for _, p := range []string{"alpha", "beta"} {
		if got := generatedTaskCount(t, brain, p); got != 1 {
			t.Errorf("%s tasks = %d, want exactly 1", p, got)
		}
	}
}

// ─── Monitor scoping (blocked inspector) ───────────────────────────

// The blocked inspector's prompt must name only its own project. A prompt
// that tells the agent to sweep every project is how one project's
// inspector ends up resetting another's tasks.
func TestMonitorPrompt_ProjectScopedInspectorNamesOnlyItsProject(t *testing.T) {
	prompt := buildMonitorPrompt("blocked-inspector", types.MonitorScope{
		Type: "project", Project: "alpha",
	})
	if !strings.Contains(prompt, `brain_tasks({ project: "alpha", status: "blocked" })`) {
		t.Errorf("prompt does not scope the query to alpha:\n%s", prompt)
	}
	if strings.Contains(prompt, "Discover all projects") {
		t.Errorf("project-scoped inspector prompt contains all-project discovery:\n%s", prompt)
	}
}

// The "all" scope is the deliberate opt-in and must still work.
func TestMonitorPrompt_AllScopeSweepsEveryProject(t *testing.T) {
	prompt := buildMonitorPrompt("blocked-inspector", types.MonitorScope{Type: "all"})
	if !strings.Contains(prompt, "Discover all projects") {
		t.Errorf("all-scope prompt missing discovery instructions:\n%s", prompt)
	}
}

// Monitor tags must be unique per project, or enabling an inspector on a
// second project would collide with the first and silently no-op.
func TestMonitorTag_IsUniquePerProject(t *testing.T) {
	a := BuildMonitorTag("blocked-inspector", types.MonitorScope{Type: "project", Project: "alpha"})
	b := BuildMonitorTag("blocked-inspector", types.MonitorScope{Type: "project", Project: "beta"})
	if a == b {
		t.Fatalf("monitor tags collide across projects: %q", a)
	}

	parsed := ParseMonitorTag(a)
	if parsed == nil {
		t.Fatalf("ParseMonitorTag(%q) returned nil", a)
	}
	if parsed.Scope.Project != "alpha" {
		t.Errorf("parsed project = %q, want alpha", parsed.Scope.Project)
	}
	if parsed.TemplateID != "blocked-inspector" {
		t.Errorf("parsed template = %q, want blocked-inspector", parsed.TemplateID)
	}
}

func TestMonitorTag_FeatureScopeIncludesProject(t *testing.T) {
	// Two projects can legitimately use the same feature id; the tag must
	// still distinguish them.
	a := BuildMonitorTag("feature-review", types.MonitorScope{
		Type: "feature", FeatureID: "checkout", Project: "alpha",
	})
	b := BuildMonitorTag("feature-review", types.MonitorScope{
		Type: "feature", FeatureID: "checkout", Project: "beta",
	})
	if a == b {
		t.Fatalf("feature monitor tags collide across projects: %q", a)
	}
}

// ─── Pause is per project ──────────────────────────────────────────

type projectPauseChecker struct {
	pausedProjects map[string]bool
	globalPaused   bool
}

func (p *projectPauseChecker) IsAutomationsPaused() bool { return p.globalPaused }
func (p *projectPauseChecker) IsAutomationsPausedForProject(project string) bool {
	if p.globalPaused {
		return true
	}
	return p.pausedProjects[project]
}

// Pausing automations on one project must not pause another's.
func TestAutomationIsolation_PauseIsPerProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "Alpha inspector", nil)
	saveProjectAutomation(t, brain, "beta", "Beta inspector", nil)

	svc := NewAutomationService(brain)
	svc.SetPauseChecker(&projectPauseChecker{pausedProjects: map[string]bool{"alpha": true}})

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent alpha: %v", err)
	}
	if err := svc.HandleEvent(ctx, taskCompletedEvent("e2", "beta")); err != nil {
		t.Fatalf("HandleEvent beta: %v", err)
	}

	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 (paused)", got)
	}
	if got := generatedTaskCount(t, brain, "beta"); got != 1 {
		t.Errorf("beta tasks = %d, want 1 — pausing alpha must not pause beta", got)
	}
}

// ─── Built-in feature checkout: the merge-to-target-branch path ────
//
// These are the automations that let a large feature build itself and then
// land on the target branch. They are registered once, globally, at server
// startup — so they must fire for events from EVERY project without any
// per-project setup step.

// ensureBuiltIns registers both built-in checkout automations exactly the
// way apiserver.Start does — global, no per-project copies.
func ensureBuiltIns(t *testing.T, brain *BrainServiceImpl, mergeTarget string) {
	t.Helper()
	ctx := context.Background()
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:            true,
		MergeTargetBranch:  mergeTarget,
		MergePolicy:        "auto_pr",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo",
		ExecutionMode:      "worktree",
	}); err != nil {
		t.Fatalf("ensure AI built-in: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:            true,
		MergeTargetBranch:  mergeTarget,
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo",
	}); err != nil {
		t.Fatalf("ensure simple built-in: %v", err)
	}
}

func featureCompletedEvent(id, project, feature, mode string) types.Event {
	evt := types.Event{
		ID:        id,
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: project,
		FeatureID: feature,
		Metadata:  map[string]string{"completed": "1", "total": "1"},
	}
	if mode != "" {
		evt.Metadata["checkout_mode"] = mode
	}
	return evt
}

// findGeneratedTask returns the single task generated by a built-in, across
// every project (the built-ins are global, so the task lands wherever
// createTask resolved the project to).
func findGeneratedTask(t *testing.T, brain *BrainServiceImpl, generatedBy string) *types.BrainEntry {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type: "task", Limit: 200,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var found *types.BrainEntry
	for i := range resp.Entries {
		if strings.Contains(resp.Entries[i].Content, generatedBy) ||
			resp.Entries[i].GeneratedBy == generatedBy {
			e := resp.Entries[i]
			found = &e
		}
	}
	return found
}

// THE headline case: a feature completing in a real project must produce a
// checkout task, with no per-project automation setup.
func TestBuiltInCheckout_FiresForProjectFeatureCompletion(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "checkout-flow", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Fatalf("alpha generated tasks = %d, want 1 — the built-in checkout automation did not fire", got)
	}
}

// The same built-ins must serve every project, since they are registered
// once globally.
func TestBuiltInCheckout_FiresForEveryProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	for i, project := range []string{"alpha", "beta", "gamma"} {
		evt := featureCompletedEvent(fmt.Sprintf("f%d", i), project, "feat-"+project, "ai")
		if err := svc.HandleEvent(ctx, evt); err != nil {
			t.Fatalf("HandleEvent %s: %v", project, err)
		}
		if got := generatedTaskCount(t, brain, project); got != 1 {
			t.Errorf("%s generated tasks = %d, want 1", project, got)
		}
	}
}

// checkout_mode discriminates the two built-ins. A simple-mode feature must
// get the deterministic script path, not the AI one — and exactly one task,
// not both.
func TestBuiltInCheckout_SimpleModeSelectsScriptAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "simple")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want exactly 1 (simple only)", len(resp.Entries))
	}
	if resp.Entries[0].Executor != "script" {
		t.Errorf("executor = %q, want script", resp.Entries[0].Executor)
	}
}

func TestBuiltInCheckout_AIModeSelectsPromptAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want exactly 1 (AI only)", len(resp.Entries))
	}
	if resp.Entries[0].Executor == "script" {
		t.Error("AI-mode feature selected the script automation")
	}
	if !strings.Contains(resp.Entries[0].Content, "feature-checkout") {
		t.Errorf("AI checkout prompt missing the skill reference:\n%s", resp.Entries[0].Content)
	}
}

// An event with no checkout_mode must fall back to AI, not silently match
// nothing. Older persisted events replayed after upgrade look like this.
func TestBuiltInCheckout_MissingModeFallsBackToAI(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Fatalf("alpha tasks = %d, want 1 (AI fallback)", got)
	}
}

// The merge target is the whole point of this automation — a checkout task
// that does not know where to land is useless for orchestration.
func TestBuiltInCheckout_TaskCarriesMergeTargetBranch(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "release/v2")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want 1", len(resp.Entries))
	}
	task := resp.Entries[0]
	if task.MergeTargetBranch != "release/v2" {
		t.Errorf("merge_target_branch = %q, want release/v2", task.MergeTargetBranch)
	}
	if !strings.Contains(task.Content, "release/v2") {
		t.Errorf("prompt does not name the merge target:\n%s", task.Content)
	}
}

func TestBuiltInCheckout_SimpleTaskCarriesMergeTargetBranch(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "release/v2")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "simple")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want 1", len(resp.Entries))
	}
	task := resp.Entries[0]
	if !strings.Contains(task.Content, "release/v2") {
		t.Errorf("simple checkout script does not target release/v2:\n%s", task.Content)
	}
	// The ff=true invariant makes the squash-merge work regardless of the
	// user's own merge.ff gitconfig. Losing it breaks checkout on some hosts.
	if !strings.Contains(task.Content, "git -c merge.ff=true merge --squash") {
		t.Errorf("simple checkout script lost the merge.ff invariant:\n%s", task.Content)
	}
}

// Every structured merge field must reach the generated task. The executor
// reads these — not the prompt prose — when it actually merges, so a task
// missing them cannot land the branch it was created to land.
func TestBuiltInCheckout_TaskInheritsAllMergeSettings(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	openPR := true
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:            true,
		MergeTargetBranch:  "release/v2",
		MergePolicy:        "auto_pr",
		MergeStrategy:      "rebase",
		RemoteBranchPolicy: "delete",
		OpenPRBeforeMerge:  &openPR,
		TargetWorkdir:      "/repo",
		ExecutionMode:      "worktree",
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want 1", len(resp.Entries))
	}
	task := resp.Entries[0]

	for _, tc := range []struct{ field, got, want string }{
		{"merge_target_branch", task.MergeTargetBranch, "release/v2"},
		{"merge_policy", task.MergePolicy, "auto_pr"},
		{"merge_strategy", task.MergeStrategy, "rebase"},
		{"remote_branch_policy", task.RemoteBranchPolicy, "delete"},
		{"execution_mode", task.ExecutionMode, "worktree"},
		{"target_workdir", task.TargetWorkdir, "/repo"},
	} {
		if tc.got != tc.want {
			t.Errorf("task %s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}
	if task.OpenPRBeforeMerge == nil || !*task.OpenPRBeforeMerge {
		t.Error("task open_pr_before_merge did not inherit true")
	}
}

// Fields the automation leaves unset must stay unset on the task, so
// task_defaults can still supply them downstream. Writing zero values here
// would silently override the user's configured defaults.
func TestBuiltInCheckout_UnsetMergeFieldsStayUnset(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled: true,
		// No merge configuration at all.
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want 1", len(resp.Entries))
	}
	if got := resp.Entries[0].MergeTargetBranch; got != "" {
		t.Errorf("merge_target_branch = %q, want empty so task_defaults can fill it", got)
	}
	if got := resp.Entries[0].MergePolicy; got != "" {
		t.Errorf("merge_policy = %q, want empty", got)
	}
}

// A user-authored automation gets the same propagation — this is not
// special-cased for the built-ins.
func TestAutomationTask_InheritsMergeSettingsFromUserAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Custom merger", Content: "x",
		Status: "active", Project: "alpha",
		Trigger:           &types.TriggerConfig{Type: "event", Event: types.EventTaskCompleted},
		Action:            &types.AutomationAction{Type: "prompt", DirectPrompt: "merge it"},
		MergeTargetBranch: "develop",
		MergeStrategy:     "squash",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("tasks = %d, want 1", len(resp.Entries))
	}
	if got := resp.Entries[0].MergeTargetBranch; got != "develop" {
		t.Errorf("merge_target_branch = %q, want develop", got)
	}
	if got := resp.Entries[0].MergeStrategy; got != "squash" {
		t.Errorf("merge_strategy = %q, want squash", got)
	}
}

// ─── Migration of pre-existing entries ─────────────────────────────

// An install that already has the built-ins must be repaired on restart.
// Before the wildcard fix those entries were inert, so without an in-place
// migration feature checkout would stay broken forever on every existing
// deployment.
func TestBuiltInCheckout_MigratesInertLegacyEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Write the pre-fix shape by hand: correct checkout_mode, no wildcard.
	global := true
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Built-in feature checkout",
		Content: "legacy", Status: "active", Global: &global,
		GeneratedBy: BuiltInFeatureCheckoutGeneratedBy,
		Trigger: &types.TriggerConfig{
			Type: "event", Event: types.EventFeatureCompleted,
			OncePer: "feature_id",
			Filter:  map[string]string{"checkout_mode": "ai"},
		},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "checkout {{.FeatureID}}"},
	}); err != nil {
		t.Fatalf("save legacy: %v", err)
	}

	// Restart-equivalent.
	ensureBuiltIns(t, brain, "main")

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 — legacy entry was not migrated", got)
	}
}

func TestBuiltInCheckoutSimple_MigratesInertLegacyEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	global := true
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Built-in feature checkout (simple/script)",
		Content: "legacy", Status: "active", Global: &global,
		GeneratedBy: BuiltInFeatureCheckoutSimpleGeneratedBy,
		Trigger: &types.TriggerConfig{
			Type: "event", Event: types.EventFeatureCompleted,
			OncePer: "feature_id",
			Filter:  map[string]string{"checkout_mode": "simple"},
		},
		Action: &types.AutomationAction{
			Type: types.AutomationActionScript, Command: "echo {{.FeatureID}}",
		},
	}); err != nil {
		t.Fatalf("save legacy: %v", err)
	}

	ensureBuiltIns(t, brain, "main")

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "simple")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 — legacy simple entry was not migrated", got)
	}
}

// Migration must not duplicate the entry, and must be a no-op once the
// shape is already right.
func TestBuiltInCheckout_EnsureIsIdempotent(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ensureBuiltIns(t, brain, "main")
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	counts := map[string]int{}
	for _, e := range resp.Entries {
		counts[e.GeneratedBy]++
	}
	if counts[BuiltInFeatureCheckoutGeneratedBy] != 1 {
		t.Errorf("AI built-ins = %d, want 1", counts[BuiltInFeatureCheckoutGeneratedBy])
	}
	if counts[BuiltInFeatureCheckoutSimpleGeneratedBy] != 1 {
		t.Errorf("simple built-ins = %d, want 1", counts[BuiltInFeatureCheckoutSimpleGeneratedBy])
	}
}

// The wildcard is what makes a global built-in visible to project events.
// Pin it explicitly so a future edit to the filter cannot silently make
// both automations inert again.
func TestBuiltInCheckout_TriggerCarriesProjectWildcard(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := 0
	for _, e := range resp.Entries {
		if e.GeneratedBy != BuiltInFeatureCheckoutGeneratedBy &&
			e.GeneratedBy != BuiltInFeatureCheckoutSimpleGeneratedBy {
			continue
		}
		seen++
		if e.Trigger == nil || e.Trigger.Filter["project"] != "*" {
			t.Errorf("%s trigger filter missing project:* — the automation will never fire", e.GeneratedBy)
		}
		if e.ProjectID != "" {
			t.Errorf("%s has project_id %q, want global", e.GeneratedBy, e.ProjectID)
		}
	}
	if seen != 2 {
		t.Fatalf("found %d built-in automations, want 2", seen)
	}
}

// The feature id must reach the task, or the checkout agent cannot know
// which branch to merge.
func TestBuiltInCheckout_TaskCarriesFeatureID(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "checkout-flow", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("alpha tasks = %d, want 1", len(resp.Entries))
	}
	if !strings.Contains(resp.Entries[0].Content, "checkout-flow") {
		t.Errorf("prompt did not interpolate FeatureID:\n%s", resp.Entries[0].Content)
	}
	if resp.Entries[0].FeatureID != "checkout-flow" {
		t.Errorf("task feature_id = %q, want checkout-flow", resp.Entries[0].FeatureID)
	}
}

// once_per: feature_id must stop a re-emitted completion event from
// queueing a second checkout for the same feature.
func TestBuiltInCheckout_DedupesRepeatedCompletionForSameFeature(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	for i := 0; i < 3; i++ {
		evt := featureCompletedEvent(fmt.Sprintf("f%d", i), "alpha", "checkout-flow", "ai")
		if err := svc.HandleEvent(ctx, evt); err != nil {
			t.Fatalf("HandleEvent #%d: %v", i, err)
		}
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 — once_per feature_id did not dedup", got)
	}
}

// Dedup must be per feature, not global: a second feature completing has to
// get its own checkout.
func TestBuiltInCheckout_DifferentFeaturesEachGetACheckout(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-one", "ai")); err != nil {
		t.Fatalf("HandleEvent one: %v", err)
	}
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f2", "alpha", "feat-two", "ai")); err != nil {
		t.Fatalf("HandleEvent two: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 2 {
		t.Errorf("alpha tasks = %d, want 2 (one per feature)", got)
	}
}

// The same feature id in two different projects is two different features.
// Dedup keyed only on feature_id would starve the second project.
func TestBuiltInCheckout_SameFeatureIDInTwoProjectsBothFire(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "shared-name", "ai")); err != nil {
		t.Fatalf("HandleEvent alpha: %v", err)
	}
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f2", "beta", "shared-name", "ai")); err != nil {
		t.Fatalf("HandleEvent beta: %v", err)
	}

	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1", got)
	}
	if got := generatedTaskCount(t, brain, "beta"); got != 1 {
		t.Errorf("beta tasks = %d, want 1 — dedup leaked across projects", got)
	}
}

// Pausing one project's automations must not stop another project's
// feature from being checked out and merged.
func TestBuiltInCheckout_PausedProjectDoesNotBlockOthers(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)
	svc.SetPauseChecker(&projectPauseChecker{pausedProjects: map[string]bool{"alpha": true}})

	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-a", "ai")); err != nil {
		t.Fatalf("HandleEvent alpha: %v", err)
	}
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f2", "beta", "feat-b", "ai")); err != nil {
		t.Fatalf("HandleEvent beta: %v", err)
	}

	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 (paused)", got)
	}
	if got := generatedTaskCount(t, brain, "beta"); got != 1 {
		t.Errorf("beta tasks = %d, want 1 — a paused project blocked an unrelated checkout", got)
	}
}

// A disabled built-in must not fire at all.
func TestBuiltInCheckout_DisabledDoesNotFire(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled: false,
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 (automation disabled)", got)
	}
}

// A non-feature event must never trigger a checkout.
func TestBuiltInCheckout_IgnoresUnrelatedEvents(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 — task.completed triggered a feature checkout", got)
	}
}

// ─── Monitors: the blocked inspector, per project ──────────────────
//
// Monitors are scheduled tasks rather than event automations, so their
// isolation comes from two places: the task's own project field, and the
// monitor tag that its prompt is derived from at read time
// (resolveBuiltinMonitorPrompt). Both have to be right, or an inspector
// enabled on one project sweeps another.

func TestMonitor_ProjectScopedTaskLandsInThatProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	svc := NewMonitorService(brain)
	ctx := context.Background()

	res, err := svc.Create(ctx, "blocked-inspector",
		types.MonitorScope{Type: "project", Project: "alpha"}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(res.Path, "projects/alpha/") {
		t.Errorf("monitor path = %q, want it under projects/alpha/", res.Path)
	}
}

// Enabling the same monitor on a second project must succeed — the
// duplicate check is scoped by tag, and tags include the project.
func TestMonitor_SameTemplateEnablesOnTwoProjects(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	svc := NewMonitorService(brain)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "blocked-inspector",
		types.MonitorScope{Type: "project", Project: "alpha"}, nil); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if _, err := svc.Create(ctx, "blocked-inspector",
		types.MonitorScope{Type: "project", Project: "beta"}, nil); err != nil {
		t.Fatalf("create beta: %v — enabling on a second project was blocked", err)
	}

	// And a genuine duplicate on the same project must still be refused.
	if _, err := svc.Create(ctx, "blocked-inspector",
		types.MonitorScope{Type: "project", Project: "alpha"}, nil); err == nil {
		t.Error("creating a duplicate monitor for alpha should fail")
	}
}

// The prompt a monitor task runs is rebuilt from its tag at read time.
// That is what carries the project scoping to the agent, so a task tagged
// for alpha must never produce beta's prompt.
func TestMonitor_PromptIsRebuiltFromTagPerProject(t *testing.T) {
	alpha := &types.ResolvedTask{
		Tags: []string{BuildMonitorTag("blocked-inspector",
			types.MonitorScope{Type: "project", Project: "alpha"})},
	}
	beta := &types.ResolvedTask{
		Tags: []string{BuildMonitorTag("blocked-inspector",
			types.MonitorScope{Type: "project", Project: "beta"})},
	}
	resolveBuiltinMonitorPrompt(alpha)
	resolveBuiltinMonitorPrompt(beta)

	if !strings.Contains(alpha.DirectPrompt, `project: "alpha"`) {
		t.Errorf("alpha monitor prompt not scoped to alpha:\n%s", alpha.DirectPrompt)
	}
	if strings.Contains(alpha.DirectPrompt, `project: "beta"`) {
		t.Errorf("alpha monitor prompt mentions beta:\n%s", alpha.DirectPrompt)
	}
	if !strings.Contains(beta.DirectPrompt, `project: "beta"`) {
		t.Errorf("beta monitor prompt not scoped to beta:\n%s", beta.DirectPrompt)
	}
}

// A task with no monitor tag must be left alone — overwriting an ordinary
// task's prompt would replace the user's instructions with an inspector's.
func TestMonitor_NonMonitorTaskPromptUntouched(t *testing.T) {
	task := &types.ResolvedTask{
		Tags:         []string{"backend", "payments"},
		DirectPrompt: "original instructions",
	}
	resolveBuiltinMonitorPrompt(task)
	if task.DirectPrompt != "original instructions" {
		t.Errorf("prompt = %q, want it untouched", task.DirectPrompt)
	}
}

// An unknown template id in a tag must not blank the prompt.
func TestMonitor_UnknownTemplateTagIsIgnored(t *testing.T) {
	task := &types.ResolvedTask{
		Tags:         []string{"monitor:not-a-real-template:project:alpha"},
		DirectPrompt: "original instructions",
	}
	resolveBuiltinMonitorPrompt(task)
	if task.DirectPrompt != "original instructions" {
		t.Errorf("prompt = %q, want it untouched for an unknown template", task.DirectPrompt)
	}
}

// The dream monitor is the other always-active scheduled template; it must
// scope per project the same way.
func TestMonitor_DreamScopesPerProject(t *testing.T) {
	a := BuildMonitorTag("dream", types.MonitorScope{Type: "project", Project: "alpha"})
	b := BuildMonitorTag("dream", types.MonitorScope{Type: "project", Project: "beta"})
	if a == b {
		t.Fatalf("dream monitor tags collide across projects: %q", a)
	}
}

// ─── checkout_mode round-trip ──────────────────────────────────────
//
// checkout_mode decides which built-in handles a completed feature: the AI
// prompt, or the deterministic squash-merge script. It is written to
// frontmatter and indexed, but was never read back onto the entry — so the
// fold always saw "" and every feature took the AI path. The simple,
// no-LLM merge was unreachable no matter what a user configured.

func TestCheckoutMode_SurvivesSaveAndRead(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "simple-mode task", Content: "x",
		Project: "alpha", FeatureID: "feat-x", CheckoutMode: "simple",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	entry, err := brain.Recall(ctx, resp.ID)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if entry.CheckoutMode != "simple" {
		t.Errorf("recalled checkout_mode = %q, want simple", entry.CheckoutMode)
	}
}

func TestCheckoutMode_SurvivesListing(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "simple-mode task", Content: "x",
		Project: "alpha", FeatureID: "feat-x", CheckoutMode: "simple",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type: "task", Project: "alpha", Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(resp.Entries))
	}
	if resp.Entries[0].CheckoutMode != "simple" {
		t.Errorf("listed checkout_mode = %q, want simple", resp.Entries[0].CheckoutMode)
	}
}

// The fold reads ResolvedTask, so the field has to survive that conversion
// too — this is the half that decides which automation actually fires.
func TestCheckoutMode_SurvivesResolvedTaskConversion(t *testing.T) {
	entry := &types.BrainEntry{
		ID: "t1", Title: "t", CheckoutMode: "simple",
	}
	rt := brainEntryToResolvedTask(entry)
	if rt.CheckoutMode != "simple" {
		t.Errorf("ResolvedTask.CheckoutMode = %q, want simple", rt.CheckoutMode)
	}
}

func TestCheckoutMode_FoldPicksSimpleWhenAnyTaskAsksForIt(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "a"},
		{ID: "b", CheckoutMode: "simple"},
		{ID: "c", CheckoutMode: "ai"},
	}
	if got := foldCheckoutMode(tasks); got != "simple" {
		t.Errorf("fold = %q, want simple (one task asked for it)", got)
	}
}

func TestCheckoutMode_FoldDefaultsToAI(t *testing.T) {
	if got := foldCheckoutMode([]types.ResolvedTask{{ID: "a"}, {ID: "b"}}); got != "ai" {
		t.Errorf("fold = %q, want ai", got)
	}
	if got := foldCheckoutMode(nil); got != "ai" {
		t.Errorf("fold of no tasks = %q, want ai", got)
	}
}

// End to end: a feature whose tasks ask for simple mode must reach the
// script automation, not the AI one. This is the case that was impossible
// before the read-path fix.
func TestCheckoutMode_SimpleTaskRoutesToScriptAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")

	// A real task carrying checkout_mode, saved and read back through
	// storage — not a hand-built struct.
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "work", Content: "x", Status: "completed",
		Project: "alpha", FeatureID: "feat-x", CheckoutMode: "simple",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	listed, err := brain.List(ctx, types.ListEntriesRequest{
		Type: "task", Project: "alpha", FeatureID: "feat-x", Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	resolved := make([]types.ResolvedTask, 0, len(listed.Entries))
	for i := range listed.Entries {
		resolved = append(resolved, brainEntryToResolvedTask(&listed.Entries[i]))
	}
	mode := foldCheckoutMode(resolved)
	if mode != "simple" {
		t.Fatalf("folded mode = %q, want simple", mode)
	}

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", mode)); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var generated *types.BrainEntry
	for i := range tasks.Entries {
		if strings.HasPrefix(tasks.Entries[i].GeneratedBy, "automation:") {
			generated = &tasks.Entries[i]
		}
	}
	if generated == nil {
		t.Fatal("no checkout task generated")
	}
	if generated.Executor != "script" {
		t.Errorf("generated executor = %q, want script (simple path)", generated.Executor)
	}
}

// ─── Workdir inheritance for feature-triggered automations ─────────
//
// A global automation has one target_workdir for every project it serves,
// which cannot be right for more than one. The built-in feature checkout is
// exactly this shape, so it falls back to the repo the feature was actually
// built in — otherwise the generated task defaults to /tmp and the merge
// dies on "not a git repository".

func saveFeatureTask(t *testing.T, brain *BrainServiceImpl, project, feature, title string, over func(*types.CreateEntryRequest)) {
	t.Helper()
	req := types.CreateEntryRequest{
		Type: "task", Title: title, Content: "x", Status: "completed",
		Project: project, FeatureID: feature,
	}
	if over != nil {
		over(&req)
	}
	if _, err := brain.Save(context.Background(), req); err != nil {
		t.Fatalf("save %q: %v", title, err)
	}
}

// ensureBuiltInsNoWorkdir registers the built-ins with no target_workdir —
// the default shape when task_defaults.target_workdir is unset, which is
// when the feature-task fallback has to do the work.
func ensureBuiltInsNoWorkdir(t *testing.T, brain *BrainServiceImpl, mergeTarget string) {
	t.Helper()
	ctx := context.Background()
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: mergeTarget,
		ExecutionMode:     "worktree",
	}); err != nil {
		t.Fatalf("ensure AI built-in: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: mergeTarget,
	}); err != nil {
		t.Fatalf("ensure simple built-in: %v", err)
	}
}

func TestAutomationWorkdir_InheritsFromFeatureTasks(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFeatureTask(t, brain, "alpha", "feat-x", "work", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/repos/alpha"
	})
	ensureBuiltInsNoWorkdir(t, brain, "main")

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var gen *types.BrainEntry
	for i := range resp.Entries {
		if strings.HasPrefix(resp.Entries[i].GeneratedBy, "automation:") {
			gen = &resp.Entries[i]
		}
	}
	if gen == nil {
		t.Fatal("no checkout task generated")
	}
	if gen.TargetWorkdir != "/repos/alpha" {
		t.Errorf("target_workdir = %q, want /repos/alpha inherited from the feature's tasks", gen.TargetWorkdir)
	}
}

// Each project's checkout must land in that project's repo, from the same
// global automation.
func TestAutomationWorkdir_DiffersPerProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFeatureTask(t, brain, "alpha", "feat-a", "work", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/repos/alpha"
	})
	saveFeatureTask(t, brain, "beta", "feat-b", "work", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/repos/beta"
	})
	ensureBuiltInsNoWorkdir(t, brain, "main")

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-a", "ai")); err != nil {
		t.Fatalf("HandleEvent alpha: %v", err)
	}
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f2", "beta", "feat-b", "ai")); err != nil {
		t.Fatalf("HandleEvent beta: %v", err)
	}

	for project, want := range map[string]string{"alpha": "/repos/alpha", "beta": "/repos/beta"} {
		resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 20})
		if err != nil {
			t.Fatalf("list %s: %v", project, err)
		}
		found := false
		for _, e := range resp.Entries {
			if !strings.HasPrefix(e.GeneratedBy, "automation:") {
				continue
			}
			found = true
			if e.TargetWorkdir != want {
				t.Errorf("%s checkout target_workdir = %q, want %q", project, e.TargetWorkdir, want)
			}
		}
		if !found {
			t.Errorf("%s: no checkout task generated", project)
		}
	}
}

// An explicit workdir on the automation must still win — inheritance is a
// fallback, not an override.
func TestAutomationWorkdir_ExplicitAutomationValueWins(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFeatureTask(t, brain, "alpha", "feat-x", "work", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/repos/alpha"
	})
	ensureBuiltIns(t, brain, "main") // ensureBuiltIns sets TargetWorkdir=/repo

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, e := range resp.Entries {
		if strings.HasPrefix(e.GeneratedBy, "automation:") && e.TargetWorkdir != "/repo" {
			t.Errorf("target_workdir = %q, want the automation's own /repo", e.TargetWorkdir)
		}
	}
}

// A previous automation's own task must not seed the fallback, or a bad
// value propagates to every later checkout for that feature.
func TestAutomationWorkdir_IgnoresGeneratedTasks(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	generated := true
	saveFeatureTask(t, brain, "alpha", "feat-x", "old automation task", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/tmp"
		r.Generated = &generated
		r.GeneratedBy = "automation:previous"
	})
	saveFeatureTask(t, brain, "alpha", "feat-x", "real work", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/repos/alpha"
	})

	svc := NewAutomationService(brain)
	got := svc.workdirFromFeatureTasks(ctx, "alpha", "feat-x")
	if got != "/repos/alpha" {
		t.Errorf("workdir = %q, want /repos/alpha (generated tasks must be skipped)", got)
	}
}

func TestAutomationWorkdir_EmptyWhenFeatureHasNone(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFeatureTask(t, brain, "alpha", "feat-x", "work", nil)

	svc := NewAutomationService(brain)
	if got := svc.workdirFromFeatureTasks(ctx, "alpha", "feat-x"); got != "" {
		t.Errorf("workdir = %q, want empty so existing defaults apply", got)
	}
}

// workdir is the per-task (possibly worktree) path; target_workdir is the
// repo root. Prefer the repo root, since the checkout merges into it.
func TestAutomationWorkdir_PrefersTargetWorkdirOverWorkdir(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFeatureTask(t, brain, "alpha", "feat-x", "a", func(r *types.CreateEntryRequest) {
		r.Workdir = "/repos/alpha/.worktrees/feat-x"
	})
	saveFeatureTask(t, brain, "alpha", "feat-x", "b", func(r *types.CreateEntryRequest) {
		r.TargetWorkdir = "/repos/alpha"
	})

	svc := NewAutomationService(brain)
	if got := svc.workdirFromFeatureTasks(ctx, "alpha", "feat-x"); got != "/repos/alpha" {
		t.Errorf("workdir = %q, want the repo root /repos/alpha", got)
	}
}

// ─── One feature completion, one checkout ──────────────────────────
//
// Two components emit feature.completed: the server's CheckFeatureCompletion
// (authoritative — reads every task and folds checkout_mode) and the
// runner's feature tracker (a local progress signal that folds nothing).
// Treating the runner's unfolded event as "ai" fired the AI checkout on top
// of the correctly folded one, so a project configured for deterministic
// merges got an LLM agent merging the same feature in parallel.

func runnerFeatureCompletedEvent(id, project, feature string) types.Event {
	return types.Event{
		ID:        id,
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: project,
		FeatureID: feature,
		// The runner tracker emits progress counters, never a fold.
		Metadata: map[string]string{"ready_count": "3"},
	}
}

func TestBuiltInCheckout_RunnerUnfoldedEventDoesNotFireAI(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, runnerFeatureCompletedEvent("r1", "alpha", "feat-x")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 — the runner's unfolded event selected a checkout automation", got)
	}
}

// The whole point: a simple-mode feature must end up with exactly one
// checkout task even though both components emit a completion event.
func TestBuiltInCheckout_SimpleFeatureGetsExactlyOneCheckout(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	// Authoritative, folded event from the server…
	if err := svc.HandleEvent(ctx, featureCompletedEvent("s1", "alpha", "feat-x", "simple")); err != nil {
		t.Fatalf("HandleEvent server: %v", err)
	}
	// …and the runner's unfolded one for the same feature.
	if err := svc.HandleEvent(ctx, runnerFeatureCompletedEvent("r1", "alpha", "feat-x")); err != nil {
		t.Fatalf("HandleEvent runner: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		var titles []string
		for _, e := range resp.Entries {
			titles = append(titles, e.Title)
		}
		t.Fatalf("alpha tasks = %d, want exactly 1 checkout; got %v", len(resp.Entries), titles)
	}
	if resp.Entries[0].Executor != "script" {
		t.Errorf("executor = %q, want script — the AI automation won the race", resp.Entries[0].Executor)
	}
}

// A runner event that DOES carry a fold (belt and braces, should it ever
// gain one) must still be honoured.
func TestBuiltInCheckout_RunnerEventWithExplicitModeStillMatches(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	evt := runnerFeatureCompletedEvent("r1", "alpha", "feat-x")
	evt.Metadata["checkout_mode"] = "simple"
	if err := svc.HandleEvent(ctx, evt); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1", got)
	}
}

// The legacy compatibility path is API-sourced and must keep defaulting to
// AI — that is the case the default was written for.
func TestBuiltInCheckout_APIEventWithoutModeStillDefaultsToAI(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	ensureBuiltIns(t, brain, "main")
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, featureCompletedEvent("a1", "alpha", "feat-x", "")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 (legacy AI default)", got)
	}
}

// Non-checkout automations must be unaffected: a runner-sourced event with
// no checkout_mode filter should still match normally.
func TestAutomation_RunnerEventStillMatchesUnfilteredAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "feature watcher", Content: "x",
		Status: "active", Project: "alpha",
		Trigger: &types.TriggerConfig{Type: "event", Event: types.EventFeatureCompleted},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "note it"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, runnerFeatureCompletedEvent("r1", "alpha", "feat-x")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 — the guard leaked into unrelated automations", got)
	}
}

// ─── Goals are not generic automations ─────────────────────────────
//
// A goal is stored as an automation entry with an event trigger, but it is
// driven by the goal reconcile loop. Letting the generic dispatcher match it
// too produced two tasks per trigger: the reconcile engine's (keyed
// "goal:<id>:<state>" and deduped) plus a bare duplicate with no
// generated_key, which nothing ever deduped.

func saveGoalAutomation(t *testing.T, brain *BrainServiceImpl, project, goalID string) {
	t.Helper()
	if _, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type: "automation", Title: "Goal " + goalID, Content: "x",
		Status: "active", Project: project,
		GeneratedBy: types.GoalGeneratedBy,
		Goal:        &types.GoalConfig{ID: goalID, Criteria: "done"},
		Trigger: &types.TriggerConfig{
			Type: "event", Event: types.EventTaskStatusChanged,
			Filter: map[string]string{"to_status": "in:completed,validated,blocked"},
		},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "reconcile"},
	}); err != nil {
		t.Fatalf("save goal: %v", err)
	}
}

func TestGoalAutomation_NotDispatchedByGenericPath(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveGoalAutomation(t, brain, "alpha", "goal-1")
	svc := NewAutomationService(brain)

	evt := types.Event{
		ID: "e1", Type: types.EventTaskStatusChanged, Source: types.EventSourceAPI,
		ProjectID: "alpha", TaskID: "t1", ToStatus: "completed",
	}
	if err := svc.HandleEvent(ctx, evt); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0 — the generic dispatcher duplicated the goal's work", got)
	}
}

// An entry carrying only a Goal config (no GeneratedBy marker) must also be
// recognised, so hand-written or migrated goals do not regress.
func TestGoalAutomation_RecognisedByConfigAlone(t *testing.T) {
	entry := types.BrainEntry{Goal: &types.GoalConfig{ID: "g"}}
	if !isGoalAutomation(entry) {
		t.Error("entry with a Goal config was not recognised as a goal")
	}
	entry2 := types.BrainEntry{GeneratedBy: types.GoalGeneratedBy}
	if !isGoalAutomation(entry2) {
		t.Error("entry with the goal GeneratedBy marker was not recognised")
	}
}

// Ordinary automations must be unaffected.
func TestGoalAutomation_OrdinaryAutomationStillDispatches(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveProjectAutomation(t, brain, "alpha", "ordinary", nil)
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(ctx, taskCompletedEvent("e1", "alpha")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 1 {
		t.Errorf("alpha tasks = %d, want 1 — the goal guard swallowed a normal automation", got)
	}
}

// A goal in one project must not be reachable from another's events either
// — the guard must not be the only thing keeping them apart.
func TestGoalAutomation_StaysProjectScoped(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saveGoalAutomation(t, brain, "alpha", "goal-1")
	svc := NewAutomationService(brain)

	evt := types.Event{
		ID: "e1", Type: types.EventTaskStatusChanged, Source: types.EventSourceAPI,
		ProjectID: "beta", TaskID: "t1", ToStatus: "completed",
	}
	if err := svc.HandleEvent(ctx, evt); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := generatedTaskCount(t, brain, "alpha"); got != 0 {
		t.Errorf("alpha tasks = %d, want 0", got)
	}
}

// ─── AI built-in: config reconciliation ────────────────────────────
//
// The AI checkout automation's prompt, agent/model, and merge fields are
// all derived from config — the same "stored artifact never reconciled"
// trap that kept the simple script pushless kept this entry's prompt
// pointing at a stale merge target forever.

func TestEnsureBuiltInCheckoutAI_ReconcilesStaleEntryToConfig(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// v1: created with no merge config at all (the shape every preview /
	// fresh install had).
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled: true,
	}); err != nil {
		t.Fatalf("ensure v1: %v", err)
	}
	// v2: config now names a target branch, policy, and model.
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		MergePolicy:       "auto_pr",
		Model:             "openrouter/openai/gpt-4o-mini",
	}); err != nil {
		t.Fatalf("ensure v2: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := false
	for _, e := range resp.Entries {
		if e.GeneratedBy != BuiltInFeatureCheckoutGeneratedBy {
			continue
		}
		seen = true
		if e.Action == nil || !strings.Contains(e.Action.DirectPrompt, "merge request into main") {
			t.Errorf("prompt still targets the stale merge target:\n%s", e.Action.DirectPrompt)
		}
		if e.Action.Model != "openrouter/openai/gpt-4o-mini" {
			t.Errorf("action model = %q, want config's model", e.Action.Model)
		}
		if e.MergePolicy != "auto_pr" {
			t.Errorf("merge_policy = %q, want auto_pr", e.MergePolicy)
		}
		if e.MergeTargetBranch != "main" {
			t.Errorf("merge_target_branch = %q, want main", e.MergeTargetBranch)
		}
	}
	if !seen {
		t.Fatal("AI built-in missing")
	}
}

func TestEnsureBuiltInCheckoutAI_ReconcileDoesNotDuplicate(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
			Enabled:           true,
			MergeTargetBranch: "main",
		}); err != nil {
			t.Fatalf("ensure #%d: %v", i, err)
		}
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	n := 0
	for _, e := range resp.Entries {
		if e.GeneratedBy == BuiltInFeatureCheckoutGeneratedBy {
			n++
		}
	}
	if n != 1 {
		t.Errorf("AI built-ins = %d, want 1", n)
	}
}

// The generated task must carry the reconciled fields — this is the chain
// auto_pr rides on (task.MergePolicy is what the skill reads).
func TestEnsureBuiltInCheckoutAI_ReconciledFieldsReachGeneratedTask(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled: true,
	}); err != nil {
		t.Fatalf("ensure v1: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		MergePolicy:       "auto_pr",
	}); err != nil {
		t.Fatalf("ensure v2: %v", err)
	}

	svc := NewAutomationService(brain)
	if err := svc.HandleEvent(ctx, featureCompletedEvent("f1", "alpha", "feat-x", "ai")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("tasks = %d, want 1", len(resp.Entries))
	}
	task := resp.Entries[0]
	if task.MergePolicy != "auto_pr" {
		t.Errorf("task merge_policy = %q, want auto_pr", task.MergePolicy)
	}
	if !strings.Contains(task.Content, "merge request into main") {
		t.Errorf("task prompt does not carry the reconciled target:\n%s", task.Content)
	}
}
