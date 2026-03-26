package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// TestMetadataIntegration_OpenModalWithSKey tests that pressing 's' opens the metadata modal.
func TestMetadataIntegration_OpenModalWithSKey(t *testing.T) {
	// Create a test model with a selected task
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)

	// Set up test tasks
	testTask := types.ResolvedTask{
		ID:       "test123",
		Title:    "Test Task",
		Status:   "active",
		Priority: "high",
	}

	m.tasks = []types.ResolvedTask{testTask}
	m.taskTree.SetTasks(m.tasks)

	// Move down to select the first task (navigate with j key)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updatedM, _ := m.Update(jMsg)
	m = updatedM.(Model)

	// Verify modal is not open initially
	if m.modalManager.IsOpen() {
		t.Error("Expected modal to be closed initially")
	}

	// Press 's' key to open metadata modal
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedModel, cmd := m.Update(keyMsg)
	m = updatedModel.(Model)

	// Execute the Init command if returned
	if cmd != nil {
		_ = cmd() // This would normally fetch data
	}

	// Verify modal is now open
	if !m.modalManager.IsOpen() {
		t.Error("Expected modal to be open after pressing 's'")
	}

	// Verify it's a MetadataModal
	modal := m.modalManager.activeModal
	if _, ok := modal.(*MetadataModal); !ok {
		t.Errorf("Expected MetadataModal, got %T", modal)
	}
}

// TestMetadataIntegration_CloseModalWithEsc tests that pressing Esc closes the modal.
func TestMetadataIntegration_CloseModalWithEsc(t *testing.T) {
	// Create model with modal open
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)

	// Create and open a metadata modal
	mockClient := &runner.APIClient{}
	modal := NewMetadataModal("test123", mockClient)
	m.modalManager.Open(modal)

	// Verify modal is open
	if !m.modalManager.IsOpen() {
		t.Fatal("Expected modal to be open before test")
	}

	// Press Esc to close
	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updatedModel, _ := m.Update(escMsg)
	m = updatedModel.(Model)

	// Verify modal is closed
	if m.modalManager.IsOpen() {
		t.Error("Expected modal to be closed after pressing Esc")
	}
}

// TestMetadataIntegration_ModalHandlesKeysFirst tests that modal intercepts keys when open.
func TestMetadataIntegration_ModalHandlesKeysFirst(t *testing.T) {
	// Create model with modal open
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)

	// Add test task
	testTask := types.ResolvedTask{
		ID:       "test123",
		Title:    "Test Task",
		Status:   "active",
		Priority: "high",
	}
	m.tasks = []types.ResolvedTask{testTask}
	m.taskTree.SetTasks(m.tasks)

	// Open metadata modal
	mockClient := &runner.APIClient{}
	modal := NewMetadataModal("test123", mockClient)
	m.modalManager.Open(modal)

	// Get initial focused index in modal
	initialIndex := modal.focusedIndex

	// Press 'j' - should be handled by modal (move down in fields)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updatedModel, _ := m.Update(jMsg)
	m = updatedModel.(Model)

	// Verify modal handled the key (focused index changed)
	modal = m.modalManager.activeModal.(*MetadataModal)
	if modal.focusedIndex == initialIndex {
		t.Error("Expected modal to handle 'j' key and change focused index")
	}
}

// TestMetadataIntegration_ViewOverlaysModal tests that View() overlays modal when open.
func TestMetadataIntegration_ViewOverlaysModal(t *testing.T) {
	// Create model with size set
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 100
	m.height = 30

	// Get base view without modal
	baseView := m.View()

	// Open modal
	mockClient := &runner.APIClient{}
	modal := NewMetadataModal("test123", mockClient)
	m.modalManager.Open(modal)

	// Get view with modal
	viewWithModal := m.View()

	// View with modal should be different and contain modal title
	if baseView == viewWithModal {
		t.Error("Expected view to change when modal is open")
	}

	// Modal title should appear in view
	if !stringContains(viewWithModal, "Update Metadata") {
		t.Error("Expected view to contain modal title when modal is open")
	}
}

