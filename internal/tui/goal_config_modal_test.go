package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// throwawayClient returns an APIClient that does not make network calls during
// construction. Tests must not invoke command closures that hit the network.
func throwawayClient() *runner.APIClient {
	return runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
}

// sampleGoal builds a GoalSummary with Config + Action populated for seeding tests.
func sampleGoal() types.GoalSummary {
	return types.GoalSummary{
		EntryID:   "entry-123",
		GoalID:    "goal-abc",
		Title:     "Ship the thing",
		Project:   "brain-api",
		FeatureID: "feat-x",
		Status:    "active",
		Config: &types.GoalConfig{
			ID:               "goal-abc",
			Criteria:         "all tests pass",
			Validation:       "go test ./...",
			Workdir:          "/tmp/work",
			TriggerSource:    "task",
			CompleteStatuses: []string{"completed", "validated"},
			BlockedStatuses:  []string{"blocked"},
		},
		Action: &types.AutomationAction{
			Type:        "prompt",
			Agent:       "tdd-dev",
			Model:       "anthropic/claude",
			SessionMode: "fresh",
		},
	}
}

// 1. Interface conformance.
var _ Modal = (*GoalConfigModal)(nil)

// 2. Constructor seeding.
func TestGoalConfigModal_ConstructorSeeding(t *testing.T) {
	goal := sampleGoal()
	m := NewGoalConfigModal(goal, throwawayClient())

	checks := map[MetadataField]string{
		FieldGoalObjective:     "Ship the thing",
		FieldGoalCriteria:      "all tests pass",
		FieldGoalValidation:    "go test ./...",
		FieldGoalWorkdir:       "/tmp/work",
		FieldGoalTriggerSource: "task",
		FieldGoalSessionMode:   "fresh",
		FieldAgent:             "tdd-dev",
		FieldModel:             "anthropic/claude",
		FieldGoalExecutor:      "opencode", // default; not on Action
	}
	for field, want := range checks {
		if got := m.values[field]; got != want {
			t.Errorf("field %s: expected %q, got %q", field, want, got)
		}
	}

	if got := m.CompleteStatuses(); !equalStringSets(got, []string{"completed", "validated"}) {
		t.Errorf("complete statuses: expected [completed validated], got %v", got)
	}
	if got := m.BlockedStatuses(); !equalStringSets(got, []string{"blocked"}) {
		t.Errorf("blocked statuses: expected [blocked], got %v", got)
	}
}

// 2b. TriggerSource normalization (empty -> both).
func TestGoalConfigModal_TriggerSourceNormalization(t *testing.T) {
	goal := sampleGoal()
	goal.Config.TriggerSource = ""
	m := NewGoalConfigModal(goal, throwawayClient())
	if got := m.values[FieldGoalTriggerSource]; got != "both" {
		t.Errorf("expected trigger source normalized to 'both', got %q", got)
	}
}

// 2c. SessionMode default (empty -> continue).
func TestGoalConfigModal_SessionModeDefault(t *testing.T) {
	goal := sampleGoal()
	goal.Action.SessionMode = ""
	m := NewGoalConfigModal(goal, throwawayClient())
	if got := m.values[FieldGoalSessionMode]; got != "continue" {
		t.Errorf("expected session mode default 'continue', got %q", got)
	}
}

// 2d. Nil Config / Action should not panic and use defaults.
func TestGoalConfigModal_NilConfigAction(t *testing.T) {
	goal := types.GoalSummary{Title: "x", Config: nil, Action: nil}
	m := NewGoalConfigModal(goal, throwawayClient())
	if m.values[FieldGoalObjective] != "x" {
		t.Errorf("expected objective 'x', got %q", m.values[FieldGoalObjective])
	}
	if m.values[FieldGoalTriggerSource] != "both" {
		t.Errorf("expected trigger source default 'both', got %q", m.values[FieldGoalTriggerSource])
	}
	if m.values[FieldGoalSessionMode] != "continue" {
		t.Errorf("expected session mode default 'continue', got %q", m.values[FieldGoalSessionMode])
	}
}

