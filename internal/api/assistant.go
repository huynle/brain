package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type AssistantPlanner interface {
	Plan(ctx context.Context, req AssistantPlanRequest) (AssistantPlanResponse, error)
}

type AssistantStreamingPlanner interface {
	AssistantPlanner
	StreamPlan(ctx context.Context, req AssistantPlanRequest, onDelta func(string) error) (AssistantPlanResponse, error)
}

type AssistantServiceOptions struct {
	Enabled   bool
	Provider  string
	BaseURL   string
	APIKeyEnv string
	Model     string
	Timeout   time.Duration
	Planner   AssistantPlanner
	Brain     BrainService
	Goals     GoalService
	Tasks     TaskService
	Runner    RunnerService
	Runners   RunnerRegistryService
	Events    EventService
	// MaxToolTurns caps how many tool-call iterations the agent loop will run
	// in a single Chat/ChatStream request. Zero uses a sensible default (6).
	MaxToolTurns int
}

type AssistantService struct {
	enabled      bool
	provider     string
	baseURL      string
	model        string
	planner      AssistantPlanner
	brain        BrainService
	goals        GoalService
	tasks        TaskService
	runner       RunnerService
	runners      RunnerRegistryService
	events       EventService
	maxToolTurns int
}

type AssistantStatusResponse struct {
	Available    bool     `json:"available"`
	Mode         string   `json:"mode"`
	Provider     string   `json:"provider,omitempty"`
	Model        string   `json:"model,omitempty"`
	Capabilities []string `json:"capabilities"`
	Reason       string   `json:"reason,omitempty"`
}

type AssistantChatRequest struct {
	Project     string            `json:"project,omitempty"`
	Message     string            `json:"message"`
	Model       string            `json:"model,omitempty"`
	Attachments []string          `json:"attachments,omitempty"`
	Context     map[string]string `json:"context,omitempty"`
}

type AssistantPlanRequest struct {
	Project     string            `json:"project,omitempty"`
	Message     string            `json:"message"`
	Model       string            `json:"model,omitempty"`
	Attachments []string          `json:"attachments,omitempty"`
	Context     map[string]string `json:"context,omitempty"`
	Mode        string            `json:"mode,omitempty"`
}

type AssistantPlanResponse struct {
	Reply   string            `json:"reply"`
	Actions []AssistantAction `json:"actions"`
}

type AssistantAction struct {
	Type     string         `json:"type"`
	Explicit bool           `json:"explicit"`
	Payload  map[string]any `json:"payload"`
}

type AssistantExecutedAction struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type AssistantChatResponse struct {
	Reply           string                    `json:"reply"`
	ExecutedActions []AssistantExecutedAction `json:"executed_actions"`
	ProposedActions []AssistantAction         `json:"proposed_actions"`
}

type AssistantStreamEvent struct {
	Type            string                    `json:"type"`
	Delta           string                    `json:"delta,omitempty"`
	Reply           string                    `json:"reply,omitempty"`
	ExecutedActions []AssistantExecutedAction `json:"executed_actions,omitempty"`
	ProposedActions []AssistantAction         `json:"proposed_actions,omitempty"`
	ToolCall        *ToolCallEvent            `json:"tool_call,omitempty"`
	ToolResult      *ToolResultEvent          `json:"tool_result,omitempty"`
	Error           string                    `json:"error,omitempty"`
}

type AssistantGoalDraftRequest struct {
	Project     string             `json:"project,omitempty"`
	Message     string             `json:"message"`
	Current     AssistantGoalDraft `json:"current,omitempty"`
	Attachments []string           `json:"attachments,omitempty"`
	Context     map[string]string  `json:"context,omitempty"`
}