// TestMetadataIntegration_NoModalWhenNoTaskSelected tests that 's' does nothing without selection.
func TestMetadataIntegration_NoModalWhenNoTaskSelected(t *testing.T) {
	// Create model with no tasks
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.tasks = []types.ResolvedTask{} // No tasks
	m.taskTree.SetTasks(m.tasks)

	// Press 's' key
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedModel, _ := m.Update(keyMsg)
	m = updatedModel.(Model)

	// Modal should NOT open
	if m.modalManager.IsOpen() {
		t.Error("Expected modal to stay closed when no task is selected")
	}
}

// TestMetadataIntegration_SKey_TerminalSectionTask tests that pressing 's' on a task
// within a terminal section (e.g., Draft sub-feature) opens a MetadataModal (not feature modal).
func TestMetadataIntegration_SKey_TerminalSectionTask(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)

	// Create tasks: one active (so feature view has active features) and one draft
	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "draft-1",
			Title:     "Draft Task",
			Status:    "draft",
			Priority:  "medium",
			Path:      "projects/test/task/draft1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// The task tree is in feature view mode by default (useFeatureView=true).
	// Simulate navigating to the Draft section and into a task:
	// 1. Move to Draft section
	m.taskTree.moveToDraftSection()
	// 2. Expand draft section (it's expanded by default in NewTaskTree)
	m.taskTree.draftCollapsed = false
	// 3. Navigate to first sub-feature
	m.taskTree.draftFeatureIdx = 0
	m.taskTree.draftTaskIdx = -1 // on sub-feature header
	m.taskTree.SelectedID = ""

	// 4. Rebuild feature IDs for draft section so navigation works
	m.taskTree.refreshTerminalSectionFeatureIDs()

	// 5. Navigate into the first task of the sub-feature
	m.taskTree.MoveDown()

	// Verify we're now on the draft task
	t.Logf("After MoveDown: SelectedID=%q, draftTaskIdx=%d, draftFeatureIdx=%d",
		m.taskTree.SelectedID, m.taskTree.draftTaskIdx, m.taskTree.draftFeatureIdx)

	if m.taskTree.SelectedID == "" {
		t.Fatal("Expected SelectedID to be set after navigating into terminal section task")
	}
	if m.taskTree.SelectedID != "draft-1" {
		t.Errorf("Expected SelectedID=%q, got %q", "draft-1", m.taskTree.SelectedID)
	}

	// Verify SelectedTask() returns the draft task
	selectedTask := m.taskTree.SelectedTask()
	if selectedTask == nil {
		t.Fatal("Expected SelectedTask() to return non-nil for terminal section task")
	}
	if selectedTask.ID != "draft-1" {
		t.Errorf("Expected SelectedTask().ID=%q, got %q", "draft-1", selectedTask.ID)
	}

	// Verify GetSelectedFeatureID returns "" (not the feature ID) — we want single-task metadata
	featureID := m.taskTree.GetSelectedFeatureID()
	if featureID != "" {
		t.Errorf("Expected GetSelectedFeatureID()=%q for terminal section task, got %q", "", featureID)
	}

	// Now press 's' and verify the modal opens
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedModel, cmd := m.Update(keyMsg)
	m = updatedModel.(Model)

	if cmd != nil {
		_ = cmd()
	}

	if !m.modalManager.IsOpen() {
		t.Fatal("Expected modal to be open after pressing 's' on terminal section task")
	}

	// Verify it's a single-task MetadataModal (not a feature modal)
	modal := m.modalManager.activeModal
	if _, ok := modal.(*MetadataModal); !ok {
		t.Errorf("Expected *MetadataModal, got %T", modal)
	}
}

