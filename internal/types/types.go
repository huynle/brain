// Package types defines all domain types for the Brain API.
//
// These types mirror the TypeScript definitions in src/core/types.ts
// and src/api/schemas.ts, ensuring API compatibility between the
// Go and TypeScript implementations.
package types

import "time"

// =============================================================================
// Entry Types
// =============================================================================

// EntryTypes enumerates all valid brain entry types.
var EntryTypes = []string{
	"summary",
	"report",
	"walkthrough",
	"plan",
	"pattern",
	"learning",
	"idea",
	"scratch",
	"decision",
	"exploration",
	"execution",
	"task",
}

// entryTypeSet is a lookup set for O(1) validation.
var entryTypeSet = makeSet(EntryTypes)

// IsValidEntryType returns true if s is a valid entry type.
func IsValidEntryType(s string) bool {
	return entryTypeSet[s]
}

// =============================================================================
// Entry Statuses
// =============================================================================

// EntryStatuses enumerates all valid entry statuses.
var EntryStatuses = []string{
	"draft",       // Initial state, not ready
	"pending",     // Queued, waiting to be worked on
	"active",      // Ready/in use (default)
	"in_progress", // Actively being worked on
	"blocked",     // Waiting on something
	"cancelled",   // User-cancelled task
	"completed",   // Done/implemented
	"validated",   // Implementation verified working
	"superseded",  // Replaced by another entry
	"archived",    // No longer relevant
}

// entryStatusSet is a lookup set for O(1) validation.
var entryStatusSet = makeSet(EntryStatuses)

// IsValidEntryStatus returns true if s is a valid entry status.
func IsValidEntryStatus(s string) bool {
	return entryStatusSet[s]
}

// =============================================================================
// Priorities
// =============================================================================

// Priorities enumerates all valid priority levels.
var Priorities = []string{"high", "medium", "low"}

var prioritySet = makeSet(Priorities)

// IsValidPriority returns true if s is a valid priority.
func IsValidPriority(s string) bool {
	return prioritySet[s]
}

// =============================================================================
// Task Classifications
// =============================================================================

// TaskClassifications enumerates dependency resolution classifications.
var TaskClassifications = []string{
	"ready",       // Pending, all deps satisfied
	"waiting",     // Pending, waiting on incomplete deps
	"blocked",     // Blocked by blocked/cancelled deps
	"not_pending", // Task is not in pending status
}

var taskClassificationSet = makeSet(TaskClassifications)

// IsValidTaskClassification returns true if s is a valid task classification.
func IsValidTaskClassification(s string) bool {
	return taskClassificationSet[s]
}

// =============================================================================
// Generated Kinds
// =============================================================================

var GeneratedKinds = []string{"feature_checkout", "feature_review", "gap_task", "other"}

var generatedKindSet = makeSet(GeneratedKinds)

func IsValidGeneratedKind(s string) bool {
	return generatedKindSet[s]
}

// =============================================================================
// Merge / Execution Enums
// =============================================================================

var MergePolicies = []string{"prompt_only", "auto_pr", "auto_merge"}
var MergeStrategies = []string{"squash", "merge", "rebase"}
var RemoteBranchPolicies = []string{"keep", "delete"}
var ExecutionModes = []string{"worktree", "current_branch"}

// =============================================================================
// Domain Structs
// =============================================================================

