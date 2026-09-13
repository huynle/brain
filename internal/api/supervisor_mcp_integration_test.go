package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/mcp"
	"github.com/huynle/brain-api/internal/mcpserver"
	"github.com/huynle/brain-api/internal/supervision"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Real stdio and HTTP MCP encoders call real REST handlers. The executor bridge
// is controlled: this verifies transport contracts, not a live LLM installation.
func TestSupervisorMCPTransportsToREST(t *testing.T) {
	h := &Handler{bridge: &mockBridgeService{historyOut: []byte(`[{"info":{"id":"m","role":"assistant"},"parts":[{"id":"p","type":"text","text":"visible result"},{"id":"r","type":"reasoning","text":"private reasoning"}]}]`), childrenOut: []byte(`[]`)}, events: &mockEventService{}}
	router := chi.NewRouter()
	router.Get("/api/v1/control/runners/{runnerId}/sessions/{sessionId}/tail", h.HandleControlSessionTail)
	router.Get("/api/v1/control/runners/{runnerId}/sessions/{sessionId}/descendants", h.HandleControlSessionDescendants)
	router.Get("/api/v1/events/wait", h.HandleEventWait)
	apiServer := httptest.NewServer(router)
	defer apiServer.Close()
	requests := []string{`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_tail","arguments":{"runner_id":"r","session_id":"s","limit":1,"max_bytes":2048}}}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"session_children","arguments":{"runner_id":"r","session_id":"s"}}}`, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"events_wait","arguments":{"project":"p","timeout_ms":0}}}`}
	var stdio bytes.Buffer
	if err := mcpserver.RunMCPServer(context.Background(), mcpserver.MCPOptions{APIURL: apiServer.URL}, strings.NewReader(strings.Join(requests, "\n")+"\n"), &stdio); err != nil {
		t.Fatal(err)
	}
	httpHandler := mcp.NewHTTPHandler(mcp.NewAPIClient(apiServer.URL))
	httpResponses := []string{}
	for _, request := range requests {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(request))
		r.Header.Set("Content-Type", "application/json")
		httpHandler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		httpResponses = append(httpResponses, strings.TrimSpace(w.Body.String()))
	}
	for transport, raw := range map[string]string{"stdio": stdio.String(), "http": strings.Join(httpResponses, "\n")} {
		lines := strings.Split(strings.TrimSpace(raw), "\n")
		if len(lines) != 4 {
			t.Fatal(transport, raw)
		}
		for _, line := range lines {
			var response map[string]any
			if err := json.Unmarshal([]byte(line), &response); err != nil {
				t.Fatal(err)
			}
			if response["error"] != nil || strings.Contains(line, `"isError":true`) {
				t.Fatal(transport, line)
			}
		}
		for tool := range supervision.ToolCapabilities() {
			if !strings.Contains(lines[0], `"name":"`+tool+`"`) {
				t.Fatal(transport, "missing", tool)
			}
		}
		if !strings.Contains(lines[1], "visible result") || strings.Contains(lines[1], "private reasoning") {
			t.Fatal(transport, lines[1])
		}
		if !strings.Contains(lines[3], "timed_out") {
			t.Fatal(transport, lines[3])
		}
	}
}