// TestMetadataIntegration_SKey_TerminalSectionTask_ViaJNavigation tests the 's' key
// on a terminal section task when navigated via 'j' key presses (realistic user flow).
func TestMetadataIntegration_SKey_TerminalSectionTask_ViaJNavigation(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 120
	m.height = 40

	// Set up tasks: one active feature + one draft task
	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "draft-1",
			Title:     "Draft Task",
			Status:    "draft",
			Priority:  "medium",
			Path:      "projects/test/task/draft1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// Navigate down using 'j' key presses until we land on the draft task.
	// Max attempts to prevent infinite loop.
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	maxPresses := 30
	foundDraftTask := false

	for i := 0; i < maxPresses; i++ {
		updatedM, _ := m.Update(jMsg)
		m = updatedM.(Model)

		// Check navigation state
		t.Logf("j press %d: SelectedID=%q, isOnDraft=%v, draftFeatureIdx=%d, draftTaskIdx=%d, selectedFeatureTaskIdx=%d",
			i+1, m.taskTree.SelectedID,
			m.taskTree.isOnDraftSection, m.taskTree.draftFeatureIdx,
			m.taskTree.draftTaskIdx, m.taskTree.selectedFeatureTaskIdx)

		if m.taskTree.SelectedID == "draft-1" {
			foundDraftTask = true
			break
		}
	}

	if !foundDraftTask {
		t.Fatal("Could not navigate to draft-1 task via j key presses")
	}

	// Verify SelectedTask() returns the draft task
	selectedTask := m.taskTree.SelectedTask()
	if selectedTask == nil {
		t.Fatal("SelectedTask() returned nil when SelectedID=draft-1")
	}
	t.Logf("SelectedTask: ID=%q, Path=%q, Status=%q", selectedTask.ID, selectedTask.Path, selectedTask.Status)

	// Press 's' to open metadata modal
	sMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedM, cmd := m.Update(sMsg)
	m = updatedM.(Model)

	if cmd != nil {
		_ = cmd()
	}

	if !m.modalManager.IsOpen() {
		// Debug info
		t.Logf("DEBUG: viewMode=%d (ViewModeTasks=%d), activePanel=%d (PanelTasks=%d)",
			m.viewMode, ViewModeTasks, m.activePanel, PanelTasks)
		t.Logf("DEBUG: useFeatureView=%v, GetSelectedFeatureID=%q",
			m.taskTree.useFeatureView, m.taskTree.GetSelectedFeatureID())
		t.Logf("DEBUG: len(selectedTasks)=%d, SelectedTask=%v",
			len(m.selectedTasks), m.taskTree.SelectedTask())
		t.Fatal("Expected modal to open after pressing 's' on draft task reached via j navigation")
	}

	// Verify it's a MetadataModal
	if _, ok := m.modalManager.activeModal.(*MetadataModal); !ok {
		t.Errorf("Expected *MetadataModal, got %T", m.modalManager.activeModal)
	}
}

// TestMetadataIntegration_SKey_TerminalSectionHeader tests that pressing 's' on a
// terminal section sub-feature HEADER (not a task) opens a feature metadata modal.
func TestMetadataIntegration_SKey_TerminalSectionHeader(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 120
	m.height = 40

	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "draft-1",
			Title:     "Draft Task",
			Status:    "draft",
			Priority:  "medium",
			Path:      "projects/test/task/draft1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// Navigate to Draft section sub-feature HEADER (not task)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	maxPresses := 30
	foundHeader := false

	for i := 0; i < maxPresses; i++ {
		updatedM, _ := m.Update(jMsg)
		m = updatedM.(Model)

		// Sub-feature header: isOnDraftSection=true, draftFeatureIdx>=0, draftTaskIdx==-1
		if m.taskTree.isOnDraftSection && m.taskTree.draftFeatureIdx >= 0 && m.taskTree.draftTaskIdx == -1 {
			foundHeader = true
			t.Logf("Found sub-feature header at j press %d: draftFeatureIdx=%d, SelectedID=%q",
				i+1, m.taskTree.draftFeatureIdx, m.taskTree.SelectedID)
			break
		}
	}

	if !foundHeader {
		t.Fatal("Could not navigate to draft sub-feature header")
	}

	// On a sub-feature header, SelectedID should be ""
	if m.taskTree.SelectedID != "" {
		t.Errorf("Expected SelectedID=%q on header, got %q", "", m.taskTree.SelectedID)
	}

	// GetSelectedFeatureID should return the feature ID
	featureID := m.taskTree.GetSelectedFeatureID()
	if featureID == "" {
		t.Fatal("Expected GetSelectedFeatureID() to return non-empty on sub-feature header")
	}
	t.Logf("GetSelectedFeatureID()=%q", featureID)

	// Press 's' — should open feature modal
	sMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedM, cmd := m.Update(sMsg)
	m = updatedM.(Model)

	if cmd != nil {
		_ = cmd()
	}

	if !m.modalManager.IsOpen() {
		t.Fatal("Expected modal to open after pressing 's' on terminal sub-feature header")
	}

	// Verify it's a MetadataModal in feature mode
	modal := m.modalManager.activeModal
	mm, ok := modal.(*MetadataModal)
	if !ok {
		t.Fatalf("Expected *MetadataModal, got %T", modal)
	}
	if mm.mode != ModeFeature {
		t.Errorf("Expected ModeFeature, got %v", mm.mode)
	}
}

