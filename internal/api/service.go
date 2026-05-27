package api

import (
	"context"
	"errors"
	"io"

	"github.com/huynle/brain-api/internal/types"
)

// Sentinel errors returned by service implementations.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// BrainService defines the interface for brain entry operations.
// Implementations handle persistence; handlers handle HTTP concerns.
type BrainService interface {
	// Save creates a new brain entry.
	Save(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error)

	// Recall retrieves a brain entry by path or 8-char ID.
	Recall(ctx context.Context, pathOrID string, include ...string) (*types.BrainEntry, error)

	// RecallFull returns the full raw file content (frontmatter + body) for an entry.
	// Used when the caller needs the complete file, not just the parsed body.
	RecallFull(ctx context.Context, pathOrID string) (string, error)

	// Update modifies an existing brain entry.
	Update(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)

	// BulkUpdate applies updates to multiple entries in a single request.
	BulkUpdate(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error)

	// UpdateMetadata merges fields into the entry's metadata JSON in SQLite.
	// Used for runtime state (sessions, claims) without filesystem access.
	UpdateMetadata(ctx context.Context, pathOrID string, fields map[string]interface{}) (*types.BrainEntry, error)

	// Delete removes a brain entry by path or ID.
	Delete(ctx context.Context, pathOrID string) error

	// List returns entries matching the given filters.
	List(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)

	// Move moves an entry to a different project.
	Move(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error)

	// Search performs full-text search across brain entries.
	Search(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error)

	// Inject returns formatted context for AI consumption.
	Inject(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error)

	// GetBacklinks returns entries that link TO the given entry.
	GetBacklinks(ctx context.Context, path string) ([]types.BrainEntry, error)

	// GetOutlinks returns entries that the given entry links TO.
	GetOutlinks(ctx context.Context, path string) ([]types.BrainEntry, error)

	// GetRelated returns entries related by co-citation.
	GetRelated(ctx context.Context, path string, limit int) ([]types.BrainEntry, error)

	// GetSections returns section headers from a brain entry.
	GetSections(ctx context.Context, path string) (*types.SectionsResponse, error)

	// GetSection returns the content of a specific section.
	GetSection(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error)

	// GetStats returns brain statistics.
	GetStats(ctx context.Context, global bool) (*types.StatsResponse, error)

	// GetOrphans returns entries with no incoming links.
	GetOrphans(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error)

	// GetStale returns entries not verified in N days.
	GetStale(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error)

	// Verify marks an entry as verified.
	Verify(ctx context.Context, path string) (*types.VerifyResponse, error)

	// GenerateLink generates a markdown link for a brain entry.
	GenerateLink(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error)
}

// EmbeddingService is optionally implemented by BrainService implementations that can generate embeddings.
type EmbeddingService interface {
	EmbedEntries(ctx context.Context, req types.EmbeddingBackfillRequest) (*types.EmbeddingBackfillResponse, error)
}

// AttachmentService defines orchestration operations for binary attachments.
// Implementations own validation, blob persistence, metadata persistence, entry
// association, and safe deletion semantics; HTTP handlers own transport details.
type AttachmentService interface {
	// Create stores binary content and creates/reuses attachment metadata for a project.
	Create(ctx context.Context, projectID string, req types.CreateAttachmentRequest, content io.Reader) (*types.CreateAttachmentResponse, error)

	// Get returns attachment metadata by ID within a project.
	Get(ctx context.Context, projectID, attachmentID string) (*types.Attachment, error)

	// Open returns attachment metadata plus a readable content stream. Callers must close the stream.
	Open(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error)

	// OpenText returns derived/plain text for a textual attachment. Callers must close the stream.
	OpenText(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error)

	// StoreDerivedText upserts extracted text/status for an attachment.
	StoreDerivedText(ctx context.Context, projectID, attachmentID string, derived types.AttachmentDerivedText) (*types.AttachmentDerivedText, error)

	// GetDerivedText returns extracted text/status for an attachment, or nil when absent.
	GetDerivedText(ctx context.Context, projectID, attachmentID string) (*types.AttachmentDerivedText, error)

	// ExtractAttachmentText orchestrates media-to-text extraction for an attachment.
	ExtractAttachmentText(ctx context.Context, projectID, attachmentID string, req types.AttachmentExtractionRequest) (*types.AttachmentExtractionResult, error)

	// BackfillAttachmentExtraction orchestrates project-level media-to-text extraction.
	BackfillAttachmentExtraction(ctx context.Context, projectID string, req types.AttachmentExtractionBackfillRequest) (*types.AttachmentExtractionBackfillResponse, error)

	// List returns attachments visible within a project.
	List(ctx context.Context, projectID string) (*types.ListAttachmentsResponse, error)

	// ListForEntry returns attachment references associated with a brain entry.
	ListForEntry(ctx context.Context, projectID, pathOrID string) (*types.AttachEntryAttachmentResponse, error)

	// Attach links an existing attachment to a brain entry and returns updated entry attachment refs.
	Attach(ctx context.Context, projectID, pathOrID string, req types.AttachEntryAttachmentRequest) (*types.AttachEntryAttachmentResponse, error)

	// Detach removes an attachment link from a brain entry and returns updated entry attachment refs.
	Detach(ctx context.Context, projectID, pathOrID, attachmentID, role string) (*types.AttachEntryAttachmentResponse, error)

	// Delete removes an attachment only when it is safe to do so.
	// The boolean reports whether metadata/blob content was actually deleted.
	Delete(ctx context.Context, projectID, attachmentID string) (bool, error)
}

