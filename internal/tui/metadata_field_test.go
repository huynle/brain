package tui

import (
	"testing"
)

// ============================================================================
// MetadataField Type Tests
// ============================================================================

func TestMetadataFieldConstants(t *testing.T) {
	// Test that field constants are defined
	tests := []struct {
		name     string
		field    MetadataField
		expected string
	}{
		{"Status field", FieldStatus, "status"},
		{"Priority field", FieldPriority, "priority"},
		{"FeatureID field", FieldFeatureID, "feature_id"},
		{"GitBranch field", FieldGitBranch, "git_branch"},
		{"Agent field", FieldAgent, "agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.field) != tt.expected {
				t.Errorf("Field %s = %q, want %q", tt.name, tt.field, tt.expected)
			}
		})
	}
}

// ============================================================================
// FieldType Tests
// ============================================================================

func TestGetFieldType(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected FieldType
	}{
		{"Status is dropdown", FieldStatus, FieldTypeDropdown},
		{"Priority is dropdown", FieldPriority, FieldTypeDropdown},
		{"FeatureID is text", FieldFeatureID, FieldTypeText},
		{"GitBranch is text", FieldGitBranch, FieldTypeText},
		{"CompleteOnIdle is boolean", FieldCompleteOnIdle, FieldTypeBoolean},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFieldType(tt.field)
			if got != tt.expected {
				t.Errorf("getFieldType(%q) = %v, want %v", tt.field, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// FieldMeta Tests
// ============================================================================

func TestGetFieldLabel(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected string
	}{
		{"Status label", FieldStatus, "Status"},
		{"FeatureID label", FieldFeatureID, "Feature ID"},
		{"GitBranch label", FieldGitBranch, "Git Branch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFieldLabel(tt.field)
			if got != tt.expected {
				t.Errorf("getFieldLabel(%q) = %q, want %q", tt.field, got, tt.expected)
			}
		})
	}
}

func TestGetEnumOptions(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected []string
	}{
		{
			"Status options",
			FieldStatus,
			[]string{"draft", "pending", "active", "in_progress", "blocked", "cancelled", "completed", "validated", "superseded", "archived"},
		},
		{
			"Priority options",
			FieldPriority,
			[]string{"high", "medium", "low"},
		},
		{
			"Text field has no options",
			FieldFeatureID,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnumOptions(tt.field)
			if !stringSlicesEqual(got, tt.expected) {
				t.Errorf("getEnumOptions(%q) = %v, want %v", tt.field, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// FieldMoveToProject Tests
// ============================================================================

func TestFieldMoveToProject_Constant(t *testing.T) {
	if string(FieldMoveToProject) != "move_to_project" {
		t.Errorf("FieldMoveToProject = %q, want %q", FieldMoveToProject, "move_to_project")
	}
}

func TestFieldMoveToProject_Type(t *testing.T) {
	got := getFieldType(FieldMoveToProject)
	if got != FieldTypeFilterDropdown {
		t.Errorf("getFieldType(FieldMoveToProject) = %v, want FieldTypeFilterDropdown", got)
	}
}

func TestFieldMoveToProject_Label(t *testing.T) {
	got := getFieldLabel(FieldMoveToProject)
	if got != "Move to Project" {
		t.Errorf("getFieldLabel(FieldMoveToProject) = %q, want %q", got, "Move to Project")
	}
}

func TestFieldMoveToProject_InTaskTab_FeatureMode(t *testing.T) {
	fields := fieldsForTab(MetaTabTask, ModeFeature)
	found := false
	for _, f := range fields {
		if f == FieldMoveToProject {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FieldMoveToProject not found in Task tab for Feature mode, got fields: %v", fields)
	}
	// Should be last field
	if fields[len(fields)-1] != FieldMoveToProject {
		t.Errorf("FieldMoveToProject should be last field in Task tab, got %v", fields)
	}
}

func TestFieldMoveToProject_InTaskTab_SingleMode(t *testing.T) {
	fields := fieldsForTab(MetaTabTask, ModeSingle)
	found := false
	for _, f := range fields {
		if f == FieldMoveToProject {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FieldMoveToProject not found in Task tab for Single mode, got fields: %v", fields)
	}
	// Should be last field
	if fields[len(fields)-1] != FieldMoveToProject {
		t.Errorf("FieldMoveToProject should be last field in Task tab, got %v", fields)
	}
}

func TestFieldMoveToProject_InTaskTab_BatchMode(t *testing.T) {
	fields := fieldsForTab(MetaTabTask, ModeBatch)
	found := false
	for _, f := range fields {
		if f == FieldMoveToProject {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FieldMoveToProject not found in Task tab for Batch mode, got fields: %v", fields)
	}
	// Should be last field
	if fields[len(fields)-1] != FieldMoveToProject {
		t.Errorf("FieldMoveToProject should be last field in Task tab, got %v", fields)
	}
}

// ============================================================================
// Goal Automation Field Tests (Phase 1)
// ============================================================================

func TestGoalFieldConstants(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected string
	}{
		{"GoalTriggerSource constant", FieldGoalTriggerSource, "goal_trigger_source"},
		{"GoalSessionMode constant", FieldGoalSessionMode, "goal_session_mode"},
		{"GoalExecutor constant", FieldGoalExecutor, "goal_executor"},
		{"GoalCriteria constant", FieldGoalCriteria, "goal_criteria"},
		{"GoalValidation constant", FieldGoalValidation, "goal_validation"},
		{"GoalWorkdir constant", FieldGoalWorkdir, "goal_workdir"},
		{"GoalObjective constant", FieldGoalObjective, "goal_objective"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.field) != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.field, tt.expected)
			}
		})
	}
}

func TestGoalFieldType(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected FieldType
	}{
		{"GoalTriggerSource is dropdown", FieldGoalTriggerSource, FieldTypeDropdown},
		{"GoalSessionMode is dropdown", FieldGoalSessionMode, FieldTypeDropdown},
		{"GoalExecutor is dropdown", FieldGoalExecutor, FieldTypeDropdown},
		{"GoalCriteria is text", FieldGoalCriteria, FieldTypeText},
		{"GoalValidation is text", FieldGoalValidation, FieldTypeText},
		{"GoalWorkdir is text", FieldGoalWorkdir, FieldTypeText},
		{"GoalObjective is text", FieldGoalObjective, FieldTypeText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFieldType(tt.field)
			if got != tt.expected {
				t.Errorf("getFieldType(%q) = %v, want %v", tt.field, got, tt.expected)
			}
		})
	}
}

func TestGoalFieldLabel(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected string
	}{
		{"GoalTriggerSource label", FieldGoalTriggerSource, "Trigger Source"},
		{"GoalSessionMode label", FieldGoalSessionMode, "Session Mode"},
		{"GoalExecutor label", FieldGoalExecutor, "Executor"},
		{"GoalCriteria label", FieldGoalCriteria, "Criteria"},
		{"GoalValidation label", FieldGoalValidation, "Validation"},
		{"GoalWorkdir label", FieldGoalWorkdir, "Workdir"},
		{"GoalObjective label", FieldGoalObjective, "Objective"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFieldLabel(tt.field)
			if got != tt.expected {
				t.Errorf("getFieldLabel(%q) = %q, want %q", tt.field, got, tt.expected)
			}
		})
	}
}

func TestGoalFieldEnumOptions(t *testing.T) {
	tests := []struct {
		name     string
		field    MetadataField
		expected []string
	}{
		{"GoalTriggerSource options", FieldGoalTriggerSource, []string{"task", "feature", "both"}},
		{"GoalSessionMode options", FieldGoalSessionMode, []string{"continue", "fresh"}},
		{"GoalExecutor options", FieldGoalExecutor, []string{"opencode", "pi"}},
		{"GoalCriteria has no options", FieldGoalCriteria, nil},
		{"GoalValidation has no options", FieldGoalValidation, nil},
		{"GoalWorkdir has no options", FieldGoalWorkdir, nil},
		{"GoalObjective has no options", FieldGoalObjective, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnumOptions(tt.field)
			if !stringSlicesEqual(got, tt.expected) {
				t.Errorf("getEnumOptions(%q) = %v, want %v", tt.field, got, tt.expected)
			}
		})
	}
}

// stringSlicesEqual compares two string slices for equality.
func stringSlicesEqual(a, b []string) bool {
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
