package types

// PlacementDenialError identifies an ineligible claiming runner. HTTP adapters
// can use errors.As to distinguish this refusal from conflicts and store errors.
type PlacementDenialError struct {
	Reason string
	Cause  error
}

func (e *PlacementDenialError) Error() string {
	if e.Cause != nil {
		return e.Reason + ": " + e.Cause.Error()
	}
	return e.Reason
}

func (e *PlacementDenialError) Unwrap() error { return e.Cause }

const (
	PlacementRunnerUnregistered        = "runner_unregistered"
	PlacementRequiredCapabilityMissing = "required_capability_missing"
	PlacementGitRemoteIneligible       = "git_remote_ineligible"
)
