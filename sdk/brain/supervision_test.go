package brain_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestOperatorAndSupervisionRoutes(t *testing.T) {
	c, seen := recordingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing token on %s", r.URL)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/monitors":
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/api/v1/sync/devices/d%201/operations/o/reconcile" || r.URL.Path == "/api/v1/sync/devices/d 1/operations/o/reconcile":
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	ctx, o := context.Background(), brain.RequestOptions{}
	project, feature, empty, schedule, raw := "p", "f 1", "", "*/5 * * * *", ""
	caps := []string{"gpu"}
	labels := map[string]string{"zone": "lab"}
	var errs []error
	keep := func(_ any, err error) { errs = append(errs, err) }
	keep(c.Monitors().Create(ctx, brain.CreateMonitorRequest{TemplateId: "dream", ScopeType: "project", Project: &project, FeatureId: &empty, Schedule: &schedule}, o))
	keep(c.Monitors().DeleteByScope(ctx, brain.DeleteMonitorByScopeRequest{TemplateId: "dream", Scope: brain.MonitorScope{Type: "feature", Project: &project, FeatureId: &feature}}, o))
	keep(c.Tasks().RunnerCandidates(ctx, "p", "t/1"))
	keep(c.Tasks().ProposedRunnerCandidates(ctx, "p q", brain.TaskRunnerCandidatesRequest{RequiresCapability: &caps}))
	keep(c.Tasks().ProposedRunnerCandidates(ctx, "p", brain.TaskRunnerCandidatesRequest{}))
	keep(c.Features().RunnerCandidates(ctx, "p", "f/1"))
	keep(c.ClientContext().Resolve(ctx, brain.ResolveClientContextRequest{Client: brain.BrainClientInfo{ClientId: "c", HostId: "h", Labels: &labels}}, o))
	keep(c.Sync().Devices(ctx))
	keep(c.Sync().Diff(ctx, "d 1", "o/1"))
	keep(c.Sync().Reconcile(ctx, "d 1", "o", brain.SyncReconcileRequest{Snapshot: "s", Action: brain.SyncReconcileRequestAction("discard"), Raw: &raw}, o))
	keep(c.RemoteControl().SessionTail(ctx, "r 1", "s/1", url.Values{"after": {"c u"}, "limit": {"5"}, "max_bytes": {"2048"}}))
	keep(c.RemoteControl().SessionDescendants(ctx, "r", "s", url.Values{"limit": {"20"}}))
	keep(c.Supervision().Capabilities(ctx))
	keep(c.Supervision().Snapshot(ctx, url.Values{"project_id": {"p q"}, "limit": {"50"}}))
	keep(c.Supervision().DispatchPreview(ctx, url.Values{"project_id": {"p"}, "task_id": {"t"}, "manual": {"true"}}))
	keep(c.Supervision().SubmitOperation(ctx, brain.SupervisorOperationRequest(`{"id":"op-12345678","operation":"trigger","bogus":1}`), o))
	keep(c.Supervision().GetOperation(ctx, "a b/c"))
	keep(c.Supervision().Checkpoints(ctx, url.Values{"project": {"p"}, "id": {"chk 1"}}))
	keep(c.Supervision().UpdateCheckpoint(ctx, brain.SupervisorCheckpointCommand(`{"action":"request","checkpoint":{"id":"x"},"extra":true}`), o))
	keep(c.Supervision().Budget(ctx, url.Values{"project": {"p"}, "id": {"b"}}))
	keep(c.Supervision().UpdateBudget(ctx, brain.ExecutionBudgetCommand(`{"action":"reserve","units":1.5}`), o))
	keep(c.Tasks().SendDeliveryCommand(ctx, "p", "t", json.RawMessage(`{"action":"configure","expected_revison":0}`), o))
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	got := []string{}
	for _, s := range *seen {
		line := s.line
		if s.body != "" {
			line += " " + s.contentType + " " + s.body
		} else if s.contentType != "" {
			t.Errorf("%s sent a content type without a body", s.line)
		}
		got = append(got, line)
	}
	want := []string{
		`POST /api/v1/monitors application/json {"feature_id":"","project":"p","schedule":"*/5 * * * *","scope_type":"project","template_id":"dream"}`,
		`DELETE /api/v1/monitors/by-scope application/json {"scope":{"feature_id":"f 1","project":"p","type":"feature"},"templateId":"dream"}`,
		`GET /api/v1/tasks/p/t%2F1/runner-candidates`,
		`POST /api/v1/tasks/p%20q/runner-candidates application/json {"requires_capability":["gpu"]}`,
		`POST /api/v1/tasks/p/runner-candidates application/json {}`,
		`GET /api/v1/tasks/p/features/f%2F1/runner-candidates`,
		`POST /api/v1/context/resolve application/json {"client":{"client_id":"c","host_id":"h","labels":{"zone":"lab"}},"workspace":{"path":""}}`,
		`GET /api/v1/sync/devices`,
		`GET /api/v1/sync/devices/d%201/operations/o%2F1/diff`,
		`POST /api/v1/sync/devices/d%201/operations/o/reconcile application/json {"action":"discard","raw":"","snapshot":"s"}`,
		`GET /api/v1/control/runners/r%201/sessions/s%2F1/tail?after=c+u&limit=5&max_bytes=2048`,
		`GET /api/v1/control/runners/r/sessions/s/descendants?limit=20`,
		`GET /api/v1/supervision/capabilities`,
		`GET /api/v1/supervision/snapshot?limit=50&project_id=p+q`,
		`GET /api/v1/supervision/dispatch-preview?manual=true&project_id=p&task_id=t`,
		// Command documents are sent verbatim: unknown fields and wrong types
		// reach the server, which is the only validator.
		`POST /api/v1/supervision/operations application/json {"id":"op-12345678","operation":"trigger","bogus":1}`,
		`GET /api/v1/supervision/operations/a%20b%2Fc`,
		`GET /api/v1/supervision/checkpoints?id=chk+1&project=p`,
		`POST /api/v1/supervision/checkpoints application/json {"action":"request","checkpoint":{"id":"x"},"extra":true}`,
		`GET /api/v1/supervision/budgets?id=b&project=p`,
		`POST /api/v1/supervision/budgets application/json {"action":"reserve","units":1.5}`,
		`POST /api/v1/tasks/p/t/delivery application/json {"action":"configure","expected_revison":0}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes:\n%q\nwant:\n%q", got, want)
	}
}

func TestCommandDocumentsAreRequiredAndValidJSON(t *testing.T) {
	c, seen := recordingClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	ctx, o := context.Background(), brain.RequestOptions{}
	for name, call := range map[string]func() error{
		"operation nil":   func() error { _, err := c.Supervision().SubmitOperation(ctx, nil, o); return err },
		"checkpoint nil":  func() error { _, err := c.Supervision().UpdateCheckpoint(ctx, nil, o); return err },
		"budget invalid":  func() error { _, err := c.Supervision().UpdateBudget(ctx, brain.ExecutionBudgetCommand(`{"action":`), o); return err },
		"delivery empty":  func() error { _, err := c.Tasks().SendDeliveryCommand(ctx, "p", "t", json.RawMessage{}, o); return err },
		"delivery broken": func() error { _, err := c.Tasks().SendDeliveryCommand(ctx, "p", "t", json.RawMessage(`nope`), o); return err },
	} {
		var be *brain.Error
		if err := call(); !errors.As(err, &be) || be.Code != "invalid_request" {
			t.Errorf("%s: %v, want invalid_request", name, err)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("refused documents reached the server: %v", *seen)
	}
}

func TestSupervisionResponsesAndErrorsDecode(t *testing.T) {
	c, _ := recordingClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/supervision/budgets":
			_, _ = w.Write([]byte(`{"budget":{"id":"b","project":"p","timezone":"UTC","unit":"runs","limit":5,"revision":1},"consumed_and_reserved":2,"enforcement":"e","monetary_cost":null,"remaining":3,"token_usage":null,"window":"2026-10-08"}`))
		case "/api/v1/control/runners/r/sessions/s/tail":
			_, _ = w.Write([]byte(`{"records":[{"id":"m:p","message_id":"m","kind":"text","text":"hi","time":{"start":1}}],"next_cursor":"c","truncated":false,"cursor_expired":false}`))
		case "/api/v1/sync/devices/d/operations/o/reconcile":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"Stale diff","message":"fetch a fresh diff before reconciling"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not Found","message":"operation not found"}`))
		}
	})
	ctx := context.Background()
	budget, err := c.Supervision().Budget(ctx, url.Values{"project": {"p"}, "id": {"b"}})
	if err != nil || budget.Remaining != 3 || budget.Budget.Limit != 5 || budget.TokenUsage != nil {
		t.Fatalf("budget=%+v err=%v", budget, err)
	}
	page, err := c.RemoteControl().SessionTail(ctx, "r", "s", nil)
	if err != nil || page.Records == nil || len(*page.Records) != 1 || (*page.Records)[0].Text == nil || *(*page.Records)[0].Text != "hi" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	_, err = c.Sync().Reconcile(ctx, "d", "o", brain.SyncReconcileRequest{Snapshot: "s", Action: "discard"}, brain.RequestOptions{})
	var be *brain.Error
	if !errors.As(err, &be) || be.Code != "conflict" || be.Status != 409 || be.Message != "fetch a fresh diff before reconciling" {
		t.Fatalf("reconcile conflict: %#v", err)
	}
	if _, err := c.Supervision().GetOperation(ctx, "op-missing-1"); !errors.As(err, &be) || be.Code != "not_found" {
		t.Fatalf("missing operation: %v", err)
	}
	if _, err := c.Supervision().GetOperation(ctx, ".."); !errors.As(err, &be) || be.Code != "invalid_request" {
		t.Fatalf("traversal id: %v", err)
	}
}
