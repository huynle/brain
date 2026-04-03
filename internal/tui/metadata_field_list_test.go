package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// TestBuildFieldList_SingleMode tests that single mode returns fields for the current tab.
func TestBuildFieldList_SingleMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task1", apiClient)

	// Default tab is Task — should have 3 fields
	fields := modal.buildFieldList()
	if len(fields) != 3 {
		t.Errorf("Single mode Task tab field count = %d, want 3", len(fields))
	}

	// Should include standard feature field on Task tab
	if !containsField(fields, FieldFeatureID) {
		t.Error("Single mode Task tab should include FieldFeatureID")
	}

	// Switch to Execution tab — should include DirectPrompt
	modal.switchToTab(MetaTabExecution)
	fields = modal.buildFieldList()
	if !containsField(fields, FieldDirectPrompt) {
		t.Error("Single mode Execution tab should include FieldDirectPrompt")
	}

	// Verify all 15 fields are accessible across all tabs
	allFields := make(map[MetadataField]bool)
	for _, tab := range modal.tabs {
		for _, field := range fieldsForTab(tab, modal.mode) {
			allFields[field] = true
		}
	}
	if len(allFields) != 15 {
		t.Errorf("Total fields across all tabs = %d, want 15", len(allFields))
	}
}

// TestBuildFieldList_BatchMode tests that batch mode returns fields for the current tab.
func TestBuildFieldList_BatchMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalBatch([]string{"task1", "task2"}, apiClient)

	// Default tab is Task — should have 3 fields
	fields := modal.buildFieldList()
	if len(fields) != 3 {
		t.Errorf("Batch mode Task tab field count = %d, want 3", len(fields))
	}

	// Switch to Execution tab — should include DirectPrompt
	modal.switchToTab(MetaTabExecution)
	fields = modal.buildFieldList()
	if !containsField(fields, FieldDirectPrompt) {
		t.Error("Batch mode Execution tab should include FieldDirectPrompt")
	}
}

// TestBuildFieldList_FeatureMode tests that feature mode returns feature-specific fields per tab.
func TestBuildFieldList_FeatureMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)

	// Default tab is Feature — should include feature fields
	fields := modal.buildFieldList()
	if !containsField(fields, FieldFeaturePriority) {
		t.Error("Feature tab should include FieldFeaturePriority")
	}
	if !containsField(fields, FieldFeatureDependsOn) {
		t.Error("Feature tab should include FieldFeatureDependsOn")
	}

	// Verify all shared fields are accessible across all tabs
	allFields := make(map[MetadataField]bool)
	for _, tab := range modal.tabs {
		for _, field := range fieldsForTab(tab, modal.mode) {
			allFields[field] = true
		}
	}

	requiredFields := []MetadataField{
		FieldStatus,
		FieldPriority,
		FieldGitBranch,
		FieldMergeTargetBranch,
		FieldMergePolicy,
		FieldMergeStrategy,
		FieldExecutionMode,
		FieldAgent,
		FieldModel,
		FieldTargetWorkdir,
		FieldOpenPRBeforeMerge,
	}

	for _, field := range requiredFields {
		if !allFields[field] {
			t.Errorf("Feature mode should include %s across tabs", field)
		}
	}
}

// TestBuildFieldList_FeatureMode_ExcludesDirectPrompt tests that feature mode excludes task-specific fields.
func TestBuildFieldList_FeatureMode_ExcludesDirectPrompt(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)

	// Check across ALL tabs — DirectPrompt should not appear in any tab
	for _, tab := range modal.tabs {
		fields := fieldsForTab(tab, modal.mode)
		if containsField(fields, FieldDirectPrompt) {
			t.Errorf("Feature mode tab %v should NOT include FieldDirectPrompt (task-specific)", tab)
		}
	}
}

