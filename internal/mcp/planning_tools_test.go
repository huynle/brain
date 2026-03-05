package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestRegisterPlanningTools_Count(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	count := len(s.tools)
	if count != 9 {
		t.Errorf("expected 9 planning tools registered, got %d", count)
	}
}

func TestRegisterPlanningTools_Names(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	expectedTools := []string{
		"plan_phase",
		"plan_discover_docs",
		"plan_confirm_docs",
		"plan_capture_intent",
		"plan_record_arch_check",
		"plan_propose_doc_updates",
		"plan_gate",
		"plan_start",
		"plan_compliance_report",
	}

	for _, name := range expectedTools {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisterPlanningTools_AllHandlersSet(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	for name, rt := range s.tools {
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
	}
}

func TestRegisterPlanningTools_Descriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	for name, rt := range s.tools {
		if rt.tool.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want %q", name, rt.tool.InputSchema.Type, "object")
		}
	}
}

func TestPlanningToolsDoNotOverlapOtherTools(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)
	RegisterTaskTools(s, client)

	existingCount := len(s.tools)

	RegisterPlanningTools(s, client)

	totalCount := len(s.tools)
	planningToolCount := totalCount - existingCount

	if planningToolCount != 9 {
		t.Errorf("expected 9 new planning tools (no overlap), got %d new tools", planningToolCount)
	}
}

// =============================================================================
// Schema Tests
// =============================================================================

func TestPlanPhase_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_phase"].tool

	// Required: action
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "action" {
		t.Errorf("plan_phase required = %v, want [action]", tool.InputSchema.Required)
	}

	// Check action enum
	actionProp := tool.InputSchema.Properties["action"]
	expectedActions := []string{"start", "transition", "status", "skip", "reset"}
	if len(actionProp.Enum) != len(expectedActions) {
		t.Errorf("action enum has %d values, want %d", len(actionProp.Enum), len(expectedActions))
	}

	// Check to enum
	toProp := tool.InputSchema.Properties["to"]
	expectedTo := []string{"init", "understand", "design", "approve", "idle"}
	if len(toProp.Enum) != len(expectedTo) {
		t.Errorf("to enum has %d values, want %d", len(toProp.Enum), len(expectedTo))
	}

	// Check optional properties exist
	for _, prop := range []string{"objective", "to", "reason"} {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("plan_phase missing property %q", prop)
		}
	}
}

func TestPlanDiscoverDocs_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_discover_docs"].tool

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("plan_discover_docs required = %v, want []", tool.InputSchema.Required)
	}

	// Check properties exist
	for _, prop := range []string{"prdPath", "archPath", "additionalDirs", "planQuery", "planId"} {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("plan_discover_docs missing property %q", prop)
		}
	}

	// Check additionalDirs is array
	adProp := tool.InputSchema.Properties["additionalDirs"]
	if adProp.Type != "array" {
		t.Errorf("additionalDirs type = %q, want array", adProp.Type)
	}
}

func TestPlanConfirmDocs_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_confirm_docs"].tool

	// Required: prdSelection, archSelection, existingPlan
	if len(tool.InputSchema.Required) != 3 {
		t.Errorf("plan_confirm_docs required fields = %d, want 3", len(tool.InputSchema.Required))
	}

	for _, req := range []string{"prdSelection", "archSelection", "existingPlan"} {
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("plan_confirm_docs missing required field %q", req)
		}
	}
}

func TestPlanCaptureIntent_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_capture_intent"].tool

	// Required: problem, success_criteria, scope_in, scope_out, constraints
	expectedRequired := []string{"problem", "success_criteria", "scope_in", "scope_out", "constraints"}
	if len(tool.InputSchema.Required) != len(expectedRequired) {
		t.Errorf("plan_capture_intent required fields = %d, want %d", len(tool.InputSchema.Required), len(expectedRequired))
	}

	for _, req := range expectedRequired {
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("plan_capture_intent missing required field %q", req)
		}
	}

	// Check array types
	for _, prop := range []string{"success_criteria", "scope_in", "scope_out", "constraints"} {
		p := tool.InputSchema.Properties[prop]
		if p.Type != "array" {
			t.Errorf("plan_capture_intent %s type = %q, want array", prop, p.Type)
		}
	}
}

