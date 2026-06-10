package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
	"github.com/huynle/brain-api/pkg/frontmatter"
	"github.com/huynle/brain-api/pkg/markdown"
)

// Compile-time check that TaskServiceImpl implements api.TaskService.
var _ api.TaskService = (*TaskServiceImpl)(nil)

// TaskServiceImpl implements api.TaskService using a StorageLayer and persistent claims.
type TaskServiceImpl struct {
	config  *config.Config
	storage *storage.StorageLayer
}

// DefaultLeaseDuration is the default lease duration for task claims (10 minutes).
// This matches the previous stale claim threshold.
const DefaultLeaseDuration = 10 * time.Minute

// NewTaskService creates a new TaskServiceImpl.
func NewTaskService(cfg *config.Config, store *storage.StorageLayer) *TaskServiceImpl {
	return &TaskServiceImpl{
		config:  cfg,
		storage: store,
	}
}

// DefaultClaimCleanupInterval is the default interval for the background claim cleanup goroutine.
const DefaultClaimCleanupInterval = 60 * time.Second

// StartClaimCleanup launches a background goroutine that periodically expires stale claims.
// The goroutine calls storage.ExpireStaleClaims on each tick and logs the count of removed claims.
// It respects context cancellation for clean shutdown.
func (s *TaskServiceImpl) StartClaimCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("claim cleanup goroutine stopped")
				return
			case <-ticker.C:
				count, err := s.storage.ExpireStaleClaims(ctx)
				if err != nil {
					slog.Error("claim cleanup failed", "error", err)
					continue
				}
				if count > 0 {
					slog.Info("expired stale claims", "count", count)
				}
			}
		}
	}()
}

// isExpired returns true if the claim's lease has expired (expires_at < now).
func isExpired(claim *storage.TaskClaimRow) bool {
	return time.Now().UnixMilli() > claim.ExpiresAt
}

// ListProjects scans <brainDir>/projects/ for subdirectories containing a task/ subfolder.
func (s *TaskServiceImpl) ListProjects(ctx context.Context) ([]string, error) {
	projectsDir := filepath.Join(s.config.BrainDir, "projects")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	var projects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskDir := filepath.Join(projectsDir, entry.Name(), "task")
		info, err := os.Stat(taskDir)
		if err != nil || !info.IsDir() {
			continue
		}
		projects = append(projects, entry.Name())
	}

	if projects == nil {
		projects = []string{}
	}
	return projects, nil
}

// GetTasks returns all tasks for a project with dependency resolution.
// Server-side task defaults are applied after resolution: empty task fields
// are filled from config.TaskDefaults so runners receive ready-to-use tasks.
func (s *TaskServiceImpl) GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
	entries, err := s.getAllTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	result := ResolveDependencies(entries)
	s.applyTaskDefaults(result.Tasks)
	return result, nil
}

// GetTask returns a single resolved task by ID for a project.
// Server-side defaults are applied (same as GetTasks).
func (s *TaskServiceImpl) GetTask(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
	resp, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	for _, t := range resp.Tasks {
		if t.ID == taskId {
			task := t
			return &task, nil
		}
	}
	return nil, fmt.Errorf("task %q not found in project %q", taskId, projectId)
}

// applyTaskDefaults fills empty fields on resolved tasks from server-side
// config.TaskDefaults. Non-empty task fields are never overwritten (task
// frontmatter wins). Nil *bool fields get the config default; non-nil keep
// their value. If TaskDefaults is zero-value, this is a no-op.
func (s *TaskServiceImpl) applyTaskDefaults(tasks []types.ResolvedTask) {
	d := s.config.TaskDefaults

	// Quick check: if all defaults are zero, nothing to do.
	if d.Agent == "" && d.Model == "" && d.Executor == "" &&
		len(d.Extensions) == 0 && d.ExecutionMode == "" &&
		d.CompleteOnIdle == nil && d.MergePolicy == "" && d.MergeStrategy == "" &&
		d.MergeTargetBranch == "" && d.RemoteBranchPolicy == "" &&
		d.OpenPRBeforeMerge == nil && d.TargetWorkdir == "" {
		return
	}

	for i := range tasks {
		t := &tasks[i]

		// String fields: fill only if task field is empty
		if t.Agent == "" && d.Agent != "" {
			t.Agent = d.Agent
		}
		if t.Model == "" && d.Model != "" {
			t.Model = d.Model
		}
		if t.Executor == "" && d.Executor != "" {
			t.Executor = d.Executor
		}
		if len(t.Extensions) == 0 && len(d.Extensions) > 0 {
			t.Extensions = append([]string(nil), d.Extensions...)
		}
		if t.ExecutionMode == "" && d.ExecutionMode != "" {
			t.ExecutionMode = d.ExecutionMode
		}
		if t.MergePolicy == "" && d.MergePolicy != "" {
			t.MergePolicy = d.MergePolicy
		}
		if t.MergeStrategy == "" && d.MergeStrategy != "" {
			t.MergeStrategy = d.MergeStrategy
		}
		if t.MergeTargetBranch == "" && d.MergeTargetBranch != "" {
			t.MergeTargetBranch = d.MergeTargetBranch
		}
		if t.RemoteBranchPolicy == "" && d.RemoteBranchPolicy != "" {
			t.RemoteBranchPolicy = d.RemoteBranchPolicy
		}
		if t.TargetWorkdir == "" && d.TargetWorkdir != "" {
			t.TargetWorkdir = d.TargetWorkdir
		}

		// Auto-derive git_branch from feature_id in worktree mode (defense-in-depth:
		// the runner layer also does this, but deriving here ensures the API response
		// already has the correct git_branch field set).
		if t.GitBranch == "" && t.ExecutionMode == "worktree" && t.FeatureID != "" {
			t.GitBranch = t.FeatureID
		}

		// *bool fields: fill only if task field is nil
		if t.CompleteOnIdle == nil && d.CompleteOnIdle != nil {
			v := *d.CompleteOnIdle
			t.CompleteOnIdle = &v
		}
		if t.OpenPRBeforeMerge == nil && d.OpenPRBeforeMerge != nil {
			v := *d.OpenPRBeforeMerge
			t.OpenPRBeforeMerge = &v
		}
	}
}

