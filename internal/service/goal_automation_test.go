package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func baseGoalInput() GoalInput {
	return GoalInput{
		Project:   "brain-api",
		FeatureID: "auth-system",
		Title:     "Ship OAuth login",
		Config: types.GoalConfig{
			ID:         "g-oauth",
			Criteria:   "All auth tasks complete and login works",
			Validation: "go test ./... passes",
			Workdir:    "/work/brain-api",
		},
		Action: types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Reconcile the OAuth goal.",
			Agent:        "tdd-dev",
			SessionMode:  "fresh",
		},
	}
}

func TestBuildGoalAutomation_BaseShape(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput())
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}

	if entry.Type != "automation" {
		t.Errorf("Type = %q, want automation", entry.Type)
	}
	if entry.Status != "active" {
		t.Errorf("Status = %q, want active", entry.Status)
	}
	if entry.GeneratedBy != types.GoalGeneratedBy {
		t.Errorf("GeneratedBy = %q, want %q", entry.GeneratedBy, types.GoalGeneratedBy)
	}
	if entry.ProjectID != "brain-api" {
		t.Errorf("ProjectID = %q, want brain-api", entry.ProjectID)
	}
	if entry.FeatureID != "auth-system" {
		t.Errorf("FeatureID = %q, want auth-system", entry.FeatureID)
	}
	if entry.Title != "Ship OAuth login" {
		t.Errorf("Title = %q, want Ship OAuth login", entry.Title)
	}
}

func TestBuildGoalAutomation_Tags(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput())
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}

	wantTags := map[string]bool{"goal": false, "goal:g-oauth": false}
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

func TestBuildGoalAutomation_MultiTriggerBoth(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput())
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}
	if entry.Trigger == nil {
		t.Fatal("Trigger is nil")
	}
	if entry.Trigger.Type != "event" {
		t.Errorf("Trigger.Type = %q, want event", entry.Trigger.Type)
	}

	patterns := entry.Trigger.EventPatterns()
	wantEvents := map[string]bool{
		types.EventTaskStatusChanged: false,
		types.EventFeatureCompleted:  false,
	}
	for _, p := range patterns {
		if _, ok := wantEvents[p]; ok {
			wantEvents[p] = true
		}
	}
	for ev, found := range wantEvents {
		if !found {
			t.Errorf("expected event %q in trigger patterns %v", ev, patterns)
		}
	}

	// feature.all_completed is dead and must never appear.
	for _, p := range patterns {
		if p == "feature.all_completed" {
			t.Errorf("trigger must not reference dead event feature.all_completed")
		}
	}
}

func TestBuildGoalAutomation_TriggerSourceTaskOnly(t *testing.T) {
	in := baseGoalInput()
	in.Config.TriggerSource = types.GoalTriggerSourceTask
	entry, err := BuildGoalAutomation(in)
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}
	patterns := entry.Trigger.EventPatterns()
	if len(patterns) != 1 || patterns[0] != types.EventTaskStatusChanged {
		t.Errorf("task-only patterns = %v, want [%s]", patterns, types.EventTaskStatusChanged)
	}
}

func TestBuildGoalAutomation_TriggerSourceFeatureOnly(t *testing.T) {
	in := baseGoalInput()
	in.Config.TriggerSource = types.GoalTriggerSourceFeature
	entry, err := BuildGoalAutomation(in)
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}
	patterns := entry.Trigger.EventPatterns()
	if len(patterns) != 1 || patterns[0] != types.EventFeatureCompleted {
		t.Errorf("feature-only patterns = %v, want [%s]", patterns, types.EventFeatureCompleted)
	}
}

func TestBuildGoalAutomation_FeatureScoping(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput())
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}
	if entry.Trigger.Filter["feature_id"] != "auth-system" {
		t.Errorf("Trigger.Filter[feature_id] = %q, want auth-system", entry.Trigger.Filter["feature_id"])
	}
}

func TestBuildGoalAutomation_StatusesORableInclBlocked(t *testing.T) {
	in := baseGoalInput()
	in.Config.CompleteStatuses = []string{"completed", "validated"}
	in.Config.BlockedStatuses = []string{"blocked"}
	entry, err := BuildGoalAutomation(in)
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}

	expr := entry.Trigger.Filter["to_status"]
	if expr == "" {
		t.Fatal("Trigger.Filter[to_status] is empty, want OR-able in: expression")
	}
	// Must be an OR-able set and include all of completed, validated, blocked.
	for _, status := range []string{"completed", "validated", "blocked"} {
		if !types.MatchFilterValue(status, expr) {
			t.Errorf("to_status filter %q does not match %q", expr, status)
		}
	}
	if types.MatchFilterValue("pending", expr) {
		t.Errorf("to_status filter %q must not match pending", expr)
	}
}

func TestBuildGoalAutomation_Action(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput())
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}
	if entry.Action == nil {
		t.Fatal("Action is nil")
	}
	if entry.Action.SessionMode != "fresh" {
		t.Errorf("Action.SessionMode = %q, want fresh", entry.Action.SessionMode)
	}
	if entry.Action.DirectPrompt != "Reconcile the OAuth goal." {
		t.Errorf("Action.DirectPrompt = %q", entry.Action.DirectPrompt)
	}
	if entry.Action.Agent != "tdd-dev" {
		t.Errorf("Action.Agent = %q, want tdd-dev", entry.Action.Agent)
	}
}

func TestBuildGoalAutomation_GoalConfigDefaults(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput())
	if err != nil {
		t.Fatalf("BuildGoalAutomation returned error: %v", err)
	}
	cfg := entry.Goal
	if cfg == nil {
		t.Fatal("entry.Goal is nil, want goal config")
	}
	if cfg.ID != "g-oauth" {
		t.Errorf("Goal.ID = %q, want g-oauth", cfg.ID)
	}
	if cfg.NormalizedTriggerSource() != types.GoalTriggerSourceBoth {
		t.Errorf("default trigger source = %q, want both", cfg.NormalizedTriggerSource())
	}
	// Default complete/blocked statuses applied when unset.
	if len(cfg.CompleteStatuses) == 0 {
		t.Error("expected default CompleteStatuses to be applied")
	}
	foundBlocked := false
	for _, s := range cfg.BlockedStatuses {
		if s == "blocked" {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Errorf("expected default BlockedStatuses to include blocked, got %v", cfg.BlockedStatuses)
	}
}

func TestBuildGoalAutomation_Validation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GoalInput)
	}{
		{"missing goal id", func(in *GoalInput) { in.Config.ID = "" }},
		{"missing project", func(in *GoalInput) { in.Project = "" }},
		{"missing title", func(in *GoalInput) { in.Title = "" }},
		{"missing action type", func(in *GoalInput) { in.Action.Type = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseGoalInput()
			tt.mutate(&in)
			if _, err := BuildGoalAutomation(in); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}
