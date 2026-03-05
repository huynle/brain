package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// =============================================================================
// Planning State Types
// =============================================================================

// PlanningState holds the in-memory state for the planning phase state machine.
// Since the Go MCP server runs as a single-session stdio process, this is
// package-level state shared across all planning tool calls.
type PlanningState struct {
	Phase            string
	PhaseStartedAt   time.Time
	Objective        string
	EnforcementLevel string
	BrainChecked     bool
	PlanSaved        bool
	CurrentPlanID    string
	CurrentPlanTitle string
	UserIntent       *UserIntent
	ArchCheckResult  string
	ConfirmedDocs    *ConfirmedDocs
	DiscoveryResults *DiscoveryResults
	SkipActive       bool
	AuditLog         []AuditEvent
}

// UserIntent captures the user's requirements during the UNDERSTAND phase.
type UserIntent struct {
	Problem         string
	SuccessCriteria []string
	ScopeIn         []string
	ScopeOut        []string
	Constraints     []string
}

// ConfirmedDocs stores confirmed document selections from plan_confirm_docs.
type ConfirmedDocs struct {
	PRDPath      string
	ArchPath     string
	ExistingPlan string
	Explorations []string
	OtherDocs    []string
}

// DiscoveryResults stores results from plan_discover_docs.
type DiscoveryResults struct {
	PRDFiles     []string
	ArchFiles    []string
	BrainPlans   []string
	Explorations []string
}

// AuditEvent records a planning session event for compliance reporting.
type AuditEvent struct {
	Timestamp time.Time
	Event     string
	Details   string
}

// newPlanningState creates a fresh planning state with defaults.
func newPlanningState() *PlanningState {
	return &PlanningState{
		Phase:            "idle",
		EnforcementLevel: "advisory",
		AuditLog:         []AuditEvent{},
	}
}

// resetState clears all planning state back to idle.
func (s *PlanningState) resetState() {
	s.Phase = "idle"
	s.PhaseStartedAt = time.Time{}
	s.Objective = ""
	s.BrainChecked = false
	s.PlanSaved = false
	s.CurrentPlanID = ""
	s.CurrentPlanTitle = ""
	s.UserIntent = nil
	s.ArchCheckResult = ""
	s.ConfirmedDocs = nil
	s.DiscoveryResults = nil
	s.SkipActive = false
}

// addAuditEvent appends an event to the audit log.
func (s *PlanningState) addAuditEvent(event, details string) {
	s.AuditLog = append(s.AuditLog, AuditEvent{
		Timestamp: time.Now(),
		Event:     event,
		Details:   details,
	})
}

// validTransitions defines allowed phase transitions.
var validTransitions = map[string][]string{
	"init":       {"understand"},
	"understand": {"design"},
	"design":     {"approve", "understand"},
	"approve":    {"idle", "design"},
}

// phaseInfo provides human-readable info about each phase.
var phaseInfo = map[string]struct {
	Description string
	Next        string
}{
	"idle": {
		Description: "No active planning session",
		Next:        `Call plan_phase(action: "start", objective: "...") to begin`,
	},
	"init": {
		Description: "Loading project docs (PRD, architecture)",
		Next:        "Call plan_discover_docs() to scan for PRD and architecture docs",
	},
	"understand": {
		Description: "Capturing user intent",
		Next:        "Ask clarifying questions, then call plan_capture_intent(...)",
	},
	"design": {
		Description: "Drafting approach + architecture check",
		Next:        "Draft design, dispatch explore subagent for arch check, then call plan_record_arch_check(...)",
	},
	"approve": {
		Description: "User approves doc updates, plan saved",
		Next:        "Call plan_propose_doc_updates(...) then plan_phase(action: \"transition\", to: \"idle\")",
	},
}

// =============================================================================
// RegisterPlanningTools
// =============================================================================

// RegisterPlanningTools registers all 9 planning tools on the server.
func RegisterPlanningTools(s *Server, client *APIClient) {
	state := newPlanningState()

	registerPlanPhase(s, client, state)
	registerPlanDiscoverDocs(s, client, state)
	registerPlanConfirmDocs(s, state)
	registerPlanCaptureIntent(s, state)
	registerPlanRecordArchCheck(s, state)
	registerPlanProposeDocUpdates(s, state)
	registerPlanGate(s, state)
	registerPlanStart(s, state)
	registerPlanComplianceReport(s, state)
}