// TaskFilterOptions holds optional filters for task queries.
type TaskFilterOptions struct {
	FeatureIDs        []string
	Executors         []string // Filter tasks by executor type (e.g., "opencode", "pi")
	RunnerID          string   // Runner requesting task selection, for server-side eligibility checks.
	GeneratedByPrefix string   // Filter tasks by generated_by prefix (e.g., "automation:").
}

// TaskService defines the interface for task queue operations.
type TaskService interface {
	// ListProjects returns all project IDs that have tasks.
	ListProjects(ctx context.Context) ([]string, error)

	// GetTasks returns all tasks for a project with dependency resolution.
	GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error)

	// GetReady returns tasks that are ready to execute.
	// Pass nil opts for no filtering (backward compatible).
	GetReady(ctx context.Context, projectId string, opts *TaskFilterOptions) ([]types.ResolvedTask, error)

	// GetWaiting returns tasks waiting on dependencies.
	GetWaiting(ctx context.Context, projectId string) ([]types.ResolvedTask, error)

	// GetBlocked returns tasks that are blocked.
	GetBlocked(ctx context.Context, projectId string) ([]types.ResolvedTask, error)

	// GetNext returns the next task to execute (highest priority ready task).
	// Pass nil opts for no filtering (backward compatible).
	GetNext(ctx context.Context, projectId string, opts *TaskFilterOptions) (*types.ResolvedTask, error)

	// ClaimTask claims a task for a runner. Returns ErrConflict if already claimed.
	ClaimTask(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error)

	// ReleaseTask releases a task claim. Returns ErrNotFound if not claimed.
	ReleaseTask(ctx context.Context, projectId, taskId, runnerId string) error

	// RenewClaim extends the claim's expiry. Returns ErrNotFound if not claimed or expired.
	RenewClaim(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error)

	// DispatchTask creates a pre-claim for direct dispatch to a target runner (60-second expiry).
	DispatchTask(ctx context.Context, projectId, taskId, targetRunnerId string) (*types.ClaimResponse, error)

	// GetClaimStatus returns the claim status of a task.
	GetClaimStatus(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error)

	// GetMultiTaskStatus returns status of multiple tasks, with optional long-polling.
	GetMultiTaskStatus(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error)

	// GetFeatures returns computed features for a project.
	GetFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error)

	// GetReadyFeatures returns features that are ready.
	GetReadyFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error)

	// GetFeature returns a single feature by ID.
	GetFeature(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error)

	// CheckoutFeature marks a feature for checkout.
	CheckoutFeature(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error)

	// AssignFeatureToRunner manually assigns or reassigns a feature to a runner.
	AssignFeatureToRunner(ctx context.Context, projectId, featureId string, req types.FeatureAssignmentRequest) (*types.FeatureAssignmentResponse, error)

	// ClearFeatureAssignment manually clears a feature assignment.
	ClearFeatureAssignment(ctx context.Context, projectId, featureId string, req types.ClearFeatureAssignmentRequest) (*types.FeatureAssignmentResponse, error)

	// GetTask returns a single task by ID with dependency resolution applied.
	GetTask(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error)

	// TriggerTask manually triggers a scheduled task.
	TriggerTask(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error)
}

// RunnerService defines the interface for runner control operations.
type RunnerService interface {
	// Pause pauses task execution for a specific project.
	Pause(ctx context.Context, projectId string) error

	// Resume resumes task execution for a specific project.
	Resume(ctx context.Context, projectId string) error

	// PauseAll pauses task execution for all projects.
	PauseAll(ctx context.Context) error

	// ResumeAll resumes task execution for all projects.
	ResumeAll(ctx context.Context) error

	// PauseAutomations pauses automation-generated task execution.
	PauseAutomations(ctx context.Context) error

	// ResumeAutomations resumes automation-generated task execution.
	ResumeAutomations(ctx context.Context) error

	// GetStatus returns the current runner status.
	GetStatus(ctx context.Context) (*types.RunnerStatusResponse, error)
}

