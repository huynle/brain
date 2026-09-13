package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubSessionHistory overrides the transcript fetch indirection for the
// test's duration, mirroring stubSessionStatus. The real fetcher dials
// localhost / reads disk, which tests can't rely on.
func stubSessionHistory(t *testing.T, body []byte, err error) {
	t.Helper()
	prev := sessionHistoryForPort
	sessionHistoryForPort = func(int, string) ([]byte, error) { return body, err }
	t.Cleanup(func() { sessionHistoryForPort = prev })
}

// msgJSON builds a single {info, parts} transcript element. partTimes are
// added as parts each carrying {"time":{"created":<v>}}.
func msgJSON(t *testing.T, role string, created, completed float64, partTimes ...float64) messageWithParts {
	t.Helper()
	info := map[string]interface{}{
		"id":        fmt.Sprintf("msg_%s_%d", role, int64(created)),
		"sessionID": "ses_abc",
		"role":      role,
		"time": map[string]interface{}{
			"created":   created,
			"completed": completed,
		},
	}
	infoRaw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	parts := make([]json.RawMessage, 0, len(partTimes))
	for _, pt := range partTimes {
		p := map[string]interface{}{
			"time": map[string]interface{}{"created": pt},
		}
		praw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal part: %v", err)
		}
		parts = append(parts, json.RawMessage(praw))
	}
	return messageWithParts{Info: infoRaw, Parts: parts}
}

func transcriptJSON(t *testing.T, msgs ...messageWithParts) []byte {
	t.Helper()
	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("marshal transcript: %v", err)
	}
	return raw
}

func TestCheckOpencodeTurnEnded_CompletedAssistant(t *testing.T) {
	// user then assistant, assistant completed.
	body := transcriptJSON(t,
		msgJSON(t, "user", 1000, 1000),
		msgJSON(t, "assistant", 2000, 3000),
	)
	stubSessionHistory(t, body, nil)

	ended, last, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if !ended {
		t.Errorf("ended = false, want true (assistant completed)")
	}
	want := time.UnixMilli(3000).UTC()
	if !last.Equal(want) {
		t.Errorf("lastActivity = %v, want %v", last, want)
	}
}

func TestCheckOpencodeTurnEnded_InFlightAssistant(t *testing.T) {
	// assistant still in-flight: completed == 0.
	body := transcriptJSON(t,
		msgJSON(t, "user", 1000, 1000),
		msgJSON(t, "assistant", 2000, 0),
	)
	stubSessionHistory(t, body, nil)

	ended, last, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if ended {
		t.Errorf("ended = true, want false (assistant in-flight)")
	}
	want := time.UnixMilli(2000).UTC()
	if !last.Equal(want) {
		t.Errorf("lastActivity = %v, want %v", last, want)
	}
}

func TestCheckOpencodeTurnEnded_LaterUserMessageIgnored(t *testing.T) {
	// A user message created AFTER a completed assistant message must not
	// change ended — we key off the latest assistant message.
	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 3000),
		msgJSON(t, "user", 4000, 4000),
	)
	stubSessionHistory(t, body, nil)

	ended, last, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if !ended {
		t.Errorf("ended = false, want true (latest assistant completed)")
	}
	// lastActivity still spans all messages.
	want := time.UnixMilli(4000).UTC()
	if !last.Equal(want) {
		t.Errorf("lastActivity = %v, want %v", last, want)
	}
}

func TestCheckOpencodeTurnEnded_NoAssistantMessage(t *testing.T) {
	body := transcriptJSON(t,
		msgJSON(t, "user", 1000, 1000),
	)
	stubSessionHistory(t, body, nil)

	_, _, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if ok {
		t.Errorf("ok = true, want false (no assistant message)")
	}
}

func TestCheckOpencodeTurnEnded_FetchError(t *testing.T) {
	stubSessionHistory(t, nil, errors.New("boom"))

	_, _, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if ok {
		t.Errorf("ok = true, want false (fetch error)")
	}
}

func TestCheckOpencodeTurnEnded_UnmarshalError(t *testing.T) {
	stubSessionHistory(t, []byte("not json"), nil)

	_, _, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if ok {
		t.Errorf("ok = true, want false (unmarshal error)")
	}
}

func TestCheckOpencodeTurnEnded_PartTimeIsNewest(t *testing.T) {
	// Part time newer than any message time drives lastActivity.
	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 3000, 5000),
	)
	stubSessionHistory(t, body, nil)

	ended, last, ok := checkOpencodeTurnEnded(1234, "ses_abc")
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if !ended {
		t.Errorf("ended = false, want true")
	}
	want := time.UnixMilli(5000).UTC()
	if !last.Equal(want) {
		t.Errorf("lastActivity = %v, want %v (part newer than message)", last, want)
	}
}

// Integration: /session/status reports busy but the transcript shows the
// assistant turn completed. Idle timer must ADVANCE (was previously cleared).
func TestCheckIdleStatus_BusyButTurnEnded_SetsIdleSince(t *testing.T) {
	// Busy status server (non-empty map).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ses_abc": map[string]interface{}{"type": "busy"},
		})
	}))
	defer server.Close()
	port := serverPort(t, server)

	// Transcript: assistant completed.
	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 3000),
	)
	stubSessionHistory(t, body, nil)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 60000 // 60s — won't trigger completion yet

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		SessionID:      "ses_abc",
		IdleSince:      "", // not yet idle
	}
	processMgr.Add("task1", task, proc)

	tr.checkIdleStatus(context.Background())

	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.IdleSince == "" {
		t.Error("IdleSince should be set: busy status but turn ended")
	}
	// Threshold not reached, no status update.
	if updates := client.getUpdateStatusCalls(); len(updates) > 0 {
		t.Errorf("should not update status before threshold, got: %+v", updates)
	}
}

// Integration: busy AND turn ended AND idle threshold exceeded ⇒ completion.
func TestCheckIdleStatus_BusyButTurnEnded_ThresholdExceeded_Completes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ses_abc": map[string]interface{}{"type": "busy"},
		})
	}))
	defer server.Close()
	port := serverPort(t, server)

	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 3000),
	)
	stubSessionHistory(t, body, nil)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 100 // 100ms — easy to exceed

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	// IdleSince already set beyond threshold.
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		SessionID:      "ses_abc",
		IdleSince:      time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339),
	}
	processMgr.Add("task1", task, proc)

	tr.checkIdleStatus(context.Background())

	updates := client.getUpdateStatusCalls()
	if len(updates) == 0 {
		t.Fatal("expected a status update (completed) after threshold exceeded with turn ended")
	}
	last := updates[len(updates)-1]
	if last.Status != "completed" {
		t.Errorf("status update = %q, want completed", last.Status)
	}
}

// Integration: busy AND turn NOT ended ⇒ old behavior, IdleSince cleared.
func TestCheckIdleStatus_BusyAndTurnNotEnded_ClearsIdleSince(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ses_abc": map[string]interface{}{"type": "busy"},
		})
	}))
	defer server.Close()
	port := serverPort(t, server)

	// Assistant in-flight (completed == 0) ⇒ turn NOT ended.
	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 0),
	)
	stubSessionHistory(t, body, nil)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		SessionID:      "ses_abc",
		IdleSince:      time.Now().Add(-10 * time.Second).Format(time.RFC3339),
	}
	processMgr.Add("task1", task, proc)

	tr.checkIdleStatus(context.Background())

	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.IdleSince != "" {
		t.Errorf("IdleSince should be cleared: busy and turn not ended, got %q", info.Task.IdleSince)
	}
}
