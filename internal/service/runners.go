package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// staleDuration is the time after which a runner without heartbeat is "lost".
const staleDuration = 2 * time.Minute

// Compile-time check that RunnersServiceImpl implements api.RunnersService.
var _ api.RunnersService = (*RunnersServiceImpl)(nil)

// RunnersServiceImpl implements api.RunnersService using the storage layer.
type RunnersServiceImpl struct {
	store *storage.StorageLayer
	tasks api.TaskService
}

// NewRunnersService creates a new RunnersServiceImpl.
func NewRunnersService(store *storage.StorageLayer, tasks api.TaskService) *RunnersServiceImpl {
	return &RunnersServiceImpl{
		store: store,
		tasks: tasks,
	}
}

// Register registers a runner or updates its existing registration.
func (s *RunnersServiceImpl) Register(ctx context.Context, req types.RegisterRunnerRequest) (*types.RunnerInfo, error) {
	row := storage.RunnerRow{
		RunnerID:     req.RunnerID,
		Hostname:     req.Hostname,
		Projects:     storage.ToJSONStringArray(req.Projects),
		Capabilities: storage.ToJSONStringArray(req.Capabilities),
		MaxParallel:  req.MaxParallel,
		Version:      req.Version,
	}
	if row.MaxParallel < 1 {
		row.MaxParallel = 1
	}

	if err := s.store.RegisterRunner(ctx, row); err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}

	// Read back the full record to get timestamps
	saved, err := s.store.GetRunnerByID(ctx, req.RunnerID)
	if err != nil {
		return nil, fmt.Errorf("get registered runner: %w", err)
	}
	if saved == nil {
		return nil, fmt.Errorf("runner not found after registration: %s", req.RunnerID)
	}

	return rowToRunnerInfo(saved), nil
}

// Heartbeat updates a runner's last heartbeat.
func (s *RunnersServiceImpl) Heartbeat(ctx context.Context, req types.HeartbeatRequest) error {
	return s.store.HeartbeatRunner(ctx, req.RunnerID, req.ActiveTasks, req.Version)
}

// Delete removes a runner registration.
func (s *RunnersServiceImpl) Delete(ctx context.Context, runnerID string) error {
	return s.store.DeleteRunner(ctx, runnerID)
}

// List returns all registered runners with computed status.
func (s *RunnersServiceImpl) List(ctx context.Context) (*types.RunnerListResponse, error) {
	rows, err := s.store.ListRunners(ctx, staleDuration)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}

	runners := make([]types.RunnerInfo, 0, len(rows))
	for _, r := range rows {
		runners = append(runners, *rowToRunnerInfo(&r))
	}

	return &types.RunnerListResponse{
		Runners: runners,
		Total:   len(runners),
	}, nil
}

// MarkStaleAndRelease detects stale runners, marks them as "lost",
// and releases any tasks they had claimed.
func (s *RunnersServiceImpl) MarkStaleAndRelease(ctx context.Context) ([]string, error) {
	staleIDs, err := s.store.MarkStaleRunners(ctx, staleDuration)
	if err != nil {
		return nil, fmt.Errorf("mark stale runners: %w", err)
	}

	if len(staleIDs) > 0 {
		slog.Warn("detected stale runners", "count", len(staleIDs), "runners", staleIDs)

		// Release tasks claimed by stale runners.
		// We iterate through projects and try to release tasks.
		// This is best-effort — some releases may fail if the task
		// was already completed or released.
		if s.tasks != nil {
			projects, err := s.tasks.ListProjects(ctx)
			if err != nil {
				slog.Error("failed to list projects for stale runner cleanup", "error", err)
			} else {
				for _, projectID := range projects {
					for _, runnerID := range staleIDs {
						s.releaseTasksForRunner(ctx, projectID, runnerID)
					}
				}
			}
		}
	}

	return staleIDs, nil
}

// releaseTasksForRunner releases all tasks claimed by a specific runner in a project.
func (s *RunnersServiceImpl) releaseTasksForRunner(ctx context.Context, projectID, runnerID string) {
	resp, err := s.tasks.GetTasks(ctx, projectID)
	if err != nil {
		slog.Debug("failed to get tasks for stale cleanup", "project", projectID, "error", err)
		return
	}

	for _, task := range resp.Tasks {
		if task.Status != "in_progress" {
			continue
		}
		// Try to release — this is best-effort
		claimStatus, err := s.tasks.GetClaimStatus(ctx, projectID, task.ID)
		if err != nil {
			continue
		}
		if claimStatus.Claimed && claimStatus.RunnerID == runnerID {
			if err := s.tasks.ReleaseTask(ctx, projectID, task.ID, runnerID); err != nil {
				slog.Debug("failed to release task for stale runner",
					"task", task.ID, "runner", runnerID, "error", err)
			} else {
				slog.Info("released task from stale runner",
					"task", task.ID, "runner", runnerID, "project", projectID)
			}
		}
	}
}

// rowToRunnerInfo converts a storage RunnerRow to a types.RunnerInfo.
func rowToRunnerInfo(r *storage.RunnerRow) *types.RunnerInfo {
	return &types.RunnerInfo{
		RunnerID:      r.RunnerID,
		Hostname:      r.Hostname,
		Projects:      storage.ParseJSONStringArray(r.Projects),
		Capabilities:  storage.ParseJSONStringArray(r.Capabilities),
		MaxParallel:   r.MaxParallel,
		ActiveTasks:   r.ActiveTasks,
		Status:        r.Status,
		Version:       r.Version,
		RegisteredAt:  r.RegisteredAt,
		LastHeartbeat: r.LastHeartbeat,
	}
}
