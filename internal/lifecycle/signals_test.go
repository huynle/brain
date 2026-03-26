package lifecycle

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// Signal Handler Setup
// =============================================================================

func TestSetupSignalHandler_DefaultOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{}
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

	shutdownCalled := false
	opts := SignalHandlerOptions{
		OnShutdown: func() {
			shutdownCalled = true
		},
		GracefulTimeout: 1 * time.Second,
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGTERM (simulate kill command)
	handler.SendSignal(syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond) // Wait for signal to be processed

	// Verify shutdown was initiated
	if !handler.IsShuttingDown() {
		t.Error("Handler should be shutting down after SIGTERM")
	}

	if !shutdownCalled {
		t.Error("OnShutdown callback should have been called")
	}

	cancel()
}

func TestSignalHandler_SIGINT(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCalled := false
	opts := SignalHandlerOptions{
		OnShutdown: func() {
			shutdownCalled = true
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGINT (simulate Ctrl+C)
	handler.SendSignal(syscall.SIGINT)
	time.Sleep(50 * time.Millisecond)

	if !handler.IsShuttingDown() {
		t.Error("Handler should be shutting down after SIGINT")
	}

	if !shutdownCalled {
		t.Error("OnShutdown callback should have been called")
	}

	cancel()
}

func TestSignalHandler_SIGHUP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reloadCalled := false
	opts := SignalHandlerOptions{
		OnReload: func() {
			reloadCalled = true
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGHUP (reload signal)
	handler.SendSignal(syscall.SIGHUP)
	time.Sleep(50 * time.Millisecond)

	// SIGHUP should not trigger shutdown
	if handler.IsShuttingDown() {
		t.Error("Handler should not be shutting down after SIGHUP")
	}

	if !reloadCalled {
		t.Error("OnReload callback should have been called")
	}

	cancel()
}

func TestSignalHandler_MultipleSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCallCount := 0
	opts := SignalHandlerOptions{
		OnShutdown: func() {
			shutdownCallCount++
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send SIGTERM twice
	handler.SendSignal(syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)
	handler.SendSignal(syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)

	// Shutdown callback should only be called once
	if shutdownCallCount != 1 {
		t.Errorf("OnShutdown called %d times, want 1", shutdownCallCount)
	}

	cancel()
}

func TestSignalHandler_IsShuttingDown_Atomic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := SetupSignalHandler(ctx, SignalHandlerOptions{})

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
