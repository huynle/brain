package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/huynle/brain-api/internal/supervision"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupervisorToolsReuseAuthorizedREST(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-grant" {
			t.Errorf("unexpected request %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/control/runners/r/sessions/s/") {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "2" {
			t.Error("lost bounds")
		}
		_, _ = w.Write([]byte(`{"records":[],"next_cursor":"next"}`))
	}))
	defer api.Close()
	s := NewServer()
	RegisterSupervisorTools(s, NewAPIClient(api.URL).WithAuthToken("test-grant"))
	for _, name := range []string{"session_tail", "session_children"} {
		out, err := s.tools[name].handler(context.Background(), map[string]any{"runner_id": "r", "session_id": "s", "limit": float64(2)})
		if err != nil || !json.Valid([]byte(out)) {
			t.Fatal(out, err)
		}
	}
	h := NewHTTPHandler(NewAPIClient(api.URL))
	httpServer := h.serverFactory(NewAPIClient(api.URL))
	for _, name := range []string{"session_tail", "session_children"} {
		if _, ok := httpServer.tools[name]; !ok {
			t.Fatal("missing HTTP tool", name)
		}
	}
}

func TestSupervisorDiscoveryParityOverWire(t *testing.T) {
	s := NewServer()
	RegisterSupervisorTools(s, NewAPIClient("http://127.0.0.1"))
	var stdio bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &stdio); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	h := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	response := postMCP(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
	for name := range supervision.ToolCapabilities() {
		for transport, raw := range map[string][]byte{"stdio": stdio.Bytes(), "http": response.Body.Bytes()} {
			var result struct {
				Result struct {
					Tools []Tool `json:"tools"`
				} `json:"result"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, tool := range result.Result.Tools {
				if tool.Name == name {
					found = true
				}
			}
			if !found {
				t.Errorf("%s missing %s", transport, name)
			}
		}
	}
}
