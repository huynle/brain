package types

// Attention is a durable, per-user notification/attention record. Unlike a
// reminder (whose lifecycle IS the notification), an attention item is a
// first-class inbox entry with its own recipient, state machine, workflow
// links, and typed actions. It is stored in SQL, scoped by tenant + recipient.
type Attention struct {
	ID        string `json:"id"`
	Recipient string `json:"recipient"`

	Kind     string `json:"kind"`
	Severity string `json:"severity"` // info | warning | critical

	Title string `json:"title"`
	Body  string `json:"body,omitempty"`

	// Workflow links. Any subset may be set; the frontend derives safe
	// navigation and actions from these rather than from arbitrary URLs.
	Project    string `json:"project,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	FeatureID  string `json:"feature_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	RunnerID   string `json:"runner_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`

	// Provenance + dedup. dedup_key makes creation idempotent per recipient.
	SourceType string `json:"source_type,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	DedupKey   string `json:"dedup_key,omitempty"`

	State   string             `json:"state"` // unread|read|snoozed|resolved|dismissed
	Actions []AttentionAction  `json:"actions,omitempty"`

	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	ReadAt       string `json:"read_at,omitempty"`
	SnoozedUntil string `json:"snoozed_until,omitempty"`
	ResolvedAt   string `json:"resolved_at,omitempty"`

	// Revision guards state transitions against concurrent writers.
	Revision int `json:"revision"`
}

// AttentionAction is a declarative, closed-kind action descriptor. The kind
// enumerates safe operations; the frontend/handler maps a kind to a concrete
// navigation or workflow call using the item's link fields. Params carries
// small opaque values (e.g. an OpenCode permission_id) needed to execute it.
type AttentionAction struct {
	Kind   string            `json:"kind"`
	Label  string            `json:"label"`
	Params map[string]string `json:"params,omitempty"`
}

// Attention state constants.
const (
	AttentionStateUnread    = "unread"
	AttentionStateRead      = "read"
	AttentionStateSnoozed   = "snoozed"
	AttentionStateResolved  = "resolved"
	AttentionStateDismissed = "dismissed"
)

// Attention severity constants.
const (
	AttentionSeverityInfo     = "info"
	AttentionSeverityWarning  = "warning"
	AttentionSeverityCritical = "critical"
)

// Closed set of action kinds v1 supports. Never execute an action kind that is
// not in this set; there are no arbitrary URLs or executable payloads.
const (
	AttentionActionOpenTask          = "open_task"
	AttentionActionOpenFeature       = "open_feature"
	AttentionActionOpenSession       = "open_session"
	AttentionActionOpenEntry         = "open_entry"
	AttentionActionReplyPermission   = "reply_permission" // OpenCode once|always|reject
	AttentionActionReplyForm         = "reply_form"       // OpenCode form reply
	AttentionActionResumeTask        = "resume_task"
	AttentionActionRetryTask         = "retry_task"
	AttentionActionAnswerCheckpoint  = "answer_checkpoint"
	AttentionActionApproveMerge      = "approve_merge"
	AttentionActionRetryJob          = "retry_job"
)

// PushSubscription is a browser Web Push endpoint registered by a user's
// device, used to deliver attention notifications when the tab is closed. It is
// tenant + recipient scoped and keyed by endpoint (a device may re-register
// with fresh keys, which upserts rather than duplicates).
type PushSubscription struct {
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	CreatedAt string `json:"created_at,omitempty"`
}

// CreateAttentionRequest is the create payload. Recipient defaults to the
// authenticated principal when empty (resolved by the handler, not here).
type CreateAttentionRequest struct {
	Recipient string `json:"recipient,omitempty"`

	Kind     string `json:"kind"`
	Severity string `json:"severity,omitempty"`

	Title string `json:"title"`
	Body  string `json:"body,omitempty"`

	Project    string `json:"project,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	FeatureID  string `json:"feature_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	RunnerID   string `json:"runner_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`

	SourceType string `json:"source_type,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	DedupKey   string `json:"dedup_key,omitempty"`

	Actions []AttentionAction `json:"actions,omitempty"`
}

// AttentionListFilter narrows a recipient's inbox listing.
type AttentionListFilter struct {
	Recipient     string
	State         string
	Project       string
	Kind          string
	Severity      string
	SourceType    string
	IncludeSnoozed bool
	Limit         int
}

// AttentionCounts summarises a recipient's inbox for the bell badge.
type AttentionCounts struct {
	Unread   int `json:"unread"`
	Total    int `json:"total"`
	Critical int `json:"critical"`
}

// ValidAttentionSeverity reports whether s is a known severity. Empty is
// allowed by the caller (it defaults to info) and is NOT accepted here.
func ValidAttentionSeverity(s string) bool {
	switch s {
	case AttentionSeverityInfo, AttentionSeverityWarning, AttentionSeverityCritical:
		return true
	}
	return false
}

// ValidAttentionState reports whether s is a known lifecycle state.
func ValidAttentionState(s string) bool {
	switch s {
	case AttentionStateUnread, AttentionStateRead, AttentionStateSnoozed,
		AttentionStateResolved, AttentionStateDismissed:
		return true
	}
	return false
}
