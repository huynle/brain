package api

// Agent-loop tests. These exercise runAgentLoop end-to-end against a scripted
// httptest server that speaks OpenAI-compatible SSE. The goal is to prove:
//
//  1. Tool calls emitted by the model are dispatched to the registered
//     handlers, their result is fed back as a role="tool" message, and the
//     loop continues until a plain assistant message ends it.
//  2. Delta content is streamed through the emit callback.
//  3. The iteration cap is respected.
//  4. Destructive tools without _explicit are surfaced as proposed actions.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// scriptedOpenRouter serves a sequence of pre-canned SSE responses so tests
// can walk the loop turn by turn.
type scriptedOpenRouter struct {
	mu       sync.Mutex
	turns    []string      // raw SSE body for each successive request
	seen     [][]byte      // captured request bodies (for assertions)
	callback func(int, []byte)
}

func newScriptedOpenRouter(turns []string) *scriptedOpenRouter {
	return &scriptedOpenRouter{turns: turns}
}

func (s *scriptedOpenRouter) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		s.seen = append(s.seen, body.Bytes())
		if s.callback != nil {
			s.callback(len(s.seen)-1, body.Bytes())
		}
		if len(s.turns) == 0 {
			t.Errorf("scripted server ran out of turns")
			http.Error(w, "no more turns", http.StatusInternalServerError)
			return
		}
		next := s.turns[0]
		s.turns = s.turns[1:]
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, line := range strings.Split(next, "\n") {
			_, _ = fmt.Fprintln(w, line)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
}

// makeSSE renders a raw SSE payload for a single OpenRouter response by
// listing individual data-frames as strings.
func makeSSE(frames ...string) string {
	var b strings.Builder
	for _, f := range frames {
		b.WriteString("data: ")
		b.WriteString(f)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// Turn 1: model asks for list_automations.
// Turn 2: model produces a natural-language reply summarizing the result.
func TestRunAgentLoop_ExecutesToolThenAnswers(t *testing.T) {
	turn1 := makeSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_automations","arguments":"{}"}}]}}]}`,
	)
	turn2 := makeSSE(
		`{"choices":[{"delta":{"content":"You have 2 active automations: Nightly cron and Weekly review."}}]}`,
	)
	scripted := newScriptedOpenRouter([]string{turn1, turn2})
	server := httptest.NewServer(scripted.handler(t))
	defer server.Close()
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	brain := &mockBrainService{listFunc: func(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
		if req.Type != "automation" || req.Project != "prod" {
			t.Fatalf("list called with %+v, want type=automation project=prod", req)
		}
		return &types.ListEntriesResponse{Total: 2, Entries: []types.BrainEntry{
			{ID: "a1", Title: "Nightly cron", Type: "automation", Status: "active"},
			{ID: "a2", Title: "Weekly review", Type: "automation", Status: "active"},
		}}, nil
	}}
	planner := NewOpenRouterAssistantPlanner("openrouter", server.URL, "BRAIN_TEST_OPENROUTER_KEY", "test-model", 0)
	svc := NewAssistantService(AssistantServiceOptions{
		Enabled: true, Planner: planner, Brain: brain,
		MaxToolTurns: 6,
	})

	events := []AssistantStreamEvent{}
	emit := func(e AssistantStreamEvent) error {
		events = append(events, e)
		return nil
	}
	res, err := svc.runAgentLoop(context.Background(), AssistantChatRequest{
		Project: "prod",
		Message: "what automations do we have?",
	}, emit)
	if err != nil {
		t.Fatalf("runAgentLoop err = %v", err)
	}
	if !strings.Contains(res.Reply, "Nightly cron") {
		t.Fatalf("reply = %q, want summary mentioning Nightly cron", res.Reply)
	}
	// Verify events: at least one tool_call, one tool_result, and deltas.
	var sawToolCall, sawToolResult, sawDelta bool
	for _, e := range events {
		switch e.Type {
		case "tool_call":
			sawToolCall = true
			if e.ToolCall == nil || e.ToolCall.Name != "list_automations" {
				t.Fatalf("tool_call event = %+v", e.ToolCall)
			}
			if e.ToolCall.Tier != "read" {
				t.Fatalf("tool_call tier = %q, want read", e.ToolCall.Tier)
			}
		case "tool_result":
			sawToolResult = true
			if e.ToolResult == nil || e.ToolResult.Status != "completed" {
				t.Fatalf("tool_result event = %+v", e.ToolResult)
			}
		case "delta":
			sawDelta = true
		}
	}
	if !sawToolCall || !sawToolResult || !sawDelta {
		t.Fatalf("events missing coverage: toolCall=%v toolResult=%v delta=%v", sawToolCall, sawToolResult, sawDelta)
	}
	// The scripted server should have received two chat requests (loop iterations).
	if len(scripted.seen) != 2 {
		t.Fatalf("chat call count = %d, want 2", len(scripted.seen))
	}
	// The second request should contain the tool response as a role="tool" message.
	var payload struct {
		Messages []struct {
			Role       string `json:"role"`
			Name       string `json:"name"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(scripted.seen[1], &payload); err != nil {
		t.Fatalf("decode 2nd request body: %v", err)
	}
	foundTool := false
	for _, m := range payload.Messages {
		if m.Role == "tool" && m.Name == "list_automations" && m.ToolCallID == "call_1" {
			foundTool = true
			if !strings.Contains(m.Content, "Nightly cron") {
				t.Fatalf("tool content missing result: %q", m.Content)
			}
		}
	}
	if !foundTool {
		t.Fatalf("2nd request did not include tool response message: %+v", payload.Messages)
	}
}

func TestRunAgentLoop_DestructiveWithoutExplicit_Proposed(t *testing.T) {
	turn1 := makeSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"delete_entry","arguments":"{\"path_or_id\":\"projects/x/task/abc.md\"}"}}]}}]}`,
	)
	turn2 := makeSSE(
		`{"choices":[{"delta":{"content":"This would delete task abc. Reply confirm to proceed."}}]}`,
	)
	scripted := newScriptedOpenRouter([]string{turn1, turn2})
	server := httptest.NewServer(scripted.handler(t))
	defer server.Close()
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	brain := &mockBrainService{deleteFunc: func(_ context.Context, _ string) error {
		t.Fatal("delete_entry should not be invoked without _explicit=true")
		return nil
	}}
	planner := NewOpenRouterAssistantPlanner("openrouter", server.URL, "BRAIN_TEST_OPENROUTER_KEY", "test-model", 0)
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: planner, Brain: brain, MaxToolTurns: 4})

	res, err := svc.runAgentLoop(context.Background(), AssistantChatRequest{Project: "x", Message: "delete task abc"}, nil)
	if err != nil {
		t.Fatalf("runAgentLoop err = %v", err)
	}
	if len(res.Proposed) != 1 || res.Proposed[0].Type != "delete_entry" {
		t.Fatalf("proposed = %+v, want single delete_entry", res.Proposed)
	}
	if !strings.Contains(res.Reply, "delete task abc") {
		t.Fatalf("reply = %q, want plain-language explanation", res.Reply)
	}
}