// getAllTasks fetches all task BrainEntries for a project from storage.
func (s *TaskServiceImpl) getAllTasks(ctx context.Context, projectId string) ([]types.BrainEntry, error) {
	pathPrefix := "projects/" + projectId + "/task"
	rows, err := s.storage.ListNotes(ctx, &storage.ListOptions{
		Type:       "task",
		PathPrefix: pathPrefix,
		Limit:      10000, // effectively unlimited
	})
	if err != nil {
		return nil, fmt.Errorf("list tasks for project %q: %w", projectId, err)
	}

	entries := make([]types.BrainEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, NoteRowToBrainEntry(row))
	}
	return entries, nil
}

// GetReady returns tasks that are ready to execute.
// If opts is non-nil and contains FeatureIDs, only tasks matching those features are returned.
// If opts contains Executors, only tasks matching those executor types are returned.
func (s *TaskServiceImpl) GetReady(ctx context.Context, projectId string, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	ready := GetReadyTasks(result)
	return s.applyTaskFilterOptions(ctx, projectId, ready, opts)
}

// GetWaiting returns tasks waiting on dependencies.
func (s *TaskServiceImpl) GetWaiting(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return GetWaitingTasks(result), nil
}

// GetBlocked returns tasks that are blocked.
func (s *TaskServiceImpl) GetBlocked(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return GetBlockedTasks(result), nil
}

// GetNext returns the next task to execute (highest priority ready task).
// If opts is non-nil and contains FeatureIDs, only tasks matching those features are considered.
// If opts contains Executors, only tasks matching those executor types are considered.
func (s *TaskServiceImpl) GetNext(ctx context.Context, projectId string, opts *api.TaskFilterOptions) (*types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	ready := GetReadyTasks(result)
	if opts != nil {
		ready, err = s.applyTaskFilterOptions(ctx, projectId, ready, opts)
		if err != nil {
			return nil, err
		}
	}
	return pickHighestPriority(ready), nil
}

func (s *TaskServiceImpl) applyTaskFilterOptions(ctx context.Context, projectID string, tasks []types.ResolvedTask, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error) {
	if opts == nil {
		return tasks, nil
	}
	if len(opts.FeatureIDs) > 0 {
		tasks = filterByFeatureIDs(tasks, opts.FeatureIDs)
	}
	if len(opts.Executors) > 0 {
		tasks = filterByExecutors(tasks, opts.Executors)
	}
	if opts.GeneratedByPrefix != "" {
		tasks = filterByGeneratedByPrefix(tasks, opts.GeneratedByPrefix)
	}
	if opts.RunnerID == "" {
		return tasks, nil
	}

	runner, err := s.storage.GetRunner(ctx, opts.RunnerID)
	if err != nil {
		return nil, fmt.Errorf("get runner %q: %w", opts.RunnerID, err)
	}
	if runner == nil {
		return tasks, nil
	}

	return s.filterByRunnerEligibility(ctx, projectID, tasks, runner)
}

func (s *TaskServiceImpl) filterByRunnerEligibility(ctx context.Context, projectID string, tasks []types.ResolvedTask, runner *storage.RunnerRow) ([]types.ResolvedTask, error) {
	if len(runner.Executors) > 0 {
		tasks = filterByExecutors(tasks, runner.Executors)
	}

	capabilities := make(map[string]bool, len(runner.Capabilities))
	for _, capability := range runner.Capabilities {
		capabilities[capability] = true
	}

	filtered := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if !runnerHasRequiredCapabilities(task, capabilities) {
			continue
		}
		if task.FeatureID != "" {
			assignment, err := s.storage.GetFeatureAssignment(ctx, projectID, task.FeatureID)
			if err != nil {
				return nil, fmt.Errorf("get feature assignment %q/%q: %w", projectID, task.FeatureID, err)
			}
			if assignment != nil && assignment.RunnerID != runner.RunnerID {
				continue
			}
		}
		filtered = append(filtered, task)
	}
	return filtered, nil
}