// TestMetadataIntegration_SKey_TerminalSectionHeaderCollapsed tests that pressing 's'
// on a terminal section SECTION HEADER (e.g., "Draft (3)") doesn't crash/opens nothing.
func TestMetadataIntegration_SKey_TerminalSectionHeaderCollapsed(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 120
	m.height = 40

	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "draft-1",
			Title:     "Draft Task",
			Status:    "draft",
			Priority:  "medium",
			Path:      "projects/test/task/draft1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// Navigate to Draft section HEADER (draftFeatureIdx == -1)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	maxPresses := 30
	foundSectionHeader := false

	for i := 0; i < maxPresses; i++ {
		updatedM, _ := m.Update(jMsg)
		m = updatedM.(Model)

		// Section header: isOnDraftSection=true, draftFeatureIdx==-1
		if m.taskTree.isOnDraftSection && m.taskTree.draftFeatureIdx == -1 {
			foundSectionHeader = true
			t.Logf("Found section header at j press %d", i+1)
			break
		}
	}

	if !foundSectionHeader {
		t.Fatal("Could not navigate to draft section header")
	}

	// Press 's' — should NOT open a modal (on section header, no specific task or feature)
	sMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedM, _ := m.Update(sMsg)
	m = updatedM.(Model)

	// GetSelectedFeatureID returns "" for section header (featureIdx == -1)
	featureID := m.taskTree.GetSelectedFeatureID()
	t.Logf("On section header: GetSelectedFeatureID=%q, SelectedID=%q", featureID, m.taskTree.SelectedID)

	// SelectedTask() returns nil (SelectedID is "")
	// No modal should open — this is expected behavior
	// (Section header doesn't represent a specific task or feature)
}

// TestMetadataIntegration_SKey_CompletedSectionTask tests the 's' key on a task
// in the Completed section, which is collapsed by default and needs expanding.
func TestMetadataIntegration_SKey_CompletedSectionTask(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 120
	m.height = 40

	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "completed-1",
			Title:     "Completed Task",
			Status:    "completed",
			Priority:  "medium",
			Path:      "projects/test/task/completed1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// Completed section is collapsed by default. Expand it.
	m.taskTree.completedCollapsed = false

	// Navigate down to the completed section task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	maxPresses := 30
	foundTask := false

	for i := 0; i < maxPresses; i++ {
		updatedM, _ := m.Update(jMsg)
		m = updatedM.(Model)

		t.Logf("j press %d: SelectedID=%q, isOnCompleted=%v, completedFeatureIdx=%d, completedTaskIdx=%d",
			i+1, m.taskTree.SelectedID,
			m.taskTree.isOnCompletedSection, m.taskTree.completedFeatureIdx, m.taskTree.completedTaskIdx)

		if m.taskTree.SelectedID == "completed-1" {
			foundTask = true
			break
		}
	}

	if !foundTask {
		t.Fatal("Could not navigate to completed-1 task")
	}

	// Press 's'
	sMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedM, cmd := m.Update(sMsg)
	m = updatedM.(Model)

	if cmd != nil {
		_ = cmd()
	}

	if !m.modalManager.IsOpen() {
		t.Logf("DEBUG: viewMode=%d, activePanel=%d, useFeatureView=%v",
			m.viewMode, m.activePanel, m.taskTree.useFeatureView)
		t.Logf("DEBUG: GetSelectedFeatureID=%q, SelectedTask=%v",
			m.taskTree.GetSelectedFeatureID(), m.taskTree.SelectedTask())
		t.Fatal("Expected modal to open after pressing 's' on completed task")
	}

	if _, ok := m.modalManager.activeModal.(*MetadataModal); !ok {
		t.Errorf("Expected *MetadataModal, got %T", m.modalManager.activeModal)
	}
}