// 3. Workdir default to cwd when empty.
func TestGoalConfigModal_WorkdirDefaultsToCwd(t *testing.T) {
	goal := sampleGoal()
	goal.Config.Workdir = ""
	m := NewGoalConfigModal(goal, throwawayClient())

	cwd, _ := os.Getwd()
	got := m.values[FieldGoalWorkdir]
	if got == "" {
		t.Fatal("expected workdir to default to cwd, got empty")
	}
	if got != cwd {
		t.Errorf("expected workdir %q (cwd), got %q", cwd, got)
	}
}

// 4. Dropdown cycling for trigger source: task -> feature -> both -> task.
func TestGoalConfigModal_DropdownCycleTriggerSource(t *testing.T) {
	goal := sampleGoal()
	goal.Config.TriggerSource = "task"
	m := NewGoalConfigModal(goal, throwawayClient())

	focusField(t, m, FieldGoalTriggerSource)

	seq := []string{"feature", "both", "task"}
	for i, want := range seq {
		m.HandleKey("l")
		if got := m.values[FieldGoalTriggerSource]; got != want {
			t.Errorf("cycle step %d: expected %q, got %q", i, want, got)
		}
	}
}

// 4b. Dropdown cycle session mode continue -> fresh -> continue.
func TestGoalConfigModal_DropdownCycleSessionMode(t *testing.T) {
	goal := sampleGoal()
	goal.Action.SessionMode = "continue"
	m := NewGoalConfigModal(goal, throwawayClient())

	focusField(t, m, FieldGoalSessionMode)
	m.HandleKey("l")
	if got := m.values[FieldGoalSessionMode]; got != "fresh" {
		t.Errorf("expected fresh, got %q", got)
	}
	m.HandleKey("l")
	if got := m.values[FieldGoalSessionMode]; got != "continue" {
		t.Errorf("expected continue (wrap), got %q", got)
	}
}

// 4c. Dropdown prev cycle with 'h'.
func TestGoalConfigModal_DropdownPrevTriggerSource(t *testing.T) {
	goal := sampleGoal()
	goal.Config.TriggerSource = "task"
	m := NewGoalConfigModal(goal, throwawayClient())

	focusField(t, m, FieldGoalTriggerSource)
	m.HandleKey("h") // task -> both (wrap backward)
	if got := m.values[FieldGoalTriggerSource]; got != "both" {
		t.Errorf("expected both after prev-wrap, got %q", got)
	}
}

// 5. Text edit: focus objective, enter to edit, type, commit.
func TestGoalConfigModal_TextEdit(t *testing.T) {
	goal := sampleGoal()
	goal.Title = ""
	m := NewGoalConfigModal(goal, throwawayClient())

	focusField(t, m, FieldGoalObjective)

	// enter edit mode
	handled, _ := m.HandleKey("enter")
	if !handled {
		t.Fatal("expected enter to be handled (enter edit mode)")
	}
	if !m.editing {
		t.Fatal("expected editing mode active after enter")
	}

	for _, r := range "abc" {
		m.HandleKey(string(r))
	}
	// backspace removes 'c'
	m.HandleKey("backspace")

	// commit
	m.HandleKey("enter")
	if m.editing {
		t.Error("expected editing mode to exit after commit")
	}
	if got := m.values[FieldGoalObjective]; got != "ab" {
		t.Errorf("expected objective 'ab', got %q", got)
	}
}

// 6. Multi-select toggle for complete statuses.
func TestGoalConfigModal_MultiSelectToggle(t *testing.T) {
	goal := sampleGoal()
	goal.Config.CompleteStatuses = nil // start empty
	m := NewGoalConfigModal(goal, throwawayClient())

	focusField(t, m, FieldGoalCompleteStatuses)

	// Sub-cursor starts at index 0 -> first status in EntryStatuses ("draft")
	first := types.EntryStatuses[0]
	m.HandleKey("space")
	if !contains(m.CompleteStatuses(), first) {
		t.Errorf("expected %q to be selected after toggle, got %v", first, m.CompleteStatuses())
	}
	// toggle again removes it
	m.HandleKey("space")
	if contains(m.CompleteStatuses(), first) {
		t.Errorf("expected %q removed after second toggle, got %v", first, m.CompleteStatuses())
	}

	// move sub-cursor right and toggle the second status
	m.HandleKey("l")
	second := types.EntryStatuses[1]
	m.HandleKey("enter")
	if !contains(m.CompleteStatuses(), second) {
		t.Errorf("expected %q selected via enter, got %v", second, m.CompleteStatuses())
	}
}

