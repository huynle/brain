package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// GET /automations/{id}/effective reads the same resolution the scheduler
// uses. These tests pin what one project would run under, and whether the
// scheduler fires for it, across parent, binding, and broken-binding states.

// effSaveGlobalCron saves an active global cron automation whose project
// filter is filter, and returns its ID.
func effSaveGlobalCron(t *testing.T, brain *BrainServiceImpl, filter string) string {
	t.Helper()
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Nightly review",
		Content: "global cron automation",
		Status:  "active",
		Global:  serviceBoolPtr(true),
		Trigger: &types.TriggerConfig{
			Type:     types.TriggerTypeCron,
			Schedule: "0 3 * * *",
			Filter:   map[string]string{"project": filter},
		},
		Action: &types.AutomationAction{
			Type:         types.AutomationActionPrompt,
			Agent:        "tdd-dev",
			DirectPrompt: "Review {{.Project}}",
		},
	})
	if err != nil {
		t.Fatalf("save global cron: %v", err)
	}
	return resp.ID
}

// effSaveBinding saves a binding of parent for project with the given status,
// trigger and action, and returns its ID.
func effSaveBinding(t *testing.T, brain *BrainServiceImpl, parent, project, status string, trigger *types.TriggerConfig, action *types.AutomationAction) string {
	t.Helper()
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Binding for " + project,
		Status:  status,
		Project: project,
		Extends: parent,
		Trigger: trigger,
		Action:  action,
	})
	if err != nil {
		t.Fatalf("save binding for %s: %v", project, err)
	}
	return resp.ID
}

// effWithProjects returns a service whose project lister knows the projects.
func effWithProjects(brain *BrainServiceImpl, projects ...string) *AutomationService {
	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: projects})
	return svc
}

func TestEffective_GlobalWithoutBindingInheritsEveryField(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	svc := effWithProjects(brain, "p1", "p2")

	got, err := svc.EffectiveAutomation(context.Background(), parent, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation: %v", err)
	}
	if got.ID != parent || got.Project != "p1" {
		t.Fatalf("id=%q project=%q, want %q and p1", got.ID, got.Project, parent)
	}
	if got.Automation == nil || got.Automation.Trigger == nil || got.Automation.Trigger.Schedule != "0 3 * * *" {
		t.Fatalf("effective trigger = %#v, want the parent schedule", got.Automation)
	}
	if got.Automation.Action == nil || got.Automation.Action.Agent != "tdd-dev" {
		t.Fatalf("effective action = %#v, want the parent agent", got.Automation.Action)
	}
	if got.BindingID != "" || got.BindingStatus != "" {
		t.Fatalf("binding = %q/%q, want none", got.BindingID, got.BindingStatus)
	}
	if !got.Targeted || got.Broken {
		t.Fatalf("targeted=%v broken=%v, want targeted and not broken", got.Targeted, got.Broken)
	}
	for _, field := range []string{"trigger.schedule", "action.agent", "action.model", "starts_at", "max_runs"} {
		if state := got.Fields[field]; state != types.EffectiveFieldInherited {
			t.Errorf("field %s = %q, want inherited", field, state)
		}
	}
}

func TestEffective_OverridingBindingReplacesItsFields(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	binding := effSaveBinding(t, brain, parent, "p1", "active",
		&types.TriggerConfig{Schedule: "0 9 * * *"},
		&types.AutomationAction{Agent: "explore"})
	svc := effWithProjects(brain, "p1", "p2")

	got, err := svc.EffectiveAutomation(context.Background(), parent, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation: %v", err)
	}
	if got.Automation == nil || got.Automation.Trigger.Schedule != "0 9 * * *" {
		t.Fatalf("effective schedule = %#v, want the binding's 09:00", got.Automation)
	}
	if got.Automation.Action.Agent != "explore" {
		t.Fatalf("effective agent = %q, want the binding's explore", got.Automation.Action.Agent)
	}
	if got.BindingID != binding || got.BindingStatus != "active" {
		t.Fatalf("binding = %q/%q, want %q/active", got.BindingID, got.BindingStatus, binding)
	}
	if !got.Targeted || got.Broken {
		t.Fatalf("targeted=%v broken=%v, want an active binding targeted", got.Targeted, got.Broken)
	}
	if state := got.Fields["trigger.schedule"]; state != types.EffectiveFieldOverridden {
		t.Errorf("trigger.schedule = %q, want overridden", state)
	}
	if state := got.Fields["action.agent"]; state != types.EffectiveFieldOverridden {
		t.Errorf("action.agent = %q, want overridden", state)
	}
	if state := got.Fields["action.executor"]; state != types.EffectiveFieldInherited {
		t.Errorf("action.executor = %q, want inherited (the binding does not set it)", state)
	}
}