func filterByGeneratedByPrefix(tasks []types.ResolvedTask, prefix string) []types.ResolvedTask {
	filtered := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if strings.HasPrefix(task.GeneratedBy, prefix) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func runnerHasRequiredCapabilities(task types.ResolvedTask, capabilities map[string]bool) bool {
	for _, required := range task.RequiresCapability {
		if !capabilities[required] {
			return false
		}
	}
	return true
}

// ClaimTask claims a task for a runner. Returns ErrConflict if already claimed by another runner.
// Claims are persisted to SQLite via the storage layer, surviving API server restarts.
func (s *TaskServiceImpl) ClaimTask(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error) {
	return s.ClaimTaskWithDuration(ctx, projectId, taskId, runnerId, DefaultLeaseDuration)
}

// ClaimTaskWithDuration claims a task with a custom lease duration.
// Used for pre-claims (dispatch) with shorter expiry.
func (s *TaskServiceImpl) ClaimTaskWithDuration(ctx context.Context, projectId, taskId, runnerId string, leaseDuration time.Duration) (*types.ClaimResponse, error) {
	featureID, err := s.featureIDForTaskClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, err
	}

	ok, existing, err := s.storage.ClaimTask(ctx, projectId, taskId, runnerId, leaseDuration)
	if err != nil {
		return nil, fmt.Errorf("storage claim task: %w", err)
	}

	if !ok {
		// Claim failed — another active runner holds it
		stale := false
		if existing != nil {
			stale = isExpired(existing)
		}
		holder := ""
		if existing != nil {
			holder = existing.RunnerID
		}
		slog.Warn("claim conflict", "project", projectId, "task_id", taskId, "runner_id", runnerId, "held_by", holder)
		return &types.ClaimResponse{
			Success:   false,
			TaskID:    taskId,
			RunnerID:  runnerId,
			Error:     "already claimed",
			Message:   fmt.Sprintf("task %s is already claimed by %s", taskId, holder),
			ClaimedBy: holder,
			IsStale:   &stale,
		}, api.ErrConflict
	}
	if featureID != "" {
		assigned, assignment, err := s.storage.AssignFeatureIfEmpty(ctx, projectId, featureID, runnerId, "auto", "active")
		if err != nil {
			if releaseErr := s.ReleaseTask(ctx, projectId, taskId, runnerId); releaseErr != nil {
				slog.Warn("failed to release task after feature assignment error", "project", projectId, "task_id", taskId, "runner_id", runnerId, "release_error", releaseErr)
			}
			return nil, fmt.Errorf("assign feature for claimed task: %w", err)
		}
		if !assigned {
			if assignment == nil {
				if releaseErr := s.ReleaseTask(ctx, projectId, taskId, runnerId); releaseErr != nil {
					slog.Warn("failed to release task after missing feature assignment", "project", projectId, "task_id", taskId, "runner_id", runnerId, "release_error", releaseErr)
				}
				return nil, fmt.Errorf("feature %q assignment disappeared", featureID)
			}
			if assignment.RunnerID != runnerId {
				if releaseErr := s.ReleaseTask(ctx, projectId, taskId, runnerId); releaseErr != nil {
					slog.Warn("failed to release task after feature assignment conflict", "project", projectId, "task_id", taskId, "runner_id", runnerId, "release_error", releaseErr)
				}
				slog.Warn("feature assignment conflict", "project", projectId, "feature_id", featureID, "runner_id", runnerId, "assigned_to", assignment.RunnerID)
				return &types.ClaimResponse{
					Success:   false,
					TaskID:    taskId,
					RunnerID:  runnerId,
					Error:     "feature assigned to another runner",
					Message:   fmt.Sprintf("feature %s is assigned to %s", featureID, assignment.RunnerID),
					ClaimedBy: assignment.RunnerID,
				}, api.ErrConflict
			}
		}
	}

	claimedAt := time.Now().UTC().Format(time.RFC3339)
	slog.Info("task claimed", "project", projectId, "task_id", taskId, "runner_id", runnerId)
	return &types.ClaimResponse{
		Success:   true,
		TaskID:    taskId,
		RunnerID:  runnerId,
		ClaimedAt: claimedAt,
	}, nil
}

func (s *TaskServiceImpl) featureIDForTaskClaim(ctx context.Context, projectID, taskID string) (string, error) {
	note, err := s.storage.GetNoteByPath(ctx, fmt.Sprintf("projects/%s/task/%s.md", projectID, taskID))
	if err != nil {
		return "", fmt.Errorf("get task note for claim: %w", err)
	}
	if note == nil {
		return "", nil
	}
	return NoteRowToBrainEntry(note).FeatureID, nil
}

// ReleaseTask releases a task claim. Returns ErrNotFound if not claimed,
// ErrConflict if claimed by a different runner.
func (s *TaskServiceImpl) ReleaseTask(ctx context.Context, projectId, taskId, runnerId string) error {
	// First check if a claim exists at all and who owns it
	existing, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return fmt.Errorf("storage get claim: %w", err)
	}
	if existing == nil {
		return api.ErrNotFound
	}
	if existing.RunnerID != runnerId {
		return api.ErrConflict
	}

	released, err := s.storage.ReleaseClaim(ctx, projectId, taskId, runnerId)
	if err != nil {
		return fmt.Errorf("storage release claim: %w", err)
	}
	if !released {
		return api.ErrNotFound
	}

	slog.Info("task released", "project", projectId, "task_id", taskId, "runner_id", runnerId)
	return nil
}

// RenewClaim extends the claim's expiry by DefaultLeaseDuration.
// Returns ErrNotFound if the claim doesn't exist, is expired, or is owned by a different runner.
func (s *TaskServiceImpl) RenewClaim(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error) {
	// Verify the claim exists and is owned by this runner
	existing, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, fmt.Errorf("storage get claim: %w", err)
	}
	if existing == nil {
		return &types.RenewClaimResponse{
			Success:  false,
			TaskID:   taskId,
			RunnerID: runnerId,
			Error:    "claim not found",
		}, api.ErrNotFound
	}
	if existing.RunnerID != runnerId {
		return &types.RenewClaimResponse{
			Success:  false,
			TaskID:   taskId,
			RunnerID: runnerId,
			Error:    "claim owned by different runner",
		}, api.ErrConflict
	}
	if isExpired(existing) {
		return &types.RenewClaimResponse{
			Success:  false,
			TaskID:   taskId,
			RunnerID: runnerId,
			Error:    "claim expired",
		}, api.ErrNotFound
	}

	// Extend the expiry
	newExpiry := time.Now().Add(DefaultLeaseDuration)
	if err := s.storage.RenewClaim(ctx, projectId, taskId, runnerId, newExpiry); err != nil {
		return nil, fmt.Errorf("storage renew claim: %w", err)
	}

	slog.Info("claim renewed", "project", projectId, "task_id", taskId, "runner_id", runnerId, "expires_at", newExpiry.UTC().Format(time.RFC3339))
	return &types.RenewClaimResponse{
		Success:   true,
		TaskID:    taskId,
		RunnerID:  runnerId,
		ExpiresAt: newExpiry.UTC().Format(time.RFC3339),
	}, nil
}

