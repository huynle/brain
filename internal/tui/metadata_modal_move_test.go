package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// ============================================================================
// Filter Dropdown Interaction Mode Tests
// ============================================================================

func TestModeEditFilterDropdown_Constant(t *testing.T) {
	// ModeEditFilterDropdown should be a valid interaction mode
	var mode MetadataInteractionMode = ModeEditFilterDropdown
	if mode == ModeNavigate || mode == ModeEditText || mode == ModeEditDropdown {
		t.Error("ModeEditFilterDropdown should be distinct from other modes")
	}
}

func TestMetadataModal_EnterEditMode_FilterDropdown_FetchesProjects(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	// Focus on MoveToProject field
	modal.focusedField = FieldMoveToProject
	modal.projectsLoaded = false

	// enterEditMode should return a command to fetch projects (not enter edit mode yet)
	cmd := modal.enterEditModeCmd()
	if cmd == nil {
		t.Error("enterEditModeCmd should return non-nil cmd to fetch projects when not loaded")
	}
}

func TestMetadataModal_EnterEditMode_FilterDropdown_AlreadyLoaded(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	// Focus on MoveToProject field with projects already loaded
	modal.focusedField = FieldMoveToProject
	modal.projectsLoaded = true
	modal.projectsList = []string{"project-a", "project-b", "project-c"}

	modal.enterEditMode()

	if modal.interactionMode != ModeEditFilterDropdown {
		t.Errorf("interactionMode = %v, want ModeEditFilterDropdown", modal.interactionMode)
	}
	if modal.editBuffer != "" {
		t.Errorf("editBuffer = %q, want empty", modal.editBuffer)
	}
	if modal.dropdownIndex != 0 {
		t.Errorf("dropdownIndex = %d, want 0", modal.dropdownIndex)
	}
	if len(modal.filteredOptions) != 3 {
		t.Errorf("filteredOptions length = %d, want 3", len(modal.filteredOptions))
	}
}

func TestMetadataModal_ProjectsListedMsg(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)
	modal.focusedField = FieldMoveToProject

	// Simulate receiving projects list
	msg := projectsListedMsg{
		projects: []string{"brain-api", "project-a", "project-b"},
		err:      nil,
	}

	updatedModal, _ := modal.Update(msg)
	m := updatedModal.(*MetadataModal)

	if !m.projectsLoaded {
		t.Error("projectsLoaded should be true after receiving projects")
	}
	if m.interactionMode != ModeEditFilterDropdown {
		t.Errorf("interactionMode = %v, want ModeEditFilterDropdown", m.interactionMode)
	}
	// Current project (brain-api) should be filtered out
	for _, p := range m.filteredOptions {
		if p == "brain-api" {
			t.Error("current project 'brain-api' should be filtered out of options")
		}
	}
	if len(m.filteredOptions) != 2 {
		t.Errorf("filteredOptions length = %d, want 2 (excluding current project)", len(m.filteredOptions))
	}
}

func TestMetadataModal_ProjectsListedMsg_Error(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	msg := projectsListedMsg{
		projects: nil,
		err:      errTestProjectsFetch,
	}

	updatedModal, _ := modal.Update(msg)
	m := updatedModal.(*MetadataModal)

	if m.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %v, want ModeNavigate after error", m.interactionMode)
	}
}

var errTestProjectsFetch = &runner.APIError{StatusCode: 500, Body: "internal error"}

func TestMetadataModal_FilterDropdown_TypeFilters(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	// Setup filter dropdown mode
	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.projectsList = []string{"project-alpha", "project-beta", "other-gamma"}
	modal.filteredOptions = modal.projectsList
	modal.editBuffer = ""
	modal.dropdownIndex = 0

	// Type "alp" to filter (avoid j/k which are navigation keys)
	modal.handleEditFilterDropdownMode("a")
	modal.handleEditFilterDropdownMode("l")
	modal.handleEditFilterDropdownMode("p")

	if modal.editBuffer != "alp" {
		t.Errorf("editBuffer = %q, want %q", modal.editBuffer, "alp")
	}
	// Only "project-alpha" matches "alp"
	if len(modal.filteredOptions) != 1 {
		t.Errorf("filteredOptions length = %d, want 1 (project-alpha)", len(modal.filteredOptions))
	}
	if modal.dropdownIndex != 0 {
		t.Errorf("dropdownIndex = %d, want 0 (reset after filter)", modal.dropdownIndex)
	}
}

