package types

import "time"

// DeliveryVerification is opt-in and independent of task status. Only the
// dedicated admin endpoint writes it; arbitrary metadata PATCH cannot attest.
type DeliveryVerification struct {
	VerificationError string            `json:"verification_error,omitempty"`
	Revision          int               `json:"revision"`
	Required          string            `json:"required"`   // none, merged, integrated
	Repository        string            `json:"repository"` // GitHub owner/repo
	PullRequest       int               `json:"pull_request"`
	Head              string            `json:"head"`
	Target            string            `json:"target"`
	RequiredChecks    []string          `json:"required_checks"`
	Evidence          *DeliveryEvidence `json:"evidence,omitempty"`
}
type DeliveryEvidence struct {
	Head                 string            `json:"head"`
	Target               string            `json:"target"`
	MergeCommit          string            `json:"merge_commit"`
	Checks               map[string]string `json:"checks"`
	ReviewAccepted       bool              `json:"review_accepted"`
	Merged               bool              `json:"merged"`
	IntegrationPassed    bool              `json:"integration_passed"`
	IntegrationVerifier  string            `json:"integration_verifier,omitempty"`
	IntegrationReference string            `json:"integration_reference,omitempty"`
	IntegrationArtifact  string            `json:"integration_artifact,omitempty"`
	VerifiedAt           time.Time         `json:"verified_at"`
	Verifier             string            `json:"verifier"`
	Source               string            `json:"source"`
}

func (d *DeliveryVerification) Unmet() []string {
	if d == nil || d.Required == "" || d.Required == "none" {
		return nil
	}
	missing := []string{}
	if d.Required != "merged" && d.Required != "integrated" {
		return []string{"unknown_delivery_policy"}
	}
	e := d.Evidence
	if e == nil {
		return []string{"delivery_evidence"}
	}
	if d.Repository == "" || d.PullRequest <= 0 || d.Head == "" || d.Target == "" {
		missing = append(missing, "artifact_identity")
	}
	if e.Head != d.Head || e.Target != d.Target {
		missing = append(missing, "current_head_and_target")
	}
	if e.Verifier == "" || e.VerifiedAt.IsZero() || e.Source != "github" {
		missing = append(missing, "provider_verification")
	}
	if !e.ReviewAccepted {
		missing = append(missing, "accepted_review")
	}
	if !e.Merged || e.MergeCommit == "" {
		missing = append(missing, "actual_merge")
	}
	for _, name := range d.RequiredChecks {
		if e.Checks[name] != "success" {
			missing = append(missing, "check:"+name)
		}
	}
	if d.Required == "integrated" && (!e.IntegrationPassed || e.IntegrationArtifact != e.MergeCommit || e.IntegrationVerifier == "" || e.IntegrationReference == "") {
		missing = append(missing, "integration_at_merge_commit")
	}
	return missing
}
