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
