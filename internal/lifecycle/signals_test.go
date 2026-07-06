package lifecycle

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// discardLogger keeps signal-handler chatter ("received SIGHUP, reloading
// configuration", ...) out of the test output, where it reads as if real OS
// signals were being delivered during other tests.
var discardLogger = log.New(io.Discard, "", 0)

// =============================================================================
// Signal Handler Setup
// =============================================================================

func TestSetupSignalHandler_DefaultOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{Logger: discardLogger}
	handler := SetupSignalHandler(ctx, opts)

	if handler == nil {
		t.Fatal("SetupSignalHandler returned nil")
	}

	// Should not be shutting down initially
	if handler.IsShuttingDown() {
		t.Error("Handler should not be shutting down initially")
	}

	// Cancel context to clean up
	cancel()
	time.Sleep(10 * time.Millisecond) // Give goroutine time to exit
}

func TestSetupSignalHandler_WithCallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCalled := false
	reloadCalled := false

	opts := SignalHandlerOptions{
		Logger: discardLogger,
		OnShutdown: func() {
			shutdownCalled = true
		},
		OnReload: func() {
			reloadCalled = true
		},
	}

	handler := SetupSignalHandler(ctx, opts)
	if handler == nil {
		t.Fatal("SetupSignalHandler returned nil")
	}

	// Verify callbacks are registered but not called yet
	if shutdownCalled {
		t.Error("OnShutdown should not be called during setup")
	}
	if reloadCalled {
		t.Error("OnReload should not be called during setup")
	}

	cancel()
	time.Sleep(10 * time.Millisecond)
}

// =============================================================================
// Signal Handling
// =============================================================================

func TestSignalHandler_SIGTERM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCalled := make(chan struct{})
	opts := SignalHandlerOptions{
		Logger: discardLogger,
		OnShutdown: func() {
			close(shutdownCalled)
		},
		GracefulTimeout: 1 * time.Second,
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGTERM (simulate kill command)
	handler.SendSignal(syscall.SIGTERM)

	// Wait for the handler goroutine to process the signal; under full-suite
	// load it may not be scheduled for a while.
	select {
	case <-shutdownCalled:
	case <-time.After(spawnWait):
		t.Fatal("OnShutdown callback should have been called")
	}

	// The shutdown flag is set before OnShutdown runs.
	if !handler.IsShuttingDown() {
		t.Error("Handler should be shutting down after SIGTERM")
	}
}

func TestSignalHandler_SIGINT(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCalled := make(chan struct{})
	opts := SignalHandlerOptions{
		Logger: discardLogger,
		OnShutdown: func() {
			close(shutdownCalled)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGINT (simulate Ctrl+C)
	handler.SendSignal(syscall.SIGINT)

	select {
	case <-shutdownCalled:
	case <-time.After(spawnWait):
		t.Fatal("OnShutdown callback should have been called")
	}

	if !handler.IsShuttingDown() {
		t.Error("Handler should be shutting down after SIGINT")
	}
}

func TestSignalHandler_SIGHUP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reloadCalled := make(chan struct{})
	opts := SignalHandlerOptions{
		Logger: discardLogger,
		OnReload: func() {
			close(reloadCalled)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGHUP (reload signal)
	handler.SendSignal(syscall.SIGHUP)

	select {
	case <-reloadCalled:
	case <-time.After(spawnWait):
		t.Fatal("OnReload callback should have been called")
	}

	// SIGHUP should not trigger shutdown
	if handler.IsShuttingDown() {
		t.Error("Handler should not be shutting down after SIGHUP")
	}
}

func TestSignalHandler_MultipleSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var shutdownCallCount atomic.Int32
	opts := SignalHandlerOptions{
		Logger: discardLogger,
		OnShutdown: func() {
			shutdownCallCount.Add(1)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGTERM twice
	handler.SendSignal(syscall.SIGTERM)
	if !waitFor(t, spawnWait, func() bool { return shutdownCallCount.Load() == 1 }) {
		t.Fatal("OnShutdown should have been called for the first SIGTERM")
	}
	handler.SendSignal(syscall.SIGTERM)

	// Wait for the run loop to consume the duplicate signal. The CAS guard in
	// handleSignal makes a second OnShutdown call impossible, so once the
	// channel is drained the count is final.
	waitFor(t, spawnWait, func() bool { return len(handler.signalCh) == 0 })
	if got := shutdownCallCount.Load(); got != 1 {
		t.Errorf("OnShutdown called %d times, want 1", got)
	}
}

func TestSignalHandler_IsShuttingDown_Atomic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := SetupSignalHandler(ctx, SignalHandlerOptions{Logger: discardLogger})

	// Check shutdown status from multiple goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_ = handler.IsShuttingDown()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or race
	if handler.IsShuttingDown() {
		t.Error("Should not be shutting down")
	}

	cancel()
}

// =============================================================================
// Timeout Configuration
// =============================================================================

func TestSignalHandler_CustomTimeouts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		Logger: discardLogger,
		GracefulTimeout:  5 * time.Second,
		ForceKillTimeout: 2 * time.Second,
	}

	handler := SetupSignalHandler(ctx, opts)
	if handler == nil {
		t.Fatal("SetupSignalHandler returned nil")
	}

	// Verify handler was created successfully with custom timeouts
	// (timeouts will be tested in integration tests)

	cancel()
	time.Sleep(10 * time.Millisecond)
}
