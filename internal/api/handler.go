package api

import "github.com/huynle/brain-api/internal/realtime"

// Handler holds service dependencies for HTTP handlers.
type Handler struct {
	brain   BrainService
	tasks   TaskService
	runner  RunnerService
	monitor MonitorService
	hub     *realtime.Hub
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// NewHandler creates a Handler with the given BrainService and optional services.
func NewHandler(brain BrainService, opts ...HandlerOption) *Handler {
	h := &Handler{brain: brain}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithTaskService sets the TaskService on the Handler.
func WithTaskService(ts TaskService) HandlerOption {
	return func(h *Handler) {
		h.tasks = ts
	}
}

// WithRunnerService sets the RunnerService on the Handler.
func WithRunnerService(rs RunnerService) HandlerOption {
	return func(h *Handler) {
		h.runner = rs
	}
}

// WithMonitorService sets the MonitorService on the Handler.
func WithMonitorService(ms MonitorService) HandlerOption {
	return func(h *Handler) {
		h.monitor = ms
	}
}

// WithHub sets the realtime Hub on the Handler.
func WithHub(hub *realtime.Hub) HandlerOption {
	return func(h *Handler) {
		h.hub = hub
	}
}
