package api

import (
	"context"
	"errors"

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
	Recall(ctx context.Context, pathOrID string) (*types.BrainEntry, error)

	// Update modifies an existing brain entry.
	Update(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)

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

// TaskService defines the interface for task queue operations.
type TaskService interface {
	// ListProjects returns all project IDs that have tasks.
	ListProjects(ctx context.Context) ([]string, error)

	// GetTasks returns all tasks for a project with dependency resolution.
	GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error)

	// GetReady returns tasks that are ready to execute.
	GetReady(ctx context.Context, projectId string) ([]types.ResolvedTask, error)

	// GetWaiting returns tasks waiting on dependencies.
	GetWaiting(ctx context.Context, projectId string) ([]types.ResolvedTask, error)

	// GetBlocked returns tasks that are blocked.
	GetBlocked(ctx context.Context, projectId string) ([]types.ResolvedTask, error)

	// GetNext returns the next task to execute (highest priority ready task).
	GetNext(ctx context.Context, projectId string) (*types.ResolvedTask, error)

	// ClaimTask claims a task for a runner. Returns ErrConflict if already claimed.
	ClaimTask(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error)

	// ReleaseTask releases a task claim. Returns ErrNotFound if not claimed.
	ReleaseTask(ctx context.Context, projectId, taskId, runnerId string) error

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
	CheckoutFeature(ctx context.Context, projectId, featureId string) error

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

	// GetStatus returns the current runner status.
	GetStatus(ctx context.Context) (*types.RunnerStatusResponse, error)
}

// MonitorService defines the interface for monitor operations.
type MonitorService interface {
	// ListTemplates returns all available monitor templates.
	ListTemplates() []types.MonitorTemplate

	// List returns monitors matching the given filter.
	List(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error)

	// Create creates a new monitor from a template.
	Create(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error)

	// Toggle enables or disables a monitor by task ID.
	Toggle(ctx context.Context, taskID string, enabled bool) (string, error)

	// Delete removes a monitor by task ID.
	Delete(ctx context.Context, taskID string) (string, error)
}
