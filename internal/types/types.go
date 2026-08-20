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
	"quirk",
	"idea",
	"scratch",
	"decision",
	"exploration",
	"execution",
	"task",
	"dream",
	"automation",
	"automation_run",
	"merge_request",
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

var GeneratedKinds = []string{"feature_checkout", "feature_review", "feature_schedule", "gap_task", "other"}

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
var Executors = []string{"opencode", "pi", "script"}

// CheckoutModes lists valid values for CheckoutMode on tasks/entries.
// "ai" (default) runs the LLM-based feature-checkout skill; "simple" triggers a
// deterministic squash-merge automation.
//
// Storage policy: we do NOT persist "ai" as a default value. Empty string is
// stored on entries that omit the field, and downstream code treats empty as
// equivalent to "ai". This preserves backward compatibility for existing tasks
// that pre-date the CheckoutMode field.
var CheckoutModes = []string{"ai", "simple"}

// IsValidCheckoutMode reports whether s is a recognized checkout mode.
// Empty string is treated as valid (defaults to "ai" downstream).
func IsValidCheckoutMode(s string) bool {
	if s == "" {
		return true
	}
	for _, v := range CheckoutModes {
		if s == v {
			return true
		}
	}
	return false
}

// =============================================================================
// Project Placement
// =============================================================================

const (
	PlacementAffinityStrict = "strict"
	PlacementAffinitySoft   = "soft"
	PlacementAffinityNone   = "none"
)

const (
	WorkspacePolicyWorktree      = "worktree"
	WorkspacePolicyCurrentBranch = "current_branch"
)

// ProjectPlacement stores Brain-owned scheduling placement policy for a project.
type ProjectPlacement struct {
	ProjectID            string            `json:"project_id"`
	Affinity             string            `json:"affinity"`
	PreferredMachines    []string          `json:"preferred_machines,omitempty"`
	AllowedMachines      []string          `json:"allowed_machines,omitempty"`
	WorkspacePolicy      string            `json:"workspace_policy,omitempty"`
	RequiredLabels       map[string]string `json:"required_labels,omitempty"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty"`
	Resources            map[string]any    `json:"resources,omitempty"`
}

// =============================================================================
// Dispatch Leases and Placement Reasons
// =============================================================================

const (
	DispatchLeaseStatePushed   = "pushed"
	DispatchLeaseStateAcked    = "acked"
	DispatchLeaseStateRejected = "rejected"
	DispatchLeaseStateExpired  = "expired"
)

