package api

// assistant_loop.go implements the agentic loop for the built-in assistant.
// See the top-level assistant.go and assistant_tools.go for the surrounding
// design.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// chatMessage models an OpenAI-compatible chat message. system/user/assistant
// carry Content; the assistant role additionally may carry ToolCalls; tool
// responses use role="tool" with ToolCallID pointing at the assistant call.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// toolSchema is the shape sent to OpenRouter under the request's `tools` key.
type toolSchema struct {
	Type     string                 `json:"type"`
	Function toolSchemaFunctionSpec `json:"function"`
}

type toolSchemaFunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

// buildToolSchemas converts ToolDefinitions into the OpenRouter tools payload.
func buildToolSchemas(defs []ToolDefinition) []toolSchema {
	out := make([]toolSchema, len(defs))
	for i, d := range defs {
		out[i] = toolSchema{
			Type: "function",
			Function: toolSchemaFunctionSpec{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Schema,
			},
		}
	}
	return out
}

// agentLoopResult is the return value of runAgentLoop: the final assistant
// text reply plus every tool that ran (or was deferred as proposed) during
// the loop.
type agentLoopResult struct {
	Reply    string
	Executed []AssistantExecutedAction
	Proposed []AssistantAction
}

// runAgentLoop drives the tool-calling loop against the assistant's planner
// backend. It emits stream events (delta/tool_call/tool_result) through the
// provided emit callback, and returns the aggregated result once the model
// produces a message without tool_calls or the loop hits its iteration cap.
//
// When the emit callback is nil, tool_call/tool_result events are suppressed
// but the final assistant text is still assembled and returned. This lets the
// non-streaming Chat handler reuse the same loop implementation.
func (s *AssistantService) runAgentLoop(
	ctx context.Context,
	req AssistantChatRequest,
	emit func(AssistantStreamEvent) error,
) (agentLoopResult, error) {
	planner, ok := s.planner.(*OpenRouterAssistantPlanner)
	if !ok {
		// Non-OpenRouter planners fall back to the legacy single-shot behavior.
		return s.runLegacyLoop(ctx, req, emit)
	}
	tools := ListToolDefinitions()
	index := toolIndex(tools)
	schemas := buildToolSchemas(tools)
	model := firstNonEmptyString(req.Model, s.model)

	msgs := []chatMessage{
		{Role: "system", Content: assistantSystemPrompt()},
		{Role: "user", Content: buildUserContent(req)},
	}

	result := agentLoopResult{}
	for turn := 0; turn < s.maxToolTurns; turn++ {
		choice, err := planner.callChat(ctx, model, msgs, schemas, emit)
		if err != nil {
			return result, err
		}
		// If the model returned tool_calls, execute each in order.
		if len(choice.ToolCalls) > 0 {
			// Preserve the assistant message with tool_calls in the conversation
			// so the model can see its own decision on the next turn.
			msgs = append(msgs, chatMessage{
				Role:      "assistant",
				Content:   choice.Content,
				ToolCalls: choice.ToolCalls,
			})
			for _, tc := range choice.ToolCalls {
				def, known := index[tc.Function.Name]
				if !known {
					msgs = append(msgs, chatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    fmt.Sprintf(`{"error":"unknown tool %q"}`, tc.Function.Name),
					})
					if emit != nil {
						_ = emit(AssistantStreamEvent{
							Type: "tool_result",
							ToolResult: &ToolResultEvent{
								ID: tc.ID, Name: tc.Function.Name,
								Status: "failed", Error: "unknown tool",
							},
						})
					}
					continue
				}
				resultVal, execRecord, proposed, toolErr := s.executeToolCall(
					ctx, req.Project, def, tc,
				)
				if execRecord != nil {
					result.Executed = append(result.Executed, *execRecord)
				}
				if proposed != nil {
					result.Proposed = append(result.Proposed, *proposed)
				}
				if emit != nil {
					args := json.RawMessage(tc.Function.Arguments)
					_ = emit(AssistantStreamEvent{
						Type: "tool_call",
						ToolCall: &ToolCallEvent{
							ID: tc.ID, Name: tc.Function.Name,
							Args: args, Tier: def.Tier.String(),
						},
					})
					evt := &ToolResultEvent{ID: tc.ID, Name: tc.Function.Name}
					switch {
					case toolErr != nil:
						evt.Status = "failed"
						evt.Error = toolErr.Error()
					case proposed != nil:
						evt.Status = "proposed"
						evt.Proposed = true
					default:
						evt.Status = "completed"
						evt.Result = summarizeToolResult(resultVal)
					}
					_ = emit(AssistantStreamEvent{Type: "tool_result", ToolResult: evt})
				}
				// Feed the tool response back to the model. Errors go as JSON
				// so the model can decide to retry or explain to the user.
				body := encodeToolResponse(resultVal, proposed, toolErr)
				msgs = append(msgs, chatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    body,
				})
			}
			continue
		}
		// Terminal turn: no tool calls, just an assistant reply.
		result.Reply = choice.Content
		return result, nil
	}
	// Hit the turn cap. Surface whatever text we have.
	result.Reply = firstNonEmptyString(result.Reply,
		"Reached tool-call limit before producing a final answer.")
	return result, nil
}