type AssistantGoalDraft struct {
	Project       string   `json:"project,omitempty"`
	FeatureID     string   `json:"feature_id,omitempty"`
	Title         string   `json:"title,omitempty"`
	Criteria      string   `json:"criteria,omitempty"`
	Validation    string   `json:"validation,omitempty"`
	Workdir       string   `json:"workdir,omitempty"`
	TriggerSource string   `json:"trigger_source,omitempty"`
	Agent         string   `json:"agent,omitempty"`
	Model         string   `json:"model,omitempty"`
	Complete      []string `json:"complete_statuses,omitempty"`
	Blocked       []string `json:"blocked_statuses,omitempty"`
}

type AssistantGoalDraftResponse struct {
	Reply string             `json:"reply"`
	Draft AssistantGoalDraft `json:"draft"`
}

func NewAssistantService(opts AssistantServiceOptions) *AssistantService {
	planner := opts.Planner
	if planner == nil && opts.Enabled {
		planner = NewOpenRouterAssistantPlanner(opts.Provider, opts.BaseURL, opts.APIKeyEnv, opts.Model, opts.Timeout)
	}
	maxTurns := opts.MaxToolTurns
	if maxTurns <= 0 {
		maxTurns = 6
	}
	return &AssistantService{
		enabled:      opts.Enabled,
		provider:     firstNonEmptyString(opts.Provider, "openrouter"),
		baseURL:      firstNonEmptyString(opts.BaseURL, "https://openrouter.ai/api/v1"),
		model:        opts.Model,
		planner:      planner,
		brain:        opts.Brain,
		goals:        opts.Goals,
		tasks:        opts.Tasks,
		runner:       opts.Runner,
		runners:      opts.Runners,
		events:       opts.Events,
		maxToolTurns: maxTurns,
	}
}

const assistantModeAgentic = "agentic"
const assistantModeDirectLLM = "direct_llm" // retained for legacy planner status

func (s *AssistantService) Status() AssistantStatusResponse {
	toolNames := []string{}
	for _, t := range ListToolDefinitions() {
		toolNames = append(toolNames, t.Name)
	}
	// Legacy capability aliases still exposed for older PWA builds.
	caps := append([]string{"chat", "attachments"}, toolNames...)
	resp := AssistantStatusResponse{
		Available:    s != nil && s.enabled && s.planner != nil,
		Mode:         assistantModeAgentic,
		Capabilities: caps,
	}
	if s != nil {
		resp.Provider = s.provider
		resp.Model = s.model
	}
	if !resp.Available {
		resp.Mode = "manual"
		resp.Reason = "assistant is not configured"
	}
	return resp
}

func (s *AssistantService) Chat(ctx context.Context, req AssistantChatRequest) (AssistantChatResponse, error) {
	if s == nil || !s.enabled || s.planner == nil {
		return AssistantChatResponse{}, fmt.Errorf("assistant is not configured")
	}
	res, err := s.runAgentLoop(ctx, req, nil)
	if err != nil {
		return AssistantChatResponse{}, err
	}
	return AssistantChatResponse{
		Reply:           res.Reply,
		ExecutedActions: res.Executed,
		ProposedActions: res.Proposed,
	}, nil
}

func (s *AssistantService) ChatStream(ctx context.Context, req AssistantChatRequest, emit func(AssistantStreamEvent) error) error {
	if s == nil || !s.enabled || s.planner == nil {
		return fmt.Errorf("assistant is not configured")
	}
	res, err := s.runAgentLoop(ctx, req, emit)
	if err != nil {
		return err
	}
	return emit(AssistantStreamEvent{
		Type: "done", Reply: res.Reply,
		ExecutedActions: res.Executed, ProposedActions: res.Proposed,
	})
}

func (s *AssistantService) responseFromPlan(ctx context.Context, project string, plan AssistantPlanResponse) AssistantChatResponse {
	resp := AssistantChatResponse{Reply: plan.Reply}
	for _, action := range plan.Actions {
		if shouldExecuteAssistantAction(action) {
			executed := s.executeAction(ctx, project, action)
			resp.ExecutedActions = append(resp.ExecutedActions, executed)
			continue
		}
		resp.ProposedActions = append(resp.ProposedActions, action)
	}
	return resp
}

