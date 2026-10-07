package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGolden_WebhookTools(t *testing.T) {
	g := newGolden(t, "webhook_tools")
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)
	name := g.project("golden-hook")

	g.call("create missing url", "webhook_create", map[string]any{"name": name, "events": []any{"task.created"}})
	g.call("create missing events", "webhook_create", map[string]any{"name": name, "url": "https://example.invalid/hook"})
	g.call("create invalid url", "webhook_create", map[string]any{"name": name, "url": "not a url", "events": []any{"task.created"}})

	created := g.call("create", "webhook_create", map[string]any{
		"name": name, "url": receiver.URL + "/hook", "events": []any{"task.created", "task.completed"},
		"secret": "s3cret", "filter": map[string]any{"project": "golden"},
	})
	id := field(t, created, `\*\*ID:\*\* (\S+)`)
	g.bind("HOOK_ID", id)
	g.bind("HOOK_PATH", receiver.URL)

	g.call("get", "webhook_get", map[string]any{"id": id})
	g.call("get missing arg", "webhook_get", map[string]any{})
	g.call("get unknown", "webhook_get", map[string]any{"id": "nonexistent"})

	g.call("update", "webhook_update", map[string]any{"id": id, "name": name + "-renamed", "events": []any{"task.completed"}, "enabled": false})
	g.call("update invalid url", "webhook_update", map[string]any{"id": id, "url": "nope"})
	g.call("update unknown", "webhook_update", map[string]any{"id": "nonexistent", "name": "x"})
	g.call("toggle on", "webhook_toggle", map[string]any{"id": id, "enabled": true})
	g.call("toggle unknown", "webhook_toggle", map[string]any{"id": "nonexistent", "enabled": true})

	g.call("list", "webhook_list", map[string]any{})
	g.call("list enabled only", "webhook_list", map[string]any{"enabled_only": true})
	g.call("deliveries none", "webhook_deliveries", map[string]any{"id": id})
	g.scrubRe(`Latency: \S+`, "Latency: <LATENCY>")
	g.scrubRe(`(?m)^(- \*\*Latency:\*\*) \S+`, "$1 <LATENCY>")
	g.scrubRe(`test_(<HOOK_ID>)_\d+`, "test_${1}_<MILLIS>")
	g.scrubRe(`(?m)^- \*\*[0-9a-f]{16}\*\* -`, "- **<DELIVERY_ID>** -")
	g.call("test", "webhook_test", map[string]any{"id": id})
	g.call("deliveries", "webhook_deliveries", map[string]any{"id": id, "limit": 5})
	g.call("test unknown", "webhook_test", map[string]any{"id": "nonexistent"})
	g.call("deliveries unknown", "webhook_deliveries", map[string]any{"id": "nonexistent", "limit": 3})

	g.call("delete", "webhook_delete", map[string]any{"id": id})
	g.call("delete again", "webhook_delete", map[string]any{"id": id})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead api list", "webhook_list", map[string]any{"enabled_only": true})
	g.callAt(dead, "dead api deliveries", "webhook_deliveries", map[string]any{"id": "w1", "limit": 2})
	g.callAt(dead, "dead api delete", "webhook_delete", map[string]any{"id": "w1"})
	g.check()
}