func TestPlanRecordArchCheck_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_record_arch_check"].tool

	// Required: aligned, conflicts, gaps, recommendations
	expectedRequired := []string{"aligned", "conflicts", "gaps", "recommendations"}
	if len(tool.InputSchema.Required) != len(expectedRequired) {
		t.Errorf("plan_record_arch_check required fields = %d, want %d", len(tool.InputSchema.Required), len(expectedRequired))
	}

	// All should be arrays
	for _, prop := range expectedRequired {
		p := tool.InputSchema.Properties[prop]
		if p.Type != "array" {
			t.Errorf("plan_record_arch_check %s type = %q, want array", prop, p.Type)
		}
	}
}

func TestPlanProposeDocUpdates_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_propose_doc_updates"].tool

	expectedRequired := []string{
		"prd_section_title", "prd_requirements", "prd_rationale",
		"arch_decision_title", "arch_context", "arch_decision", "arch_consequences",
	}
	if len(tool.InputSchema.Required) != len(expectedRequired) {
		t.Errorf("plan_propose_doc_updates required fields = %d, want %d", len(tool.InputSchema.Required), len(expectedRequired))
	}
}

func TestPlanGate_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_gate"].tool

	// Required: action
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "action" {
		t.Errorf("plan_gate required = %v, want [action]", tool.InputSchema.Required)
	}

	// Check action enum
	actionProp := tool.InputSchema.Properties["action"]
	expectedActions := []string{"status", "set_enforcement", "set_objective", "reset"}
	if len(actionProp.Enum) != len(expectedActions) {
		t.Errorf("action enum has %d values, want %d", len(actionProp.Enum), len(expectedActions))
	}

	// Check level enum
	levelProp := tool.InputSchema.Properties["level"]
	if len(levelProp.Enum) != 2 {
		t.Errorf("level enum has %d values, want 2", len(levelProp.Enum))
	}

	// Legacy description
	if !strings.Contains(tool.Description, "LEGACY") {
		t.Errorf("plan_gate description should contain LEGACY, got: %s", tool.Description)
	}
}

func TestPlanStart_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_start"].tool

	// Required: objective
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "objective" {
		t.Errorf("plan_start required = %v, want [objective]", tool.InputSchema.Required)
	}

	// Legacy description
	if !strings.Contains(tool.Description, "LEGACY") {
		t.Errorf("plan_start description should contain LEGACY, got: %s", tool.Description)
	}
}

func TestPlanComplianceReport_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterPlanningTools(s, client)

	tool := s.tools["plan_compliance_report"].tool

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("plan_compliance_report required = %v, want []", tool.InputSchema.Required)
	}

	// Check format enum
	formatProp := tool.InputSchema.Properties["format"]
	if len(formatProp.Enum) != 3 {
		t.Errorf("format enum has %d values, want 3", len(formatProp.Enum))
	}
}

// =============================================================================
// Planning State Tests
// =============================================================================

func TestPlanningState_InitialState(t *testing.T) {
	state := newPlanningState()
	if state.Phase != "idle" {
		t.Errorf("initial phase = %q, want idle", state.Phase)
	}
	if state.EnforcementLevel != "advisory" {
		t.Errorf("initial enforcement = %q, want advisory", state.EnforcementLevel)
	}
}

// =============================================================================
// Handler Tests - plan_phase
// =============================================================================

func TestPlanPhase_Start(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action":    "start",
		"objective": "Build auth system",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Planning session started") {
		t.Errorf("result should confirm start, got: %s", result)
	}
	if !strings.Contains(result, "Build auth system") {
		t.Errorf("result should contain objective, got: %s", result)
	}
	if state.Phase != "init" {
		t.Errorf("phase should be init after start, got: %s", state.Phase)
	}
}

func TestPlanPhase_StartAlreadyActive(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "design"
	state.Objective = "Existing"
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action":    "start",
		"objective": "New objective",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "already active") {
		t.Errorf("result should indicate already active, got: %s", result)
	}
}

func TestPlanPhase_Status(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "understand"
	state.Objective = "Test objective"
	state.EnforcementLevel = "strict"
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "status",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "understand") {
		t.Errorf("result should contain phase, got: %s", result)
	}
	if !strings.Contains(result, "Test objective") {
		t.Errorf("result should contain objective, got: %s", result)
	}
	if !strings.Contains(result, "strict") {
		t.Errorf("result should contain enforcement level, got: %s", result)
	}
}

