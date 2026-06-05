package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

var errTest = errors.New("test error")

// testModelForGoalRows builds a minimal Model wired for goal row action tests.
func testModelForGoalRows() Model {
	m := NewModel(Config{APIURL: "http://localhost:9999", Project: "brain-api"})
	m.width = 100
	m.height = 30
	m.activeContentTab = ContentTabAutomation
	m.activeAutomationSubTab = AutomationSubTabAutomations
	return m
}

func goalRow() AutomationListRow {
	return AutomationListRow{
		ID:      "goal-entry",
		Path:    "projects/brain-api/automation/goal-entry.md",
		Title:   "My Goal",
		Source:  "automation",
		Status:  "active",
		Enabled: true,
		IsGoal:  true,
	}
}

func nonGoalRow() AutomationListRow {
	return AutomationListRow{
		ID:      "plain-auto",
		Path:    "projects/brain-api/automation/plain-auto.md",
		Title:   "Plain Automation",
		Source:  "automation",
		Status:  "active",
		Enabled: true,
		IsGoal:  false,
	}
}

func validGoalSummary() types.GoalSummary {
	return types.GoalSummary{
		EntryID: "goal-entry",
		GoalID:  "g-123",
		Title:   "My Goal",
		Project: "brain-api",
		Status:  "active",
		Config:  &types.GoalConfig{Criteria: "all tasks done"},
		Action:  &types.AutomationAction{Type: "prompt", Agent: "tdd-dev"},
	}
}

// --- editSelectedGoalRow / runSelectedGoalRow routing ---

func TestEditSelectedGoalRow_ReturnsCmdForGoalRow(t *testing.T) {
	m := testModelForGoalRows()
	m.automationList.SetRows([]AutomationListRow{goalRow()})

	if cmd := m.editSelectedGoalRow(); cmd == nil {
		t.Fatal("expected non-nil cmd when a goal row is selected")
	}
}

func TestEditSelectedGoalRow_NilForNonGoalRow(t *testing.T) {
	m := testModelForGoalRows()
	m.automationList.SetRows([]AutomationListRow{nonGoalRow()})

	if cmd := m.editSelectedGoalRow(); cmd != nil {
		t.Fatal("expected nil cmd when a non-goal row is selected")
	}
}

func TestEditSelectedGoalRow_NilWhenNoRow(t *testing.T) {
	m := testModelForGoalRows()
	m.automationList.SetRows(nil)

	if cmd := m.editSelectedGoalRow(); cmd != nil {
		t.Fatal("expected nil cmd when no row is selected")
	}
}

func TestRunSelectedGoalRow_ReturnsCmdForGoalRow(t *testing.T) {
	m := testModelForGoalRows()
	m.automationList.SetRows([]AutomationListRow{goalRow()})

	if cmd := m.runSelectedGoalRow(); cmd == nil {
		t.Fatal("expected non-nil cmd when a goal row is selected")
	}
}

func TestRunSelectedGoalRow_NilForNonGoalRow(t *testing.T) {
	m := testModelForGoalRows()
	m.automationList.SetRows([]AutomationListRow{nonGoalRow()})

	if cmd := m.runSelectedGoalRow(); cmd != nil {
		t.Fatal("expected nil cmd when a non-goal row is selected")
	}
}

// --- goalConfigOpenMsg opens the modal ---

func TestGoalConfigOpenMsg_OpensModal(t *testing.T) {
	m := testModelForGoalRows()
	summary := validGoalSummary()

	updated, _ := m.Update(goalConfigOpenMsg{summary: &summary})
	model := updated.(Model)

	if !model.modalManager.IsOpen() {
		t.Fatal("expected modal to be open after goalConfigOpenMsg")
	}
	if _, ok := model.modalManager.ActiveModal().(*GoalConfigModal); !ok {
		t.Fatalf("expected active modal to be *GoalConfigModal, got %T", model.modalManager.ActiveModal())
	}
}

func TestGoalConfigOpenMsg_ErrorSetsStatusNoModal(t *testing.T) {
	m := testModelForGoalRows()

	updated, _ := m.Update(goalConfigOpenMsg{err: errTest})
	model := updated.(Model)

	if model.modalManager.IsOpen() {
		t.Fatal("expected no modal to open on goalConfigOpenMsg error")
	}
	if model.statusMessageType != "error" {
		t.Fatalf("expected error status, got %q", model.statusMessageType)
	}
}

