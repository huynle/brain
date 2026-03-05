// Package doctor provides diagnostic and fix operations for brain installations.
package doctor

// CheckStatus represents the result status of a diagnostic check.
type CheckStatus int

const (
	CheckStatusPass CheckStatus = iota
	CheckStatusWarn
	CheckStatusFail
	CheckStatusSkip
)

// String returns the string representation of the check status.
func (s CheckStatus) String() string {
	switch s {
	case CheckStatusPass:
		return "pass"
	case CheckStatusWarn:
		return "warn"
	case CheckStatusFail:
		return "fail"
	case CheckStatusSkip:
		return "skip"
	default:
		return "unknown"
	}
}

// Symbol returns the emoji symbol for the check status.
func (s CheckStatus) Symbol() string {
	switch s {
	case CheckStatusPass:
		return "✅"
	case CheckStatusWarn:
		return "⚠️"
	case CheckStatusFail:
		return "❌"
	case CheckStatusSkip:
		return "⏭"
	default:
		return "?"
	}
}

// Check represents a single diagnostic check result.
type Check struct {
	Name    string
	Status  CheckStatus
	Message string
	Fixable bool
}

// DoctorOptions contains options for running doctor diagnostics.
type DoctorOptions struct {
	Fix              bool
	Force            bool
	DryRun           bool
	Verbose          bool
	SkipVersionCheck bool
	BrainDir         string
}

// DoctorResult contains the results of running doctor diagnostics.
type DoctorResult struct {
	Checks  []Check
	Summary string
}