func TestPlanPhase_Transition(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "init"
	state.Objective = "Test"
	state.DiscoveryResults = &DiscoveryResults{}
	state.ConfirmedDocs = &ConfirmedDocs{}
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "transition",
		"to":     "understand",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "understand") {
		t.Errorf("result should contain new phase, got: %s", result)
	}
	if state.Phase != "understand" {
		t.Errorf("phase should be understand, got: %s", state.Phase)
	}
}

func TestPlanPhase_TransitionMissingTo(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "init"
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "transition",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Missing") || !strings.Contains(result, "to") {
		t.Errorf("result should indicate missing 'to' param, got: %s", result)
	}
}

func TestPlanPhase_TransitionInvalidFromIdle(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "transition",
		"to":     "understand",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No active planning session") {
		t.Errorf("result should indicate no active session, got: %s", result)
	}
}

func TestPlanPhase_Skip(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "init"
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "skip",
		"reason": "emergency hotfix",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Skip activated") {
		t.Errorf("result should confirm skip, got: %s", result)
	}
	if !state.SkipActive {
		t.Error("skip should be active")
	}
}

func TestPlanPhase_SkipMissingReason(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "init"
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "skip",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Missing") || !strings.Contains(result, "reason") {
		t.Errorf("result should indicate missing reason, got: %s", result)
	}
}

func TestPlanPhase_Reset(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "design"
	state.Objective = "Something"
	state.BrainChecked = true
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "reset",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "reset") || !strings.Contains(result, "idle") {
		t.Errorf("result should confirm reset, got: %s", result)
	}
	if state.Phase != "idle" {
		t.Errorf("phase should be idle after reset, got: %s", state.Phase)
	}
	if state.Objective != "" {
		t.Errorf("objective should be cleared after reset, got: %s", state.Objective)
	}
}

// =============================================================================
// Handler Tests - plan_capture_intent
// =============================================================================

