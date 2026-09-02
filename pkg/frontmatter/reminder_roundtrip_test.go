package frontmatter

import (
	"encoding/json"
	"strings"
	"testing"
)

// The nested `reminder:` block has to survive all seven registration points.
// Missing any one of them is SILENT: an unknown YAML key lands in
// Frontmatter.Extra, which nothing reads, so the entry round-trips through the
// API looking correct and comes back from disk with no schedule at all. That
// is exactly how checkout_mode shipped write-only.
func TestReminderConfig_SurvivesGenerateAndParse(t *testing.T) {
	in := &ReminderConfig{
		ID:              "rem123",
		RemindAt:        "2026-09-10T09:00:00-06:00",
		Timezone:        "America/Denver",
		Action:          "task",
		Prompt:          "Follow up on the migration",
		Agent:           "tdd-dev",
		Model:           "anthropic/claude-opus-4-5",
		Executor:        "opencode",
		ExecutionMode:   "worktree",
		TargetWorkdir:   "/repos/thing",
		FiredAt:         "2026-09-10T09:00:04-06:00",
		GeneratedTaskID: "tsk9",
	}

	out := Generate(&GenerateOptions{
		Title: "Check the migration", Type: "reminder", Status: "active",
		Reminder: in,
	})
	if !strings.Contains(out, "reminder:") {
		t.Fatalf("Generate emitted no reminder block:\n%s", out)
	}

	doc, err := Parse("---\n" + out + "---\n\nbody\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := doc.Frontmatter.Reminder
	if got == nil {
		t.Fatalf("reminder block did not survive the round trip:\n%s", out)
	}

	// Field by field, so a single missed yaml tag is named rather than
	// showing up as a vague inequality.
	for _, c := range []struct{ name, want, got string }{
		{"id", in.ID, got.ID},
		{"remind_at", in.RemindAt, got.RemindAt},
		{"timezone", in.Timezone, got.Timezone},
		{"action", in.Action, got.Action},
		{"prompt", in.Prompt, got.Prompt},
		{"agent", in.Agent, got.Agent},
		{"model", in.Model, got.Model},
		{"executor", in.Executor, got.Executor},
		{"execution_mode", in.ExecutionMode, got.ExecutionMode},
		{"target_workdir", in.TargetWorkdir, got.TargetWorkdir},
		{"fired_at", in.FiredAt, got.FiredAt},
		{"generated_task_id", in.GeneratedTaskID, got.GeneratedTaskID},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}

	// And it must NOT have fallen through to Extra — that is the silent
	// failure mode, and it can coexist with a correct-looking parse.
	if _, leaked := doc.Frontmatter.Extra["reminder"]; leaked {
		t.Error(`"reminder" landed in Extra: knownFields is missing the key`)
	}
}

// The indexer marshals Frontmatter into notes.metadata as JSON, so the json
// tags are as load-bearing as the yaml ones — a missing json tag makes every
// database read-back blind while the markdown file looks perfect.
func TestReminderConfig_HasJSONTagsForTheIndexer(t *testing.T) {
	fm := &Frontmatter{
		Title: "t", Type: "reminder", Status: "active",
		Reminder: &ReminderConfig{ID: "r1", RemindAt: "2026-09-10T09:00:00Z", Action: "notify"},
	}
	blob, err := json.Marshal(fm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatal(err)
	}
	rem, ok := back["reminder"].(map[string]any)
	if !ok {
		t.Fatalf(`no "reminder" key in the indexed metadata: %s`, blob)
	}
	if rem["remind_at"] != "2026-09-10T09:00:00Z" {
		t.Errorf("remind_at missing from indexed metadata: %v", rem)
	}
	if rem["action"] != "notify" {
		t.Errorf("action missing from indexed metadata: %v", rem)
	}
}

// An undated reminder is a first-class shape — "just something to come back
// to" — and must not acquire an empty remind_at key on the way through.
func TestReminderConfig_UndatedStaysUndated(t *testing.T) {
	out := Generate(&GenerateOptions{
		Title: "Look at this sometime", Type: "reminder", Status: "active",
		Reminder: &ReminderConfig{ID: "r2", Action: "notify"},
	})
	if strings.Contains(out, "remind_at") {
		t.Errorf("undated reminder emitted a remind_at key:\n%s", out)
	}
	doc, err := Parse("---\n" + out + "---\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Frontmatter.Reminder == nil || doc.Frontmatter.Reminder.RemindAt != "" {
		t.Errorf("undated reminder did not survive: %+v", doc.Frontmatter.Reminder)
	}
}
