package frontmatter

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Automation scheduling (design docs/plans/2026-10-08-automation-scheduling-design.md)
// adds keys that nothing evaluates yet. These tests pin that every one of them
// survives the on-disk hops: a hand-written file -> Parse -> Serialize, and
// the JSON of Frontmatter that the indexer stores in notes.metadata. A key
// missing from rawFrontmatter/knownFields lands in Extra (json:"-") and is
// silently dropped on its way to SQLite.

// schedulingFieldsYAML is a raw automation file as a user would type it,
// carrying every new key. all_day is an unquoted YAML bool and at is an
// unquoted clock value on purpose: hand edits look like this.
const schedulingFieldsYAML = `---
title: Hindsight dream
type: automation
status: active
extends: abc12345
scheduled_for: "2026-10-09T03:00:00Z"
binding: def67890
trigger:
  type: calendar
  every: 4d
  at: 03:00
  stagger: 2h
  catch_up: none
  calendar: xnys
  skip_if_event:
    calendar: work
    title: "re:(?i)^OOO"
    description: "*"
    location: in:home,remote
    all_day: true
  only_if_event:
    calendar: personal
    title: "has:focus"
  match:
    title: "re:(?i)^1:1 (?P<person>.+)$"
    location: "*"
  offset: -15m
action:
  type: prompt
  agent: explore
  prompt_append: Weight decisions about the ingestion pipeline more heavily.
---

Body
`

// schedulingFieldsJSON is what notes.metadata must contain for the keys
// above, expressed as the JSON paths the service read-back reads.
var schedulingFieldsJSON = map[string]any{
	"extends":       "abc12345",
	"scheduled_for": "2026-10-09T03:00:00Z",
	"binding":       "def67890",
	"trigger": map[string]any{
		"type":     "calendar",
		"event":    "",
		"every":    "4d",
		"at":       "03:00",
		"stagger":  "2h",
		"catch_up": "none",
		"calendar": "xnys",
		"skip_if_event": map[string]any{
			"calendar":    "work",
			"title":       "re:(?i)^OOO",
			"description": "*",
			"location":    "in:home,remote",
			"all_day":     "true",
		},
		"only_if_event": map[string]any{
			"calendar": "personal",
			"title":    "has:focus",
		},
		"match": map[string]any{
			"title":    "re:(?i)^1:1 (?P<person>.+)$",
			"location": "*",
		},
		"offset": "-15m",
	},
	"action": map[string]any{
		"type":          "prompt",
		"agent":         "explore",
		"prompt_append": "Weight decisions about the ingestion pipeline more heavily.",
	},
}

