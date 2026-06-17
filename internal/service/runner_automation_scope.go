package service

import "context"

// PauseProjectAutomations pauses automation-generated task execution for one project.
func (s *RunnerServiceImpl) PauseProjectAutomations(ctx context.Context, projectID string) error {
	if s.store != nil {
		return s.store.SetProjectAutomationsPaused(ctx, projectID, true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.automationPausedProjects == nil {
		s.automationPausedProjects = make(map[string]bool)
	}
	s.automationPausedProjects[projectID] = true
	return nil
}

// ResumeProjectAutomations resumes automation-generated task execution for one project.
func (s *RunnerServiceImpl) ResumeProjectAutomations(ctx context.Context, projectID string) error {
	if s.store != nil {
		return s.store.SetProjectAutomationsPaused(ctx, projectID, false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.automationPausedProjects, projectID)
	return nil
}
