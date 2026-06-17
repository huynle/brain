package api

import (
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

const assistantModeDirectLLM = "direct_llm"

type AssistantPlanner interface {
	Plan(ctx context.Context, req AssistantPlanRequest) (AssistantPlanResponse, error)
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
}

type AssistantService struct {
	enabled  bool
	provider string
	baseURL  string
	model    string
	planner  AssistantPlanner
	brain    BrainService
	goals    GoalService
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
	Attachments []string          `json:"attachments,omitempty"`
	Context     map[string]string `json:"context,omitempty"`
}

type AssistantPlanRequest struct {
	Project     string            `json:"project,omitempty"`
	Message     string            `json:"message"`
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
	return &AssistantService{
		enabled:  opts.Enabled,
		provider: firstNonEmptyString(opts.Provider, "openrouter"),
		baseURL:  firstNonEmptyString(opts.BaseURL, "https://openrouter.ai/api/v1"),
		model:    opts.Model,
		planner:  planner,
		brain:    opts.Brain,
		goals:    opts.Goals,
	}
}

func (s *AssistantService) Status() AssistantStatusResponse {
	resp := AssistantStatusResponse{
		Available:    s != nil && s.enabled && s.planner != nil,
		Mode:         assistantModeDirectLLM,
		Capabilities: []string{"chat", "create_goal", "create_task", "create_automation", "create_entry", "attachments"},
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
	plan, err := s.planner.Plan(ctx, AssistantPlanRequest{Project: req.Project, Message: req.Message, Attachments: req.Attachments, Context: req.Context})
	if err != nil {
		return AssistantChatResponse{}, err
	}
	resp := AssistantChatResponse{Reply: plan.Reply}
	for _, action := range plan.Actions {
		if shouldExecuteAssistantAction(action) {
			executed := s.executeAction(ctx, req.Project, action)
			resp.ExecutedActions = append(resp.ExecutedActions, executed)
			continue
		}
		resp.ProposedActions = append(resp.ProposedActions, action)
	}
	return resp, nil
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
	payload := map[string]any{
		"model":           p.model,
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

func assistantSystemPrompt() string {
	return `You are Brain's built-in assistant. Return only JSON matching {"reply":"...","actions":[{"type":"create_task|create_goal|create_entry|create_automation","explicit":true|false,"payload":{...}}]}. Mark explicit true only when the user directly asks to create/save/make/add a Brain object now. For ambiguous planning or drafting, explicit must be false. If the user JSON includes attachment IDs and the action creates a task, entry, or automation, include those IDs in payload.attachments.`
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
