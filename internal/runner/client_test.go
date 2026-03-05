package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testConfig returns a RunnerConfig pointing at the given test server.
func testConfig(serverURL string) RunnerConfig {
	return RunnerConfig{
		BrainAPIURL:            serverURL,
		APIToken:               "test-token",
		PollInterval:           30,
		TaskPollInterval:       5,
		MaxParallel:            2,
		StateDir:               "/tmp/state",
		LogDir:                 "/tmp/log",
		WorkDir:                "/tmp/work",
		APITimeout:             5000,
		TaskTimeout:            0,
		IdleDetectionThreshold: 60000,
		MaxTotalProcesses:      10,
		MemoryThresholdPercent: 10,
		Opencode: OpencodeConfig{
			Bin:   "opencode",
			Agent: "",
			Model: "",
		},
	}
}

// ---------------------------------------------------------------------------
// CheckHealth
// ---------------------------------------------------------------------------

func TestAPIClient_CheckHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIHealth{
			Status:      "healthy",
			ZKAvailable: true,
			DBAvailable: true,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	health, err := client.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("Status = %q, want %q", health.Status, "healthy")
	}
	if !health.ZKAvailable {
		t.Error("expected ZKAvailable to be true")
	}
	if !health.DBAvailable {
		t.Error("expected DBAvailable to be true")
	}
}

func TestAPIClient_CheckHealth_Caching(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIHealth{Status: "healthy"})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	ctx := context.Background()

	// First call should hit the server
	_, err := client.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call within TTL should use cache
	_, err = client.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	count := atomic.LoadInt32(&callCount)
	if count != 1 {
		t.Errorf("expected 1 server call (cached), got %d", count)
	}
}

func TestAPIClient_CheckHealth_Unreachable(t *testing.T) {
	// Point at a server that doesn't exist
	client := NewAPIClient(testConfig("http://127.0.0.1:1"))
	health, err := client.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("CheckHealth should not return error for unreachable server, got: %v", err)
	}
	if health.Status != "unhealthy" {
		t.Errorf("Status = %q, want %q for unreachable server", health.Status, "unhealthy")
	}
}

// ---------------------------------------------------------------------------
// Authorization Header
// ---------------------------------------------------------------------------

func TestAPIClient_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"projects": {"proj-1"}})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
}

func TestAPIClient_NoAuthHeader_WhenTokenEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"projects": {}})
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.APIToken = ""
	client := NewAPIClient(cfg)
	_, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty when no token", gotAuth)
	}
}

// ---------------------------------------------------------------------------
// ListProjects
// ---------------------------------------------------------------------------

func TestAPIClient_ListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{
			"projects": {"brain-api", "my-project"},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	if projects[0] != "brain-api" {
		t.Errorf("projects[0] = %q, want %q", projects[0], "brain-api")
	}
}

func TestAPIClient_ListProjects_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.ListProjects(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// GetReadyTasks
// ---------------------------------------------------------------------------

func TestAPIClient_GetReadyTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/brain-api/ready" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.TaskListResponse{
			Tasks: []types.ResolvedTask{
				{ID: "abc123", Title: "Fix bug", Priority: "high"},
			},
			Count: 1,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	tasks, err := client.GetReadyTasks(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "abc123" {
		t.Errorf("task ID = %q, want %q", tasks[0].ID, "abc123")
	}
}

// ---------------------------------------------------------------------------
// GetNextTask
// ---------------------------------------------------------------------------

func TestAPIClient_GetNextTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/brain-api/next" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task": types.ResolvedTask{ID: "xyz789", Title: "Next task"},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	task, err := client.GetNextTask(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	if task.ID != "xyz789" {
		t.Errorf("task ID = %q, want %q", task.ID, "xyz789")
	}
}

func TestAPIClient_GetNextTask_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	task, err := client.GetNextTask(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error for 404: %v", err)
	}
	if task != nil {
		t.Errorf("expected nil task for 404, got %+v", task)
	}
}

// ---------------------------------------------------------------------------
// GetAllTasks
// ---------------------------------------------------------------------------

func TestAPIClient_GetAllTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/brain-api" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []types.ResolvedTask{
				{ID: "t1", Title: "Task 1"},
				{ID: "t2", Title: "Task 2"},
			},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	tasks, err := client.GetAllTasks(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

// ---------------------------------------------------------------------------
// UpdateTaskStatus
// ---------------------------------------------------------------------------

func TestAPIClient_UpdateTaskStatus(t *testing.T) {
	var gotMethod, gotRequestURI string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotRequestURI = r.RequestURI
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.UpdateTaskStatus(context.Background(), "projects/brain-api/task/abc123.md", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	// RequestURI preserves percent-encoding (r.URL.Path decodes it)
	wantURI := "/api/v1/entries/projects%2Fbrain-api%2Ftask%2Fabc123.md"
	if gotRequestURI != wantURI {
		t.Errorf("RequestURI = %q, want %q", gotRequestURI, wantURI)
	}
	if gotBody["status"] != "completed" {
		t.Errorf("body status = %q, want %q", gotBody["status"], "completed")
	}
}

// ---------------------------------------------------------------------------
// AppendToTask
// ---------------------------------------------------------------------------

func TestAPIClient_AppendToTask(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.AppendToTask(context.Background(), "projects/p/task/t.md", "## Progress\n- Done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["append"] != "## Progress\n- Done" {
		t.Errorf("body append = %q, want progress content", gotBody["append"])
	}
}

// ---------------------------------------------------------------------------
// ClaimTask
// ---------------------------------------------------------------------------

func TestAPIClient_ClaimTask_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/brain-api/abc123/claim" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["runnerId"] != "runner-1" {
			t.Errorf("runnerId = %q, want %q", body["runnerId"], "runner-1")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"taskId":  "abc123",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	result, err := client.ClaimTask(context.Background(), "brain-api", "abc123", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.TaskID != "abc123" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "abc123")
	}
}

func TestAPIClient_ClaimTask_AlreadyClaimed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"claimedBy": "other-runner",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	result, err := client.ClaimTask(context.Background(), "brain-api", "abc123", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error for 409: %v", err)
	}
	if result.Success {
		t.Error("expected Success to be false for 409")
	}
	if result.ClaimedBy != "other-runner" {
		t.Errorf("ClaimedBy = %q, want %q", result.ClaimedBy, "other-runner")
	}
}

// ---------------------------------------------------------------------------
// ReleaseTask
// ---------------------------------------------------------------------------

func TestAPIClient_ReleaseTask(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.ReleaseTask(context.Background(), "brain-api", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/brain-api/abc123/release" {
		t.Errorf("path = %q, want release path", gotPath)
	}
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

func TestAPIClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.APITimeout = 50 // 50ms timeout
	client := NewAPIClient(cfg)

	_, err := client.ListProjects(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// ---------------------------------------------------------------------------
// APIError
// ---------------------------------------------------------------------------

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 404, Body: "not found"}
	got := err.Error()
	want := "api error (404): not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
