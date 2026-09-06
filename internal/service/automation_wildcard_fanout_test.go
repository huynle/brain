package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// The built-in Dream Consolidation entry is a GLOBAL cron automation carrying
// `filter.project: "*"`. That wildcard was only ever read on the event path,
// so on the cron path it produced exactly one task with an empty project —
// filed under `default`, with `{{.Project}}` rendered as the empty string.
// These tests pin the wildcard to the meaning it reads like on both the
// scheduled and the manual path, and pin the pause gate to firing only for an
// automation that was actually due.

type stubProjectLister struct {
	projects []string
	err      error
	calls    int
}

func (s *stubProjectLister) ListProjects(ctx context.Context) ([]string, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.projects, nil
}

func saveWildcardCronAutomation(t *testing.T, brain *BrainServiceImpl, schedule string) *types.CreateEntryResponse {
	t.Helper()
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Dream Consolidation",
		Content: "global wildcard cron automation",
		Status:  "active",
		// Global, like the built-in: Save files an empty project under
		// "default", so the flag is what makes an entry projectless.
		Global: serviceBoolPtr(true),
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: schedule,
			Filter:   map[string]string{"project": "*"},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Consolidate knowledge in project {{.Project}}.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation: %v", err)
	}
	return resp
}

func generatedTasksFor(t *testing.T, brain *BrainServiceImpl, project, automationID string) []types.BrainEntry {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("List tasks for %q: %v", project, err)
	}
	out := make([]types.BrainEntry, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		if e.GeneratedBy == "automation:"+automationID {
			out = append(out, e)
		}
	}
	return out
}

func TestCheckScheduled_WildcardCronFansOutPerProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveWildcardCronAutomation(t, brain, "* * * * *")

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{"hindsight", "brain-api"}})

	now := time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)
	if err := svc.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	for _, project := range []string{"hindsight", "brain-api"} {
		tasks := generatedTasksFor(t, brain, project, auto.ID)
		if len(tasks) != 1 {
			t.Fatalf("project %q: got %d generated tasks, want 1", project, len(tasks))
		}
		// The whole point: the prompt names the project, and the task is
		// filed under it. Both were empty before the fan-out existed.
		want := fmt.Sprintf("Consolidate knowledge in project %s.", project)
		if tasks[0].DirectPrompt != want {
			t.Errorf("project %q: prompt = %q, want %q", project, tasks[0].DirectPrompt, want)
		}
		if !strings.Contains(tasks[0].GeneratedKey, ":"+project+":") {
			t.Errorf("project %q: dedup key %q does not name the project",
				project, tasks[0].GeneratedKey)
		}
	}

	// Nothing lands in the catch-all project any more.
	if tasks := generatedTasksFor(t, brain, "default", auto.ID); len(tasks) != 0 {
		t.Errorf("default: got %d generated tasks, want 0", len(tasks))
	}
}

func TestCheckScheduled_WildcardFanOutIsDedupedPerProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveWildcardCronAutomation(t, brain, "* * * * *")

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{"hindsight", "brain-api"}})

	now := time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := svc.CheckScheduled(ctx, now); err != nil {
			t.Fatalf("CheckScheduled run %d: %v", i, err)
		}
	}

	for _, project := range []string{"hindsight", "brain-api"} {
		if tasks := generatedTasksFor(t, brain, project, auto.ID); len(tasks) != 1 {
			t.Errorf("project %q: got %d tasks after two ticks of the same minute, want 1",
				project, len(tasks))
		}
	}
}

