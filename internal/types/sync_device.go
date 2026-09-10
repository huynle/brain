package types

// Browser reports are observations, never proof that a disconnected device is clean.
type SyncPending struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Method   string `json:"method"`
	Revision string `json:"revision"`
	Raw      string `json:"raw"`
	Error    string `json:"error,omitempty"`
	Failure  string `json:"failure,omitempty"`
}
type SyncCommand struct {
	ID             string `json:"id"`
	OperationID    string `json:"operation_id"`
	Action         string `json:"action"`
	ExpectedRaw    string `json:"expected_raw"`
	ServerRevision string `json:"server_revision"`
	Raw            string `json:"raw,omitempty"`
	Outcome        string `json:"outcome,omitempty"`
}
type SyncDevice struct {
	ID         string        `json:"device_id"`
	Owner      string        `json:"owner,omitempty"`
	LastSeen   string        `json:"last_seen"`
	Connection string        `json:"connection"`
	Online     bool          `json:"reported_online"`
	Syncing    bool          `json:"syncing"`
	Ready      bool          `json:"ready"`
	Cursor     int64         `json:"cursor"`
	Epoch      string        `json:"epoch"`
	LastSync   string        `json:"last_successful_sync,omitempty"`
	Error      string        `json:"error,omitempty"`
	Pending    []SyncPending `json:"pending"`
	Command    *SyncCommand  `json:"command,omitempty"`
}