// GetClaimStatus returns the claim status of a task.
func (s *TaskServiceImpl) GetClaimStatus(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error) {
	existing, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, fmt.Errorf("storage get claim: %w", err)
	}

	if existing == nil {
		return &types.ClaimStatusResponse{
			TaskID:  taskId,
			Claimed: false,
			IsStale: false,
		}, nil
	}

	expired := isExpired(existing)
	claimedAt := time.UnixMilli(existing.ClaimedAt).UTC().Format(time.RFC3339)
	return &types.ClaimStatusResponse{
		TaskID:    taskId,
		Claimed:   true,
		RunnerID:  existing.RunnerID,
		ClaimedAt: claimedAt,
		IsStale:   expired,
	}, nil
}

// DispatchTask creates a pre-claim for direct dispatch to a target runner.
// Pre-claims have a shorter 60-second expiry to allow quick recovery if the runner doesn't respond.
func (s *TaskServiceImpl) DispatchTask(ctx context.Context, projectId, taskId, targetRunnerId string) (*types.ClaimResponse, error) {
	const dispatchLeaseDuration = 60 * time.Second
	return s.ClaimTaskWithDuration(ctx, projectId, taskId, targetRunnerId, dispatchLeaseDuration)
}

// GetMultiTaskStatus returns status of multiple tasks.
func (s *TaskServiceImpl) GetMultiTaskStatus(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	// Build lookup map of resolved tasks by ID
	taskMap := make(map[string]types.ResolvedTask, len(result.Tasks))
	for _, t := range result.Tasks {
		taskMap[t.ID] = t
	}

	// Collect requested tasks
	var tasks []types.ResolvedTask
	allCompleted := true
	for _, id := range req.TaskIDs {
		if t, ok := taskMap[id]; ok {
			tasks = append(tasks, t)
			if t.Status != "completed" && t.Status != "validated" {
				allCompleted = false
			}
		}
	}

	if tasks == nil {
		tasks = []types.ResolvedTask{}
	}

	return &types.MultiTaskStatusResponse{
		Tasks:        tasks,
		AllCompleted: allCompleted,
	}, nil
}

// GetTasksByFeature returns all resolved tasks belonging to a specific feature.
// This satisfies the FeatureTaskLister interface used by EventServiceImpl
// for server-side feature completion detection.
func (s *TaskServiceImpl) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return filterByFeatureIDs(result.Tasks, []string{featureID}), nil
}

// GetFeatures returns computed features for a project.
func (s *TaskServiceImpl) GetFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	featureResult := ComputeAndResolveFeatures(result.Tasks)
	return featuresToResponse(featureResult.Features), nil
}

// GetReadyFeatures returns features that are ready.
func (s *TaskServiceImpl) GetReadyFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	featureResult := ComputeAndResolveFeatures(result.Tasks)
	readyFeatures := GetReadyFeatures(featureResult.Features)
	return featuresToResponse(readyFeatures), nil
}

// GetFeature returns a single feature by ID.
func (s *TaskServiceImpl) GetFeature(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	featureResult := ComputeAndResolveFeatures(result.Tasks)
	for _, f := range featureResult.Features {
		if f.ID == featureId {
			return &types.FeatureResponse{
				Feature: computedFeatureToFeature(f),
			}, nil
		}
	}

	return nil, api.ErrNotFound
}

// CheckoutFeature marks a feature for checkout.
func (s *TaskServiceImpl) CheckoutFeature(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error) {
	// Sanitize inputs
	sanitizedProjectID := strings.TrimSpace(projectId)
	sanitizedFeatureID := strings.TrimSpace(featureId)

	if sanitizedProjectID == "" {
		return nil, fmt.Errorf("projectId is required")
	}
	if sanitizedFeatureID == "" {
		return nil, fmt.Errorf("featureId is required")
	}

	// Normalize options with defaults
	normalizedOpts := normalizeFeatureCheckoutOptions(opts)

	// Generate checkout task key
	generatedKey := fmt.Sprintf("feature-checkout:%s:round-1", sanitizedFeatureID)

	// Check if checkout task already exists (idempotency)
	taskDir := filepath.Join(s.config.BrainDir, "projects", sanitizedProjectID, "task")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create task directory: %w", err)
	}

	// Look for existing checkout task with this generated_key
	existingTask, err := findCheckoutTaskByKey(taskDir, generatedKey)
	if err == nil && existingTask != nil {
		// Task already exists
		return &types.CheckoutFeatureResult{
			Created:      false,
			GeneratedKey: generatedKey,
			Task:         existingTask,
		}, nil
	}

	// Get all feature tasks to build depends_on list
	featureTasks, err := s.getFeatureTasksFromFilesystem(sanitizedProjectID, sanitizedFeatureID)
	if err != nil {
		return nil, fmt.Errorf("failed to get feature tasks: %w", err)
	}

	// Extract non-generated task IDs
	dependsOn := extractUniqueNonGeneratedTaskIds(featureTasks)

	// Generate new task ID
	shortID := markdown.GenerateShortID()
	filename := shortID + ".md"
	taskPath := filepath.Join(taskDir, filename)

	// Build checkout task content
	content := buildFeatureCheckoutContent(sanitizedFeatureID, normalizedOpts)

	// Build frontmatter
	trueVal := true
	fm := &frontmatter.Frontmatter{
		Type:     "task",
		Title:    fmt.Sprintf("Feature checkout: %s", sanitizedFeatureID),
		Status:   "pending",
		Priority: "medium",
		Created:  time.Now().Format(time.RFC3339),
		Tags:     []string{"checkout", sanitizedFeatureID},
	}

	// Set fields that use frontmatter struct fields
	fm.FeatureID = sanitizedFeatureID
	fm.DependsOn = dependsOn
	fm.Generated = &trueVal
	fm.GeneratedKind = "feature_checkout"
	fm.GeneratedKey = generatedKey
	fm.GeneratedBy = "feature-checkout"

	// Set merge-related fields
	if normalizedOpts.ExecutionBranch != "" {
		fm.GitBranch = normalizedOpts.ExecutionBranch
	}
	if normalizedOpts.MergeTargetBranch != "" {
		fm.MergeTargetBranch = normalizedOpts.MergeTargetBranch
	}
	if normalizedOpts.MergePolicy != "" {
		fm.MergePolicy = normalizedOpts.MergePolicy
	}
	if normalizedOpts.MergeStrategy != "" {
		fm.MergeStrategy = normalizedOpts.MergeStrategy
	}
	if normalizedOpts.RemoteBranchPolicy != "" {
		fm.RemoteBranchPolicy = normalizedOpts.RemoteBranchPolicy
	}
	if normalizedOpts.ExecutionMode != "" {
		fm.ExecutionMode = normalizedOpts.ExecutionMode
	}
	if normalizedOpts.OpenPRBeforeMerge {
		fm.OpenPRBeforeMerge = &normalizedOpts.OpenPRBeforeMerge
	}

	// Write file
	fmYAML := frontmatter.Serialize(fm)
	var fileBuilder strings.Builder
	fileBuilder.WriteString("---\n")
	fileBuilder.WriteString(fmYAML)
	fileBuilder.WriteString("---\n")
	if content != "" {
		fileBuilder.WriteString("\n")
		fileBuilder.WriteString(content)
		fileBuilder.WriteString("\n")
	}
	fileContent := fileBuilder.String()
	if err := os.WriteFile(taskPath, []byte(fileContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write checkout task: %w", err)
	}

	// Build response
	relPath := fmt.Sprintf("projects/%s/task/%s", sanitizedProjectID, filename)
	result := &types.CheckoutFeatureResult{
		Created:      true,
		GeneratedKey: generatedKey,
		Task: &types.CreateEntryResponse{
			ID:     shortID,
			Path:   relPath,
			Title:  fmt.Sprintf("Feature checkout: %s", sanitizedFeatureID),
			Type:   "task",
			Status: "pending",
		},
	}

	return result, nil
}

