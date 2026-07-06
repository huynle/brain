package runner

// PauseProjectAutomations pauses automation-generated task processing for one project.
func (tr *TaskRunner) PauseProjectAutomations(projectID string) {
	tr.pauseMu.Lock()
	if tr.automationPausedProjects == nil {
		tr.automationPausedProjects = make(map[string]bool)
	}
	tr.automationPausedProjects[projectID] = true
	tr.pauseMu.Unlock()
	tr.wake()
}

// ResumeProjectAutomations resumes automation-generated task processing for one project.
func (tr *TaskRunner) ResumeProjectAutomations(projectID string) {
	tr.pauseMu.Lock()
	delete(tr.automationPausedProjects, projectID)
	tr.pauseMu.Unlock()
	tr.wake()
}

// IsAutomationsPausedForProject reports whether automation-generated tasks are
// paused for a project, by local request or server-side state.
func (tr *TaskRunner) IsAutomationsPausedForProject(projectID string) bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	return tr.automationsPaused || tr.automationPausedProjects[projectID] ||
		tr.serverAutosPaused[""] || tr.serverAutosPaused[projectID]
}
