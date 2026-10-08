package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

// step3Operation selects the SDK operations added for the step-3 tools.
func step3Operation(op string) bool {
	group, _, _ := strings.Cut(op, ".")
	switch group {
	case "monitors", "clientContext", "sync", "supervision":
		return true
	}
	return slices.Contains([]string{"tasks.runnerCandidates", "tasks.proposedRunnerCandidates", "features.runnerCandidates", "control.sessionTail", "control.sessionDescendants", "tasks.verifyDelivery"}, op)
}

// TestStep3OperationsMatchContractLive drives every step-3 SDK operation
// through the Go SDK against the real in-process API (and a scripted runner on
// the real bridge for the session views), checking each live response, success
// and error alike, against the declared OpenAPI response: undeclared or
// missing properties, wrong JSON types and undeclared statuses all fail. The
// typed Go decode of every success is exercised on the way.
func TestStep3OperationsMatchContractLive(t *testing.T) {
	api := dedicatedAPI(t)
	rec := &requestRecorder{}
	rec.contractCheck(t, step3Operation)
	proxy := httptest.NewServer(rec.handler(api))
	t.Cleanup(proxy.Close)
	sc, err := brain.New(brain.Config{BaseURL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sc.Close)
	ctx, o := context.Background(), brain.RequestOptions{}
	p := "contract3"
	task := entryFields(t, api, map[string]any{"type": "task", "title": "Contract", "content": "c", "project": p, "status": "pending", "feature_id": "feat-c"})
	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{"runner_id": "contract-runner", "hostname": "h", "executors": []string{"opencode"}, "projects": []string{p}})
	ok := func(label string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", label, err)
		}
	}
	fails := func(label, code string, err error) {
		t.Helper()
		var be *brain.Error
		if !errors.As(err, &be) || be.Code != code {
			t.Errorf("%s: %v, want %s", label, err, code)
		}
	}
	str := func(s string) *string { return &s }

	// Monitors.
	_, err = sc.Monitors().Create(ctx, brain.CreateMonitorRequest{TemplateId: "dream", ScopeType: "project", Project: str(p)}, o)
	ok("monitor create", err)
	_, err = sc.Monitors().Create(ctx, brain.CreateMonitorRequest{TemplateId: "dream", ScopeType: "project", Project: str(p)}, o)
	fails("monitor duplicate", "conflict", err)
	_, err = sc.Monitors().Create(ctx, brain.CreateMonitorRequest{TemplateId: "no-such-template", ScopeType: "project", Project: str(p)}, o)
	fails("monitor unknown template", "invalid_request", err)
	_, err = sc.Monitors().DeleteByScope(ctx, brain.DeleteMonitorByScopeRequest{TemplateId: "dream", Scope: brain.MonitorScope{Type: "project", Project: str(p)}}, o)
	ok("monitor delete", err)
	_, err = sc.Monitors().DeleteByScope(ctx, brain.DeleteMonitorByScopeRequest{TemplateId: "dream", Scope: brain.MonitorScope{Type: "project", Project: str(p)}}, o)
	fails("monitor delete again", "not_found", err)
	_, err = sc.Monitors().DeleteByScope(ctx, brain.DeleteMonitorByScopeRequest{Scope: brain.MonitorScope{Type: "project"}}, o)
	fails("monitor delete without template", "invalid_request", err)

	// Runner candidates and client context.
	_, err = sc.Tasks().RunnerCandidates(ctx, p, task)
	ok("task candidates", err)
	_, err = sc.Tasks().RunnerCandidates(ctx, p, "zzzzzzzz")
	fails("task candidates unknown", "not_found", err)
	_, err = sc.Tasks().ProposedRunnerCandidates(ctx, p, brain.TaskRunnerCandidatesRequest{Executor: str("opencode")})
	ok("proposed candidates", err)
	_, err = sc.Features().RunnerCandidates(ctx, p, "feat-c")
	ok("feature candidates", err)
	_, err = sc.ClientContext().Resolve(ctx, brain.ResolveClientContextRequest{Client: brain.BrainClientInfo{ClientId: "c", HostId: "machine_00000001"}}, o)
	ok("resolve", err)
	_, err = sc.ClientContext().Resolve(ctx, brain.ResolveClientContextRequest{Client: brain.BrainClientInfo{HostId: "h"}}, o)
	fails("resolve without client", "invalid_request", err)

	// Browser sync.
	synced := entryFields(t, api, map[string]any{"type": "scratch", "title": "Synced", "content": "server", "project": p})
	apiJSON(t, "POST", api+"/api/v1/sync/devices/contract-device-001/report", map[string]any{
		"reported_online": true, "ready": true, "epoch": "e", "cache_mode": "recent", "cached_entries": 1,
		"pending": []map[string]any{{"id": "op-1", "path": "projects/" + p + "/scratch/" + synced + ".md", "method": "PATCH", "revision": "r", "raw": "---\ntitle: Synced\n---\nbrowser", "error": "conflict"}},
	}, nil)
	_, err = sc.Sync().Devices(ctx)
	ok("sync devices", err)
	diff, err := sc.Sync().Diff(ctx, "contract-device-001", "op-1")
	ok("sync diff", err)
	_, err = sc.Sync().Diff(ctx, "contract-device-001", "op-9")
	fails("sync diff unknown op", "not_found", err)
	_, err = sc.Sync().Reconcile(ctx, "contract-device-001", "op-1", brain.SyncReconcileRequest{Snapshot: "stale", Action: "discard"}, o)
	fails("sync reconcile stale", "conflict", err)
	_, err = sc.Sync().Reconcile(ctx, "contract-device-001", "op-1", brain.SyncReconcileRequest{Snapshot: "s", Action: "keep"}, o)
	fails("sync reconcile bad action", "invalid_request", err)
	_, err = sc.Sync().Reconcile(ctx, "unknown-device-0001", "op-1", brain.SyncReconcileRequest{Snapshot: "s", Action: "discard"}, o)
	fails("sync reconcile unknown device", "not_found", err)
	if diff != nil {
		_, err = sc.Sync().Reconcile(ctx, "contract-device-001", "op-1", brain.SyncReconcileRequest{Snapshot: diff.Snapshot, Action: "discard"}, o)
		ok("sync reconcile", err)
	}

	// Session views: unreachable runner, then a scripted runner on the bridge.
	_, err = sc.RemoteControl().SessionTail(ctx, "contract-runner", "ses-1", nil)
	fails("tail without bridge", "http_error", err)
	connectScriptedRunner(t, api, "contract-runner", scriptedResponse)
	_, err = sc.RemoteControl().SessionTail(ctx, "contract-runner", "ses-1", url.Values{"limit": {"2"}})
	ok("tail", err)
	_, err = sc.RemoteControl().SessionTail(ctx, "contract-runner", "ses-1", url.Values{"limit": {"500"}})
	fails("tail bad limit", "invalid_request", err)
	_, err = sc.RemoteControl().SessionDescendants(ctx, "contract-runner", "ses-parent", nil)
	ok("descendants", err)
	_, err = sc.RemoteControl().SessionDescendants(ctx, "contract-runner", "ses-parent", url.Values{"after": {"not-a-cursor"}})
	fails("descendants bad cursor", "invalid_request", err)

	// Supervision reads.
	_, err = sc.Supervision().Capabilities(ctx)
	ok("capabilities", err)
	_, err = sc.Supervision().Snapshot(ctx, url.Values{"project_id": {p}})
	ok("snapshot", err)
	_, err = sc.Supervision().Snapshot(ctx, url.Values{"project_id": {p}, "limit": {"1000"}})
	fails("snapshot bad limit", "invalid_request", err)
	_, err = sc.Supervision().DispatchPreview(ctx, url.Values{"project_id": {p}, "task_id": {task}})
	ok("preview", err)
	_, err = sc.Supervision().DispatchPreview(ctx, url.Values{"project_id": {p}, "task_id": {"zzzzzzzz"}})
	fails("preview unknown task", "invalid_request", err)

	// Checkpoints and budgets.
	chk := func(doc string) error {
		_, err := sc.Supervision().UpdateCheckpoint(ctx, brain.SupervisorCheckpointCommand(doc), o)
		return err
	}
	ok("checkpoint request", chk(`{"action":"request","expected_revision":0,"checkpoint":{"id":"chk-contract","project":"`+p+`","task_id":"`+task+`","artifact":"a1","question":"ship?"}}`))
	ok("checkpoint answer", chk(`{"action":"answer","expected_revision":1,"checkpoint":{"id":"chk-contract","project":"`+p+`","artifact":"a1","answer":"yes"}}`))
	fails("checkpoint stale", "conflict", chk(`{"action":"answer","expected_revision":1,"checkpoint":{"id":"chk-contract","project":"`+p+`","artifact":"a1","answer":"no"}}`))
	fails("checkpoint unknown field", "invalid_request", chk(`{"action":"request","checkpoint":{"id":"chk-contract"},"extra":1}`))
	_, err = sc.Supervision().Checkpoints(ctx, url.Values{"project": {p}})
	ok("checkpoints", err)
	_, err = sc.Supervision().Checkpoints(ctx, url.Values{"project": {p}, "id": {"chk-contract"}})
	ok("checkpoint versions", err)
	_, err = sc.Supervision().Checkpoints(ctx, url.Values{})
	fails("checkpoints without project", "invalid_request", err)
	bud := func(doc string) error {
		_, err := sc.Supervision().UpdateBudget(ctx, brain.ExecutionBudgetCommand(doc), o)
		return err
	}
	_, err = sc.Supervision().Budget(ctx, url.Values{"project": {p}, "id": {"bud-contract"}})
	fails("budget unknown", "not_found", err)
	ok("budget configure", bud(`{"action":"configure","expected_revision":0,"budget":{"id":"bud-contract","project":"`+p+`","timezone":"UTC","unit":"runs","limit":3}}`))
	ok("budget reserve", bud(`{"action":"reserve","budget":{"id":"bud-contract","project":"`+p+`"},"reservation_id":"res-1","units":1}`))
	ok("budget commit", bud(`{"action":"commit","budget":{"id":"bud-contract","project":"`+p+`"},"reservation_id":"res-1"}`))
	fails("budget over", "conflict", bud(`{"action":"reserve","budget":{"id":"bud-contract","project":"`+p+`"},"reservation_id":"res-2","units":99}`))
	fails("budget unknown action", "invalid_request", bud(`{"action":"spend","budget":{"id":"bud-contract","project":"`+p+`"}}`))
	_, err = sc.Supervision().Budget(ctx, url.Values{"project": {p}, "id": {"bud-contract"}})
	ok("budget", err)

	// Operations.
	op := func(doc string) error {
		_, err := sc.Supervision().SubmitOperation(ctx, brain.SupervisorOperationRequest(doc), o)
		return err
	}
	ok("operation trigger", op(`{"id":"op-contract-01","operation":"trigger","project":"`+p+`","task_id":"`+task+`"}`))
	ok("operation replay", op(`{"id":"op-contract-01","operation":"trigger","project":"`+p+`","task_id":"`+task+`"}`))
	fails("operation conflict", "conflict", op(`{"id":"op-contract-01","operation":"trigger","project":"`+p+`","task_id":"other"}`))
	fails("operation unknown field", "invalid_request", op(`{"id":"op-contract-02","operation":"trigger","bogus":true}`))
	ok("operation prompt", op(`{"id":"op-contract-03","operation":"prompt","runner_id":"contract-runner","instance_id":"ins-x","session_id":"ses-1","text":"hello"}`))
	_, err = sc.Supervision().GetOperation(ctx, "op-contract-01")
	ok("operation get", err)
	_, err = sc.Supervision().GetOperation(ctx, "op-nothing-1")
	fails("operation get unknown", "not_found", err)

	// Delivery command documents (tasks.verifyDelivery, raw form).
	_, err = sc.Tasks().SendDeliveryCommand(ctx, p, task, json.RawMessage(`{"action":"configure","expected_revision":0,"policy":{"required":"none"}}`), o)
	ok("delivery configure", err)
	_, err = sc.Tasks().SendDeliveryCommand(ctx, p, task, json.RawMessage(`{"action":"configure","expected_revison":0}`), o)
	fails("delivery unknown field", "invalid_request", err)

	seen := rec.contractReport(t)
	want := map[string][]int{
		"monitors.create": {201, 400, 409}, "monitors.deleteByScope": {200, 400, 404},
		"tasks.runnerCandidates": {200, 404}, "tasks.proposedRunnerCandidates": {200}, "features.runnerCandidates": {200},
		"clientContext.resolve": {200, 400},
		"sync.devices":          {200}, "sync.diff": {200, 404}, "sync.reconcile": {202, 400, 404, 409},
		"control.sessionTail": {200, 400, 502}, "control.sessionDescendants": {200, 400},
		"supervision.capabilities": {200}, "supervision.snapshot": {200, 400}, "supervision.dispatchPreview": {200, 400},
		"supervision.submitOperation": {200, 400, 409}, "supervision.getOperation": {200, 404},
		"supervision.checkpoints": {200, 400}, "supervision.updateCheckpoint": {200, 400, 409},
		"supervision.budget": {200, 404}, "supervision.updateBudget": {200, 400, 409},
		"tasks.verifyDelivery": {200, 400},
	}
	t.Logf("contract-checked live statuses: %v", seen)
	for op, statuses := range want {
		for _, status := range statuses {
			if !seen[op][status] {
				t.Errorf("%s: no live %d response was checked (saw %v)", op, status, seen[op])
			}
		}
	}
}