// normalizeFeatureCheckoutOptions returns normalized options with defaults.
func normalizeFeatureCheckoutOptions(opts *types.FeatureCheckoutOptions) *types.FeatureCheckoutOptions {
	if opts == nil {
		opts = &types.FeatureCheckoutOptions{}
	}

	normalized := &types.FeatureCheckoutOptions{
		ExecutionBranch:    strings.TrimSpace(opts.ExecutionBranch),
		MergeTargetBranch:  strings.TrimSpace(opts.MergeTargetBranch),
		MergePolicy:        strings.TrimSpace(opts.MergePolicy),
		MergeStrategy:      strings.TrimSpace(opts.MergeStrategy),
		RemoteBranchPolicy: strings.TrimSpace(opts.RemoteBranchPolicy),
		ExecutionMode:      strings.TrimSpace(opts.ExecutionMode),
		OpenPRBeforeMerge:  opts.OpenPRBeforeMerge,
	}

	// Apply defaults
	if normalized.MergePolicy == "" {
		normalized.MergePolicy = "auto_merge"
	}
	if normalized.MergeStrategy == "" {
		normalized.MergeStrategy = "squash"
	}
	if normalized.RemoteBranchPolicy == "" {
		normalized.RemoteBranchPolicy = "delete"
	}
	if normalized.ExecutionMode == "" {
		normalized.ExecutionMode = "worktree"
	}

	return normalized
}

// buildFeatureCheckoutContent builds the content body for a checkout task.
func buildFeatureCheckoutContent(featureID string, opts *types.FeatureCheckoutOptions) string {
	executionBranch := opts.ExecutionBranch
	if executionBranch == "" {
		executionBranch = "(default branch for execution context)"
	}
	mergeTargetBranch := opts.MergeTargetBranch
	if mergeTargetBranch == "" {
		mergeTargetBranch = "(no explicit merge target)"
	}

	lines := []string{
		fmt.Sprintf("Automated feature checkout for %s.", featureID),
		"",
		"Merge intent:",
		fmt.Sprintf("- execution_branch: %s", executionBranch),
		fmt.Sprintf("- merge_target_branch: %s", mergeTargetBranch),
		fmt.Sprintf("- merge_policy: %s", opts.MergePolicy),
		fmt.Sprintf("- merge_strategy: %s", opts.MergeStrategy),
		fmt.Sprintf("- remote_branch_policy: %s", opts.RemoteBranchPolicy),
		fmt.Sprintf("- open_pr_before_merge: %v", opts.OpenPRBeforeMerge),
		"",
		"Safety gates before merge:",
		"- checkout validation pass",
		"- merge precheck pass",
		"- verification commands pass",
		"",
		"Guardrails:",
		"- If merge target is a protected branch, use open_pr_before_merge before auto merge",
		"- For optional PR-before-merge flow, ensure PR checks are green before final merge",
		"- cleanup only after confirmed successful push",
	}

	return strings.Join(lines, "\n")
}

// findCheckoutTaskByKey searches for a checkout task with the given generated_key.
func findCheckoutTaskByKey(taskDir, generatedKey string) (*types.CreateEntryResponse, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(taskDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}

		if doc.Frontmatter.GeneratedKey == generatedKey {
			// Found existing task
			shortID := strings.TrimSuffix(entry.Name(), ".md")
			title := doc.Frontmatter.Title
			status := doc.Frontmatter.Status

			return &types.CreateEntryResponse{
				ID:     shortID,
				Path:   fmt.Sprintf("projects/%s/%s", filepath.Base(filepath.Dir(taskDir)), entry.Name()),
				Title:  title,
				Type:   "task",
				Status: status,
			}, nil
		}
	}

	return nil, fmt.Errorf("not found")
}

