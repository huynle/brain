package tui

// ============================================================================
// Metadata Field Types
// ============================================================================

// MetadataField represents a specific metadata field that can be edited.
type MetadataField string

// Field constants - all 15 editable metadata fields
const (
	FieldStatus            MetadataField = "status"
	FieldPriority          MetadataField = "priority"
	FieldFeatureID         MetadataField = "feature_id"
	FieldGitBranch         MetadataField = "git_branch"
	FieldMergeTargetBranch MetadataField = "merge_target_branch"
	FieldMergePolicy       MetadataField = "merge_policy"
	FieldMergeStrategy     MetadataField = "merge_strategy"
	FieldExecutionMode     MetadataField = "execution_mode"
	FieldDirectPrompt      MetadataField = "direct_prompt"
	FieldAgent             MetadataField = "agent"
	FieldModel             MetadataField = "model"
	FieldTargetWorkdir     MetadataField = "target_workdir"
	FieldCompleteOnIdle    MetadataField = "complete_on_idle"
	FieldOpenPRBeforeMerge MetadataField = "open_pr_before_merge"
	FieldSchedule          MetadataField = "schedule"
)

// ============================================================================
// Field Type Enum
// ============================================================================

// FieldType represents the type of input for a field.
type FieldType int

const (
	FieldTypeText FieldType = iota
	FieldTypeDropdown
	FieldTypeBoolean
)

// ============================================================================
// Field Metadata
// ============================================================================

// FieldMeta contains metadata about a field.
type FieldMeta struct {
	Label       string
	Hint        string
	Type        FieldType
	EnumOptions []string
}

// fieldMetadata maps fields to their metadata
var fieldMetadata = map[MetadataField]FieldMeta{
	FieldStatus: {
		Label:       "Status",
		Hint:        "Task status",
		Type:        FieldTypeDropdown,
		EnumOptions: []string{"draft", "pending", "active", "in_progress", "blocked", "completed", "validated", "superseded", "archived"},
	},
	FieldPriority: {
		Label:       "Priority",
		Hint:        "Task priority",
		Type:        FieldTypeDropdown,
		EnumOptions: []string{"high", "medium", "low"},
	},
	FieldFeatureID: {
		Label: "Feature ID",
		Hint:  "Feature grouping identifier",
		Type:  FieldTypeText,
	},
	FieldGitBranch: {
		Label: "Git Branch",
		Hint:  "Target git branch for task execution",
		Type:  FieldTypeText,
	},
	FieldMergeTargetBranch: {
		Label: "Merge Target Branch",
		Hint:  "Branch to merge into after completion",
		Type:  FieldTypeText,
	},
	FieldMergePolicy: {
		Label:       "Merge Policy",
		Hint:        "How to handle PR/merge after completion",
		Type:        FieldTypeDropdown,
		EnumOptions: []string{"prompt_only", "auto_pr", "auto_merge"},
	},
	FieldMergeStrategy: {
		Label:       "Merge Strategy",
		Hint:        "Git merge strategy",
		Type:        FieldTypeDropdown,
		EnumOptions: []string{"squash", "merge", "rebase"},
	},
	FieldExecutionMode: {
		Label:       "Execution Mode",
		Hint:        "Worktree isolation mode",
		Type:        FieldTypeDropdown,
		EnumOptions: []string{"worktree", "current_branch"},
	},
	FieldDirectPrompt: {
		Label: "Direct Prompt",
		Hint:  "Direct prompt to pass to agent",
		Type:  FieldTypeText,
	},
	FieldAgent: {
		Label: "Agent",
		Hint:  "OpenCode agent to use for execution",
		Type:  FieldTypeText,
	},
	FieldModel: {
		Label: "Model",
		Hint:  "LLM model override",
		Type:  FieldTypeText,
	},
	FieldTargetWorkdir: {
		Label: "Target Workdir",
		Hint:  "Working directory for task execution",
		Type:  FieldTypeText,
	},
	FieldCompleteOnIdle: {
		Label: "Complete On Idle",
		Hint:  "Auto-complete when agent goes idle",
		Type:  FieldTypeBoolean,
	},
	FieldOpenPRBeforeMerge: {
		Label: "Open PR Before Merge",
		Hint:  "Open PR before auto-merge",
		Type:  FieldTypeBoolean,
	},
	FieldSchedule: {
		Label: "Schedule",
		Hint:  "Cron expression for scheduled execution",
		Type:  FieldTypeText,
	},
}

// ============================================================================
// Helper Functions
// ============================================================================

// getFieldType returns the type of a field.
func getFieldType(field MetadataField) FieldType {
	if meta, ok := fieldMetadata[field]; ok {
		return meta.Type
	}
	return FieldTypeText // default to text
}

// getFieldLabel returns the display label for a field.
func getFieldLabel(field MetadataField) string {
	if meta, ok := fieldMetadata[field]; ok {
		return meta.Label
	}
	return string(field) // fallback to field name
}

// getEnumOptions returns the enum options for a dropdown field.
func getEnumOptions(field MetadataField) []string {
	if meta, ok := fieldMetadata[field]; ok {
		return meta.EnumOptions
	}
	return nil
}
