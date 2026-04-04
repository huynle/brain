package tui

// ============================================================================
// Metadata Field Types
// ============================================================================

// MetadataField represents a specific metadata field that can be edited.
type MetadataField string

// Field constants - all editable metadata fields
const (
	FieldStatus            MetadataField = "status"
	FieldPriority          MetadataField = "priority"
	FieldFeatureID         MetadataField = "feature_id"
	FieldFeaturePriority   MetadataField = "feature_priority"
	FieldFeatureDependsOn  MetadataField = "feature_depends_on"
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
	FieldMoveToProject     MetadataField = "move_to_project"

	// Task-level scheduling fields
	FieldScheduleEnabled MetadataField = "schedule_enabled"
	FieldRunOnceAt       MetadataField = "run_once_at"
	FieldStartsAt        MetadataField = "starts_at"
	FieldExpiresAt       MetadataField = "expires_at"
	FieldTimezone        MetadataField = "timezone"

	// Feature-level scheduling fields
	FieldFeatureSchedule  MetadataField = "feature_schedule"
	FieldFeatureStartsAt  MetadataField = "feature_starts_at"
	FieldFeatureExpiresAt MetadataField = "feature_expires_at"
	FieldFeatureRunOnceAt MetadataField = "feature_run_once_at"
	FieldFeatureTimezone  MetadataField = "feature_timezone"
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
	FieldTypeFilterDropdown
	FieldTypeMultiFilterDropdown
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
		EnumOptions: []string{"draft", "pending", "active", "in_progress", "blocked", "cancelled", "completed", "validated", "superseded", "archived"},
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
	FieldFeaturePriority: {
		Label:       "Feature Priority",
		Hint:        "Priority for entire feature group (high/medium/low)",
		Type:        FieldTypeDropdown,
		EnumOptions: []string{"high", "medium", "low"},
	},
	FieldFeatureDependsOn: {
		Label: "Feature Dependencies",
		Hint:  "Select feature dependencies (multi-select)",
		Type:  FieldTypeMultiFilterDropdown,
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
	FieldScheduleEnabled: {
		Label: "Schedule Enabled",
		Hint:  "Toggle schedule on/off",
		Type:  FieldTypeBoolean,
	},
	FieldRunOnceAt: {
		Label: "Run Once At",
		Hint:  "RFC3339 timestamp for one-shot execution (e.g. 2026-04-05T14:00:00-06:00)",
		Type:  FieldTypeText,
	},
	FieldStartsAt: {
		Label: "Starts At",
		Hint:  "Schedule window start time (RFC3339)",
		Type:  FieldTypeText,
	},
	FieldExpiresAt: {
		Label: "Expires At",
		Hint:  "Schedule window end time (RFC3339)",
		Type:  FieldTypeText,
	},
	FieldTimezone: {
		Label: "Timezone",
		Hint:  "IANA timezone for schedule evaluation (e.g. America/Denver, Asia/Tokyo)",
		Type:  FieldTypeText,
	},
	FieldFeatureSchedule: {
		Label: "Feature Schedule",
		Hint:  "Cron expression for feature-level scheduling",
		Type:  FieldTypeText,
	},
	FieldFeatureStartsAt: {
		Label: "Feature Starts At",
		Hint:  "Feature schedule window start (RFC3339)",
		Type:  FieldTypeText,
	},
	FieldFeatureExpiresAt: {
		Label: "Feature Expires At",
		Hint:  "Feature schedule window end (RFC3339)",
		Type:  FieldTypeText,
	},
	FieldFeatureRunOnceAt: {
		Label: "Feature Run Once At",
		Hint:  "One-shot feature enable time (RFC3339)",
		Type:  FieldTypeText,
	},
	FieldFeatureTimezone: {
		Label: "Feature Timezone",
		Hint:  "IANA timezone for feature schedule (e.g. America/Denver)",
		Type:  FieldTypeText,
	},
	FieldMoveToProject: {
		Label: "Move to Project",
		Hint:  "Move tasks to another project (type to filter)",
		Type:  FieldTypeFilterDropdown,
	},
}

// ============================================================================
// Tab Types
// ============================================================================

// MetadataTab represents a tab section in the metadata modal.
type MetadataTab int

const (
	MetaTabFeature MetadataTab = iota
	MetaTabTask
	MetaTabExecution
	MetaTabGitMerge
	MetaTabMonitors
)

// tabLabel returns the display label for a tab.
func tabLabel(tab MetadataTab) string {
	switch tab {
	case MetaTabFeature:
		return "Feature"
	case MetaTabTask:
		return "Task"
	case MetaTabExecution:
		return "Execution"
	case MetaTabGitMerge:
		return "Git & Merge"
	case MetaTabMonitors:
		return "Monitors"
	default:
		return "Unknown"
	}
}

// tabsForMode returns the ordered list of tabs for a given mode.
func tabsForMode(mode MetadataMode) []MetadataTab {
	if mode == ModeFeature {
		return []MetadataTab{
			MetaTabFeature,
			MetaTabTask,
			MetaTabExecution,
			MetaTabGitMerge,
			MetaTabMonitors,
		}
	}
	// Single and Batch modes
	return []MetadataTab{
		MetaTabTask,
		MetaTabExecution,
		MetaTabGitMerge,
	}
}

// fieldsForTab returns the fields belonging to a tab for a given mode.
func fieldsForTab(tab MetadataTab, mode MetadataMode) []MetadataField {
	switch tab {
	case MetaTabFeature:
		// Only in feature mode
		return []MetadataField{
			FieldFeaturePriority,
			FieldFeatureDependsOn,
			FieldFeatureSchedule,
			FieldFeatureStartsAt,
			FieldFeatureExpiresAt,
			FieldFeatureRunOnceAt,
			FieldFeatureTimezone,
		}
	case MetaTabTask:
		if mode == ModeFeature {
			return []MetadataField{
				FieldStatus,
				FieldPriority,
				FieldMoveToProject,
			}
		}
		// Single/Batch modes include FeatureID
		return []MetadataField{
			FieldStatus,
			FieldPriority,
			FieldFeatureID,
			FieldMoveToProject,
		}
	case MetaTabExecution:
		if mode == ModeFeature {
			// Feature mode excludes DirectPrompt
			return []MetadataField{
				FieldAgent,
				FieldModel,
				FieldExecutionMode,
				FieldTargetWorkdir,
				FieldCompleteOnIdle,
				FieldSchedule,
				FieldScheduleEnabled,
				FieldRunOnceAt,
				FieldStartsAt,
				FieldExpiresAt,
				FieldTimezone,
			}
		}
		// Single/Batch modes include DirectPrompt
		return []MetadataField{
			FieldAgent,
			FieldModel,
			FieldExecutionMode,
			FieldTargetWorkdir,
			FieldDirectPrompt,
			FieldCompleteOnIdle,
			FieldSchedule,
			FieldScheduleEnabled,
			FieldRunOnceAt,
			FieldStartsAt,
			FieldExpiresAt,
			FieldTimezone,
		}
	case MetaTabGitMerge:
		return []MetadataField{
			FieldGitBranch,
			FieldMergeTargetBranch,
			FieldMergePolicy,
			FieldMergeStrategy,
			FieldOpenPRBeforeMerge,
		}
	case MetaTabMonitors:
		// Monitors tab has no regular fields — only monitor template rows
		return []MetadataField{}
	default:
		return []MetadataField{}
	}
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
