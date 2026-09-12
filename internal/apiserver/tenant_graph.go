package apiserver

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/logbuffer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// graphIdentity is supplied only by trusted single-mode composition. Never give
// future tenant graphs the single-mode token adapter or platform capabilities.
type graphIdentity struct {
	tokens    api.TokenService
	verifier  api.CredentialVerifier
	passwords api.PasswordTokenStore
}

// tenantGraph is an immutable binding, not an authorization grant or a pool
// owner. Construction does not install built-ins, scan content or start workers.
// Mutable coordination state is private to this graph. Phase 2 will own leases
// on it; callers must drain requests and stop workers before Close.
type tenantGraph struct {
	id                tenant.ID
	config            config.Config
	handler           *api.Handler
	indexer           *indexer.Indexer
	brain             *service.BrainServiceImpl
	tasks             *service.TaskServiceImpl
	runners           *service.RunnerRegistryServiceImpl
	scheduler         *service.SchedulerService
	cascade           *service.FeatureCascadeService
	automations       *service.AutomationService
	goals             *service.GoalService
	reminders         *service.ReminderService
	webhooks          *service.WebhookServiceImpl
	eventHub          *realtime.EventHub
	hub               *realtime.Hub
	webhookDispatcher *realtime.WebhookDispatcher
	triggerDispatcher *realtime.TriggerDispatcher
	embeddingReady    bool
	closeOnce         sync.Once
}

func newTenantGraph(ctx context.Context, store *storage.TenantStore, roots *tenantfs.Resolver, cfg config.Config, identity graphIdentity) (*tenantGraph, error) {
	if store == nil || roots == nil {
		return nil, fmt.Errorf("tenant graph requires store and authoritative roots")
	}
	mapping, err := roots.Lookup(ctx, store.TenantID())
	if err != nil {
		return nil, fmt.Errorf("bind tenant graph roots: %w", err)
	}
	cfg = copyGraphConfig(cfg)
	cfg.BrainDir = mapping.BrainAbsolute
	cfg.Attachments = normalizeAttachmentConfig(cfg.BrainDir, cfg.Attachments)
	cfg.Attachments.StorageRoot = mapping.BlobAbsolute
	idx := indexer.NewIndexer(cfg.BrainDir, store, roots.Brain(store.TenantID()))
	// This constructor validates the persisted CAS policy and creates its private
	// staging directory. It does not scan content or provision a mapping.
	blobs, err := blobstore.NewTenantFilesystemStore(roots, store.TenantID(), cfg.Attachments.MaxUploadSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("initialize attachment blob store: %w", err)
	}
	var embedding service.EmbeddingClient
	if cfg.Embedding.Enabled {
		embedding, err = service.NewAiFactoryEmbeddingClient(cfg.Embedding)
		if err != nil {
			slog.Warn("Failed to create embedding client, semantic search disabled", "error", err)
			embedding = nil
		}
	}
	brain := service.NewBrainService(&cfg, store, idx, nil, embedding)
	attachments := service.NewAttachmentService(store, blobs, brain, cfg.Attachments.MaxUploadSizeBytes,
		service.WithAttachmentMIMEPolicy(cfg.Attachments.AllowedMIMETypes, cfg.Attachments.BlockedMIMETypes),
		service.WithAttachmentExtractor(service.NewOpenRouterAttachmentExtractor(cfg.AttachmentExtraction)),
		service.WithAttachmentDerivedChangeHook(brain),
	)
	tasks := service.NewTaskService(&cfg, store, idx)
	runner := service.NewRunnerServiceWithStorage(store)
	runners := service.NewRunnerRegistryService(store)
	clients := service.NewClientContextService(store)
	placement := service.NewProjectPlacementService(store)
	monitor := service.NewMonitorService(brain)
	webhooks := service.NewWebhookService(store)
	hub := realtime.NewHub()
	runners.SetHub(hub)
	scheduler := service.NewSchedulerService(tasks, runner, runners, placement, store, hub)
	eventHub := realtime.NewEventHub()
	events := service.NewEventService(eventHub)
	events.SetFeatureTaskLister(tasks)
	events.SetFeatureAssignmentCleaner(store)
	cascade := service.NewFeatureCascadeService(eventHub, scheduler)
	scheduler.SetFeatureCascade(cascade)
	automations := service.NewAutomationService(brain)
	automations.SetPauseChecker(runner)
	// Wildcard automations enumerate this graph's projects, never an ambient
	// deployment-wide project list or an unscoped fallback task.
	automations.SetProjectLister(tasks)
	bridgeHub := bridge.NewHub(hub)
	goals := service.NewGoalService(brain, tasks, store, service.WithGoalSteerer(newBridgeGoalSteerer(runners, bridgeHub)), service.WithGoalPauseChecker(runner))
	assistant := api.NewAssistantService(api.AssistantServiceOptions{
		Enabled:   cfg.Assistant.Enabled,
		Provider:  cfg.Assistant.Provider,
		BaseURL:   cfg.Assistant.BaseURL,
		APIKeyEnv: cfg.Assistant.APIKeyEnv,
		Model:     cfg.Assistant.Model,
		Timeout:   time.Duration(cfg.Assistant.TimeoutMs) * time.Millisecond,
		Brain:     brain,
		Goals:     goals,
		Tasks:     tasks,
		Runner:    runner,
		Runners:   runners,
		Events:    events,
	})
	reminders := service.NewReminderService(brain, store, service.WithReminderEventIngester(events), service.WithReminderPauseChecker(runner))
	handler := api.NewHandler(brain,
		api.WithAttachmentService(attachments),
		api.WithTaskService(tasks),
		api.WithRunnerService(runner),
		api.WithRunnerRegistryService(runners),
		api.WithClientContextService(clients),
		api.WithProjectPlacementService(placement),
		api.WithSchedulerService(scheduler),
		api.WithSchedulerVisibilityService(store),
		api.WithRunTaskService(scheduler),
		api.WithRunFeatureService(scheduler),
		api.WithDependentChainService(scheduler),
		api.WithRunProjectService(scheduler),
		api.WithMonitorService(monitor),
		api.WithTokenService(identity.tokens),
		api.WithHub(hub),
		api.WithEventService(events),
		api.WithWebhookService(webhooks),
		api.WithGoalService(goals),
		api.WithReminderService(reminders),
		api.WithAutomationRunService(automations),
		api.WithAssistantService(assistant),
		api.WithBridgeService(bridgeHub),
		api.WithLogBuffer(logbuffer.New(logbuffer.DefaultMaxLines)),
		api.WithTaskDefaults(cfg.TaskDefaults),
		api.WithCredentialVerifier(identity.verifier),
		api.WithPasswordTokenStore(identity.passwords),
	)
	return &tenantGraph{
		id:                store.TenantID(),
		config:            cfg,
		handler:           handler,
		indexer:           idx,
		brain:             brain,
		tasks:             tasks,
		runners:           runners,
		scheduler:         scheduler,
		cascade:           cascade,
		automations:       automations,
		goals:             goals,
		reminders:         reminders,
		webhooks:          webhooks,
		eventHub:          eventHub,
		hub:               hub,
		webhookDispatcher: realtime.NewWebhookDispatcher(eventHub, webhooks),
		triggerDispatcher: realtime.NewTriggerDispatcher(eventHub, service.NewTriggerService(service.NewTriggerTaskStoreAdapter(store))),
		embeddingReady:    !cfg.Embedding.Enabled || embedding != nil,
	}, nil
}

