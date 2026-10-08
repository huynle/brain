package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/mcp"
	"github.com/huynle/brain-api/internal/sdkcontract"
)

// requestRecorder is a reverse proxy between the hosted MCP and the real
// API that records, per tool call, exactly what the MCP sent: method, path,
// raw query, canonical JSON body and the headers that carry meaning
// (credential, content negotiation, and any X-Brain-* caller header, which
// must never reach the API).
type requestRecorder struct {
	mu   sync.Mutex
	reqs []string
	// contract, when set, checks every real API response of an operation
	// selected by checked against the public contract (see contractCheck).
	contract   *sdkcontract.ResponseChecker
	checked    func(op string) bool
	violations []string
	seen       map[string]map[int]bool // operation -> statuses observed
}

// contractCheck makes the recorder validate the real responses of the
// operations selected by checked against api/openapi.yaml.
func (r *requestRecorder) contractCheck(t *testing.T, checked func(op string) bool) {
	t.Helper()
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if r.contract, err = sdkcontract.NewResponseChecker(data); err != nil {
		t.Fatal(err)
	}
	r.checked, r.seen = checked, map[string]map[int]bool{}
}

// capturingWriter keeps a copy of the status and body the proxy relays.
type capturingWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *capturingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *capturingWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (r *requestRecorder) handler(target string) http.Handler {
	u, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(u)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
		var leaked []string
		for k := range req.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-brain-") {
				leaked = append(leaked, k)
			}
		}
		sort.Strings(leaked)
		line := fmt.Sprintf("%s %s", req.Method, req.URL.EscapedPath())
		if req.URL.RawQuery != "" {
			line += "?" + req.URL.RawQuery
		}
		line += fmt.Sprintf("\n    auth=%q content-type=%q accept=%q x-brain=%q body=%s",
			req.Header.Get("Authorization"), req.Header.Get("Content-Type"), req.Header.Get("Accept"),
			strings.Join(leaked, ","), canonicalJSON(body))
		r.mu.Lock()
		r.reqs = append(r.reqs, line)
		r.mu.Unlock()
		if r.contract == nil {
			proxy.ServeHTTP(w, req)
			return
		}
		cw := &capturingWriter{ResponseWriter: w}
		proxy.ServeHTTP(cw, req)
		if cw.status == 0 {
			cw.status = http.StatusOK
		}
		path := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v1")
		if op := r.contract.Operation(req.Method, path); op != "" && r.checked(op) {
			_, err := r.contract.Check(req.Method, path, cw.status, cw.body.Bytes())
			r.mu.Lock()
			if r.seen[op] == nil {
				r.seen[op] = map[int]bool{}
			}
			r.seen[op][cw.status] = true
			if err != nil {
				r.violations = append(r.violations, err.Error())
			}
			r.mu.Unlock()
		}
	})
}

// contractReport fails on any contract violation and returns, per checked
// operation, the statuses seen (for coverage assertions).
func (r *requestRecorder) contractReport(t *testing.T) map[string]map[int]bool {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.violations {
		t.Errorf("live response violates the public contract: %s", v)
	}
	return r.seen
}

func (r *requestRecorder) drain() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.reqs
	r.reqs = nil
	return out
}

