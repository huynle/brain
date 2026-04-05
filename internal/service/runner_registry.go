package service

import (
	"context"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Thresholds for computing runner status from heartbeat age.
const (
	// RunnerOnlineThreshold is the max heartbeat age for "online" status.
	RunnerOnlineThreshold = 90 * time.Second
	// RunnerStaleThreshold is the max heartbeat age for "stale" status.
	// Beyond this, the runner is considered "offline".
	RunnerStaleThreshold = 5 * time.Minute
)

// Compile-time check that RunnerRegistryServiceImpl implements api.RunnerRegistryService.
var _ api.RunnerRegistryService = (*RunnerRegistryServiceImpl)(nil)

// RunnerRegistryServiceImpl implements api.RunnerRegistryService using the storage layer.
type RunnerRegistryServiceImpl struct {
	storage *storage.StorageLayer
}

// NewRunnerRegistryService creates a new RunnerRegistryServiceImpl.
func NewRunnerRegistryService(store *storage.StorageLayer) *RunnerRegistryServiceImpl {
	return &RunnerRegistryServiceImpl{storage: store}
}

// Register registers or re-registers a runner. Sets status to online and
// timestamps to now.
func (s *RunnerRegistryServiceImpl) Register(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error) {
	now := time.Now().UnixMilli()

	maxParallel := req.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 1
	}

	labels := req.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	executors := req.Executors
	if executors == nil {
		executors = []string{}
	}

	row := &storage.RunnerRow{
		RunnerID:      req.RunnerID,
		Hostname:      req.Hostname,
		Labels:        labels,
		Executors:     executors,
		MaxParallel:   maxParallel,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	}

	if err := s.storage.UpsertRunner(ctx, row); err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}

	return rowToRunnerInfo(row), nil
}

// Heartbeat updates a runner's heartbeat timestamp and running task count.
func (s *RunnerRegistryServiceImpl) Heartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
	if err := s.storage.UpdateHeartbeat(ctx, runnerID, req.RunningTasks, req.Stats); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	return nil
}

// Deregister removes a runner and releases all its task claims.
func (s *RunnerRegistryServiceImpl) Deregister(ctx context.Context, runnerID string) error {
	// Release all claims held by this runner first
	_, err := s.storage.ReleaseAllByRunner(ctx, runnerID)
	if err != nil {
		return fmt.Errorf("release claims on deregister: %w", err)
	}

	// Delete the runner record
	deleted, err := s.storage.DeleteRunner(ctx, runnerID)
	if err != nil {
		return fmt.Errorf("deregister runner: %w", err)
	}
	if !deleted {
		return api.ErrNotFound
	}

	return nil
}

// ListRunners returns all runners with computed status based on heartbeat age.
func (s *RunnerRegistryServiceImpl) ListRunners(ctx context.Context) (*types.RunnerListResponse, error) {
	rows, err := s.storage.ListRunners(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}

	runners := make([]types.RunnerInfo, 0, len(rows))
	for i := range rows {
		info := rowToRunnerInfo(&rows[i])
		info.Status = computeRunnerStatus(rows[i].LastHeartbeat)
		runners = append(runners, *info)
	}

	return &types.RunnerListResponse{
		Runners: runners,
		Total:   len(runners),
	}, nil
}

// GetRunner returns a single runner by ID with computed status.
func (s *RunnerRegistryServiceImpl) GetRunner(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
	row, err := s.storage.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, fmt.Errorf("get runner: %w", err)
	}
	if row == nil {
		return nil, api.ErrNotFound
	}

	info := rowToRunnerInfo(row)
	info.Status = computeRunnerStatus(row.LastHeartbeat)
	return info, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// computeRunnerStatus computes the runner status from the last heartbeat timestamp.
// online: heartbeat < 90s ago
// stale:  heartbeat between 90s and 5min ago
// offline: heartbeat > 5min ago
func computeRunnerStatus(lastHeartbeatMs int64) types.RunnerStatus {
	age := time.Since(time.UnixMilli(lastHeartbeatMs))

	if age < RunnerOnlineThreshold {
		return types.RunnerStatusOnline
	}
	if age < RunnerStaleThreshold {
		return types.RunnerStatusStale
	}
	return types.RunnerStatusOffline
}

// rowToRunnerInfo converts a storage RunnerRow to an API RunnerInfo.
func rowToRunnerInfo(row *storage.RunnerRow) *types.RunnerInfo {
	return &types.RunnerInfo{
		RunnerID:      row.RunnerID,
		Hostname:      row.Hostname,
		Labels:        row.Labels,
		Executors:     row.Executors,
		MaxParallel:   row.MaxParallel,
		FeatureIDs:    row.FeatureIDs,
		RegisteredAt:  time.UnixMilli(row.RegisteredAt).UTC().Format(time.RFC3339),
		LastHeartbeat: time.UnixMilli(row.LastHeartbeat).UTC().Format(time.RFC3339),
		Status:        types.RunnerStatus(row.Status),
	}
}