func (s *AssistantService) GoalDraft(ctx context.Context, req AssistantGoalDraftRequest) (AssistantGoalDraftResponse, error) {
	if s == nil || !s.enabled || s.planner == nil {
		return AssistantGoalDraftResponse{}, fmt.Errorf("assistant is not configured")
	}
	plan, err := s.planner.Plan(ctx, AssistantPlanRequest{Project: req.Project, Message: req.Message, Attachments: req.Attachments, Context: req.Context, Mode: "goal_draft"})
	if err != nil {
		return AssistantGoalDraftResponse{}, err
	}
	for _, action := range plan.Actions {
		if action.Type == "create_goal" {
			return AssistantGoalDraftResponse{Reply: plan.Reply, Draft: goalDraftFromPayload(req.Project, action.Payload)}, nil
		}
	}
	return AssistantGoalDraftResponse{Reply: plan.Reply, Draft: req.Current}, nil
}

func shouldExecuteAssistantAction(action AssistantAction) bool {
	if !action.Explicit {
		return false
	}
	switch action.Type {
	case "create_task", "create_goal", "create_entry", "create_automation":
		return true
	default:
		return false
	}
}

func (s *AssistantService) executeAction(ctx context.Context, defaultProject string, action AssistantAction) AssistantExecutedAction {
	switch action.Type {
	case "create_task", "create_entry", "create_automation":
		if s.brain == nil {
			return AssistantExecutedAction{Type: action.Type, Status: "failed", Error: "brain service is unavailable"}
		}
		req := createEntryRequestFromAction(defaultProject, action)
		res, err := s.brain.Save(ctx, req)
		if err != nil {
			return AssistantExecutedAction{Type: action.Type, Status: "failed", Error: err.Error()}
		}
		return AssistantExecutedAction{Type: action.Type, Status: "completed", Result: res}
	case "create_goal":
		if s.goals == nil {
			return AssistantExecutedAction{Type: action.Type, Status: "failed", Error: "goal service is unavailable"}
		}
		req := createGoalRequestFromAction(defaultProject, action)
		res, err := s.goals.CreateGoal(ctx, req)
		if err != nil {
			return AssistantExecutedAction{Type: action.Type, Status: "failed", Error: err.Error()}
		}
		return AssistantExecutedAction{Type: action.Type, Status: "completed", Result: res}
	default:
		return AssistantExecutedAction{Type: action.Type, Status: "failed", Error: "unsupported action"}
	}
}

func createEntryRequestFromAction(defaultProject string, action AssistantAction) types.CreateEntryRequest {
	entryType := strings.TrimPrefix(action.Type, "create_")
	if entryType == "automation" {
		entryType = "automation"
	}
	req := types.CreateEntryRequest{
		Type:                entryType,
		Project:             stringPayload(action.Payload, "project", defaultProject),
		Title:               stringPayload(action.Payload, "title", "Untitled"),
		Content:             stringPayload(action.Payload, "content", ""),
		Status:              stringPayload(action.Payload, "status", "pending"),
		FeatureID:           stringPayload(action.Payload, "feature_id", ""),
		UserOriginalRequest: stringPayload(action.Payload, "user_original_request", ""),
		DirectPrompt:        stringPayload(action.Payload, "direct_prompt", ""),
		Agent:               stringPayload(action.Payload, "agent", ""),
		Model:               stringPayload(action.Payload, "model", ""),
		Attachments:         attachmentRefsFromPayload(action.Payload),
	}
	if trigger := triggerFromPayload(action.Payload); trigger != nil {
		req.Trigger = trigger
	}
	if actionCfg := actionFromPayload(action.Payload); actionCfg != nil {
		req.Action = actionCfg
	}
	return req
}

