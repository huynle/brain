package runner

import (
	"context"
	"encoding/json"
	"io"
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
		if r.URL.Path != "/api/v1/health" {
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
		// Production code unmarshals directly into ResolvedTask (no wrapper)
		json.NewEncoder(w).Encode(types.ResolvedTask{ID: "xyz789", Title: "Next task"})
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
	// UpdateTaskStatus now uses UpdateMetadata which hits /metadata endpoint
	// encodePathComponent keeps slashes intact (each segment is PathEscaped)
	wantURI := "/api/v1/entries/projects/brain-api/task/abc123.md/metadata"
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
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.ReleaseTask(context.Background(), "brain-api", "abc123", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/brain-api/abc123/release" {
		t.Errorf("path = %q, want release path", gotPath)
	}
	if gotBody["runnerId"] != "runner-1" {
		t.Errorf("runnerId = %q, want %q", gotBody["runnerId"], "runner-1")
	}
}

// ---------------------------------------------------------------------------
// RenewClaim
// ---------------------------------------------------------------------------

func TestAPIClient_RenewClaim_Success(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"taskId":    "abc123",
			"runnerId":  "runner-1",
			"expiresAt": "2024-01-01T00:10:00Z",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.RenewClaim(context.Background(), "brain-api", "abc123", "runner-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/brain-api/abc123/renew" {
		t.Errorf("path = %q, want renew path", gotPath)
	}
	if gotBody["runnerId"] != "runner-1" {
		t.Errorf("runnerId = %q, want %q", gotBody["runnerId"], "runner-1")
	}
}

func TestAPIClient_RenewClaim_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "claim not found",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.RenewClaim(context.Background(), "brain-api", "abc123", "runner-1")
	if err == nil {
		t.Fatal("expected error for 404 response")
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
// GetEntry
// ---------------------------------------------------------------------------

func TestAPIClient_GetEntry(t *testing.T) {
	var gotRequestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		// Brain API returns entries as flat top-level JSON objects (not wrapped)
		response := map[string]interface{}{
			"id":       "abc123def",
			"path":     "projects/brain-api/task/abc123def.md",
			"title":    "Test Task",
			"type":     "task",
			"status":   "pending",
			"priority": "high",
			"content":  "# Test Task\n\nTask content",
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	entry, err := client.GetEntry(context.Background(), "projects/brain-api/task/abc123def.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	// encodePathComponent keeps slashes intact (each segment is PathEscaped)
	wantURI := "/api/v1/entries/projects/brain-api/task/abc123def.md"
	if gotRequestURI != wantURI {
		t.Errorf("RequestURI = %q, want %q", gotRequestURI, wantURI)
	}
	if entry.ID != "abc123def" {
		t.Errorf("ID = %q, want %q", entry.ID, "abc123def")
	}
	if entry.Title != "Test Task" {
		t.Errorf("Title = %q, want %q", entry.Title, "Test Task")
	}
	if entry.Status != "pending" {
		t.Errorf("Status = %q, want %q", entry.Status, "pending")
	}
}

func TestAPIClient_GetEntry_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Entry not found",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GetEntry(context.Background(), "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// UpdateEntry
// ---------------------------------------------------------------------------

func TestAPIClient_UpdateEntry(t *testing.T) {
	var gotBody map[string]interface{}
	var gotRequestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method: %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		// Brain API returns updated entries as flat top-level JSON objects (not wrapped)
		response := map[string]interface{}{
			"id":       "abc123def",
			"path":     "projects/brain-api/task/abc123def.md",
			"title":    "Updated Task",
			"type":     "task",
			"status":   "completed",
			"priority": "high",
			"agent":    "dev",
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	updates := map[string]interface{}{
		"status": "completed",
		"agent":  "dev",
	}
	entry, err := client.UpdateEntry(context.Background(), "projects/brain-api/task/abc123def.md", updates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	// encodePathComponent keeps slashes intact (each segment is PathEscaped)
	wantURI := "/api/v1/entries/projects/brain-api/task/abc123def.md"
	if gotRequestURI != wantURI {
		t.Errorf("RequestURI = %q, want %q", gotRequestURI, wantURI)
	}

	// Check request body
	if gotBody["status"] != "completed" {
		t.Errorf("body status = %v, want 'completed'", gotBody["status"])
	}
	if gotBody["agent"] != "dev" {
		t.Errorf("body agent = %v, want 'dev'", gotBody["agent"])
	}

	// Check response
	if entry.Status != "completed" {
		t.Errorf("entry Status = %q, want 'completed'", entry.Status)
	}
	if entry.Agent != "dev" {
		t.Errorf("entry Agent = %q, want 'dev'", entry.Agent)
	}
}

func TestAPIClient_UpdateEntry_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid status value",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	updates := map[string]interface{}{
		"status": "invalid",
	}
	_, err := client.UpdateEntry(context.Background(), "task.md", updates)
	if err == nil {
		t.Fatal("expected error for 400 response")
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

// ---------------------------------------------------------------------------
// GetFeature
// ---------------------------------------------------------------------------

func TestAPIClient_GetFeature_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/brain-api/features/dark-mode" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		// Production decodes into FeatureResponse{Feature Feature `json:"feature"`}
		// so the API response must be wrapped in {"feature": {...}}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"feature": map[string]interface{}{
				"featureId": "dark-mode",
				"tasks": []map[string]interface{}{
					{
						"id":         "abc123",
						"title":      "Add dark mode toggle",
						"status":     "active",
						"priority":   "high",
						"featureId":  "dark-mode",
						"dependsOn":  []string{},
						"dependents": []string{},
					},
					{
						"id":         "def456",
						"title":      "Update theme colors",
						"status":     "pending",
						"priority":   "medium",
						"featureId":  "dark-mode",
						"dependsOn":  []string{"abc123"},
						"dependents": []string{},
					},
				},
				"ready": true,
				"stats": map[string]int{
					"ready":     1,
					"waiting":   0,
					"blocked":   0,
					"completed": 0,
				},
			},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	feature, err := client.GetFeature(context.Background(), "brain-api", "dark-mode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if feature == nil {
		t.Fatal("expected feature, got nil")
	}

	if feature.Feature.FeatureID != "dark-mode" {
		t.Errorf("FeatureID = %q, want %q", feature.Feature.FeatureID, "dark-mode")
	}

	if len(feature.Feature.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(feature.Feature.Tasks))
	}

	if feature.Feature.Tasks[0].ID != "abc123" {
		t.Errorf("Tasks[0].ID = %q, want %q", feature.Feature.Tasks[0].ID, "abc123")
	}

	if feature.Feature.Tasks[1].ID != "def456" {
		t.Errorf("Tasks[1].ID = %q, want %q", feature.Feature.Tasks[1].ID, "def456")
	}

	if !feature.Feature.Ready {
		t.Error("expected Ready to be true")
	}

	if feature.Feature.Stats == nil {
		t.Fatal("expected Stats, got nil")
	}

	if feature.Feature.Stats.Ready != 1 {
		t.Errorf("Stats.Ready = %d, want 1", feature.Feature.Stats.Ready)
	}
}

func TestAPIClient_GetFeature_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"feature not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GetFeature(context.Background(), "brain-api", "nonexistent")

	if err == nil {
		t.Fatal("expected error for 404 response")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}

	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
}

func TestAPIClient_GetFeature_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GetFeature(context.Background(), "brain-api", "dark-mode")

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// GetFeatures
// ---------------------------------------------------------------------------

func TestAPIClient_GetFeatures_Success(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.FeatureListResponse{
			Features: []types.Feature{
				{
					FeatureID: "dark-mode",
					Tasks:     []types.ResolvedTask{{ID: "t1", Title: "Task 1"}},
					Ready:     true,
				},
				{
					FeatureID: "auth-flow",
					Tasks:     []types.ResolvedTask{{ID: "t2", Title: "Task 2"}},
					Ready:     false,
				},
			},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	features, err := client.GetFeatures(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/tasks/brain-api/features" {
		t.Errorf("path = %q, want /api/v1/tasks/brain-api/features", gotPath)
	}
	if len(features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(features))
	}
	if features[0].FeatureID != "dark-mode" {
		t.Errorf("features[0].FeatureID = %q, want %q", features[0].FeatureID, "dark-mode")
	}
	if !features[0].Ready {
		t.Error("expected features[0].Ready to be true")
	}
	if features[1].FeatureID != "auth-flow" {
		t.Errorf("features[1].FeatureID = %q, want %q", features[1].FeatureID, "auth-flow")
	}
}

func TestAPIClient_GetFeatures_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.FeatureListResponse{Features: []types.Feature{}})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	features, err := client.GetFeatures(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(features) != 0 {
		t.Errorf("expected 0 features, got %d", len(features))
	}
}

func TestAPIClient_GetFeatures_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GetFeatures(context.Background(), "brain-api")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// BulkUpdate
// ---------------------------------------------------------------------------

func TestAPIClient_BulkUpdate_Success(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody types.BulkUpdateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.BulkUpdateResponse{
			Updated: 2,
			Failed:  0,
			Total:   2,
			DryRun:  false,
			Results: []types.BulkUpdateResult{
				{Path: "projects/p/task/t1.md", ID: "t1", Title: "Task 1", Status: "ok"},
				{Path: "projects/p/task/t2.md", ID: "t2", Title: "Task 2", Status: "ok"},
			},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	completed := "completed"
	req := types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: "projects/p/task/t1.md", Updates: types.UpdateEntryRequest{Status: &completed}},
			{Path: "projects/p/task/t2.md", Updates: types.UpdateEntryRequest{Status: &completed}},
		},
	}
	result, err := client.BulkUpdate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/entries/bulk-update" {
		t.Errorf("path = %q, want /api/v1/entries/bulk-update", gotPath)
	}
	if len(gotBody.Entries) != 2 {
		t.Errorf("body entries = %d, want 2", len(gotBody.Entries))
	}
	if result.Updated != 2 {
		t.Errorf("Updated = %d, want 2", result.Updated)
	}
	if result.Failed != 0 {
		t.Errorf("Failed = %d, want 0", result.Failed)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if result.Results[0].Status != "ok" {
		t.Errorf("Results[0].Status = %q, want %q", result.Results[0].Status, "ok")
	}
}