// getFeatureTasksFromFilesystem reads tasks from filesystem for a feature.
func (s *TaskServiceImpl) getFeatureTasksFromFilesystem(projectID, featureID string) ([]types.BrainEntry, error) {
	taskDir := filepath.Join(s.config.BrainDir, "projects", projectID, "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []types.BrainEntry{}, nil
		}
		return nil, err
	}

	var tasks []types.BrainEntry
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(taskDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}

		taskFeatureID := doc.Frontmatter.FeatureID
		if taskFeatureID != featureID {
			continue
		}

		// Parse Generated field
		generated := doc.Frontmatter.Generated

		shortID := strings.TrimSuffix(entry.Name(), ".md")
		tasks = append(tasks, types.BrainEntry{
			ID:            shortID,
			Generated:     generated,
			GeneratedKind: doc.Frontmatter.GeneratedKind,
		})
	}

	return tasks, nil
}

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// extractUniqueNonGeneratedTaskIds extracts unique task IDs from non-generated tasks.
func extractUniqueNonGeneratedTaskIds(tasks []types.BrainEntry) []string {
	seen := make(map[string]bool)
	var result []string

	for _, task := range tasks {
		// Skip generated tasks
		if task.Generated != nil && *task.Generated {
			continue
		}
		// Skip empty IDs
		if task.ID == "" {
			continue
		}
		// Skip duplicates
		if seen[task.ID] {
			continue
		}
		seen[task.ID] = true
		result = append(result, task.ID)
	}

	// Sort for deterministic output
	if len(result) > 0 {
		sortStrings(result)
	}
	return result
}

// isTerminalCheckoutStatus returns true if the status is terminal.
func isTerminalCheckoutStatus(status string) bool {
	return status == "completed" ||
		status == "validated" ||
		status == "cancelled" ||
		status == "superseded" ||
		status == "archived"
}

// extractGeneratedDependentTasks extracts checkout and review tasks.
func extractGeneratedDependentTasks(tasks []types.BrainEntry) []types.BrainEntry {
	var result []types.BrainEntry
	for _, task := range tasks {
		if task.Generated == nil || !*task.Generated {
			continue
		}
		if task.GeneratedKind == "feature_checkout" || task.GeneratedKind == "feature_review" {
			result = append(result, task)
		}
	}
	return result
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	// Simple insertion sort for small slices
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// TriggerTask manually triggers a scheduled task. It validates the schedule,
// creates a run record, computes next_run, and resets the task to pending.
func (s *TaskServiceImpl) TriggerTask(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
	// 1. Find the task
	tasks, err := s.getAllTasks(ctx, projectId)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	var task *types.BrainEntry
	for i := range tasks {
		if tasks[i].ID == taskId {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s/%s", projectId, taskId)
	}

	// 2. Validate schedule exists
	if task.Schedule == "" {
		return &types.TriggerResponse{
			Success: true, TaskID: taskId, Triggered: false,
			Reason: "task has no schedule",
		}, nil
	}

	// 3. Validate schedule_enabled
	if task.ScheduleEnabled != nil && !*task.ScheduleEnabled {
		return &types.TriggerResponse{
			Success: true, TaskID: taskId, Triggered: false,
			Reason: "schedule is disabled",
		}, nil
	}

	// 4. Validate eligible status (active, completed, blocked)
	eligibleStatuses := map[string]bool{"active": true, "completed": true, "blocked": true}
	if !eligibleStatuses[task.Status] {
		return &types.TriggerResponse{
			Success: true, TaskID: taskId, Triggered: false,
			Reason: fmt.Sprintf("task status %q is not eligible for triggering", task.Status),
		}, nil
	}

	// 5. Check max_runs
	if task.MaxRuns != nil && *task.MaxRuns > 0 {
		runCount := countCompletedRuns(task.Runs)
		if runCount >= *task.MaxRuns {
			return &types.TriggerResponse{
				Success: true, TaskID: taskId, Triggered: false,
				Reason: fmt.Sprintf("max_runs reached (%d/%d)", runCount, *task.MaxRuns),
			}, nil
		}
	}

	// 6. Parse cron expression and compute next_run
	sched, err := cron.Parse(task.Schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule expression %q: %w", task.Schedule, err)
	}
	now := time.Now().UTC()
	nextRun := sched.NextAfter(now)

	// 7. Generate run ID and create run record
	runID := generateTriggerRunID(now)

	// Build updated runs array
	runs := make([]interface{}, 0, len(task.Runs)+1)
	for _, r := range task.Runs {
		runMap := map[string]interface{}{
			"run_id":  r.RunID,
			"status":  r.Status,
			"started": r.Started,
		}
		if r.Completed != "" {
			runMap["completed"] = r.Completed
		}
		if r.SkipReason != "" {
			runMap["skip_reason"] = r.SkipReason
		}
		runs = append(runs, runMap)
	}
	runs = append(runs, map[string]interface{}{
		"run_id":  runID,
		"status":  "in_progress",
		"started": now.Format(time.RFC3339),
		"tasks":   1,
	})

	// 8. Update metadata: runs + next_run + status
	if _, err := s.storage.MergeMetadata(ctx, task.Path, map[string]interface{}{
		"runs":     runs,
		"next_run": nextRun.Format(time.RFC3339),
		"status":   "pending",
	}); err != nil {
		return nil, fmt.Errorf("failed to update task metadata: %w", err)
	}

	return &types.TriggerResponse{
		Success:   true,
		TaskID:    taskId,
		Triggered: true,
		RunID:     runID,
		NextRun:   nextRun.Format(time.RFC3339),
	}, nil
}

// countCompletedRuns counts runs with terminal-ish statuses.
// Mirrors countRuns in internal/runner/schedule.go.
func countCompletedRuns(runs []types.CronRun) int {
	count := 0
	for _, r := range runs {
		switch r.Status {
		case "completed", "failed", "skipped", "in_progress":
			count++
		}
	}
	return count
}

// generateTriggerRunID creates a unique run identifier.
// Format: YYYYMMDD-HHmm-XXXXXX (6 hex chars).
func generateTriggerRunID(t time.Time) string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%s-%s", t.Format("20060102-1504"), hex.EncodeToString(b))
}

// =============================================================================
// Helper functions
// =============================================================================

// NoteRowToBrainEntry converts a storage NoteRow to a types.BrainEntry,
// parsing the JSON metadata field into proper BrainEntry fields.
func NoteRowToBrainEntry(row *storage.NoteRow) types.BrainEntry {
	entry := types.BrainEntry{
		Path:  row.Path,
		Title: row.Title,
		ID:    row.ShortID,
	}

	// Nullable string fields
	if row.Type != nil {
		entry.Type = *row.Type
	}
	if row.Status != nil {
		entry.Status = *row.Status
	}
	if row.Priority != nil {
		entry.Priority = *row.Priority
	}
	if row.ProjectID != nil {
		entry.ProjectID = *row.ProjectID
	}
	if row.FeatureID != nil {
		entry.FeatureID = *row.FeatureID
	}
	if row.Created != nil {
		entry.Created = *row.Created
	}
	if row.Modified != nil {
		entry.Modified = *row.Modified
	}
	if row.Body != nil {
		entry.Content = *row.Body
	}

	// Parse JSON metadata for additional fields
	if row.Metadata != "" && row.Metadata != "{}" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(row.Metadata), &meta); err == nil {
			parseMetadataIntoEntry(&entry, meta)
		}
	}

	return entry
}