// DispatchLease stores Brain-owned push dispatch state for a task assignment.
type DispatchLease struct {
	LeaseID           string `json:"leaseId"`
	ID                string `json:"id,omitempty"`
	ProjectID         string `json:"project_id"`
	TaskID            string `json:"task_id"`
	AssignedRunnerID  string `json:"assigned_runner_id"`
	AssignedMachineID string `json:"assigned_machine_id"`
	State             string `json:"state"`
	PushedAt          int64  `json:"pushed_at"`
	AckedAt           int64  `json:"acked_at,omitempty"`
	RejectedAt        int64  `json:"rejected_at,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	ExpiresAt         int64  `json:"expires_at"`
}

// DispatchAckRequest acknowledges receipt of a pushed dispatch command.
type DispatchAckRequest struct {
	LeaseID   string `json:"leaseId"`
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
}

// DispatchAckResponse reports the persisted ack state for a dispatch lease.
type DispatchAckResponse struct {
	Success   bool   `json:"success"`
	LeaseID   string `json:"leaseId"`
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
	RunnerID  string `json:"runnerId"`
	Error     string `json:"error,omitempty"`
}

// DispatchRejectReason is a structured rejection reason from a runner.
type DispatchRejectReason struct {
	Code    string            `json:"code"`
	Message string            `json:"message,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

// DispatchRejectRequest rejects a pushed dispatch command.
type DispatchRejectRequest struct {
	LeaseID   string               `json:"leaseId"`
	ProjectID string               `json:"projectId"`
	TaskID    string               `json:"taskId"`
	Reason    DispatchRejectReason `json:"reason"`
}

// DispatchRejectResponse reports the persisted reject state for a dispatch lease.
type DispatchRejectResponse struct {
	Success   bool                 `json:"success"`
	LeaseID   string               `json:"leaseId"`
	ProjectID string               `json:"projectId"`
	TaskID    string               `json:"taskId"`
	RunnerID  string               `json:"runnerId"`
	Reason    DispatchRejectReason `json:"reason"`
	Error     string               `json:"error,omitempty"`
}

// DispatchReleaseResponse reports explicit release/finalization of a dispatch lease.
type DispatchReleaseResponse struct {
	Success   bool   `json:"success"`
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
	RunnerID  string `json:"runnerId"`
	Error     string `json:"error,omitempty"`
}

// PlacementReason stores scheduler placement decision details independently from
// task content/status so dispatch decisions remain queryable after task edits.
type PlacementReason struct {
	ID             int64  `json:"id,omitempty"`
	ProjectID      string `json:"project_id"`
	TaskID         string `json:"task_id"`
	RunnerID       string `json:"runner_id,omitempty"`
	MachineID      string `json:"machine_id,omitempty"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason,omitempty"`
	RequiredLabels string `json:"required_labels,omitempty"`
	RunnerLabels   string `json:"runner_labels,omitempty"`
	MissingLabels  string `json:"missing_labels,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

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

	// Attachments contains typed references to binary artifacts associated with
	// this entry. Binary data is stored outside entry DTOs and referenced by ID.
	Attachments []AttachmentReference `json:"attachments,omitempty"`

	// EmbeddingStatus is optional semantic-search index state when reported by the API.
	// Expected values include current, missing, stale, and unknown.
	EmbeddingStatus string `json:"embedding_status,omitempty"`

	ParentID  string   `json:"parent_id,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	FeatureID string   `json:"feature_id,omitempty"`

	Created      string `json:"created,omitempty"`
	Modified     string `json:"modified,omitempty"`
	CompletedAt  string `json:"completed_at,omitempty"`
	AccessCount  int    `json:"access_count,omitempty"`
	LastVerified string `json:"last_verified,omitempty"`

	// Schedule fields
	Schedule        string `json:"schedule,omitempty"`
	ScheduleEnabled *bool  `json:"schedule_enabled,omitempty"`
	NextRun         string `json:"next_run,omitempty"`
	MaxRuns         *int   `json:"max_runs,omitempty"`
	StartsAt        string `json:"starts_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RunOnceAt       string `json:"run_once_at,omitempty"`
	Timezone        string `json:"timezone,omitempty"`

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
	SessionMode        string `json:"session_mode,omitempty"`
	CheckoutMode       string `json:"checkout_mode,omitempty"`

	// Task execution fields
	UserOriginalRequest string   `json:"user_original_request,omitempty"`
	DirectPrompt        string   `json:"direct_prompt,omitempty"`
	Agent               string   `json:"agent,omitempty"`
	Model               string   `json:"model,omitempty"`
	Executor            string   `json:"executor,omitempty"`
	Extensions          []string `json:"extensions,omitempty"`
	RequiresCapability  []string `json:"requires_capability,omitempty"`
	CompleteOnIdle      *bool    `json:"complete_on_idle,omitempty"`
	TargetWorkdir       string   `json:"target_workdir,omitempty"`

	// Feature grouping
	FeaturePriority  string   `json:"feature_priority,omitempty"`
	FeatureDependsOn []string `json:"feature_depends_on,omitempty"`

	// Feature-level schedule fields
	FeatureSchedule  string `json:"feature_schedule,omitempty"`
	FeatureStartsAt  string `json:"feature_starts_at,omitempty"`
	FeatureExpiresAt string `json:"feature_expires_at,omitempty"`
	FeatureRunOnceAt string `json:"feature_run_once_at,omitempty"`
	FeatureTimezone  string `json:"feature_timezone,omitempty"`

	// Generated entry metadata
	Generated       *bool  `json:"generated,omitempty"`
	GeneratedKind   string `json:"generated_kind,omitempty"`
	GeneratedKey    string `json:"generated_key,omitempty"`
	GeneratedBy     string `json:"generated_by,omitempty"`
	AutomationRunID string `json:"automation_run_id,omitempty"`

	// Event trigger configuration
	Trigger *TriggerConfig    `json:"trigger,omitempty"`
	Action  *AutomationAction `json:"action,omitempty"`
	Retry   *AutomationRetry  `json:"retry,omitempty"`

	// Goal automation configuration (set when generated_by=brain-goal).
	Goal *GoalConfig `json:"goal,omitempty"`

	// Session tracking
	Sessions         map[string]SessionInfo     `json:"sessions,omitempty"`
	Runs             []CronRun                  `json:"runs,omitempty"`
	RunFinalizations map[string]RunFinalization `json:"run_finalizations,omitempty"`

	// Resume-abandoned-tasks flow. ResumeRequested is set by POST /resume and
	// read by the runner at claim time to route through the IsResume prompt
	// template. Runtime-only fields — never in on-disk frontmatter, preserved
	// across re-index via service.runtimeKeys.
	ResumeRequested   bool   `json:"resume_requested,omitempty"`
	ResumeRequestedAt string `json:"resume_requested_at,omitempty"`

	// Backlinks (populated on GET)
	Backlinks []BacklinkEntry `json:"backlinks,omitempty"`
}

// SessionInfo tracks session metadata. The runner/machine/host/workdir fields
// record where an OpenCode session lives so remote control can re-open or
// resume it after the task's live instance is gone.
type SessionInfo struct {
	Timestamp string `json:"timestamp"`
	CronID    string `json:"cron_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	RunnerID  string `json:"runner_id,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
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
// Attachment Types
// =============================================================================

// Attachment represents first-class metadata for a stored binary artifact.
// It intentionally contains no canonical binary payload or base64 field.
type Attachment struct {
	ID          string              `json:"id"`
	Filename    string              `json:"filename"`
	ContentType string              `json:"content_type"`
	Size        int64               `json:"size"`
	SHA256      string              `json:"sha256,omitempty"`
	StorageKey  string              `json:"storage_key,omitempty"`
	Created     string              `json:"created,omitempty"`
	Modified    string              `json:"modified,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
	Derived     []AttachmentDerived `json:"derived,omitempty"`
}

// AttachmentReference is the entry-frontmatter/API representation for an
// attachment associated with a brain entry.
type AttachmentReference struct {
	ID          string                 `json:"id"`
	Filename    string                 `json:"filename,omitempty"`
	ContentType string                 `json:"content_type,omitempty"`
	Size        int64                  `json:"size,omitempty"`
	SHA256      string                 `json:"sha256,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
	DownloadURL string                 `json:"download_url,omitempty"`
	TextURL     string                 `json:"text_url,omitempty"`
	Role        string                 `json:"role,omitempty"`
	Caption     string                 `json:"caption,omitempty"`
	Derived     []AttachmentDerived    `json:"derived,omitempty"`
	DerivedText *AttachmentDerivedText `json:"derived_text,omitempty"`
}

// AttachmentDerived references generated artifacts such as thumbnails,
// extracted markdown, OCR text, or previews for an attachment.
type AttachmentDerived struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	StorageKey  string `json:"storage_key,omitempty"`
	Created     string `json:"created,omitempty"`
}

// AttachmentDerivedText stores extracted text and extraction status for an attachment.
type AttachmentDerivedText struct {
	ID          string            `json:"id,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Status      string            `json:"status"`
	ContentType string            `json:"content_type,omitempty"`
	Text        string            `json:"text,omitempty"`
	Error       string            `json:"error,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Created     string            `json:"created,omitempty"`
	Modified    string            `json:"modified,omitempty"`
}

// Attachment extraction statuses describe the media-to-text lifecycle.
const (
	AttachmentExtractionStatusPending = "pending"
	AttachmentExtractionStatusReady   = "ready"
	AttachmentExtractionStatusFailed  = "failed"
	AttachmentExtractionStatusSkipped = "skipped"
)

// AttachmentExtractionStatuses enumerates all valid attachment extraction statuses.
var AttachmentExtractionStatuses = []string{
	AttachmentExtractionStatusPending,
	AttachmentExtractionStatusReady,
	AttachmentExtractionStatusFailed,
	AttachmentExtractionStatusSkipped,
}

var attachmentExtractionStatusSet = makeSet(AttachmentExtractionStatuses)

// IsValidAttachmentExtractionStatus returns true if s is a valid extraction status.
func IsValidAttachmentExtractionStatus(s string) bool {
	return attachmentExtractionStatusSet[s]
}

// AttachmentExtractionRequest is the shared request shape for extracting text
// from a stored attachment. Raw bytes are carried in Content and are not JSON encoded.
type AttachmentExtractionRequest struct {
	ProjectID    string            `json:"project_id,omitempty"`
	EntryID      string            `json:"entry_id,omitempty"`
	AttachmentID string            `json:"attachment_id"`
	Filename     string            `json:"filename,omitempty"`
	ContentType  string            `json:"content_type"`
	Size         int64             `json:"size,omitempty"`
	Content      []byte            `json:"-"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// AttachmentExtractionResponse reports the result of media-to-text extraction.
type AttachmentExtractionResponse struct {
	AttachmentID string            `json:"attachment_id"`
	Status       string            `json:"status"`
	Text         string            `json:"text,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Error        string            `json:"error,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Model        string            `json:"model,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	DurationMs   int64             `json:"duration_ms,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// AttachmentLinkedEntry identifies an entry that references an attachment and
// may need dependent processing, such as embedding refresh, after extraction.
type AttachmentLinkedEntry struct {
	Path string `json:"path"`
	Role string `json:"role,omitempty"`
}

// AttachmentExtractionResult is the service-level orchestration result for
// attachment text extraction. It pairs the derived text status with linked
// entry references so downstream work can refresh searchable representations.
type AttachmentExtractionResult struct {
	Attachment    Attachment              `json:"attachment"`
	DerivedText   AttachmentDerivedText   `json:"derived_text"`
	LinkedEntries []AttachmentLinkedEntry `json:"linked_entries,omitempty"`
}

// AttachmentExtractionBackfillRequest configures project-level attachment text extraction.
type AttachmentExtractionBackfillRequest struct {
	DryRun           bool `json:"dry_run,omitempty"`
	Force            bool `json:"force,omitempty"`
	BatchSize        int  `json:"batch_size,omitempty"`
	RateLimitDelayMs int  `json:"rate_limit_delay_ms,omitempty"`
}

// AttachmentExtractionBackfillItem reports one attachment considered by extraction backfill.
type AttachmentExtractionBackfillItem struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	Skipped      bool   `json:"skipped,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// AttachmentExtractionBackfillResponse summarizes project-level extraction backfill.
type AttachmentExtractionBackfillResponse struct {
	Total       int                                `json:"total"`
	Candidates  int                                `json:"candidates"`
	Processed   int                                `json:"processed"`
	Skipped     int                                `json:"skipped"`
	Failed      int                                `json:"failed"`
	DryRun      bool                               `json:"dry_run,omitempty"`
	Attachments []AttachmentExtractionBackfillItem `json:"attachments,omitempty"`
}

// CreateAttachmentRequest is metadata submitted before/with an attachment upload.
// The binary payload is transported separately; do not add base64 fields here.
type CreateAttachmentRequest struct {
	Filename    string            `json:"filename"`
	ContentType string            `json:"content_type"`
	Size        int64             `json:"size,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CreateAttachmentResponse is returned after attachment metadata/storage is created.
type CreateAttachmentResponse struct {
	Attachment Attachment `json:"attachment"`
}

// AttachEntryAttachmentRequest associates an existing attachment with an entry.
type AttachEntryAttachmentRequest struct {
	Attachment AttachmentReference `json:"attachment"`
}

// AttachEntryAttachmentResponse reports an entry attachment association.
type AttachEntryAttachmentResponse struct {
	EntryID     string                `json:"entry_id"`
	Path        string                `json:"path"`
	Attachments []AttachmentReference `json:"attachments"`
}

// ListAttachmentsResponse is the response for listing stored attachments.
type ListAttachmentsResponse struct {
	Attachments []Attachment `json:"attachments"`
	Total       int          `json:"total"`
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

	Attachments []AttachmentReference `json:"attachments,omitempty"`

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
	Timezone        string `json:"timezone,omitempty"`

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
	SessionMode        string `json:"session_mode,omitempty"`
	CompleteOnIdle     *bool  `json:"complete_on_idle,omitempty"`
	CheckoutMode       string `json:"checkout_mode,omitempty"`

	UserOriginalRequest string   `json:"user_original_request,omitempty"`
	TargetWorkdir       string   `json:"target_workdir,omitempty"`
	Executor            string   `json:"executor,omitempty"`
	Extensions          []string `json:"extensions,omitempty"`
	FeatureID           string   `json:"feature_id,omitempty"`
	FeaturePriority     string   `json:"feature_priority,omitempty"`
	FeatureDependsOn    []string `json:"feature_depends_on,omitempty"`
	FeatureSchedule     string   `json:"feature_schedule,omitempty"`
	FeatureStartsAt     string   `json:"feature_starts_at,omitempty"`
	FeatureExpiresAt    string   `json:"feature_expires_at,omitempty"`
	FeatureRunOnceAt    string   `json:"feature_run_once_at,omitempty"`
	FeatureTimezone     string   `json:"feature_timezone,omitempty"`

	DirectPrompt string `json:"direct_prompt,omitempty"`
	Agent        string `json:"agent,omitempty"`
	Model        string `json:"model,omitempty"`

	Generated       *bool  `json:"generated,omitempty"`
	GeneratedKind   string `json:"generated_kind,omitempty"`
	GeneratedKey    string `json:"generated_key,omitempty"`
	GeneratedBy     string `json:"generated_by,omitempty"`
	AutomationRunID string `json:"automation_run_id,omitempty"`

	Trigger *TriggerConfig    `json:"trigger,omitempty"`
	Action  *AutomationAction `json:"action,omitempty"`
	Retry   *AutomationRetry  `json:"retry,omitempty"`
	Goal    *GoalConfig       `json:"goal,omitempty"`

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

	Attachments *[]AttachmentReference `json:"attachments,omitempty"`

	DependsOn *[]string `json:"depends_on,omitempty"`
	Priority  *string   `json:"priority,omitempty"`

	Schedule        *string `json:"schedule,omitempty"`
	ScheduleEnabled *bool   `json:"schedule_enabled,omitempty"`
	NextRun         *string `json:"next_run,omitempty"`
	MaxRuns         *int    `json:"max_runs,omitempty"`
	StartsAt        *string `json:"starts_at,omitempty"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	RunOnceAt       *string `json:"run_once_at,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`

	TargetWorkdir      *string   `json:"target_workdir,omitempty"`
	Workdir            *string   `json:"workdir,omitempty"`
	GitBranch          *string   `json:"git_branch,omitempty"`
	GitRemote          *string   `json:"git_remote,omitempty"`
	MergeTargetBranch  *string   `json:"merge_target_branch,omitempty"`
	MergePolicy        *string   `json:"merge_policy,omitempty"`
	MergeStrategy      *string   `json:"merge_strategy,omitempty"`
	RemoteBranchPolicy *string   `json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool     `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      *string   `json:"execution_mode,omitempty"`
	CompleteOnIdle     *bool     `json:"complete_on_idle,omitempty"`
	Executor           *string   `json:"executor,omitempty"`
	Extensions         *[]string `json:"extensions,omitempty"`
	CheckoutMode       *string   `json:"checkout_mode,omitempty"`

	FeatureID        *string   `json:"feature_id,omitempty"`
	FeaturePriority  *string   `json:"feature_priority,omitempty"`
	FeatureDependsOn *[]string `json:"feature_depends_on,omitempty"`

	// Feature schedule fields (trigger auto-creation of a feature_schedule gate task)
	FeatureSchedule  *string `json:"feature_schedule,omitempty"`
	FeatureRunOnceAt *string `json:"feature_run_once_at,omitempty"`
	FeatureStartsAt  *string `json:"feature_starts_at,omitempty"`
	FeatureExpiresAt *string `json:"feature_expires_at,omitempty"`
	FeatureTimezone  *string `json:"feature_timezone,omitempty"`

	DirectPrompt        *string `json:"direct_prompt,omitempty"`
	UserOriginalRequest *string `json:"user_original_request,omitempty"`
	Agent               *string `json:"agent,omitempty"`
	Model               *string `json:"model,omitempty"`

	Sessions         map[string]SessionInfo     `json:"sessions,omitempty"`
	Runs             []CronRun                  `json:"runs,omitempty"`
	RunFinalizations map[string]RunFinalization `json:"run_finalizations,omitempty"`

	Generated       *bool   `json:"generated,omitempty"`
	GeneratedKind   *string `json:"generated_kind,omitempty"`
	GeneratedKey    *string `json:"generated_key,omitempty"`
	GeneratedBy     *string `json:"generated_by,omitempty"`
	AutomationRunID *string `json:"automation_run_id,omitempty"`

	Trigger *TriggerConfig    `json:"trigger,omitempty"`
	Action  *AutomationAction `json:"action,omitempty"`
	Retry   *AutomationRetry  `json:"retry,omitempty"`
	Goal    *GoalConfig       `json:"goal,omitempty"`
}

// =============================================================================
// Bulk Update Types
// =============================================================================

// BulkUpdateFilter selects entries to update by matching criteria.
type BulkUpdateFilter struct {
	FeatureID *string  `json:"feature_id,omitempty"`
	Project   *string  `json:"project,omitempty"`
	Type      *string  `json:"type,omitempty"`
	Status    *string  `json:"status,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Priority  *string  `json:"priority,omitempty"`

	// Task-specific filters (mirror TaskEntry / BrainEntry fields).
	// Applied as an in-memory pass over the storage list because the storage
	// layer does not index these fields.
	GeneratedBy   *string `json:"generated_by,omitempty"`
	GeneratedKey  *string `json:"generated_key,omitempty"`
	Agent         *string `json:"agent,omitempty"`
	Executor      *string `json:"executor,omitempty"`
	ExecutionMode *string `json:"execution_mode,omitempty"`
}

// BulkUpdateEntry targets a specific entry with updates.
type BulkUpdateEntry struct {
	Path    string             `json:"path"`
	Updates UpdateEntryRequest `json:"updates"`
}

// BulkUpdateRequest supports two modes: filter-based or explicit entries.
// Must have either (Filter + Updates) or Entries, not both.
type BulkUpdateRequest struct {
	Filter  *BulkUpdateFilter   `json:"filter,omitempty"`
	Updates *UpdateEntryRequest `json:"updates,omitempty"`
	Entries []BulkUpdateEntry   `json:"entries,omitempty"`
	DryRun  bool                `json:"dry_run,omitempty"`
	Limit   int                 `json:"limit,omitempty"` // default 100, max 100

	// Force bypasses the live-claim guard: without it, a live run that
	// would touch a task currently being executed by an online runner is
	// refused with 409. Only the handler reads this; the service layer
	// ignores it.
	Force bool `json:"force,omitempty"`
}

// BulkUpdateResult represents the outcome of a single entry update.
type BulkUpdateResult struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // "ok" or "error"
	Error  string `json:"error,omitempty"`
}

// BulkUpdateResponse is the response for POST /entries/bulk-update.
type BulkUpdateResponse struct {
	Updated int                `json:"updated"`
	Failed  int                `json:"failed"`
	Total   int                `json:"total"`
	DryRun  bool               `json:"dry_run"`
	Results []BulkUpdateResult `json:"results"`

	// Truncated is true when the filter matched more entries than the
	// safety cap allowed us to touch. Historically this truncation was
	// silent, so a caller updating a >100-task feature saw a success
	// response having mutated only the first 100. Callers should treat
	// Truncated=true on a dry run as "do not proceed; narrow the filter".
	Truncated bool `json:"truncated,omitempty"`

	// MatchedTotal is how many entries the filter matched before the cap
	// was applied. On a dry run this is exact up to the candidate fetch
	// limit; on a live run it is a lower bound (we stop counting shortly
	// past the cap). Zero in explicit-entries mode, where no matching
	// happens.
	MatchedTotal int `json:"matched_total,omitempty"`
}

// BulkDeleteRequest mirrors BulkUpdateRequest for deletions. Filter mode
// selects entries the same way; explicit mode takes concrete paths.
// Exactly one of Filter / Paths must be set.
//
// There is deliberately no "updates" analogue: deletion has no payload.
type BulkDeleteRequest struct {
	Filter *BulkUpdateFilter `json:"filter,omitempty"`
	Paths  []string          `json:"paths,omitempty"`
	DryRun bool              `json:"dry_run,omitempty"`
	Limit  int               `json:"limit,omitempty"` // default 100, max 100

	// Force bypasses the live-claim guard (see BulkUpdateRequest.Force).
	// An explicit "force" key in the body wins over the legacy ?force=true
	// query param. Only the handler reads this.
	Force bool `json:"force,omitempty"`
}

// BulkDeleteResponse is the response for POST /entries/bulk-delete.
// Deleted/Failed/Total/Results mirror the bulk-update shape so clients can
// render both with one component.
type BulkDeleteResponse struct {
	Deleted int                `json:"deleted"`
	Failed  int                `json:"failed"`
	Total   int                `json:"total"`
	DryRun  bool               `json:"dry_run"`
	Results []BulkUpdateResult `json:"results"`

	// See BulkUpdateResponse for semantics.
	Truncated    bool `json:"truncated,omitempty"`
	MatchedTotal int  `json:"matched_total,omitempty"`
}

// LiveClaim describes an active runner claim on a task. Returned by
// TaskLivenessService so callers can refuse destructive operations on work
// that is genuinely in flight.
type LiveClaim struct {
	// Live is true only when a claim exists, has not expired, AND the
	// owning runner is currently online. A claim held by an offline or
	// crashed runner is not live — that is exactly the abandoned case
	// the resume flow exists to recover.
	Live bool `json:"live"`
	// RunnerID owning the claim. Empty when Live is false.
	RunnerID string `json:"runnerId,omitempty"`
}

// ListEntriesRequest holds query parameters for GET /entries.
type ListEntriesRequest struct {
	Type      string   `json:"type,omitempty"`
	Status    string   `json:"status,omitempty"`
	FeatureID string   `json:"feature_id,omitempty"`
	Filename  string   `json:"filename,omitempty"`
	Tags      string   `json:"tags,omitempty"`
	Include   []string `json:"include,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Offset    int      `json:"offset,omitempty"`
	Global    *bool    `json:"global,omitempty"`
	SortBy    string   `json:"sortBy,omitempty"`
	Project   string   `json:"project,omitempty"`
	SortOrder string   `json:"sortOrder,omitempty"`
	Priority  string   `json:"priority,omitempty"`
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
	OldPath string `json:"oldPath"` // Same as From, for client compatibility
	NewPath string `json:"newPath"` // Same as To, for client compatibility
	Project string `json:"project"` // Target project ID
	ID      string `json:"id"`      // Entry ID (filename without .md)
	Title   string `json:"title"`   // Entry title
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
	Include   []string `json:"include,omitempty"`
	Limit     *int     `json:"limit,omitempty"`
	Global    *bool    `json:"global,omitempty"`
	Project   string   `json:"project,omitempty"`
	Strategy  string   `json:"strategy,omitempty"`
	Priority  string   `json:"priority,omitempty"`
}

// SearchResult is a single search result.
type SearchResult struct {
	ID          string                `json:"id"`
	Path        string                `json:"path"`
	Title       string                `json:"title"`
	Type        string                `json:"type"`
	Status      string                `json:"status"`
	Snippet     string                `json:"snippet"`
	MatchSource string                `json:"match_source,omitempty"`
	Attachments []AttachmentReference `json:"attachments,omitempty"`
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
	Project    string `json:"project,omitempty"`
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
	Content   string   `json:"content,omitempty"`
	Priority  string   `json:"priority"`
	Status    string   `json:"status"`
	ParentID  string   `json:"parent_id,omitempty"`
	DependsOn []string `json:"depends_on"`
	Created   string   `json:"created"`
	// Modified/CompletedAt power the PWA's history ordering. Modified was
	// historically declared by the web Task type but never sent.
	Modified    string `json:"modified,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`

	Workdir            string `json:"workdir"`
	GitRemote          string `json:"git_remote"`
	GitBranch          string `json:"git_branch"`
	MergeTargetBranch  string `json:"merge_target_branch,omitempty"`
	MergePolicy        string `json:"merge_policy,omitempty"`
	MergeStrategy      string `json:"merge_strategy,omitempty"`
	RemoteBranchPolicy string `json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool  `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      string `json:"execution_mode,omitempty"`
	CheckoutMode       string `json:"checkout_mode,omitempty"`

	FeatureID        string   `json:"feature_id,omitempty"`
	FeaturePriority  string   `json:"feature_priority,omitempty"`
	FeatureDependsOn []string `json:"feature_depends_on,omitempty"`

	// Feature-level schedule fields
	FeatureSchedule  string `json:"feature_schedule,omitempty"`
	FeatureStartsAt  string `json:"feature_starts_at,omitempty"`
	FeatureExpiresAt string `json:"feature_expires_at,omitempty"`
	FeatureRunOnceAt string `json:"feature_run_once_at,omitempty"`
	FeatureTimezone  string `json:"feature_timezone,omitempty"`

	// Schedule fields
	Schedule        string    `json:"schedule,omitempty"`
	ScheduleEnabled *bool     `json:"schedule_enabled,omitempty"`
	NextRun         string    `json:"next_run,omitempty"`
	MaxRuns         *int      `json:"max_runs,omitempty"`
	StartsAt        string    `json:"starts_at,omitempty"`
	ExpiresAt       string    `json:"expires_at,omitempty"`
	RunOnceAt       string    `json:"run_once_at,omitempty"`
	Timezone        string    `json:"timezone,omitempty"`
	Runs            []CronRun `json:"runs,omitempty"`

	UserOriginalRequest string            `json:"user_original_request,omitempty"`
	DirectPrompt        string            `json:"direct_prompt"`
	Agent               string            `json:"agent"`
	Model               string            `json:"model"`
	Executor            string            `json:"executor,omitempty"`
	Extensions          []string          `json:"extensions,omitempty"`
	CompleteOnIdle      *bool             `json:"complete_on_idle,omitempty"`
	TargetWorkdir       string            `json:"target_workdir,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`

	// Session tracking
	Sessions map[string]SessionInfo `json:"sessions,omitempty"`

	// Tags from the brain entry (used for capability-based routing)
	Tags []string `json:"tags,omitempty"`

	// RequiresCapability specifies capabilities a runner must have to claim this task.
	// Tasks without this field are claimable by any runner (backward compatible).
	RequiresCapability []string `json:"requires_capability,omitempty"`

	Generated     *bool  `json:"generated,omitempty"`
	GeneratedKind string `json:"generated_kind,omitempty"`
	GeneratedKey  string `json:"generated_key,omitempty"`
	GeneratedBy   string `json:"generated_by,omitempty"`

	// Event trigger configuration
	Trigger *TriggerConfig    `json:"trigger,omitempty"`
	Action  *AutomationAction `json:"action,omitempty"`
	Retry   *AutomationRetry  `json:"retry,omitempty"`

	// Dependency resolution fields
	ResolvedDeps    []string `json:"resolved_deps"`
	UnresolvedDeps  []string `json:"unresolved_deps"`
	Classification  string   `json:"classification"`
	BlockedBy       []string `json:"blocked_by"`
	BlockedByReason string   `json:"blocked_by_reason,omitempty"`
	WaitingOn       []string `json:"waiting_on"`
	InCycle         bool     `json:"in_cycle"`
	ResolvedWorkdir string   `json:"resolved_workdir"`

	// Dispatch diagnostics expose scheduler push state and placement decisions.
	DispatchLease       *DispatchLease    `json:"dispatch_lease,omitempty"`
	PlacementReasons    []PlacementReason `json:"placement_reasons,omitempty"`
	LastPlacementReason *PlacementReason  `json:"last_placement_reason,omitempty"`

	// Abandonment surface for the resume-abandoned-tasks flow. Derived
	// server-side from task_claims + runners.status + reaper metadata by
	// enrichAbandonmentState — never written directly by clients. When
	// IsAbandoned is true, AbandonReason names the underlying cause and the
	// PWA renders a Resume affordance.
	IsAbandoned   bool   `json:"is_abandoned,omitempty"`
	AbandonReason string `json:"abandon_reason,omitempty"`

	// ResumeRequested is the durable flag written by POST /resume. The runner
	// reads this at claim time and passes IsResume=true to the executor's
	// prompt builder, then clears the flag via PATCH so re-polls don't loop-
	// resume the same task forever.
	ResumeRequested   bool   `json:"resume_requested,omitempty"`
	ResumeRequestedAt string `json:"resume_requested_at,omitempty"`
}

// TaskStats holds aggregate task statistics.
//
// Blocked and StatusBlocked are intentionally separate counters (see task
// ghtzzp1x / plan 24urhmtl#Finding-4):
//
//   - Blocked (json: "blocked") counts tasks whose dependency Classification
//     is "blocked" (unmet deps, blocked-by another task, or in a cycle).
//     Kept named "blocked" for wire compatibility with existing clients.
//   - StatusBlocked (json: "status_blocked") counts tasks whose Status field
//     equals "blocked" (set explicitly by user/agent).
//
// A single task may be in both counters simultaneously — they are not
// mutually exclusive.
type TaskStats struct {
	Total         int `json:"total"`
	Ready         int `json:"ready"`
	Waiting       int `json:"waiting"`
	Blocked       int `json:"blocked"`
	StatusBlocked int `json:"status_blocked"`
	NotPending    int `json:"not_pending"`
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

// ReleaseRequest is the request body for POST /tasks/:projectId/:taskId/release.
// The optional Reason field surfaces as event.Reason and event.Metadata["reason"]
// on the emitted task.released event so consumers reading either field see the
// same diagnostic (parity with runner-emitted task.released events).
type ReleaseRequest struct {
	RunnerID string `json:"runnerId"`
	Reason   string `json:"reason,omitempty"`
}

// DispatchRequest is the request body for POST /tasks/:projectId/:taskId/dispatch.
type DispatchRequest struct {
	TargetRunnerID string `json:"targetRunnerId"`
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

// DispatchResponse is the response for POST /tasks/:projectId/:taskId/dispatch.
type DispatchResponse struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId"`
	RunnerID  string `json:"runnerId"`
	LeaseID   string `json:"leaseId,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
	ClaimedBy string `json:"claimedBy,omitempty"`
	IsStale   *bool  `json:"isStale,omitempty"`
}

// RunTaskRequest is the request body for POST /tasks/:projectId/:taskId/run.
//
// Run is the user-explicit "execute this task now" path used by the PWA "x"
// shortcut. It mirrors the TUI's runner-controller dispatch: pick an eligible
// runner automatically, create a dispatch lease, and push a dispatch command
// over the realtime hub. When Force is true, the dispatch is accepted even
// for paused projects (the user explicitly chose this task).
type RunTaskRequest struct {
	Force bool `json:"force,omitempty"`
}

// RunTaskResponse is the response for POST /tasks/:projectId/:taskId/run.
//
// Dispatched indicates whether a runner was assigned and the dispatch command
// was published. When false, Reason carries a short machine-readable token
// (e.g. "no_online_runner", "task_not_ready", "all_runners_at_capacity")
// the UI can branch on.
type RunTaskResponse struct {
	Dispatched bool   `json:"dispatched"`
	TaskID     string `json:"taskId"`
	ProjectID  string `json:"projectId"`
	RunnerID   string `json:"runnerId,omitempty"`
	LeaseID    string `json:"leaseId,omitempty"`
	LeaseState string `json:"leaseState,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// RunFeatureRequest is the request body for POST /tasks/:projectId/features/:featureId/run.
//
// This is the user-explicit "execute this entire feature now" path. The server
// iterates every ready task in the feature and dispatches as many as runner
// capacity allows; leftover ready tasks are marked for a feature-scoped manual
// cascade so they auto-dispatch as task.completed events fire — even when the
// project is paused. Pause is unconditionally bypassed (mirrors RunTaskRequest).
type RunFeatureRequest struct {
	Force bool `json:"force,omitempty"`
}

// RunFeatureResponse is the response for POST /tasks/:projectId/features/:featureId/run.
//
// Dispatched indicates whether at least one task was dispatched in this call.
// Results contains one RunTaskResponse per ready task attempted, in the order
// they were considered. Queued lists task IDs the server is holding for the
// manual cascade (they did not dispatch this call but will fire as slots free).
// Reason carries a feature-level token when nothing could be done (e.g.
// "feature_not_found", "no_ready_tasks", "feature_in_progress").
type RunFeatureResponse struct {
	Dispatched      bool              `json:"dispatched"`
	ProjectID       string            `json:"projectId"`
	FeatureID       string            `json:"featureId"`
	Results         []RunTaskResponse `json:"results,omitempty"`
	Queued          []string          `json:"queued,omitempty"`
	DispatchedCount int               `json:"dispatchedCount"`
	SkippedCount    int               `json:"skippedCount"`
	Reason          string            `json:"reason,omitempty"`
	Detail          string            `json:"detail,omitempty"`
	CascadeActive   bool              `json:"cascadeActive,omitempty"`
}

// RunProjectRequest is the body for POST /tasks/:projectId/run.
type RunProjectRequest struct {
	Force bool `json:"force,omitempty"`
}

// RunProjectResponse is the response for POST /tasks/:projectId/run — a
// project-scoped fanout that runs every ready feature in the project. The
// server iterates the feature-ready list and calls RunFeatureNow per feature.
// Features that had no ready tasks appear in the results with a reason;
// aggregated dispatch counts summarize the batch. Non-fatal per-feature
// errors surface as skipped entries so partial success doesn't fail the batch.
type RunProjectResponse struct {
	ProjectID           string                `json:"projectId"`
	FeaturesConsidered  int                   `json:"featuresConsidered"`
	FeaturesDispatched  int                   `json:"featuresDispatched"`
	FeaturesSkipped     int                   `json:"featuresSkipped"`
	TotalTasksDispatched int                  `json:"totalTasksDispatched"`
	Results             []RunFeatureResponse `json:"results,omitempty"`
	Reason              string                `json:"reason,omitempty"`
}

// ClaimStatusResponse is the response for GET /tasks/:projectId/:taskId/claim-status.
type ClaimStatusResponse struct {
	TaskID    string `json:"taskId"`
	Claimed   bool   `json:"claimed"`
	RunnerID  string `json:"runnerId,omitempty"`
	ClaimedAt string `json:"claimedAt,omitempty"`
	IsStale   bool   `json:"isStale"`
}

// RenewClaimResponse is the response for POST /tasks/:projectId/:taskId/renew.
type RenewClaimResponse struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId"`
	RunnerID  string `json:"runnerId"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Error     string `json:"error,omitempty"`
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
// The API returns {"feature": {...}}, so we use a wrapper field.
type FeatureResponse struct {
	Feature Feature `json:"feature"`
}

// TriggerResponse is the response for POST /tasks/:projectId/:taskId/trigger.
type TriggerResponse struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId"`
	Triggered bool   `json:"triggered"`
	RunID     string `json:"runId,omitempty"`   // ID of the created run
	NextRun   string `json:"nextRun,omitempty"` // when the next run is scheduled
	Reason    string `json:"reason,omitempty"`  // why trigger was skipped
}

// FeatureCheckoutOptions contains options for creating a checkout task.
type FeatureCheckoutOptions struct {
	ExecutionBranch    string `json:"execution_branch,omitempty"`
	MergeTargetBranch  string `json:"merge_target_branch,omitempty"`
	MergePolicy        string `json:"merge_policy,omitempty"`         // "prompt_only", "auto_pr", "auto_merge"
	MergeStrategy      string `json:"merge_strategy,omitempty"`       // "squash", "merge", "rebase"
	RemoteBranchPolicy string `json:"remote_branch_policy,omitempty"` // "keep", "delete"
	OpenPRBeforeMerge  bool   `json:"open_pr_before_merge,omitempty"`
	ExecutionMode      string `json:"execution_mode,omitempty"` // "worktree", "current_branch"
	CheckoutMode       string `json:"checkout_mode,omitempty"`  // "ai" (default) or "simple"
}

// CheckoutFeatureResult is the response for CheckoutFeature.
type CheckoutFeatureResult struct {
	Created      bool                 `json:"created"`
	GeneratedKey string               `json:"generatedKey"`
	Task         *CreateEntryResponse `json:"task,omitempty"`
}

// ResumeTaskOptions is the request body for POST /tasks/{project}/{task}/resume.
// Force=true bypasses the IsAbandoned gate — required to resume a task whose
// runtime state still looks live (unexpired claim on an online runner).
type ResumeTaskOptions struct {
	Force bool `json:"force,omitempty"`
}

// ResumeTaskResult is the outcome of a resume request. When Resumed=false the
// call was a no-op and Reason names why (idempotent replay, not-abandoned, or
// terminal status). Callers should surface Reason to the user rather than
// treating this as an error.
type ResumeTaskResult struct {
	TaskID             string `json:"task_id"`
	Resumed            bool   `json:"resumed"`
	PriorStatus        string `json:"prior_status,omitempty"`
	PriorSessionsCount int    `json:"prior_sessions_count,omitempty"`
	AbandonReason      string `json:"abandon_reason,omitempty"`
	Reason             string `json:"reason,omitempty"` // populated when Resumed=false
}

// ResumeFeatureResult is the response for POST /features/{featureId}/resume.
// Each task in the feature gets its own ResumeTaskResult in Results.
// TotalResumed / TotalSkipped are convenience counters — computed over
// the full loop and NOT bounded by any client-side cap on Results. When
// the per-task detail list is truncated for response-size reasons,
// Truncated is set to true and TotalResults carries the pre-truncation
// count; clients that need the complete audit trail fall back to
// GET /tasks?feature_id=X. A single task that errored during processing
// is reported as a skipped result with Reason describing the error, so
// partial failures don't fail the whole batch.
type ResumeFeatureResult struct {
	FeatureID    string             `json:"feature_id"`
	TotalResumed int                `json:"total_resumed"`
	TotalSkipped int                `json:"total_skipped"`
	Results      []ResumeTaskResult `json:"results"`
	// Truncated is true when Results was capped below its natural length.
	// TotalResumed / TotalSkipped remain authoritative regardless.
	Truncated bool `json:"truncated,omitempty"`
	// TotalResults is len(Results) before any client-side cap. Populated
	// only when Truncated=true so callers know how much detail was dropped.
	TotalResults int `json:"total_results,omitempty"`
}

// RunnerStatusResponse is the response for GET /tasks/runner/status.
type RunnerStatusResponse struct {
	Running                  bool     `json:"running"`
	Paused                   bool     `json:"paused"`
	PausedProjects           []string `json:"pausedProjects"`
	AutomationsPaused        bool     `json:"automationsPaused"`
	AutomationPausedProjects []string `json:"automationPausedProjects"`
}

// =============================================================================
// Runner Registry Types
// =============================================================================

// RunnerStatus represents the computed status of a runner based on heartbeat age.
type RunnerStatus string

const (
	RunnerStatusOnline  RunnerStatus = "online"
	RunnerStatusStale   RunnerStatus = "stale"
	RunnerStatusOffline RunnerStatus = "offline"
)

// RunnerRegistration is the request body for POST /runners (register).
type RunnerRegistration struct {
	RunnerID       string                 `json:"runner_id"`
	MachineID      string                 `json:"machine_id,omitempty"`
	Hostname       string                 `json:"hostname"`
	Labels         map[string]string      `json:"labels,omitempty"`
	Executors      []string               `json:"executors,omitempty"`
	Capabilities   []string               `json:"capabilities,omitempty"`
	DispatchPush   bool                   `json:"dispatch_push,omitempty"`
	WorkspaceRoots []string               `json:"workspace_roots,omitempty"`
	Projects       []string               `json:"projects,omitempty"`
	Resources      map[string]interface{} `json:"resources,omitempty"`
	Capacity       map[string]interface{} `json:"capacity,omitempty"`
	Draining       bool                   `json:"draining,omitempty"`
	MaxParallel    int                    `json:"max_parallel,omitempty"`
}

// RunnerHeartbeatRequest is the request body for POST /runners/:id/heartbeat.
type RunnerHeartbeatRequest struct {
	RunningTasks   int                    `json:"running_tasks"`
	Stats          map[string]interface{} `json:"stats,omitempty"`
	DispatchPush   *bool                  `json:"dispatch_push,omitempty"`
	Labels         map[string]string      `json:"labels,omitempty"`
	WorkspaceRoots []string               `json:"workspace_roots,omitempty"`
	Projects       []string               `json:"projects,omitempty"`
	Resources      map[string]interface{} `json:"resources,omitempty"`
	Capacity       map[string]interface{} `json:"capacity,omitempty"`
	Draining       *bool                  `json:"draining,omitempty"`

	// Instances is a full reconcile list of OpenCode instances managed by this
	// runner. nil means the runner does not report instances (older runners);
	// an empty list means the runner has no instances.
	Instances []OpencodeInstance `json:"instances"`
}

// OpencodeInstance describes an OpenCode HTTP server instance managed by a
// runner. The port is informational only — the API never dials it directly;
// all access goes through the runner's bridge connection.
type OpencodeInstance struct {
	InstanceID string   `json:"instance_id"`
	RunnerID   string   `json:"runner_id"`
	Hostname   string   `json:"hostname,omitempty"`
	Kind       string   `json:"kind"` // "task" | "adhoc"
	ProjectID  string   `json:"project_id,omitempty"`
	TaskID     string   `json:"task_id,omitempty"`
	FeatureID  string   `json:"feature_id,omitempty"`
	Priority   string   `json:"priority,omitempty"`
	Title      string   `json:"title,omitempty"`
	Workdir    string   `json:"workdir,omitempty"`
	Port       int      `json:"port,omitempty"`
	PID        int      `json:"pid,omitempty"`
	SessionIDs []string `json:"session_ids,omitempty"`
	Status     string   `json:"status"` // "starting" | "idle" | "busy" | "exited"
	Executor   string   `json:"executor,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	Model      string   `json:"model,omitempty"`
	StartedAt  int64    `json:"started_at,omitempty"` // Unix milliseconds
	LastSeen   int64    `json:"last_seen,omitempty"`  // Unix milliseconds

	// Live decorations merged from the bridge hub on read paths; not persisted.
	PendingPermissions int  `json:"pending_permissions,omitempty"`
	BridgeConnected    bool `json:"bridge_connected,omitempty"`
}

// Instance kinds.
const (
	InstanceKindTask  = "task"
	InstanceKindAdhoc = "adhoc"
)

// Instance statuses.
const (
	InstanceStatusStarting = "starting"
	InstanceStatusIdle     = "idle"
	InstanceStatusBusy     = "busy"
	InstanceStatusExited   = "exited"
)

// InstanceListResponse is the response for instance list endpoints.
type InstanceListResponse struct {
	Instances []OpencodeInstance `json:"instances"`
	Total     int                `json:"total"`
}

// SpawnInstanceSpec describes an ad-hoc OpenCode instance to spawn on a
// runner. The workdir must be absolute and under the runner's allowed roots.
type SpawnInstanceSpec struct {
	Workdir string `json:"workdir"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
	Title   string `json:"title,omitempty"`
}

// RunnerInfo is the API-level runner representation with computed status.
//
// Paused is the runner-scoped pause dial, owned by the API server and toggled
// through PUT /runners/{runnerId}/pause|resume. It is NOT reported by the
// runner on registration or heartbeat — a runner must never be able to resume
// itself by restarting. The scheduler treats a paused runner as ineligible for
// dispatch, and the runner reconciles its own dial against this field.
type RunnerInfo struct {
	RunnerID           string                      `json:"runner_id"`
	MachineID          string                      `json:"machine_id,omitempty"`
	Hostname           string                      `json:"hostname"`
	BridgeConnected    bool                        `json:"bridge_connected,omitempty"`
	Labels             map[string]string           `json:"labels,omitempty"`
	Executors          []string                    `json:"executors,omitempty"`
	Projects           []string                    `json:"projects,omitempty"`
	Capabilities       []string                    `json:"capabilities,omitempty"`
	DispatchPush       bool                        `json:"dispatch_push,omitempty"`
	WorkspaceRoots     []string                    `json:"workspace_roots,omitempty"`
	Resources          map[string]interface{}      `json:"resources,omitempty"`
	Capacity           map[string]interface{}      `json:"capacity,omitempty"`
	Draining           bool                        `json:"draining,omitempty"`
	Paused             bool                        `json:"paused,omitempty"`
	MaxParallel        int                         `json:"max_parallel"`
	ActiveTasks        int                         `json:"active_tasks,omitempty"`
	FeatureIDs         string                      `json:"feature_ids,omitempty"`
	FeatureAssignments []FeatureAssignmentResponse `json:"feature_assignments,omitempty"`
	RegisteredAt       string                      `json:"registered_at"`
	LastHeartbeat      string                      `json:"last_heartbeat"`
	Status             RunnerStatus                `json:"status"`
	Version            string                      `json:"version,omitempty"`
}

// RunnerListResponse is the response for GET /runners.
type RunnerListResponse struct {
	Runners []RunnerInfo `json:"runners"`
	Total   int          `json:"total"`
}

// SchedulerResult summarizes one scheduler pass for a project.
type SchedulerResult struct {
	ProjectID  string `json:"project_id"`
	Considered int    `json:"considered"`
	Dispatched int    `json:"dispatched"`
	Skipped    int    `json:"skipped"`
}

// SchedulerStatus is lightweight scheduler loop state suitable for API exposure.
type SchedulerStatus struct {
	Started            bool                       `json:"started"`
	Running            bool                       `json:"running"`
	Interval           string                     `json:"interval"`
	LastTickAt         string                     `json:"last_tick_at,omitempty"`
	LastSuccessAt      string                     `json:"last_success_at,omitempty"`
	LastError          string                     `json:"last_error,omitempty"`
	TotalTicks         int64                      `json:"total_ticks"`
	LastProjectResults map[string]SchedulerResult `json:"last_project_results,omitempty"`
	LastExpiredLeases  int64                      `json:"last_expired_leases"`
}

// PlacementReasonListResponse is the response for task placement diagnostics.
type PlacementReasonListResponse struct {
	Reasons []PlacementReason `json:"reasons"`
	Total   int               `json:"total"`
}

// ServerRequestRecord is one HTTP request handled by the Brain server, for the
// global server-request log (GET /server/requests/recent).
type ServerRequestRecord struct {
	Seq        int64  `json:"seq"`
	Time       int64  `json:"time"` // unix milliseconds
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	ActorType  string `json:"actor_type"`
	ActorName  string `json:"actor_name"`
	RequestID  string `json:"request_id,omitempty"`
}

// ServerRequestListResponse is the response for GET /server/requests/recent.
type ServerRequestListResponse struct {
	Requests []ServerRequestRecord `json:"requests"`
	Total    int                   `json:"total"`
}

type BrainClientInfo struct {
	ClientID     string            `json:"client_id"`
	Kind         string            `json:"kind,omitempty"`
	HostID       string            `json:"host_id"`
	Hostname     string            `json:"hostname,omitempty"`
	OS           string            `json:"os,omitempty"`
	Arch         string            `json:"arch,omitempty"`
	Username     string            `json:"username,omitempty"`
	HomeDir      string            `json:"home_dir,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
}

type WorkspaceObservation struct {
	Path            string `json:"path"`
	GitRoot         string `json:"git_root,omitempty"`
	GitCommonDir    string `json:"git_common_dir,omitempty"`
	GitWorktreeMain string `json:"git_worktree_main,omitempty"`
	GitBranch       string `json:"git_branch,omitempty"`
	GitRemote       string `json:"git_remote,omitempty"`
	FolderName      string `json:"folder_name,omitempty"`
}

type ResolveClientContextRequest struct {
	Client    BrainClientInfo      `json:"client"`
	Workspace WorkspaceObservation `json:"workspace"`
}

type DreamContext struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
}

type ResolveClientContextResponse struct {
	ProjectID  string        `json:"project_id"`
	Confidence string        `json:"confidence"`
	Source     string        `json:"source"`
	Dream      *DreamContext `json:"dream,omitempty"`
}

// =============================================================================
// Health / Stats Types
// =============================================================================

// HealthResponse is the response for GET /health.
type HealthResponse struct {
	Status    string                `json:"status"`
	Timestamp string                `json:"timestamp"`
	Embedding EmbeddingHealthStatus `json:"embedding"`
}

// EmbeddingHealthStatus reports whether semantic-search embeddings are usable.
type EmbeddingHealthStatus struct {
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// EmbeddingBackfillRequest requests embedding generation for matching notes.
type EmbeddingBackfillRequest struct {
	Project string `json:"project,omitempty"`
	Path    string `json:"path,omitempty"`
	Force   bool   `json:"force,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty"`
}

// EmbeddingBackfillEntry describes a note that matches an embedding backfill request.
type EmbeddingBackfillEntry struct {
	ID      int64   `json:"id"`
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Project *string `json:"project,omitempty"`
	Type    *string `json:"type,omitempty"`
}

// EmbeddingBackfillResponse reports embedding generation results.
type EmbeddingBackfillResponse struct {
	Processed int                      `json:"processed"`
	Skipped   int                      `json:"skipped"`
	Failed    int                      `json:"failed"`
	Duration  string                   `json:"duration"`
	DryRun    bool                     `json:"dry_run,omitempty"`
	Entries   []EmbeddingBackfillEntry `json:"entries,omitempty"`
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
	SSEEventConnected        SSEEventType = "connected"
	SSEEventTasksSnapshot    SSEEventType = "tasks_snapshot"
	SSEEventProjectDirty     SSEEventType = "project_dirty"
	SSEEventHeartbeat        SSEEventType = "heartbeat"
	SSEEventError            SSEEventType = "error"
	SSEEventTasksChanged     SSEEventType = "tasks_changed"
	SSEEventCommand          SSEEventType = "command"
	SSEEventRunnerLog        SSEEventType = "runner_log"
	SSEEventRunnerRegistered SSEEventType = "runner_registered"
	SSEEventRunnerOffline    SSEEventType = "runner_offline"
	SSEEventTaskClaimed      SSEEventType = "task_claimed"
	SSEEventTaskReleased     SSEEventType = "task_released"
	SSEEventRunnersUpdate    SSEEventType = "runners_update"
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

// SSERunnersUpdateData is the data for a "runners_update" SSE event.
// Sent when runner state changes (register, heartbeat, lost).
type SSERunnersUpdateData struct {
	SSEEventData
	Runners []RunnerInfo `json:"runners"`
	Total   int          `json:"total"`
}

// =============================================================================
// Runner Registration Types
// =============================================================================

// RegisterRunnerRequest is the request body for POST /runners/register.
type RegisterRunnerRequest struct {
	RunnerID     string   `json:"runner_id"`
	Hostname     string   `json:"hostname"`
	Projects     []string `json:"projects,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	MaxParallel  int      `json:"max_parallel,omitempty"`
	Version      string   `json:"version,omitempty"`
}

// HeartbeatRequest is the request body for POST /runners/heartbeat.
type HeartbeatRequest struct {
	RunnerID    string `json:"runner_id"`
	ActiveTasks int    `json:"active_tasks,omitempty"`
	Version     string `json:"version,omitempty"`
}

// =============================================================================
// Runner SSE Event Types
// =============================================================================

// RunnerSSEEventData is the base data structure for runner-scoped SSE events.
type RunnerSSEEventData struct {
	Type      SSEEventType `json:"type"`
	Transport string       `json:"transport"`
	Timestamp string       `json:"timestamp"`
	RunnerID  string       `json:"runnerId"`
}

// RunnerSSEConnectedData is the data for a runner "connected" SSE event.
type RunnerSSEConnectedData struct {
	RunnerSSEEventData
}

// RunnerSSECommandData is the data for a runner "command" SSE event.
// Commands include: affinity, config, dispatch, shutdown.
type RunnerSSECommandData struct {
	RunnerSSEEventData
	Command string      `json:"command"`
	Payload interface{} `json:"payload,omitempty"`
}

// =============================================================================
// Runner Lifecycle SSE Event Types (published to project subscribers)
// =============================================================================

// SSERunnerRegisteredData is the payload for a "runner_registered" event.
// Emitted when a runner registers or re-registers with the API.
type SSERunnerRegisteredData struct {
	SSEEventData
	RunnerID    string            `json:"runnerId"`
	Hostname    string            `json:"hostname"`
	Executors   []string          `json:"executors"`
	MaxParallel int               `json:"maxParallel"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// SSERunnerOfflineData is the payload for a "runner_offline" event.
// Emitted when a runner transitions to stale or offline status.
type SSERunnerOfflineData struct {
	SSEEventData
	RunnerID string `json:"runnerId"`
	Hostname string `json:"hostname,omitempty"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
}

// SSETaskClaimedData is the payload for a "task_claimed" event.
// Emitted when a task is successfully claimed by a runner.
type SSETaskClaimedData struct {
	SSEEventData
	TaskID   string `json:"taskId"`
	RunnerID string `json:"runnerId"`
}

// SSETaskReleasedData is the payload for a "task_released" event.
// Emitted when a task claim is released by a runner.
type SSETaskReleasedData struct {
	SSEEventData
	TaskID   string `json:"taskId"`
	RunnerID string `json:"runnerId"`
}

// =============================================================================
// Log Ingestion Types
// =============================================================================

// LogLine represents a single log line from a runner.
type LogLine struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Content   string `json:"content"`
}

// LogIngestRequest is the request body for POST /tasks/{projectId}/{taskId}/logs.
type LogIngestRequest struct {
	RunnerID string    `json:"runnerId"`
	Lines    []LogLine `json:"lines"`
}

// LogIngestResponse is the response for POST /tasks/{projectId}/{taskId}/logs.
type LogIngestResponse struct {
	Accepted int `json:"accepted"`
}

// LogQueryResponse is the response for GET /tasks/{projectId}/{taskId}/logs.
type LogQueryResponse struct {
	Lines  []LogLine `json:"lines"`
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
}

// SSERunnerLogData is the data for a "runner_log" SSE event.
type SSERunnerLogData struct {
	SSEEventData
	TaskID   string    `json:"taskId"`
	RunnerID string    `json:"runnerId"`
	Lines    []LogLine `json:"lines"`
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
