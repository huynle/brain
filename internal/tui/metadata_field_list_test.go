package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// TestBuildFieldList_SingleMode tests that single mode returns all standard fields.
func TestBuildFieldList_SingleMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task1", apiClient)
	fields := modal.buildFieldList()

	// Should include all 15 standard fields
	expectedCount := 15
	if len(fields) != expectedCount {
		t.Errorf("Single mode field count = %d, want %d", len(fields), expectedCount)
	}

	// Should include task-specific field
	if !containsField(fields, FieldDirectPrompt) {
		t.Error("Single mode should include FieldDirectPrompt")
	}

	// Should include standard feature field
	if !containsField(fields, FieldFeatureID) {
		t.Error("Single mode should include FieldFeatureID")
	}
}

// TestBuildFieldList_BatchMode tests that batch mode returns all standard fields.
func TestBuildFieldList_BatchMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalBatch([]string{"task1", "task2"}, apiClient)
	fields := modal.buildFieldList()

	// Should include all 15 standard fields
	expectedCount := 15
	if len(fields) != expectedCount {
		t.Errorf("Batch mode field count = %d, want %d", len(fields), expectedCount)
	}

	// Should include task-specific field
	if !containsField(fields, FieldDirectPrompt) {
		t.Error("Batch mode should include FieldDirectPrompt")
	}
}

// TestBuildFieldList_FeatureMode tests that feature mode returns feature-specific fields.
func TestBuildFieldList_FeatureMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)
	fields := modal.buildFieldList()

	// Should include new feature fields
	if !containsField(fields, FieldFeaturePriority) {
		t.Error("Feature mode should include FieldFeaturePriority")
	}
	if !containsField(fields, FieldFeatureDependsOn) {
		t.Error("Feature mode should include FieldFeatureDependsOn")
	}

	// Should include shared fields
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
		if !containsField(fields, field) {
			t.Errorf("Feature mode should include %s", field)
		}
	}
}

// TestBuildFieldList_FeatureMode_ExcludesDirectPrompt tests that feature mode excludes task-specific fields.
func TestBuildFieldList_FeatureMode_ExcludesDirectPrompt(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)
	fields := modal.buildFieldList()

	// Should NOT include task-specific field
	if containsField(fields, FieldDirectPrompt) {
		t.Error("Feature mode should NOT include FieldDirectPrompt (task-specific)")
	}
}

// TestBuildFieldList_FeatureMode_ExcludesFeatureID tests that feature mode excludes FieldFeatureID.
func TestBuildFieldList_FeatureMode_ExcludesFeatureID(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)
	fields := modal.buildFieldList()

	// Should NOT include FieldFeatureID (already grouped by feature)
	if containsField(fields, FieldFeatureID) {
		t.Error("Feature mode should NOT include FieldFeatureID (already grouped)")
	}
}

// TestBuildFieldList_FeatureMode_IncludesFeaturePriority tests that new feature field is present.
func TestBuildFieldList_FeatureMode_IncludesFeaturePriority(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "test-project", apiClient)
	fields := modal.buildFieldList()

	// Should include new feature-level field
	if !containsField(fields, FieldFeaturePriority) {
		t.Error("Feature mode should include FieldFeaturePriority")
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
