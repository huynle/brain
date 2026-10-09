package service

import (
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// Per-project bindings.
//
// A binding is a project-owned automation with extends: <global automation id>.
// It customizes or opts out of its parent for one project without copying the
// parent. This file holds the pure parts: the overlay that produces a binding's
// effective config, and the tag that makes bindings discoverable. Discovery,
// target selection and evaluation live with the scheduler and the event path.

// bindingTagPrefix namespaces the tag that marks a binding's parent. Bindings
// are found by this tag, so Save and Update keep it in sync with extends.
const bindingTagPrefix = "extends:"

// bindingTag returns the tag that marks an automation as a binding of parentID.
func bindingTag(parentID string) string {
	return bindingTagPrefix + parentID
}

// syncExtendsTag returns tags with every extends: tag removed and, when
// parentID is set, exactly one extends:<parentID> tag appended. A stale tag
// never survives an update that changed or cleared extends.
func syncExtendsTag(tags []string, parentID string) []string {
	out := make([]string, 0, len(tags)+1)
	for _, tag := range tags {
		if strings.HasPrefix(tag, bindingTagPrefix) {
			continue
		}
		out = append(out, tag)
	}
	if parentID != "" {
		out = append(out, bindingTag(parentID))
	}
	return out
}

// bindingIsActive reports whether a binding opts its project in, or keeps it
// in. Only status active counts: inactive, archived, completed and expired
// bindings all opt the project out.
func bindingIsActive(binding types.BrainEntry) bool {
	return binding.Status == "active"
}

// effectiveAutomation returns the config one project runs under: parent with
// binding's overrides applied. Each class of field follows its own rule:
//
//   - Timing: a binding schedule or every replaces the parent's schedule,
//     every and at as one unit. timezone, stagger, catch_up, calendar,
//     skip_if_event and only_if_event override individually.
//   - Execution: agent, model, executor, target_workdir and execution_mode
//     are resolved as the scheduler resolves them, firstNonEmpty over the
//     top-level and action fields, binding first. Both top-level and action
//     copies are set so every reader agrees. timeout is action-only.
//   - Lifecycle: starts_at and expires_at override when non-empty. max_runs
//     overrides when non-zero, so -1 is explicit unlimited and 0 inherits.
//   - Prompt: prompt_append is appended to the parent's prompt after a blank
//     line. A binding never supplies direct_prompt, trigger.type, action.type
//     or a project filter.
//
// The effective ID stays the parent's, so history and dedup stay keyed to the
// parent. ProjectID is the binding's project and Binding names the binding.
// Modified is the later of the two writes, so the slot floor sees both. The
// result shares no pointers with parent, so callers may not mutate it into the
// parent.
func effectiveAutomation(parent, binding types.BrainEntry) types.BrainEntry {
	eff := parent
	eff.Trigger = copyTrigger(parent.Trigger)
	eff.Action = copyAction(parent.Action)
	if parent.MaxRuns != nil {
		m := *parent.MaxRuns
		eff.MaxRuns = &m
	}
	if eff.Trigger == nil {
		eff.Trigger = &types.TriggerConfig{}
	}
	if eff.Action == nil {
		eff.Action = &types.AutomationAction{}
	}
	eff.ProjectID = binding.ProjectID
	eff.Binding = binding.ID
	eff.Extends = ""

	bindAction := types.AutomationAction{}
	if binding.Action != nil {
		bindAction = *binding.Action
	}
	parentAction := types.AutomationAction{}
	if parent.Action != nil {
		parentAction = *parent.Action
	}

	if bt := binding.Trigger; bt != nil {
		if bt.Schedule != "" || bt.Every != "" {
			eff.Trigger.Schedule = bt.Schedule
			eff.Trigger.Every = bt.Every
			eff.Trigger.At = bt.At
			// An every anchor is the instant its schedule was written, so
			// the binding's created time anchors a binding's schedule.
			if binding.Created != "" {
				eff.Created = binding.Created
			}
		}
		if bt.Stagger != "" {
			eff.Trigger.Stagger = bt.Stagger
		}
		if bt.CatchUp != "" {
			eff.Trigger.CatchUp = bt.CatchUp
		}
		if bt.Calendar != "" {
			eff.Trigger.Calendar = bt.Calendar
		}
		if bt.SkipIfEvent != nil {
			eff.Trigger.SkipIfEvent = bt.SkipIfEvent
		}
		if bt.OnlyIfEvent != nil {
			eff.Trigger.OnlyIfEvent = bt.OnlyIfEvent
		}
	}
	if tz := firstNonEmpty(bindingTriggerTimezone(binding), binding.Timezone); tz != "" {
		eff.Trigger.Timezone = tz
		eff.Timezone = tz
	}

	if binding.StartsAt != "" {
		eff.StartsAt = binding.StartsAt
	}
	if binding.ExpiresAt != "" {
		eff.ExpiresAt = binding.ExpiresAt
	}
	if binding.MaxRuns != nil && *binding.MaxRuns != 0 {
		m := *binding.MaxRuns
		eff.MaxRuns = &m
	}

	agent := firstNonEmpty(binding.Agent, bindAction.Agent, parent.Agent, parentAction.Agent)
	eff.Agent, eff.Action.Agent = agent, agent
	model := firstNonEmpty(binding.Model, bindAction.Model, parent.Model, parentAction.Model)
	eff.Model, eff.Action.Model = model, model
	executor := firstNonEmpty(binding.Executor, bindAction.Executor, parent.Executor, parentAction.Executor)
	eff.Executor, eff.Action.Executor = executor, executor
	workdir := firstNonEmpty(binding.TargetWorkdir, bindAction.TargetWorkdir, parent.TargetWorkdir, parentAction.TargetWorkdir)
	eff.TargetWorkdir, eff.Action.TargetWorkdir = workdir, workdir
	mode := firstNonEmpty(binding.ExecutionMode, bindAction.ExecutionMode, parent.ExecutionMode, parentAction.ExecutionMode)
	eff.ExecutionMode, eff.Action.ExecutionMode = mode, mode
	eff.Action.Timeout = firstNonEmpty(bindAction.Timeout, parentAction.Timeout)

	if bindAction.PromptAppend != "" {
		base := firstNonEmpty(eff.Action.DirectPrompt, eff.DirectPrompt)
		if base != "" {
			base += "\n\n"
		}
		prompt := base + bindAction.PromptAppend
		eff.Action.DirectPrompt = prompt
		eff.DirectPrompt = prompt
	}

	if later, ok := laterModified(parent.Modified, binding.Modified); ok {
		eff.Modified = later
	}
	return eff
}

// bindingTriggerTimezone returns the timezone a binding sets on its trigger.
func bindingTriggerTimezone(binding types.BrainEntry) string {
	if binding.Trigger == nil {
		return ""
	}
	return binding.Trigger.Timezone
}

// laterModified returns whichever RFC 3339 instant is later, as the original
// string. ok is false when neither parses to a later value than the parent.
func laterModified(parent, binding string) (string, bool) {
	if binding == "" {
		return "", false
	}
	b, err := time.Parse(time.RFC3339, binding)
	if err != nil {
		return "", false
	}
	p, err := time.Parse(time.RFC3339, parent)
	if err != nil || b.After(p) {
		return binding, true
	}
	return "", false
}

// copyTrigger returns a copy of tc that shares no mutable state with it.
func copyTrigger(tc *types.TriggerConfig) *types.TriggerConfig {
	if tc == nil {
		return nil
	}
	c := *tc
	if tc.Filter != nil {
		c.Filter = make(map[string]string, len(tc.Filter))
		for k, v := range tc.Filter {
			c.Filter[k] = v
		}
	}
	return &c
}

// copyAction returns a copy of action that shares no mutable state with it.
func copyAction(action *types.AutomationAction) *types.AutomationAction {
	if action == nil {
		return nil
	}
	c := *action
	return &c
}