// BrainEntry represents a single brain entry (note/task/plan/etc).
type BrainEntry struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Content  string   `json:"content"`
	Tags     []string `json:"tags"`
	Priority string   `json:"priority,omitempty"`

	ParentID  string   `json:"parent_id,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	FeatureID string   `json:"feature_id,omitempty"`

	Created      string `json:"created,omitempty"`
	Modified     string `json:"modified,omitempty"`
	AccessCount  int    `json:"access_count,omitempty"`
	LastVerified string `json:"last_verified,omitempty"`

	// Schedule fields
	Schedule        string `json:"schedule,omitempty"`
	ScheduleEnabled *bool  `json:"schedule_enabled,omitempty"`
	NextRun         string `json:"next_run,omitempty"`
	MaxRuns         *int   `json:"max_runs,omitempty"`
	StartsAt        string `json:"starts_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`

	// Git/execution fields
	Workdir            string `json:"workdir,omitempty"`
	GitRemote          string `json:"git_remote,omitempty"`
	GitBranch          string `json:"git_branch,omitempty"`
	MergeTargetBranch  string `json:"merge_target_branch,omitempty"`
	MergePolicy        string `json:"merge_policy,omitempty"`
	MergeStrategy      string `json:"merge_strategy,omitempty"`
	RemoteBranchPolicy string `json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool  `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      string `json:"execution_mode,omitempty"`

	// Task execution fields
	UserOriginalRequest string `json:"user_original_request,omitempty"`
	DirectPrompt        string `json:"direct_prompt,omitempty"`
	Agent               string `json:"agent,omitempty"`
	Model               string `json:"model,omitempty"`
	CompleteOnIdle      *bool  `json:"complete_on_idle,omitempty"`
	TargetWorkdir       string `json:"target_workdir,omitempty"`

	// Feature grouping
	FeaturePriority  string   `json:"feature_priority,omitempty"`
	FeatureDependsOn []string `json:"feature_depends_on,omitempty"`

	// Generated entry metadata
	Generated     *bool  `json:"generated,omitempty"`
	GeneratedKind string `json:"generated_kind,omitempty"`
	GeneratedKey  string `json:"generated_key,omitempty"`
	GeneratedBy   string `json:"generated_by,omitempty"`

	// Session tracking
	Sessions         map[string]SessionInfo     `json:"sessions,omitempty"`
	Runs             []CronRun                  `json:"runs,omitempty"`
	RunFinalizations map[string]RunFinalization `json:"run_finalizations,omitempty"`

	// Backlinks (populated on GET)
	Backlinks []BacklinkEntry `json:"backlinks,omitempty"`
}

// SessionInfo tracks session metadata.
type SessionInfo struct {
	Timestamp string `json:"timestamp"`
	CronID    string `json:"cron_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
}

// CronRun tracks a single cron execution.
type CronRun struct {
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	Started    string `json:"started"`
	Completed  string `json:"completed,omitempty"`
	Duration   *int   `json:"duration,omitempty"`
	Tasks      *int   `json:"tasks,omitempty"`
	FailedTask string `json:"failed_task,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
}

// RunFinalization is a durable run completion marker.
type RunFinalization struct {
	Status      string `json:"status"`
	FinalizedAt string `json:"finalized_at"`
	SessionID   string `json:"session_id,omitempty"`
}

// BacklinkEntry is a minimal entry reference used in graph responses.
type BacklinkEntry struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// =============================================================================
// Request / Response Types
// =============================================================================

