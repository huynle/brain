package types

// AutomationEffective is the config one project runs one automation under: the
// parent automation with the project's binding (if any) overlaid, exactly as
// the scheduler resolves it. Targeted reports whether the scheduler fires the
// automation for that project. The view is read-only and nothing here is
// persisted.
type AutomationEffective struct {
	// ID is the parent automation's ID. A binding input reports the parent its
	// extends tag names.
	ID string `json:"id"`
	// Project is the project the view describes.
	Project string `json:"project"`
	// Automation is the effective config. It is absent when Broken names a
	// parent that cannot be read.
	Automation *AutomationEffectiveConfig `json:"automation,omitempty"`
	// Fields maps each overridable field to EffectiveFieldInherited or
	// EffectiveFieldOverridden. A project-owned automation has no parent, so
	// Fields is empty.
	Fields map[string]string `json:"fields"`
	// BindingID and BindingStatus name the binding that governs Project. An
	// opted-out project keeps its inactive binding here.
	BindingID     string `json:"binding_id,omitempty"`
	BindingStatus string `json:"binding_status,omitempty"`
	// Targeted reports whether the scheduler fires the automation for Project.
	// It covers project selection, binding opt-in and opt-out, and the parent's
	// status. It does not evaluate the lifecycle window or an event's type.
	Targeted bool `json:"targeted"`
	// Broken is true when the view cannot be trusted as the one the scheduler
	// uses. BrokenReason then names why.
	Broken       bool   `json:"broken"`
	BrokenReason string `json:"broken_reason,omitempty"`
}

// AutomationEffectiveConfig is the effective trigger, action and lifecycle of
// one automation for one project.
type AutomationEffectiveConfig struct {
	ID        string            `json:"id"`
	Trigger   *TriggerConfig    `json:"trigger,omitempty"`
	Action    *AutomationAction `json:"action,omitempty"`
	Timezone  string            `json:"timezone,omitempty"`
	StartsAt  string            `json:"starts_at,omitempty"`
	ExpiresAt string            `json:"expires_at,omitempty"`
	MaxRuns   *int              `json:"max_runs,omitempty"`
}

// Field states reported in AutomationEffective.Fields.
const (
	EffectiveFieldInherited  = "inherited"
	EffectiveFieldOverridden = "overridden"
)

// Reasons an AutomationEffective view is broken.
const (
	// EffectiveBrokenDuplicateBinding: more than one binding exists for the
	// project. The oldest governs, and the others are ignored.
	EffectiveBrokenDuplicateBinding = "duplicate_binding"
	// EffectiveBrokenParentMissing: the binding's extends names no entry.
	EffectiveBrokenParentMissing = "parent_missing"
	// EffectiveBrokenParentNotAutomation: the entry extends names something
	// that is not a global automation.
	EffectiveBrokenParentNotAutomation = "parent_not_automation"
)