func createGoalRequestFromAction(defaultProject string, action AssistantAction) types.CreateGoalRequest {
	draft := goalDraftFromPayload(defaultProject, action.Payload)
	goalID := slugForGoal(draft.Title)
	return types.CreateGoalRequest{
		Project:   draft.Project,
		FeatureID: draft.FeatureID,
		Title:     draft.Title,
		Content:   firstNonEmptyString(stringPayload(action.Payload, "content", ""), draft.Criteria, draft.Title),
		Config: types.GoalConfig{
			ID:               goalID,
			Criteria:         draft.Criteria,
			Validation:       draft.Validation,
			Workdir:          draft.Workdir,
			TriggerSource:    firstNonEmptyString(draft.TriggerSource, types.GoalTriggerSourceTask),
			CompleteStatuses: draft.Complete,
			BlockedStatuses:  draft.Blocked,
		},
		Action: types.AutomationAction{Type: "prompt", DirectPrompt: firstNonEmptyString(stringPayload(action.Payload, "direct_prompt", ""), draft.Criteria, draft.Title), Agent: draft.Agent, Model: draft.Model},
	}
}

func goalDraftFromPayload(defaultProject string, payload map[string]any) AssistantGoalDraft {
	return AssistantGoalDraft{
		Project:       stringPayload(payload, "project", defaultProject),
		FeatureID:     stringPayload(payload, "feature_id", ""),
		Title:         stringPayload(payload, "title", ""),
		Criteria:      stringPayload(payload, "criteria", ""),
		Validation:    stringPayload(payload, "validation", ""),
		Workdir:       stringPayload(payload, "workdir", ""),
		TriggerSource: stringPayload(payload, "trigger_source", ""),
		Agent:         stringPayload(payload, "agent", ""),
		Model:         stringPayload(payload, "model", ""),
		Complete:      stringSlicePayload(payload, "complete_statuses"),
		Blocked:       stringSlicePayload(payload, "blocked_statuses"),
	}
}

func attachmentRefsFromPayload(payload map[string]any) []types.AttachmentReference {
	vals := stringSlicePayload(payload, "attachments")
	if len(vals) == 0 {
		vals = stringSlicePayload(payload, "attachment_ids")
	}
	refs := make([]types.AttachmentReference, 0, len(vals))
	for _, id := range vals {
		refs = append(refs, types.AttachmentReference{ID: id, Role: "source"})
	}
	return refs
}

func triggerFromPayload(payload map[string]any) *types.TriggerConfig {
	raw, ok := payload["trigger"].(map[string]any)
	if !ok {
		return nil
	}
	trigger := &types.TriggerConfig{
		Type:     stringPayload(raw, "type", ""),
		Event:    stringPayload(raw, "event", ""),
		Schedule: stringPayload(raw, "schedule", ""),
	}
	if filter, ok := raw["filter"].(map[string]any); ok {
		trigger.Filter = map[string]string{}
		for k, v := range filter {
			if s, ok := v.(string); ok {
				trigger.Filter[k] = s
			}
		}
	}
	return trigger
}

func actionFromPayload(payload map[string]any) *types.AutomationAction {
	raw, ok := payload["action"].(map[string]any)
	if !ok {
		return nil
	}
	return &types.AutomationAction{
		Type:          stringPayload(raw, "type", ""),
		DirectPrompt:  stringPayload(raw, "direct_prompt", ""),
		Command:       stringPayload(raw, "command", ""),
		Agent:         stringPayload(raw, "agent", ""),
		Model:         stringPayload(raw, "model", ""),
		Executor:      stringPayload(raw, "executor", ""),
		TargetWorkdir: stringPayload(raw, "target_workdir", ""),
		ExecutionMode: stringPayload(raw, "execution_mode", ""),
	}
}

