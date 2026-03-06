package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Compile-time check that TaskServiceImpl implements api.TaskService.
var _ api.TaskService = (*TaskServiceImpl)(nil)

// TaskServiceImpl implements api.TaskService using a StorageLayer and in-memory claims.
type TaskServiceImpl struct {
	config  *config.Config
	storage *storage.StorageLayer

	mu     sync.Mutex
	claims map[string]*types.TaskClaim // key: "projectId:taskId"
}

// NewTaskService creates a new TaskServiceImpl.
func NewTaskService(cfg *config.Config, store *storage.StorageLayer) *TaskServiceImpl {
	return &TaskServiceImpl{
		config:  cfg,
		storage: store,
		claims:  make(map[string]*types.TaskClaim),
	}
}

// claimKey returns the composite key for a task claim.
func claimKey(projectId, taskId string) string {
	return projectId + ":" + taskId
}

// staleClaimThreshold is the duration after which a claim is considered stale.
const staleClaimThreshold = 10 * time.Minute

// isStale returns true if the claim is older than staleClaimThreshold.
func isStale(claim *types.TaskClaim) bool {
	claimedAt := time.UnixMilli(claim.ClaimedAt)
	return time.Since(claimedAt) > staleClaimThreshold
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
func (s *TaskServiceImpl) GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
	entries, err := s.getAllTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return ResolveDependencies(entries), nil
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
func (s *TaskServiceImpl) GetReady(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return GetReadyTasks(result), nil
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
func (s *TaskServiceImpl) GetNext(ctx context.Context, projectId string) (*types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return GetNextTask(result), nil
}

// ClaimTask claims a task for a runner. Returns ErrConflict if already claimed by another runner.
func (s *TaskServiceImpl) ClaimTask(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error) {
	key := claimKey(projectId, taskId)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.claims[key]; ok {
		stale := isStale(existing)
		if !stale && existing.RunnerID != runnerId {
			return &types.ClaimResponse{
				Success:   false,
				TaskID:    taskId,
				RunnerID:  runnerId,
				Error:     "already claimed",
				Message:   fmt.Sprintf("task %s is already claimed by %s", taskId, existing.RunnerID),
				ClaimedBy: existing.RunnerID,
				IsStale:   &stale,
			}, api.ErrConflict
		}
		// Stale claim or same runner — allow re-claim
	}

	now := time.Now().UnixMilli()
	s.claims[key] = &types.TaskClaim{
		RunnerID:  runnerId,
		ClaimedAt: now,
	}

	claimedAt := time.UnixMilli(now).UTC().Format(time.RFC3339)
	return &types.ClaimResponse{
		Success:   true,
		TaskID:    taskId,
		RunnerID:  runnerId,
		ClaimedAt: claimedAt,
	}, nil
}

// ReleaseTask releases a task claim. Returns ErrNotFound if not claimed.
func (s *TaskServiceImpl) ReleaseTask(ctx context.Context, projectId, taskId, runnerId string) error {
	key := claimKey(projectId, taskId)

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.claims[key]
	if !ok {
		return api.ErrNotFound
	}

	if existing.RunnerID != runnerId {
		return api.ErrConflict
	}

	delete(s.claims, key)
	return nil
}

// GetClaimStatus returns the claim status of a task.
func (s *TaskServiceImpl) GetClaimStatus(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error) {
	key := claimKey(projectId, taskId)

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.claims[key]
	if !ok {
		return &types.ClaimStatusResponse{
			TaskID:  taskId,
			Claimed: false,
			IsStale: false,
		}, nil
	}

	stale := isStale(existing)
	claimedAt := time.UnixMilli(existing.ClaimedAt).UTC().Format(time.RFC3339)
	return &types.ClaimStatusResponse{
		TaskID:    taskId,
		Claimed:   true,
		RunnerID:  existing.RunnerID,
		ClaimedAt: claimedAt,
		IsStale:   stale,
	}, nil
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

// CheckoutFeature marks a feature for checkout. Stub implementation.
func (s *TaskServiceImpl) CheckoutFeature(ctx context.Context, projectId, featureId string) error {
	return nil
}

// TriggerTask manually triggers a scheduled task. Stub implementation.
func (s *TaskServiceImpl) TriggerTask(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
	return &types.TriggerResponse{
		Success:   true,
		TaskID:    taskId,
		Triggered: true,
	}, nil
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
	if v, ok := metaBool(meta, "complete_on_idle"); ok {
		entry.CompleteOnIdle = &v
	}
	if v, ok := metaString(meta, "target_workdir"); ok {
		entry.TargetWorkdir = v
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
