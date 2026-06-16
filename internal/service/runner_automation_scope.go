package service

import "context"

// PauseProjectAutomations pauses automation-generated task execution for one project.
func (s *RunnerServiceImpl) PauseProjectAutomations(_ context.Context, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.automationPausedProjects == nil {
		s.automationPausedProjects = make(map[string]bool)
	}
	s.automationPausedProjects[projectID] = true
	return nil
}

// ResumeProjectAutomations resumes automation-generated task execution for one project.
func (s *RunnerServiceImpl) ResumeProjectAutomations(_ context.Context, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.automationPausedProjects, projectID)
	return nil
}
