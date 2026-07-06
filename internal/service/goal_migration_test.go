package service

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// fullPlanBody mirrors the legacy buildPlanContent output with all sections
// populated, including a trigger metadata line.
const fullPlanBody = `# Goal: Ship OAuth login

## Desired Outcome

Ship OAuth login

## Acceptance Criteria

- All auth tasks complete
- Login works end to end

## Validation Commands

- ` + "`go test ./...`" + `
- ` + "`go build ./...`" + `

## Execution Metadata

- project: brain-api
- agent: tdd-dev
- model: anthropic/claude-sonnet-4
- executor: pi
- target_workdir: /work/brain-api
- feature_id: auth-system
- schedule: 
- goal_session_mode: fresh
- trigger: task.completed

## Reconciliation Notes

Reconcile until criteria met.
`

func fullPlanEntry() types.BrainEntry {
	return types.BrainEntry{
		Title:         "Ship OAuth login",
		Type:          "plan",
		Status:        "active",
		Content:       fullPlanBody,
		Tags:          []string{"goal", "goal:v1", "goal:plan"},
		ProjectID:     "brain-api",
		FeatureID:     "auth-system",
		Agent:         "tdd-dev",
		Model:         "anthropic/claude-sonnet-4",
		Executor:      "pi",
		TargetWorkdir: "/work/brain-api",
		Schedule:      "",
		GeneratedKey:  "goal:ship-oauth-login:plan",
		GeneratedBy:   "brain-goal",
	}
}

func fullReconcilerEntry() *types.BrainEntry {
	idle := true
	return &types.BrainEntry{
		Title:          "Goal Reconcile: Ship OAuth login",
		Type:           "task",
		Status:         "pending",
		Tags:           []string{"goal", "goal:v1", "goal:reconciler"},
		ProjectID:      "brain-api",
		FeatureID:      "auth-system",
		DirectPrompt:   "Reconcile the OAuth goal until done.",
		Agent:          "tdd-dev",
		Model:          "anthropic/claude-sonnet-4",
		Executor:       "pi",
		TargetWorkdir:  "/work/brain-api",
		CompleteOnIdle: &idle,
		GeneratedKey:   "goal:ship-oauth-login:reconcile",
		GeneratedBy:    "brain-goal",
	}
}

func TestLegacyGoalToInput_FullPlanWithReconciler(t *testing.T) {
	in, err := LegacyGoalToInput(fullPlanEntry(), fullReconcilerEntry())
	if err != nil {
		t.Fatalf("LegacyGoalToInput returned error: %v", err)
	}

	if in.Config.ID != "ship-oauth-login" {
		t.Errorf("Config.ID = %q, want ship-oauth-login", in.Config.ID)
	}
	if in.Project != "brain-api" {
		t.Errorf("Project = %q, want brain-api", in.Project)
	}
	if in.FeatureID != "auth-system" {
		t.Errorf("FeatureID = %q, want auth-system", in.FeatureID)
	}
	if in.Title != "Ship OAuth login" {
		t.Errorf("Title = %q, want Ship OAuth login", in.Title)
	}

	wantCriteria := "All auth tasks complete\nLogin works end to end"
	if in.Config.Criteria != wantCriteria {
		t.Errorf("Config.Criteria = %q, want %q", in.Config.Criteria, wantCriteria)
	}

	wantValidation := "go test ./...\ngo build ./..."
	if in.Config.Validation != wantValidation {
		t.Errorf("Config.Validation = %q, want %q", in.Config.Validation, wantValidation)
	}

	if in.Config.Workdir != "/work/brain-api" {
		t.Errorf("Config.Workdir = %q, want /work/brain-api", in.Config.Workdir)
	}

	if in.Action.Type != "prompt" {
		t.Errorf("Action.Type = %q, want prompt", in.Action.Type)
	}
	if in.Action.DirectPrompt != "Reconcile the OAuth goal until done." {
		t.Errorf("Action.DirectPrompt = %q, want reconciler prompt", in.Action.DirectPrompt)
	}
	if in.Action.Agent != "tdd-dev" {
		t.Errorf("Action.Agent = %q, want tdd-dev", in.Action.Agent)
	}
	if in.Action.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("Action.Model = %q, want anthropic/claude-sonnet-4", in.Action.Model)
	}
	if in.Action.SessionMode != "fresh" {
		t.Errorf("Action.SessionMode = %q, want fresh", in.Action.SessionMode)
	}
	if in.Action.CompleteOnIdle == nil || !*in.Action.CompleteOnIdle {
		t.Errorf("Action.CompleteOnIdle = %v, want true", in.Action.CompleteOnIdle)
	}
}