// CreateEntryRequest is the request body for POST /entries.
type CreateEntryRequest struct {
	Type    string   `json:"type"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Status  string   `json:"status,omitempty"`

	Priority  string   `json:"priority,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	Global    *bool    `json:"global,omitempty"`
	Project   string   `json:"project,omitempty"`

	RelatedEntries []string `json:"relatedEntries,omitempty"`

	// Schedule fields
	Schedule        string `json:"schedule,omitempty"`
	ScheduleEnabled *bool  `json:"schedule_enabled,omitempty"`
	NextRun         string `json:"next_run,omitempty"`
	MaxRuns         *int   `json:"max_runs,omitempty"`
	StartsAt        string `json:"starts_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RunOnceAt       string `json:"run_once_at,omitempty"`

	// Git/execution fields
	Workdir            string `json:"workdir,omitempty"`
	GitRemote          string `json:"git_remote,omitempty"`
	GitBranch          string `json:"git_branch,omitempty"`
	MergeTargetBranch  string `json:"merge_target_branch,omitempty"`
	MergePolicy        string `json:"merge_policy,omitempty"`
	MergeStrategy      string `json:"merge_strategy,omitempty"`
	RemoteBranchPolicy string `json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool  `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      string `json:"execution_mode,omitempty"`
	CompleteOnIdle     *bool  `json:"complete_on_idle,omitempty"`

	UserOriginalRequest string   `json:"user_original_request,omitempty"`
	TargetWorkdir       string   `json:"target_workdir,omitempty"`
	FeatureID           string   `json:"feature_id,omitempty"`
	FeaturePriority     string   `json:"feature_priority,omitempty"`
	FeatureDependsOn    []string `json:"feature_depends_on,omitempty"`

	DirectPrompt string `json:"direct_prompt,omitempty"`
	Agent        string `json:"agent,omitempty"`
	Model        string `json:"model,omitempty"`

	Generated     *bool  `json:"generated,omitempty"`
	GeneratedKind string `json:"generated_kind,omitempty"`
	GeneratedKey  string `json:"generated_key,omitempty"`
	GeneratedBy   string `json:"generated_by,omitempty"`

	Runs             []CronRun                  `json:"runs,omitempty"`
	RunFinalizations map[string]RunFinalization `json:"run_finalizations,omitempty"`
}

// CreateEntryResponse is the response for POST /entries.
type CreateEntryResponse struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Link   string `json:"link"`
}

// UpdateEntryRequest is the request body for PATCH /entries/:id.
type UpdateEntryRequest struct {
	Status  *string  `json:"status,omitempty"`
	Title   *string  `json:"title,omitempty"`
	Content *string  `json:"content,omitempty"`
	Append  *string  `json:"append,omitempty"`
	Note    *string  `json:"note,omitempty"`
	Tags    []string `json:"tags,omitempty"`

	DependsOn *[]string `json:"depends_on,omitempty"`
	Priority  *string   `json:"priority,omitempty"`

	Schedule        *string `json:"schedule,omitempty"`
	ScheduleEnabled *bool   `json:"schedule_enabled,omitempty"`
	NextRun         *string `json:"next_run,omitempty"`
	MaxRuns         *int    `json:"max_runs,omitempty"`
	StartsAt        *string `json:"starts_at,omitempty"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	RunOnceAt       *string `json:"run_once_at,omitempty"`

	TargetWorkdir      *string `json:"target_workdir,omitempty"`
	GitBranch          *string `json:"git_branch,omitempty"`
	MergeTargetBranch  *string `json:"merge_target_branch,omitempty"`
	MergePolicy        *string `json:"merge_policy,omitempty"`
	MergeStrategy      *string `json:"merge_strategy,omitempty"`
	RemoteBranchPolicy *string `json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool   `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      *string `json:"execution_mode,omitempty"`
	CompleteOnIdle     *bool   `json:"complete_on_idle,omitempty"`

	FeatureID        *string   `json:"feature_id,omitempty"`
	FeaturePriority  *string   `json:"feature_priority,omitempty"`
	FeatureDependsOn *[]string `json:"feature_depends_on,omitempty"`

	DirectPrompt *string `json:"direct_prompt,omitempty"`
	Agent        *string `json:"agent,omitempty"`
	Model        *string `json:"model,omitempty"`

	Sessions         map[string]SessionInfo     `json:"sessions,omitempty"`
	Runs             []CronRun                  `json:"runs,omitempty"`
	RunFinalizations map[string]RunFinalization `json:"run_finalizations,omitempty"`

	Generated     *bool   `json:"generated,omitempty"`
	GeneratedKind *string `json:"generated_kind,omitempty"`
	GeneratedKey  *string `json:"generated_key,omitempty"`
	GeneratedBy   *string `json:"generated_by,omitempty"`
}

// ListEntriesRequest holds query parameters for GET /entries.
type ListEntriesRequest struct {
	Type      string `json:"type,omitempty"`
	Status    string `json:"status,omitempty"`
	FeatureID string `json:"feature_id,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Tags      string `json:"tags,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	Global    *bool  `json:"global,omitempty"`
	SortBy    string `json:"sortBy,omitempty"`
}