// metadataJSON mirrors the indexer: json.Marshal(*Frontmatter) is the
// notes.metadata column.
func metadataJSON(t *testing.T, fm *Frontmatter) map[string]any {
	t.Helper()
	data, err := json.Marshal(fm)
	if err != nil {
		t.Fatalf("marshal frontmatter: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	return out
}

func assertSchedulingMetadata(t *testing.T, stage string, fm *Frontmatter) {
	t.Helper()
	meta := metadataJSON(t, fm)
	for key, want := range schedulingFieldsJSON {
		if got := meta[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: metadata[%q] =\n  %#v\nwant\n  %#v", stage, key, got, want)
		}
	}
}

func TestAutomationSchedulingFields_RawFileRoundTrip(t *testing.T) {
	doc, err := Parse(schedulingFieldsYAML)
	if err != nil {
		t.Fatalf("parse raw file: %v", err)
	}
	for _, key := range []string{"extends", "scheduled_for", "binding"} {
		if _, leaked := doc.Frontmatter.Extra[key]; leaked {
			t.Errorf("%s leaked into Frontmatter.Extra — missing from knownFields", key)
		}
	}
	assertSchedulingMetadata(t, "parse", &doc.Frontmatter)

	// Serialize -> Parse must keep every value (the write path after an
	// update), and a second pass must be byte-identical (canonical form).
	first := Serialize(&doc.Frontmatter)
	reparsed, err := Parse("---\n" + first + "---\n")
	if err != nil {
		t.Fatalf("parse serialized frontmatter: %v\n%s", err, first)
	}
	assertSchedulingMetadata(t, "serialize->parse", &reparsed.Frontmatter)
	if second := Serialize(&reparsed.Frontmatter); second != first {
		t.Errorf("serialization is not stable:\n--- first ---\n%s--- second ---\n%s", first, second)
	}
}

// TestAutomationSchedulingFields_CanonicalOrder pins where the new top-level
// keys sit, so files do not churn when later tasks start writing them.
func TestAutomationSchedulingFields_CanonicalOrder(t *testing.T) {
	doc, err := Parse(schedulingFieldsYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := Serialize(&doc.Frontmatter)
	order := []string{"\nextends:", "\nscheduled_for:", "\nbinding:", "\ntrigger:", "\naction:"}
	last := -1
	for _, key := range order {
		idx := strings.Index(out, key)
		if idx < 0 {
			t.Fatalf("%q not serialized:\n%s", strings.TrimSpace(key), out)
		}
		if idx < last {
			t.Errorf("%q is out of canonical order:\n%s", strings.TrimSpace(key), out)
		}
		last = idx
	}
}

// legacyAutomationGolden is Serialize's output for an automation that uses
// every pre-scheduling field, captured before the scheduling fields existed.
// Existing files must re-serialize to exactly these bytes.
const legacyAutomationGolden = `title: Dream consolidation
type: automation
tags:
  - automation
  - dream
status: active
max_runs: 5
starts_at: "2026-01-03T00:00:00Z"
expires_at: "2027-06-30T00:00:00-04:00"
timezone: America/New_York
created: 2026-01-02T03:04:05Z
projectId: brain-api
agent: general
generated_by: "automation:abc12345"
automation_run_id: run12345
trigger:
  type: cron
  event: ""
  events:
      - task.completed
      - feature.completed
  schedule: 0 3 * * *
  timezone: America/New_York
  filter:
      project: '*'
      to_status: in:completed,blocked
  once_per: day
  ignore_automation_events: false
  cooldown: 5m
  max_concurrent: 2
action:
  type: prompt
  direct_prompt: Consolidate {{.Project}}
  agent: general
  model: openai/gpt-5
  executor: opencode
  target_workdir: /tmp/w
  execution_mode: current_branch
  session_mode: fresh
  timeout: 1h
  set_status: archived
retry:
  max_attempts: 2
  backoff: fixed
  delay: 30s
`

func TestLegacyAutomation_ReserializesByteIdentically(t *testing.T) {
	doc, err := Parse("---\n" + legacyAutomationGolden + "---\n\nBody\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := Serialize(&doc.Frontmatter); got != legacyAutomationGolden {
		t.Errorf("legacy automation re-serialized differently:\n--- got ---\n%s--- want ---\n%s", got, legacyAutomationGolden)
	}
	if len(doc.Frontmatter.Extra) != 0 {
		t.Errorf("legacy keys landed in Extra: %v", doc.Frontmatter.Extra)
	}
}

func TestAutomationSchedulingFields_OmittedWhenEmpty(t *testing.T) {
	out := Generate(&GenerateOptions{
		Title:   "Plain automation",
		Type:    "automation",
		Trigger: &TriggerConfig{Type: "cron", Schedule: "0 3 * * *"},
		Action:  &AutomationAction{Type: "prompt", DirectPrompt: "go"},
	})
	for _, key := range []string{
		"\nextends:", "\nscheduled_for:", "\nbinding:",
		"\n  every:", "\n  at:", "\n  stagger:", "\n  catch_up:", "\n  calendar:",
		"\n  skip_if_event:", "\n  only_if_event:", "\n  match:", "\n  offset:",
		"\n  prompt_append:",
	} {
		if strings.Contains(out, key) {
			t.Errorf("empty %s was emitted:\n%s", strings.TrimSpace(key), out)
		}
	}
}

// TestAutomationSchedulingFields_GenerateRoundTrip follows
// TestOriginFields_RoundTrip for the typed create path: GenerateOptions ->
// Generate -> Parse must return every value, with none left in Extra.
func TestAutomationSchedulingFields_GenerateRoundTrip(t *testing.T) {
	opts := &GenerateOptions{
		Title:        "Calendar automation",
		Type:         "automation",
		Status:       "active",
		Extends:      "abc12345",
		ScheduledFor: "2026-10-09T03:00:00Z",
		Binding:      "def67890",
		Trigger: &TriggerConfig{
			Type:        "calendar",
			Every:       "2d",
			At:          "start",
			Stagger:     "0s",
			CatchUp:     "1h",
			Calendar:    "work",
			SkipIfEvent: &CalendarEventFilter{Calendar: "work", Title: "re:^OOO", Description: "has:x", Location: "in:a,b", AllDay: "true"},
			OnlyIfEvent: &CalendarEventFilter{Calendar: "personal", AllDay: "false"},
			Match:       map[string]string{"title": "re:(?i)^1:1 (?P<person>.+)$", "all_day": "false"},
			Offset:      "-15m",
		},
		Action: &AutomationAction{Type: "prompt", PromptAppend: "Line one: with a colon\nline two"},
	}

	generated := Generate(opts)
	doc, err := Parse("---\n" + generated + "---\n")
	if err != nil {
		t.Fatalf("parse generated frontmatter: %v\n%s", err, generated)
	}
	fm := doc.Frontmatter
	if fm.Extends != opts.Extends || fm.ScheduledFor != opts.ScheduledFor || fm.Binding != opts.Binding {
		t.Errorf("top-level fields = (%q, %q, %q), want (%q, %q, %q)\n%s",
			fm.Extends, fm.ScheduledFor, fm.Binding, opts.Extends, opts.ScheduledFor, opts.Binding, generated)
	}
	if fm.Trigger == nil || !reflect.DeepEqual(*fm.Trigger, *opts.Trigger) {
		t.Errorf("trigger = %+v, want %+v\n%s", fm.Trigger, opts.Trigger, generated)
	}
	if fm.Action == nil || fm.Action.PromptAppend != opts.Action.PromptAppend {
		t.Errorf("action = %+v, want prompt_append %q\n%s", fm.Action, opts.Action.PromptAppend, generated)
	}
	if len(fm.Extra) != 0 {
		t.Errorf("keys leaked into Extra (missing from knownFields): %v", fm.Extra)
	}
}
