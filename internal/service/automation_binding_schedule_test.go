package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// Bindings on the scheduler, driven on the slot fixture's fake clock. A
// binding changes what one project runs, never the parent's history: slots,
// dedup keys and max_runs stay keyed to (parent, project).

// bindingSpec describes one binding for the fixture to save.
type bindingSpec struct {
	status    string // default active
	trigger   *types.TriggerConfig
	action    *types.AutomationAction
	startsAt  string
	expiresAt string
	maxRuns   *int
}

func cronTrigger(schedule string) *types.TriggerConfig {
	return &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: schedule}
}

// saveBinding writes a binding of parent for project at the fixture's instant.
func (f *slotFixture) saveBinding(parent, project string, b bindingSpec) *types.CreateEntryResponse {
	f.t.Helper()
	status := b.status
	if status == "" {
		status = "active"
	}
	resp, err := f.brain.Save(context.Background(), types.CreateEntryRequest{
		Type:      "automation",
		Title:     "Binding for " + project,
		Content:   "binding fixture",
		Status:    status,
		Project:   project,
		Extends:   parent,
		Trigger:   b.trigger,
		Action:    b.action,
		StartsAt:  b.startsAt,
		ExpiresAt: b.expiresAt,
		MaxRuns:   b.maxRuns,
	})
	if err != nil {
		f.t.Fatalf("Save binding for %s: %v", project, err)
	}
	stampModified(f.t, f.brain, resp.Path, f.now)
	return resp
}

// setStatus moves an entry to status at the instant at, as a PATCH would.
func (f *slotFixture) setStatus(path, status string, at time.Time) {
	f.t.Helper()
	f.setNow(at)
	s := status
	if _, err := f.brain.Update(context.Background(), path, types.UpdateEntryRequest{Status: &s}); err != nil {
		f.t.Fatalf("set status %s on %s: %v", status, path, err)
	}
	stampModified(f.t, f.brain, path, at)
}

func (f *slotFixture) recall(id string) types.BrainEntry {
	f.t.Helper()
	entry, err := f.brain.Recall(context.Background(), id)
	if err != nil {
		f.t.Fatalf("recall %s: %v", id, err)
	}
	return *entry
}

func tasksIn(t *testing.T, brain *BrainServiceImpl, project, automationID string) int {
	t.Helper()
	return len(generatedTasksFor(t, brain, project, automationID))
}

// ---------------------------------------------------------------------------
// Target selection
// ---------------------------------------------------------------------------

func TestBinding_OptInFiresAProjectOutsideTheFilter(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "p1"}}})
	f.saveBinding(parent.ID, "p2", bindingSpec{trigger: cronTrigger("0 5 * * *")})

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 0 {
		t.Fatalf("p2 at 03:00: %d tasks, want 0 (its binding runs at 05:00)", n)
	}
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("p1 at 03:00: %d tasks, want 1", n)
	}

	f.tick(slotUTC(2026, 10, 8, 5, 0, 0))
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 1 {
		t.Fatalf("p2 opted in: %d tasks at 05:00, want 1", n)
	}
}

func TestBinding_OptOutStopsOnlyItsProject(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	f.saveBinding(parent.ID, "p1", bindingSpec{status: "inactive"})

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 0 {
		t.Fatalf("opted-out p1: %d tasks, want 0", n)
	}
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 1 {
		t.Fatalf("p2 unaffected: %d tasks, want 1", n)
	}
}

func TestBinding_CompletedBindingOptsItsProjectOut(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	f.saveBinding(parent.ID, "p1", bindingSpec{status: "completed"})

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 0 {
		t.Fatalf("completed binding must opt its project out, got %d tasks", n)
	}
}

func TestBinding_BindingOverridesTheSchedule(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 6 * * *")})

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 0 {
		t.Fatalf("p1 runs at 06:00 under its binding, got %d tasks at 03:00", n)
	}
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 1 {
		t.Fatalf("p2 keeps the parent schedule: %d tasks at 03:00, want 1", n)
	}

	f.tick(slotUTC(2026, 10, 8, 6, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("p1 at 06:00: %d tasks, want 1", n)
	}
}

func TestBinding_InactiveParentStopsEveryProject(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 3 * * *")})
	f.setStatus(parent.Path, "inactive", slotUTC(2026, 10, 8, 0, 0, 1))

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	for _, project := range []string{"p1", "p2"} {
		if n := tasksIn(t, f.brain, project, parent.ID); n != 0 {
			t.Fatalf("inactive parent: %s has %d tasks, want 0", project, n)
		}
	}
}

func TestBinding_DeletedParentLeavesBindingsInert(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "p2"}}})
	binding := f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 4 * * *")})
	if err := f.brain.Delete(context.Background(), parent.ID); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	f.tick(slotUTC(2026, 10, 8, 4, 0, 0))
	if n := allGeneratedTasks(t, f.brain, []string{"p1", "p2"}, binding.ID); n != 0 {
		t.Fatalf("a binding must never run on its own: %d tasks under its own id", n)
	}
}

