package brain_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

type seenRequest struct{ line, contentType, body string }

func recordingClient(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*brain.Client, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, seenRequest{r.Method + " " + r.URL.RequestURI(), r.Header.Get("Content-Type"), string(b)})
		respond(w, r)
	}, brain.Config{Token: "tok"})
	return c, &seen
}

func TestRunnerDispatchSchedulerRoutes(t *testing.T) {
	c, seen := recordingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing token on %s", r.URL)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	ctx, o := context.Background(), brain.RequestOptions{}
	var errs []error
	keep := func(_ any, err error) { errs = append(errs, err) }
	keep(c.Runners().Status(ctx))
	keep(c.Runners().List(ctx))
	keep(c.Runners().Get(ctx, "r 1/x"))
	keep(c.Runners().Instances(ctx, "r1"))
	keep(c.Runners().AllInstances(ctx))
	keep(c.Dispatch().PauseAll(ctx, o))
	keep(c.Dispatch().ResumeAll(ctx, o))
	keep(c.Dispatch().PauseProject(ctx, "p q", o))
	keep(c.Dispatch().ResumeProject(ctx, "p", o))
	keep(c.Dispatch().PauseFeature(ctx, "p", "f/1", o))
	keep(c.Dispatch().ResumeFeature(ctx, "p", "f1", o))
	keep(c.Dispatch().PauseProjectAutomations(ctx, "p", o))
	keep(c.Dispatch().ResumeProjectAutomations(ctx, "p", o))
	keep(c.Tasks().DispatchLease(ctx, "p", "t"))
	keep(c.Tasks().PlacementReasons(ctx, "p", "t/2"))
	keep(c.Scheduler().Status(ctx))
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	var lines []string
	for _, s := range *seen {
		lines = append(lines, s.line)
		if s.body != "" || s.contentType != "" {
			t.Errorf("%s sent a body/content type (%q %q); these handlers read none", s.line, s.contentType, s.body)
		}
	}
	want := []string{
		"GET /api/v1/tasks/runner/status", "GET /api/v1/runners", "GET /api/v1/runners/r%201%2Fx", "GET /api/v1/runners/r1/instances", "GET /api/v1/instances",
		"POST /api/v1/tasks/runner/pause", "POST /api/v1/tasks/runner/resume", "POST /api/v1/tasks/runner/pause/p%20q", "POST /api/v1/tasks/runner/resume/p",
		"POST /api/v1/tasks/runner/features/pause/p/f%2F1", "POST /api/v1/tasks/runner/features/resume/p/f1",
		"POST /api/v1/tasks/runner/automations/pause/p", "POST /api/v1/tasks/runner/automations/resume/p",
		"GET /api/v1/tasks/p/t/dispatch-lease", "GET /api/v1/tasks/p/t%2F2/placement-reasons", "GET /api/v1/scheduler/status",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("routes:\n%v\nwant:\n%v", lines, want)
	}
}