// TestBuildFieldList_FeatureMode_ExcludesFeatureID tests that feature mode excludes FieldFeatureID.
func TestBuildFieldList_FeatureMode_ExcludesFeatureID(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)

	// Check across ALL tabs — FeatureID should not appear in any tab
	for _, tab := range modal.tabs {
		fields := fieldsForTab(tab, modal.mode)
		if containsField(fields, FieldFeatureID) {
			t.Errorf("Feature mode tab %v should NOT include FieldFeatureID (already grouped)", tab)
		}
	}
}

// TestBuildFieldList_FeatureMode_IncludesFeaturePriority tests that new feature field is present.
func TestBuildFieldList_FeatureMode_IncludesFeaturePriority(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)

	// Feature tab (default) should include FeaturePriority
	fields := modal.buildFieldList()
	if !containsField(fields, FieldFeaturePriority) {
		t.Error("Feature mode Feature tab should include FieldFeaturePriority")
	}
}

// ============================================================================
// Tab Helper Tests
// ============================================================================

// TestTabsForMode_Feature tests that feature mode returns 5 tabs.
func TestTabsForMode_Feature(t *testing.T) {
	tabs := tabsForMode(ModeFeature)
	if len(tabs) != 5 {
		t.Errorf("Feature mode tab count = %d, want 5", len(tabs))
	}
	expected := []MetadataTab{MetaTabFeature, MetaTabTask, MetaTabExecution, MetaTabGitMerge, MetaTabMonitors}
	for i, tab := range expected {
		if tabs[i] != tab {
			t.Errorf("tabs[%d] = %v, want %v", i, tabs[i], tab)
		}
	}
}

// TestTabsForMode_Single tests that single mode returns 3 tabs.
func TestTabsForMode_Single(t *testing.T) {
	tabs := tabsForMode(ModeSingle)
	if len(tabs) != 3 {
		t.Errorf("Single mode tab count = %d, want 3", len(tabs))
	}
	expected := []MetadataTab{MetaTabTask, MetaTabExecution, MetaTabGitMerge}
	for i, tab := range expected {
		if tabs[i] != tab {
			t.Errorf("tabs[%d] = %v, want %v", i, tabs[i], tab)
		}
	}
}

// TestTabsForMode_Batch tests that batch mode returns 3 tabs (same as single).
func TestTabsForMode_Batch(t *testing.T) {
	tabs := tabsForMode(ModeBatch)
	if len(tabs) != 3 {
		t.Errorf("Batch mode tab count = %d, want 3", len(tabs))
	}
}

// TestFieldsForTab_FeatureTab tests fields in the Feature tab.
func TestFieldsForTab_FeatureTab(t *testing.T) {
	fields := fieldsForTab(MetaTabFeature, ModeFeature)
	if len(fields) != 2 {
		t.Errorf("Feature tab field count = %d, want 2", len(fields))
	}
	if !containsField(fields, FieldFeaturePriority) {
		t.Error("Feature tab should include FieldFeaturePriority")
	}
	if !containsField(fields, FieldFeatureDependsOn) {
		t.Error("Feature tab should include FieldFeatureDependsOn")
	}
}

// TestFieldsForTab_TaskTab_FeatureMode tests fields in the Task tab for feature mode.
func TestFieldsForTab_TaskTab_FeatureMode(t *testing.T) {
	fields := fieldsForTab(MetaTabTask, ModeFeature)
	if len(fields) != 2 {
		t.Errorf("Task tab (feature mode) field count = %d, want 2", len(fields))
	}
	if !containsField(fields, FieldStatus) {
		t.Error("Task tab should include FieldStatus")
	}
	if !containsField(fields, FieldPriority) {
		t.Error("Task tab should include FieldPriority")
	}
}

// TestFieldsForTab_TaskTab_SingleMode tests fields in the Task tab for single mode.
func TestFieldsForTab_TaskTab_SingleMode(t *testing.T) {
	fields := fieldsForTab(MetaTabTask, ModeSingle)
	if len(fields) != 3 {
		t.Errorf("Task tab (single mode) field count = %d, want 3", len(fields))
	}
	if !containsField(fields, FieldFeatureID) {
		t.Error("Task tab (single mode) should include FieldFeatureID")
	}
}