func canonicalJSON(b []byte) string {
	if len(b) == 0 {
		return "<none>"
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Sprintf("<non-json %q>", b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// callWithHeaders invokes a tool over real HTTP with a caller bearer token
// and X-Brain-* origin headers, returning OK/ERROR and the tool text.
func callWithHeaders(t *testing.T, mcpURL, token, name string, args map[string]any) (bool, string) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	req, _ := http.NewRequest(http.MethodPost, mcpURL, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(mcp.HeaderBrainHostID, "machine_00000001")
	req.Header.Set(mcp.HeaderBrainClientID, "parity")
	req.Header.Set(mcp.HeaderBrainWorkdir, "/tmp/parity")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		t.Fatal(err)
	}
	text := ""
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	return !rpc.Result.IsError, text
}

type parityCall struct {
	tool string
	args map[string]any
}

// TestRequestParity_RunnerControlSchedulerSnooze records the exact REST
// requests each tool sends. The transcript was recorded on the legacy client
// and is compared, line for line, after the move to the SDK.
func TestRequestParity_RunnerControlSchedulerSnooze(t *testing.T) {
	api := dedicatedAPI(t)
	rec := &requestRecorder{}
	proxy := httptest.NewServer(rec.handler(api))
	t.Cleanup(proxy.Close)
	mcpSrv := httptest.NewServer(mcp.NewHTTPHandler(mcp.NewAPIClient(proxy.URL)))
	t.Cleanup(mcpSrv.Close)

	g := newGoldenAt(t, "request_parity", api)
	g.keepTimes = true
	taskID := seedTask(t, api, "parity", "Parity task")
	g.bind("TASK_ID", taskID)
	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{"runner_id": "parity-runner", "hostname": "h"})
	apiDo(t, "PUT", api+"/api/v1/runners/parity-runner/instances/ins-1", map[string]any{"kind": "adhoc", "status": "idle", "started_at": 1, "last_seen": 1})
	apiDo(t, "PUT", api+"/api/v1/runners/parity-runner/instances/ins-task", map[string]any{"kind": "task", "status": "busy", "started_at": 2, "last_seen": 2})
	connectFakeRunner(t, api, "parity-runner")

	reminder := createJSON(t, api+"/api/v1/reminders", map[string]any{"title": "Parity reminder", "project": "parity"}, "reminder_id")
	g.bind("REMINDER_ID", reminder)
	attention := createJSON(t, api+"/api/v1/attention", map[string]any{"title": "Parity item", "kind": "custom", "project": "parity"}, "id")
	g.bind("ATTENTION_ID", attention)

	session := map[string]any{"runner_id": "parity-runner", "instance_id": "ins-1", "session_id": "ses/1"}
	merge := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range session {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	// Spawns are rate limited per caller (6/min, process-wide, and every
	// caller here is anonymous to the auth-less server), so this transcript
	// and the golden spawn only 3 times per run; -count<=2 stays under it.
	calls := []parityCall{
		{"runner_status", map[string]any{"project": "parity"}},
		{"runners", map[string]any{"status": "online", "limit": 5}},
		{"runner_get", map[string]any{"runner_id": "parity-runner"}},
		{"runner_get", map[string]any{"runner_id": "a b/c"}},
		{"runner_instances", map[string]any{"runner_id": "parity-runner", "kind": "adhoc"}},
		{"runner_instances_all", map[string]any{"status": "idle"}},
		{"runner_pause_project", map[string]any{"project": "parity"}},
		{"runner_resume_project", map[string]any{"project": "parity"}},
		{"runner_pause_feature", map[string]any{"project": "parity", "feature_id": "f 1/x"}},
		{"runner_resume_feature", map[string]any{"project": "parity", "feature_id": "f 1/x"}},
		{"runner_pause_project_automations", map[string]any{"project": "parity"}},
		{"runner_resume_project_automations", map[string]any{"project": "parity"}},
		{"runner_pause_all", map[string]any{"confirm": true}},
		{"runner_resume_all", map[string]any{"confirm": true}},
		{"control_send_prompt", merge(map[string]any{"text": " hi ", "agent": "build", "provider_id": "p", "model_id": "m"})},
		{"control_send_prompt", merge(map[string]any{"text": "plain"})},
		{"control_abort_session", session},
		{"control_permission", merge(map[string]any{"permission_id": "per 1", "response": "reject"})},
		{"control_spawn_instance", map[string]any{"runner_id": "parity-runner", "workdir": "/srv/q", "agent": "a", "model": "m", "title": "t"}},
		{"control_kill_instance", map[string]any{"runner_id": "parity-runner", "instance_id": "ins-spawned", "confirm": true}},
		{"control_kill_instance", map[string]any{"runner_id": "parity-runner", "instance_id": "ins-task", "confirm": true}},
		{"task_dispatch_lease", map[string]any{"project": "parity", "task_id": taskID}},
		{"task_placement_reasons", map[string]any{"project": "parity", "task_id": taskID}},
		{"task_placement_reasons", map[string]any{"project": "pa rity", "task_id": "x/y"}},
		{"scheduler_status", nil},
		{"reminder_snooze", map[string]any{"reminder_id": reminder, "remind_at": " 2031-03-04T07:06:07.250+02:00 "}},
		{"reminder_snooze", map[string]any{"reminder_id": reminder, "remind_at": "later"}},
		{"reminder_snooze", map[string]any{"reminder_id": "a/b", "remind_at": "2031-03-04T05:06:07Z"}},
		{"attention_snooze", map[string]any{"id": attention, "snoozed_until": "2031-01-01T00:00:00.500-05:00"}},
		{"attention_snooze", map[string]any{"id": attention, "snoozed_until": "later"}},
		{"attention_snooze", map[string]any{"id": attention}},
	}
	for i, c := range calls {
		ok, text := callWithHeaders(t, mcpSrv.URL+"/mcp", fmt.Sprintf("probe-%02d", i), c.tool, c.args)
		status := "OK"
		if !ok {
			status = "ERROR"
		}
		first, _, _ := strings.Cut(text, "\n")
		g.note("=== %s %s -> %s: %s", c.tool, canonicalJSON(mustJSON(c.args)), status, first)
		for _, r := range rec.drain() {
			g.note("  %s", r)
		}
	}
	g.check()
}

func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func createJSON(t *testing.T, url string, body map[string]any, idField string) string {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	id, _ := out[idField].(string)
	if id == "" {
		t.Fatalf("create %s: status %d body %v", url, resp.StatusCode, out)
	}
	return id
}
