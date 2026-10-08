package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Step-3 tools (task, feature, assignment, project, reader, sync and
// supervisor) under concurrency, covering every SDK helper path: typed
// decodes (sdkInto), verbatim pass-through (sdkRaw), command documents sent
// as given, a two-request tool (task_get) and queries. Each hosted-MCP request
// reaches the API with exactly its own bearer token (or none) on every request
// it makes, no X-Brain-* header leaks, and success and caller-specific error
// text belong to the calling request. Run under -race.
func TestHostedMCP_Step3ToolsForwardPerRequestToken(t *testing.T) {
	type seen struct{ auth, xbrain string }
	var mu sync.Mutex
	byCaller := map[string][]seen{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		path, _ := url.PathUnescape(r.URL.EscapedPath())
		id := callerIDRe.FindString(path + "?" + r.URL.RawQuery + " " + string(body))
		var leaked []string
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-brain-") {
				leaked = append(leaked, k)
			}
		}
		mu.Lock()
		byCaller[id] = append(byCaller[id], seen{r.Header.Get("Authorization"), strings.Join(leaked, ",")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var n int
		_, _ = fmt.Sscanf(id, "rid-%d", &n)
		if n%3 == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"Internal Server Error","message":"boom for %s"}`, id)
			return
		}
		const at = "2026-10-08T00:00:00Z"
		switch {
		case r.Method == http.MethodGet && path == "/api/v1/tasks/"+id:
			fmt.Fprintf(w, `{"tasks":[{"id":"x","title":"%s","status":"pending","classification":"ready","path":"projects/%s/task/x.md"}],"count":1}`, id, id)
		case strings.HasPrefix(path, "/api/v1/entries/"):
			fmt.Fprintf(w, `{"id":"x","path":%q,"title":"%s","type":"task","status":"pending","content":"body of %s"}`, strings.TrimPrefix(path, "/api/v1/entries/"), id, id)
		case strings.HasSuffix(path, "/status"):
			fmt.Fprintf(w, `{"tasks":[{"id":"x","title":"%s","status":"pending","priority":"high"}],"allCompleted":false}`, id)
		case path == "/api/v1/monitors":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":"m","path":"p","title":"%s"}`, id)
		case path == "/api/v1/monitors/by-scope":
			fmt.Fprintf(w, `{"success":true,"path":"p","taskId":"%s"}`, id)
		case strings.HasSuffix(path, "/resume-with-context"):
			fmt.Fprintf(w, `{"task_id":"x","project_id":"%s","resumed":true,"resume_mode":"rehydrate","injected_live":false}`, id)
		case strings.HasSuffix(path, "/assignment"):
			fmt.Fprintf(w, `{"project_id":"%s","feature_id":"f","source":"manual","status":"assigned"}`, id)
		case strings.HasSuffix(path, "/runner-candidates"):
			fmt.Fprintf(w, `{"project_id":"%s","candidates":[]}`, id)
		case path == "/api/v1/context/resolve":
			fmt.Fprintf(w, `{"project_id":"%s","confidence":"high","source":"fixture"}`, id)
		case strings.HasSuffix(path, "/placement"):
			fmt.Fprintf(w, `{"project_id":"%s","affinity":"soft"}`, id)
		case strings.HasSuffix(path, "/reconcile"):
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"command_id":"%s","status":"queued","note":"n"}`, id)
		case strings.HasSuffix(path, "/tail"):
			fmt.Fprintf(w, `{"records":[],"next_cursor":"%s","truncated":false,"cursor_expired":false}`, id)
		case path == "/api/v1/events/wait":
			fmt.Fprintf(w, `{"events":[],"next_cursor":"%s","cursor_expired":false,"timed_out":true,"shutdown":false,"truncated":false}`, id)
		case strings.HasSuffix(path, "/delivery"):
			fmt.Fprintf(w, `{"delivery":null,"unmet":["%s"]}`, id)
		case path == "/api/v1/supervision/operations":
			fmt.Fprintf(w, `{"id":"%s","operation":"trigger","state":"accepted","created_at":%q,"updated_at":%q,"detail":"d"}`, id, at, at)
		case path == "/api/v1/supervision/budgets":
			fmt.Fprintf(w, `{"state":"%s"}`, id)
		case path == "/api/v1/supervision/snapshot":
			fmt.Fprintf(w, `{"snapshot_started_at":%q,"snapshot_finished_at":%q,"atomic":false,"checkpoints":[],"checkpoint_coverage":"c","tasks":[],"truncated":false,"after_task":"","event_cursor":"%s","event_filters":{"project_id":"%s"},"unavailable_sources":[]}`, at, at, id, id)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":"Not Found","message":"stub has no route for %s %s"}`, r.Method, path)
		}
	}))
	defer api.Close()
	mcpSrv := httptest.NewServer(NewHTTPHandler(NewAPIClient(api.URL)))
	defer mcpSrv.Close()

	type call struct {
		tool     string
		args     func(id string) map[string]any
		requests int // API requests a successful call makes
	}
	calls := []call{
		{"tasks", func(id string) map[string]any { return map[string]any{"project": id} }, 1},
		{"task_get", func(id string) map[string]any { return map[string]any{"project": id, "task_id": "x"} }, 2},
		{"tasks_status", func(id string) map[string]any { return map[string]any{"project": id, "task_ids": []string{"x"}} }, 1},
		{"monitor_enable", func(id string) map[string]any {
			return map[string]any{"template_id": "blocked-inspector", "project": id, "feature_id": "f"}
		}, 1},
		{"dream_disable", func(id string) map[string]any { return map[string]any{"project": id} }, 1},
		{"resume_task_with_context", func(id string) map[string]any {
			return map[string]any{"project": id, "task_id": "x", "injected_context": "c"}
		}, 1},
		{"feature_assign", func(id string) map[string]any {
			return map[string]any{"project": id, "feature_id": "f", "runner_id": "r"}
		}, 1},
		{"runner_candidates", func(id string) map[string]any { return map[string]any{"project": id, "executor": "pi"} }, 1},
		{"context_resolve", func(id string) map[string]any { return map[string]any{"client_id": id, "host_id": "h"} }, 1},
		{"project_placement_put", func(id string) map[string]any { return map[string]any{"project": id, "affinity": "soft"} }, 1},
		{"reader_url", func(id string) map[string]any { return map[string]any{"path": "projects/" + id + "/note/a.md"} }, 1},
		{"sync_reconcile", func(id string) map[string]any {
			return map[string]any{"device_id": id, "operation_id": "o", "snapshot": "s", "action": "discard"}
		}, 1},
		{"session_tail", func(id string) map[string]any { return map[string]any{"runner_id": id, "session_id": "s"} }, 1},
		{"events_wait", func(id string) map[string]any { return map[string]any{"project": id, "timeout_ms": 0} }, 1},
		{"delivery_record", func(id string) map[string]any {
			return map[string]any{"project": id, "task_id": "x", "command": map[string]any{"action": "configure", "expected_revision": 0}}
		}, 1},
		{"supervisor_operation", func(id string) map[string]any {
			return map[string]any{"id": "op-" + id, "operation": "trigger", "project": id, "task_id": "x"}
		}, 1},
		{"execution_budget", func(id string) map[string]any {
			return map[string]any{"command": map[string]any{"action": "commit", "budget": map[string]any{"id": "b", "project": id}}}
		}, 1},
		{"supervisor_snapshot", func(id string) map[string]any { return map[string]any{"project": id} }, 1},
	}

	const n = 180
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("rid-%d", i)
			c := calls[i%len(calls)]
			payload, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": i, "method": "tools/call",
				"params": map[string]any{"name": c.tool, "arguments": c.args(id)},
			})
			req, _ := http.NewRequest(http.MethodPost, mcpSrv.URL+"/mcp", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			if i%4 != 0 {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer tok-%d", i))
			}
			req.Header.Set(HeaderBrainHostID, fmt.Sprintf("machine_%08x", i))
			req.Header.Set(HeaderBrainClientID, "client-x")
			req.Header.Set(HeaderBrainWorkdir, "/tmp/w")
			req.Header.Set("X-Brain-Tenant", "injected")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var rpc struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			_ = json.Unmarshal(raw, &rpc)
			text := ""
			if len(rpc.Result.Content) > 0 {
				text = rpc.Result.Content[0].Text
			}
			wantErr := i%3 == 1
			if rpc.Result.IsError != wantErr {
				errs <- fmt.Errorf("%s call %d: isError=%t text=%q", c.tool, i, rpc.Result.IsError, text)
				return
			}
			if wantErr && text != "Error: boom for "+id {
				errs <- fmt.Errorf("%s call %d: error text %q is not its own", c.tool, i, text)
			}
			named := callerIDRe.FindAllString(text, -1)
			if !wantErr && len(named) == 0 {
				errs <- fmt.Errorf("%s call %d: text %q does not name its own id", c.tool, i, text)
			}
			for _, other := range named {
				if other != id {
					errs <- fmt.Errorf("%s call %d: foreign id %s in %q", c.tool, i, other, text)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("rid-%d", i)
		want := calls[i%len(calls)].requests
		if i%3 == 1 {
			want = 1 // the first request fails
		}
		got := byCaller[id]
		if len(got) != want {
			t.Fatalf("caller %s (%s) reached the API %d times, want %d", id, calls[i%len(calls)].tool, len(got), want)
		}
		token := fmt.Sprintf("Bearer tok-%d", i)
		if i%4 == 0 {
			token = ""
		}
		for _, s := range got {
			if s.auth != token {
				t.Errorf("caller %s: API saw Authorization %q, want %q", id, s.auth, token)
			}
			if s.xbrain != "" {
				t.Errorf("caller %s: caller headers reached the API: %s", id, s.xbrain)
			}
		}
	}
	if unknown := byCaller[""]; len(unknown) != 0 {
		t.Fatalf("%d requests carried no caller id", len(unknown))
	}
}