// TestFieldsForTab_ExecutionTab_FeatureMode tests fields in the Execution tab for feature mode.
func TestFieldsForTab_ExecutionTab_FeatureMode(t *testing.T) {
	fields := fieldsForTab(MetaTabExecution, ModeFeature)
	if containsField(fields, FieldDirectPrompt) {
		t.Error("Execution tab (feature mode) should NOT include FieldDirectPrompt")
	}
	if !containsField(fields, FieldAgent) {
		t.Error("Execution tab should include FieldAgent")
	}
	if !containsField(fields, FieldSchedule) {
		t.Error("Execution tab should include FieldSchedule")
	}
}

// TestFieldsForTab_ExecutionTab_SingleMode tests fields in the Execution tab for single mode.
func TestFieldsForTab_ExecutionTab_SingleMode(t *testing.T) {
	fields := fieldsForTab(MetaTabExecution, ModeSingle)
	if !containsField(fields, FieldDirectPrompt) {
		t.Error("Execution tab (single mode) should include FieldDirectPrompt")
	}
}

// TestFieldsForTab_GitMergeTab tests fields in the Git & Merge tab.
func TestFieldsForTab_GitMergeTab(t *testing.T) {
	fields := fieldsForTab(MetaTabGitMerge, ModeSingle)
	if len(fields) != 5 {
		t.Errorf("Git & Merge tab field count = %d, want 5", len(fields))
	}
	if !containsField(fields, FieldGitBranch) {
		t.Error("Git & Merge tab should include FieldGitBranch")
	}
	if !containsField(fields, FieldOpenPRBeforeMerge) {
		t.Error("Git & Merge tab should include FieldOpenPRBeforeMerge")
	}
}

// TestFieldsForTab_MonitorsTab tests that Monitors tab has no regular fields.
func TestFieldsForTab_MonitorsTab(t *testing.T) {
	fields := fieldsForTab(MetaTabMonitors, ModeFeature)
	if len(fields) != 0 {
		t.Errorf("Monitors tab field count = %d, want 0", len(fields))
	}
}

// TestTabLabel tests tab label display strings.
func TestTabLabel(t *testing.T) {
	tests := []struct {
		tab      MetadataTab
		expected string
	}{
		{MetaTabFeature, "Feature"},
		{MetaTabTask, "Task"},
		{MetaTabExecution, "Execution"},
		{MetaTabGitMerge, "Git & Merge"},
		{MetaTabMonitors, "Monitors"},
	}
	for _, tt := range tests {
		label := tabLabel(tt.tab)
		if label != tt.expected {
			t.Errorf("tabLabel(%v) = %q, want %q", tt.tab, label, tt.expected)
		}
	}
}

// TestBuildFieldList_ReturnsCurrentTabFields tests that buildFieldList returns fields for the current tab.
func TestBuildFieldList_ReturnsCurrentTabFields(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task1", apiClient)

	// Default tab should be Task (first tab for single mode)
	fields := modal.buildFieldList()
	expectedFields := fieldsForTab(MetaTabTask, ModeSingle)
	if len(fields) != len(expectedFields) {
		t.Errorf("buildFieldList() returned %d fields, want %d (Task tab)", len(fields), len(expectedFields))
	}
}

// TestBuildFieldList_FeatureMode_FirstTab tests that feature mode starts on Feature tab.
func TestBuildFieldList_FeatureMode_FirstTab(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)

	// Default tab should be Feature (first tab for feature mode)
	fields := modal.buildFieldList()
	expectedFields := fieldsForTab(MetaTabFeature, ModeFeature)
	if len(fields) != len(expectedFields) {
		t.Errorf("buildFieldList() returned %d fields, want %d (Feature tab)", len(fields), len(expectedFields))
	}
}

// containsField checks if a field is in the list.
func containsField(fields []MetadataField, target MetadataField) bool {
	for _, field := range fields {
		if field == target {
			return true
		}
	}
	return false
}
