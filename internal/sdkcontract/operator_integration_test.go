package sdkcontract_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

// exerciseOperatorSupervisionSDK runs the step-3 operations through the
// authenticated real handler with real storage: monitors, runner candidates,
// client context, sync reads, supervision ledgers and an idempotent trigger
// receipt. No runner is connected, so the session views stop at the bridge
// (502) and nothing executes anywhere.
func exerciseOperatorSupervisionSDK(t *testing.T, c *brain.Client) {
	t.Helper()
	ctx, o := context.Background(), brain.RequestOptions{}
	p, draft := "sdk-operator", "draft"
	created, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Operator fixture", Content: "x", Project: &p, Status: &draft}, o)
	if err != nil {
		t.Fatal(err)
	}
	code := func(err error) string {
		var be *brain.Error
		if errors.As(err, &be) {
			return be.Code
		}
		return ""
	}
	if m, err := c.Monitors().Create(ctx, brain.CreateMonitorRequest{TemplateId: "dream", ScopeType: "project", Project: &p}, o); err != nil || m.Id == "" {
		t.Fatalf("monitor create: %+v %v", m, err)
	}
	if _, err := c.Monitors().Create(ctx, brain.CreateMonitorRequest{TemplateId: "dream", ScopeType: "project", Project: &p}, o); code(err) != "conflict" {
		t.Fatalf("duplicate monitor: %v", err)
	}
	if d, err := c.Monitors().DeleteByScope(ctx, brain.DeleteMonitorByScopeRequest{TemplateId: "dream", Scope: brain.MonitorScope{Type: "project", Project: &p}}, o); err != nil || !d.Success {
		t.Fatalf("monitor delete: %+v %v", d, err)
	}
	if r, err := c.Tasks().RunnerCandidates(ctx, p, created.Id); err != nil || r.Candidates == nil || len(*r.Candidates) == 0 {
		t.Fatalf("task candidates: %+v %v", r, err)
	}
	opencode := "opencode"
	if _, err := c.Tasks().ProposedRunnerCandidates(ctx, p, brain.TaskRunnerCandidatesRequest{Executor: &opencode}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Features().RunnerCandidates(ctx, p, "none"); err != nil {
		t.Fatal(err)
	}
	if r, err := c.ClientContext().Resolve(ctx, brain.ResolveClientContextRequest{Client: brain.BrainClientInfo{ClientId: "sdk-client", HostId: "machine_00000042"}}, o); err != nil || r.ProjectId == "" {
		t.Fatalf("resolve: %+v %v", r, err)
	}
	if d, err := c.Sync().Devices(ctx); err != nil || d.StaleAfterSeconds != 35 {
		t.Fatalf("sync devices: %+v %v", d, err)
	}
	if _, err := c.Sync().Diff(ctx, "unreported-device-1", "op"); code(err) != "not_found" {
		t.Fatalf("sync diff: %v", err)
	}
	if _, err := c.RemoteControl().SessionTail(ctx, "sdk-fixture-runner", "ses", nil); code(err) != "http_error" {
		t.Fatalf("session tail without a connected runner: %v", err)
	}
	if _, err := c.RemoteControl().SessionDescendants(ctx, "sdk-fixture-runner", "ses", nil); code(err) != "http_error" {
		t.Fatalf("session descendants without a connected runner: %v", err)
	}
	if caps, err := c.Supervision().Capabilities(ctx); err != nil || len(caps.Tools) == 0 {
		t.Fatalf("capabilities: %+v %v", caps, err)
	}
	if s, err := c.Supervision().Snapshot(ctx, url.Values{"project_id": {p}}); err != nil || len(s.Tasks) != 1 {
		t.Fatalf("snapshot: %+v %v", s, err)
	}
	if _, err := c.Supervision().DispatchPreview(ctx, url.Values{"project_id": {p}, "task_id": {created.Id}}); err != nil {
		t.Fatal(err)
	}
	doc := json.RawMessage(`{"id":"sdk-op-000001","operation":"trigger","project":"` + p + `","task_id":"` + created.Id + `"}`)
	first, err := c.Supervision().SubmitOperation(ctx, doc, o)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := c.Supervision().SubmitOperation(ctx, doc, o); err != nil || again.State != first.State || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("idempotent replay: %+v %v", again, err)
	}
	if got, err := c.Supervision().GetOperation(ctx, "sdk-op-000001"); err != nil || got.Operation != "trigger" {
		t.Fatalf("operation receipt: %+v %v", got, err)
	}
	if _, err := c.Supervision().UpdateCheckpoint(ctx, json.RawMessage(`{"action":"request","checkpoint":{"id":"sdk-chk-001","project":"`+p+`","artifact":"a","question":"q?"}}`), o); err != nil {
		t.Fatal(err)
	}
	if l, err := c.Supervision().Checkpoints(ctx, url.Values{"project": {p}}); err != nil || l.Checkpoints == nil || len(*l.Checkpoints) != 1 {
		t.Fatalf("checkpoints: %+v %v", l, err)
	}
	if _, err := c.Supervision().UpdateBudget(ctx, json.RawMessage(`{"action":"configure","expected_revision":0,"budget":{"id":"sdk-bud","project":"`+p+`","timezone":"UTC","unit":"runs","limit":2}}`), o); err != nil {
		t.Fatal(err)
	}
	if b, err := c.Supervision().Budget(ctx, url.Values{"project": {p}, "id": {"sdk-bud"}}); err != nil || b.Remaining != 2 {
		t.Fatalf("budget: %+v %v", b, err)
	}
	if err := c.Entries().Delete(ctx, created.Id, false); err != nil {
		t.Fatal(err)
	}
	t.Log("real operator/supervision SDK: monitors, candidates, client context, sync reads, ledgers and an idempotent trigger receipt; session views refused at the unconnected bridge")
}
