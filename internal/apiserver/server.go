package apiserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/logbuffer"
	mcppkg "github.com/huynle/brain-api/internal/mcp"
	"github.com/huynle/brain-api/internal/oauth"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/webui"
	"github.com/huynle/brain-api/pkg/pathutil"
)

// ServerOptions holds configuration for running the Brain API server.
type ServerOptions struct {
	Host       string
	Port       int
	BrainDir   string
	EnableAuth bool
	LogLevel   string
	// LogWriter, when set, receives all slog output instead of os.Stderr.
	// Callers use it to direct server logs to the configured log_file so
	// `brain api logs` works regardless of how the server was started.
	LogWriter       io.Writer
	CORSOrigin      string
	OAuthPIN        string
	JWTSecret       string
	TaskDefaults    config.TaskDefaultsConfig
	FeatureCheckout config.FeatureCheckoutConfig
	// IndexWatch, when enabled, runs a filesystem watcher that re-indexes
	// out-of-band writes to BrainDir. Off by default; see
	// config.IndexWatchConfig for why.
	IndexWatch  config.IndexWatchConfig
	Embedding   config.EmbeddingConfig
	Attachments config.AttachmentConfig

	AttachmentExtraction config.AttachmentExtractionConfig
	Assistant            config.AssistantConfig

	// TLSCert / TLSKey, when both set, cause the server to run TLS via
	// ListenAndServeTLS. Go's net/http auto-enables HTTP/2 on TLS servers
	// through the h2 package's implicit registration, so no separate http2
	// wiring is needed. When either is empty, the server runs plain HTTP/1.1.
	TLSCert string
	TLSKey  string
}

const defaultAttachmentMaxUploadSizeBytes int64 = 100 * 1024 * 1024

func normalizeAttachmentConfig(brainDir string, cfg config.AttachmentConfig) config.AttachmentConfig {
	if cfg.StorageRoot == "" {
		cfg.StorageRoot = filepath.Join(brainDir, "attachments")
	}
	if cfg.MaxUploadSizeBytes <= 0 {
		cfg.MaxUploadSizeBytes = defaultAttachmentMaxUploadSizeBytes
	}
	return cfg
}

// trustSelfSignedCertForLoopback adds the server's own TLS cert to
// http.DefaultTransport's root pool so in-process HTTP clients (the embedded
// runner, MCP dispatcher, health checkers) can validate the connection to
// https://localhost. Without this every one of those callers would need
// individually-configured InsecureSkipVerify, which is easy to get wrong and
// hard to audit. Scoped to loopback hosts on the assumption that a real
// deployment either uses a public CA cert or configures each client's trust
// store out-of-band; the panes-v2 dev loop is the target use case.
func trustSelfSignedCertForLoopback(certPath, host string) error {
	if host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return nil
	}
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read tls cert: %w", err)
	}
	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(pemBytes) {
		return fmt.Errorf("no PEM certs in %s", certPath)
	}
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("http.DefaultTransport is not *http.Transport (got %T)", http.DefaultTransport)
	}
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.RootCAs = rootCAs
	return nil
}