// 6b. Blocked statuses multi-select toggle works independently.
func TestGoalConfigModal_BlockedMultiSelectToggle(t *testing.T) {
	goal := sampleGoal()
	goal.Config.BlockedStatuses = nil
	m := NewGoalConfigModal(goal, throwawayClient())

	focusField(t, m, FieldGoalBlockedStatuses)
	first := types.EntryStatuses[0]
	m.HandleKey("space")
	if !contains(m.BlockedStatuses(), first) {
		t.Errorf("expected %q selected in blocked, got %v", first, m.BlockedStatuses())
	}
	// complete statuses untouched
	if contains(m.CompleteStatuses(), first) {
		t.Errorf("blocked toggle leaked into complete statuses")
	}
}

// 7. Navigation wrap.
func TestGoalConfigModal_NavigationWrap(t *testing.T) {
	m := NewGoalConfigModal(sampleGoal(), throwawayClient())

	n := len(m.fields)
	if n == 0 {
		t.Fatal("expected non-empty fields")
	}

	// move to last field
	for i := 0; i < n-1; i++ {
		m.HandleKey("j")
	}
	if m.focusIndex != n-1 {
		t.Fatalf("expected focus at last field %d, got %d", n-1, m.focusIndex)
	}
	// j past last wraps to 0
	m.HandleKey("j")
	if m.focusIndex != 0 {
		t.Errorf("expected wrap to 0, got %d", m.focusIndex)
	}
	// k past first wraps to last
	m.HandleKey("k")
	if m.focusIndex != n-1 {
		t.Errorf("expected wrap to last %d, got %d", n-1, m.focusIndex)
	}
}

// 8. buildUpdateRequest payload.
func TestGoalConfigModal_BuildUpdateRequest(t *testing.T) {
	goal := sampleGoal()
	m := NewGoalConfigModal(goal, throwawayClient())

	// Edit objective text
	focusField(t, m, FieldGoalObjective)
	m.HandleKey("enter")
	// clear existing then type new
	for range "Ship the thing" {
		m.HandleKey("backspace")
	}
	for _, r := range "New objective" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")

	req := m.buildUpdateRequest()

	if req.Title == nil || *req.Title != "New objective" {
		t.Errorf("expected Title 'New objective', got %v", req.Title)
	}
	if req.Criteria == nil || *req.Criteria != "all tests pass" {
		t.Errorf("expected Criteria 'all tests pass', got %v", req.Criteria)
	}
	if req.TriggerSource == nil || *req.TriggerSource != "task" {
		t.Errorf("expected TriggerSource 'task', got %v", req.TriggerSource)
	}
	if req.CompleteStatuses == nil || !equalStringSets(*req.CompleteStatuses, []string{"completed", "validated"}) {
		t.Errorf("expected CompleteStatuses [completed validated], got %v", req.CompleteStatuses)
	}
	if req.BlockedStatuses == nil || !equalStringSets(*req.BlockedStatuses, []string{"blocked"}) {
		t.Errorf("expected BlockedStatuses [blocked], got %v", req.BlockedStatuses)
	}
	if req.Action == nil {
		t.Fatal("expected Action to be set")
	}
	if req.Action.Agent != "tdd-dev" {
		t.Errorf("expected Action.Agent 'tdd-dev', got %q", req.Action.Agent)
	}
	if req.Action.SessionMode != "fresh" {
		t.Errorf("expected Action.SessionMode 'fresh', got %q", req.Action.SessionMode)
	}
	if req.Action.Type != "prompt" {
		t.Errorf("expected Action.Type 'prompt', got %q", req.Action.Type)
	}
}