// =============================================================================
// plan_phase
// =============================================================================

func registerPlanPhase(s *Server, client *APIClient, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_phase",
		Description: `Control the planning phase state machine.

PHASES (in order):
1. IDLE → No active session
2. INIT → Loading project docs (PRD, architecture)
3. UNDERSTAND → Capturing user intent
4. DESIGN → Drafting approach + architecture check
5. APPROVE → User approves doc updates, plan saved

ACTIONS:
- start: Begin planning session (IDLE → INIT)
- transition: Move to next phase (requires completing current phase)
- status: Check current phase and requirements
- skip: Emergency one-time bypass (logged for audit)
- reset: Clear session and return to IDLE

TRANSITIONS:
- init → understand (after docs loaded)
- understand → design (after intent captured)
- design → approve (after arch check + user approval)
- design → understand (to refine intent)
- approve → idle (complete)
- approve → design (to revise approach)`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"action":    {Type: "string", Enum: []string{"start", "transition", "status", "skip", "reset"}, Description: "Action to perform"},
				"objective": {Type: "string", Description: "Planning objective (for start action)"},
				"to":        {Type: "string", Enum: []string{"init", "understand", "design", "approve", "idle"}, Description: "Target phase (for transition action)"},
				"reason":    {Type: "string", Description: "Reason for skip (required for skip action)"},
			},
			Required: []string{"action"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		action := StringArg(args, "action", "")

		switch action {
		case "start":
			if state.Phase != "idle" {
				return fmt.Sprintf("❌ Planning session already active in phase: %s\n\nObjective: %s\n\n"+
					`Use plan_phase(action: "reset") first, or plan_phase(action: "status") to check state.`,
					state.Phase, state.Objective), nil
			}

			objective := StringArg(args, "objective", "")
			if objective == "" {
				objective = "(no objective set)"
			}

			state.Phase = "init"
			state.PhaseStartedAt = time.Now()
			state.Objective = objective
			state.addAuditEvent("start", fmt.Sprintf("objective: %s", objective))

			info := phaseInfo["init"]
			return fmt.Sprintf("✅ Planning session started\n\n"+
				"**Objective:** %s\n"+
				"**Phase:** init - %s\n"+
				"**Enforcement:** %s\n\n"+
				"**Next:** %s\n\n"+
				"Call `plan_discover_docs()` to scan for:\n"+
				"- PRD documents\n"+
				"- Architecture docs\n"+
				"- Existing brain plans",
				objective, info.Description, state.EnforcementLevel, info.Next), nil

		case "status":
			info := phaseInfo[state.Phase]
			lines := []string{
				"## Planning Session Status",
				"",
				fmt.Sprintf("**Phase:** %s", state.Phase),
				fmt.Sprintf("**Description:** %s", info.Description),
				fmt.Sprintf("**Objective:** %s", orDefault(state.Objective, "(none)")),
				fmt.Sprintf("**Enforcement:** %s", state.EnforcementLevel),
				"",
			}

			if state.Phase != "idle" {
				lines = append(lines, fmt.Sprintf("**Started:** %s", state.PhaseStartedAt.Format(time.RFC3339)))
			}

			// Show what's been completed
			lines = append(lines, "", "### Completed Steps")
			if state.DiscoveryResults != nil {
				lines = append(lines, "- ✅ Documentation discovered")
			}
			if state.ConfirmedDocs != nil {
				lines = append(lines, "- ✅ Documentation confirmed")
			}
			if state.UserIntent != nil {
				lines = append(lines, "- ✅ User intent captured")
			}
			if state.ArchCheckResult != "" {
				lines = append(lines, "- ✅ Architecture check recorded")
			}
			if state.BrainChecked {
				lines = append(lines, "- ✅ Brain checked")
			}
			if state.PlanSaved {
				lines = append(lines, "- ✅ Plan saved")
			}

			lines = append(lines, "", fmt.Sprintf("**Next:** %s", info.Next))

			return strings.Join(lines, "\n"), nil

		case "transition":
			to := StringArg(args, "to", "")
			if to == "" {
				return `❌ Missing 'to' parameter

Usage: plan_phase(action: "transition", to: "understand")`, nil
			}

			if state.Phase == "idle" {
				return `❌ No active planning session.

Use plan_phase(action: "start", objective: "...") to begin.`, nil
			}

			// Validate transition
			allowed, ok := validTransitions[state.Phase]
			if !ok {
				return fmt.Sprintf("❌ Invalid transition from phase: %s", state.Phase), nil
			}

			isAllowed := false
			for _, a := range allowed {
				if a == to {
					isAllowed = true
					break
				}
			}
			if !isAllowed {
				return fmt.Sprintf("❌ Invalid transition: %s → %s\n\nAllowed transitions from %s: %s",
					state.Phase, to, state.Phase, strings.Join(allowed, ", ")), nil
			}

			// Check prerequisites
			switch {
			case state.Phase == "init" && to == "understand":
				if state.ConfirmedDocs == nil {
					return "❌ Complete INIT phase first. Call plan_discover_docs() to load project documentation, then plan_confirm_docs() to confirm selections.", nil
				}
			case state.Phase == "understand" && to == "design":
				if state.UserIntent == nil {
					return "❌ Complete UNDERSTAND phase first. Capture user intent with plan_capture_intent().", nil
				}
			}

			oldPhase := state.Phase
			state.Phase = to
			state.PhaseStartedAt = time.Now()
			state.addAuditEvent("phase_transition", fmt.Sprintf("%s -> %s", oldPhase, to))

			info := phaseInfo[to]
			return fmt.Sprintf("✅ Transitioned: %s → %s\n\n**Phase:** %s - %s\n\n**Next:** %s",
				oldPhase, to, to, info.Description, info.Next), nil

		case "skip":
			reason := StringArg(args, "reason", "")
			if reason == "" {
				return `❌ Missing 'reason' parameter

Usage: plan_phase(action: "skip", reason: "emergency hotfix")`, nil
			}

			state.SkipActive = true
			state.addAuditEvent("skip", reason)

			return fmt.Sprintf("⚠️ Skip activated (one-time bypass)\n\n"+
				"**Reason:** %s\n"+
				"**Phase:** %s\n\n"+
				"This skip is logged for audit. Enforcement resumes after one action.",
				reason, state.Phase), nil

		case "reset":
			oldPhase := state.Phase
			state.resetState()
			state.addAuditEvent("reset", fmt.Sprintf("from phase: %s", oldPhase))

			return fmt.Sprintf("✅ Planning session reset to idle.\n\n"+
				"Previous phase: %s\n\n"+
				"Use plan_phase(action: \"start\", objective: \"...\") to begin a new session.",
				oldPhase), nil

		default:
			return fmt.Sprintf("❌ Unknown action: %q\n\nValid actions: start, transition, status, skip, reset", action), nil
		}
	})
}

