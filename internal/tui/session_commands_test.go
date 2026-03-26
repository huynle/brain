package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func TestGetOpencodeBin_Default(t *testing.T) {
	// Unset env var to test default
	orig := os.Getenv("OPENCODE_BIN")
	os.Unsetenv("OPENCODE_BIN")
	defer func() {
		if orig != "" {
			os.Setenv("OPENCODE_BIN", orig)
		}
	}()

	bin := getOpencodeBin()
	if bin != "opencode" {
		t.Errorf("getOpencodeBin() = %q, want %q", bin, "opencode")
	}
}

func TestGetOpencodeBin_FromEnv(t *testing.T) {
	orig := os.Getenv("OPENCODE_BIN")
	os.Setenv("OPENCODE_BIN", "/usr/local/bin/my-opencode")
	defer func() {
		if orig != "" {
			os.Setenv("OPENCODE_BIN", orig)
		} else {
			os.Unsetenv("OPENCODE_BIN")
		}
	}()

	bin := getOpencodeBin()
	if bin != "/usr/local/bin/my-opencode" {
		t.Errorf("getOpencodeBin() = %q, want %q", bin, "/usr/local/bin/my-opencode")
	}
}

func TestSessionOpenedMsg_Fields(t *testing.T) {
	msg := sessionOpenedMsg{
		taskID:    "task123",
		sessionID: "ses_abc",
		err:       nil,
	}

	if msg.taskID != "task123" {
		t.Errorf("taskID = %q, want %q", msg.taskID, "task123")
	}
	if msg.sessionID != "ses_abc" {
		t.Errorf("sessionID = %q, want %q", msg.sessionID, "ses_abc")
	}
	if msg.err != nil {
		t.Errorf("err = %v, want nil", msg.err)
	}
}

func TestSessionsFetchedMsg_Fields(t *testing.T) {
	msg := sessionsFetchedMsg{
		sessionIDs: []string{"ses_a", "ses_b"},
		taskPath:   "projects/test/task/abc.md",
		tmuxMode:   true,
		err:        nil,
	}

	if len(msg.sessionIDs) != 2 {
		t.Errorf("sessionIDs length = %d, want 2", len(msg.sessionIDs))
	}
	if msg.taskPath != "projects/test/task/abc.md" {
		t.Errorf("taskPath = %q, want %q", msg.taskPath, "projects/test/task/abc.md")
	}
	if !msg.tmuxMode {
		t.Error("tmuxMode should be true")
	}
}

func TestOpenSessionFullscreen_ReturnsCmd(t *testing.T) {
	cmd := openSessionFullscreen("ses_test123", "task_abc")
	if cmd == nil {
		t.Error("openSessionFullscreen should return a non-nil command")
	}
}

func TestOpenSessionTmux_ReturnsCmd(t *testing.T) {
	cmd := openSessionTmux("ses_test123", "task_abc")
	if cmd == nil {
		t.Error("openSessionTmux should return a non-nil command")
	}
}

func TestFetchSessionsCmd_Success(t *testing.T) {
	// Create a test server that returns an entry with sessions
	entry := types.BrainEntry{
		ID:   "test-id",
		Path: "projects/test/task/abc.md",
		Sessions: map[string]types.SessionInfo{
			"ses_older":  {Timestamp: "2024-01-01T00:00:00Z"},
			"ses_newer":  {Timestamp: "2024-06-15T12:00:00Z"},
			"ses_newest": {Timestamp: "2024-12-25T18:00:00Z"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}))
	defer server.Close()

	client := runner.NewAPIClient(runner.RunnerConfig{
		BrainAPIURL: server.URL,
		APITimeout:  5000,
	})

	cmd := fetchSessionsCmd(client, "projects/test/task/abc.md", false)
	if cmd == nil {
		t.Fatal("fetchSessionsCmd should return a non-nil command")
	}

	// Execute the command
	msg := cmd()
	fetchedMsg, ok := msg.(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg, got %T", msg)
	}

	if fetchedMsg.err != nil {
		t.Fatalf("unexpected error: %v", fetchedMsg.err)
	}

	if len(fetchedMsg.sessionIDs) != 3 {
		t.Fatalf("expected 3 session IDs, got %d", len(fetchedMsg.sessionIDs))
	}

	// Should be sorted by timestamp descending (newest first)
	if fetchedMsg.sessionIDs[0] != "ses_newest" {
		t.Errorf("first session should be ses_newest (latest), got %q", fetchedMsg.sessionIDs[0])
	}
	if fetchedMsg.sessionIDs[1] != "ses_newer" {
		t.Errorf("second session should be ses_newer, got %q", fetchedMsg.sessionIDs[1])
	}
	if fetchedMsg.sessionIDs[2] != "ses_older" {
		t.Errorf("third session should be ses_older, got %q", fetchedMsg.sessionIDs[2])
	}

	if fetchedMsg.taskPath != "projects/test/task/abc.md" {
		t.Errorf("taskPath = %q, want %q", fetchedMsg.taskPath, "projects/test/task/abc.md")
	}

	if fetchedMsg.tmuxMode {
		t.Error("tmuxMode should be false")
	}
}

func TestFetchSessionsCmd_NoSessions(t *testing.T) {
	// Create a test server that returns an entry with no sessions
	entry := types.BrainEntry{
		ID:       "test-id",
		Path:     "projects/test/task/abc.md",
		Sessions: nil,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}))
	defer server.Close()

	client := runner.NewAPIClient(runner.RunnerConfig{
		BrainAPIURL: server.URL,
		APITimeout:  5000,
	})

	cmd := fetchSessionsCmd(client, "projects/test/task/abc.md", true)
	msg := cmd()
	fetchedMsg, ok := msg.(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg, got %T", msg)
	}

	if fetchedMsg.err == nil {
		t.Error("expected error for no sessions")
	}

	if fetchedMsg.sessionIDs != nil {
		t.Errorf("expected nil sessionIDs, got %v", fetchedMsg.sessionIDs)
	}

	if !fetchedMsg.tmuxMode {
		t.Error("tmuxMode should be true")
	}
}

func TestFetchSessionsCmd_APIError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := runner.NewAPIClient(runner.RunnerConfig{
		BrainAPIURL: server.URL,
		APITimeout:  5000,
	})

	cmd := fetchSessionsCmd(client, "projects/test/task/abc.md", false)
	msg := cmd()
	fetchedMsg, ok := msg.(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg, got %T", msg)
	}

	if fetchedMsg.err == nil {
		t.Error("expected error for API failure")
	}
}

func TestFetchSessionsCmd_TmuxModePassthrough(t *testing.T) {
	// Verify tmuxMode is passed through correctly
	entry := types.BrainEntry{
		ID:   "test-id",
		Path: "projects/test/task/abc.md",
		Sessions: map[string]types.SessionInfo{
			"ses_one": {Timestamp: "2024-01-01T00:00:00Z"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}))
	defer server.Close()

	client := runner.NewAPIClient(runner.RunnerConfig{
		BrainAPIURL: server.URL,
		APITimeout:  5000,
	})

	// Test with tmuxMode=true
	cmd := fetchSessionsCmd(client, "projects/test/task/abc.md", true)
	msg := cmd()
	fetchedMsg := msg.(sessionsFetchedMsg)

	if !fetchedMsg.tmuxMode {
		t.Error("tmuxMode should be true when passed as true")
	}
}
