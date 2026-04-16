package api

import (
	"context"

	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/realtime"
)

// WebhookAutomationSource provides active automations for webhook matching.
// This is a narrower interface than events.AutomationSource, scoped to the api package.
type WebhookAutomationSource interface {
	ListActiveAutomations(ctx context.Context) ([]events.AutomationEntry, error)
}

// Handler holds service dependencies for HTTP handlers.
type Handler struct {
	brain       BrainService
	tasks       TaskService
	runner      RunnerService
	runners     RunnersService
	monitor     MonitorService
	tokens      TokenService
	hub         *realtime.Hub
	eventBus    events.Bus
	automations WebhookAutomationSource
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

// WithRunnersService sets the RunnersService on the Handler.
func WithRunnersService(rs RunnersService) HandlerOption {
	return func(h *Handler) {
		h.runners = rs
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

// WithEventBus sets the event bus on the Handler.
func WithEventBus(bus events.Bus) HandlerOption {
	return func(h *Handler) {
		h.eventBus = bus
	}
}

// WithAutomationSource sets the automation source for webhook matching.
func WithAutomationSource(src WebhookAutomationSource) HandlerOption {
	return func(h *Handler) {
		h.automations = src
	}
}