func TestCheckScheduled_ProjectScopedAutomationIgnoresWildcard(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	// A wildcard filter on an entry that OWNS a project must not widen it.
	resp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Scoped",
		Content: "x",
		Status:  "active",
		Project: "hindsight",
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: "* * * * *",
			Filter:   map[string]string{"project": "*"},
		},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "for {{.Project}}"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	lister := &stubProjectLister{projects: []string{"hindsight", "brain-api", "pwa"}}
	svc := NewAutomationService(brain)
	svc.SetProjectLister(lister)

	if err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	if lister.calls != 0 {
		t.Errorf("project lister consulted %d times for a project-scoped automation, want 0", lister.calls)
	}
	if tasks := generatedTasksFor(t, brain, "brain-api", resp.ID); len(tasks) != 0 {
		t.Errorf("brain-api: got %d tasks, want 0 — a scoped automation must not fan out", len(tasks))
	}
	if tasks := generatedTasksFor(t, brain, "hindsight", resp.ID); len(tasks) != 1 {
		t.Errorf("hindsight: got %d tasks, want 1", len(tasks))
	}
}

func TestCheckScheduled_GlobalAutomationWithoutWildcardKeepsSingleRun(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	resp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Global, no wildcard",
		Content: "x",
		Status:  "active",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "* * * * *"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "x"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	lister := &stubProjectLister{projects: []string{"hindsight", "brain-api"}}
	svc := NewAutomationService(brain)
	svc.SetProjectLister(lister)

	if err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	if lister.calls != 0 {
		t.Errorf("project lister consulted %d times without a wildcard, want 0", lister.calls)
	}
	if tasks := generatedTasksFor(t, brain, "default", resp.ID); len(tasks) != 1 {
		t.Errorf("default: got %d tasks, want the historical single unscoped run", len(tasks))
	}
}

// A wildcard automation with no lister wired must say so rather than silently
// falling back to the empty-project run that was the bug in the first place.
func TestCheckScheduled_WildcardWithoutListerErrors(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveWildcardCronAutomation(t, brain, "* * * * *")

	svc := NewAutomationService(brain)
	err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "no project lister") {
		t.Fatalf("err = %v, want a wired-lister complaint", err)
	}
	if tasks := generatedTasksFor(t, brain, "default", auto.ID); len(tasks) != 0 {
		t.Errorf("default: got %d tasks, want 0 — an unresolvable fan-out must generate nothing", len(tasks))
	}
}

// The pause gate used to run BEFORE the schedule match, so a paused cron
// automation wrote a "skipped: paused" audit on every one-minute tick whether
// or not it was due — burying every real run in the history.
func TestCheckScheduled_PausedAutomationIsNotAuditedWhenNotDue(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	// Due at 03:00 only.
	auto := saveWildcardCronAutomation(t, brain, "0 3 * * *")

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{"hindsight"}})
	svc.SetPauseChecker(&fakeAutomationPauseChecker{paused: true})

	// Ten ticks across a window where the cron is never due.
	for i := 0; i < 10; i++ {
		at := time.Date(2026, 9, 6, 11, 20+i, 8, 0, time.UTC)
		if err := svc.CheckScheduled(ctx, at); err != nil {
			t.Fatalf("CheckScheduled at %s: %v", at, err)
		}
	}

	runs := automationRunAudits(t, brain, auto.ID)
	if len(runs) != 0 {
		t.Fatalf("got %d run audits for a paused automation that was never due, want 0", len(runs))
	}

	// When it IS due, the paused skip is still recorded — that is the case
	// where "it was paused" actually explains something.
	if err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 8, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled when due: %v", err)
	}
	runs = automationRunAudits(t, brain, auto.ID)
	if len(runs) != 1 {
		t.Fatalf("got %d run audits when due and paused, want 1", len(runs))
	}
	if !strings.Contains(runs[0].Content, "skip_reason: paused") {
		t.Errorf("audit does not record the pause: %q", runs[0].Content)
	}
	// The audit is scoped to the project the run was for, not to `default`.
	if !strings.Contains(runs[0].Content, "project: hindsight") {
		t.Errorf("audit is not scoped to the fanned-out project: %q", runs[0].Content)
	}
}