// parseMetadataIntoEntry extracts known fields from the metadata JSON map
// into the BrainEntry struct fields.
func parseMetadataIntoEntry(entry *types.BrainEntry, meta map[string]interface{}) {
	// parent_id: hierarchical parent reference
	if v, ok := metaString(meta, "parent_id"); ok {
		entry.ParentID = v
	}

	// depends_on: can be string or []string in JSON
	if v, ok := metaStringSlice(meta, "depends_on"); ok {
		entry.DependsOn = v
	} else if v, ok := metaString(meta, "depends_on"); ok {
		entry.DependsOn = []string{v}
	}

	if v, ok := metaStringSlice(meta, "tags"); ok {
		entry.Tags = v
	}
	if v, ok := meta["attachments"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var attachments []types.AttachmentReference
			if err := json.Unmarshal(data, &attachments); err == nil {
				entry.Attachments = attachments
			}
		}
	}

	// Schedule fields
	if v, ok := metaString(meta, "schedule"); ok {
		entry.Schedule = v
	}
	if v, ok := metaBool(meta, "schedule_enabled"); ok {
		entry.ScheduleEnabled = &v
	}
	if v, ok := metaString(meta, "next_run"); ok {
		entry.NextRun = v
	}
	if v, ok := metaInt(meta, "max_runs"); ok {
		entry.MaxRuns = &v
	}
	if v, ok := metaString(meta, "starts_at"); ok {
		entry.StartsAt = v
	}
	if v, ok := metaString(meta, "expires_at"); ok {
		entry.ExpiresAt = v
	}
	if v, ok := metaString(meta, "run_once_at"); ok {
		entry.RunOnceAt = v
	}
	if v, ok := metaString(meta, "timezone"); ok {
		entry.Timezone = v
	}

	// Git/execution fields
	if v, ok := metaString(meta, "workdir"); ok {
		entry.Workdir = v
	}
	if v, ok := metaString(meta, "git_remote"); ok {
		entry.GitRemote = v
	}
	if v, ok := metaString(meta, "git_branch"); ok {
		entry.GitBranch = v
	}
	if v, ok := metaString(meta, "merge_target_branch"); ok {
		entry.MergeTargetBranch = v
	}
	if v, ok := metaString(meta, "merge_policy"); ok {
		entry.MergePolicy = v
	}
	if v, ok := metaString(meta, "merge_strategy"); ok {
		entry.MergeStrategy = v
	}
	if v, ok := metaString(meta, "remote_branch_policy"); ok {
		entry.RemoteBranchPolicy = v
	}
	if v, ok := metaBool(meta, "open_pr_before_merge"); ok {
		entry.OpenPRBeforeMerge = &v
	}
	if v, ok := metaString(meta, "execution_mode"); ok {
		entry.ExecutionMode = v
	}
	if v, ok := metaString(meta, "session_mode"); ok {
		entry.SessionMode = v
	}

	// Task execution fields
	if v, ok := metaString(meta, "user_original_request"); ok {
		entry.UserOriginalRequest = v
	}
	if v, ok := metaString(meta, "direct_prompt"); ok {
		entry.DirectPrompt = v
	}
	if v, ok := metaString(meta, "agent"); ok {
		entry.Agent = v
	}
	if v, ok := metaString(meta, "model"); ok {
		entry.Model = v
	}
	if v, ok := metaString(meta, "executor"); ok {
		entry.Executor = v
	}
	if v, ok := meta["extensions"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					entry.Extensions = append(entry.Extensions, s)
				}
			}
		}
	}
	if v, ok := metaBool(meta, "complete_on_idle"); ok {
		entry.CompleteOnIdle = &v
	}
	if v, ok := metaString(meta, "target_workdir"); ok {
		entry.TargetWorkdir = v
	}
	if v, ok := metaString(meta, "executor"); ok {
		entry.Executor = v
	}
	if v, ok := metaStringSlice(meta, "extensions"); ok {
		entry.Extensions = v
	}
	if v, ok := metaStringSlice(meta, "requires_capability"); ok {
		entry.RequiresCapability = v
	} else if v, ok := metaString(meta, "requires_capability"); ok {
		entry.RequiresCapability = []string{v}
	}

	// Feature grouping (feature_id from metadata as fallback)
	if v, ok := metaString(meta, "feature_id"); ok {
		if entry.FeatureID == "" {
			entry.FeatureID = v
		}
	}
	if v, ok := metaString(meta, "feature_priority"); ok {
		entry.FeaturePriority = v
	}
	if v, ok := metaStringSlice(meta, "feature_depends_on"); ok {
		entry.FeatureDependsOn = v
	}

	// Feature-level schedule fields
	if v, ok := metaString(meta, "feature_schedule"); ok {
		entry.FeatureSchedule = v
	}
	if v, ok := metaString(meta, "feature_starts_at"); ok {
		entry.FeatureStartsAt = v
	}
	if v, ok := metaString(meta, "feature_expires_at"); ok {
		entry.FeatureExpiresAt = v
	}
	if v, ok := metaString(meta, "feature_run_once_at"); ok {
		entry.FeatureRunOnceAt = v
	}
	if v, ok := metaString(meta, "feature_timezone"); ok {
		entry.FeatureTimezone = v
	}

	// Generated entry metadata
	if v, ok := metaBool(meta, "generated"); ok {
		entry.Generated = &v
	}
	if v, ok := metaString(meta, "generated_kind"); ok {
		entry.GeneratedKind = v
	}
	if v, ok := metaString(meta, "generated_key"); ok {
		entry.GeneratedKey = v
	}
	if v, ok := metaString(meta, "generated_by"); ok {
		entry.GeneratedBy = v
	}
	if v, ok := metaString(meta, "automation_run_id"); ok {
		entry.AutomationRunID = v
	}

	// Automation fields (nested maps from metadata JSON)
	if v, ok := meta["trigger"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var tc types.TriggerConfig
			if err := json.Unmarshal(data, &tc); err == nil {
				entry.Trigger = &tc
			}
		}
	}
	if v, ok := meta["action"]; ok {
		entry.Action = metaToAutomationAction(v)
	}
	if v, ok := meta["retry"]; ok {
		entry.Retry = metaToAutomationRetry(v)
	}
	if v, ok := meta["goal"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var gc types.GoalConfig
			if err := json.Unmarshal(data, &gc); err == nil {
				entry.Goal = &gc
			}
		}
	}

	// Sessions: map[string]SessionInfo from metadata JSON
	if sessionsRaw, ok := meta["sessions"]; ok {
		if sessionsMap, ok := sessionsRaw.(map[string]interface{}); ok {
			sessions := make(map[string]types.SessionInfo, len(sessionsMap))
			for sid, infoRaw := range sessionsMap {
				si := types.SessionInfo{}
				if infoMap, ok := infoRaw.(map[string]interface{}); ok {
					if ts, ok := infoMap["timestamp"].(string); ok {
						si.Timestamp = ts
					}
				}
				sessions[sid] = si
			}
			entry.Sessions = sessions
		}
	}

	// Schedule fields
	if v, ok := metaString(meta, "schedule"); ok {
		entry.Schedule = v
	}
	if v, ok := metaBool(meta, "schedule_enabled"); ok {
		entry.ScheduleEnabled = &v
	}
	if v, ok := metaBool(meta, "complete_on_idle"); ok {
		entry.CompleteOnIdle = &v
	}
	if v, ok := metaString(meta, "direct_prompt"); ok {
		entry.DirectPrompt = v
	}
	if v, ok := metaInt(meta, "max_runs"); ok {
		entry.MaxRuns = &v
	}
	if v, ok := metaString(meta, "next_run"); ok {
		entry.NextRun = v
	}
	if v, ok := metaString(meta, "run_once_at"); ok {
		entry.RunOnceAt = v
	}
	if v, ok := metaString(meta, "timezone"); ok {
		entry.Timezone = v
	}

	// Runs: []CronRun from metadata JSON
	if runsRaw, ok := meta["runs"]; ok {
		if runsSlice, ok := runsRaw.([]interface{}); ok {
			for _, runRaw := range runsSlice {
				if runMap, ok := runRaw.(map[string]interface{}); ok {
					run := types.CronRun{}
					if v, ok := runMap["run_id"].(string); ok {
						run.RunID = v
					}
					if v, ok := runMap["status"].(string); ok {
						run.Status = v
					}
					if v, ok := runMap["started"].(string); ok {
						run.Started = v
					}
					if v, ok := runMap["completed"].(string); ok {
						run.Completed = v
					}
					if v, ok := runMap["skip_reason"].(string); ok {
						run.SkipReason = v
					}
					entry.Runs = append(entry.Runs, run)
				}
			}
		}
	}
}

