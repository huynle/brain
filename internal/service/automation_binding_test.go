package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// Pure overlay rules for per-project bindings (automation_binding.go). A
// binding overrides a field only when the field is present, and never takes
// the trigger type, action type, direct prompt or project filter.

func bindingParentFixture() types.BrainEntry {
	maxRuns := 7
	return types.BrainEntry{
		ID:        "parent01",
		Path:      "global/automation/dream.md",
		Type:      "automation",
		Status:    "active",
		Created:   "2026-10-01T00:00:00Z",
		Modified:  "2026-10-02T00:00:00Z",
		StartsAt:  "2026-10-01T00:00:00Z",
		ExpiresAt: "2027-06-30T00:00:00Z",
		MaxRuns:   &maxRuns,
		Timezone:  "UTC",
		Agent:     "tdd-dev",
		Model:     "parent-model",
		Trigger: &types.TriggerConfig{
			Type:     types.TriggerTypeCron,
			Schedule: "0 3 * * *",
			Timezone: "America/New_York",
			Stagger:  "2h",
			CatchUp:  "10m",
			Filter:   map[string]string{"project": "*"},
		},
		Action: &types.AutomationAction{
			Type:          types.AutomationActionPrompt,
			Agent:         "tdd-dev",
			Model:         "parent-model",
			Timeout:       "5m",
			TargetWorkdir: "/parent/workdir",
			DirectPrompt:  "Consolidate memory.",
		},
	}
}

func bindingFixture(mutate func(b *types.BrainEntry)) types.BrainEntry {
	b := types.BrainEntry{
		ID:        "bind0001",
		Path:      "projects/hindsight/automation/dream.md",
		Type:      "automation",
		Status:    "active",
		ProjectID: "hindsight",
		Extends:   "parent01",
		Created:   "2026-10-05T00:00:00Z",
		Modified:  "2026-10-05T00:00:00Z",
	}
	if mutate != nil {
		mutate(&b)
	}
	return b
}

func TestEffectiveAutomation_InheritsEverythingWhenBindingIsEmpty(t *testing.T) {
	parent := bindingParentFixture()
	eff := effectiveAutomation(parent, bindingFixture(nil))

	if eff.Trigger.Schedule != "0 3 * * *" || eff.Trigger.Timezone != "America/New_York" {
		t.Fatalf("timing not inherited: schedule=%q tz=%q", eff.Trigger.Schedule, eff.Trigger.Timezone)
	}
	if eff.Trigger.Stagger != "2h" || eff.Trigger.CatchUp != "10m" {
		t.Fatalf("stagger/catch_up not inherited: %q %q", eff.Trigger.Stagger, eff.Trigger.CatchUp)
	}
	if eff.StartsAt != parent.StartsAt || eff.ExpiresAt != parent.ExpiresAt {
		t.Fatalf("lifecycle not inherited: %q %q", eff.StartsAt, eff.ExpiresAt)
	}
	if eff.MaxRuns == nil || *eff.MaxRuns != 7 {
		t.Fatalf("max_runs not inherited: %v", eff.MaxRuns)
	}
	if eff.Action.Agent != "tdd-dev" || eff.Action.Timeout != "5m" || eff.Action.TargetWorkdir != "/parent/workdir" {
		t.Fatalf("execution not inherited: %+v", eff.Action)
	}
}

func TestEffectiveAutomation_ScheduleReplacesAsAUnit(t *testing.T) {
	parent := bindingParentFixture()
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Trigger = &types.TriggerConfig{Every: "4d", At: "01:00"}
	}))

	if eff.Trigger.Schedule != "" {
		t.Fatalf("parent cron schedule must be replaced, got %q", eff.Trigger.Schedule)
	}
	if eff.Trigger.Every != "4d" || eff.Trigger.At != "01:00" {
		t.Fatalf("binding every/at not applied: every=%q at=%q", eff.Trigger.Every, eff.Trigger.At)
	}
	// The anchor follows the schedule the binding defines.
	if eff.Created != "2026-10-05T00:00:00Z" {
		t.Fatalf("created = %q, want the binding's created instant as the every anchor", eff.Created)
	}
	if eff.Trigger.Stagger != "2h" {
		t.Fatalf("stagger is independent of timing and must still inherit, got %q", eff.Trigger.Stagger)
	}
}

func TestEffectiveAutomation_CronScheduleReplacesParentEvery(t *testing.T) {
	parent := bindingParentFixture()
	parent.Trigger = &types.TriggerConfig{Type: types.TriggerTypeCron, Every: "1d", At: "09:00"}
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Trigger = &types.TriggerConfig{Schedule: "30 6 * * 1-5"}
	}))

	if eff.Trigger.Every != "" || eff.Trigger.At != "" {
		t.Fatalf("parent every/at must be cleared by a binding schedule, got every=%q at=%q", eff.Trigger.Every, eff.Trigger.At)
	}
	if eff.Trigger.Schedule != "30 6 * * 1-5" {
		t.Fatalf("schedule = %q", eff.Trigger.Schedule)
	}
}

