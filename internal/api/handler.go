package api

// Handler holds service dependencies for HTTP handlers.
// Designed to accept additional services (search, tasks, etc.) in later phases.
type Handler struct {
	brain BrainService
}

// NewHandler creates a Handler with the given BrainService.
func NewHandler(brain BrainService) *Handler {
	return &Handler{brain: brain}
}
