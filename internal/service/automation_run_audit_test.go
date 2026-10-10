package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// hasRunAuditTag reports whether tags contains want exactly.
func hasRunAuditTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// saveRunAuditForTest writes one audit through createRunAudit and returns
// the stored entry, so assertions read what the store really holds.
func saveRunAuditForTest(t *testing.T, brain *BrainServiceImpl, audit automationRunAudit) types.BrainEntry {
	t.Helper()
	svc := NewAutomationService(brain)
	id, err := svc.createRunAudit(context.Background(), audit)
	if err != nil {
		t.Fatalf("createRunAudit: %v", err)
	}
	entry, err := brain.Recall(context.Background(), id)
	if err != nil {
		t.Fatalf("Recall(%s): %v", id, err)
	}
	return *entry
}

// TestCreateRunAudit_TagsAutomationID pins that every run audit is tagged
// with its parent automation, so /automation-runs can query by tag instead
// of scanning bodies.
func TestCreateRunAudit_TagsAutomationID(t *testing.T) {
	brain, _, _ := newTestBrainService(t)

	entry := saveRunAuditForTest(t, brain, automationRunAudit{
		automation: types.BrainEntry{ID: "auto1", Path: "projects/p/automation/auto1.md"},
		project:    "p",
		status:     "queued",
	})

	if !hasRunAuditTag(entry.Tags, "automation:auto1") {
		t.Errorf("tags %v missing automation:auto1", entry.Tags)
	}
	// Body lines stay for legacy readers.
	if !strings.Contains(entry.Content, "automation_id: auto1\n") {
		t.Errorf("body lost automation_id line:\n%s", entry.Content)
	}
}

// TestCreateRunAudit_ZeroValuesAddNoStructuredFields pins that callers
// passing zero values (every caller today) get no typed slot, no binding
// tag, and no binding or scheduled_for body lines.
func TestCreateRunAudit_ZeroValuesAddNoStructuredFields(t *testing.T) {
	brain, _, _ := newTestBrainService(t)

	entry := saveRunAuditForTest(t, brain, automationRunAudit{
		automation: types.BrainEntry{ID: "auto2", Path: "projects/p/automation/auto2.md"},
		project:    "p",
		status:     "queued",
	})

	if entry.ScheduledFor != "" {
		t.Errorf("ScheduledFor = %q, want empty", entry.ScheduledFor)
	}
	if entry.Binding != "" {
		t.Errorf("Binding = %q, want empty", entry.Binding)
	}
	for _, tag := range entry.Tags {
		if strings.HasPrefix(tag, "binding:") {
			t.Errorf("unexpected binding tag %q on unbound audit", tag)
		}
	}
	if strings.Contains(entry.Content, "scheduled_for:") || strings.Contains(entry.Content, "binding:") {
		t.Errorf("zero-value audit wrote structured body lines:\n%s", entry.Content)
	}
}