func TestEffectiveAutomation_TimingFieldsOverrideIndividually(t *testing.T) {
	parent := bindingParentFixture()
	skip := &types.CalendarEventFilter{Title: "holiday"}
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Trigger = &types.TriggerConfig{Timezone: "Europe/Paris", Stagger: "0s", CatchUp: "none", Calendar: "team", SkipIfEvent: skip}
	}))

	if eff.Trigger.Timezone != "Europe/Paris" {
		t.Fatalf("timezone = %q", eff.Trigger.Timezone)
	}
	if eff.Trigger.Stagger != "0s" {
		t.Fatalf("explicit 0s stagger must override, got %q", eff.Trigger.Stagger)
	}
	if eff.Trigger.CatchUp != "none" {
		t.Fatalf("explicit catch_up none must override, got %q", eff.Trigger.CatchUp)
	}
	if eff.Trigger.Calendar != "team" || eff.Trigger.SkipIfEvent == nil || eff.Trigger.SkipIfEvent.Title != "holiday" {
		t.Fatalf("calendar/skip_if_event not applied: %+v", eff.Trigger)
	}
	if eff.Trigger.Schedule != "0 3 * * *" {
		t.Fatalf("a timing-field override must not drop the inherited schedule, got %q", eff.Trigger.Schedule)
	}
	if eff.Trigger.OnlyIfEvent != nil {
		t.Fatalf("an absent only_if_event must inherit nil, got %+v", eff.Trigger.OnlyIfEvent)
	}
}

func TestEffectiveAutomation_ExecutionFieldsOverrideTopLevelAndAction(t *testing.T) {
	parent := bindingParentFixture()
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Action = &types.AutomationAction{Agent: "explore", Model: "bind-model", Timeout: "20m", TargetWorkdir: "/bind/workdir", ExecutionMode: "current_branch", Executor: "pi"}
	}))

	if eff.Agent != "explore" || eff.Action.Agent != "explore" {
		t.Fatalf("agent: top=%q action=%q, want both explore", eff.Agent, eff.Action.Agent)
	}
	if eff.Model != "bind-model" || eff.Action.Model != "bind-model" {
		t.Fatalf("model: top=%q action=%q", eff.Model, eff.Action.Model)
	}
	if eff.Action.Timeout != "20m" {
		t.Fatalf("timeout = %q", eff.Action.Timeout)
	}
	if eff.TargetWorkdir != "/bind/workdir" || eff.Action.TargetWorkdir != "/bind/workdir" {
		t.Fatalf("target_workdir: top=%q action=%q", eff.TargetWorkdir, eff.Action.TargetWorkdir)
	}
	if eff.ExecutionMode != "current_branch" || eff.Action.ExecutionMode != "current_branch" {
		t.Fatalf("execution_mode: top=%q action=%q", eff.ExecutionMode, eff.Action.ExecutionMode)
	}
	if eff.Executor != "pi" || eff.Action.Executor != "pi" {
		t.Fatalf("executor: top=%q action=%q", eff.Executor, eff.Action.Executor)
	}
}

func TestEffectiveAutomation_TopLevelBindingFieldWinsOverParentAction(t *testing.T) {
	// The scheduler reads firstNonEmpty(entry.X, action.X). A binding that
	// sets only the top-level agent must still beat the parent's action agent.
	parent := bindingParentFixture()
	parent.Agent = ""
	parent.Action.Agent = "parent-action-agent"
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Agent = "bind-agent"
	}))

	if got := firstNonEmpty(eff.Agent, eff.Action.Agent); got != "bind-agent" {
		t.Fatalf("scheduler would resolve agent %q, want bind-agent", got)
	}
}

func TestEffectiveAutomation_LifecycleOverrides(t *testing.T) {
	parent := bindingParentFixture()

	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.StartsAt = "2026-11-01T00:00:00Z"
		b.ExpiresAt = "2026-12-01T00:00:00Z"
		n := 3
		b.MaxRuns = &n
	}))
	if eff.StartsAt != "2026-11-01T00:00:00Z" || eff.ExpiresAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("starts/expires not overridden: %q %q", eff.StartsAt, eff.ExpiresAt)
	}
	if eff.MaxRuns == nil || *eff.MaxRuns != 3 {
		t.Fatalf("max_runs = %v, want 3", eff.MaxRuns)
	}
}

func TestEffectiveAutomation_MaxRunsZeroInheritsAndMinusOneIsUnlimited(t *testing.T) {
	parent := bindingParentFixture()

	zero := 0
	inheritsZero := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) { b.MaxRuns = &zero }))
	if inheritsZero.MaxRuns == nil || *inheritsZero.MaxRuns != 7 {
		t.Fatalf("max_runs 0 must inherit the parent's 7, got %v", inheritsZero.MaxRuns)
	}

	unlimited := -1
	unlimitedEff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) { b.MaxRuns = &unlimited }))
	if unlimitedEff.MaxRuns == nil || *unlimitedEff.MaxRuns != -1 {
		t.Fatalf("max_runs -1 must mean explicit unlimited, got %v", unlimitedEff.MaxRuns)
	}
}