// --- goalConfigSavedMsg ---

func TestGoalConfigSavedMsg_SuccessClosesModal(t *testing.T) {
	m := testModelForGoalRows()
	summary := validGoalSummary()
	// Open the modal first.
	openMsg := goalConfigOpenMsg{summary: &summary}
	updated, _ := m.Update(openMsg)
	model := updated.(Model)
	if !model.modalManager.IsOpen() {
		t.Fatal("setup: modal should be open")
	}

	updated2, _ := model.Update(goalConfigSavedMsg{goalID: "g-123", summary: &summary})
	model2 := updated2.(Model)

	if model2.modalManager.IsOpen() {
		t.Fatal("expected modal closed after successful save")
	}
	if model2.statusMessageType != "success" {
		t.Fatalf("expected success status, got %q", model2.statusMessageType)
	}
}

func TestGoalConfigSavedMsg_ErrorSetsErrorStatus(t *testing.T) {
	m := testModelForGoalRows()

	updated, _ := m.Update(goalConfigSavedMsg{goalID: "g-123", err: errTest})
	model := updated.(Model)

	if model.statusMessageType != "error" {
		t.Fatalf("expected error status, got %q", model.statusMessageType)
	}
}

// --- goalReconcileResultMsg ---

func TestGoalReconcileResultMsg_SuccessSetsDecisionStatus(t *testing.T) {
	m := testModelForGoalRows()
	audit := &types.GoalReconcileAudit{
		GoalID:   "g-123",
		Decision: types.ReconcileDecision("no_action"),
		Reason:   "all tasks complete",
	}

	updated, _ := m.Update(goalReconcileResultMsg{goalID: "g-123", audit: audit})
	model := updated.(Model)

	if model.statusMessageType != "success" {
		t.Fatalf("expected success status, got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "no_action") {
		t.Fatalf("expected status to contain decision, got %q", model.statusMessage)
	}
}

func TestGoalReconcileResultMsg_ErrorSetsErrorStatus(t *testing.T) {
	m := testModelForGoalRows()

	updated, _ := m.Update(goalReconcileResultMsg{goalID: "g-123", err: errTest})
	model := updated.(Model)

	if model.statusMessageType != "error" {
		t.Fatalf("expected error status, got %q", model.statusMessageType)
	}
}

// --- goal detail audit rendering ---

func TestAutomationDetailContent_GoalWithCachedAudit(t *testing.T) {
	m := testModelForGoalRows()
	row := goalRow()
	m.automationList.SetRows([]AutomationListRow{row})
	m.goalAuditByEntry = map[string][]types.GoalReconcileAudit{
		row.ID: {
			{
				Timestamp: "2026-01-01T00:00:00Z",
				GoalID:    "g-123",
				Decision:  types.ReconcileDecision("create_task"),
				Reason:    "found incomplete work",
			},
		},
	}

	out := m.automationDetailContent(row.Path, "trigger: event")
	if !strings.Contains(out, "create_task") {
		t.Fatalf("expected detail to contain decision, got:\n%s", out)
	}
	if !strings.Contains(out, "found incomplete work") {
		t.Fatalf("expected detail to contain reason, got:\n%s", out)
	}
}

func TestAutomationDetailContent_GoalWithNoCacheShowsPlaceholder(t *testing.T) {
	m := testModelForGoalRows()
	row := goalRow()
	m.automationList.SetRows([]AutomationListRow{row})

	out := m.automationDetailContent(row.Path, "trigger: event")
	if !strings.Contains(out, "reconcile") && !strings.Contains(out, "Reconcile") {
		t.Fatalf("expected reconcile placeholder section, got:\n%s", out)
	}
}

func TestAutomationDetailContent_NonGoalUnaffected(t *testing.T) {
	m := testModelForGoalRows()
	row := nonGoalRow()
	m.automationList.SetRows([]AutomationListRow{row})

	out := m.automationDetailContent(row.Path, "trigger: cron")
	if strings.Contains(out, "Reconcile") {
		t.Fatalf("non-goal row should not render reconcile section, got:\n%s", out)
	}
}
