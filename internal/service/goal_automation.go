package service

import (
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// GoalInput captures the inputs required to build a goal automation entry.
//
// A goal is an automation entry (type=automation, generated_by=brain-goal)
// whose trigger fires on task and/or feature lifecycle events and whose action
// drives the deterministic in-process reconcile loop. BuildGoalAutomation
// translates these inputs into the canonical automation entry shape.
type GoalInput struct {
	// Project scopes the automation and its generated work.
	Project string
	// FeatureID scopes the goal to a feature (trigger filter + entry field).
	FeatureID string
	// Title is the human-readable goal title.
	Title string
	// Content is the optional entry body (defaults to the goal criteria).
	Content string
	// Config holds the goal-specific configuration.
	Config types.GoalConfig
	// Action defines what the goal does when it reconciles (the prompt/agent
	// used to generate coding work). SessionMode is honored.
	Action types.AutomationAction
}

// defaultCompleteStatuses are the task statuses that count toward completion
// when a goal does not specify its own CompleteStatuses.
var defaultCompleteStatuses = []string{"completed", "validated"}

// defaultBlockedStatuses are the task statuses tracked as "blocked" when a
// goal does not specify its own BlockedStatuses.
var defaultBlockedStatuses = []string{"blocked"}

// BuildGoalAutomation produces a canonical goal-automation BrainEntry from the
// provided goal inputs.
//
// The returned entry has:
//   - type=automation, status=active, generated_by=brain-goal
//   - tags: "goal" and "goal:<id>"
//   - feature_id scoping (entry field + trigger filter)
//   - a multi-event trigger (task.status_changed and/or feature.completed),
//     selected by Config.TriggerSource (task|feature|both, default both)
//   - OR-able to_status filter spanning complete + blocked statuses
//     (so the reconcile loop sees completions AND blocked work)
//   - an action carrying the reconcile prompt/agent/session_mode
//   - a normalized GoalConfig (defaults applied)
//
// It uses feature.completed (the live event); feature.all_completed is dead and
// is never emitted into the trigger.
func BuildGoalAutomation(in GoalInput) (types.BrainEntry, error) {
	if strings.TrimSpace(in.Config.ID) == "" {
		return types.BrainEntry{}, fmt.Errorf("goal: missing goal id")
	}
	if strings.TrimSpace(in.Project) == "" {
		return types.BrainEntry{}, fmt.Errorf("goal %s: missing project", in.Config.ID)
	}
	if strings.TrimSpace(in.Title) == "" {
		return types.BrainEntry{}, fmt.Errorf("goal %s: missing title", in.Config.ID)
	}
	if strings.TrimSpace(in.Action.Type) == "" {
		return types.BrainEntry{}, fmt.Errorf("goal %s: missing action type", in.Config.ID)
	}

	cfg := normalizeGoalConfig(in.Config)

	content := in.Content
	if strings.TrimSpace(content) == "" {
		content = cfg.Criteria
	}

	action := in.Action

	entry := types.BrainEntry{
		Type:        "automation",
		Status:      "active",
		Title:       in.Title,
		Content:     content,
		ProjectID:   in.Project,
		FeatureID:   in.FeatureID,
		GeneratedBy: types.GoalGeneratedBy,
		Tags:        []string{types.GoalTag, types.GoalIDTag(cfg.ID)},
		Trigger:     buildGoalTrigger(in.FeatureID, in.Project, &cfg),
		Action:      &action,
		Goal:        &cfg,
	}

	return entry, nil
}

// normalizeGoalConfig returns a copy of cfg with trigger source and status
// sets defaulted when unset.
func normalizeGoalConfig(cfg types.GoalConfig) types.GoalConfig {
	cfg.TriggerSource = cfg.NormalizedTriggerSource()
	if len(cfg.CompleteStatuses) == 0 {
		cfg.CompleteStatuses = append([]string(nil), defaultCompleteStatuses...)
	}
	if len(cfg.BlockedStatuses) == 0 {
		cfg.BlockedStatuses = append([]string(nil), defaultBlockedStatuses...)
	}
	return cfg
}

// buildGoalTrigger constructs the multi-event TriggerConfig for a goal based
// on its trigger source and status configuration.
func buildGoalTrigger(featureID, project string, cfg *types.GoalConfig) *types.TriggerConfig {
	events := goalTriggerEvents(cfg.NormalizedTriggerSource())

	filter := map[string]string{}
	if featureID != "" {
		filter["feature_id"] = featureID
	}
	if project != "" {
		filter["project_id"] = project
	}
	if expr := goalStatusFilter(cfg); expr != "" {
		filter["to_status"] = expr
	}
	if len(filter) == 0 {
		filter = nil
	}

	trigger := &types.TriggerConfig{
		Type:   "event",
		Filter: filter,
	}
	// Use Event for a single pattern and Events for multiple, keeping the
	// entry shape minimal while EventPatterns() unifies them.
	if len(events) == 1 {
		trigger.Event = events[0]
	} else if len(events) > 1 {
		trigger.Events = events
	}
	return trigger
}

// goalTriggerEvents returns the event patterns for a trigger source.
// feature.all_completed is dead and never returned; feature completion uses
// the live feature.completed event.
func goalTriggerEvents(source string) []string {
	switch source {
	case types.GoalTriggerSourceTask:
		return []string{types.EventTaskStatusChanged}
	case types.GoalTriggerSourceFeature:
		return []string{types.EventFeatureCompleted}
	default: // both
		return []string{types.EventTaskStatusChanged, types.EventFeatureCompleted}
	}
}

// goalStatusFilter builds an OR-able "in:" filter expression spanning the
// goal's complete and blocked statuses (deduped, order-preserving). Returns
// empty when there are no statuses to match.
func goalStatusFilter(cfg *types.GoalConfig) string {
	seen := make(map[string]bool)
	var members []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		members = append(members, s)
	}
	for _, s := range cfg.CompleteStatuses {
		add(s)
	}
	for _, s := range cfg.BlockedStatuses {
		add(s)
	}
	if len(members) == 0 {
		return ""
	}
	return "in:" + strings.Join(members, ",")
}
