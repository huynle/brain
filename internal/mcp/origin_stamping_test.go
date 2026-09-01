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

func originTestContext() func() {
	cachedContext = &ExecutionContext{
		ProjectID: "test-project",
		Workdir:   "projects/test",
		AbsPath:   "/Users/huy/projects/test",
		GitRemote: "git@github.com:test/repo.git",
		GitBranch: "main",
		ClientID:  "mcp-deadbeef",
		HostID:    "machine_cafebabe",
	}
	return func() { cachedContext = nil }
}

// TestBrainSave_StampsOriginOverStdio is the happy path: a stdio server shares
// a machine with its caller, so the ambient identity really is the caller's.
func TestBrainSave_StampsOriginOverStdio(t *testing.T) {
	defer originTestContext()()

	body := saveWithServer(t, NewServer(WithLocalFilesystem()), map[string]any{
		"type": "task", "title": "Test Task", "content": "Do something",
	})

	if body["origin_machine_id"] != "machine_cafebabe" {
		t.Errorf("origin_machine_id = %v, want machine_cafebabe", body["origin_machine_id"])
	}
	if body["origin_client_id"] != "mcp-deadbeef" {
		t.Errorf("origin_client_id = %v, want mcp-deadbeef", body["origin_client_id"])
	}
	if body["origin_path"] != "/Users/huy/projects/test" {
		t.Errorf("origin_path = %v, want /Users/huy/projects/test", body["origin_path"])
	}
}

// TestBrainSave_NoOriginOverHTTPTransport is the guard that matters most.
//
// GetCachedContext is a process-global computed from os.Getwd(). The HTTP
// transport runs inside brain-api, so that identity is the API HOST's, shared
// by every client. Stamping it would brand every task created through the PWA
// or a remote MCP client with the API server's machine id — and, at
// machine_affinity=local, pin them all there. Nothing else in the codebase
// protects this.
func TestBrainSave_NoOriginOverHTTPTransport(t *testing.T) {
	defer originTestContext()()

	// NewServer() without WithLocalFilesystem() == the HTTP transport.
	body := saveWithServer(t, NewServer(), map[string]any{
		"type": "task", "title": "Test Task", "content": "Do something",
	})

	for _, key := range []string{"origin_machine_id", "origin_client_id", "origin_path"} {
		if v, ok := body[key]; ok && v != nil && v != "" {
			t.Errorf("%s = %v; the HTTP transport must not stamp the API host's identity onto tasks", key, v)
		}
	}
}

// TestBrainSave_MachineAffinityIsCallerIntent: unlike the origin fields,
// machine_affinity comes from args and so is honored on every transport.
func TestBrainSave_MachineAffinityIsCallerIntent(t *testing.T) {
	defer originTestContext()()

	body := saveWithServer(t, NewServer(WithLocalFilesystem()), map[string]any{
		"type": "task", "title": "Pinned", "content": "x",
		"machine_affinity": types.MachineAffinityLocal,
	})
	if body["machine_affinity"] != types.MachineAffinityLocal {
		t.Errorf("machine_affinity = %v, want local", body["machine_affinity"])
	}

	// Omitted means omitted — the default is resolved server-side, not here,
	// so an unset field must not be sent as a literal value.
	body = saveWithServer(t, NewServer(WithLocalFilesystem()), map[string]any{
		"type": "task", "title": "Unpinned", "content": "x",
	})
	if v, ok := body["machine_affinity"]; ok && v != nil && v != "" {
		t.Errorf("machine_affinity = %v, want unset", v)
	}
}

// TestBrainSave_OriginNotSpoofableFromArgs: origin describes the caller, so a
// caller-supplied value must be ignored rather than trusted.
func TestBrainSave_OriginNotSpoofableFromArgs(t *testing.T) {
	defer originTestContext()()

	body := saveWithServer(t, NewServer(WithLocalFilesystem()), map[string]any{
		"type": "task", "title": "Spoof", "content": "x",
		"origin_machine_id": "machine_attacker",
		"origin_client_id":  "mcp-attacker",
		"origin_path":       "/etc",
	})

	if body["origin_machine_id"] != "machine_cafebabe" {
		t.Errorf("origin_machine_id = %v, want the ambient machine_cafebabe", body["origin_machine_id"])
	}
	if body["origin_path"] != "/Users/huy/projects/test" {
		t.Errorf("origin_path = %v, want the ambient path", body["origin_path"])
	}
}

// TestBrainSave_NonTaskGetsNoOrigin mirrors TestBrainSave_NonTaskNoEnrichment:
// provenance is a task-execution concern, and notes should not carry it.
func TestBrainSave_NonTaskGetsNoOrigin(t *testing.T) {
	defer originTestContext()()

	body := saveWithServer(t, NewServer(WithLocalFilesystem()), map[string]any{
		"type": "note", "title": "A note", "content": "x",
	})
	for _, key := range []string{"origin_machine_id", "origin_client_id", "origin_path"} {
		if v, ok := body[key]; ok && v != nil && v != "" {
			t.Errorf("%s = %v on a non-task entry, want unset", key, v)
		}
	}
}

// TestGetExecutionContext_AbsPathRequiresRepo: outside a git repository there
// is no directory worth handing a runner, and guessing one invites it to open
// an unrelated path that happens to exist there.
func TestGetExecutionContext_AbsPathRequiresRepo(t *testing.T) {
	ctx := GetExecutionContext(t.TempDir())
	if ctx.AbsPath != "" {
		t.Errorf("AbsPath = %q outside a git repo, want empty", ctx.AbsPath)
	}
}

// TestBrainSave_LocalAffinityRefusedOverHTTPTransport: "local" needs an origin
// machine, and the HTTP transport deliberately stamps none. Accepting it would
// queue a task that every runner refuses forever with
// machine_affinity_unresolved. Fail where the caller can still see why.
func TestBrainSave_LocalAffinityRefusedOverHTTPTransport(t *testing.T) {
	defer originTestContext()()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "abc", "path": "p.md", "title": "T", "type": "task", "status": "draft"})
	}))
	defer server.Close()

	s := NewServer() // no WithLocalFilesystem == HTTP transport
	RegisterBrainTools(s, NewAPIClient(server.URL))

	_, err := s.tools["save"].handler(context.Background(), map[string]any{
		"type": "task", "title": "Pinned", "content": "x",
		"machine_affinity": types.MachineAffinityLocal,
	})
	if err == nil {
		t.Fatal("machine_affinity=local accepted over the HTTP transport; the task would never be runnable")
	}

	// The soft values stay usable on every transport.
	for _, v := range []string{types.MachineAffinityPreferred, types.MachineAffinityNone} {
		s2 := NewServer()
		RegisterBrainTools(s2, NewAPIClient(server.URL))
		if _, err := s2.tools["save"].handler(context.Background(), map[string]any{
			"type": "task", "title": "OK", "content": "x", "machine_affinity": v,
		}); err != nil {
			t.Errorf("machine_affinity=%q rejected over HTTP transport: %v", v, err)
		}
	}
}