func TestControlRoutesBodiesAndProxiedResponses(t *testing.T) {
	c, seen := recordingClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/abort"), strings.Contains(r.URL.Path, "/permissions/"):
			_, _ = w.Write([]byte(`true`))
		case r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"success":true,"instance":{"instance_id":"i9","runner_id":"r","kind":"adhoc","status":"starting"}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	})
	ctx, o := context.Background(), brain.RequestOptions{}
	text, agent := "hi", "build"
	raw, err := c.RemoteControl().SendPrompt(ctx, "r", "i", "s/1", brain.ControlPromptRequest{Text: &text, Agent: &agent, Model: &brain.ControlPromptModel{ProviderID: "p", ModelID: "m"}}, o)
	if err != nil || len(raw) != 0 {
		t.Fatalf("prompt: %q %v", raw, err)
	}
	raw, err = c.RemoteControl().AbortSession(ctx, "r", "i", "s", o)
	if err != nil || string(raw) != "true" {
		t.Fatalf("abort: %q %v", raw, err)
	}
	raw, err = c.RemoteControl().RespondPermission(ctx, "r", "i", "s", "per 1", brain.ControlPermissionRequest{Response: brain.PermissionReject}, o)
	if err != nil || string(raw) != "true" {
		t.Fatalf("permission: %q %v", raw, err)
	}
	title := "t"
	spawned, err := c.RemoteControl().SpawnInstance(ctx, "r", brain.SpawnInstanceSpec{Workdir: "/w", Title: &title}, o)
	if err != nil || !spawned.Success || spawned.Instance.InstanceId != "i9" {
		t.Fatalf("spawn: %+v %v", spawned, err)
	}
	killed, err := c.RemoteControl().KillInstance(ctx, "r", "i9", o)
	if err != nil || !killed.Success {
		t.Fatalf("kill: %+v %v", killed, err)
	}
	want := []seenRequest{
		{"POST /api/v1/control/runners/r/instances/i/sessions/s%2F1/prompt", "application/json", `{"agent":"build","model":{"modelID":"m","providerID":"p"},"text":"hi"}`},
		{"POST /api/v1/control/runners/r/instances/i/sessions/s/abort", "", ""},
		{"POST /api/v1/control/runners/r/instances/i/sessions/s/permissions/per%201", "application/json", `{"response":"reject"}`},
		{"POST /api/v1/control/runners/r/instances", "application/json", `{"title":"t","workdir":"/w"}`},
		{"DELETE /api/v1/control/runners/r/instances/i9", "", ""},
	}
	if !reflect.DeepEqual(*seen, want) {
		t.Fatalf("requests:\n%+v\nwant:\n%+v", *seen, want)
	}
}

func TestControlProxiedResponseFailures(t *testing.T) {
	status, body := 200, "not json"
	c, _ := recordingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	ctx := context.Background()
	var e *brain.Error
	if _, err := c.RemoteControl().AbortSession(ctx, "r", "i", "s", brain.RequestOptions{}); !errors.As(err, &e) || e.Code != "invalid_response" {
		t.Fatalf("non-JSON proxied body: %v", err)
	}
	status, body = 502, `{"error":"Bad Gateway","message":"runner bridge not connected"}`
	if _, err := c.RemoteControl().SendPrompt(ctx, "r", "i", "s", brain.ControlPromptRequest{}, brain.RequestOptions{}); !errors.As(err, &e) || e.Message != "runner bridge not connected" || e.Status != 502 {
		t.Fatalf("bridge error: %v", err)
	}
	status, body = 409, `{"error":"Conflict","message":"task instances are owned by the task lifecycle"}`
	if _, err := c.RemoteControl().KillInstance(ctx, "r", "t", brain.RequestOptions{}); !errors.As(err, &e) || e.Code != "conflict" {
		t.Fatalf("kill conflict: %v", err)
	}
	for _, id := range []string{"..", "%2e%2e"} {
		if _, err := c.RemoteControl().KillInstance(ctx, "r", id, brain.RequestOptions{}); !errors.As(err, &e) || e.Code != "invalid_request" {
			t.Fatalf("dot segment %q: %v", id, err)
		}
	}
}

// Snooze timestamps travel as the caller's text: the server validates and
// owns the error message (TS already typed these as strings).
func TestSnoozeForwardsCallerTimestampVerbatim(t *testing.T) {
	c, seen := recordingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/reminders/") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"Bad Request","message":"remind_at \"later\" is not RFC3339 with an offset"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := context.Background()
	var e *brain.Error
	if _, err := c.Reminders().Snooze(ctx, "r", brain.SnoozeReminderRequest{RemindAt: "later"}, brain.RequestOptions{}); !errors.As(err, &e) || !strings.Contains(e.Message, `"later" is not RFC3339`) {
		t.Fatalf("server validation message lost: %v", err)
	}
	for _, v := range []string{"2031-01-01T00:00:00.500-05:00", ""} {
		if _, err := c.Attention().Snooze(ctx, "a", brain.SnoozeAttentionRequest{SnoozedUntil: v}, brain.RequestOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	got := []string{(*seen)[0].body, (*seen)[1].body, (*seen)[2].body}
	want := []string{`{"remind_at":"later"}`, `{"snoozed_until":"2031-01-01T00:00:00.500-05:00"}`, `{"snoozed_until":""}`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bodies=%v", got)
	}
}