func TestEffective_OptOutBindingIsNotTargeted(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	binding := effSaveBinding(t, brain, parent, "p1", "inactive", nil, nil)
	svc := effWithProjects(brain, "p1", "p2")

	optOut, err := svc.EffectiveAutomation(context.Background(), parent, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation(p1): %v", err)
	}
	if optOut.Targeted {
		t.Fatal("an opted-out project is still reported as targeted")
	}
	if optOut.BindingID != binding || optOut.BindingStatus != "inactive" {
		t.Fatalf("binding = %q/%q, want the inactive opt-out %q", optOut.BindingID, optOut.BindingStatus, binding)
	}
	if optOut.Broken {
		t.Fatal("a plain opt-out is not broken")
	}

	other, err := svc.EffectiveAutomation(context.Background(), parent, "p2")
	if err != nil {
		t.Fatalf("EffectiveAutomation(p2): %v", err)
	}
	if !other.Targeted || other.BindingID != "" {
		t.Fatalf("p2 targeted=%v binding=%q, want targeted with no binding", other.Targeted, other.BindingID)
	}
}

func TestEffective_InactiveParentIsNotTargeted(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Paused parent",
		Status:  "inactive",
		Global:  serviceBoolPtr(true),
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}},
		Action:  &types.AutomationAction{Type: types.AutomationActionPrompt, DirectPrompt: "x"},
	})
	if err != nil {
		t.Fatalf("save inactive parent: %v", err)
	}
	svc := effWithProjects(brain, "p1")

	got, err := svc.EffectiveAutomation(context.Background(), resp.ID, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation: %v", err)
	}
	if got.Targeted {
		t.Fatal("an inactive parent is reported as targeted; the scheduler never lists it")
	}
}

func TestEffective_ProjectOwnedAutomationRequiresItsOwner(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Owned by p1",
		Status:  "active",
		Project: "p1",
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: "0 6 * * *"},
		Action:  &types.AutomationAction{Type: types.AutomationActionPrompt, DirectPrompt: "x"},
	})
	if err != nil {
		t.Fatalf("save project-owned: %v", err)
	}
	svc := effWithProjects(brain, "p1", "p2")

	got, err := svc.EffectiveAutomation(context.Background(), resp.ID, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation(owner): %v", err)
	}
	if !got.Targeted || got.Broken || got.BindingID != "" {
		t.Fatalf("owner view targeted=%v broken=%v binding=%q", got.Targeted, got.Broken, got.BindingID)
	}
	if len(got.Fields) != 0 {
		t.Fatalf("project-owned fields = %v, want none (there is no parent to inherit from)", got.Fields)
	}

	_, err = svc.EffectiveAutomation(context.Background(), resp.ID, "p2")
	if !errors.Is(err, api.ErrInvalidInput) {
		t.Fatalf("project-owned read by another project: err = %v, want ErrInvalidInput", err)
	}
}

func TestEffective_DuplicateBindingIsFlaggedBroken(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	first := effSaveBinding(t, brain, parent, "p1", "active", nil, nil)
	second := effSaveBinding(t, brain, parent, "p2", "active", nil, nil)
	// Save refuses a second binding for one project, so the duplicate is made
	// the way a racing pair of saves would leave it: move one into p1.
	if _, err := brain.Move(context.Background(), second, "p1"); err != nil {
		t.Fatalf("Move second binding into p1: %v", err)
	}
	svc := effWithProjects(brain, "p1", "p2")

	got, err := svc.EffectiveAutomation(context.Background(), parent, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation: %v", err)
	}
	if !got.Broken || got.BrokenReason != types.EffectiveBrokenDuplicateBinding {
		t.Fatalf("broken=%v reason=%q, want duplicate_binding", got.Broken, got.BrokenReason)
	}
	if got.BindingID != first && got.BindingID != second {
		t.Fatalf("binding = %q, want one of the two bindings", got.BindingID)
	}
	if got.Automation == nil {
		t.Fatal("a duplicate still has a governing binding, so Automation must be set")
	}
}

func TestEffective_UnknownIDIsNotFound(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	svc := effWithProjects(brain, "p1")

	_, err := svc.EffectiveAutomation(context.Background(), "does-not-exist", "p1")
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestEffective_NonAutomationIsNotFound(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	note, err := brain.Save(context.Background(), types.CreateEntryRequest{Type: "note", Title: "Not an automation", Content: "x", Project: "p1"})
	if err != nil {
		t.Fatalf("save note: %v", err)
	}
	svc := effWithProjects(brain, "p1")

	_, err = svc.EffectiveAutomation(context.Background(), note.ID, "p1")
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a note", err)
	}
}