// Close owns only detached service work. It never closes TenantStore, the shared
// SQLite pool or shared transports. A lease owner must stop producers first.
func (g *tenantGraph) Close() { g.closeOnce.Do(func() { g.brain.Close(); g.webhooks.Close() }) }

// copyGraphConfig explicitly copies all reference-bearing fields of Config.
// Keep this list and the aliasing test current when configuration gains fields.
func copyGraphConfig(cfg config.Config) config.Config {
	cfg.TaskDefaults.Extensions = slices.Clone(cfg.TaskDefaults.Extensions)
	if cfg.TaskDefaults.CompleteOnIdle != nil {
		v := *cfg.TaskDefaults.CompleteOnIdle
		cfg.TaskDefaults.CompleteOnIdle = &v
	}
	if cfg.TaskDefaults.OpenPRBeforeMerge != nil {
		v := *cfg.TaskDefaults.OpenPRBeforeMerge
		cfg.TaskDefaults.OpenPRBeforeMerge = &v
	}
	cfg.Attachments.AllowedMIMETypes = slices.Clone(cfg.Attachments.AllowedMIMETypes)
	cfg.Attachments.BlockedMIMETypes = slices.Clone(cfg.Attachments.BlockedMIMETypes)
	cfg.AttachmentExtraction.SupportedMIMETypes = slices.Clone(cfg.AttachmentExtraction.SupportedMIMETypes)
	return cfg
}
