package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

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

	// BulkDelete removes multiple entries in a single request, selected by
	// the same filter shape BulkUpdate accepts. Not transactional — see the
	// per-entry Results list on the response.
	BulkDelete(ctx context.Context, req types.BulkDeleteRequest) (*types.BulkDeleteResponse, error)

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
	// When project is set, stats are scoped to that project (path prefix
	// projects/<project>/); the `global` flag is ignored in that case.
	// When project is empty and global=true, stats cover global entries only.
	// When both are empty/false, stats span all entries.
	GetStats(ctx context.Context, global bool, project string) (*types.StatsResponse, error)

	// GetOrphans returns entries with no incoming links.
	// When project is set, results are scoped to entries under projects/<project>/.
	GetOrphans(ctx context.Context, entryType string, limit int, project string) ([]types.BrainEntry, error)

	// GetStale returns entries not verified in N days.
	// When project is set, results are scoped to entries under projects/<project>/.
	GetStale(ctx context.Context, days int, entryType string, limit int, project string) ([]types.BrainEntry, error)

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

// ProjectPlacementService defines project-level scheduling placement metadata operations.
type ProjectPlacementService interface {
	Get(ctx context.Context, projectID string) (*types.ProjectPlacement, error)
	Put(ctx context.Context, projectID string, placement types.ProjectPlacement) (*types.ProjectPlacement, error)
}

// SchedulerService exposes scheduler lifecycle status for API visibility.
type SchedulerService interface {
	Status() types.SchedulerStatus
}

// RunTaskService implements the user-explicit "run this task now" path used
// by the PWA "x" shortcut. Implementations pick an eligible runner, create a
// dispatch lease, and publish a dispatch command via the realtime hub.
//
// Kept narrow on purpose: the existing SchedulerService interface only
// exposes Status() and adding methods to it would require updating every
// mock and test. A separate interface lets the SchedulerService
// implementation satisfy both without forcing other implementations to
// stub RunTaskNow.
type RunTaskService interface {
	RunTaskNow(ctx context.Context, projectID, taskID string, force bool) (*types.RunTaskResponse, error)
}

// RunFeatureService is the optional capability that backs POST
// /tasks/{projectId}/features/{featureId}/run. Surfaced as its own interface
// (mirroring RunTaskService) so implementations are free to wire one without
// the other. In production both are satisfied by *service.SchedulerService.
type RunFeatureService interface {
	RunFeatureNow(ctx context.Context, projectID, featureID string, force bool) (*types.RunFeatureResponse, error)
}

// RunProjectService backs POST /tasks/{projectId}/run — fans out RunFeatureNow
// across every ready feature in a project. Optional capability (mirrors
// RunFeatureService); when nil the handler returns 501 so the PWA can fall
// back gracefully. In production satisfied by *service.SchedulerService.
type RunProjectService interface {
	RunProjectNow(ctx context.Context, projectID string, force bool) (*types.RunProjectResponse, error)
}

