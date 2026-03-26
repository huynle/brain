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
			[]string{"draft", "pending", "active", "in_progress", "blocked", "completed", "validated", "superseded", "archived"},
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
