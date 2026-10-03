package types

import "time"

const (
	TimelineStateActual    = "actual"
	TimelineStateProjected = "projected"

	TimelineKindExecution = "execution"
	TimelineKindReminder  = "reminder"
	TimelineKindStart     = "start"
	TimelineKindExpiry    = "expiry"
)

// TimelineItem is either a recorded event or an ephemeral future occurrence.
type TimelineItem struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	Source          string            `json:"source"`
	Timestamp       time.Time         `json:"timestamp"`
	ProjectID       string            `json:"project_id,omitempty"`
	TaskID          string            `json:"task_id,omitempty"`
	TaskPath        string            `json:"task_path,omitempty"`
	TaskTitle       string            `json:"task_title,omitempty"`
	FeatureID       string            `json:"feature_id,omitempty"`
	RunnerID        string            `json:"runner_id,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Summary         string            `json:"summary,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	TemporalState   string            `json:"temporal_state"`
	TemporalKind    string            `json:"temporal_kind,omitempty"`
	SourceKind      string            `json:"source_kind,omitempty"`
	SourceID        string            `json:"source_id,omitempty"`
	SourcePath      string            `json:"source_path,omitempty"`
	Timezone        string            `json:"timezone,omitempty"`
	ProjectionRule  string            `json:"projection_rule,omitempty"`
	OccurrenceCount int               `json:"occurrence_count,omitempty"`
	WindowStart     *time.Time        `json:"window_start,omitempty"`
	WindowEnd       *time.Time        `json:"window_end,omitempty"`
}

type TimelineWarning struct {
	SourceID string `json:"source_id,omitempty"`
	Message  string `json:"message"`
}

type TimelineResponse struct {
	From        time.Time         `json:"from"`
	To          time.Time         `json:"to"`
	GeneratedAt time.Time         `json:"generated_at"`
	Items       []TimelineItem    `json:"items"`
	Warnings    []TimelineWarning `json:"warnings"`
	Truncated   bool              `json:"truncated"`
}
