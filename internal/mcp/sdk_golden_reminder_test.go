package mcp_test

import "testing"

func TestGolden_ReminderTools(t *testing.T) {
	g := newGolden(t, "reminder_tools")
	project := g.project("golden-reminders")

	g.call("create refuses without project", "reminder_create", map[string]any{"title": "nowhere"})
	g.call("create missing title", "reminder_create", map[string]any{"project": project})
	g.call("create invalid remind_at", "reminder_create", map[string]any{"project": project, "title": "bad date", "remind_at": "tomorrow"})
	g.call("create task action without prompt", "reminder_create", map[string]any{"project": project, "title": "no prompt", "remind_at": "2031-01-02T09:00:00Z", "action": "task"})

	undated := g.call("create undated", "reminder_create", map[string]any{
		"project": project, "title": "Undated thing", "content": "body", "tags": []any{"a", "b"},
	})
	undatedID := field(t, undated, "reminder_id: `([^`]+)`")
	g.bind("UNDATED_ID", undatedID)

	dated := g.call("create dated repeating", "reminder_create", map[string]any{
		"project": project, "title": "Weekly check", "remind_at": "2031-01-02T09:00:00-06:00",
		"timezone": "America/Denver", "repeat": "weekly", "repeat_until": "2032-01-01T00:00:00Z",
		"action": "task", "prompt": "check the thing", "feature_id": "golden-feature",
	})
	datedID := field(t, dated, "reminder_id: `([^`]+)`")
	g.bind("DATED_ID", datedID)

	g.call("get", "reminder_get", map[string]any{"reminder_id": datedID})
	g.call("get missing id arg", "reminder_get", map[string]any{})
	g.call("get unknown", "reminder_get", map[string]any{"reminder_id": "zzzzzzzz"})

	g.call("list project", "reminder_list", map[string]any{"project": project})
	g.call("list state undated", "reminder_list", map[string]any{"project": project, "state": "undated"})
	g.call("list empty project", "reminder_list", map[string]any{"project": g.project("golden-reminders-empty")})
	g.call("list invalid state", "reminder_list", map[string]any{"project": project, "state": "bogus"})

	g.call("update title", "reminder_update", map[string]any{"reminder_id": undatedID, "title": "Renamed"})
	g.call("update clears date", "reminder_update", map[string]any{"reminder_id": datedID, "remind_at": "", "repeat": "", "repeat_until": ""})
	g.call("update invalid action", "reminder_update", map[string]any{"reminder_id": undatedID, "action": "explode"})
	g.call("update unknown", "reminder_update", map[string]any{"reminder_id": "zzzzzzzz", "title": "x"})

	g.call("snooze missing args", "reminder_snooze", map[string]any{"reminder_id": undatedID})
	g.call("snooze", "reminder_snooze", map[string]any{"reminder_id": undatedID, "remind_at": "2031-03-04T05:06:07Z"})
	g.call("snooze invalid time", "reminder_snooze", map[string]any{"reminder_id": undatedID, "remind_at": "later"})
	g.call("snooze offset fraction", "reminder_snooze", map[string]any{"reminder_id": undatedID, "remind_at": " 2031-03-04T07:06:07.250+02:00 "})
	g.call("snooze no offset", "reminder_snooze", map[string]any{"reminder_id": undatedID, "remind_at": "2031-03-04T05:06:07"})
	g.call("snooze unknown bad time", "reminder_snooze", map[string]any{"reminder_id": "zzzzzzzz", "remind_at": "later"})
	g.call("snooze unknown", "reminder_snooze", map[string]any{"reminder_id": "zzzzzzzz", "remind_at": "2031-03-04T05:06:07Z"})
	g.call("ack", "reminder_ack", map[string]any{"reminder_id": undatedID})
	g.call("ack unknown", "reminder_ack", map[string]any{"reminder_id": "zzzzzzzz"})

	g.call("delete", "reminder_delete", map[string]any{"reminder_id": datedID})
	g.call("delete again", "reminder_delete", map[string]any{"reminder_id": datedID})
	g.call("delete missing arg", "reminder_delete", map[string]any{})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead api get", "reminder_get", map[string]any{"reminder_id": "abc"})
	g.callAt(dead, "dead api list", "reminder_list", map[string]any{"project": project, "state": "fired"})
	g.callAt(dead, "dead api delete", "reminder_delete", map[string]any{"reminder_id": "abc"})
	g.callAt(dead, "dead api snooze", "reminder_snooze", map[string]any{"reminder_id": "a/b", "remind_at": "later"})
	g.check()
}
