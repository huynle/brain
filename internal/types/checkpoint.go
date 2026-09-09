package types

import "time"

type SupervisorCheckpoint struct {
	ID                    string     `json:"id"`
	Project               string     `json:"project"`
	TaskID                string     `json:"task_id,omitempty"`
	FeatureID             string     `json:"feature_id,omitempty"`
	Artifact              string     `json:"artifact"`
	Question              string     `json:"question"`
	State                 string     `json:"state"`
	Answer                string     `json:"answer,omitempty"`
	AnsweredBy            string     `json:"answered_by,omitempty"`
	AnsweredAt            *time.Time `json:"answered_at,omitempty"`
	VerificationReference string     `json:"verification_reference,omitempty"`
	VerifiedBy            string     `json:"verified_by,omitempty"`
	Revision              int        `json:"revision"`
}