// RunServer starts the Brain API HTTP server and blocks until context is cancelled.
// Returns error if server fails to start or encounters an error during shutdown.
func RunServer(ctx context.Context, opts ServerOptions) error {
	// Configure structured logging
	var logLevel slog.Level
	switch opts.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	logOut := io.Writer(os.Stderr)
	if opts.LogWriter != nil {
		logOut = opts.LogWriter
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{
		Level: logLevel,
	})))

	router, dbPath, cleanup, err := buildHTTPHandler(ctx, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	// ─── HTTP Server ────────────────────────────────────────────────
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	corsOrigin := opts.CORSOrigin
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	srv := &http.Server{
		Addr:        addr,
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout disabled (0) to support SSE long-lived streaming connections.
		// Heartbeats at 15s intervals keep SSE connections alive and detect dead clients.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
		// Route net/http's own diagnostics (connection errors, recovered
		// per-request panics) through the same writer as slog. Otherwise they
		// go to os.Stderr, which in daemon mode is an inherited fd on the log
		// file that strands on the rotated backup after the writer rotates.
		// Note: unrecovered Go *runtime* panics still write fd 2 directly and
		// bypass this — but such a panic terminates the daemon, so no further
		// rotation occurs and the trace remains in the active file.
		ErrorLog: log.New(logOut, "", log.LstdFlags),
	}

	// Start server in background
	errCh := make(chan error, 1)
	tlsEnabled := opts.TLSCert != "" && opts.TLSKey != ""

	// BIND SYNCHRONOUSLY, before the goroutine exists.
	//
	// ListenAndServe binds inside the goroutine, so the bind result could
	// only reach the caller through errCh — and the select below races that
	// channel against ctx.Done(). Go chooses uniformly at random among ready
	// select cases, so a port conflict that coincided with cancellation
	// returned nil: brain-api logged "server stopped" and exited 0 having
	// never bound the port. A test caught it as flakiness, but an operator
	// would have seen a successful-looking start with nothing listening.
	//
	// net.Listen + Serve is exactly what ListenAndServe does internally, so
	// behaviour is unchanged — but the bind becomes a return value that no
	// context deadline can outrace.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	// Shutdown closes the listener once Serve has registered it, but there is
	// a window before that where returning via ctx.Done() would leak the fd.
	// The stdlib's ListenAndServe does the same for the same reason.
	defer func() { _ = ln.Close() }()
	if tlsEnabled {
		if err := trustSelfSignedCertForLoopback(opts.TLSCert, opts.Host); err != nil {
			slog.Warn("failed to trust server cert for in-process clients; runner/MCP calls to loopback https may fail with x509 errors",
				"err", err, "cert", opts.TLSCert)
		}
	}
	go func() {
		scheme := "http"
		if tlsEnabled {
			scheme = "https"
		}
		slog.Info("starting brain-api",
			"addr", addr,
			"scheme", scheme,
			"brain_dir", opts.BrainDir,
			"db_path", dbPath,
			"auth_enabled", opts.EnableAuth,
			"oauth_enabled", true,
			"cors_origin", corsOrigin,
			"tls", tlsEnabled,
		)
		var err error
		if tlsEnabled {
			// Go's net/http registers h2 via golang.org/x/net/http2 when the
			// server has a non-nil TLSConfig or is started via *TLS(); h2 is
			// negotiated through ALPN. Browsers only speak h2 over TLS, which
			// is why plain HTTP hits the 6-conn-per-origin cap for our SSE
			// streams. See docs/panes-v2-followups.md.
			err = srv.ServeTLS(ln, opts.TLSCert, opts.TLSKey)
		} else {
			err = srv.Serve(ln)
		}
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		slog.Info("shutting down server")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown with 10s timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	slog.Info("server stopped")
	return nil
}

func buildHTTPHandler(ctx context.Context, opts ServerOptions) (http.Handler, string, func(), error) {
	// ─── Storage Layer ──────────────────────────────────────────────
	// Expand ~ to home directory (Go does not do this automatically)
	opts.BrainDir = pathutil.ExpandTilde(opts.BrainDir)
	dataDir := config.MigrateDataDir(opts.BrainDir)
	dbPath := filepath.Join(dataDir, "brain.db")

	// Ensure the data directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, "", nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	store, err := storage.New(dbPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}
	cleanup := func() { _ = store.Close() }

	// ─── Indexer ────────────────────────────────────────────────────
	idx := indexer.NewIndexer(opts.BrainDir, store)

	// The boot index is a one-shot pass. Writes that reach BrainDir without
	// going through the API — a git pull into the brain dir, a manual edit,
	// another process — are invisible to search, the link graph, and orphan
	// detection until the next restart, unless the optional watcher below is
	// enabled to re-index them as they land.
	var fileWatcher *indexer.FileWatcher
	// watchMu serializes the deferred Start() below against cleanup's Stop().
	// Without it a shutdown landing between the two would leave a watcher
	// running against a closed store. Holding it across Start() means
	// shutdown waits for the watcher's initial directory walk, which is a
	// fraction of the boot index and bounded.
	var watchMu sync.Mutex
	watchStopped := false
	if opts.IndexWatch.Enabled {
		fw, err := indexer.NewFileWatcher(opts.BrainDir, idx, &indexer.FileWatcherOptions{
			DebounceMs:     opts.IndexWatch.DebounceMs,
			IgnorePatterns: opts.IndexWatch.IgnorePatterns,
		})
		if err != nil {
			cleanup()
			return nil, "", nil, fmt.Errorf("failed to create index file watcher: %w", err)
		}
		fileWatcher = fw
		// Stop the watcher before the store closes, so no debounced flush
		// can fire an IndexFile against a closed database.
		storeCleanup := cleanup
		cleanup = func() {
			watchMu.Lock()
			watchStopped = true
			fileWatcher.Stop()
			watchMu.Unlock()
			storeCleanup()
		}
	}

	// Run incremental index in the background so it does NOT block HTTP
	// server startup. On large .brain directories (60k+ files) a full scan
	// takes 15+ seconds, and callers like the embedded runner wait on
	// /health with a bounded deadline (see cmd/brain/commands/lifecycle.go).
	// The previous SQLite index remains valid for reads while the re-scan
	// runs; new/changed/deleted files just show up a few seconds late.
	go func() {
		slog.Info("indexing brain directory", "dir", opts.BrainDir)
		result, err := idx.IndexChanged()
		if err != nil {
			slog.Warn("indexing failed, continuing with stale index", "error", err)
		} else {
			slog.Info("indexing complete",
				"added", result.Added,
				"updated", result.Updated,
				"deleted", result.Deleted,
				"skipped", result.Skipped,
				"errors", len(result.Errors),
				"duration", result.Duration,
			)
		}

		// Start watching only after the boot scan finishes, so the two
		// aren't racing to index the same paths. Started even when the scan
		// failed — a stale index is exactly the case that benefits most from
		// incremental updates.
		if fileWatcher == nil {
			return
		}
		watchMu.Lock()
		defer watchMu.Unlock()
		if watchStopped {
			return // shut down while the boot index was running
		}
		if err := fileWatcher.Start(); err != nil {
			slog.Warn("index file watcher failed to start, out-of-band writes need a restart to appear",
				"dir", opts.BrainDir, "error", err)
			return
		}
		slog.Info("watching brain directory for out-of-band writes", "dir", opts.BrainDir)
	}()

	// ─── Build Config ───────────────────────────────────────────────
	corsOrigin := opts.CORSOrigin
	if corsOrigin == "" {
		corsOrigin = "*" // Match standalone brain-api default
	}
	cfg := config.Config{
		BrainDir:        opts.BrainDir,
		Host:            opts.Host,
		Port:            opts.Port,
		EnableAuth:      opts.EnableAuth,
		CORSOrigin:      corsOrigin,
		OAuthPIN:        opts.OAuthPIN,
		JWTSecret:       opts.JWTSecret,
		TaskDefaults:    opts.TaskDefaults,
		FeatureCheckout: opts.FeatureCheckout,
		Embedding:       opts.Embedding,
		Attachments:     normalizeAttachmentConfig(opts.BrainDir, opts.Attachments),

		AttachmentExtraction: opts.AttachmentExtraction,
		Assistant:            opts.Assistant,
	}

	// ─── Services ───────────────────────────────────────────────────
	// Create embedding client if enabled
	var embeddingClient service.EmbeddingClient
	if cfg.Embedding.Enabled {
		var err error
		embeddingClient, err = service.NewAiFactoryEmbeddingClient(cfg.Embedding)
		if err != nil {
			slog.Warn("Failed to create embedding client, semantic search disabled", "error", err)
			embeddingClient = nil
		}
	}

	brainSvc := service.NewBrainService(&cfg, store, idx, nil, embeddingClient)
	if err := service.EnsureBuiltInFeatureCheckoutAutomation(ctx, brainSvc, service.BuiltInFeatureCheckoutConfig{
		Enabled:            cfg.FeatureCheckout.Enabled,
		Agent:              cfg.TaskDefaults.Agent,
		Model:              cfg.TaskDefaults.Model,
		Executor:           cfg.TaskDefaults.Executor,
		ExecutionMode:      cfg.TaskDefaults.ExecutionMode,
		TargetWorkdir:      cfg.TaskDefaults.TargetWorkdir,
		MergeTargetBranch:  cfg.TaskDefaults.MergeTargetBranch,
		MergePolicy:        cfg.TaskDefaults.MergePolicy,
		MergeStrategy:      cfg.TaskDefaults.MergeStrategy,
		RemoteBranchPolicy: cfg.TaskDefaults.RemoteBranchPolicy,
		OpenPRBeforeMerge:  cfg.TaskDefaults.OpenPRBeforeMerge,
	}); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to ensure built-in feature checkout automation: %w", err)
	}
	// Phase 3.4: register the parallel deterministic script-based
	// feature-checkout automation. Reuses the same enable toggle — users
	// who want only the AI path can archive the "simple" automation entry
	// via the existing UX. Both automations share the same feature.completed
	// trigger but are discriminated by their checkout_mode trigger filter
	// (ai vs simple), set from event metadata by CheckFeatureCompletion.
	if err := service.EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brainSvc, service.BuiltInFeatureCheckoutSimpleConfig{
		Enabled:            cfg.FeatureCheckout.Enabled,
		MergeTargetBranch:  cfg.TaskDefaults.MergeTargetBranch,
		RemoteBranchPolicy: cfg.TaskDefaults.RemoteBranchPolicy,
		TargetWorkdir:      cfg.TaskDefaults.TargetWorkdir,
	}); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to ensure built-in feature checkout simple automation: %w", err)
	}
	blobStore, err := blobstore.NewFilesystemStore(cfg.Attachments.StorageRoot, cfg.Attachments.MaxUploadSizeBytes)
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to initialize attachment blob store: %w", err)
	}
	attachmentExtractor := service.NewOpenRouterAttachmentExtractor(cfg.AttachmentExtraction)
	attachmentSvc := service.NewAttachmentService(
		store,
		blobStore,
		brainSvc,
		cfg.Attachments.MaxUploadSizeBytes,
		service.WithAttachmentMIMEPolicy(cfg.Attachments.AllowedMIMETypes, cfg.Attachments.BlockedMIMETypes),
		service.WithAttachmentExtractor(attachmentExtractor),
		service.WithAttachmentDerivedChangeHook(brainSvc),
	)
	taskSvc := service.NewTaskService(&cfg, store, idx)
	runnerSvc := service.NewRunnerServiceWithStorage(store)
	runnerRegistrySvc := service.NewRunnerRegistryService(store)
	clientContextSvc := service.NewClientContextService(store)
	placementSvc := service.NewProjectPlacementService(store)
	monitorSvc := service.NewMonitorService(brainSvc)
	webhookSvc := service.NewWebhookService(store)

	// ─── Realtime Hub ───────────────────────────────────────────────
	hub := realtime.NewHub()

	// ─── Background Claim Cleanup ──────────────────────────────────
	taskSvc.StartClaimCleanup(ctx, service.DefaultClaimCleanupInterval)

	// ─── Runner Lifecycle Management ───────────────────────────────
	runnerRegistrySvc.SetHub(hub)
	runnerRegistrySvc.StartLifecycleManager(ctx, service.DefaultLifecycleInterval)

	// ─── Scheduler Lifecycle ────────────────────────────────────────
	schedulerSvc := service.NewSchedulerService(taskSvc, runnerSvc, runnerRegistrySvc, placementSvc, store, hub)
	schedulerSvc.Start(ctx, service.DefaultSchedulerInterval)

	// ─── Event Hub & Services ──────────────────────────────────────
	eventHub := realtime.NewEventHub()
	eventSvc := service.NewEventService(eventHub)
	eventSvc.SetFeatureTaskLister(taskSvc)
	eventSvc.SetFeatureAssignmentCleaner(store)

	// ─── Feature Cascade ───────────────────────────────────────────
	// Manual "Run feature now" workflow needs the cascade to drain queued
	// tasks as in-flight ones complete — even while the project is paused.
	// Wire here so SchedulerService can register cascades from RunFeatureNow
	// and the cascade can call back via the FeatureRunner interface.
	featureCascade := service.NewFeatureCascadeService(eventHub, schedulerSvc)
	schedulerSvc.SetFeatureCascade(featureCascade)
	featureCascade.Start(ctx)
	automationSvc := service.NewAutomationService(brainSvc)
	automationSvc.SetPauseChecker(runnerSvc)
	// Without a project lister an automation scoped to all projects
	// (filter.project: "*") cannot fan out and falls back to a single
	// unscoped run — which is how the built-in Dream Consolidation spent
	// months writing one empty-project task a night into `default`.
	automationSvc.SetProjectLister(taskSvc)
	go automationSvc.Start(ctx, eventHub)

	// ─── Runner Bridge Hub (remote control) ────────────────────────
	// Created before the goal service so the goal steerer can reuse the same
	// in-process control plumbing (instance registry + bridge proxy).
	bridgeHub := bridge.NewHub(hub)

	// ─── Goal Reconcile Handler ────────────────────────────────────
	// GoalService subscribes to the EventHub and drives the deterministic
	// in-process reconcile for goal automations when their linked task/feature
	// lifecycle events fire (plus a periodic re-check ticker). The steerer
	// nudges live agent sessions toward the goal while work is in progress;
	// the pause checker suppresses generation/steering while automations are
	// paused, mirroring AutomationService.
	goalSvc := service.NewGoalService(brainSvc, taskSvc, store,
		service.WithGoalSteerer(newBridgeGoalSteerer(runnerRegistrySvc, bridgeHub)),
		service.WithGoalPauseChecker(runnerSvc),
	)
	assistantSvc := api.NewAssistantService(api.AssistantServiceOptions{
		Enabled:   cfg.Assistant.Enabled,
		Provider:  cfg.Assistant.Provider,
		BaseURL:   cfg.Assistant.BaseURL,
		APIKeyEnv: cfg.Assistant.APIKeyEnv,
		Model:     cfg.Assistant.Model,
		Timeout:   time.Duration(cfg.Assistant.TimeoutMs) * time.Millisecond,
		Brain:     brainSvc,
		Goals:     goalSvc,
		Tasks:     taskSvc,
		Runner:    runnerSvc,
		Runners:   runnerRegistrySvc,
		Events:    eventSvc,
	})
	go goalSvc.Start(ctx, eventHub)

	// ─── Reminders ─────────────────────────────────────────────────
	// The sweeper runs HERE, in the API process, not in the runner. A
	// notify-action reminder has nothing to do with runners and must not go
	// undelivered because none happens to be polling; the runner's existing
	// run_once_at path is additionally gated by the project and feature pause
	// dials, which have no business suppressing a notification.
	reminderSvc := service.NewReminderService(brainSvc, store,
		service.WithReminderEventIngester(eventSvc),
		service.WithReminderPauseChecker(runnerSvc),
	)
	go reminderSvc.Start(ctx)

	// ─── Webhook Dispatcher ────────────────────────────────────────
	// Subscribe to all EventHub events and deliver to matching webhooks.
	webhookDispatcher := realtime.NewWebhookDispatcher(eventHub, webhookSvc)
	go webhookDispatcher.Start(ctx)

	// ─── Trigger Dispatcher ────────────────────────────────────────
	// Subscribe to all EventHub events and evaluate task triggers.
	// When events match a task's trigger config, the task is activated (set to pending).
	triggerStore := service.NewTriggerTaskStoreAdapter(store)
	triggerSvc := service.NewTriggerService(triggerStore)
	triggerDispatcher := realtime.NewTriggerDispatcher(eventHub, triggerSvc)
	go triggerDispatcher.Start(ctx)

	// ─── Log Buffer ─────────────────────────────────────────────────
	logBuf := logbuffer.New(logbuffer.DefaultMaxLines)

	// ─── API Handler & Router ───────────────────────────────────────
	// Shared operator credential verifier (password login + OAuth consent).
	credVerifier := auth.NewVerifierFromEnv()

	handler := api.NewHandler(
		brainSvc,
		api.WithAttachmentService(attachmentSvc),
		api.WithTaskService(taskSvc),
		api.WithRunnerService(runnerSvc),
		api.WithRunnerRegistryService(runnerRegistrySvc),
		api.WithClientContextService(clientContextSvc),
		api.WithProjectPlacementService(placementSvc),
		api.WithSchedulerService(schedulerSvc),
		api.WithSchedulerVisibilityService(store),
		api.WithRunTaskService(schedulerSvc),
		api.WithRunFeatureService(schedulerSvc),
		api.WithDependentChainService(schedulerSvc),
		api.WithRunProjectService(schedulerSvc),
		api.WithMonitorService(monitorSvc),
		api.WithTokenService(store),
		api.WithHub(hub),
		api.WithEventService(eventSvc),
		api.WithWebhookService(webhookSvc),
		api.WithGoalService(goalSvc),
		api.WithReminderService(reminderSvc),
		api.WithAutomationRunService(automationSvc),
		api.WithAssistantService(assistantSvc),
		api.WithBridgeService(bridgeHub),
		api.WithLogBuffer(logBuf),
		api.WithTaskDefaults(cfg.TaskDefaults),
		api.WithCredentialVerifier(credVerifier),
		api.WithPasswordTokenStore(store),
	)

	// ─── Rate Limiting ─────────────────────────────────────────────
	var rateLimiter *api.RateLimiter
	if cfg.RateLimitPerMinute > 0 {
		burst := cfg.RateLimitBurst
		if burst <= 0 {
			burst = cfg.RateLimitPerMinute
		}
		rateLimiter = api.NewRateLimiter(api.RateLimitConfig{
			RequestsPerMinute: cfg.RateLimitPerMinute,
			BurstSize:         burst,
			CleanupInterval:   5 * time.Minute,
		})
		defer rateLimiter.Stop()
		slog.Info("rate limiting enabled",
			"requests_per_minute", cfg.RateLimitPerMinute,
			"burst", burst,
		)
	}

	routerOpts := []api.RouterOption{
		api.WithHandler(handler),
		api.WithDualAuth(store, store),
		api.WithEmbeddingReady(!cfg.Embedding.Enabled || embeddingClient != nil),
		api.WithConfigHandler(api.NewConfigHandler("", newHotReloader())),
	}
	if rateLimiter != nil {
		routerOpts = append(routerOpts, api.WithRateLimiter(rateLimiter))
	}
	router := api.NewRouter(cfg, routerOpts...)

	// ─── OAuth ─────────────────────────────────────────────────────
	// Persist OAuth flow state (clients, auth codes, refresh tokens) in SQLite
	// so registered clients survive restarts — otherwise every restart yields
	// "unknown client_id" for the Claude connector and the PWA.
	oauthStore := oauth.NewPersistentStore(store)
	oauthHandler := oauth.NewHandler(oauthStore,
		oauth.WithAccessTokenStore(store),
		oauth.WithCredentialVerifier(credVerifier),
	)
	oauth.RegisterRoutes(router, oauthHandler)

	// ─── MCP Streamable HTTP Transport ──────────────────────────────
	// The in-process MCP handler makes API calls back to this same server
	// (e.g. brain_stats -> GET /api/v1/entries). When TLS is on those
	// callbacks must use https://; otherwise the plain-HTTP request hits
	// the TLS listener and returns 400 Bad Request. The self-signed cert
	// is already installed into http.DefaultTransport's root pool by
	// trustSelfSignedCertForLoopback earlier in RunServer, so the client
	// validates cleanly without InsecureSkipVerify.
	mcpScheme := "http"
	if opts.TLSCert != "" && opts.TLSKey != "" {
		mcpScheme = "https"
	}
	mcpClient := mcppkg.NewAPIClient(fmt.Sprintf("%s://localhost:%d", mcpScheme, opts.Port))
	mcpHTTP := mcppkg.NewHTTPHandler(mcpClient)
	authValidator := &api.CompositeValidator{
		APIValidator:   store,
		OAuthValidator: store,
	}
	router.Route("/mcp", func(r chi.Router) {
		r.Use(api.Auth(opts.EnableAuth, authValidator, opts.JWTSecret))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Get("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})
	// MCP at root / (for clients that use the base URL as the MCP endpoint).
	// Note: GET / is intentionally omitted here — the embedded web UI (see
	// webui.Handler below) owns browser navigations to "/", while MCP clients
	// use POST/DELETE at the root. Clients that need a GET stream use /mcp.
	router.Group(func(r chi.Router) {
		r.Use(api.Auth(opts.EnableAuth, authValidator, opts.JWTSecret))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})

	// Wrap the API router with the embedded Brain PWA. The webui handler serves
	// the SPA + static assets for browser navigations and delegates all API,
	// OAuth, MCP, and well-known routes back to the router untouched.
	return webui.Handler(router), dbPath, cleanup, nil
}