// 8b. buildUpdateRequest preserves a non-empty existing action type.
func TestGoalConfigModal_BuildUpdateRequest_PreservesActionType(t *testing.T) {
	goal := sampleGoal()
	goal.Action.Type = "script"
	m := NewGoalConfigModal(goal, throwawayClient())

	req := m.buildUpdateRequest()
	if req.Action == nil || req.Action.Type != "script" {
		t.Errorf("expected preserved action type 'script', got %v", req.Action)
	}
}

// 8c. buildUpdateRequest defaults action type to prompt when goal.Action nil.
func TestGoalConfigModal_BuildUpdateRequest_DefaultActionType(t *testing.T) {
	goal := types.GoalSummary{Title: "t", Config: nil, Action: nil}
	m := NewGoalConfigModal(goal, throwawayClient())
	req := m.buildUpdateRequest()
	if req.Action == nil || req.Action.Type != "prompt" {
		t.Errorf("expected default action type 'prompt', got %v", req.Action)
	}
}

// 9. esc behavior.
func TestGoalConfigModal_EscBehavior(t *testing.T) {
	m := NewGoalConfigModal(sampleGoal(), throwawayClient())

	// Normal mode: esc returns (false, nil) so ModalManager closes
	handled, cmd := m.HandleKey("esc")
	if handled {
		t.Error("expected esc not handled in normal mode (false)")
	}
	if cmd != nil {
		t.Error("expected nil cmd on esc")
	}

	// Enter text edit, then esc exits edit mode and returns (true, nil)
	focusField(t, m, FieldGoalObjective)
	m.HandleKey("enter")
	if !m.editing {
		t.Fatal("expected editing mode active")
	}
	handled, _ = m.HandleKey("esc")
	if !handled {
		t.Error("expected esc handled (true) while editing")
	}
	if m.editing {
		t.Error("expected editing mode exited after esc")
	}
}

// 10. Audit msg rendering.
func TestGoalConfigModal_AuditLoaded(t *testing.T) {
	m := NewGoalConfigModal(sampleGoal(), throwawayClient())

	audit := []types.GoalReconcileAudit{
		{
			Timestamp: "2026-06-05T10:00:00Z",
			GoalID:    "goal-abc",
			Decision:  types.ReconcileNeedWork,
			Reason:    "more work required",
		},
	}
	updated, _ := m.Update(goalAuditLoadedMsg{goalID: "goal-abc", audit: audit, err: nil})
	gm := updated.(*GoalConfigModal)

	if gm.loadingAudit {
		t.Error("expected loadingAudit cleared after msg")
	}
	view := gm.View()
	if !strings.Contains(view, "need_work") {
		t.Errorf("expected view to contain decision 'need_work', got:\n%s", view)
	}
	if !strings.Contains(view, "more work required") {
		t.Errorf("expected view to contain reason, got:\n%s", view)
	}
}

// 10b. Audit msg with mismatched goalID is ignored.
func TestGoalConfigModal_AuditLoaded_Mismatch(t *testing.T) {
	m := NewGoalConfigModal(sampleGoal(), throwawayClient())
	m.loadingAudit = true
	updated, _ := m.Update(goalAuditLoadedMsg{goalID: "other-goal", audit: []types.GoalReconcileAudit{{Decision: types.ReconcileComplete}}, err: nil})
	gm := updated.(*GoalConfigModal)
	if len(gm.audit) != 0 {
		t.Errorf("expected audit ignored for mismatched goalID, got %d entries", len(gm.audit))
	}
}

// 11. View dimensions and content.
func TestGoalConfigModal_ViewDimensions(t *testing.T) {
	m := NewGoalConfigModal(sampleGoal(), throwawayClient())

	if m.Title() != "Configure Goal" {
		t.Errorf("expected title 'Configure Goal', got %q", m.Title())
	}
	if m.Width() <= 0 {
		t.Errorf("expected positive width, got %d", m.Width())
	}
	if m.Height() <= 0 {
		t.Errorf("expected positive height, got %d", m.Height())
	}
	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
	for _, label := range []string{"Objective", "Criteria", "Trigger Source", "Session Mode"} {
		if !strings.Contains(view, label) {
			t.Errorf("expected view to contain label %q", label)
		}
	}
	if !strings.Contains(view, "ctrl+s") {
		t.Error("expected footer hint with ctrl+s")
	}
}