func TestEffectiveAutomation_PromptAppendAfterBlankLine(t *testing.T) {
	parent := bindingParentFixture()
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Action = &types.AutomationAction{PromptAppend: "Weight ingestion decisions."}
	}))

	want := "Consolidate memory.\n\nWeight ingestion decisions."
	if eff.Action.DirectPrompt != want {
		t.Fatalf("action prompt = %q, want %q", eff.Action.DirectPrompt, want)
	}
	if eff.DirectPrompt != want {
		t.Fatalf("top-level prompt = %q, want %q", eff.DirectPrompt, want)
	}
}

func TestEffectiveAutomation_PromptAppendOnEmptyParentPromptIsTheWholePrompt(t *testing.T) {
	parent := bindingParentFixture()
	parent.Action.DirectPrompt = ""
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Action = &types.AutomationAction{PromptAppend: "Only this."}
	}))
	if eff.Action.DirectPrompt != "Only this." {
		t.Fatalf("prompt = %q, want just the appended text", eff.Action.DirectPrompt)
	}
}

func TestEffectiveAutomation_NeverTakesTypeFilterOrPromptFromBinding(t *testing.T) {
	parent := bindingParentFixture()
	eff := effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Trigger = &types.TriggerConfig{Type: types.TriggerTypeEvent, Filter: map[string]string{"project": "hindsight"}}
		b.Action = &types.AutomationAction{Type: types.AutomationActionScript, DirectPrompt: "rogue prompt", Command: "rm -rf /"}
	}))

	if eff.Trigger.Type != types.TriggerTypeCron {
		t.Fatalf("trigger.type changed to %q", eff.Trigger.Type)
	}
	if eff.Action.Type != types.AutomationActionPrompt {
		t.Fatalf("action.type changed to %q", eff.Action.Type)
	}
	if eff.Action.DirectPrompt != "Consolidate memory." {
		t.Fatalf("direct_prompt replaced by binding: %q", eff.Action.DirectPrompt)
	}
	if eff.Trigger.Filter["project"] != "*" {
		t.Fatalf("filter.project changed to %q", eff.Trigger.Filter["project"])
	}
	if eff.Action.Command != "" {
		t.Fatalf("command taken from binding: %q", eff.Action.Command)
	}
}

func TestEffectiveAutomation_IdentityAndFloorBelongToTheParent(t *testing.T) {
	parent := bindingParentFixture()
	binding := bindingFixture(func(b *types.BrainEntry) { b.Modified = "2026-10-08T00:00:00Z" })
	eff := effectiveAutomation(parent, binding)

	if eff.ID != "parent01" {
		t.Fatalf("effective ID = %q: history must stay keyed to the parent", eff.ID)
	}
	if eff.ProjectID != "hindsight" || eff.Binding != "bind0001" {
		t.Fatalf("project/binding = %q/%q", eff.ProjectID, eff.Binding)
	}
	if eff.Extends != "" {
		t.Fatalf("effective entry must not claim to be a binding, extends=%q", eff.Extends)
	}
	// The floor reads Modified: the later of parent and binding wins.
	if eff.Modified != "2026-10-08T00:00:00Z" {
		t.Fatalf("modified = %q, want the binding's later write", eff.Modified)
	}
}

func TestEffectiveAutomation_DoesNotMutateTheParent(t *testing.T) {
	parent := bindingParentFixture()
	_ = effectiveAutomation(parent, bindingFixture(func(b *types.BrainEntry) {
		b.Trigger = &types.TriggerConfig{Every: "2d", At: "01:00", Filter: map[string]string{"project": "x"}}
		b.Action = &types.AutomationAction{Agent: "explore", PromptAppend: "more"}
	}))

	if parent.Trigger.Schedule != "0 3 * * *" || parent.Trigger.Every != "" {
		t.Fatalf("parent trigger mutated: %+v", parent.Trigger)
	}
	if parent.Action.Agent != "tdd-dev" || parent.Action.DirectPrompt != "Consolidate memory." {
		t.Fatalf("parent action mutated: %+v", parent.Action)
	}
	if parent.Trigger.Filter["project"] != "*" {
		t.Fatalf("parent filter mutated: %v", parent.Trigger.Filter)
	}
}

func TestSyncExtendsTag_ReplacesStaleAndDropsEmpty(t *testing.T) {
	got := syncExtendsTag([]string{"automation", "extends:old0001", "keep"}, "parent01")
	want := []string{"automation", "keep", "extends:parent01"}
	if !sameTags(got, want) {
		t.Fatalf("sync = %v, want %v", got, want)
	}
	got = syncExtendsTag([]string{"automation", "extends:parent01"}, "")
	if !sameTags(got, []string{"automation"}) {
		t.Fatalf("clearing extends must drop the tag, got %v", got)
	}
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
