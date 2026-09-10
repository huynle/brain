package types

import "time"

// ResourceSample is measured by the existing runner memory guard. Null
// measurements are unavailable, not zero. No command arguments are collected.
type ResourceSample struct {
	InstanceID        string     `json:"instance_id,omitempty"`
	SessionID         string     `json:"session_id,omitempty"`
	Executor          string     `json:"executor,omitempty"`
	SampledAt         time.Time  `json:"sampled_at"`
	RSSBytes          *int64     `json:"rss_bytes"`
	ProcessCount      *int       `json:"process_count"`
	LimitBytes        int64      `json:"limit_bytes"`
	LastActivity      *time.Time `json:"last_activity"`
	CommandSummary    *string    `json:"command_summary"`
	UnavailableReason string     `json:"unavailable_reason,omitempty"`
	TerminationReason string     `json:"termination_reason,omitempty"`
	Warning           bool       `json:"warning"`
}