func TestMetadataModal_FilterDropdown_Backspace(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.projectsList = []string{"project-alpha", "project-beta", "other-gamma"}
	modal.editBuffer = "proj"
	modal.filterProjects()

	// Backspace should remove last char and re-filter
	modal.handleEditFilterDropdownMode("backspace")

	if modal.editBuffer != "pro" {
		t.Errorf("editBuffer = %q, want %q", modal.editBuffer, "pro")
	}
}

func TestMetadataModal_FilterDropdown_CtrlU(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.projectsList = []string{"project-alpha", "project-beta", "other-gamma"}
	modal.editBuffer = "proj"
	modal.filterProjects()

	// Ctrl+U should clear buffer and show all projects
	modal.handleEditFilterDropdownMode("ctrl+u")

	if modal.editBuffer != "" {
		t.Errorf("editBuffer = %q, want empty", modal.editBuffer)
	}
	if len(modal.filteredOptions) != 3 {
		t.Errorf("filteredOptions length = %d, want 3 (all projects)", len(modal.filteredOptions))
	}
}

func TestMetadataModal_FilterDropdown_Navigation(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.projectsList = []string{"project-a", "project-b", "project-c"}
	modal.filteredOptions = modal.projectsList
	modal.editBuffer = ""
	modal.dropdownIndex = 0

	// j moves down
	modal.handleEditFilterDropdownMode("j")
	if modal.dropdownIndex != 1 {
		t.Errorf("after j: dropdownIndex = %d, want 1", modal.dropdownIndex)
	}

	// k moves up
	modal.handleEditFilterDropdownMode("k")
	if modal.dropdownIndex != 0 {
		t.Errorf("after k: dropdownIndex = %d, want 0", modal.dropdownIndex)
	}

	// down arrow
	modal.handleEditFilterDropdownMode("down")
	if modal.dropdownIndex != 1 {
		t.Errorf("after down: dropdownIndex = %d, want 1", modal.dropdownIndex)
	}

	// up arrow
	modal.handleEditFilterDropdownMode("up")
	if modal.dropdownIndex != 0 {
		t.Errorf("after up: dropdownIndex = %d, want 0", modal.dropdownIndex)
	}

	// Wrap around: k from 0 goes to last
	modal.handleEditFilterDropdownMode("k")
	if modal.dropdownIndex != 2 {
		t.Errorf("after k wrap: dropdownIndex = %d, want 2", modal.dropdownIndex)
	}

	// Wrap around: j from last goes to 0
	modal.handleEditFilterDropdownMode("j")
	if modal.dropdownIndex != 0 {
		t.Errorf("after j wrap: dropdownIndex = %d, want 0", modal.dropdownIndex)
	}
}

func TestMetadataModal_FilterDropdown_Esc(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.editBuffer = "proj"

	handled, _ := modal.handleEditFilterDropdownMode("esc")

	if !handled {
		t.Error("Esc should be handled")
	}
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}
}

func TestMetadataModal_FilterDropdown_Enter_ConfirmsSelection(t *testing.T) {
	// Create test server that handles both entry fetch and move
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			// Move endpoint
			json.NewEncoder(w).Encode(types.MoveResult{
				Success: true,
				From:    "projects/brain-api/task/abc123.md",
				To:      "projects/target-project/task/abc123.md",
				OldPath: "projects/brain-api/task/abc123.md",
				NewPath: "projects/target-project/task/abc123.md",
				Project: "target-project",
				ID:      "abc123",
				Title:   "Test Task",
			})
		} else {
			// Entry fetch
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     "abc123",
				"status": "pending",
			})
		}
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.filteredOptions = []string{"target-project", "other-project"}
	modal.dropdownIndex = 0
	modal.editBuffer = "target"

	handled, cmd := modal.handleEditFilterDropdownMode("enter")

	if !handled {
		t.Error("Enter should be handled")
	}
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %v, want ModeNavigate after enter", modal.interactionMode)
	}
	if modal.values[FieldMoveToProject] != "target-project" {
		t.Errorf("values[FieldMoveToProject] = %q, want %q", modal.values[FieldMoveToProject], "target-project")
	}
	if cmd == nil {
		t.Error("Enter should return a command to execute the move")
	}
}