func TestAPIClient_BulkUpdate_DryRun(t *testing.T) {
	var gotBody types.BulkUpdateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.BulkUpdateResponse{
			Updated: 0,
			Failed:  0,
			Total:   1,
			DryRun:  true,
			Results: []types.BulkUpdateResult{
				{Path: "projects/p/task/t1.md", ID: "t1", Title: "Task 1", Status: "ok"},
			},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	completed := "completed"
	req := types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: "projects/p/task/t1.md", Updates: types.UpdateEntryRequest{Status: &completed}},
		},
		DryRun: true,
	}
	result, err := client.BulkUpdate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotBody.DryRun {
		t.Error("expected DryRun to be true in request body")
	}
	if !result.DryRun {
		t.Error("expected DryRun to be true in response")
	}
}

func TestAPIClient_BulkUpdate_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"validation failed"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.BulkUpdate(context.Background(), types.BulkUpdateRequest{})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// PauseProject
// ---------------------------------------------------------------------------

func TestAPIClient_PauseProject(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.PauseProject(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/runner/pause/brain-api" {
		t.Errorf("path = %q, want /api/v1/tasks/runner/pause/brain-api", gotPath)
	}
}

func TestAPIClient_PauseProject_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.PauseProject(context.Background(), "brain-api")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// ResumeProject
// ---------------------------------------------------------------------------