// =============================================================================
// plan_discover_docs
// =============================================================================

func registerPlanDiscoverDocs(s *Server, client *APIClient, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_discover_docs",
		Description: `Discover project documentation AND existing brain plans.

This tool performs interactive discovery:
1. Scans for PRD and architecture documents in common locations
2. Searches brain for existing plans (fuzzy match against objective)
3. Searches brain for related explorations
4. Returns numbered options for user to select

After discovery, use plan_confirm_docs() to confirm selections.

Arguments:
- prdPath: Custom PRD path (skips auto-discovery for PRD)
- archPath: Custom architecture doc path (skips auto-discovery for arch)
- additionalDirs: Additional directories to scan for docs
- planQuery: Search query for existing plans (uses objective if not provided)
- planId: Specific brain plan ID to load directly

Call this during INIT phase to load project context.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prdPath":        {Type: "string", Description: "Custom PRD path (skips auto-discovery for PRD)"},
				"archPath":       {Type: "string", Description: "Custom architecture doc path (skips auto-discovery for arch)"},
				"additionalDirs": {Type: "array", Items: &Property{Type: "string"}, Description: "Additional directories to scan for docs"},
				"planQuery":      {Type: "string", Description: "Search query for existing plans (uses objective if not provided)"},
				"planId":         {Type: "string", Description: "Specific brain plan ID to load directly"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		discovery := &DiscoveryResults{}

		// Get working directory
		execCtx := GetCachedContext()
		home, _ := os.UserHomeDir()
		workdir := filepath.Join(home, execCtx.Workdir)

		// Scan for PRD files
		prdPath := StringArg(args, "prdPath", "")
		if prdPath != "" {
			discovery.PRDFiles = []string{prdPath}
		} else {
			prdPatterns := []string{
				"docs/prd/*.md", "docs/prd.md", "PRD.md", "prd.md",
				"docs/requirements/*.md",
			}
			for _, pattern := range prdPatterns {
				matches, _ := filepath.Glob(filepath.Join(workdir, pattern))
				for _, m := range matches {
					rel, _ := filepath.Rel(workdir, m)
					discovery.PRDFiles = append(discovery.PRDFiles, rel)
				}
			}
		}

		// Scan for architecture files
		archPath := StringArg(args, "archPath", "")
		if archPath != "" {
			discovery.ArchFiles = []string{archPath}
		} else {
			archPatterns := []string{
				"docs/architecture/*.md", "docs/architecture.md",
				"ARCHITECTURE.md", "architecture.md",
				"docs/design/*.md", "DESIGN.md",
			}
			for _, pattern := range archPatterns {
				matches, _ := filepath.Glob(filepath.Join(workdir, pattern))
				for _, m := range matches {
					rel, _ := filepath.Rel(workdir, m)
					discovery.ArchFiles = append(discovery.ArchFiles, rel)
				}
			}
		}

		// Scan additional directories
		additionalDirs := StringSliceArg(args, "additionalDirs")
		for _, dir := range additionalDirs {
			fullDir := filepath.Join(workdir, dir)
			matches, _ := filepath.Glob(filepath.Join(fullDir, "*.md"))
			for _, m := range matches {
				rel, _ := filepath.Rel(workdir, m)
				// Categorize based on name
				lower := strings.ToLower(filepath.Base(m))
				if strings.Contains(lower, "prd") || strings.Contains(lower, "requirement") {
					discovery.PRDFiles = append(discovery.PRDFiles, rel)
				} else if strings.Contains(lower, "arch") || strings.Contains(lower, "design") {
					discovery.ArchFiles = append(discovery.ArchFiles, rel)
				}
			}
		}

		// Search brain for existing plans
		planQuery := StringArg(args, "planQuery", state.Objective)
		if planQuery != "" {
			var searchResp struct {
				Results []struct {
					Path  string `json:"path"`
					Title string `json:"title"`
					Type  string `json:"type"`
				} `json:"results"`
			}
			err := client.Request(ctx, "POST", "/search", map[string]any{
				"query": planQuery,
				"type":  "plan",
				"limit": 5,
			}, nil, &searchResp)
			if err == nil {
				for _, r := range searchResp.Results {
					discovery.BrainPlans = append(discovery.BrainPlans, fmt.Sprintf("%s (%s)", r.Title, r.Path))
				}
			}

			// Also search for explorations
			var exploResp struct {
				Results []struct {
					Path  string `json:"path"`
					Title string `json:"title"`
				} `json:"results"`
			}
			err = client.Request(ctx, "POST", "/search", map[string]any{
				"query": planQuery,
				"type":  "exploration",
				"limit": 5,
			}, nil, &exploResp)
			if err == nil {
				for _, r := range exploResp.Results {
					discovery.Explorations = append(discovery.Explorations, fmt.Sprintf("%s (%s)", r.Title, r.Path))
				}
			}
		}

		// Handle specific planId
		planId := StringArg(args, "planId", "")
		if planId != "" {
			discovery.BrainPlans = append([]string{planId}, discovery.BrainPlans...)
		}

		state.DiscoveryResults = discovery
		state.addAuditEvent("discover_docs", fmt.Sprintf("prd:%d arch:%d plans:%d",
			len(discovery.PRDFiles), len(discovery.ArchFiles), len(discovery.BrainPlans)))

		// Format output
		lines := []string{
			"## Discovery Results",
			"",
		}

		// PRD files
		lines = append(lines, "### PRD Documents")
		if len(discovery.PRDFiles) == 0 {
			lines = append(lines, "  (none found)")
		} else {
			for i, f := range discovery.PRDFiles {
				lines = append(lines, fmt.Sprintf("  %d. %s", i+1, f))
			}
		}
		lines = append(lines, "")

		// Architecture files
		lines = append(lines, "### Architecture Documents")
		if len(discovery.ArchFiles) == 0 {
			lines = append(lines, "  (none found)")
		} else {
			for i, f := range discovery.ArchFiles {
				lines = append(lines, fmt.Sprintf("  %d. %s", i+1, f))
			}
		}
		lines = append(lines, "")

		// Brain plans
		if len(discovery.BrainPlans) > 0 {
			lines = append(lines, "### Existing Brain Plans")
			for i, p := range discovery.BrainPlans {
				lines = append(lines, fmt.Sprintf("  %d. %s", i+1, p))
			}
			lines = append(lines, "")
		}

		// Explorations
		if len(discovery.Explorations) > 0 {
			lines = append(lines, "### Related Explorations")
			for i, e := range discovery.Explorations {
				lines = append(lines, fmt.Sprintf("  %d. %s", i+1, e))
			}
			lines = append(lines, "")
		}

		lines = append(lines, "---")
		lines = append(lines, "")
		lines = append(lines, "**Next:** Present these options to the user, then call `plan_confirm_docs()` with their selections.")

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// plan_confirm_docs
// =============================================================================

func registerPlanConfirmDocs(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_confirm_docs",
		Description: `Confirm documentation selections from plan_discover_docs.

Use this after plan_discover_docs to lock in user selections:
- PRD document (number from list, 0 for create new, or custom path)
- Architecture document (number from list, 0 for create new, or custom path)
- Existing plan to update (number from list, 0 for new plan, or brain ID)
- Explorations to include as context

This stores the confirmed selections and marks INIT phase as ready for transition.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prdSelection":        {Type: "string", Description: "PRD selection: number from list, '0' for create new, or custom path"},
				"archSelection":       {Type: "string", Description: "Architecture selection: number from list, '0' for create new, or custom path"},
				"existingPlan":        {Type: "string", Description: "Existing plan: number from list, '0' for new plan, or brain ID"},
				"includeExplorations": {Type: "array", Items: &Property{Type: "string"}, Description: "Exploration IDs to include as context"},
				"includeOtherDocs":    {Type: "array", Items: &Property{Type: "string"}, Description: "Other doc paths to include"},
			},
			Required: []string{"prdSelection", "archSelection", "existingPlan"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if state.DiscoveryResults == nil {
			return "❌ No discovery results found.\n\nCall `plan_discover_docs()` first to discover available documentation.", nil
		}

		confirmed := &ConfirmedDocs{}

		// Resolve PRD selection
		prdSel := resolveDocSelection(args, "prdSelection", state.DiscoveryResults.PRDFiles)
		confirmed.PRDPath = prdSel

		// Resolve arch selection
		archSel := resolveDocSelection(args, "archSelection", state.DiscoveryResults.ArchFiles)
		confirmed.ArchPath = archSel

		// Resolve existing plan
		planSel := resolveDocSelection(args, "existingPlan", state.DiscoveryResults.BrainPlans)
		confirmed.ExistingPlan = planSel

		// Optional explorations
		confirmed.Explorations = StringSliceArg(args, "includeExplorations")
		confirmed.OtherDocs = StringSliceArg(args, "includeOtherDocs")

		state.ConfirmedDocs = confirmed
		state.addAuditEvent("confirm_docs", fmt.Sprintf("prd:%s arch:%s plan:%s",
			orDefault(confirmed.PRDPath, "(new)"),
			orDefault(confirmed.ArchPath, "(new)"),
			orDefault(confirmed.ExistingPlan, "(new)")))

		lines := []string{
			"✅ Confirmed documentation selections:",
			"",
			fmt.Sprintf("- **PRD:** %s", orDefault(confirmed.PRDPath, "(create new)")),
			fmt.Sprintf("- **Architecture:** %s", orDefault(confirmed.ArchPath, "(create new)")),
			fmt.Sprintf("- **Existing Plan:** %s", orDefault(confirmed.ExistingPlan, "(new plan)")),
		}

		if len(confirmed.Explorations) > 0 {
			lines = append(lines, fmt.Sprintf("- **Explorations:** %s", strings.Join(confirmed.Explorations, ", ")))
		}
		if len(confirmed.OtherDocs) > 0 {
			lines = append(lines, fmt.Sprintf("- **Other Docs:** %s", strings.Join(confirmed.OtherDocs, ", ")))
		}

		lines = append(lines, "")
		lines = append(lines, `**Next:** Call plan_phase(action: "transition", to: "understand") to proceed.`)

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// plan_capture_intent
// =============================================================================

func registerPlanCaptureIntent(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_capture_intent",
		Description: `Capture the user's intent after clarifying questions.

Store the understood requirements for later validation:
- Problem statement
- Success criteria
- Scope (in/out)
- Constraints

Call this during UNDERSTAND phase after asking clarifying questions.
This enables checking the final plan against original intent.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"problem":          {Type: "string", Description: "Problem statement"},
				"success_criteria": {Type: "array", Items: &Property{Type: "string"}, Description: "Success criteria"},
				"scope_in":         {Type: "array", Items: &Property{Type: "string"}, Description: "What's in scope"},
				"scope_out":        {Type: "array", Items: &Property{Type: "string"}, Description: "What's out of scope"},
				"constraints":      {Type: "array", Items: &Property{Type: "string"}, Description: "Constraints and limitations"},
			},
			Required: []string{"problem", "success_criteria", "scope_in", "scope_out", "constraints"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		intent := &UserIntent{
			Problem:         StringArg(args, "problem", ""),
			SuccessCriteria: StringSliceArg(args, "success_criteria"),
			ScopeIn:         StringSliceArg(args, "scope_in"),
			ScopeOut:        StringSliceArg(args, "scope_out"),
			Constraints:     StringSliceArg(args, "constraints"),
		}

		state.UserIntent = intent
		state.addAuditEvent("capture_intent", fmt.Sprintf("problem: %s", intent.Problem))

		lines := []string{
			"✅ Intent captured",
			"",
			"### Problem",
			intent.Problem,
			"",
			"### Success Criteria",
		}
		for _, c := range intent.SuccessCriteria {
			lines = append(lines, fmt.Sprintf("- %s", c))
		}
		lines = append(lines, "", "### In Scope")
		for _, s := range intent.ScopeIn {
			lines = append(lines, fmt.Sprintf("- %s", s))
		}
		lines = append(lines, "", "### Out of Scope")
		for _, s := range intent.ScopeOut {
			lines = append(lines, fmt.Sprintf("- %s", s))
		}
		lines = append(lines, "", "### Constraints")
		for _, c := range intent.Constraints {
			lines = append(lines, fmt.Sprintf("- %s", c))
		}

		lines = append(lines, "")
		lines = append(lines, `**Next:** Call plan_phase(action: "transition", to: "design") to proceed.`)

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// plan_record_arch_check
// =============================================================================

func registerPlanRecordArchCheck(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_record_arch_check",
		Description: `Record the result of an architecture check.

Call this after dispatching an explore subagent to analyze the design
against existing PRD and architecture. Store the analysis result.

The agent should dispatch the subagent and then call this tool with the result.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"aligned":         {Type: "array", Items: &Property{Type: "string"}, Description: "Aspects aligned with architecture"},
				"conflicts":       {Type: "array", Items: &Property{Type: "string"}, Description: "Conflicts with architecture"},
				"gaps":            {Type: "array", Items: &Property{Type: "string"}, Description: "Gaps in architecture coverage"},
				"recommendations": {Type: "array", Items: &Property{Type: "string"}, Description: "Recommendations"},
			},
			Required: []string{"aligned", "conflicts", "gaps", "recommendations"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		aligned := StringSliceArg(args, "aligned")
		conflicts := StringSliceArg(args, "conflicts")
		gaps := StringSliceArg(args, "gaps")
		recommendations := StringSliceArg(args, "recommendations")

		// Format the result
		lines := []string{
			"## Architecture check recorded",
			"",
			"### Aligned",
		}
		for _, a := range aligned {
			lines = append(lines, fmt.Sprintf("- ✅ %s", a))
		}
		lines = append(lines, "", "### Conflicts")
		if len(conflicts) == 0 {
			lines = append(lines, "- (none)")
		} else {
			for _, c := range conflicts {
				lines = append(lines, fmt.Sprintf("- ❌ %s", c))
			}
		}
		lines = append(lines, "", "### Gaps")
		if len(gaps) == 0 {
			lines = append(lines, "- (none)")
		} else {
			for _, g := range gaps {
				lines = append(lines, fmt.Sprintf("- ⚠️ %s", g))
			}
		}
		lines = append(lines, "", "### Recommendations")
		for _, r := range recommendations {
			lines = append(lines, fmt.Sprintf("- 💡 %s", r))
		}

		result := strings.Join(lines, "\n")
		state.ArchCheckResult = result
		state.addAuditEvent("arch_check", fmt.Sprintf("aligned:%d conflicts:%d gaps:%d",
			len(aligned), len(conflicts), len(gaps)))

		lines = append(lines, "")
		lines = append(lines, `**Next:** Call plan_phase(action: "transition", to: "approve") to proceed.`)

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// plan_propose_doc_updates
// =============================================================================

func registerPlanProposeDocUpdates(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_propose_doc_updates",
		Description: `Format proposed documentation updates for user approval.

Creates a diff-style proposal showing:
- New PRD requirements to add
- New architecture decisions to add

Call this during APPROVE phase to present changes before writing.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prd_section_title":   {Type: "string", Description: "Title for the new PRD section"},
				"prd_requirements":    {Type: "array", Items: &Property{Type: "string"}, Description: "New requirements to add"},
				"prd_rationale":       {Type: "string", Description: "Rationale for the requirements"},
				"arch_decision_title": {Type: "string", Description: "Title for the architecture decision"},
				"arch_context":        {Type: "string", Description: "Context for the decision"},
				"arch_decision":       {Type: "string", Description: "The decision made"},
				"arch_consequences":   {Type: "string", Description: "Consequences of the decision"},
			},
			Required: []string{"prd_section_title", "prd_requirements", "prd_rationale",
				"arch_decision_title", "arch_context", "arch_decision", "arch_consequences"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		prdTitle := StringArg(args, "prd_section_title", "")
		prdReqs := StringSliceArg(args, "prd_requirements")
		prdRationale := StringArg(args, "prd_rationale", "")
		archTitle := StringArg(args, "arch_decision_title", "")
		archContext := StringArg(args, "arch_context", "")
		archDecision := StringArg(args, "arch_decision", "")
		archConsequences := StringArg(args, "arch_consequences", "")

		lines := []string{
			"## Proposed Documentation Updates",
			"",
			"---",
			"",
			"### PRD Update",
			"",
			fmt.Sprintf("**Section:** %s", prdTitle),
			"",
			"**Requirements:**",
		}
		for _, r := range prdReqs {
			lines = append(lines, fmt.Sprintf("+ %s", r))
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("**Rationale:** %s", prdRationale))
		lines = append(lines, "")
		lines = append(lines, "---")
		lines = append(lines, "")
		lines = append(lines, "### Architecture Decision")
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("**Title:** %s", archTitle))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("**Context:** %s", archContext))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("**Decision:** %s", archDecision))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("**Consequences:** %s", archConsequences))
		lines = append(lines, "")
		lines = append(lines, "---")
		lines = append(lines, "")
		lines = append(lines, "**Action required:** Review the proposed changes above and approve or request modifications.")

		state.addAuditEvent("propose_doc_updates", fmt.Sprintf("prd:%s arch:%s", prdTitle, archTitle))

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// plan_gate (legacy)
// =============================================================================

func registerPlanGate(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_gate",
		Description: `[LEGACY] Control brain planning enforcement. Use plan_phase instead for new workflow.

ACTIONS:
- status: Check current planning session state
- set_enforcement: Set enforcement level ("advisory" or "strict")
- set_objective: Set the current planning objective
- reset: Clear session state`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"action":    {Type: "string", Enum: []string{"status", "set_enforcement", "set_objective", "reset"}, Description: "Action to perform"},
				"level":     {Type: "string", Enum: []string{"advisory", "strict"}, Description: "Enforcement level (for set_enforcement)"},
				"objective": {Type: "string", Description: "Planning objective (for set_objective)"},
			},
			Required: []string{"action"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		action := StringArg(args, "action", "")

		switch action {
		case "status":
			return fmt.Sprintf("## Planning Gate Status\n\n"+
				"**Phase:** %s\n"+
				"**Objective:** %s\n"+
				"**Enforcement:** %s\n"+
				"**Brain Checked:** %v\n"+
				"**Plan Saved:** %v",
				state.Phase, orDefault(state.Objective, "(none)"),
				state.EnforcementLevel, state.BrainChecked, state.PlanSaved), nil

		case "set_enforcement":
			level := StringArg(args, "level", "advisory")
			state.EnforcementLevel = level
			state.addAuditEvent("set_enforcement", level)
			return fmt.Sprintf("Enforcement level set to: %s", level), nil

		case "set_objective":
			objective := StringArg(args, "objective", "")
			state.Objective = objective
			state.addAuditEvent("set_objective", objective)
			return fmt.Sprintf("Objective set: %s", objective), nil

		case "reset":
			state.resetState()
			state.addAuditEvent("gate_reset", "session cleared")
			return "Planning session reset to idle.", nil

		default:
			return fmt.Sprintf("Unknown action: %q\n\nValid actions: status, set_enforcement, set_objective, reset", action), nil
		}
	})
}

// =============================================================================
// plan_start (legacy)
// =============================================================================

func registerPlanStart(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name:        "plan_start",
		Description: `[LEGACY] Start a planning session. Use plan_phase(action: "start") instead.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"objective":   {Type: "string", Description: "Planning objective"},
				"enforcement": {Type: "string", Enum: []string{"advisory", "strict"}, Description: "Enforcement level"},
			},
			Required: []string{"objective"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		objective := StringArg(args, "objective", "")
		enforcement := StringArg(args, "enforcement", "advisory")

		state.Phase = "init"
		state.PhaseStartedAt = time.Now()
		state.Objective = objective
		state.EnforcementLevel = enforcement
		state.addAuditEvent("legacy_start", fmt.Sprintf("objective: %s, enforcement: %s", objective, enforcement))

		return fmt.Sprintf("✅ Planning session started (legacy)\n\n"+
			"**Objective:** %s\n"+
			"**Enforcement:** %s\n"+
			"**Phase:** init\n\n"+
			"**Next:** Call plan_discover_docs() to load project documentation.",
			objective, enforcement), nil
	})
}

