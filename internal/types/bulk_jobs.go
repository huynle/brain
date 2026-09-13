package types

// BulkJobRequest names a bounded, immutable union of paths and filters.
// RequestID is an idempotency key: retry an uncertain submission with the SAME
// key and body to recover its existing job rather than submit another mutation.
type BulkJobRequest struct {
	Label         string             `json:"label,omitempty"`
	RequestID     string             `json:"request_id"`
	Operation     string             `json:"operation"` // archive, delete, set_status, move
	Paths         []string           `json:"paths,omitempty"`
	Filters       []BulkUpdateFilter `json:"filters,omitempty"`
	Status        string             `json:"status,omitempty"`
	TargetProject string             `json:"target_project,omitempty"`
	Force         bool               `json:"force,omitempty"`
}

type BulkJob struct {
	Label       string         `json:"label,omitempty"`
	ID          string         `json:"id"`
	RequestID   string         `json:"request_id"`
	Operation   string         `json:"operation"`
	State       string         `json:"state"`
	SubmittedBy string         `json:"submitted_by"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Total       int            `json:"total"`
	Pending     int            `json:"pending"`
	Running     int            `json:"running"`
	Succeeded   int            `json:"succeeded"`
	Failed      int            `json:"failed"`
	Uncertain   int            `json:"uncertain"`
	Skipped     int            `json:"skipped"`
	Request     BulkJobRequest `json:"-"`
	RequestHash string         `json:"-"`
}

type BulkJobItem struct {
	Sequence    int    `json:"sequence"`
	Path        string `json:"path"`
	EntryID     string `json:"entry_id"`
	Title       string `json:"title"`
	Fingerprint string `json:"-"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	Error       string `json:"error,omitempty"`
	Destination string `json:"destination,omitempty"`
}
