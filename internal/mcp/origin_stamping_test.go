package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// saveWithServer runs the save tool against a stub API and returns the request
// body it sent.
func saveWithServer(t *testing.T, s *Server, args map[string]any) map[string]any {
	t.Helper()
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "abc", "path": "p/task/abc.md", "title": "Task", "type": "task", "status": "draft",
		})
	}))
	defer server.Close()

	RegisterBrainTools(s, NewAPIClient(server.URL))
	if _, err := s.tools["save"].handler(context.Background(), args); err != nil {
		t.Fatalf("save handler error: %v", err)
	}
	return capturedBody
}

// ambientTestContext stands in for the Brain API process's own context: a
// project it was launched in, but no caller identity.
func ambientTestContext() func() {
	cachedContext = &ExecutionContext{
		ProjectID: "api-host-project",
		Workdir:   "projects/api-host",
		GitBranch: "main",
	}
	return func() { cachedContext = nil }
}

func serverWithCaller() *Server {
	s := NewServer()
	s.caller = &CallerContext{
		HostID:   "machine_cafebabe",
		ClientID: "opencode-deadbeef",
		Workdir:  "/Users/huy/projects/test",
		Home:     "/Users/huy",
	}
	return s
}

// TestBrainSave_StampsOriginFromCallerHeaders is the happy path: the caller
// declared its machine, install and folder.
func TestBrainSave_StampsOriginFromCallerHeaders(t *testing.T) {
	defer ambientTestContext()()

	body := saveWithServer(t, serverWithCaller(), map[string]any{
		"type": "task", "title": "Test Task", "content": "Do something",
	})

	if body["origin_machine_id"] != "machine_cafebabe" {
		t.Errorf("origin_machine_id = %v, want machine_cafebabe", body["origin_machine_id"])
	}
	if body["origin_client_id"] != "opencode-deadbeef" {
		t.Errorf("origin_client_id = %v, want opencode-deadbeef", body["origin_client_id"])
	}
	if body["origin_path"] != "/Users/huy/projects/test" {
		t.Errorf("origin_path = %v, want /Users/huy/projects/test", body["origin_path"])
	}
	if body["workdir"] != "projects/test" {
		t.Errorf("workdir = %v, want the caller's projects/test, not the API host's", body["workdir"])
	}
}

// TestBrainSave_NoOriginWithoutCallerHeaders is the guard that matters most.
//
// GetCachedContext is a process-global computed from the Brain API's own
// os.Getwd(), shared by every client. Stamping it would brand every task with
// the API server's identity and, at machine_affinity=local, pin them all there.
func TestBrainSave_NoOriginWithoutCallerHeaders(t *testing.T) {
	defer ambientTestContext()()

	body := saveWithServer(t, NewServer(), map[string]any{
		"type": "task", "title": "Test Task", "content": "Do something",
	})

	for _, key := range []string{"origin_machine_id", "origin_client_id", "origin_path"} {
		if v, ok := body[key]; ok && v != nil && v != "" {
			t.Errorf("%s = %v; a headerless call must not stamp the API host's identity onto tasks", key, v)
		}
	}
}

// TestBrainSave_MachineAffinityIsCallerIntent: unlike the origin fields,
// machine_affinity comes from args.
func TestBrainSave_MachineAffinityIsCallerIntent(t *testing.T) {
	defer ambientTestContext()()

	body := saveWithServer(t, serverWithCaller(), map[string]any{
		"type": "task", "title": "Pinned", "content": "x",
		"machine_affinity": types.MachineAffinityLocal,
	})
	if body["machine_affinity"] != types.MachineAffinityLocal {
		t.Errorf("machine_affinity = %v, want local", body["machine_affinity"])
	}

	// Omitted means omitted — the default is resolved server-side, not here,
	// so an unset field must not be sent as a literal value.
	body = saveWithServer(t, serverWithCaller(), map[string]any{
		"type": "task", "title": "Unpinned", "content": "x",
	})
	if v, ok := body["machine_affinity"]; ok && v != nil && v != "" {
		t.Errorf("machine_affinity = %v, want unset", v)
	}
}

// TestBrainSave_OriginNotSpoofableFromArgs: origin describes the caller, so a
// tool-argument value must be ignored rather than trusted.
func TestBrainSave_OriginNotSpoofableFromArgs(t *testing.T) {
	defer ambientTestContext()()

	body := saveWithServer(t, serverWithCaller(), map[string]any{
		"type": "task", "title": "Spoof", "content": "x",
		"origin_machine_id": "machine_attacker",
		"origin_client_id":  "mcp-attacker",
		"origin_path":       "/etc",
	})

	if body["origin_machine_id"] != "machine_cafebabe" {
		t.Errorf("origin_machine_id = %v, want the header's machine_cafebabe", body["origin_machine_id"])
	}
	if body["origin_path"] != "/Users/huy/projects/test" {
		t.Errorf("origin_path = %v, want the header's path", body["origin_path"])
	}
}

// TestBrainSave_NonTaskGetsNoOrigin mirrors TestBrainSave_NonTaskNoEnrichment:
// provenance is a task-execution concern, and notes should not carry it.
func TestBrainSave_NonTaskGetsNoOrigin(t *testing.T) {
	defer ambientTestContext()()

	body := saveWithServer(t, serverWithCaller(), map[string]any{
		"type": "note", "title": "A note", "content": "x",
	})
	for _, key := range []string{"origin_machine_id", "origin_client_id", "origin_path"} {
		if v, ok := body[key]; ok && v != nil && v != "" {
			t.Errorf("%s = %v on a non-task entry, want unset", key, v)
		}
	}
}

// TestBrainSave_LocalAffinityRefusedWithoutHostID: "local" needs an origin
// machine. Accepting it would queue a task that every runner refuses forever
// with machine_affinity_unresolved. Fail where the caller can still see why.
func TestBrainSave_LocalAffinityRefusedWithoutHostID(t *testing.T) {
	defer ambientTestContext()()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "abc", "path": "p.md", "title": "T", "type": "task", "status": "draft"})
	}))
	defer server.Close()

	noHost := NewServer()
	noHost.caller = &CallerContext{ClientID: "opencode-x", Workdir: "/Users/huy/projects/test"}
	for name, s := range map[string]*Server{"headerless": NewServer(), "no host id": noHost} {
		RegisterBrainTools(s, NewAPIClient(server.URL))
		_, err := s.tools["save"].handler(context.Background(), map[string]any{
			"type": "task", "title": "Pinned", "content": "x",
			"machine_affinity": types.MachineAffinityLocal,
		})
		if err == nil {
			t.Errorf("%s: machine_affinity=local accepted; the task would never be runnable", name)
		}
	}

	// The soft values stay usable without caller headers.
	for _, v := range []string{types.MachineAffinityPreferred, types.MachineAffinityNone} {
		s2 := NewServer()
		RegisterBrainTools(s2, NewAPIClient(server.URL))
		if _, err := s2.tools["save"].handler(context.Background(), map[string]any{
			"type": "task", "title": "OK", "content": "x", "machine_affinity": v,
		}); err != nil {
			t.Errorf("machine_affinity=%q rejected without caller headers: %v", v, err)
		}
	}
}