// =============================================================================
// plan_compliance_report
// =============================================================================

func registerPlanComplianceReport(s *Server, state *PlanningState) {
	s.RegisterTool(Tool{
		Name: "plan_compliance_report",
		Description: `Generate a compliance report for planning sessions.

Returns audit events showing:
- Phase transitions
- Brain checks performed
- Plans saved
- Enforcement blocks
- Skips used`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"sessionID": {Type: "string", Description: "Session ID to report on (optional)"},
				"format":    {Type: "string", Enum: []string{"summary", "detailed", "json"}, Description: "Report format"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		format := StringArg(args, "format", "summary")

		if len(state.AuditLog) == 0 {
			return "No audit events recorded.\n\nStart a planning session with plan_phase(action: \"start\") to begin tracking.", nil
		}

		if format == "json" {
			data, err := json.MarshalIndent(map[string]any{
				"phase":       state.Phase,
				"objective":   state.Objective,
				"enforcement": state.EnforcementLevel,
				"events":      state.AuditLog,
			}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal report: %w", err)
			}
			return string(data), nil
		}

		lines := []string{
			"## Compliance Report",
			"",
			fmt.Sprintf("**Current Phase:** %s", state.Phase),
			fmt.Sprintf("**Objective:** %s", orDefault(state.Objective, "(none)")),
			fmt.Sprintf("**Enforcement:** %s", state.EnforcementLevel),
			fmt.Sprintf("**Total Events:** %d", len(state.AuditLog)),
			"",
			"### Audit Events",
			"",
		}

		for _, event := range state.AuditLog {
			ts := event.Timestamp.Format("15:04:05")
			if format == "detailed" {
				lines = append(lines, fmt.Sprintf("- **[%s] %s:** %s", ts, event.Event, event.Details))
			} else {
				lines = append(lines, fmt.Sprintf("- %s: %s", event.Event, event.Details))
			}
		}

		// Summary stats
		skipCount := 0
		transitionCount := 0
		for _, event := range state.AuditLog {
			switch event.Event {
			case "skip":
				skipCount++
			case "phase_transition":
				transitionCount++
			}
		}

		lines = append(lines, "")
		lines = append(lines, "### Summary")
		lines = append(lines, fmt.Sprintf("- Phase transitions: %d", transitionCount))
		lines = append(lines, fmt.Sprintf("- Skips used: %d", skipCount))

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// Helper functions
// =============================================================================

// orDefault returns the value if non-empty, otherwise the default.
func orDefault(value, defaultVal string) string {
	if value == "" {
		return defaultVal
	}
	return value
}

// resolveDocSelection resolves a selection from args against a list of discovered files.
// Handles: numeric index (1-based), "0" for create new, or custom string path.
func resolveDocSelection(args map[string]any, key string, files []string) string {
	// Try as number first (JSON numbers come as float64)
	if v, ok := args[key].(float64); ok {
		idx := int(v)
		if idx == 0 {
			return "" // Create new
		}
		if idx >= 1 && idx <= len(files) {
			return files[idx-1]
		}
		return "" // Out of range
	}

	// Try as string
	if v, ok := args[key].(string); ok {
		if v == "0" || v == "" {
			return "" // Create new
		}
		return v // Custom path
	}

	return ""
}