// TestRunAuditScheduledFor pins the slot format: RFC3339 in UTC, and empty
// for the zero time so unscheduled callers write nothing.
func TestRunAuditScheduledFor(t *testing.T) {
	slot := time.Date(2026, 10, 9, 5, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	if got := runAuditScheduledFor(slot); got != "2026-10-09T09:00:00Z" {
		t.Errorf("runAuditScheduledFor(%v) = %q, want 2026-10-09T09:00:00Z", slot, got)
	}
	if got := runAuditScheduledFor(time.Time{}); got != "" {
		t.Errorf("runAuditScheduledFor(zero) = %q, want empty", got)
	}
}

// TestRunAuditBindingTag pins the binding tag: present only when a binding
// applied.
func TestRunAuditBindingTag(t *testing.T) {
	if got := runAuditBindingTag("bind1"); got != "binding:bind1" {
		t.Errorf("runAuditBindingTag(bind1) = %q, want binding:bind1", got)
	}
	if got := runAuditBindingTag(""); got != "" {
		t.Errorf("runAuditBindingTag(empty) = %q, want empty", got)
	}
}

// TestCreateRunAudit_StructuredScheduledForAndBinding pins that a run audit
// carries its slot and binding as typed fields, tags, and body lines, and
// that the slot is stored as the same instant in UTC.
func TestCreateRunAudit_StructuredScheduledForAndBinding(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	slot := time.Date(2026, 10, 9, 5, 0, 0, 0, time.FixedZone("EDT", -4*3600))

	entry := saveRunAuditForTest(t, brain, automationRunAudit{
		automation:   types.BrainEntry{ID: "auto1", Path: "projects/p/automation/auto1.md"},
		project:      "p",
		status:       "queued",
		scheduledFor: slot,
		binding:      "bind1",
	})

	if entry.ScheduledFor != "2026-10-09T09:00:00Z" {
		t.Errorf("ScheduledFor = %q, want 2026-10-09T09:00:00Z", entry.ScheduledFor)
	}
	if entry.Binding != "bind1" {
		t.Errorf("Binding = %q, want bind1", entry.Binding)
	}
	if !hasRunAuditTag(entry.Tags, "binding:bind1") {
		t.Errorf("tags %v missing binding:bind1", entry.Tags)
	}
	if !strings.Contains(entry.Content, "scheduled_for: 2026-10-09T09:00:00Z\n") {
		t.Errorf("body missing scheduled_for line:\n%s", entry.Content)
	}
	if !strings.Contains(entry.Content, "binding: bind1\n") {
		t.Errorf("body missing binding line:\n%s", entry.Content)
	}
	if !strings.Contains(entry.Content, "automation_id: auto1\n") {
		t.Errorf("body lost automation_id line:\n%s", entry.Content)
	}
}

// saveRunAuditAt writes one audit whose created instant is at.
func saveRunAuditAt(t *testing.T, brain *BrainServiceImpl, at time.Time, audit automationRunAudit) {
	t.Helper()
	original := types.TimeNowUTC
	types.TimeNowUTC = func() time.Time { return at }
	t.Cleanup(func() { types.TimeNowUTC = original })
	saveRunAuditForTest(t, brain, audit)
}

// TestListRunAudits_NewestFirstOwnAutomationOnly pins that the helper returns
// one automation's audits from the tag index, newest first, and nothing else.
func TestListRunAudits_NewestFirstOwnAutomationOnly(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	svc := NewAutomationService(brain)

	for i, id := range []string{"runOld", "runMid", "runNew"} {
		saveRunAuditAt(t, brain, base.Add(time.Duration(i)*time.Minute), automationRunAudit{
			automation: types.BrainEntry{ID: "auto1", Path: "projects/p/automation/auto1.md"},
			project:    "p",
			status:     "queued",
			summary:    id,
		})
	}
	saveRunAuditAt(t, brain, base.Add(10*time.Minute), automationRunAudit{
		automation: types.BrainEntry{ID: "auto2", Path: "projects/p/automation/auto2.md"},
		project:    "p",
		status:     "queued",
	})

	got, err := svc.listRunAudits(context.Background(), "p", "auto1", 10)
	if err != nil {
		t.Fatalf("listRunAudits: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d audits, want 3 for auto1", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Created < got[i].Created {
			t.Errorf("audits not newest first: %q before %q", got[i-1].Created, got[i].Created)
		}
	}
	for _, e := range got {
		if !hasRunAuditTag(e.Tags, "automation:auto1") {
			t.Errorf("audit %s returned without automation:auto1 tag: %v", e.ID, e.Tags)
		}
	}
	if !strings.Contains(got[0].Content, "summary: runNew") {
		t.Errorf("first audit is not the newest run:\n%s", got[0].Content)
	}
}

// TestListRunAudits_RespectsLimit pins that limit caps the page at the
// newest entries.
func TestListRunAudits_RespectsLimit(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	svc := NewAutomationService(brain)
	for i := 0; i < 4; i++ {
		saveRunAuditAt(t, brain, base.Add(time.Duration(i)*time.Minute), automationRunAudit{
			automation: types.BrainEntry{ID: "auto1", Path: "projects/p/automation/auto1.md"},
			project:    "p",
			status:     "queued",
			summary:    []string{"r0", "r1", "r2", "r3"}[i],
		})
	}

	got, err := svc.listRunAudits(context.Background(), "p", "auto1", 2)
	if err != nil {
		t.Fatalf("listRunAudits: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d audits, want 2", len(got))
	}
	if !strings.Contains(got[0].Content, "summary: r3") || !strings.Contains(got[1].Content, "summary: r2") {
		t.Errorf("limit did not keep the two newest audits:\n%q\n%q", got[0].Content, got[1].Content)
	}
}

// TestListRunAudits_ProjectScoped pins that the same automation id in another
// project is not returned.
func TestListRunAudits_ProjectScoped(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	svc := NewAutomationService(brain)
	auto := types.BrainEntry{ID: "auto1", Path: "projects/p/automation/auto1.md"}
	saveRunAuditAt(t, brain, base, automationRunAudit{automation: auto, project: "p", status: "queued"})
	saveRunAuditAt(t, brain, base.Add(time.Minute), automationRunAudit{automation: auto, project: "q", status: "queued"})

	got, err := svc.listRunAudits(context.Background(), "p", "auto1", 10)
	if err != nil {
		t.Fatalf("listRunAudits: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d audits for project p, want 1", len(got))
	}
	if !strings.Contains(got[0].Content, "project: p") {
		t.Errorf("returned audit is not from project p:\n%s", got[0].Content)
	}
}

// TestListRunAudits_LegacyUntaggedAuditsAreNotReturned pins that the helper is
// tag-only; legacy body-only audits are the API handler's fallback.
func TestListRunAudits_LegacyUntaggedAuditsAreNotReturned(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	_, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation_run",
		Title:   "Automation Run: legacy",
		Content: "## Automation Run Audit\n\nautomation_id: auto1\nproject: p\n",
		Status:  "queued",
		Project: "p",
	})
	if err != nil {
		t.Fatalf("Save legacy audit: %v", err)
	}

	svc := NewAutomationService(brain)
	got, err := svc.listRunAudits(context.Background(), "p", "auto1", 10)
	if err != nil {
		t.Fatalf("listRunAudits: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d audits, want 0: legacy audit has no tag", len(got))
	}
}

// TestListRunAudits_RejectsEmptyAutomationID pins that an empty id is an error,
// not a query for the bare "automation:" tag.
func TestListRunAudits_RejectsEmptyAutomationID(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	svc := NewAutomationService(brain)
	if _, err := svc.listRunAudits(context.Background(), "p", "", 10); err == nil {
		t.Fatal("listRunAudits with empty automation id returned nil error")
	}
}