func TestRunAgentLoop_TurnCapEnforced(t *testing.T) {
	// Model asks for list_automations on every turn, never terminates.
	loopTurn := makeSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_automations","arguments":"{}"}}]}}]}`,
	)
	turns := []string{loopTurn, loopTurn, loopTurn}
	scripted := newScriptedOpenRouter(turns)
	server := httptest.NewServer(scripted.handler(t))
	defer server.Close()
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	brain := &mockBrainService{listFunc: func(_ context.Context, _ types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
		return &types.ListEntriesResponse{}, nil
	}}
	planner := NewOpenRouterAssistantPlanner("openrouter", server.URL, "BRAIN_TEST_OPENROUTER_KEY", "test-model", 0)
	// Cap at 3 turns to match the scripted script size.
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: planner, Brain: brain, MaxToolTurns: 3})

	res, err := svc.runAgentLoop(context.Background(), AssistantChatRequest{Project: "prod", Message: "loop"}, nil)
	if err != nil {
		t.Fatalf("runAgentLoop err = %v", err)
	}
	if !strings.Contains(res.Reply, "Reached tool-call limit") {
		t.Fatalf("reply = %q, want turn-cap notice", res.Reply)
	}
	if len(scripted.seen) != 3 {
		t.Fatalf("chat calls = %d, want 3 (turn cap)", len(scripted.seen))
	}
}

// ─── replayHistory unit tests ─────────────────────────────────────────