func automationRunAudits(t *testing.T, brain *BrainServiceImpl, automationID string) []types.BrainEntry {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type:  "automation_run",
		Limit: 500,
	})
	if err != nil {
		t.Fatalf("List automation runs: %v", err)
	}
	out := make([]types.BrainEntry, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		if strings.Contains(e.Content, "automation_id: "+automationID) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

// "Run now" from a project's Automations tab must run the global automation
// FOR that project — the PWA merges global entries into every project's list,
// so the project the user was looking at is the only scope the run has.
func TestRunAutomationNow_ScopesGlobalAutomationToTheGivenProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveWildcardCronAutomation(t, brain, "0 3 * * *")

	svc := NewAutomationService(brain)
	lister := &stubProjectLister{projects: []string{"hindsight", "brain-api"}}
	svc.SetProjectLister(lister)

	ids, err := svc.RunAutomationNow(ctx, auto.Path, "hindsight")
	if err != nil {
		t.Fatalf("RunAutomationNow: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d task ids, want exactly the one project asked for: %v", len(ids), ids)
	}
	if lister.calls != 0 {
		t.Errorf("project lister consulted %d times for an explicitly scoped run, want 0", lister.calls)
	}

	task, err := brain.Recall(ctx, ids[0])
	if err != nil {
		t.Fatalf("recall task: %v", err)
	}
	if task.ProjectID != "hindsight" {
		t.Errorf("task project = %q, want hindsight", task.ProjectID)
	}
	if task.DirectPrompt != "Consolidate knowledge in project hindsight." {
		t.Errorf("prompt = %q, want the project rendered", task.DirectPrompt)
	}
	if tasks := generatedTasksFor(t, brain, "brain-api", auto.ID); len(tasks) != 0 {
		t.Errorf("brain-api: got %d tasks, want 0", len(tasks))
	}
}

func TestRunAutomationNow_UnscopedWildcardRunFansOut(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveWildcardCronAutomation(t, brain, "0 3 * * *")

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{"hindsight", "brain-api"}})

	ids, err := svc.RunAutomationNow(ctx, auto.Path, "")
	if err != nil {
		t.Fatalf("RunAutomationNow: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d task ids, want one per project: %v", len(ids), ids)
	}
	for _, project := range []string{"hindsight", "brain-api"} {
		if tasks := generatedTasksFor(t, brain, project, auto.ID); len(tasks) != 1 {
			t.Errorf("project %q: got %d tasks, want 1", project, len(tasks))
		}
	}
}

// A caller-supplied project must never override an automation that owns one:
// that would let a manual run write into a project the automation is not for.
func TestRunAutomationNow_EntryProjectWinsOverCallerProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveAutomation(t, brain, &types.AutomationAction{
		Type:         "prompt",
		DirectPrompt: "for {{.Project}}",
	})

	svc := NewAutomationService(brain)
	ids, err := svc.RunAutomationNow(ctx, auto.Path, "some-other-project")
	if err != nil {
		t.Fatalf("RunAutomationNow: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d ids, want 1", len(ids))
	}
	task, err := brain.Recall(ctx, ids[0])
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if task.ProjectID != "manual-run-test" {
		t.Errorf("task project = %q, want the automation's own project", task.ProjectID)
	}
}

// The trigger's project filter is a SELECTOR, matched with the same
// types.MatchFilterValue the event path uses. "*" is one expression among
// several — `in:a,b,c` narrows a global cron automation to those projects,
// which is how dream consolidation is limited to the projects worth dreaming
// about without splitting it into one entry per project.
func saveFilteredCronAutomation(t *testing.T, brain *BrainServiceImpl, expr string) *types.CreateEntryResponse {
	t.Helper()
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Dream Consolidation",
		Content: "global cron automation with a project selector",
		Status:  "active",
		Global:  serviceBoolPtr(true),
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: "* * * * *",
			Filter:   map[string]string{"project": expr},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Consolidate knowledge in project {{.Project}}.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation: %v", err)
	}
	return resp
}

