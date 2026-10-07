package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Concurrent hosted-MCP requests with different bearer tokens must each reach
// the API with their own token and nothing else: no cross-request leakage of
// the per-request SDK binding, and caller X-Brain-* headers (origin hints for
// the MCP layer) never forwarded to the REST API. Run under -race.
func TestHostedMCP_SDKForwardsPerRequestTokenWithoutLeakage(t *testing.T) {
	type seen struct{ auth, xbrain string }
	var mu sync.Mutex
	byPath := map[string][]seen{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var leaked []string
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-brain-") {
				leaked = append(leaked, k)
			}
		}
		mu.Lock()
		byPath[r.URL.Path] = append(byPath[r.URL.Path], seen{r.Header.Get("Authorization"), strings.Join(leaked, ",")})
		mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/reminders/")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"reminder_id":%q,"title":"t","state":"armed","status":"active","action":"notify","entry_id":"e"}`, id)
	}))
	defer api.Close()
	mcpSrv := httptest.NewServer(NewHTTPHandler(NewAPIClient(api.URL)))
	defer mcpSrv.Close()

	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": i, "method": "tools/call",
				"params": map[string]any{"name": "reminder_get", "arguments": map[string]any{"reminder_id": fmt.Sprintf("rid-%d", i)}},
			})
			req, _ := http.NewRequest(http.MethodPost, mcpSrv.URL+"/mcp", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			if i%5 != 0 { // every fifth caller is anonymous
				req.Header.Set("Authorization", fmt.Sprintf("Bearer tok-%d", i))
			}
			req.Header.Set(HeaderBrainHostID, fmt.Sprintf("machine_%08x", i))
			req.Header.Set(HeaderBrainClientID, "client-x")
			req.Header.Set(HeaderBrainWorkdir, "/tmp/w")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(raw), fmt.Sprintf("rid-%d", i)) || strings.Contains(string(raw), `"isError":true`) {
				errs <- fmt.Errorf("call %d: unexpected response %s", i, raw)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for i := 0; i < n; i++ {
		got := byPath[fmt.Sprintf("/api/v1/reminders/rid-%d", i)]
		if len(got) != 1 {
			t.Fatalf("call %d reached the API %d times", i, len(got))
		}
		want := fmt.Sprintf("Bearer tok-%d", i)
		if i%5 == 0 {
			want = ""
		}
		if got[0].auth != want {
			t.Errorf("call %d: API saw Authorization %q, want %q", i, got[0].auth, want)
		}
		if got[0].xbrain != "" {
			t.Errorf("call %d: caller headers reached the API: %s", i, got[0].xbrain)
		}
	}
}