func stringPayload(payload map[string]any, key, fallback string) string {
	if payload == nil {
		return fallback
	}
	if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func stringSlicePayload(payload map[string]any, key string) []string {
	v, ok := payload[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func slugForGoal(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "goal"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type OpenRouterAssistantPlanner struct {
	provider string
	baseURL  string
	apiKey   string
	model    string
	client   *http.Client
}

func NewOpenRouterAssistantPlanner(provider, baseURL, apiKeyEnv, model string, timeout time.Duration) *OpenRouterAssistantPlanner {
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	return &OpenRouterAssistantPlanner{
		provider: firstNonEmptyString(provider, "openrouter"),
		baseURL:  firstNonEmptyString(baseURL, "https://openrouter.ai/api/v1"),
		apiKey:   os.Getenv(firstNonEmptyString(apiKeyEnv, "OPENROUTER_API_KEY")),
		model:    model,
		client:   &http.Client{Timeout: timeout},
	}
}

func (p *OpenRouterAssistantPlanner) Plan(ctx context.Context, req AssistantPlanRequest) (AssistantPlanResponse, error) {
	if p.apiKey == "" {
		return AssistantPlanResponse{}, fmt.Errorf("assistant API key is not configured")
	}
	model := firstNonEmptyString(req.Model, p.model)
	payload := map[string]any{
		"model":           model,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": assistantSystemPrompt()},
			{"role": "user", "content": mustJSON(req)},
		},
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AssistantPlanResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AssistantPlanResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AssistantPlanResponse{}, fmt.Errorf("assistant provider returned %s", resp.Status)
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return AssistantPlanResponse{}, err
	}
	if len(decoded.Choices) == 0 {
		return AssistantPlanResponse{}, fmt.Errorf("assistant provider returned no choices")
	}
	var plan AssistantPlanResponse
	if err := json.Unmarshal([]byte(decoded.Choices[0].Message.Content), &plan); err != nil {
		return AssistantPlanResponse{}, fmt.Errorf("decode assistant plan: %w", err)
	}
	return plan, nil
}

func (p *OpenRouterAssistantPlanner) StreamPlan(ctx context.Context, req AssistantPlanRequest, onDelta func(string) error) (AssistantPlanResponse, error) {
	if p.apiKey == "" {
		return AssistantPlanResponse{}, fmt.Errorf("assistant API key is not configured")
	}
	model := firstNonEmptyString(req.Model, p.model)
	payload := map[string]any{
		"model":           model,
		"stream":          true,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": assistantSystemPrompt()},
			{"role": "user", "content": mustJSON(req)},
		},
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AssistantPlanResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AssistantPlanResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AssistantPlanResponse{}, fmt.Errorf("assistant provider returned %s", resp.Status)
	}

	var content strings.Builder
	lastReply := ""
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		content.WriteString(chunk.Choices[0].Delta.Content)
		reply := extractAssistantReplyPrefix(content.String())
		if strings.HasPrefix(reply, lastReply) && len(reply) > len(lastReply) {
			if err := onDelta(reply[len(lastReply):]); err != nil {
				return AssistantPlanResponse{}, err
			}
			lastReply = reply
		}
	}
	if err := scanner.Err(); err != nil {
		return AssistantPlanResponse{}, err
	}
	var plan AssistantPlanResponse
	if err := json.Unmarshal([]byte(content.String()), &plan); err != nil {
		return AssistantPlanResponse{}, fmt.Errorf("decode assistant plan: %w", err)
	}
	if lastReply == "" && plan.Reply != "" {
		if err := onDelta(plan.Reply); err != nil {
			return AssistantPlanResponse{}, err
		}
	}
	return plan, nil
}

func extractAssistantReplyPrefix(input string) string {
	idx := strings.Index(input, `"reply"`)
	if idx < 0 {
		return ""
	}
	rest := input[idx+len(`"reply"`):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimLeft(rest[colon+1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	var out strings.Builder
	for i := 1; i < len(rest); i++ {
		ch := rest[i]
		if ch == '"' {
			return out.String()
		}
		if ch != '\\' {
			out.WriteByte(ch)
			continue
		}
		i++
		if i >= len(rest) {
			return out.String()
		}
		switch rest[i] {
		case '"', '\\', '/':
			out.WriteByte(rest[i])
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'u':
			// Avoid emitting malformed partial unicode escapes. The final full JSON
			// parse handles these exactly; streaming just waits for more bytes.
			if i+4 >= len(rest) {
				return out.String()
			}
			var decoded string
			if err := json.Unmarshal([]byte(`"\u`+rest[i+1:i+5]+`"`), &decoded); err == nil {
				out.WriteString(decoded)
			}
			i += 4
		default:
			out.WriteByte(rest[i])
		}
	}
	return out.String()
}

func assistantSystemPrompt() string {
	return `You are Brain's built-in assistant. You can both read and write the user's Brain via function/tool calls.

Behavior:
- If the user asks a factual/state question (e.g. "what automations do we have?", "why is task X stuck?"), CALL the relevant read tool first, then answer based on the result. Do not guess or invent state.
- If the user asks you to create/update something and it is safe (non-destructive), call the write tool directly. Report what happened in plain language.
- For destructive tools (delete_*, bulk_*, move_*, runner pause/resume, feature assign/clear), do NOT auto-execute. Describe what you would do and ask for confirmation. When the user then explicitly confirms, retry the same tool call with argument _explicit=true.
- The active project comes from the user's request context (see the JSON user message). Prefer that project unless the user names a different one.
- Keep replies short and direct. When tool results are lists, summarize with counts and the most relevant items; do not dump raw JSON. When a task is stuck, cite the fields that explain why (classification, blocked_by, waiting_on, in_cycle, resolved_workdir, next_run, schedule_enabled, dispatch_lease).
- If a tool returns an error, tell the user what went wrong and propose the next step. Do not silently retry the same call.

Tool categories:
- Reads: list_entries, get_entry, search_brain, list_tasks, get_task, get_task_metadata, list_features, get_feature, list_automations, list_goals, goal_progress, list_runners, runner_status, get_stats, get_backlinks, get_outlinks, get_related, get_sections, get_section, recent_events.
- Writes (auto-execute): create_task, create_entry, create_automation, create_goal, update_entry, update_task, update_automation, verify_entry, link_entry, run_goal, trigger_task, checkout_feature.
- Destructive (require _explicit=true after user confirmation): delete_entry, bulk_update, move_entry, pause_project, resume_project, pause_automations, resume_automations, assign_feature, clear_feature_assignment.

Never fabricate tool results. If you don't have a tool for something, say so.`
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (h *Handler) HandleAssistantStatus(w http.ResponseWriter, r *http.Request) {
	if h.assistant == nil {
		WriteJSON(w, http.StatusOK, (&AssistantService{}).Status())
		return
	}
	WriteJSON(w, http.StatusOK, h.assistant.Status())
}

func (h *Handler) HandleAssistantChat(w http.ResponseWriter, r *http.Request) {
	if h.assistant == nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "assistant is not configured")
		return
	}
	var req AssistantChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	resp, err := h.assistant.Chat(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleAssistantChatStream(w http.ResponseWriter, r *http.Request) {
	if h.assistant == nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "assistant is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming unsupported")
		return
	}
	var req AssistantChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	enc := json.NewEncoder(w)
	err := h.assistant.ChatStream(r.Context(), req, func(event AssistantStreamEvent) error {
		if err := enc.Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		_ = enc.Encode(AssistantStreamEvent{Type: "error", Error: err.Error()})
		flusher.Flush()
	}
}

func (h *Handler) HandleAssistantGoalDraft(w http.ResponseWriter, r *http.Request) {
	if h.assistant == nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "assistant is not configured")
		return
	}
	var req AssistantGoalDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	resp, err := h.assistant.GoalDraft(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}