func TestPlanCaptureIntent_Handler(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "understand"
	registerPlanCaptureIntent(s, state)

	handler := s.tools["plan_capture_intent"].handler
	result, err := handler(context.Background(), map[string]any{
		"problem":          "Users can't authenticate",
		"success_criteria": []any{"JWT tokens work", "OAuth supported"},
		"scope_in":         []any{"Login flow", "Token refresh"},
		"scope_out":        []any{"User management", "Admin panel"},
		"constraints":      []any{"Must use existing DB"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Intent captured") {
		t.Errorf("result should confirm capture, got: %s", result)
	}
	if !strings.Contains(result, "Users can't authenticate") {
		t.Errorf("result should contain problem, got: %s", result)
	}
	if state.UserIntent == nil {
		t.Fatal("user intent should be stored in state")
	}
	if state.UserIntent.Problem != "Users can't authenticate" {
		t.Errorf("stored problem = %q, want %q", state.UserIntent.Problem, "Users can't authenticate")
	}
	if len(state.UserIntent.SuccessCriteria) != 2 {
		t.Errorf("stored success_criteria count = %d, want 2", len(state.UserIntent.SuccessCriteria))
	}
}

// =============================================================================
// Handler Tests - plan_record_arch_check
// =============================================================================

func TestPlanRecordArchCheck_Handler(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "design"
	registerPlanRecordArchCheck(s, state)

	handler := s.tools["plan_record_arch_check"].handler
	result, err := handler(context.Background(), map[string]any{
		"aligned":         []any{"Uses REST API pattern"},
		"conflicts":       []any{"Conflicts with GraphQL plan"},
		"gaps":            []any{"No caching strategy"},
		"recommendations": []any{"Add Redis caching"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Architecture check recorded") {
		t.Errorf("result should confirm recording, got: %s", result)
	}
	if !strings.Contains(result, "Uses REST API pattern") {
		t.Errorf("result should contain aligned items, got: %s", result)
	}
	if !strings.Contains(result, "Conflicts with GraphQL plan") {
		t.Errorf("result should contain conflicts, got: %s", result)
	}
	if state.ArchCheckResult == "" {
		t.Error("arch check result should be stored in state")
	}
}

// =============================================================================
// Handler Tests - plan_propose_doc_updates
// =============================================================================

func TestPlanProposeDocUpdates_Handler(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "approve"
	registerPlanProposeDocUpdates(s, state)

	handler := s.tools["plan_propose_doc_updates"].handler
	result, err := handler(context.Background(), map[string]any{
		"prd_section_title":   "Authentication",
		"prd_requirements":    []any{"JWT support", "OAuth2 support"},
		"prd_rationale":       "Users need secure auth",
		"arch_decision_title": "Use JWT for auth",
		"arch_context":        "Need stateless auth",
		"arch_decision":       "JWT with RS256",
		"arch_consequences":   "Need key rotation",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "PRD Update") {
		t.Errorf("result should contain PRD section, got: %s", result)
	}
	if !strings.Contains(result, "Architecture Decision") {
		t.Errorf("result should contain arch section, got: %s", result)
	}
	if !strings.Contains(result, "JWT support") {
		t.Errorf("result should contain requirements, got: %s", result)
	}
	if !strings.Contains(result, "JWT with RS256") {
		t.Errorf("result should contain decision, got: %s", result)
	}
}

// =============================================================================
// Handler Tests - plan_gate (legacy)
// =============================================================================

func TestPlanGate_Status(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "design"
	state.Objective = "Test"
	state.EnforcementLevel = "strict"
	registerPlanGate(s, state)

	handler := s.tools["plan_gate"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "status",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "design") {
		t.Errorf("result should contain phase, got: %s", result)
	}
	if !strings.Contains(result, "strict") {
		t.Errorf("result should contain enforcement level, got: %s", result)
	}
}

func TestPlanGate_SetEnforcement(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	registerPlanGate(s, state)

	handler := s.tools["plan_gate"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "set_enforcement",
		"level":  "strict",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "strict") {
		t.Errorf("result should confirm strict, got: %s", result)
	}
	if state.EnforcementLevel != "strict" {
		t.Errorf("enforcement should be strict, got: %s", state.EnforcementLevel)
	}
}

func TestPlanGate_SetObjective(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	registerPlanGate(s, state)

	handler := s.tools["plan_gate"].handler
	result, err := handler(context.Background(), map[string]any{
		"action":    "set_objective",
		"objective": "Build feature X",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Build feature X") {
		t.Errorf("result should contain objective, got: %s", result)
	}
	if state.Objective != "Build feature X" {
		t.Errorf("objective should be set, got: %s", state.Objective)
	}
}

func TestPlanGate_Reset(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "design"
	state.Objective = "Something"
	registerPlanGate(s, state)

	handler := s.tools["plan_gate"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "reset",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "reset") {
		t.Errorf("result should confirm reset, got: %s", result)
	}
	if state.Phase != "idle" {
		t.Errorf("phase should be idle, got: %s", state.Phase)
	}
}

// =============================================================================
// Handler Tests - plan_start (legacy)
// =============================================================================

func TestPlanStart_Handler(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	registerPlanStart(s, state)

	handler := s.tools["plan_start"].handler
	result, err := handler(context.Background(), map[string]any{
		"objective":   "Build auth system",
		"enforcement": "strict",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Planning session started") {
		t.Errorf("result should confirm start, got: %s", result)
	}
	if state.Phase != "init" {
		t.Errorf("phase should be init, got: %s", state.Phase)
	}
	if state.Objective != "Build auth system" {
		t.Errorf("objective should be set, got: %s", state.Objective)
	}
	if state.EnforcementLevel != "strict" {
		t.Errorf("enforcement should be strict, got: %s", state.EnforcementLevel)
	}
}

func TestPlanStart_DefaultEnforcement(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	registerPlanStart(s, state)

	handler := s.tools["plan_start"].handler
	_, err := handler(context.Background(), map[string]any{
		"objective": "Build something",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if state.EnforcementLevel != "advisory" {
		t.Errorf("default enforcement should be advisory, got: %s", state.EnforcementLevel)
	}
}

// =============================================================================
// Handler Tests - plan_compliance_report
// =============================================================================

func TestPlanComplianceReport_Handler(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "design"
	state.Objective = "Test"
	state.AuditLog = append(state.AuditLog, AuditEvent{
		Event:   "phase_transition",
		Details: "idle -> init",
	})
	registerPlanComplianceReport(s, state)

	handler := s.tools["plan_compliance_report"].handler
	result, err := handler(context.Background(), map[string]any{
		"format": "summary",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Compliance Report") {
		t.Errorf("result should contain header, got: %s", result)
	}
	if !strings.Contains(result, "phase_transition") {
		t.Errorf("result should contain audit events, got: %s", result)
	}
}

func TestPlanComplianceReport_JSON(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.AuditLog = append(state.AuditLog, AuditEvent{
		Event:   "start",
		Details: "objective set",
	})
	registerPlanComplianceReport(s, state)

	handler := s.tools["plan_compliance_report"].handler
	result, err := handler(context.Background(), map[string]any{
		"format": "json",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Should be valid JSON
	var parsed any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result should be valid JSON: %v\nGot: %s", err, result)
	}
}

func TestPlanComplianceReport_Empty(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	registerPlanComplianceReport(s, state)

	handler := s.tools["plan_compliance_report"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No audit events") {
		t.Errorf("result should indicate no events, got: %s", result)
	}
}

// =============================================================================
// Handler Tests - plan_confirm_docs
// =============================================================================

func TestPlanConfirmDocs_Handler(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "init"
	state.DiscoveryResults = &DiscoveryResults{
		PRDFiles:  []string{"docs/prd.md"},
		ArchFiles: []string{"docs/architecture.md"},
	}
	registerPlanConfirmDocs(s, state)

	handler := s.tools["plan_confirm_docs"].handler
	result, err := handler(context.Background(), map[string]any{
		"prdSelection":  float64(1),
		"archSelection": float64(1),
		"existingPlan":  float64(0),
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Confirmed") {
		t.Errorf("result should confirm selections, got: %s", result)
	}
	if state.ConfirmedDocs == nil {
		t.Fatal("confirmed docs should be stored in state")
	}
}

func TestPlanConfirmDocs_NoDiscovery(t *testing.T) {
	s := NewServer()
	state := newPlanningState()
	state.Phase = "init"
	registerPlanConfirmDocs(s, state)

	handler := s.tools["plan_confirm_docs"].handler
	result, err := handler(context.Background(), map[string]any{
		"prdSelection":  float64(1),
		"archSelection": float64(1),
		"existingPlan":  float64(0),
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No discovery results") {
		t.Errorf("result should indicate no discovery, got: %s", result)
	}
}

// =============================================================================
// Handler Tests - plan_discover_docs
// =============================================================================

func TestPlanDiscoverDocs_Handler(t *testing.T) {
	// Mock brain API for plan search
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{},
			"total":   0,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	state := newPlanningState()
	state.Phase = "init"
	state.Objective = "Test"
	registerPlanDiscoverDocs(s, client, state)

	handler := s.tools["plan_discover_docs"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Should return discovery results (even if no files found)
	if !strings.Contains(result, "Discovery") {
		t.Errorf("result should contain discovery header, got: %s", result)
	}
}

// =============================================================================
// Transition Validation Tests
// =============================================================================

func TestPlanPhase_TransitionInitToUnderstand_RequiresDocs(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "init"
	state.Objective = "Test"
	// No discovery results or confirmed docs
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "transition",
		"to":     "understand",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "plan_discover_docs") {
		t.Errorf("result should mention plan_discover_docs requirement, got: %s", result)
	}
}

func TestPlanPhase_TransitionUnderstandToDesign_RequiresIntent(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "understand"
	state.Objective = "Test"
	// No user intent captured
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "transition",
		"to":     "design",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "plan_capture_intent") {
		t.Errorf("result should mention plan_capture_intent requirement, got: %s", result)
	}
}

func TestPlanPhase_InvalidTransition(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	state := newPlanningState()
	state.Phase = "init"
	state.Objective = "Test"
	registerPlanPhase(s, client, state)

	handler := s.tools["plan_phase"].handler
	result, err := handler(context.Background(), map[string]any{
		"action": "transition",
		"to":     "approve", // Can't go from init to approve directly
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Invalid transition") {
		t.Errorf("result should indicate invalid transition, got: %s", result)
	}
}