func TestReplayHistory_DropsUnpairedToolCalls(t *testing.T) {
	// Assistant turn declares two tool_calls but only one has a matching
	// role="tool" reply. The unpaired call must be stripped, otherwise
	// OpenRouter rejects the request.
	history := []AssistantHistoryMessage{
		{Role: "user", Content: "check automations"},
		{Role: "assistant", Content: "looking", ToolCalls: []AssistantHistoryToolCall{
			{ID: "c1", Name: "list_automations", Arguments: "{}"},
			{ID: "orphan", Name: "list_tasks", Arguments: "{}"},
		}},
		{Role: "tool", ToolCallID: "c1", Name: "list_automations", Status: "completed"},
	}
	msgs := replayHistory(history)
	// Expected: user, assistant (with c1 only), tool for c1.
	if len(msgs) != 3 {
		t.Fatalf("msgs = %d, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "check automations" {
		t.Fatalf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" {
		t.Fatalf("msgs[1] role = %q", msgs[1].Role)
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "c1" {
		t.Fatalf("assistant tool_calls = %+v, want just c1", msgs[1].ToolCalls)
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "c1" {
		t.Fatalf("msgs[2] = %+v", msgs[2])
	}
	// The stripped tool body must include a note so the model knows the
	// payload was omitted.
	if !strings.Contains(msgs[2].Content, "omitted") {
		t.Fatalf("tool content = %q, want stripped-payload note", msgs[2].Content)
	}
}

func TestReplayHistory_EmptyReturnsNil(t *testing.T) {
	if replayHistory(nil) != nil {
		t.Fatalf("nil history should return nil, got non-nil")
	}
	if replayHistory([]AssistantHistoryMessage{}) != nil {
		t.Fatalf("empty history should return nil, got non-nil")
	}
}

// End-to-end: server must forward the caller's history into the OpenRouter
// messages array, ahead of the new user turn.
func TestRunAgentLoop_ForwardsHistory(t *testing.T) {
	turn := makeSSE(
		`{"choices":[{"delta":{"content":"applied 1-5 to the cron."}}]}`,
	)
	scripted := newScriptedOpenRouter([]string{turn})
	server := httptest.NewServer(scripted.handler(t))
	defer server.Close()
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	planner := NewOpenRouterAssistantPlanner("openrouter", server.URL, "BRAIN_TEST_OPENROUTER_KEY", "test-model", 0)
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: planner, Brain: &mockBrainService{}, MaxToolTurns: 3})

	history := []AssistantHistoryMessage{
		{Role: "user", Content: "did you set it to Mon-Fri only?"},
		{Role: "assistant", Content: "no, it's every day. Want me to update it to */5 * * * 1-5?"},
	}
	_, err := svc.runAgentLoop(context.Background(), AssistantChatRequest{
		Project: "prod", Message: "yes please", History: history,
	}, nil)
	if err != nil {
		t.Fatalf("runAgentLoop err = %v", err)
	}
	if len(scripted.seen) != 1 {
		t.Fatalf("chat calls = %d, want 1", len(scripted.seen))
	}
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(scripted.seen[0], &payload); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	// Expected order: system, user(hist), assistant(hist), user(new).
	if len(payload.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(payload.Messages), payload.Messages)
	}
	if payload.Messages[0].Role != "system" {
		t.Fatalf("msg[0] role = %q, want system", payload.Messages[0].Role)
	}
	if payload.Messages[1].Role != "user" || !strings.Contains(payload.Messages[1].Content, "Mon-Fri") {
		t.Fatalf("msg[1] = %+v, want user history", payload.Messages[1])
	}
	if payload.Messages[2].Role != "assistant" || !strings.Contains(payload.Messages[2].Content, "every day") {
		t.Fatalf("msg[2] = %+v, want assistant history", payload.Messages[2])
	}
	if payload.Messages[3].Role != "user" || !strings.Contains(payload.Messages[3].Content, "yes please") {
		t.Fatalf("msg[3] = %+v, want current user turn", payload.Messages[3])
	}
	// Sanity: the current user turn is the JSON-encoded request; make sure
	// the history field itself isn't echoed back into the message body.
	if strings.Contains(payload.Messages[3].Content, "\"history\"") {
		t.Fatalf("current user content leaked history field: %q", payload.Messages[3].Content)
	}
}

// Cancellation: if the caller's context is already cancelled when the loop
// starts, the very first ctx.Err() check should return before we make a
// single OpenRouter call. This proves the client-abort path stops server
// work cleanly.
func TestRunAgentLoop_RespectsCancelledContext(t *testing.T) {
	scripted := newScriptedOpenRouter(nil) // no turns available
	server := httptest.NewServer(scripted.handler(t))
	defer server.Close()
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	planner := NewOpenRouterAssistantPlanner("openrouter", server.URL, "BRAIN_TEST_OPENROUTER_KEY", "test-model", 0)
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: planner, Brain: &mockBrainService{}, MaxToolTurns: 6})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.runAgentLoop(ctx, AssistantChatRequest{Project: "prod", Message: "hi"}, nil)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), "canceled") && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context cancellation", err)
	}
	if len(scripted.seen) != 0 {
		t.Fatalf("scripted server got %d calls, want 0 (loop should have bailed)", len(scripted.seen))
	}
}