// ListEntriesResponse is the response for GET /entries.
type ListEntriesResponse struct {
	Entries []BrainEntry `json:"entries"`
	Total   int          `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
}

// MoveResult is the response for POST /entries/:id/move.
type MoveResult struct {
	Success bool   `json:"success"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// MoveEntryRequest is the request body for POST /entries/:id/move.
type MoveEntryRequest struct {
	Project string `json:"project"`
}

// SearchRequest is the request body for POST /search.
type SearchRequest struct {
	Query     string   `json:"query"`
	Type      string   `json:"type,omitempty"`
	Status    string   `json:"status,omitempty"`
	FeatureID string   `json:"feature_id,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Limit     *int     `json:"limit,omitempty"`
	Global    *bool    `json:"global,omitempty"`
}

// SearchResult is a single search result.
type SearchResult struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Snippet string `json:"snippet"`
}

// SearchResponse is the response for POST /search.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
}

// InjectRequest is the request body for POST /inject.
type InjectRequest struct {
	Query      string `json:"query"`
	Type       string `json:"type,omitempty"`
	MaxEntries *int   `json:"maxEntries,omitempty"`
}

// InjectEntry is a minimal entry reference in inject responses.
type InjectEntry struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// InjectResponse is the response for POST /inject.
type InjectResponse struct {
	Context string        `json:"context"`
	Entries []InjectEntry `json:"entries"`
	Total   int           `json:"total"`
}

// SectionHeader describes a section heading in a brain entry.
type SectionHeader struct {
	Title string `json:"title"`
	Level int    `json:"level"`
}

// SectionsResponse is the response for GET /entries/{id}/sections.
type SectionsResponse struct {
	Sections []SectionHeader `json:"sections"`
	Path     string          `json:"path"`
}

// SectionContentResponse is the response for GET /entries/{id}/sections/{title}.
type SectionContentResponse struct {
	Title              string `json:"title"`
	Content            string `json:"content"`
	Path               string `json:"path"`
	IncludeSubsections bool   `json:"includeSubsections"`
}

// VerifyResponse is the response for POST /entries/{id}/verify.
type VerifyResponse struct {
	Success    bool   `json:"success"`
	Path       string `json:"path"`
	VerifiedAt string `json:"verified_at"`
}

// LinkRequest is the request body for POST /link.
type LinkRequest struct {
	Path      string `json:"path"`
	Title     string `json:"title,omitempty"`
	WithTitle *bool  `json:"withTitle,omitempty"`
}

// LinkResponse is the response for POST /link.
type LinkResponse struct {
	Link string `json:"link"`
}

// =============================================================================
// Task Types
// =============================================================================

// ResolvedTask is a task with dependency resolution info.
type ResolvedTask struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Priority  string   `json:"priority"`
	Status    string   `json:"status"`
	ParentID  string   `json:"parent_id,omitempty"`
	DependsOn []string `json:"depends_on"`
	Created   string   `json:"created"`

	Workdir            string `json:"workdir"`
	GitRemote          string `json:"git_remote"`
	GitBranch          string `json:"git_branch"`
	MergeTargetBranch  string `json:"merge_target_branch,omitempty"`
	MergePolicy        string `json:"merge_policy,omitempty"`
	MergeStrategy      string `json:"merge_strategy,omitempty"`
	RemoteBranchPolicy string `json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool  `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      string `json:"execution_mode,omitempty"`

	FeatureID        string   `json:"feature_id,omitempty"`
	FeaturePriority  string   `json:"feature_priority,omitempty"`
	FeatureDependsOn []string `json:"feature_depends_on,omitempty"`

	DirectPrompt  string `json:"direct_prompt"`
	Agent         string `json:"agent"`
	Model         string `json:"model"`
	TargetWorkdir string `json:"target_workdir,omitempty"`

	Generated     *bool  `json:"generated,omitempty"`
	GeneratedKind string `json:"generated_kind,omitempty"`
	GeneratedKey  string `json:"generated_key,omitempty"`
	GeneratedBy   string `json:"generated_by,omitempty"`

	// Dependency resolution fields
	ResolvedDeps    []string `json:"resolved_deps"`
	UnresolvedDeps  []string `json:"unresolved_deps"`
	Classification  string   `json:"classification"`
	BlockedBy       []string `json:"blocked_by"`
	BlockedByReason string   `json:"blocked_by_reason,omitempty"`
	WaitingOn       []string `json:"waiting_on"`
	InCycle         bool     `json:"in_cycle"`
	ResolvedWorkdir string   `json:"resolved_workdir"`
}

// TaskStats holds aggregate task statistics.
type TaskStats struct {
	Total      int `json:"total"`
	Ready      int `json:"ready"`
	Waiting    int `json:"waiting"`
	Blocked    int `json:"blocked"`
	NotPending int `json:"not_pending"`
}

// TaskListResponse is the response for GET /tasks/:projectId.
type TaskListResponse struct {
	Tasks  []ResolvedTask `json:"tasks"`
	Count  int            `json:"count"`
	Stats  *TaskStats     `json:"stats,omitempty"`
	Cycles [][]string     `json:"cycles,omitempty"`
}

// TaskClaim tracks which runner has claimed a task.
type TaskClaim struct {
	RunnerID  string `json:"runnerId"`
	ClaimedAt int64  `json:"claimedAt"` // Unix millis
}

// ProjectListResponse is the response for GET /tasks.
type ProjectListResponse struct {
	Projects []string `json:"projects"`
}

// ClaimRequest is the request body for POST /tasks/:projectId/:taskId/claim.
type ClaimRequest struct {
	RunnerID string `json:"runnerId"`
}

// ClaimResponse is the response for POST /tasks/:projectId/:taskId/claim.
type ClaimResponse struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId"`
	RunnerID  string `json:"runnerId"`
	ClaimedAt string `json:"claimedAt,omitempty"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
	ClaimedBy string `json:"claimedBy,omitempty"`
	IsStale   *bool  `json:"isStale,omitempty"`
}

// ClaimStatusResponse is the response for GET /tasks/:projectId/:taskId/claim-status.
type ClaimStatusResponse struct {
	TaskID    string `json:"taskId"`
	Claimed   bool   `json:"claimed"`
	RunnerID  string `json:"runnerId,omitempty"`
	ClaimedAt string `json:"claimedAt,omitempty"`
	IsStale   bool   `json:"isStale"`
}

// MultiTaskStatusRequest is the request body for POST /tasks/:projectId/status.
type MultiTaskStatusRequest struct {
	TaskIDs []string `json:"taskIds"`
	WaitFor string   `json:"waitFor,omitempty"`
	Timeout int      `json:"timeout,omitempty"`
}

// MultiTaskStatusResponse is the response for POST /tasks/:projectId/status.
type MultiTaskStatusResponse struct {
	Tasks        []ResolvedTask `json:"tasks"`
	AllCompleted bool           `json:"allCompleted"`
}

// Feature represents a computed feature grouping of tasks.
type Feature struct {
	FeatureID string         `json:"featureId"`
	Tasks     []ResolvedTask `json:"tasks"`
	Ready     bool           `json:"ready"`
	Stats     *TaskStats     `json:"stats,omitempty"`
}

// FeatureListResponse is the response for GET /tasks/:projectId/features.
type FeatureListResponse struct {
	Features []Feature `json:"features"`
}

// FeatureResponse is the response for GET /tasks/:projectId/features/:featureId.
type FeatureResponse struct {
	Feature
}

// TriggerResponse is the response for POST /tasks/:projectId/:taskId/trigger.
type TriggerResponse struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId"`
	Triggered bool   `json:"triggered"`
}

// RunnerStatusResponse is the response for GET /tasks/runner/status.
type RunnerStatusResponse struct {
	Running        bool     `json:"running"`
	Paused         bool     `json:"paused"`
	PausedProjects []string `json:"pausedProjects"`
}

// =============================================================================
// Health / Stats Types
// =============================================================================

// HealthResponse is the response for GET /health.
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// StatsResponse is the response for GET /stats.
type StatsResponse struct {
	BrainDir       string         `json:"brainDir"`
	DBPath         string         `json:"dbPath"`
	TotalEntries   int            `json:"totalEntries"`
	GlobalEntries  int            `json:"globalEntries"`
	ProjectEntries int            `json:"projectEntries"`
	ByType         map[string]int `json:"byType"`
	OrphanCount    int            `json:"orphanCount"`
	TrackedEntries int            `json:"trackedEntries"`
	StaleCount     int            `json:"staleCount"`
}

// =============================================================================
// Error Types
// =============================================================================

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	Error   string             `json:"error"`
	Message string             `json:"message"`
	Details []ValidationDetail `json:"details,omitempty"`
}