func TestLegacyGoalToInput_TriggerSourceTask(t *testing.T) {
	in, err := LegacyGoalToInput(fullPlanEntry(), fullReconcilerEntry())
	if err != nil {
		t.Fatalf("LegacyGoalToInput returned error: %v", err)
	}
	if in.Config.TriggerSource != types.GoalTriggerSourceTask {
		t.Errorf("Config.TriggerSource = %q, want %q", in.Config.TriggerSource, types.GoalTriggerSourceTask)
	}
}

func TestLegacyGoalToInput_NoTriggerMetadata(t *testing.T) {
	plan := fullPlanEntry()
	// Strip the trigger line from the body.
	plan.Content = strings.Replace(plan.Content, "- trigger: task.completed\n", "", 1)
	in, err := LegacyGoalToInput(plan, fullReconcilerEntry())
	if err != nil {
		t.Fatalf("LegacyGoalToInput returned error: %v", err)
	}
	if in.Config.TriggerSource != "" {
		t.Errorf("Config.TriggerSource = %q, want empty (defaults to both)", in.Config.TriggerSource)
	}
}

func TestLegacyGoalToInput_NilReconciler(t *testing.T) {
	in, err := LegacyGoalToInput(fullPlanEntry(), nil)
	if err != nil {
		t.Fatalf("LegacyGoalToInput returned error: %v", err)
	}
	// DirectPrompt falls back to a non-empty value derived from plan content/criteria.
	if strings.TrimSpace(in.Action.DirectPrompt) == "" {
		t.Error("Action.DirectPrompt is empty, want fallback prompt")
	}
	// Agent/Model come from plan fields when reconciler is absent.
	if in.Action.Agent != "tdd-dev" {
		t.Errorf("Action.Agent = %q, want tdd-dev (from plan)", in.Action.Agent)
	}
	if in.Action.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("Action.Model = %q, want from plan", in.Action.Model)
	}
	// SessionMode still parsed from body.
	if in.Action.SessionMode != "fresh" {
		t.Errorf("Action.SessionMode = %q, want fresh", in.Action.SessionMode)
	}
}

func TestLegacyGoalToInput_PlaceholderSections(t *testing.T) {
	body := `# Goal: Placeholder goal

## Desired Outcome

Placeholder goal

## Acceptance Criteria

- Define measurable success criteria.

## Validation Commands

_No validation commands configured._

## Execution Metadata

- project: brain-api
- agent: tdd-dev
- goal_session_mode: continue
`
	plan := types.BrainEntry{
		Title:        "Placeholder goal",
		Type:         "plan",
		Content:      body,
		Tags:         []string{"goal", "goal:plan"},
		ProjectID:    "brain-api",
		GeneratedKey: "goal:placeholder-goal:plan",
		GeneratedBy:  "brain-goal",
	}
	in, err := LegacyGoalToInput(plan, nil)
	if err != nil {
		t.Fatalf("LegacyGoalToInput returned error: %v", err)
	}
	// Placeholder criteria is skipped; with no real bullets it falls back to Content.
	if in.Config.Criteria != strings.TrimSpace(body) {
		// Acceptable: criteria equals content fallback. Just ensure the
		// placeholder text itself is not the sole criteria value.
		if in.Config.Criteria == "Define measurable success criteria." {
			t.Errorf("Config.Criteria should skip placeholder, got %q", in.Config.Criteria)
		}
	}
	// Placeholder validation is skipped → empty.
	if in.Config.Validation != "" {
		t.Errorf("Config.Validation = %q, want empty (placeholder skipped)", in.Config.Validation)
	}
}

