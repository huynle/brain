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
	clientContext  ClientContextService
	monitor        MonitorService
	tokens         TokenService
	events         EventService
	webhooks       WebhookService
	goalService    GoalService
	automationRun  AutomationRunService
	assistant      *AssistantService
	placement      ProjectPlacementService
	scheduler      SchedulerService
	schedulerViews SchedulerVisibilityService
	runTask        RunTaskService
	runFeature     RunFeatureService
	runProject     RunProjectService
	bridge         BridgeService
	hub            *realtime.Hub
	logBuffer      *logbuffer.Buffer
	taskDefaults   config.TaskDefaultsConfig
	credentials    CredentialVerifier
	passwordTokens PasswordTokenStore
	loginThrottle  *loginThrottle
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// NewHandler creates a Handler with the given BrainService and optional services.
func NewHandler(brain BrainService, opts ...HandlerOption) *Handler {
	h := &Handler{brain: brain, loginThrottle: newLoginThrottle()}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithCredentialVerifier enables the password login flow with the given verifier.
func WithCredentialVerifier(v CredentialVerifier) HandlerOption {
	return func(h *Handler) {
		h.credentials = v
	}
}

// WithPasswordTokenStore sets the store used to issue/rotate password-login tokens.
func WithPasswordTokenStore(s PasswordTokenStore) HandlerOption {
	return func(h *Handler) {
		h.passwordTokens = s
	}
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

// WithClientContextService sets the ClientContextService on the Handler.
func WithClientContextService(ccs ClientContextService) HandlerOption {
	return func(h *Handler) {
		h.clientContext = ccs
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

// WithGoalService sets the GoalService on the Handler.
func WithGoalService(gs GoalService) HandlerOption {
	return func(h *Handler) {
		h.goalService = gs
	}
}

// WithAutomationRunService sets the manual automation-run service on the Handler.
func WithAutomationRunService(ar AutomationRunService) HandlerOption {
	return func(h *Handler) {
		h.automationRun = ar
	}
}

// WithAssistantService sets the built-in assistant service on the Handler.
func WithAssistantService(as *AssistantService) HandlerOption {
	return func(h *Handler) {
		h.assistant = as
	}
}

// WithBridgeService sets the runner BridgeService on the Handler.
func WithBridgeService(b BridgeService) HandlerOption {
	return func(h *Handler) {
		h.bridge = b
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

// WithProjectPlacementService sets the project placement service on the Handler.
func WithProjectPlacementService(ps ProjectPlacementService) HandlerOption {
	return func(h *Handler) {
		h.placement = ps
	}
}

// WithSchedulerService sets the scheduler service on the Handler.
func WithSchedulerService(s SchedulerService) HandlerOption {
	return func(h *Handler) {
		h.scheduler = s
	}
}

// WithSchedulerVisibilityService sets the scheduler visibility service on the Handler.
func WithSchedulerVisibilityService(s SchedulerVisibilityService) HandlerOption {
	return func(h *Handler) {
		h.schedulerViews = s
	}
}

// WithRunTaskService sets the RunTaskService used by HandleRunTask. When
// unset, /tasks/{project}/{task}/run returns 501 Not Implemented so clients
// fall back to /trigger.
func WithRunTaskService(s RunTaskService) HandlerOption {
	return func(h *Handler) {
		h.runTask = s
	}
}

// WithRunFeatureService sets the RunFeatureService used by HandleRunFeature.
// When unset, /tasks/{project}/features/{feature}/run returns 501 Not
// Implemented so clients can show a clear "feature not supported" message
// instead of a confusing dispatch failure.
func WithRunFeatureService(s RunFeatureService) HandlerOption {
	return func(h *Handler) {
		h.runFeature = s
	}
}

// WithRunProjectService sets the RunProjectService used by HandleRunProject.
// When unset, POST /tasks/{project}/run returns 501 Not Implemented so the
// PWA can fall back gracefully (project-run is a batch convenience over the
// per-feature endpoint that IS wired).
func WithRunProjectService(s RunProjectService) HandlerOption {
	return func(h *Handler) {
		h.runProject = s
	}
}
