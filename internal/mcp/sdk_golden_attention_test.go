package mcp_test

import (
	"testing"
	"time"
)

func TestGolden_AttentionTools(t *testing.T) {
	g := newGolden(t, "attention_tools")
	project := g.project("golden-attention")

	g.call("create missing kind", "attention_create", map[string]any{"title": "x"})
	g.call("create invalid severity", "attention_create", map[string]any{"kind": "custom", "title": "bad", "severity": "apocalyptic", "project": project})

	first := g.call("create", "attention_create", map[string]any{
		"kind": "custom", "title": "Look at this", "body": "details here", "severity": "warning",
		"project": project, "task_id": "tsk00001", "feature_id": "f1", "dedup_key": g.project("dedup"),
	})
	firstID := field(t, first, `ID: (\S+)`)
	g.bind("ATTN1", firstID)
	// created_at has second granularity; keep list order deterministic.
	time.Sleep(1100 * time.Millisecond)
	second := g.call("create minimal", "attention_create", map[string]any{"kind": "custom", "title": "Minimal", "project": project})
	secondID := field(t, second, `ID: (\S+)`)
	g.bind("ATTN2", secondID)

	g.call("get", "attention_get", map[string]any{"id": firstID})
	g.call("get missing arg", "attention_get", map[string]any{})
	g.call("get unknown", "attention_get", map[string]any{"id": "nonexistent"})

	g.call("list project", "attention_list", map[string]any{"project": project})
	g.call("list severity", "attention_list", map[string]any{"project": project, "severity": "warning"})
	g.call("list empty", "attention_list", map[string]any{"project": g.project("golden-attention-empty")})
	g.call("list invalid state", "attention_list", map[string]any{"project": project, "state": "bogus"})

	g.call("snooze", "attention_snooze", map[string]any{"id": secondID, "snoozed_until": "2031-01-01T00:00:00Z"})
	g.call("snooze invalid", "attention_snooze", map[string]any{"id": secondID, "snoozed_until": "later"})
	g.call("snooze unknown", "attention_snooze", map[string]any{"id": "nonexistent", "snoozed_until": "2031-01-01T00:00:00Z"})
	g.call("resolve", "attention_resolve", map[string]any{"id": firstID})
	g.call("resolve unknown", "attention_resolve", map[string]any{"id": "nonexistent"})
	g.call("list resolved", "attention_list", map[string]any{"project": project, "state": "resolved"})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead api list", "attention_list", map[string]any{"project": "p", "kind": "k"})
	g.callAt(dead, "dead api resolve", "attention_resolve", map[string]any{"id": "a1"})
	g.check()
}
