package api

import (
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/logbuffer"
	"github.com/huynle/brain-api/internal/realtime"
)

// Handler holds service dependencies for HTTP handlers.
type Handler struct {
	brain          BrainService
	attachments    AttachmentService
	tasks          TaskService
	runner         RunnerService
	runnerRegistry RunnerRegistryService
	monitor        MonitorService
	tokens         TokenService
	events         EventService
	webhooks       WebhookService
	hub            *realtime.Hub
	logBuffer      *logbuffer.Buffer
	taskDefaults   config.TaskDefaultsConfig
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

// WithAttachmentService sets the AttachmentService on the Handler.
func WithAttachmentService(as AttachmentService) HandlerOption {
	return func(h *Handler) {
		h.attachments = as
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

// WithRunnerRegistryService sets the RunnerRegistryService on the Handler.
func WithRunnerRegistryService(rrs RunnerRegistryService) HandlerOption {
	return func(h *Handler) {
		h.runnerRegistry = rrs
	}
}

// WithEventService sets the EventService on the Handler.
func WithEventService(es EventService) HandlerOption {
	return func(h *Handler) {
		h.events = es
	}
}

// WithWebhookService sets the WebhookService on the Handler.
func WithWebhookService(ws WebhookService) HandlerOption {
	return func(h *Handler) {
		h.webhooks = ws
	}
}

// WithHub sets the realtime Hub on the Handler.
func WithHub(hub *realtime.Hub) HandlerOption {
	return func(h *Handler) {
		h.hub = hub
	}
}

// WithTaskDefaults sets the task defaults configuration on the Handler.
func WithTaskDefaults(td config.TaskDefaultsConfig) HandlerOption {
	return func(h *Handler) {
		h.taskDefaults = td
	}
}
