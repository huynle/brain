package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var callerIDRe = regexp.MustCompile(`rid-\d+\b`)

// Step-2 tools (runner, dispatch dials, remote control, scheduler views,
// snooze) under concurrency: each hosted-MCP request reaches the API with
// exactly its own bearer token (or none), no X-Brain-* header leaks, and both
// success text and caller-specific error text belong to the calling request.
// Run under -race.
func TestHostedMCP_RunnerControlToolsForwardPerRequestToken(t *testing.T) {
	type seen struct{ auth, xbrain string }
	var mu sync.Mutex
	byCaller := map[string][]seen{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		id := callerIDRe.FindString(r.URL.Path)
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
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/runners/"):
			fmt.Fprintf(w, `{"runner_id":%q,"hostname":"h","max_parallel":1,"registered_at":"","last_heartbeat":"","status":"online"}`, id)
		case strings.HasSuffix(r.URL.Path, "/dispatch-lease"):
			fmt.Fprintf(w, `{"leaseId":"l","project_id":%q,"task_id":"t","assigned_runner_id":"","assigned_machine_id":"","state":"pushed","pushed_at":1,"expires_at":2}`, id)
		case strings.HasSuffix(r.URL.Path, "/placement-reasons"):
			_, _ = w.Write([]byte(`{"reasons":[],"total":0}`))
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/instances"):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"success":true,"instance":{"instance_id":"i-%s","runner_id":%q,"kind":"adhoc","status":"starting"}}`, id, id)
		case strings.HasPrefix(r.URL.Path, "/api/v1/reminders/"):
			fmt.Fprintf(w, `{"reminder_id":%q,"title":%q,"state":"armed","status":"active","action":"notify","entry_id":"e"}`, id, id)
		case strings.HasPrefix(r.URL.Path, "/api/v1/attention/"):
			fmt.Fprintf(w, `{"id":%q,"title":%q,"recipient":"r","kind":"k","severity":"info","state":"snoozed"}`, id, id)
		default:
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	}))
	defer api.Close()
	mcpSrv := httptest.NewServer(NewHTTPHandler(NewAPIClient(api.URL)))
	defer mcpSrv.Close()

	calls := []func(id string) (string, map[string]any){
		func(id string) (string, map[string]any) { return "runner_get", map[string]any{"runner_id": id} },
		func(id string) (string, map[string]any) {
			return "runner_pause_project", map[string]any{"project": id}
		},
		func(id string) (string, map[string]any) {
			return "runner_resume_feature", map[string]any{"project": id, "feature_id": "f"}
		},
		func(id string) (string, map[string]any) {
			return "control_send_prompt", map[string]any{"runner_id": id, "instance_id": "i", "session_id": "s", "text": "t"}
		},
		func(id string) (string, map[string]any) {
			return "control_spawn_instance", map[string]any{"runner_id": id, "workdir": "/w"}
		},
		func(id string) (string, map[string]any) {
			return "control_kill_instance", map[string]any{"runner_id": id, "instance_id": "i", "confirm": true}
		},
		func(id string) (string, map[string]any) {
			return "task_dispatch_lease", map[string]any{"project": id, "task_id": "t"}
		},
		func(id string) (string, map[string]any) {
			return "task_placement_reasons", map[string]any{"project": id, "task_id": "t"}
		},
		func(id string) (string, map[string]any) {
			return "reminder_snooze", map[string]any{"reminder_id": id, "remind_at": "2031-01-01T00:00:00Z"}
		},
		func(id string) (string, map[string]any) {
			return "attention_snooze", map[string]any{"id": id, "snoozed_until": "later"}
		},
	}

	const n = 120
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("rid-%d", i)
			name, args := calls[i%len(calls)](id)
			payload, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": i, "method": "tools/call",
				"params": map[string]any{"name": name, "arguments": args},
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
				errs <- fmt.Errorf("%s call %d: isError=%t text=%q", name, i, rpc.Result.IsError, text)
				return
			}
			if wantErr && text != "Error: boom for "+id {
				errs <- fmt.Errorf("%s call %d: error text %q is not its own", name, i, text)
			}
			named := callerIDRe.FindAllString(text, -1)
			if !wantErr && len(named) == 0 && name != "task_placement_reasons" {
				errs <- fmt.Errorf("%s call %d: text %q does not name its own id", name, i, text)
			}
			for _, other := range named {
				if other != id {
					errs <- fmt.Errorf("%s call %d: foreign id %s in %q", name, i, other, text)
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
		got := byCaller[id]
		if len(got) != 1 {
			t.Fatalf("caller %s reached the API %d times", id, len(got))
		}
		want := fmt.Sprintf("Bearer tok-%d", i)
		if i%4 == 0 {
			want = ""
		}
		if got[0].auth != want {
			t.Errorf("caller %s: API saw Authorization %q, want %q", id, got[0].auth, want)
		}
		if got[0].xbrain != "" {
			t.Errorf("caller %s: caller headers reached the API: %s", id, got[0].xbrain)
		}
	}
}