// ValidationDetail describes a single field-level validation error.
type ValidationDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// =============================================================================
// SSE Event Types
// =============================================================================

// SSEEventType enumerates Server-Sent Event types for the task stream.
type SSEEventType string

const (
	SSEEventConnected     SSEEventType = "connected"
	SSEEventTasksSnapshot SSEEventType = "tasks_snapshot"
	SSEEventProjectDirty  SSEEventType = "project_dirty"
	SSEEventHeartbeat     SSEEventType = "heartbeat"
	SSEEventError         SSEEventType = "error"
)

// SSEEventData is the base data structure for SSE events.
type SSEEventData struct {
	Type      SSEEventType `json:"type"`
	Transport string       `json:"transport"`
	Timestamp string       `json:"timestamp"`
	ProjectID string       `json:"projectId"`
}

// SSEConnectedData is the data for a "connected" SSE event.
type SSEConnectedData struct {
	SSEEventData
}

// SSETasksSnapshotData is the data for a "tasks_snapshot" SSE event.
type SSETasksSnapshotData struct {
	SSEEventData
	Tasks  []ResolvedTask `json:"tasks"`
	Count  int            `json:"count"`
	Stats  *TaskStats     `json:"stats,omitempty"`
	Cycles [][]string     `json:"cycles,omitempty"`
}

// SSEProjectDirtyData is the data for a "project_dirty" SSE event.
type SSEProjectDirtyData struct {
	SSEEventData
}

// SSEErrorData is the data for an "error" SSE event.
type SSEErrorData struct {
	SSEEventData
	Message string `json:"message"`
}

// =============================================================================
// Helpers
// =============================================================================

func makeSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// TimeNowUTC returns the current time in UTC. Extracted for testability.
var TimeNowUTC = func() time.Time {
	return time.Now().UTC()
}