// SchedulerVisibilityService exposes persisted scheduler placement artifacts.
type SchedulerVisibilityService interface {
	GetDispatchLease(ctx context.Context, projectID, taskID string) (*types.DispatchLease, error)
	ListPlacementReasons(ctx context.Context, projectID, taskID string) ([]types.PlacementReason, error)
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

	// AckDispatch acknowledges a pushed dispatch lease for a runner.
	AckDispatch(ctx context.Context, projectId, taskId, runnerId, leaseId string) (*types.DispatchAckResponse, error)

	// RejectDispatch rejects a pushed dispatch lease with a structured reason.
	RejectDispatch(ctx context.Context, projectId, taskId, runnerId, leaseId string, reason types.DispatchRejectReason) (*types.DispatchRejectResponse, error)

	// ReleaseDispatch explicitly releases/finalizes a pushed dispatch lease.
	ReleaseDispatch(ctx context.Context, projectId, taskId, runnerId string) (*types.DispatchReleaseResponse, error)

	// DispatchTask creates a short-lived dispatch lease for direct dispatch to a target runner.
	DispatchTask(ctx context.Context, projectId, taskId, targetRunnerId string) (*types.DispatchResponse, error)

	// GetClaimStatus returns the claim status of a task.
	GetClaimStatus(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error)

	// GetLiveClaim reports whether a task is held by a claim that is both
	// unexpired and owned by an online runner — i.e. work genuinely in
	// flight, as opposed to the abandoned claims the resume flow recovers.
	//
	// Destructive handlers use this to refuse mutating a running task.
	// Implementations that cannot determine runner liveness should return
	// Live=false rather than an error, so the guard fails open on a
	// degraded registry instead of blocking all deletes.
	GetLiveClaim(ctx context.Context, projectId, taskId string) (*types.LiveClaim, error)

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

	// ResumeTask flips an abandoned task back to pending, cleaning up its
	// stale claim + acked dispatch lease, and stamps resume_requested so the
	// runner will re-spawn it with IsResume=true. See service.TaskServiceImpl
	// for the full contract; opts may be nil.
	ResumeTask(ctx context.Context, projectId, taskId string, opts *types.ResumeTaskOptions) (*types.ResumeTaskResult, error)

	// ResumeFeature fans out ResumeTask across every task in a feature. Non-
	// abandoned tasks appear in Results as skipped entries with a Reason,
	// so the caller can treat this as "resume everything you can in this
	// feature" without pre-filtering. opts may be nil.
	ResumeFeature(ctx context.Context, projectId, featureId string, opts *types.ResumeTaskOptions) (*types.ResumeFeatureResult, error)
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

	// PauseProjectAutomations pauses automation-generated task execution for a specific project.
	PauseProjectAutomations(ctx context.Context, projectId string) error

	// ResumeProjectAutomations resumes automation-generated task execution for a specific project.
	ResumeProjectAutomations(ctx context.Context, projectId string) error

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

// AutomationRunService triggers a manual run of an automation entry through
// the same server-side task-generation path the cron/event dispatchers use.
type AutomationRunService interface {
	// RunAutomationNow generates the automation's task now. Returns the
	// created task id, or "" when generation was skipped (concurrency guard).
	RunAutomationNow(ctx context.Context, pathOrID string) (string, error)
}

// GoalService defines the interface for goal automation operations exposed over
// the API: create/update/list/run a goal, fetch goal-scoped linked-task
// progress, and fetch reconcile audit history. The concrete
// service.GoalService satisfies this interface. Request/response types live in
// the shared types package to avoid an api->service import cycle.
type GoalService interface {
	// CreateGoal builds and persists a goal automation, returning its summary.
	CreateGoal(ctx context.Context, req types.CreateGoalRequest) (*types.GoalSummary, error)

	// UpdateGoal merges the request onto an existing goal (by goal ID),
	// rebuilds its trigger/action, and persists the change.
	UpdateGoal(ctx context.Context, goalID string, req types.UpdateGoalRequest) (*types.GoalSummary, error)

	// ListGoals returns active goal summaries, optionally filtered by project
	// and/or feature ID.
	ListGoals(ctx context.Context, project, featureID string) ([]types.GoalSummary, error)

	// RunGoal triggers a manual reconcile for a goal and returns the audit.
	RunGoal(ctx context.Context, goalID string) (*types.GoalReconcileAudit, error)

	// GoalProgress computes goal-scoped linked-task progress.
	GoalProgress(ctx context.Context, goalID string) (*types.GoalProgressResponse, error)

	// GoalAuditHistory returns reconcile audit history for a goal, newest first.
	GoalAuditHistory(ctx context.Context, goalID string, limit int) ([]types.GoalReconcileAudit, error)
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

	// SetPaused persists the runner-scoped pause dial. A paused runner is
	// ineligible for push dispatch; the flag is server-owned and survives
	// SSE reconnects, runner restarts, and deregistration.
	SetPaused(ctx context.Context, runnerID string, paused bool) error

	// UpdateAffinity updates a runner's feature affinity.
	UpdateAffinity(ctx context.Context, runnerID string, featureIDs []string) error

	// UpsertInstance records or updates an OpenCode instance reported by a runner.
	UpsertInstance(ctx context.Context, runnerID string, inst types.OpencodeInstance) error

	// DeleteInstance removes an instance record reported by a runner.
	DeleteInstance(ctx context.Context, runnerID, instanceID string) error

	// GetInstance returns a single instance scoped to a runner.
	GetInstance(ctx context.Context, runnerID, instanceID string) (*types.OpencodeInstance, error)

	// ListInstances returns all instances reported by one runner.
	ListInstances(ctx context.Context, runnerID string) (*types.InstanceListResponse, error)

	// ListAllInstances returns every instance across all runners.
	ListAllInstances(ctx context.Context) (*types.InstanceListResponse, error)
}

// BridgeService is the runner-bridge surface used by API handlers. The
// concrete implementation lives in internal/bridge; this interface avoids an
// api->bridge import cycle.
type BridgeService interface {
	// DecorateInstances merges live bridge state (pending permission counts,
	// connection-derived status) into instance records.
	DecorateInstances(instances []types.OpencodeInstance)

	// ServeBridge upgrades the request to a WebSocket and serves a runner's
	// bridge connection until it drops.
	ServeBridge(w http.ResponseWriter, r *http.Request, runnerID string)

	// Connected reports whether a runner has a live bridge connection.
	Connected(runnerID string) bool

	// Do proxies one HTTP request to an instance on a connected runner.
	Do(ctx context.Context, runnerID, instanceID, method, path string, body []byte) (int, []byte, error)

	// SpawnInstance asks a runner to spawn a fresh ad-hoc OpenCode instance.
	SpawnInstance(ctx context.Context, runnerID string, spec types.SpawnInstanceSpec) (*types.OpencodeInstance, error)

	// KillInstance asks a runner to terminate an ad-hoc instance.
	KillInstance(ctx context.Context, runnerID, instanceID string) error

	// AbortTask asks a runner to terminate a task-owned instance and reset it to pending.
	AbortTask(ctx context.Context, runnerID, taskID string) error

	// FetchHistory returns the transcript of a session by ID from a runner,
	// served from a live instance if one hosts it, otherwise read from
	// OpenCode's on-disk storage. Returns raw JSON (array of {info, parts}).
	FetchHistory(ctx context.Context, runnerID, sessionID string) ([]byte, error)

	// AcquireStream enables full event forwarding for an instance
	// (refcounted); the release function must be called on detach.
	AcquireStream(runnerID, instanceID string) (func(), error)

	// PendingPermissions returns raw permission events awaiting a response.
	PendingPermissions(runnerID, instanceID string) []json.RawMessage

	// StartExec asks a runner to run a shell command and stream its output.
	// It returns once the process is spawned; output arrives asynchronously
	// on realtime topic exec:{runnerID}:{execID}, so callers must subscribe
	// to that topic before calling this.
	StartExec(ctx context.Context, runnerID, execID, command, workdir string, timeoutMs int) error

	// SignalExec delivers a signal ("int", "term", "kill") to a running exec.
	SignalExec(ctx context.Context, runnerID, execID, signal string) error

	// ExecOutcome reports what the bridge knows about an exec: whether it has
	// ended (and how), plus how much of its output the realtime fan-out
	// dropped. The bool is false once the exec is no longer tracked. Both the
	// terminal event and the output ride a lossy fan-out, so this is the
	// authority the streaming handler falls back on.
	ExecOutcome(runnerID, execID string) (types.ExecOutcome, bool)

	// ReleaseExec drops the bridge's record of an exec. The streaming handler
	// calls it when it is done so per-command state cannot accumulate.
	ReleaseExec(runnerID, execID string)
}

// ClientContextService resolves Brain client workspace context into project context.
type ClientContextService interface {
	Resolve(ctx context.Context, req types.ResolveClientContextRequest) (*types.ResolveClientContextResponse, error)
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
