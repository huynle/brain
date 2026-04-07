package types

// =============================================================================
// Automation Entry Types
// =============================================================================

// AutomationTrigger defines when an automation fires.
type AutomationTrigger struct {
	Type                   string            `json:"type" yaml:"type"`                                                             // cron, event, webhook, session
	Event                  string            `json:"event,omitempty" yaml:"event,omitempty"`                                       // event name for type=event
	Schedule               string            `json:"schedule,omitempty" yaml:"schedule,omitempty"`                                 // cron expression for type=cron
	Filter                 map[string]string `json:"filter,omitempty" yaml:"filter,omitempty"`                                     // key-value filter conditions
	OncePer                string            `json:"once_per,omitempty" yaml:"once_per,omitempty"`                                 // dedup key (e.g. "session", "day")
	Webhook                string            `json:"webhook,omitempty" yaml:"webhook,omitempty"`                                   // webhook path for type=webhook
	IgnoreAutomationEvents *bool             `json:"ignore_automation_events,omitempty" yaml:"ignore_automation_events,omitempty"` // default true; set false to process automation-generated events
}

// AutomationAction defines what an automation does when triggered.
type AutomationAction struct {
	Type               string `json:"type" yaml:"type"`                                                   // prompt, script, update, http
	DirectPrompt       string `json:"direct_prompt,omitempty" yaml:"direct_prompt,omitempty"`             // prompt text for type=prompt
	Command            string `json:"command,omitempty" yaml:"command,omitempty"`                         // shell command for type=script
	Agent              string `json:"agent,omitempty" yaml:"agent,omitempty"`                             // agent to use for type=prompt
	Model              string `json:"model,omitempty" yaml:"model,omitempty"`                             // model override
	ExecutionMode      string `json:"execution_mode,omitempty" yaml:"execution_mode,omitempty"`           // worktree, current_branch
	CompleteOnIdle     *bool  `json:"complete_on_idle,omitempty" yaml:"complete_on_idle,omitempty"`       // auto-complete on idle
	Timeout            string `json:"timeout,omitempty" yaml:"timeout,omitempty"`                         // execution timeout (e.g. "5m", "1h")
	RequiresCapability string `json:"requires_capability,omitempty" yaml:"requires_capability,omitempty"` // required runner capability
}

// AutomationRetry defines retry behavior for failed automation actions.
type AutomationRetry struct {
	MaxAttempts int    `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"` // max retry attempts (default: 0 = no retry)
	Backoff     string `json:"backoff,omitempty" yaml:"backoff,omitempty"`           // fixed, exponential
	Delay       string `json:"delay,omitempty" yaml:"delay,omitempty"`               // delay between retries (e.g. "30s", "5m")
}
