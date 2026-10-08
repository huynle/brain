package mcp_test

import (
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/mcp"
)

// TestRequestParity_Step3Tools records the exact REST requests sent by the
// task, feature, assignment, project, reader, sync and supervisor tools
// (method, escaped path, query, canonical body, credential, content
// negotiation and any leaked X-Brain-* header). Recorded on the legacy client
// and compared line for line after the move to the SDK.
func TestRequestParity_Step3Tools(t *testing.T) {
	api := dedicatedAPI(t)
	rec := &requestRecorder{}
	// Every real response these tools receive is checked against the public
	// contract they are about to be served through.
	rec.contractCheck(t, func(string) bool { return true })
	proxy := httptest.NewServer(rec.handler(api))
	t.Cleanup(proxy.Close)
	mcpSrv := httptest.NewServer(mcp.NewHTTPHandler(mcp.NewAPIClient(proxy.URL)))
	t.Cleanup(mcpSrv.Close)

	g := newGoldenAt(t, "request_parity_step3", api)
	g.scrubRe(`[0-9a-f]{64}`, "<SHA256>")
	p := "parity3"
	alpha := entryFields(t, api, map[string]any{"type": "task", "title": "Parity alpha", "content": "a", "project": p, "status": "pending", "feature_id": "f 1/x"})
	g.bind("ALPHA", alpha)
	solo := entryFields(t, api, map[string]any{"type": "task", "title": "Parity solo", "content": "s", "project": p, "status": "pending"})
	g.bind("SOLO", solo)
	note := entryFields(t, api, map[string]any{"type": "scratch", "title": "Parity note", "content": "n", "project": p})
	g.bind("NOTE", note)
	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{"runner_id": "parity-runner", "hostname": "h", "executors": []string{"opencode"}})
	apiDo(t, "PUT", api+"/api/v1/runners/parity-runner/instances/ins-1", map[string]any{"kind": "adhoc", "status": "idle", "started_at": 1, "last_seen": 1})
	connectScriptedRunner(t, api, "parity-runner", scriptedResponse)
	apiJSON(t, "POST", api+"/api/v1/sync/devices/parity-device-00001/report", map[string]any{
		"reported_online": true, "ready": true, "epoch": "e",
		"pending": []map[string]any{{"id": "op 1", "path": "projects/" + p + "/scratch/" + note + ".md", "method": "PATCH", "revision": "r", "raw": "---\nx: y\n---\nz", "error": "conflict"}},
	}, nil)

	session := map[string]any{"runner_id": "parity-runner", "session_id": "ses/1"}
	calls := []parityCall{
		{"tasks", map[string]any{"project": p, "status": "pending", "limit": 3}},
		{"tasks", map[string]any{"project": "par ity/x"}},
		{"task_next", map[string]any{"project": p}},
		{"task_next", map[string]any{"project": "parity-empty"}},
		{"task_get", map[string]any{"project": p, "task_id": alpha}},
		{"task_metadata", map[string]any{"project": p, "taskId": "parity solo"}},
		{"tasks_status", map[string]any{"project": p, "task_ids": []string{alpha, "x/y"}}},
		{"tasks_status", map[string]any{"project": p, "task_ids": []string{alpha}, "wait_for": "any", "timeout": 1}},
		{"task_trigger", map[string]any{"project": p, "task_id": "a b/c"}},
		{"monitor_enable", map[string]any{"template_id": "blocked-inspector", "project": p, "feature_id": "f 1/x", "schedule": "*/30 * * * *"}},
		{"monitor_disable", map[string]any{"template_id": "blocked-inspector", "project": p, "feature_id": "f 1/x"}},
		{"feature_review_enable", map[string]any{"project": p, "feature_id": "rev"}},
		{"feature_review_disable", map[string]any{"project": p, "feature_id": "rev"}},
		{"blocked_inspector_enable", map[string]any{"project": p, "feature_id": "blk", "schedule": "0 * * * *"}},
		{"blocked_inspector_disable", map[string]any{"project": p, "feature_id": "blk"}},
		{"dream_enable", map[string]any{"project": p}},
		{"dream_disable", map[string]any{"project": p}},
		{"resume_task_with_context", map[string]any{"project": p, "task_id": solo, "injected_context": "ctx", "prefer_same_session": false, "executor_override": "pi", "force": true}},
		{"resume_task_with_context", map[string]any{"project": p, "task_id": "t/1", "injected_context": "ctx"}},
		{"features", map[string]any{"project": p, "limit": 2}},
		{"features", map[string]any{"project": p, "ready_only": true}},
		{"feature_ready", map[string]any{"project": "par ity"}},
		{"feature_get", map[string]any{"project": p, "feature_id": "f 1/x"}},
		{"feature_checkout", map[string]any{"project": p, "feature_id": "f 1/x"}},
		{"feature_checkout", map[string]any{"project": p, "feature_id": "f 1/x", "execution_branch": "b", "merge_target_branch": "main", "merge_policy": "prompt_only", "merge_strategy": "rebase", "remote_branch_policy": "delete", "open_pr_before_merge": true, "execution_mode": "current_branch", "checkout_mode": "ai"}},
		{"feature_assign", map[string]any{"project": p, "feature_id": "f 1/x", "runner_id": "parity-runner"}},
		{"feature_assign", map[string]any{"project": p, "feature_id": "f 1/x", "runner_id": "parity-runner", "intent": "reassign", "force": true}},
		{"feature_clear_assignment", map[string]any{"project": p, "feature_id": "f 1/x"}},
		{"feature_clear_assignment", map[string]any{"project": p, "feature_id": "f 1/x", "intent": "clear"}},
		{"feature_runner_candidates", map[string]any{"project": p, "feature_id": "f 1/x", "include_rejected": true}},
		{"runner_candidates", map[string]any{"project": p, "task_id": solo}},
		{"runner_candidates", map[string]any{"project": p}},
		{"runner_candidates", map[string]any{"project": p, "feature_id": "f", "executor": "pi", "requires_capability": []string{"gpu", "x y"}, "git_remote": "https://example.test/r.git", "machine_affinity": "local", "origin_machine_id": "m", "execution_mode": "worktree", "target_workdir": "/w"}},
		{"task_assign", map[string]any{"project": p, "task_id": solo, "runner_id": "parity-runner"}},
		{"task_assign", map[string]any{"project": p, "task_id": solo, "runner_id": "parity-runner", "intent": "reassign", "force": true}},
		{"task_clear_assignment", map[string]any{"project": p, "task_id": solo}},
		{"context_resolve", map[string]any{"client_id": "c", "host_id": "h"}},
		{"context_resolve", map[string]any{"client_id": "c", "host_id": "h", "kind": "k", "hostname": "n", "os": "o", "arch": "a", "username": "u", "home_dir": "/h", "labels": map[string]any{"a": "b", "n": 1}, "capabilities": []string{"c1"}, "path": "/p", "git_root": "/g", "git_common_dir": "/g/.git", "git_worktree_main": "/g", "git_branch": "b", "git_remote": "r", "folder_name": "f"}},
		{"project_placement_get", map[string]any{"project": "par ity/x"}},
		{"project_placement_put", map[string]any{"project": "par ity", "affinity": "none"}},
		{"project_placement_put", map[string]any{"project": p, "affinity": "strict", "preferred_machines": []string{"m1"}, "allowed_machines": []string{"m1", "m2"}, "workspace_policy": "current_branch", "required_labels": map[string]any{"z": "1"}, "required_capabilities": []string{"git"}, "resources": map[string]any{"cpu": 2}}},
		{"reader_url", map[string]any{"path": note}},
		{"reader_url", map[string]any{"path": "projects/" + p + "/scratch/" + note + ".md", "base_url": "https://brain.example.test"}},
		{"reader_url", map[string]any{"path": "projects/a b/scratch/c%d.md"}},
		{"session_tail", session},
		{"session_tail", map[string]any{"runner_id": "parity-runner", "session_id": "ses/1", "after": "cur 1", "limit": 5, "max_bytes": 2048}},
		{"session_children", session},
		{"session_children", map[string]any{"runner_id": "parity-runner", "session_id": "ses/1", "after": "x", "limit": 3}},
		{"resource_health", map[string]any{"project": p}},
		{"resource_health", map[string]any{"project": "par ity", "task_id": "t/1"}},
		{"events_wait", map[string]any{"project": p, "timeout_ms": 0}},
		{"events_wait", map[string]any{"project": "par ity", "task_id": "t", "feature_id": "f", "type": "task.*", "timeout_ms": 0, "limit": 5, "after": "c u"}},
		{"delivery_gate", map[string]any{"project": p, "task_id": solo}},
		{"delivery_verify", map[string]any{"project": p, "task_id": "t/1", "expected_revision": 4}},
		{"delivery_verify", map[string]any{"project": p, "task_id": solo}},
		{"delivery_record", map[string]any{"project": p, "task_id": solo, "command": map[string]any{"action": "configure", "expected_revision": 0, "policy": map[string]any{"required": "none"}}}},
		{"delivery_record", map[string]any{"project": p, "task_id": solo, "command": map[string]any{"action": "integration", "expected_revision": 1, "artifact": "abc", "passed": false, "evidence_reference": "r"}}},
		{"supervisor_capabilities", map[string]any{"project": p}},
		{"supervisor_snapshot", map[string]any{"project": p}},
		{"supervisor_snapshot", map[string]any{"project": p, "task_id": solo, "feature_id": "f 1/x", "limit": 2, "after_task": "a", "manual": false}},
		{"task_dispatch_preview", map[string]any{"project": p, "task_id": solo, "manual": true}},
		{"task_dispatch_preview", map[string]any{"project": p, "task_id": solo, "feature_id": "f", "after_task": "z", "limit": 9}},
		{"supervisor_operation", map[string]any{"id": "parity-op-01", "operation": "prompt", "runner_id": "parity-runner", "instance_id": "ins-1", "session_id": "ses-1", "text": "go"}},
		{"supervisor_operation", map[string]any{"id": "parity-op-02", "operation": "trigger", "project": p, "task_id": solo, "budget_id": "", "budget_units": 0, "checkpoint_revision": 0}},
		{"supervisor_operation_get", map[string]any{"id": "parity-op-01"}},
		{"supervisor_operation_get", map[string]any{"id": "a b/c"}},
		{"supervisor_checkpoint", map[string]any{"project": p}},
		{"supervisor_checkpoint", map[string]any{"project": p, "id": "chk 1", "after": "z"}},
		{"supervisor_checkpoint", map[string]any{"command": map[string]any{"action": "request", "expected_revision": 0, "checkpoint": map[string]any{"id": "parity-chk-1", "project": p, "artifact": "a", "question": "q?"}}}},
		{"execution_budget", map[string]any{"project": p, "id": "b 1"}},
		{"execution_budget", map[string]any{"command": map[string]any{"action": "configure", "expected_revision": 0, "budget": map[string]any{"id": "parity-bud-1", "project": p, "timezone": "UTC", "unit": "runs", "limit": 3}}}},
		{"sync_status", nil},
		{"sync_diff", map[string]any{"device_id": "parity-device-00001", "operation_id": "op 1"}},
		{"sync_reconcile", map[string]any{"device_id": "parity-device-00001", "operation_id": "op 1", "snapshot": "stale", "action": "discard"}},
		{"sync_reconcile", map[string]any{"device_id": "d/1", "operation_id": "o", "snapshot": "s", "action": "merge", "raw": "---\nx\n---\n"}},
	}
	for i, c := range calls {
		ok, text := callWithHeaders(t, mcpSrv.URL+"/mcp", fmt.Sprintf("probe3-%02d", i), c.tool, c.args)
		// Tool text is pinned by the goldens; errors are kept here because
		// they say which request the server refused.
		outcome := "OK"
		if !ok {
			first, _, _ := strings.Cut(text, "\n")
			outcome = "ERROR: " + first
		}
		g.note("=== %s %s -> %s", c.tool, canonicalJSON(mustJSON(c.args)), outcome)
		for _, r := range rec.drain() {
			g.note("  %s", r)
		}
	}
	g.check()
	seen := rec.contractReport(t)
	var ops []string
	for op, statuses := range seen {
		var codes []string
		for code := range statuses {
			codes = append(codes, fmt.Sprint(code))
		}
		sort.Strings(codes)
		ops = append(ops, op+" "+strings.Join(codes, ","))
	}
	sort.Strings(ops)
	t.Logf("contract-checked live responses:\n%s", strings.Join(ops, "\n"))
}