func TestEffective_MissingOrInvalidProjectIsInvalidInput(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	svc := effWithProjects(brain, "p1")

	for _, project := range []string{"", "..", "a/b"} {
		_, err := svc.EffectiveAutomation(context.Background(), parent, project)
		if !errors.Is(err, api.ErrInvalidInput) {
			t.Errorf("project %q: err = %v, want ErrInvalidInput", project, err)
		}
	}
}

func TestEffective_BindingInputReportsItsOwnersView(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	binding := effSaveBinding(t, brain, parent, "p1", "active", nil, &types.AutomationAction{Agent: "explore"})
	svc := effWithProjects(brain, "p1", "p2")

	got, err := svc.EffectiveAutomation(context.Background(), binding, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation(binding, p1): %v", err)
	}
	if got.ID != parent || got.BindingID != binding || got.Automation.Action.Agent != "explore" {
		t.Fatalf("binding input: id=%q binding=%q agent=%v", got.ID, got.BindingID, got.Automation)
	}

	_, err = svc.EffectiveAutomation(context.Background(), binding, "p2")
	if !errors.Is(err, api.ErrInvalidInput) {
		t.Fatalf("binding read for another project: err = %v, want ErrInvalidInput", err)
	}
}

// effRetypeToNote rewrites an entry's frontmatter type to note, the way a hand
// edit would, and re-indexes it. The API cannot retype an entry, so this is the
// only route to a binding whose parent stopped being an automation.
func effRetypeToNote(t *testing.T, brain *BrainServiceImpl, dir, id string) {
	t.Helper()
	entry, err := brain.Recall(context.Background(), id)
	if err != nil {
		t.Fatalf("recall %s: %v", id, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, entry.Path))
	if err != nil {
		t.Fatalf("read %s: %v", entry.Path, err)
	}
	edited := strings.Replace(string(raw), "type: automation", "type: note", 1)
	if edited == string(raw) {
		t.Fatalf("%s has no 'type: automation' line to edit", entry.Path)
	}
	if err := os.WriteFile(filepath.Join(dir, entry.Path), []byte(edited), 0o644); err != nil {
		t.Fatalf("write %s: %v", entry.Path, err)
	}
	if err := brain.indexer.IndexFile(entry.Path); err != nil {
		t.Fatalf("re-index %s: %v", entry.Path, err)
	}
}

func TestEffective_BindingWithMissingParentIsBroken(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	binding := effSaveBinding(t, brain, parent, "p1", "active", nil, nil)
	if err := brain.Delete(context.Background(), parent); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	svc := effWithProjects(brain, "p1")

	got, err := svc.EffectiveAutomation(context.Background(), binding, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation: %v", err)
	}
	if !got.Broken || got.BrokenReason != types.EffectiveBrokenParentMissing {
		t.Fatalf("broken=%v reason=%q, want parent_missing", got.Broken, got.BrokenReason)
	}
	if got.Automation != nil || got.Targeted {
		t.Fatalf("a binding with no parent has automation=%v targeted=%v, want neither", got.Automation, got.Targeted)
	}
}

func TestEffective_BindingOfANonAutomationIsBroken(t *testing.T) {
	brain, _, dir := newTestBrainService(t)
	parent := effSaveGlobalCron(t, brain, "*")
	binding := effSaveBinding(t, brain, parent, "p1", "active", nil, nil)
	effRetypeToNote(t, brain, dir, parent)
	svc := effWithProjects(brain, "p1")

	got, err := svc.EffectiveAutomation(context.Background(), binding, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation: %v", err)
	}
	if !got.Broken || got.BrokenReason != types.EffectiveBrokenParentNotAutomation {
		t.Fatalf("broken=%v reason=%q, want parent_not_automation", got.Broken, got.BrokenReason)
	}
}

func TestEffective_EventParentFollowsItsProjectFilter(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Only p2 on completion",
		Status:  "active",
		Global:  serviceBoolPtr(true),
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeEvent, Event: "task.completed", Filter: map[string]string{"project": "p2"}},
		Action:  &types.AutomationAction{Type: types.AutomationActionPrompt, DirectPrompt: "x"},
	})
	if err != nil {
		t.Fatalf("save event parent: %v", err)
	}
	svc := effWithProjects(brain, "p1", "p2")

	p1, err := svc.EffectiveAutomation(context.Background(), resp.ID, "p1")
	if err != nil {
		t.Fatalf("EffectiveAutomation(p1): %v", err)
	}
	p2, err := svc.EffectiveAutomation(context.Background(), resp.ID, "p2")
	if err != nil {
		t.Fatalf("EffectiveAutomation(p2): %v", err)
	}
	if p1.Targeted || !p2.Targeted {
		t.Fatalf("event filter project=p2: p1 targeted=%v p2 targeted=%v, want false/true", p1.Targeted, p2.Targeted)
	}
}