func TestBinding_ProjectOwnedParentIgnoresItsBindings(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{project: "p1", trigger: types.TriggerConfig{Schedule: "0 3 * * *"}})
	f.saveBinding(parent.ID, "p2", bindingSpec{trigger: cronTrigger("0 5 * * *")})

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("the project-owned parent still runs for its project: %d tasks, want 1", n)
	}
	f.tick(slotUTC(2026, 10, 8, 5, 0, 0))
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 0 {
		t.Fatalf("a binding of a project-owned parent is never evaluated: %d tasks for p2", n)
	}
}

// ---------------------------------------------------------------------------
// History and lifecycle
// ---------------------------------------------------------------------------

func TestBinding_AddingABindingNeverFiresAPastSlot(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))

	// Added at 10:00 with a 09:00 slot: that slot already passed.
	f.setNow(slotUTC(2026, 10, 8, 10, 0, 0))
	f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 9 * * *")})
	f.tick(slotUTC(2026, 10, 8, 10, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("adding a binding fired a past slot: p1 has %d tasks, want the 03:00 one only", n)
	}
}

func TestBinding_OptOutAndBackNeverReplaysASlot(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	binding := f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 9 * * *")})
	// Tick through the 09:00 slot as the per-minute scheduler would, so the
	// slot is seen on time and handled.
	f.tick(slotUTC(2026, 10, 8, 9, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 9, 30, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("setup: p1 after its 09:00 binding slot has %d tasks, want 1", n)
	}

	f.setStatus(binding.Path, "inactive", slotUTC(2026, 10, 8, 10, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 10, 0, 0))

	// Reactivated at 13:00. The 09:00 slot was already past and must not
	// fire on reactivation, though it was never handled while opted out.
	f.setStatus(binding.Path, "active", slotUTC(2026, 10, 8, 13, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 13, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 13, 1, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("reactivation replayed a past slot: p1 has %d tasks, want 1", n)
	}

	f.tick(slotUTC(2026, 10, 9, 9, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 2 {
		t.Fatalf("the next binding slot: p1 has %d tasks, want 2", n)
	}
}

func TestBinding_ExpiredBindingOptsItsProjectOut(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	binding := f.saveBinding(parent.ID, "p1", bindingSpec{
		trigger:   cronTrigger("0 5 * * *"),
		expiresAt: "2026-10-08T04:00:00Z",
	})

	// Tick every slot on time, as the scheduler would: p2's 03:00 today counts.
	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 5, 0, 0))
	if got := f.recall(binding.ID).Status; got != "completed" {
		t.Fatalf("expired binding status = %q, want completed", got)
	}
	if got := f.recall(parent.ID).Status; got != "active" {
		t.Fatalf("expiring a binding must not touch the parent: status %q", got)
	}

	// Tomorrow's 03:00 is the parent's own slot. An expired binding has opted
	// its project out, so p1 must not run; p2 keeps the parent's schedule.
	f.tick(slotUTC(2026, 10, 9, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 0 {
		t.Fatalf("expired binding: p1 ran on the parent's slot (%d tasks)", n)
	}
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 2 {
		t.Fatalf("p2 keeps the parent schedule: %d tasks, want 2 (both days)", n)
	}
}

func TestBinding_StartsAtGatesOnlyItsProject(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	f.saveBinding(parent.ID, "p1", bindingSpec{
		trigger:  cronTrigger("0 5 * * *"),
		startsAt: "2026-10-08T12:00:00Z",
	})

	// Before starts_at the binding's schedule is not owed, and the parent's
	// 03:00 is no longer p1's schedule, so p1 stays quiet while p2 runs.
	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 0 {
		t.Fatalf("p1 at 03:00 before its binding starts: %d tasks, want 0", n)
	}
	if n := tasksIn(t, f.brain, "p2", parent.ID); n != 1 {
		t.Fatalf("p2 at 03:00: %d tasks, want 1", n)
	}
	f.tick(slotUTC(2026, 10, 8, 5, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 0 {
		t.Fatalf("p1 at 05:00 before its binding starts: %d tasks, want 0", n)
	}
	f.tick(slotUTC(2026, 10, 9, 5, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("p1 after its starts_at: %d tasks, want 1", n)
	}
}

func TestBinding_MaxRunsCountsPerParentAndProjectWithoutCompletingParent(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	one := 1
	binding := f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 5 * * *"), maxRuns: &one})

	f.tick(slotUTC(2026, 10, 8, 5, 0, 0))
	f.tick(slotUTC(2026, 10, 9, 5, 0, 0))
	if n := tasksIn(t, f.brain, "p1", parent.ID); n != 1 {
		t.Fatalf("binding max_runs 1: p1 has %d tasks, want 1", n)
	}
	if got := f.recall(parent.ID).Status; got != "active" {
		t.Fatalf("a binding's max_runs must not complete the parent: status %q", got)
	}
	if got := f.recall(binding.ID).Status; got != "active" {
		t.Fatalf("binding's max_runs is a skip, not a completion: status %q", got)
	}
	if n := len(auditsSkippedFor(t, f.brain, parent.ID, "max_runs")); n == 0 {
		t.Fatalf("the exhausted binding must write a max_runs skip audit under the parent")
	}
}

// ---------------------------------------------------------------------------
// Lineage: generated tasks and audits
// ---------------------------------------------------------------------------

func TestBinding_GeneratedTaskKeepsParentLineageAndNamesTheBinding(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p1", "p2")
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{Schedule: "0 3 * * *", Filter: map[string]string{"project": "*"}}})
	binding := f.saveBinding(parent.ID, "p1", bindingSpec{trigger: cronTrigger("0 3 * * *")})

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	tasks := generatedTasksFor(t, f.brain, "p1", parent.ID)
	if len(tasks) != 1 {
		t.Fatalf("p1 tasks = %d, want 1", len(tasks))
	}
	task := f.recall(tasks[0].ID)
	if task.GeneratedBy != "automation:"+parent.ID {
		t.Fatalf("generated_by = %q, want the parent", task.GeneratedBy)
	}
	if task.Binding != binding.ID {
		t.Fatalf("binding = %q, want %q", task.Binding, binding.ID)
	}
	audits := automationRunAudits(t, f.brain, parent.ID)
	if len(audits) != 2 {
		t.Fatalf("audits under the parent = %d, want 2 (p1 and p2)", len(audits))
	}
	tagged := 0
	for _, audit := range audits {
		for _, tag := range audit.Tags {
			if tag == "binding:"+binding.ID {
				tagged++
			}
		}
	}
	if tagged != 1 {
		t.Fatalf("binding-tagged audits = %d, want exactly the p1 run", tagged)
	}
}

// ---------------------------------------------------------------------------
// Event and manual paths
// ---------------------------------------------------------------------------

func saveEventParent(t *testing.T, brain *BrainServiceImpl) string {
	t.Helper()
	global := true
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:   "automation",
		Title:  "Review on completion",
		Global: &global,
		Status: "active",
		Agent:  "tdd-dev",
		Trigger: &types.TriggerConfig{
			Type:   types.TriggerTypeEvent,
			Event:  "task.completed",
			Filter: map[string]string{"project": "*"},
		},
		Action: &types.AutomationAction{Type: types.AutomationActionPrompt, Agent: "tdd-dev", DirectPrompt: "Review {{.Project}}"},
	})
	if err != nil {
		t.Fatalf("save event parent: %v", err)
	}
	return resp.ID
}

func completedEvent(project string) types.Event {
	return types.Event{Type: "task.completed", Source: "api", ProjectID: project, Timestamp: time.Now().UTC()}
}

func TestBinding_EventAutomationUsesThePerProjectAgent(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveEventParent(t, brain)
	saveBindingDirect(t, brain, parent, "p1", bindingSpecDirect{action: &types.AutomationAction{Agent: "explore"}})
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(context.Background(), completedEvent("p1")); err != nil {
		t.Fatalf("HandleEvent p1: %v", err)
	}
	if err := svc.HandleEvent(context.Background(), completedEvent("p2")); err != nil {
		t.Fatalf("HandleEvent p2: %v", err)
	}
	p1 := generatedTasksFor(t, brain, "p1", parent)
	p2 := generatedTasksFor(t, brain, "p2", parent)
	if len(p1) != 1 || len(p2) != 1 {
		t.Fatalf("tasks p1=%d p2=%d, want 1 each", len(p1), len(p2))
	}
	if p1[0].Agent != "explore" {
		t.Fatalf("p1 agent = %q, want the binding's explore", p1[0].Agent)
	}
	if p2[0].Agent != "tdd-dev" {
		t.Fatalf("p2 agent = %q, want the parent's tdd-dev", p2[0].Agent)
	}
}

func TestBinding_EventOptOutAndOptInFollowTheBinding(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveEventParent(t, brain)
	saveBindingDirect(t, brain, parent, "p1", bindingSpecDirect{status: "inactive"})
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(context.Background(), completedEvent("p1")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if n := tasksIn(t, brain, "p1", parent); n != 0 {
		t.Fatalf("event for an opted-out project ran: %d tasks", n)
	}
}

func TestBinding_EventOptInOutsideTheFilterRuns(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	global := true
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type: "automation", Title: "Only p2", Global: &global, Status: "active",
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeEvent, Event: "task.completed", Filter: map[string]string{"project": "p2"}},
		Action:  &types.AutomationAction{Type: types.AutomationActionPrompt, DirectPrompt: "x"},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	saveBindingDirect(t, brain, resp.ID, "p1", bindingSpecDirect{})
	svc := NewAutomationService(brain)

	if err := svc.HandleEvent(context.Background(), completedEvent("p1")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if n := tasksIn(t, brain, "p1", resp.ID); n != 1 {
		t.Fatalf("an active binding opts its project in on events: %d tasks, want 1", n)
	}
}

func TestBinding_RunAutomationNowOnABindingRunsTheParentForItsProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveEventParent(t, brain)
	binding := saveBindingDirect(t, brain, parent, "p1", bindingSpecDirect{action: &types.AutomationAction{Agent: "explore"}})
	svc := NewAutomationService(brain)

	ids, err := svc.RunAutomationNow(context.Background(), binding, "")
	if err != nil {
		t.Fatalf("RunAutomationNow(binding): %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("created %d tasks, want 1", len(ids))
	}
	tasks := generatedTasksFor(t, brain, "p1", parent)
	if len(tasks) != 1 {
		t.Fatalf("p1 tasks = %d, want 1", len(tasks))
	}
	if tasks[0].Agent != "explore" {
		t.Fatalf("manual run agent = %q, want the binding's explore", tasks[0].Agent)
	}
	if tasks[0].Binding != binding {
		t.Fatalf("manual run binding = %q, want %q", tasks[0].Binding, binding)
	}
}

// ---------------------------------------------------------------------------
// Duplicates
// ---------------------------------------------------------------------------

func TestBinding_OldestBindingWinsADuplicate(t *testing.T) {
	older := types.BrainEntry{ID: "aaaaaaaa", ProjectID: "p1", Created: "2026-10-05T00:00:00Z", Status: "active"}
	newer := types.BrainEntry{ID: "bbbbbbbb", ProjectID: "p1", Created: "2026-10-09T00:00:00Z", Status: "active"}
	tieLow := types.BrainEntry{ID: "00000001", ProjectID: "p1", Created: "2026-10-05T00:00:00Z", Status: "active"}

	winners, losers := pickBindingWinners([]types.BrainEntry{newer, older})
	if winners["p1"].ID != "aaaaaaaa" || len(losers) != 1 || losers[0].ID != "bbbbbbbb" {
		t.Fatalf("oldest must win: winner=%q losers=%v", winners["p1"].ID, losers)
	}

	winners, _ = pickBindingWinners([]types.BrainEntry{older, tieLow})
	if winners["p1"].ID != "00000001" {
		t.Fatalf("equal created times: the lower ID must win, got %q", winners["p1"].ID)
	}
}

// ---------------------------------------------------------------------------
// Direct binding writes for the event and manual tests
// ---------------------------------------------------------------------------

type bindingSpecDirect struct {
	status string
	action *types.AutomationAction
}

// saveBindingDirect saves a binding of parent for project without a clock and
// returns its ID.
func saveBindingDirect(t *testing.T, brain *BrainServiceImpl, parent, project string, b bindingSpecDirect) string {
	t.Helper()
	status := b.status
	if status == "" {
		status = "active"
	}
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Binding for " + project,
		Status:  status,
		Project: project,
		Extends: parent,
		Action:  b.action,
	})
	if err != nil {
		t.Fatalf("Save binding for %s: %v", project, err)
	}
	return resp.ID
}
