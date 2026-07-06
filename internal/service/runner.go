package service

import (
	"context"
	"sync"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Compile-time check that RunnerServiceImpl implements api.RunnerService.
var _ api.RunnerService = (*RunnerServiceImpl)(nil)

// RunnerServiceImpl implements api.RunnerService with in-memory pause state.
// This is a stub implementation that tracks pause/resume state without
// actually controlling task execution (that's the runner's job).
type RunnerServiceImpl struct {
	store                    *storage.StorageLayer
	mu                       sync.RWMutex
	globalPaused             bool
	automationsPaused        bool
	automationPausedProjects map[string]bool
	pausedProjects           map[string]bool
}

// NewRunnerService creates a new RunnerServiceImpl.
func NewRunnerService() *RunnerServiceImpl {
	return NewRunnerServiceWithStorage(nil)
}

// NewRunnerServiceWithStorage creates a RunnerServiceImpl backed by durable storage.
func NewRunnerServiceWithStorage(store *storage.StorageLayer) *RunnerServiceImpl {
	return &RunnerServiceImpl{
		store:                    store,
		pausedProjects:           make(map[string]bool),
		automationPausedProjects: make(map[string]bool),
	}
}

// Pause pauses task execution for a specific project.
func (s *RunnerServiceImpl) Pause(ctx context.Context, projectId string) error {
	if s.store != nil {
		return s.store.SetProjectTaskPaused(ctx, projectId, true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausedProjects[projectId] = true
	return nil
}

// Resume resumes task execution for a specific project.
func (s *RunnerServiceImpl) Resume(ctx context.Context, projectId string) error {
	if s.store != nil {
		return s.store.SetProjectTaskPaused(ctx, projectId, false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pausedProjects, projectId)
	return nil
}

// PauseAll pauses task execution for all projects.
func (s *RunnerServiceImpl) PauseAll(ctx context.Context) error {
	if s.store != nil {
		return s.store.SetAllProjectTasksPaused(ctx, true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalPaused = true
	return nil
}

// ResumeAll resumes task execution for all projects.
func (s *RunnerServiceImpl) ResumeAll(ctx context.Context) error {
	if s.store != nil {
		return s.store.SetAllProjectTasksPaused(ctx, false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalPaused = false
	// Also clear per-project pauses
	s.pausedProjects = make(map[string]bool)
	return nil
}

// PauseAutomations pauses automation-generated task execution.
func (s *RunnerServiceImpl) PauseAutomations(ctx context.Context) error {
	if s.store != nil {
		return s.store.SetAllProjectAutomationsPaused(ctx, true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automationsPaused = true
	return nil
}

// ResumeAutomations resumes automation-generated task execution.
func (s *RunnerServiceImpl) ResumeAutomations(ctx context.Context) error {
	if s.store != nil {
		return s.store.SetAllProjectAutomationsPaused(ctx, false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automationsPaused = false
	return nil
}

// GetStatus returns the current runner status.
func (s *RunnerServiceImpl) GetStatus(ctx context.Context) (*types.RunnerStatusResponse, error) {
	if s.store != nil {
		rows, err := s.store.ListProjectPauseStates(ctx)
		if err != nil {
			return nil, err
		}
		var pausedProjects []string
		var automationPausedProjects []string
		for _, row := range rows {
			if row.TasksPaused {
				pausedProjects = append(pausedProjects, row.ProjectID)
			}
			if row.AutomationsPaused {
				automationPausedProjects = append(automationPausedProjects, row.ProjectID)
			}
		}
		return &types.RunnerStatusResponse{
			Running:                  true,
			Paused:                   len(pausedProjects) > 0,
			PausedProjects:           pausedProjects,
			AutomationsPaused:        len(automationPausedProjects) > 0,
			AutomationPausedProjects: automationPausedProjects,
		}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	paused := s.globalPaused || len(s.pausedProjects) > 0

	var pausedProjects []string
	for p := range s.pausedProjects {
		pausedProjects = append(pausedProjects, p)
	}
	var automationPausedProjects []string
	for p := range s.automationPausedProjects {
		automationPausedProjects = append(automationPausedProjects, p)
	}

	return &types.RunnerStatusResponse{
		Running:                  true, // API server is always "running"
		Paused:                   paused,
		PausedProjects:           pausedProjects,
		AutomationsPaused:        s.automationsPaused,
		AutomationPausedProjects: automationPausedProjects,
	}, nil
}

// IsAutomationsPaused returns true if automation-generated task execution is
// paused. When storage is enabled it reflects the persisted per-project
// pause rows (any project paused ⇒ true); otherwise it uses in-memory state.
//
// NOTE: this is the *global* signal used by legacy callers. Per-project
// enforcement should call IsAutomationsPausedForProject instead so that a
// pause on project A does not silently stop project B.
func (s *RunnerServiceImpl) IsAutomationsPaused() bool {
	if s.store != nil {
		rows, err := s.store.ListProjectPauseStates(context.Background())
		if err == nil {
			for _, row := range rows {
				if row.AutomationsPaused {
					return true
				}
			}
			return false
		}
		// fall through to in-memory on error
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.automationsPaused
}

// IsAutomationsPausedForProject returns true when automation-generated task
// execution is paused for the given project. It consults the durable
// project_pause_state row when storage is enabled and falls back to the
// in-memory map otherwise. This is the check that must succeed to honor
// the per-project "autos: off" state exposed in the PWA.
func (s *RunnerServiceImpl) IsAutomationsPausedForProject(projectID string) bool {
	if projectID == "" {
		return false
	}
	if s.store != nil {
		paused, err := s.store.IsProjectAutomationsPaused(context.Background(), projectID)
		if err == nil {
			return paused
		}
		// fall through to in-memory on error
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.automationPausedProjects[projectID] {
		return true
	}
	return s.automationsPaused
}

// IsPaused returns true if the given project is paused (either globally or
// per-project). Consults durable storage when available.
func (s *RunnerServiceImpl) IsPaused(projectId string) bool {
	if projectId == "" {
		return false
	}
	if s.store != nil {
		paused, err := s.store.IsProjectTaskPaused(context.Background(), projectId)
		if err == nil {
			return paused
		}
		// fall through to in-memory on error
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.globalPaused || s.pausedProjects[projectId]
}
