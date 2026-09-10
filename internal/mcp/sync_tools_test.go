package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncToolsCallAPI(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "POST" {
			var b map[string]string
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b["snapshot"] != "reviewed" || b["raw"] != "merged" {
				t.Error("lost reconciliation body", b, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	}))
	defer server.Close()
	s := NewServer()
	RegisterSyncTools(s, NewAPIClient(server.URL))
	for _, name := range []string{"sync_status", "sync_diff", "sync_reconcile"} {
		out, err := s.tools[name].handler(context.Background(), map[string]any{"device_id": "device", "operation_id": "op", "snapshot": "reviewed", "action": "merge", "raw": "merged"})
		if err != nil || out != `{"status":"queued"}` {
			t.Fatal(name, out, err)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
	if _, err := s.tools["sync_reconcile"].handler(context.Background(), map[string]any{}); err == nil {
		t.Fatal("missing review accepted")
	}
}
