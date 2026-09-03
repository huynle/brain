package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// entryCreatedAutomation builds a minimal, matchable event automation scoped
// to a project, with the given trigger filter.
func entryCreatedAutomation(id string, filter map[string]string) types.BrainEntry {
	return types.BrainEntry{
		ID:        id,
		Type:      "automation",
		ProjectID: "supernote",
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventEntryCreated,
			Filter: filter,
		},
		Action: &types.AutomationAction{Type: "create_task"},
	}
}

// entryCreatedEvent mirrors what HandleCreateEntry now emits.
func entryCreatedEvent(entryID, tags, mediaTypes string) types.Event {
	evt := types.NewEvent(types.EventEntryCreated, types.EventSourceAPI)
	evt.ProjectID = "supernote"
	evt.TaskID = entryID
	evt.TaskPath = "projects/supernote/report/" + entryID + ".md"
	evt.Metadata = map[string]string{
		"entry_type":             "report",
		"title":                  "Captured page",
		"entry_id":               entryID,
		"tags":                   tags,
		"has_attachment":         "true",
		"attachment_media_types": mediaTypes,
	}
	return evt
}

// TestAutomationMatchesEntryCreatedMetadataFallback proves the claim that no
// matcher change was needed for the new metadata keys: getEventField falls
// through to evt.Metadata for any key it does not name explicitly, so
// entry_type / entry_id / has_attachment / attachment_media_types are all
// usable as trigger filters through the real automationMatchesEvent path.
func TestAutomationMatchesEntryCreatedMetadataFallback(t *testing.T) {
	evt := entryCreatedEvent("abc12def", "supernote,page,report", "image/png")

	tests := []struct {
		name   string
		filter map[string]string
		want   bool
	}{
		{"entry_type exact match", map[string]string{"entry_type": "report"}, true},
		{"entry_type mismatch", map[string]string{"entry_type": "task"}, false},
		{"entry_id exact match", map[string]string{"entry_id": "abc12def"}, true},
		{"entry_id mismatch", map[string]string{"entry_id": "other"}, false},
		{"has_attachment true", map[string]string{"has_attachment": "true"}, true},
		{"has_attachment false does not match", map[string]string{"has_attachment": "false"}, false},
		{"media type exact", map[string]string{"attachment_media_types": "image/png"}, true},
		{"wildcard on metadata key", map[string]string{"attachment_media_types": "*"}, true},
		{"in: form over metadata", map[string]string{"entry_type": "in:report,plan"}, true},
		{"unknown metadata key fails closed", map[string]string{"nonexistent": "x"}, false},
		{
			name:   "several metadata filters are ANDed",
			filter: map[string]string{"entry_type": "report", "has_attachment": "true"},
			want:   true,
		},
		{
			name:   "one failing filter rejects the whole match",
			filter: map[string]string{"entry_type": "report", "has_attachment": "false"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			automation := entryCreatedAutomation("auto-1", tt.filter)
			if got := automationMatchesEvent(automation, evt); got != tt.want {
				t.Errorf("automationMatchesEvent(filter=%v) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

// TestAutomationMatchesEntryCreatedTagsHasFilter is the end-to-end proof for
// the has: form over the real matcher. The substring case is the reason has:
// is element-exact rather than a contains: test: "note" lives inside
// "supernote" and must not match.
func TestAutomationMatchesEntryCreatedTagsHasFilter(t *testing.T) {
	evt := entryCreatedEvent("abc12def", "supernote,page,report", "image/png")

	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{"tag present", "has:supernote", true},
		{"middle tag present", "has:page", true},
		{"auto-appended type tag present", "has:report", true},
		{"tag absent", "has:archived", false},
		{"substring of a tag does not match", "has:note", false},
		{"empty operand fails closed", "has:", false},
		{"exact-match form still requires the whole joined value", "supernote", false},
		{"exact-match form matches the whole joined value", "supernote,page,report", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			automation := entryCreatedAutomation("auto-tags", map[string]string{"tags": tt.filter})
			if got := automationMatchesEvent(automation, evt); got != tt.want {
				t.Errorf("automationMatchesEvent(tags=%q) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

// TestAutomationGeneratedKeyOncePerTaskIDIsDistinctPerEntry is the regression
// that most justifies setting evt.TaskID on entry.created.
//
// automationGeneratedKey resolves once_per through getEventField. With TaskID
// unset, every entry.created produced "automation:<id>:" — one constant key —
// so the dedup check matched the first generated task forever and the
// automation fired exactly once, ever. This asserts both halves: the old
// shape collapses, the new shape does not.
func TestAutomationGeneratedKeyOncePerTaskIDIsDistinctPerEntry(t *testing.T) {
	automation := entryCreatedAutomation("auto-oncePer", nil)
	automation.Trigger.OncePer = "task_id"

	first := automationGeneratedKey(automation, entryCreatedEvent("abc12def", "report", ""))
	second := automationGeneratedKey(automation, entryCreatedEvent("xyz98765", "report", ""))

	if first == "" || second == "" {
		t.Fatalf("generated keys must be non-empty; got %q and %q", first, second)
	}
	if first == second {
		t.Fatalf("once_per: task_id produced identical keys %q for two different entries — the automation would fire only once, ever", first)
	}
	if first != "automation:auto-oncePer:abc12def" {
		t.Errorf("first key = %q, want %q", first, "automation:auto-oncePer:abc12def")
	}
	if second != "automation:auto-oncePer:xyz98765" {
		t.Errorf("second key = %q, want %q", second, "automation:auto-oncePer:xyz98765")
	}

	// Pin the pre-fix behaviour so the regression is unambiguous: an
	// entry.created without TaskID collapses both entries onto one key.
	legacyA := entryCreatedEvent("abc12def", "report", "")
	legacyB := entryCreatedEvent("xyz98765", "report", "")
	legacyA.TaskID, legacyB.TaskID = "", ""
	if automationGeneratedKey(automation, legacyA) != automationGeneratedKey(automation, legacyB) {
		t.Fatal("expected the pre-fix (empty TaskID) shape to collapse onto one key; the regression this guards no longer reproduces")
	}
}

// TestAutomationMatchesAttachmentEvents covers the two new event types through
// the real matcher, including the entry identity carried by
// entry.attachment_added.
func TestAutomationMatchesAttachmentEvents(t *testing.T) {
	attachmentCreated := types.NewEvent(types.EventAttachmentCreated, types.EventSourceAPI)
	attachmentCreated.ProjectID = "supernote"
	attachmentCreated.Metadata = map[string]string{
		"attachment_id": "att_9",
		"media_type":    "image/png",
		"filename":      "page.png",
		"size_bytes":    "2048",
	}

	added := types.NewEvent(types.EventEntryAttachmentAdded, types.EventSourceAPI)
	added.ProjectID = "supernote"
	added.TaskID = "abc12def"
	added.TaskPath = "projects/supernote/report/abc12def.md"
	added.Metadata = map[string]string{
		"attachment_id": "att_9",
		"media_type":    "image/png",
		"role":          "source",
	}

	t.Run("attachment.created matches on media type", func(t *testing.T) {
		automation := entryCreatedAutomation("auto-att", map[string]string{"media_type": "image/png"})
		automation.Trigger.Event = types.EventAttachmentCreated
		if !automationMatchesEvent(automation, attachmentCreated) {
			t.Error("attachment.created did not match a media_type filter")
		}
	})

	t.Run("entry.attachment_added matches on role", func(t *testing.T) {
		automation := entryCreatedAutomation("auto-added", map[string]string{"role": "source"})
		automation.Trigger.Event = types.EventEntryAttachmentAdded
		if !automationMatchesEvent(automation, added) {
			t.Error("entry.attachment_added did not match a role filter")
		}
	})

	t.Run("entry.attachment_added dedups per entry via once_per task_id", func(t *testing.T) {
		automation := entryCreatedAutomation("auto-added", nil)
		automation.Trigger.Event = types.EventEntryAttachmentAdded
		automation.Trigger.OncePer = "task_id"
		other := added
		other.TaskID = "zzz11111"
		if automationGeneratedKey(automation, added) == automationGeneratedKey(automation, other) {
			t.Error("two different entries produced the same once_per: task_id key")
		}
	})

	t.Run("a wildcard entry.* trigger picks up entry.attachment_added", func(t *testing.T) {
		automation := entryCreatedAutomation("auto-wild", nil)
		automation.Trigger.Event = "entry.*"
		if !automationMatchesEvent(automation, added) {
			t.Error("entry.* did not match entry.attachment_added")
		}
	})
}