// EventService defines the interface for event ingestion and querying.
// Implementations accept events from runners and API mutations,
// publish them to the EventHub, and support querying recent events.
type EventService interface {
	// Ingest accepts a batch of events, validates them, assigns IDs if missing,
	// deduplicates by ID, and publishes to the EventHub.
	Ingest(ctx context.Context, events []types.Event) error

	// Recent returns recent events from the ring buffer with optional filters.
	// Filters are key-value pairs matching event fields (e.g., "project_id", "type").
	Recent(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error)

	// Subscribe returns a channel of events matching the given filters and
	// an unsubscribe function. Used by the SSE handler for real-time streaming.
	Subscribe(ctx context.Context, filters map[string]string) (<-chan types.Event, func())

	// CheckFeatureCompletion checks if all tasks in a feature are completed
	// after a task status update. Emits feature.completed or feature.progress
	// events as appropriate. Safe to call with empty featureID.
	CheckFeatureCompletion(ctx context.Context, projectID, featureID, taskID string)
}

// RunnerRegistryService defines the interface for runner lifecycle management.
// This handles registration, heartbeat, deregistration, and listing of runners.
type RunnerRegistryService interface {
	// Register registers or re-registers a runner.
	Register(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error)

	// Heartbeat updates a runner's heartbeat timestamp and running task count.
	Heartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error

	// Deregister removes a runner and releases all its task claims.
	Deregister(ctx context.Context, runnerID string) error

	// ListRunners returns all runners with computed status.
	ListRunners(ctx context.Context) (*types.RunnerListResponse, error)

	// GetRunner returns a single runner by ID with computed status.
	GetRunner(ctx context.Context, runnerID string) (*types.RunnerInfo, error)

	// UpdateConfig updates a runner's max_parallel configuration and persists it to the database.
	UpdateConfig(ctx context.Context, runnerID string, maxParallel int) error

	// UpdateAffinity updates a runner's feature affinity.
	UpdateAffinity(ctx context.Context, runnerID string, featureIDs []string) error
}

// MonitorService defines the interface for monitor operations.
type MonitorService interface {
	// ListTemplates returns all available monitor templates.
	ListTemplates() []types.MonitorTemplate

	// GetTemplate returns a template by ID, or nil if not found.
	GetTemplate(templateID string) *types.MonitorTemplate

	// List returns monitors matching the given filter.
	List(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error)

	// Create creates a new monitor from a template.
	Create(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error)

	// CreateForFeature creates a feature-review monitor as a dependency-gated one-shot task.
	CreateForFeature(ctx context.Context, templateID string, scope types.MonitorScope) (*types.CreateMonitorResult, error)

	// Toggle enables or disables a monitor by task ID.
	Toggle(ctx context.Context, taskID string, enabled bool) (string, error)

	// Delete removes a monitor by task ID.
	Delete(ctx context.Context, taskID string) (string, error)

	// Find finds an existing monitor for a template+scope combo.
	Find(ctx context.Context, templateID string, scope types.MonitorScope) (*types.MonitorFindResult, error)
}

// WebhookService defines the interface for webhook CRUD, event matching,
// and HTTP delivery with HMAC signing.
type WebhookService interface {
	// Create registers a new webhook.
	Create(ctx context.Context, req types.CreateWebhookRequest) (*types.WebhookResponse, error)

	// Get returns a webhook by ID.
	Get(ctx context.Context, id string) (*types.WebhookResponse, error)

	// List returns all webhooks, optionally filtered by enabled status.
	List(ctx context.Context, enabledOnly bool) ([]types.WebhookResponse, error)

	// Update modifies an existing webhook.
	Update(ctx context.Context, id string, req types.UpdateWebhookRequest) (*types.WebhookResponse, error)

	// Delete removes a webhook by ID.
	Delete(ctx context.Context, id string) error

	// Deliver sends an event to all matching webhooks.
	// It matches the event type and filters against registered webhooks,
	// delivers via HTTP POST with HMAC-SHA256 signing, retries on failure,
	// and logs delivery results.
	Deliver(ctx context.Context, event types.Event) error

	// ListDeliveries returns recent delivery attempts for a webhook.
	ListDeliveries(ctx context.Context, webhookID string, limit int) ([]types.WebhookDeliveryResponse, error)

	// TestDeliver sends a synthetic test event to a specific webhook synchronously
	// and returns the delivery result. Unlike Deliver, this targets a single webhook
	// by ID and waits for the result.
	TestDeliver(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error)
}
