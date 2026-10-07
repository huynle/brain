package commands

// UsageError is a command-line usage mistake (unknown command, renamed
// command, missing argument). The CLI prints Message verbatim to stderr and
// exits with status 2, without the generic "Error:" prefix.
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string { return e.Message }