// 11b. Empty audit shows "No reconcile history".
func TestGoalConfigModal_ViewEmptyAudit(t *testing.T) {
	m := NewGoalConfigModal(sampleGoal(), throwawayClient())
	m.loadingAudit = false
	view := m.View()
	if !strings.Contains(view, "No reconcile history") {
		t.Errorf("expected 'No reconcile history' in view, got:\n%s", view)
	}
}

// ---- test helpers ----

func focusField(t *testing.T, m *GoalConfigModal, field MetadataField) {
	t.Helper()
	for i, f := range m.fields {
		if f == field {
			m.focusIndex = i
			return
		}
	}
	t.Fatalf("field %s not found in modal fields", field)
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]bool{}
	for _, x := range a {
		am[x] = true
	}
	for _, x := range b {
		if !am[x] {
			return false
		}
	}
	return true
}

// --- Create-mode (New Goal) ---

func TestGoalCreateModal_Seeding(t *testing.T) {
	m := NewGoalCreateModal("demo", throwawayClient())
	if !m.createMode {
		t.Fatal("expected createMode=true")
	}
	if m.Title() != "New Goal" {
		t.Errorf("title = %q, want New Goal", m.Title())
	}
	if got := m.values[FieldGoalProject]; got != "demo" {
		t.Errorf("project seed = %q, want demo", got)
	}
	if m.Init() != nil {
		t.Error("Init should be nil in create mode (no audit load)")
	}
	// Project + Feature fields present in the field list.
	var hasProject, hasFeature bool
	for _, f := range m.fields {
		if f == FieldGoalProject {
			hasProject = true
		}
		if f == FieldGoalFeature {
			hasFeature = true
		}
	}
	if !hasProject || !hasFeature {
		t.Errorf("expected Project and Feature fields; project=%v feature=%v", hasProject, hasFeature)
	}
}

func TestGoalCreateModal_BuildCreateRequest(t *testing.T) {
	m := NewGoalCreateModal("demo", throwawayClient())
	m.values[FieldGoalObjective] = "Reach 90% Coverage!"
	m.values[FieldGoalCriteria] = "all packages > 90%"
	m.values[FieldGoalFeature] = "tests"
	m.values[FieldAgent] = "tdd-dev"

	req := m.buildCreateRequest()
	if req.Project != "demo" {
		t.Errorf("project = %q", req.Project)
	}
	if req.Title != "Reach 90% Coverage!" {
		t.Errorf("title = %q", req.Title)
	}
	if req.FeatureID != "tests" {
		t.Errorf("feature = %q", req.FeatureID)
	}
	if req.Action.Type != "prompt" {
		t.Errorf("action type = %q, want prompt", req.Action.Type)
	}
	if req.Action.Agent != "tdd-dev" {
		t.Errorf("agent = %q", req.Action.Agent)
	}
	if req.Config.ID == "" {
		t.Error("config.id must be generated (non-empty)")
	}
	if !strings.HasPrefix(req.Config.ID, "reach-90-coverage") {
		t.Errorf("config.id = %q, want slug prefix reach-90-coverage", req.Config.ID)
	}
	if req.Config.Criteria != "all packages > 90%" {
		t.Errorf("criteria = %q", req.Config.Criteria)
	}
	// Content falls back to criteria.
	if req.Content != "all packages > 90%" {
		t.Errorf("content = %q, want criteria fallback", req.Content)
	}
}

func TestGenerateGoalID(t *testing.T) {
	id := generateGoalID("Ship the PWA!!!")
	if !strings.HasPrefix(id, "ship-the-pwa-") {
		t.Errorf("id = %q, want ship-the-pwa- prefix", id)
	}
	if generateGoalID("") == "" {
		t.Error("empty objective should still yield a non-empty id")
	}
	if strings.HasPrefix(generateGoalID(""), "-") {
		t.Error("id should not start with a dash")
	}
}