// metaString extracts a string value from a metadata map.
func metaString(meta map[string]interface{}, key string) (string, bool) {
	v, ok := meta[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// metaBool extracts a bool value from a metadata map.
func metaBool(meta map[string]interface{}, key string) (bool, bool) {
	v, ok := meta[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// metaInt extracts an int value from a metadata map (JSON numbers are float64).
func metaInt(meta map[string]interface{}, key string) (int, bool) {
	v, ok := meta[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// metaStringSlice extracts a []string from a metadata map.
func metaStringSlice(meta map[string]interface{}, key string) ([]string, bool) {
	v, ok := meta[key]
	if !ok {
		return nil, false
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result, true
}

// computedFeatureToFeature converts a ComputedFeature to a types.Feature.
func computedFeatureToFeature(f *ComputedFeature) types.Feature {
	stats := &types.TaskStats{
		Total:   f.TaskStats.Total,
		Ready:   f.TaskStats.Pending,
		Blocked: f.TaskStats.Blocked,
	}

	return types.Feature{
		FeatureID: f.ID,
		Tasks:     f.Tasks,
		Ready:     f.Classification == "ready",
		Stats:     stats,
	}
}

// featuresToResponse converts a slice of ComputedFeatures to a FeatureListResponse.
func featuresToResponse(features []*ComputedFeature) *types.FeatureListResponse {
	result := make([]types.Feature, 0, len(features))
	for _, f := range features {
		result = append(result, computedFeatureToFeature(f))
	}
	return &types.FeatureListResponse{
		Features: result,
	}
}