func TestLegacyGoalToInput_IDDerivation(t *testing.T) {
	tests := []struct {
		name         string
		generatedKey string
		title        string
		wantID       string
	}{
		{"from generated key", "goal:my-goal:plan", "My Goal", "my-goal"},
		{"empty key slugifies title", "", "My Cool Goal!", "my-cool-goal"},
		{"key with multiple colons in slug", "goal:a-b-c:plan", "A B C", "a-b-c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := types.BrainEntry{
				Title:        tt.title,
				Type:         "plan",
				Content:      "# Goal: " + tt.title,
				Tags:         []string{"goal", "goal:plan"},
				ProjectID:    "brain-api",
				GeneratedKey: tt.generatedKey,
				GeneratedBy:  "brain-goal",
			}
			in, err := LegacyGoalToInput(plan, nil)
			if err != nil {
				t.Fatalf("LegacyGoalToInput returned error: %v", err)
			}
			if in.Config.ID != tt.wantID {
				t.Errorf("Config.ID = %q, want %q", in.Config.ID, tt.wantID)
			}
		})
	}
}

func TestLegacyGoalToInput_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		plan types.BrainEntry
	}{
		{
			name: "wrong type and no goal:plan tag",
			plan: types.BrainEntry{
				Title:     "Not a plan",
				Type:      "task",
				Tags:      []string{"goal", "goal:reconciler"},
				ProjectID: "brain-api",
			},
		},
		{
			name: "empty title",
			plan: types.BrainEntry{
				Title:     "",
				Type:      "plan",
				Tags:      []string{"goal", "goal:plan"},
				ProjectID: "brain-api",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LegacyGoalToInput(tt.plan, nil); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestLegacyGoalToInput_AcceptsGoalPlanTagWithoutPlanType(t *testing.T) {
	// A plan identified by the goal:plan tag even if Type isn't exactly "plan".
	plan := types.BrainEntry{
		Title:        "Tag-only plan",
		Type:         "",
		Content:      "# Goal: Tag-only plan",
		Tags:         []string{"goal", "goal:v1", "goal:plan"},
		ProjectID:    "brain-api",
		GeneratedKey: "goal:tag-only-plan:plan",
		GeneratedBy:  "brain-goal",
	}
	if _, err := LegacyGoalToInput(plan, nil); err != nil {
		t.Errorf("expected goal:plan tag to be accepted, got error: %v", err)
	}
}

func TestLegacyGoalToInput_EndToEndBuildable(t *testing.T) {
	in, err := LegacyGoalToInput(fullPlanEntry(), fullReconcilerEntry())
	if err != nil {
		t.Fatalf("LegacyGoalToInput returned error: %v", err)
	}
	entry, err := BuildGoalAutomation(in)
	if err != nil {
		t.Fatalf("BuildGoalAutomation(LegacyGoalToInput(...)) returned error: %v", err)
	}

	if entry.Type != "automation" {
		t.Errorf("entry.Type = %q, want automation", entry.Type)
	}
	if entry.Goal == nil {
		t.Fatal("entry.Goal is nil")
	}
	if entry.Goal.ID != "ship-oauth-login" {
		t.Errorf("entry.Goal.ID = %q, want ship-oauth-login", entry.Goal.ID)
	}
	if strings.TrimSpace(entry.Goal.Criteria) == "" {
		t.Error("entry.Goal.Criteria is empty")
	}

	wantTags := map[string]bool{"goal": false, "goal:ship-oauth-login": false}
	for _, tag := range entry.Tags {
		if _, ok := wantTags[tag]; ok {
			wantTags[tag] = true
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Errorf("expected tag %q in %v", tag, entry.Tags)
		}
	}
}