func TestAPIClient_ResumeProject(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.ResumeProject(context.Background(), "brain-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/runner/resume/brain-api" {
		t.Errorf("path = %q, want /api/v1/tasks/runner/resume/brain-api", gotPath)
	}
}

func TestAPIClient_ResumeProject_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.ResumeProject(context.Background(), "brain-api")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// PauseAll
// ---------------------------------------------------------------------------

func TestAPIClient_PauseAll(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.PauseAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/runner/pause" {
		t.Errorf("path = %q, want /api/v1/tasks/runner/pause", gotPath)
	}
}

func TestAPIClient_PauseAll_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.PauseAll(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// ResumeAll
// ---------------------------------------------------------------------------

func TestAPIClient_ResumeAll(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.ResumeAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/runner/resume" {
		t.Errorf("path = %q, want /api/v1/tasks/runner/resume", gotPath)
	}
}

func TestAPIClient_ResumeAll_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.ResumeAll(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// GetRunnerStatus
// ---------------------------------------------------------------------------

func TestAPIClient_GetRunnerStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/runner/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"running":        true,
			"paused":         true,
			"pausedProjects": []string{"brain-api", "my-project"},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	status, err := client.GetRunnerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if !status.Paused {
		t.Error("expected Paused to be true")
	}
	if len(status.PausedProjects) != 2 {
		t.Fatalf("expected 2 paused projects, got %d", len(status.PausedProjects))
	}
	if status.PausedProjects[0] != "brain-api" {
		t.Errorf("PausedProjects[0] = %q, want %q", status.PausedProjects[0], "brain-api")
	}
}

func TestAPIClient_GetRunnerStatus_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GetRunnerStatus(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestAPIClient_GetRunnerStatus_NotPaused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"running":        true,
			"paused":         false,
			"pausedProjects": []string{},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	status, err := client.GetRunnerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Paused {
		t.Error("expected Paused to be false")
	}
	if len(status.PausedProjects) != 0 {
		t.Errorf("expected 0 paused projects, got %d", len(status.PausedProjects))
	}
}

