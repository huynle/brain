package service

import (
	"context"
	"sync"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// Compile-time check that RunnerServiceImpl implements api.RunnerService.
var _ api.RunnerService = (*RunnerServiceImpl)(nil)

// RunnerServiceImpl implements api.RunnerService with in-memory pause state.
// This is a stub implementation that tracks pause/resume state without
// actually controlling task execution (that's the runner's job).
type RunnerServiceImpl struct {
	mu             sync.RWMutex
	globalPaused   bool
	pausedProjects map[string]bool
}

// NewRunnerService creates a new RunnerServiceImpl.
func NewRunnerService() *RunnerServiceImpl {
	return &RunnerServiceImpl{
		pausedProjects: make(map[string]bool),
	}
}

// Pause pauses task execution for a specific project.
func (s *RunnerServiceImpl) Pause(_ context.Context, projectId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausedProjects[projectId] = true
	return nil
}

// Resume resumes task execution for a specific project.
func (s *RunnerServiceImpl) Resume(_ context.Context, projectId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pausedProjects, projectId)
	return nil
}

// PauseAll pauses task execution for all projects.
func (s *RunnerServiceImpl) PauseAll(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalPaused = true
	return nil
}

// ResumeAll resumes task execution for all projects.
func (s *RunnerServiceImpl) ResumeAll(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalPaused = false
	// Also clear per-project pauses
	s.pausedProjects = make(map[string]bool)
	return nil
}

// GetStatus returns the current runner status.
func (s *RunnerServiceImpl) GetStatus(_ context.Context) (*types.RunnerStatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	paused := s.globalPaused || len(s.pausedProjects) > 0

	var pausedProjects []string
	for p := range s.pausedProjects {
		pausedProjects = append(pausedProjects, p)
	}

	return &types.RunnerStatusResponse{
		Running:        true, // API server is always "running"
		Paused:         paused,
		PausedProjects: pausedProjects,
	}, nil
}

// IsPaused returns true if the given project is paused (either globally or per-project).
func (s *RunnerServiceImpl) IsPaused(projectId string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.globalPaused || s.pausedProjects[projectId]
}
