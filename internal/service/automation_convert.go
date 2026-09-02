package service

import (
	"encoding/json"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// =============================================================================
// Conversion helpers: types.Automation* <-> frontmatter.Automation*
// =============================================================================

// automationActionToFM converts a domain AutomationAction to a frontmatter AutomationAction.
func automationActionToFM(a *types.AutomationAction) *frontmatter.AutomationAction {
	if a == nil {
		return nil
	}
	return &frontmatter.AutomationAction{
		Type:               a.Type,
		DirectPrompt:       a.DirectPrompt,
		Command:            a.Command,
		Agent:              a.Agent,
		Model:              a.Model,
		Executor:           a.Executor,
		TargetWorkdir:      a.TargetWorkdir,
		ExecutionMode:      a.ExecutionMode,
		SessionMode:        a.SessionMode,
		CompleteOnIdle:     a.CompleteOnIdle,
		Timeout:            a.Timeout,
		RequiresCapability: a.RequiresCapability,
		SetStatus:          a.SetStatus,
	}
}

// automationRetryToFM converts a domain AutomationRetry to a frontmatter AutomationRetry.
func automationRetryToFM(r *types.AutomationRetry) *frontmatter.AutomationRetry {
	if r == nil {
		return nil
	}
	return &frontmatter.AutomationRetry{
		MaxAttempts: r.MaxAttempts,
		Backoff:     r.Backoff,
		Delay:       r.Delay,
	}
}

// goalConfigToFM converts a domain GoalConfig to a frontmatter GoalConfig.
func goalConfigToFM(g *types.GoalConfig) *frontmatter.GoalConfig {
	if g == nil {
		return nil
	}
	out := &frontmatter.GoalConfig{
		ID:               g.ID,
		Criteria:         g.Criteria,
		Validation:       g.Validation,
		Workdir:          g.Workdir,
		TriggerSource:    g.TriggerSource,
		TaskID:           g.TaskID,
		CompleteStatuses: g.CompleteStatuses,
		BlockedStatuses:  g.BlockedStatuses,
	}
	if g.Steering != nil {
		out.Steering = &frontmatter.GoalSteering{
			Enabled:         g.Steering.Enabled,
			CooldownMinutes: g.Steering.CooldownMinutes,
		}
	}
	return out
}

// reminderConfigToFM converts the API-facing reminder config to its on-disk
// mirror. Both structs exist for the frontmatter package boundary; keeping
// them in step is the whole job of this function.
func reminderConfigToFM(r *types.ReminderConfig) *frontmatter.ReminderConfig {
	if r == nil {
		return nil
	}
	return &frontmatter.ReminderConfig{
		ID:              r.ID,
		RemindAt:        r.RemindAt,
		Timezone:        r.Timezone,
		Action:          r.Action,
		Prompt:          r.Prompt,
		Agent:           r.Agent,
		Model:           r.Model,
		Executor:        r.Executor,
		ExecutionMode:   r.ExecutionMode,
		TargetWorkdir:   r.TargetWorkdir,
		Repeat:          r.Repeat,
		RepeatUntil:     r.RepeatUntil,
		FireCount:       r.FireCount,
		FiredAt:         r.FiredAt,
		GeneratedTaskID: r.GeneratedTaskID,
	}
}

// =============================================================================
// Metadata JSON -> frontmatter/types conversion helpers
// These convert untyped map[string]interface{} (from JSON metadata) back
// into typed automation structs. Used by reconstructFrontmatter and
// parseMetadataIntoEntry.
// =============================================================================

// metaToAutomationActionFM converts a metadata JSON value to a frontmatter AutomationAction.
func metaToAutomationActionFM(v interface{}) *frontmatter.AutomationAction {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	var a frontmatter.AutomationAction
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return nil
	}
	return &a
}

// metaToAutomationRetryFM converts a metadata JSON value to a frontmatter AutomationRetry.
func metaToAutomationRetryFM(v interface{}) *frontmatter.AutomationRetry {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	var r frontmatter.AutomationRetry
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}
	return &r
}

// metaToAutomationAction converts a metadata JSON value to a domain AutomationAction.
func metaToAutomationAction(v interface{}) *types.AutomationAction {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	var a types.AutomationAction
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return nil
	}
	return &a
}

// metaToAutomationRetry converts a metadata JSON value to a domain AutomationRetry.
func metaToAutomationRetry(v interface{}) *types.AutomationRetry {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	var r types.AutomationRetry
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}
	return &r
}