func TestCheckScheduled_ProjectFilterNarrowsTheFanOut(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveFilteredCronAutomation(t, brain, "in:hindsight,supernote,brain-api")

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{
		"hindsight", "supernote", "brain-api", "hindsight-v2", "pwa", "default",
	}})

	if err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	for _, project := range []string{"hindsight", "supernote", "brain-api"} {
		if tasks := generatedTasksFor(t, brain, project, auto.ID); len(tasks) != 1 {
			t.Errorf("selected project %q: got %d tasks, want 1", project, len(tasks))
		}
	}
	// hindsight-v2 is a DIFFERENT project from hindsight: `in:` matches whole
	// elements, never prefixes, so a near-miss name must not be swept in.
	for _, project := range []string{"hindsight-v2", "pwa", "default"} {
		if tasks := generatedTasksFor(t, brain, project, auto.ID); len(tasks) != 0 {
			t.Errorf("unselected project %q: got %d tasks, want 0", project, len(tasks))
		}
	}
}

func TestCheckScheduled_SingleProjectFilterSelectsOne(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveFilteredCronAutomation(t, brain, "hindsight")

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{"hindsight", "pwa"}})

	if err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	if tasks := generatedTasksFor(t, brain, "hindsight", auto.ID); len(tasks) != 1 {
		t.Errorf("hindsight: got %d tasks, want 1", len(tasks))
	}
	if tasks := generatedTasksFor(t, brain, "pwa", auto.ID); len(tasks) != 0 {
		t.Errorf("pwa: got %d tasks, want 0", len(tasks))
	}
}

// An empty selector must not read as "every project" — that is the difference
// between "I named nothing" and "I named everything".
func TestCheckScheduled_EmptyProjectFilterIsNotAWildcard(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveFilteredCronAutomation(t, brain, "")

	lister := &stubProjectLister{projects: []string{"hindsight", "pwa"}}
	svc := NewAutomationService(brain)
	svc.SetProjectLister(lister)

	if err := svc.CheckScheduled(ctx, time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	if lister.calls != 0 {
		t.Errorf("project lister consulted %d times for an empty selector, want 0", lister.calls)
	}
	for _, project := range []string{"hindsight", "pwa"} {
		if tasks := generatedTasksFor(t, brain, project, auto.ID); len(tasks) != 0 {
			t.Errorf("%s: got %d tasks, want 0", project, len(tasks))
		}
	}
	if tasks := generatedTasksFor(t, brain, "default", auto.ID); len(tasks) != 1 {
		t.Errorf("default: got %d tasks, want the historical single unscoped run", len(tasks))
	}
}

// The same selector must mean the same thing on the EVENT path. A global
// automation filtered to `in:a,b` used to be rejected by
// globalAutomationMatchesProjectEvent (which demanded a literal "*") and so
// matched nothing at all, one step before matchAutomationFilters would have
// matched a and b correctly.
func TestHandleEvent_GlobalAutomationWithInProjectFilterMatches(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	resp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Global, in: filter",
		Content: "x",
		Status:  "active",
		Global:  serviceBoolPtr(true),
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Events: []string{"task.completed"},
			Filter: map[string]string{"project": "in:hindsight,supernote"},
		},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "for {{.Project}}"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	svc := NewAutomationService(brain)
	for _, project := range []string{"hindsight", "supernote", "pwa"} {
		if err := svc.HandleEvent(ctx, types.Event{
			Type:      "task.completed",
			Source:    "api",
			ProjectID: project,
			TaskID:    "t-" + project,
		}); err != nil {
			t.Fatalf("HandleEvent for %s: %v", project, err)
		}
	}

	for _, project := range []string{"hindsight", "supernote"} {
		if tasks := generatedTasksFor(t, brain, project, resp.ID); len(tasks) != 1 {
			t.Errorf("selected project %q: got %d tasks, want 1", project, len(tasks))
		}
	}
	if tasks := generatedTasksFor(t, brain, "pwa", resp.ID); len(tasks) != 0 {
		t.Errorf("unselected project pwa: got %d tasks, want 0", len(tasks))
	}
}
