package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestFormatServerRequestLine(t *testing.T) {
	rec := types.ServerRequestRecord{
		Time:       1_700_000_000_000,
		Method:     "POST",
		Path:       "/api/v1/runners/runner_abc/heartbeat",
		Status:     200,
		DurationMs: 3,
		ActorType:  "api_token",
		ActorName:  "runner",
	}
	line := formatServerRequestLine(rec, 120)
	for _, want := range []string{"POST", "200", "runner", "/api/v1/runners/runner_abc/heartbeat", "3ms"} {
		if !strings.Contains(line, want) {
			t.Errorf("line missing %q: %s", want, line)
		}
	}
}

func TestFormatServerRequestLine_FallsBackToAnon(t *testing.T) {
	line := formatServerRequestLine(types.ServerRequestRecord{Method: "GET", Path: "/x", Status: 200}, 120)
	if !strings.Contains(line, "anon") {
		t.Errorf("expected anon actor fallback: %s", line)
	}
}

func TestRenderTaskLogLines(t *testing.T) {
	lines := []types.LogLine{
		{Timestamp: "2026-06-13T12:00:00Z", Level: "info", Content: "starting build"},
		{Timestamp: "2026-06-13T12:00:01Z", Level: "error", Content: "compile failed"},
	}
	out := renderTaskLogLines(lines, 120, 20)
	for _, want := range []string{"starting build", "compile failed", "INFO", "ERROR"} {
		if !strings.Contains(out, want) {
			t.Errorf("task log output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderServerRequestsPanel(t *testing.T) {
	m := NewModel(Config{APIURL: "http://localhost:3333", Project: "all"})
	m.serverRequests = []types.ServerRequestRecord{
		{Time: 1_700_000_000_000, Method: "GET", Path: "/api/v1/tasks/demo/next", Status: 200, ActorName: "runner"},
		{Time: 1_700_000_001_000, Method: "POST", Path: "/api/v1/events", Status: 202, ActorName: "runner"},
	}
	out := m.renderServerRequestsPanel(120, 20)
	if !strings.Contains(out, "/api/v1/events") {
		t.Errorf("expected request path in panel output:\n%s", out)
	}

	// Empty state should not panic and shows a hint.
	m.serverRequests = nil
	empty := m.renderServerRequestsPanel(120, 20)
	if !strings.Contains(empty, "No requests yet") {
		t.Errorf("expected empty-state hint, got:\n%s", empty)
	}
}