// executeToolCall dispatches a single tool call. It returns:
//   - the raw handler result (or nil on failure/proposal)
//   - an executed-action record if the call ran successfully
//   - a proposed-action record if a destructive call was gated
//   - an error, if any
//
// For TierDestructive tools, the model must set arg _explicit=true in the
// tool call arguments to run the action; otherwise a proposed-action stub is
// returned and the handler is not invoked.
func (s *AssistantService) executeToolCall(
	ctx context.Context,
	defaultProject string,
	def ToolDefinition,
	call toolCall,
) (any, *AssistantExecutedAction, *AssistantAction, error) {
	rawArgs := json.RawMessage(call.Function.Arguments)
	if def.Tier == TierDestructive {
		explicit := false
		if m := decodeArgs(rawArgs); m != nil {
			if b, ok := m["_explicit"].(bool); ok {
				explicit = b
			}
		}
		if !explicit {
			payload := map[string]any{}
			_ = json.Unmarshal(rawArgs, &payload)
			delete(payload, "_explicit")
			proposed := &AssistantAction{
				Type:     def.Name,
				Explicit: false,
				Payload:  payload,
			}
			return nil, nil, proposed, nil
		}
	}
	res, err := def.Handler(ctx, s, defaultProject, rawArgs)
	if err != nil {
		return nil, &AssistantExecutedAction{
			Type:   def.Name,
			Status: "failed",
			Error:  err.Error(),
		}, nil, err
	}
	// Only surface writes in ExecutedActions. Reads are shown as tool_result
	// stream events but don't clutter the ExecutedActions list.
	if def.Tier == TierRead {
		return res, nil, nil, nil
	}
	return res, &AssistantExecutedAction{
		Type:   def.Name,
		Status: "completed",
		Result: res,
	}, nil, nil
}

// buildUserContent renders the user turn as JSON so the model gets the full
// request including project, attachments, and PWA context. This mirrors the
// legacy planner behavior.
func buildUserContent(req AssistantChatRequest) string {
	return mustJSON(req)
}

// encodeToolResponse renders a tool result (or error) as JSON for the tool
// message content field. Result payload is size-capped to protect the
// context window.
func encodeToolResponse(result any, proposed *AssistantAction, err error) string {
	var payload any
	switch {
	case err != nil:
		payload = map[string]any{"error": err.Error()}
	case proposed != nil:
		payload = map[string]any{
			"status": "proposed",
			"note":   "This tool is destructive and was surfaced as a proposed action for user confirmation. Explain the intent and wait for approval before retrying with _explicit=true.",
		}
	default:
		payload = result
	}
	b, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Sprintf(`{"error":"encode tool result: %s"}`, marshalErr.Error())
	}
	// Cap to protect context. 12k characters is generous for structured JSON.
	const cap = 12000
	if len(b) > cap {
		return string(b[:cap]) + `..."<truncated>"}`
	}
	return string(b)
}

// summarizeToolResult trims large results before they go on the SSE stream.
// The full result still flows back to the model via encodeToolResponse; this
// is only for what the PWA renders in the tool_result chip.
func summarizeToolResult(result any) any {
	if result == nil {
		return nil
	}
	b, err := json.Marshal(result)
	if err != nil || len(b) <= 4000 {
		return result
	}
	// Too big: return a marker instead of the whole thing.
	return map[string]any{
		"summary":  "result truncated for streaming; assistant still received full result",
		"size":     len(b),
		"preview":  string(b[:800]),
	}
}

// chatChoice is a decoded OpenRouter choice.message payload.
type chatChoice struct {
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	Role      string     `json:"role,omitempty"`
}

// callChat streams a single completion from OpenRouter. When emit is non-nil
// and the choice contains natural-language text (no tool_calls), we emit each
// content delta as it arrives. Tool-call deltas are buffered until the choice
// finishes so we can execute the fully-formed call.
func (p *OpenRouterAssistantPlanner) callChat(
	ctx context.Context,
	model string,
	msgs []chatMessage,
	tools []toolSchema,
	emit func(AssistantStreamEvent) error,
) (chatChoice, error) {
	if p.apiKey == "" {
		return chatChoice{}, errors.New("assistant API key is not configured")
	}
	payload := map[string]any{
		"model":    model,
		"stream":   true,
		"messages": msgs,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.baseURL, "/")+"/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return chatChoice{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return chatChoice{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := readAll(resp.Body)
		return chatChoice{}, fmt.Errorf("assistant provider returned %s: %s", resp.Status, string(buf))
	}
	return consumeChatStream(resp.Body, emit)
}

