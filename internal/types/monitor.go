package types

// =============================================================================
// Monitor Types
// =============================================================================

// MonitorScope defines the scope for a monitor (all, project, or feature).
type MonitorScope struct {
	Type      string `json:"type"` // "all", "project", or "feature"
	Project   string `json:"project,omitempty"`
	FeatureID string `json:"feature_id,omitempty"`
}

// MonitorTagResult holds the parsed result of a monitor tag.
type MonitorTagResult struct {
	TemplateID string       `json:"template_id"`
	Scope      MonitorScope `json:"scope"`
}

// MonitorInfo holds information about an active monitor.
type MonitorInfo struct {
	ID         string       `json:"id"`
	Path       string       `json:"path"`
	TemplateID string       `json:"template_id"`
	Scope      MonitorScope `json:"scope"`
	Enabled    bool         `json:"enabled"`
	Schedule   string       `json:"schedule"`
	Title      string       `json:"title"`
}

// MonitorTemplate defines a reusable template for recurring monitoring tasks.
type MonitorTemplate struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	DefaultSchedule string   `json:"default_schedule"`
	Tags            []string `json:"tags"`
}

// CreateMonitorResult holds the result of creating a monitor.
type CreateMonitorResult struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

// CreateMonitorOptions holds optional parameters for creating a monitor.
type CreateMonitorOptions struct {
	Schedule string `json:"schedule,omitempty"`
	Project  string `json:"project,omitempty"`
}

// MonitorListFilter holds optional filters for listing monitors.
type MonitorListFilter struct {
	Project    string `json:"project,omitempty"`
	FeatureID  string `json:"feature_id,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
}

// MonitorFindResult holds the result of finding a monitor.
type MonitorFindResult struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`
}

// =============================================================================
// Monitor Request/Response Types
// =============================================================================

// CreateMonitorRequest is the HTTP request body for creating a monitor.
type CreateMonitorRequest struct {
	TemplateID string `json:"template_id"`
	Project    string `json:"project,omitempty"`
	FeatureID  string `json:"feature_id,omitempty"`
	ScopeType  string `json:"scope_type"` // "all", "project", or "feature"
	Schedule   string `json:"schedule,omitempty"`
}

// ToggleMonitorRequest is the HTTP request body for toggling a monitor.
type ToggleMonitorRequest struct {
	Enabled bool `json:"enabled"`
}

// MonitorToggleResponse is the HTTP response for toggling a monitor.
type MonitorToggleResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
}

// MonitorDeleteResponse is the HTTP response for deleting a monitor.
type MonitorDeleteResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
}

// MonitorTemplatesResponse is the HTTP response for listing templates.
type MonitorTemplatesResponse struct {
	Templates []MonitorTemplate `json:"templates"`
}

// MonitorListResponse is the HTTP response for listing monitors.
type MonitorListResponse struct {
	Monitors []MonitorInfo `json:"monitors"`
}