// ---------------------------------------------------------------------------
// CreateEntry
// ---------------------------------------------------------------------------

func TestAPIClient_CreateEntry(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody types.CreateEntryRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(types.CreateEntryResponse{
			ID:     "abc12345",
			Path:   "projects/brain-api/task/abc12345.md",
			Title:  "New Task",
			Type:   "task",
			Status: "pending",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	req := types.CreateEntryRequest{
		Type:    "task",
		Title:   "New Task",
		Content: "# New Task\n\nTask content",
		Tags:    []string{"test"},
	}
	result, err := client.CreateEntry(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/entries" {
		t.Errorf("path = %q, want /api/v1/entries", gotPath)
	}
	if gotBody.Type != "task" {
		t.Errorf("body type = %q, want %q", gotBody.Type, "task")
	}
	if gotBody.Title != "New Task" {
		t.Errorf("body title = %q, want %q", gotBody.Title, "New Task")
	}
	if result.ID != "abc12345" {
		t.Errorf("result ID = %q, want %q", result.ID, "abc12345")
	}
	if result.Path != "projects/brain-api/task/abc12345.md" {
		t.Errorf("result Path = %q, want expected path", result.Path)
	}
}

func TestAPIClient_CreateEntry_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"missing required field: type"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.CreateEntry(context.Background(), types.CreateEntryRequest{})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

// ---------------------------------------------------------------------------
// SearchEntries
// ---------------------------------------------------------------------------

func TestAPIClient_SearchEntries(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody types.SearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.SearchResponse{
			Results: []types.SearchResult{
				{ID: "r1", Path: "projects/p/task/r1.md", Title: "Result 1", Snippet: "match here"},
				{ID: "r2", Path: "projects/p/task/r2.md", Title: "Result 2", Snippet: "another match"},
			},
			Total: 2,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	result, err := client.SearchEntries(context.Background(), types.SearchRequest{
		Query: "test query",
		Type:  "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/search" {
		t.Errorf("path = %q, want /api/v1/search", gotPath)
	}
	if gotBody.Query != "test query" {
		t.Errorf("body query = %q, want %q", gotBody.Query, "test query")
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if result.Results[0].ID != "r1" {
		t.Errorf("Results[0].ID = %q, want %q", result.Results[0].ID, "r1")
	}
}

func TestAPIClient_SearchEntries_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.SearchEntries(context.Background(), types.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// ListEntries
// ---------------------------------------------------------------------------

func TestAPIClient_ListEntries(t *testing.T) {
	var gotMethod string
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.ListEntriesResponse{
			Entries: []types.BrainEntry{
				{ID: "e1", Title: "Entry 1", Type: "task"},
				{ID: "e2", Title: "Entry 2", Type: "task"},
			},
			Total:  2,
			Limit:  50,
			Offset: 0,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	result, err := client.ListEntries(context.Background(), map[string]string{
		"type":   "task",
		"status": "pending",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	// Check query params are present (order may vary)
	if gotQuery == "" {
		t.Error("expected query params, got empty")
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
}

func TestAPIClient_ListEntries_NoParams(t *testing.T) {
	var gotRequestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.ListEntriesResponse{
			Entries: []types.BrainEntry{},
			Total:   0,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.ListEntries(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRequestURI != "/api/v1/entries" {
		t.Errorf("RequestURI = %q, want /api/v1/entries (no query params)", gotRequestURI)
	}
}

func TestAPIClient_ListEntries_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.ListEntries(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// GetEntryRaw
// ---------------------------------------------------------------------------

func TestAPIClient_GetEntryRaw(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("X-Brain-Id", "abc123")
		w.Header().Set("X-Brain-Title", "Test Entry")
		w.Header().Set("X-Brain-Type", "task")
		w.Header().Set("X-Brain-Status", "pending")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# Test Entry\n\nRaw markdown content"))
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	content, headers, err := client.GetEntryRaw(context.Background(), "projects/brain-api/task/abc123.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccept != "text/markdown" {
		t.Errorf("Accept header = %q, want %q", gotAccept, "text/markdown")
	}
	if content != "# Test Entry\n\nRaw markdown content" {
		t.Errorf("content = %q, want raw markdown", content)
	}
	if headers.Get("X-Brain-Id") != "abc123" {
		t.Errorf("X-Brain-Id = %q, want %q", headers.Get("X-Brain-Id"), "abc123")
	}
	if headers.Get("X-Brain-Title") != "Test Entry" {
		t.Errorf("X-Brain-Title = %q, want %q", headers.Get("X-Brain-Title"), "Test Entry")
	}
}

func TestAPIClient_GetEntryRaw_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, _, err := client.GetEntryRaw(context.Background(), "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// ---------------------------------------------------------------------------
// GetEntryFull
// ---------------------------------------------------------------------------

func TestAPIClient_GetEntryFull(t *testing.T) {
	var gotAccept string
	fullContent := "---\ntitle: Test Entry\ntype: task\nstatus: pending\n---\n\n# Test Entry\n\nFull content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/x-brain-full")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fullContent))
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	content, err := client.GetEntryFull(context.Background(), "projects/brain-api/task/abc123.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccept != "text/x-brain-full" {
		t.Errorf("Accept header = %q, want %q", gotAccept, "text/x-brain-full")
	}
	if content != fullContent {
		t.Errorf("content = %q, want full frontmatter+body", content)
	}
}

func TestAPIClient_GetEntryFull_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GetEntryFull(context.Background(), "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// ---------------------------------------------------------------------------
// UpdateEntryRaw
// ---------------------------------------------------------------------------

func TestAPIClient_UpdateEntryRaw(t *testing.T) {
	var gotMethod string
	var gotContentType string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		bodyBytes, _ := io.ReadAll(r.Body)
		gotBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	newContent := "# Updated\n\nNew raw markdown"
	err := client.UpdateEntryRaw(context.Background(), "projects/brain-api/task/abc123.md", newContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotContentType != "text/markdown" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "text/markdown")
	}
	if gotBody != newContent {
		t.Errorf("body = %q, want raw markdown content", gotBody)
	}
}

func TestAPIClient_UpdateEntryRaw_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.UpdateEntryRaw(context.Background(), "task.md", "content")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// UpdateEntryFull
// ---------------------------------------------------------------------------

func TestAPIClient_UpdateEntryFull(t *testing.T) {
	var gotMethod string
	var gotContentType string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		bodyBytes, _ := io.ReadAll(r.Body)
		gotBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	fullContent := "---\ntitle: Updated\nstatus: completed\n---\n\n# Updated\n\nNew content"
	err := client.UpdateEntryFull(context.Background(), "projects/brain-api/task/abc123.md", fullContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotContentType != "text/x-brain-full" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "text/x-brain-full")
	}
	if gotBody != fullContent {
		t.Errorf("body = %q, want full frontmatter+body content", gotBody)
	}
}

func TestAPIClient_UpdateEntryFull_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	err := client.UpdateEntryFull(context.Background(), "task.md", "content")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// MoveEntry
// ---------------------------------------------------------------------------

func TestAPIClient_MoveEntry(t *testing.T) {
	var gotMethod, gotRequestURI string
	var gotBody types.MoveEntryRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotRequestURI = r.RequestURI
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.MoveResult{
			Success: true,
			From:    "projects/brain-api/task/abc123.md",
			To:      "projects/other-project/task/abc123.md",
			OldPath: "projects/brain-api/task/abc123.md",
			NewPath: "projects/other-project/task/abc123.md",
			Project: "other-project",
			ID:      "abc123",
			Title:   "Test Task",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	result, err := client.MoveEntry(context.Background(), "projects/brain-api/task/abc123.md", "other-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	wantURI := "/api/v1/entries/projects/brain-api/task/abc123.md/move"
	if gotRequestURI != wantURI {
		t.Errorf("RequestURI = %q, want %q", gotRequestURI, wantURI)
	}
	if gotBody.Project != "other-project" {
		t.Errorf("body project = %q, want %q", gotBody.Project, "other-project")
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.From != "projects/brain-api/task/abc123.md" {
		t.Errorf("From = %q, want source path", result.From)
	}
	if result.To != "projects/other-project/task/abc123.md" {
		t.Errorf("To = %q, want target path", result.To)
	}
}

func TestAPIClient_MoveEntry_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"entry not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.MoveEntry(context.Background(), "nonexistent.md", "other-project")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// doRequestWithHeaders
// ---------------------------------------------------------------------------

func TestAPIClient_DoRequestWithHeaders_OverridesDefaults(t *testing.T) {
	var gotAccept, gotContentType, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	resp, err := client.doRequestWithHeaders(context.Background(), http.MethodGet, "/test", nil, map[string]string{
		"Accept": "text/markdown",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotAccept != "text/markdown" {
		t.Errorf("Accept = %q, want %q", gotAccept, "text/markdown")
	}
	// Content-Type should still be the default since we only overrode Accept
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q (default)", gotContentType, "application/json")
	}
	// Auth should still be present
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
}