func TestMetadataModal_MetadataMovedMsg_Success(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	msg := metadataMovedMsg{
		targetProject: "target-project",
		pathMapping: map[string]string{
			"projects/brain-api/task/abc123.md": "projects/target-project/task/abc123.md",
		},
		errors: nil,
		err:    nil,
	}

	updatedModal, _ := modal.Update(msg)
	m := updatedModal.(*MetadataModal)

	if !m.saveSuccess {
		t.Error("saveSuccess should be true after successful move")
	}
	if m.saveError != nil {
		t.Errorf("saveError = %v, want nil", m.saveError)
	}
	if m.projectID != "target-project" {
		t.Errorf("projectID = %q, want %q", m.projectID, "target-project")
	}
	if m.lastSavedField != FieldMoveToProject {
		t.Errorf("lastSavedField = %v, want FieldMoveToProject", m.lastSavedField)
	}
	// Task paths should be updated
	if len(m.taskPaths) != 1 || m.taskPaths[0] != "projects/target-project/task/abc123.md" {
		t.Errorf("taskPaths = %v, want updated paths", m.taskPaths)
	}
}

func TestMetadataModal_MetadataMovedMsg_Error(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	moveErr := &runner.APIError{StatusCode: 404, Body: "not found"}
	msg := metadataMovedMsg{
		targetProject: "target-project",
		pathMapping:   nil,
		errors:        []error{moveErr},
		err:           moveErr,
	}

	updatedModal, _ := modal.Update(msg)
	m := updatedModal.(*MetadataModal)

	if m.saveSuccess {
		t.Error("saveSuccess should be false after move error")
	}
	if m.saveError == nil {
		t.Error("saveError should be set after move error")
	}
}

func TestMetadataModal_HandleKey_RoutesToFilterDropdown(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.interactionMode = ModeEditFilterDropdown
	modal.focusedField = FieldMoveToProject
	modal.projectsList = []string{"project-a", "project-b"}
	modal.filteredOptions = modal.projectsList
	modal.editBuffer = ""
	modal.dropdownIndex = 0

	// j should be handled in filter dropdown mode
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("HandleKey should handle 'j' in ModeEditFilterDropdown")
	}
	if modal.dropdownIndex != 1 {
		t.Errorf("dropdownIndex = %d, want 1", modal.dropdownIndex)
	}
}

func TestMetadataModal_FilterProjects(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.projectsList = []string{"project-alpha", "project-beta", "other-gamma", "UPPER-CASE"}

	// Empty query shows all
	modal.editBuffer = ""
	modal.filterProjects()
	if len(modal.filteredOptions) != 4 {
		t.Errorf("empty query: filteredOptions = %d, want 4", len(modal.filteredOptions))
	}

	// Case-insensitive filter
	modal.editBuffer = "project"
	modal.filterProjects()
	if len(modal.filteredOptions) != 2 {
		t.Errorf("'project' query: filteredOptions = %d, want 2", len(modal.filteredOptions))
	}

	// Case-insensitive match
	modal.editBuffer = "UPPER"
	modal.filterProjects()
	if len(modal.filteredOptions) != 1 {
		t.Errorf("'UPPER' query: filteredOptions = %d, want 1", len(modal.filteredOptions))
	}

	// No matches
	modal.editBuffer = "nonexistent"
	modal.filterProjects()
	if len(modal.filteredOptions) != 0 {
		t.Errorf("'nonexistent' query: filteredOptions = %d, want 0", len(modal.filteredOptions))
	}
}

func TestMetadataModal_SaveField_MoveToProject_UsesSaveMoveField(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	// When focusedField is FieldMoveToProject, saveField should delegate to saveMoveField
	modal.focusedField = FieldMoveToProject
	modal.filteredOptions = []string{"target-project"}
	modal.dropdownIndex = 0
	modal.editBuffer = ""

	cmd := modal.saveField()
	// saveMoveField returns a command (or nil if empty target)
	// With filteredOptions set, it should return a non-nil command
	if cmd == nil {
		t.Error("saveField for FieldMoveToProject should return a command")
	}
}

func TestMetadataModal_GetFieldDisplayValue_MoveToProject(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	// Should show current project extracted from task path
	value := modal.getFieldDisplayValue(FieldMoveToProject)
	if value == "" {
		t.Error("getFieldDisplayValue for MoveToProject should not be empty")
	}
	// Not asserted beyond non-emptiness: the value may carry ANSI styling, and
	// modal.values[FieldMoveToProject] is legitimately empty until edited.
}

func TestMetadataModal_RenderFilterDropdown(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/abc123.md", apiClient)

	modal.filteredOptions = []string{"project-a", "project-b", "project-c"}
	modal.dropdownIndex = 1
	modal.editBuffer = "proj"

	rendered := modal.renderFilterDropdown()
	if rendered == "" {
		t.Error("renderFilterDropdown should return non-empty string")
	}
}
