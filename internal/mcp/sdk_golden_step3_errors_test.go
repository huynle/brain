package mcp_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/mcp"
)

// cannedAPI answers every request with one configurable response, so each
// tool's handling of HTTP error shapes is pinned independently of whether the
// real server can be driven into that error.
type cannedAPI struct {
	mu          sync.Mutex
	status      int
	ctype, body string
	srv, mcpSrv *httptest.Server
}

func newCannedAPI(t *testing.T) *cannedAPI {
	t.Helper()
	c := &cannedAPI{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		status, ctype, body := c.status, c.ctype, c.body
		c.mu.Unlock()
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(c.srv.Close)
	c.mcpSrv = httptest.NewServer(mcp.NewHTTPHandler(mcp.NewAPIClient(c.srv.URL)))
	t.Cleanup(c.mcpSrv.Close)
	return c
}

func (c *cannedAPI) respond(status int, ctype, body string) {
	c.mu.Lock()
	c.status, c.ctype, c.body = status, ctype, body
	c.mu.Unlock()
}

func (c *cannedAPI) mcpURL() string { return c.mcpSrv.URL + "/mcp" }

// step3ToolCalls is one representative call per API request made by the task,
// feature, assignment, project, reader, sync and supervisor tools.
func step3ToolCalls() []parityCall {
	return []parityCall{
		{"tasks", map[string]any{"project": "p"}},
		{"task_next", map[string]any{"project": "p"}},
		{"task_get", map[string]any{"project": "p", "task_id": "t"}},
		{"task_metadata", map[string]any{"project": "p", "task_id": "t"}},
		{"tasks_status", map[string]any{"project": "p", "task_ids": []string{"t"}}},
		{"task_trigger", map[string]any{"project": "p", "task_id": "t"}},
		{"monitor_enable", map[string]any{"template_id": "blocked-inspector", "project": "p", "feature_id": "f"}},
		{"monitor_disable", map[string]any{"template_id": "blocked-inspector", "project": "p", "feature_id": "f"}},
		{"feature_review_enable", map[string]any{"project": "p", "feature_id": "f"}},
		{"feature_review_disable", map[string]any{"project": "p", "feature_id": "f"}},
		{"blocked_inspector_enable", map[string]any{"project": "p", "feature_id": "f"}},
		{"blocked_inspector_disable", map[string]any{"project": "p", "feature_id": "f"}},
		{"dream_enable", map[string]any{"project": "p"}},
		{"dream_disable", map[string]any{"project": "p"}},
		{"resume_task_with_context", map[string]any{"project": "p", "task_id": "t", "injected_context": "c"}},
		{"runner_candidates", map[string]any{"project": "p", "task_id": "t"}},
		{"runner_candidates", map[string]any{"project": "p", "executor": "pi"}},
		{"task_assign", map[string]any{"project": "p", "task_id": "t", "runner_id": "r"}},
		{"task_clear_assignment", map[string]any{"project": "p", "task_id": "t"}},
		{"features", map[string]any{"project": "p"}},
		{"features", map[string]any{"project": "p", "ready_only": true}},
		{"feature_ready", map[string]any{"project": "p"}},
		{"feature_get", map[string]any{"project": "p", "feature_id": "f"}},
		{"feature_checkout", map[string]any{"project": "p", "feature_id": "f"}},
		{"feature_assign", map[string]any{"project": "p", "feature_id": "f", "runner_id": "r"}},
		{"feature_clear_assignment", map[string]any{"project": "p", "feature_id": "f"}},
		{"feature_runner_candidates", map[string]any{"project": "p", "feature_id": "f"}},
		{"context_resolve", map[string]any{"client_id": "c", "host_id": "h"}},
		{"project_placement_get", map[string]any{"project": "p"}},
		{"project_placement_put", map[string]any{"project": "p", "affinity": "soft"}},
		{"reader_url", map[string]any{"path": "abcd1234"}},
		{"reader_url", map[string]any{"path": "projects/p/note/abcd1234.md"}},
		{"sync_status", nil},
		{"sync_diff", map[string]any{"device_id": "d", "operation_id": "o"}},
		{"sync_reconcile", map[string]any{"device_id": "d", "operation_id": "o", "snapshot": "s", "action": "discard"}},
		{"session_tail", map[string]any{"runner_id": "r", "session_id": "s"}},
		{"session_children", map[string]any{"runner_id": "r", "session_id": "s"}},
		{"resource_health", map[string]any{"project": "p"}},
		{"events_wait", map[string]any{"project": "p", "timeout_ms": 0}},
		{"delivery_gate", map[string]any{"project": "p", "task_id": "t"}},
		{"delivery_verify", map[string]any{"project": "p", "task_id": "t", "expected_revision": 1}},
		{"delivery_record", map[string]any{"project": "p", "task_id": "t", "command": map[string]any{"action": "configure"}}},
		{"supervisor_capabilities", nil},
		{"supervisor_snapshot", map[string]any{"project": "p"}},
		{"task_dispatch_preview", map[string]any{"project": "p", "task_id": "t"}},
		{"supervisor_operation", map[string]any{"id": "op-errors-01", "operation": "trigger", "project": "p", "task_id": "t"}},
		{"supervisor_operation_get", map[string]any{"id": "op-errors-01"}},
		{"supervisor_checkpoint", map[string]any{"project": "p"}},
		{"supervisor_checkpoint", map[string]any{"command": map[string]any{"action": "request"}}},
		{"execution_budget", map[string]any{"project": "p", "id": "b"}},
		{"execution_budget", map[string]any{"command": map[string]any{"action": "commit"}}},
	}
}

// TestGolden_Step3HTTPErrorShapes pins, for every step-3 tool, the text an
// agent reads when the API answers with each legacy error shape: a JSON
// message, a JSON error without a message, a non-JSON error body and a
// non-JSON success body (a decode failure, not an HTTP error). The monitor
// tools classify errors by their text (409/conflict, 404/not found), so those
// statuses are pinned with both bare and JSON bodies too.
func TestGolden_Step3HTTPErrorShapes(t *testing.T) {
	api := newCannedAPI(t)
	g := newGoldenAt(t, "step3_http_error_shapes", api.srv.URL)
	shapes := []struct {
		label, ctype, body string
		status             int
	}{
		{"json message 503", "application/json", `{"error":"Service Unavailable","message":"maintenance window"}`, 503},
		{"json error only 403", "application/json", `{"error":"forbidden here"}`, 403},
		{"non-json 502", "text/html", "<html>bad gateway</html>", 502},
		{"non-json 200", "text/html", "<html>ok</html>", 200},
	}
	for _, shape := range shapes {
		api.respond(shape.status, shape.ctype, shape.body)
		for _, c := range step3ToolCalls() {
			g.callAt(api.mcpURL(), shape.label+" "+canonicalJSON(mustJSON(c.args)), c.tool, c.args)
		}
	}
	monitors := []parityCall{
		{"monitor_enable", map[string]any{"template_id": "dream", "project": "p"}},
		{"feature_review_enable", map[string]any{"project": "p", "feature_id": "f"}},
		{"blocked_inspector_enable", map[string]any{"project": "p", "feature_id": "f"}},
		{"dream_enable", map[string]any{"project": "p"}},
		{"monitor_disable", map[string]any{"template_id": "dream", "project": "p"}},
		{"feature_review_disable", map[string]any{"project": "p", "feature_id": "f"}},
		{"blocked_inspector_disable", map[string]any{"project": "p", "feature_id": "f"}},
		{"dream_disable", map[string]any{"project": "p"}},
	}
	for _, shape := range []struct {
		label, ctype, body string
		status             int
	}{
		{"bare 409", "text/plain", "taken", 409},
		{"json 409 conflict", "application/json", `{"error":"Conflict","message":"Conflict: already there"}`, 409},
		{"bare 404", "text/plain", "gone", 404},
		{"json 404 not found", "application/json", `{"error":"Not Found","message":"monitor not found"}`, 404},
		{"json 500 mentions 404", "application/json", `{"error":"Internal Server Error","message":"upstream said 404"}`, 500},
	} {
		api.respond(shape.status, shape.ctype, shape.body)
		for _, c := range monitors {
			g.callAt(api.mcpURL(), shape.label+" "+canonicalJSON(mustJSON(c.args)), c.tool, c.args)
		}
	}
	g.check()
}