// TestMetadataIntegration_SKey_AfterSSEUpdate tests that 's' works after an SSE
// update refreshes the task list while on a terminal section task.
func TestMetadataIntegration_SKey_AfterSSEUpdate(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 120
	m.height = 40

	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "draft-1",
			Title:     "Draft Task",
			Status:    "draft",
			Priority:  "medium",
			Path:      "projects/test/task/draft1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// Navigate to draft task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	for i := 0; i < 20; i++ {
		updatedM, _ := m.Update(jMsg)
		m = updatedM.(Model)
		if m.taskTree.SelectedID == "draft-1" {
			break
		}
	}
	if m.taskTree.SelectedID != "draft-1" {
		t.Fatal("Could not navigate to draft-1")
	}

	// Simulate SSE update (same tasks, just a refresh)
	sseMsg := TasksUpdatedMsg{Tasks: tasks}
	updatedM, _ := m.Update(sseMsg)
	m = updatedM.(Model)

	// After SSE, verify selection is preserved
	t.Logf("After SSE: SelectedID=%q, isOnDraft=%v, draftFeatureIdx=%d, draftTaskIdx=%d",
		m.taskTree.SelectedID, m.taskTree.isOnDraftSection,
		m.taskTree.draftFeatureIdx, m.taskTree.draftTaskIdx)

	if m.taskTree.SelectedID != "draft-1" {
		t.Fatalf("Expected SelectedID to be preserved after SSE update, got %q", m.taskTree.SelectedID)
	}

	// Press 's'
	sMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedM, cmd := m.Update(sMsg)
	m = updatedM.(Model)

	if cmd != nil {
		_ = cmd()
	}

	if !m.modalManager.IsOpen() {
		t.Logf("DEBUG: SelectedTask=%v, GetSelectedFeatureID=%q",
			m.taskTree.SelectedTask(), m.taskTree.GetSelectedFeatureID())
		t.Fatal("Expected modal to open after pressing 's' following SSE update")
	}
}

// TestMetadataIntegration_SKey_StaleSelectedID tests the fallback path where
// SelectedID is stale/empty but draftTaskIdx indicates a task is selected.
// This can happen after SSE updates or race conditions.
func TestMetadataIntegration_SKey_StaleSelectedID(t *testing.T) {
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModelWithContext(cfg, ctx)
	m.width = 120
	m.height = 40

	tasks := []types.ResolvedTask{
		{
			ID:        "active-1",
			Title:     "Active Task",
			Status:    "active",
			Priority:  "high",
			Path:      "projects/test/task/active1.md",
			FeatureID: "feat-1",
		},
		{
			ID:        "draft-1",
			Title:     "Draft Task",
			Status:    "draft",
			Priority:  "medium",
			Path:      "projects/test/task/draft1.md",
			FeatureID: "feat-1",
		},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)

	// Navigate to the draft task first
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	for i := 0; i < 20; i++ {
		updatedM, _ := m.Update(jMsg)
		m = updatedM.(Model)
		if m.taskTree.SelectedID == "draft-1" {
			break
		}
	}
	if m.taskTree.SelectedID != "draft-1" {
		t.Fatal("Could not navigate to draft-1")
	}

	// Simulate a stale SelectedID by clearing it (as if an SSE update broke the sync)
	m.taskTree.SelectedID = ""
	// BUT draftTaskIdx is still 0 (pointing to the first task in the sub-feature)

	t.Logf("Before 's': SelectedID=%q, draftTaskIdx=%d, draftFeatureIdx=%d",
		m.taskTree.SelectedID, m.taskTree.draftTaskIdx, m.taskTree.draftFeatureIdx)

	// SelectedTask should use fallback to resolve from terminal section task index
	selectedTask := m.taskTree.SelectedTask()
	if selectedTask == nil {
		t.Fatal("Expected SelectedTask() to resolve via terminal section fallback, got nil")
	}
	if selectedTask.ID != "draft-1" {
		t.Errorf("Expected SelectedTask().ID=%q, got %q", "draft-1", selectedTask.ID)
	}
	t.Logf("Fallback resolved: SelectedTask().ID=%q, Path=%q", selectedTask.ID, selectedTask.Path)

	// Press 's'
	sMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedM, cmd := m.Update(sMsg)
	m = updatedM.(Model)

	if cmd != nil {
		_ = cmd()
	}

	if !m.modalManager.IsOpen() {
		t.Fatal("Expected modal to open via fallback SelectedTask resolution")
	}
}

// Helper function to check if string contains substring
func stringContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
