package lifecycle

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// SignalHandlerOptions configures the signal handler behavior.
type SignalHandlerOptions struct {
	// GracefulTimeout is the maximum time to wait for graceful shutdown.
	// Default: 30 seconds.
	GracefulTimeout time.Duration

	// ForceKillTimeout is the time to wait after force kill before exiting.
	// Default: 5 seconds.
	ForceKillTimeout time.Duration

	// OnShutdown is called when a shutdown signal is received (SIGTERM, SIGINT).
	OnShutdown func()

	// OnReload is called when SIGHUP is received (config reload).
	OnReload func()

	// Logger is an optional logger. If nil, the default logger is used.
	Logger *log.Logger
}

// SignalHandler manages OS signal handling for the server.
type SignalHandler struct {
	opts         SignalHandlerOptions
	shuttingDown int32 // atomic flag
	signalCh     chan os.Signal
	logger       *log.Logger
}

// SetupSignalHandler registers signal handlers and starts a goroutine
// to process incoming signals. Returns the handler for status queries.
func SetupSignalHandler(ctx context.Context, opts SignalHandlerOptions) *SignalHandler {
	// Apply defaults
	if opts.GracefulTimeout == 0 {
		opts.GracefulTimeout = 30 * time.Second
	}
	if opts.ForceKillTimeout == 0 {
		opts.ForceKillTimeout = 5 * time.Second
	}

	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	handler := &SignalHandler{
		opts:     opts,
		signalCh: make(chan os.Signal, 2),
		logger:   logger,
	}

	// Register for OS signals
	signal.Notify(handler.signalCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	// Start signal processing goroutine
	go handler.run(ctx)

	return handler
}

// IsShuttingDown returns true if a shutdown has been initiated.
func (h *SignalHandler) IsShuttingDown() bool {
	return atomic.LoadInt32(&h.shuttingDown) == 1
}

// SendSignal sends a signal to the handler for testing purposes.
// In production, signals come from the OS.
func (h *SignalHandler) SendSignal(sig os.Signal) {
	h.signalCh <- sig
}

// run processes signals in a loop until the context is cancelled.
func (h *SignalHandler) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			signal.Stop(h.signalCh)
			return
		case sig := <-h.signalCh:
			h.handleSignal(sig)
		}
	}
}

// handleSignal dispatches a signal to the appropriate handler.
func (h *SignalHandler) handleSignal(sig os.Signal) {
	switch sig {
	case syscall.SIGHUP:
		h.logger.Printf("received SIGHUP, reloading configuration")
		if h.opts.OnReload != nil {
			h.opts.OnReload()
		}
	case syscall.SIGTERM, syscall.SIGINT:
		// Only handle shutdown once
		if !atomic.CompareAndSwapInt32(&h.shuttingDown, 0, 1) {
			h.logger.Printf("received %v during shutdown, ignoring", sig)
			return
		}

		h.logger.Printf("received %v, initiating graceful shutdown", sig)

		// Unregister signal handler to allow force-kill on second signal
		signal.Stop(h.signalCh)

		// Call shutdown callback
		if h.opts.OnShutdown != nil {
			h.opts.OnShutdown()
		}
	}
}