// readAll is a tiny helper that avoids importing io just for this.
func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}

// consumeChatStream reads an OpenRouter SSE body, streaming text deltas to
// emit while accumulating tool calls until the choice finishes.
func consumeChatStream(body interface{ Read([]byte) (int, error) }, emit func(AssistantStreamEvent) error) (chatChoice, error) {
	return consumeSSE(body, emit)
}

// consumeSSE is the workhorse for parsing OpenRouter's streamed choices.
func consumeSSE(body interface{ Read([]byte) (int, error) }, emit func(AssistantStreamEvent) error) (chatChoice, error) {
	rd := readerAdapter{r: body}
	scanner := bufio.NewScanner(&rd)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var choice chatChoice
	// tool_calls arrive as sparse deltas (per-index). Track partial state by index.
	toolBuilders := map[int]*toolCallBuilder{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id,omitempty"`
						Type     string `json:"type,omitempty"`
						Function struct {
							Name      string `json:"name,omitempty"`
							Arguments string `json:"arguments,omitempty"`
						} `json:"function"`
					} `json:"tool_calls,omitempty"`
					Role string `json:"role,omitempty"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason,omitempty"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.Delta.Content != "" {
			choice.Content += ch.Delta.Content
			if emit != nil {
				_ = emit(AssistantStreamEvent{Type: "delta", Delta: ch.Delta.Content})
			}
		}
		for _, tc := range ch.Delta.ToolCalls {
			b, ok := toolBuilders[tc.Index]
			if !ok {
				b = &toolCallBuilder{}
				toolBuilders[tc.Index] = b
			}
			if tc.ID != "" {
				b.id = tc.ID
			}
			if tc.Type != "" {
				b.kind = tc.Type
			}
			if tc.Function.Name != "" {
				b.name += tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				b.args += tc.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return choice, err
	}
	if len(toolBuilders) > 0 {
		// Sort by index so tool calls execute in the order the model emitted.
		maxIdx := -1
		for i := range toolBuilders {
			if i > maxIdx {
				maxIdx = i
			}
		}
		for i := 0; i <= maxIdx; i++ {
			b, ok := toolBuilders[i]
			if !ok {
				continue
			}
			choice.ToolCalls = append(choice.ToolCalls, toolCall{
				ID:   firstNonEmptyString(b.id, fmt.Sprintf("call_%d", i)),
				Type: firstNonEmptyString(b.kind, "function"),
				Function: toolCallFunction{
					Name:      b.name,
					Arguments: b.args,
				},
			})
		}
	}
	return choice, nil
}

// toolCallBuilder accumulates the streamed pieces of a single tool call.
type toolCallBuilder struct {
	id   string
	kind string
	name string
	args string
}

// readerAdapter lets consumeSSE accept our narrow interface as an io.Reader.
type readerAdapter struct {
	r interface{ Read([]byte) (int, error) }
}

func (a *readerAdapter) Read(p []byte) (int, error) { return a.r.Read(p) }

// runLegacyLoop is used when the configured planner is not the OpenRouter
// planner (e.g., a test stub without tool-call support). It falls back to the
// old single-shot behavior so existing tests and non-tool-capable models keep
// working.
func (s *AssistantService) runLegacyLoop(
	ctx context.Context,
	req AssistantChatRequest,
	emit func(AssistantStreamEvent) error,
) (agentLoopResult, error) {
	planReq := AssistantPlanRequest{
		Project: req.Project, Message: req.Message, Model: req.Model,
		Attachments: req.Attachments, Context: req.Context,
	}
	var plan AssistantPlanResponse
	var err error
	if streamer, ok := s.planner.(AssistantStreamingPlanner); ok && emit != nil {
		plan, err = streamer.StreamPlan(ctx, planReq, func(delta string) error {
			if delta == "" {
				return nil
			}
			return emit(AssistantStreamEvent{Type: "delta", Delta: delta})
		})
	} else {
		plan, err = s.planner.Plan(ctx, planReq)
		if err == nil && plan.Reply != "" && emit != nil {
			_ = emit(AssistantStreamEvent{Type: "delta", Delta: plan.Reply})
		}
	}
	if err != nil {
		return agentLoopResult{}, err
	}
	resp := s.responseFromPlan(ctx, req.Project, plan)
	return agentLoopResult{
		Reply:    resp.Reply,
		Executed: resp.ExecutedActions,
		Proposed: resp.ProposedActions,
	}, nil
}
