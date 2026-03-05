package runner

import (
	"context"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// SignalHandler Tests
// =============================================================================

func TestSetupSignalHandler_CallsOnShutdown(t *testing.T) {
	var called int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		GracefulTimeout:  1 * time.Second,
		ForceKillTimeout: 500 * time.Millisecond,
		OnShutdown: func() {
			atomic.StoreInt32(&called, 1)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Simulate SIGTERM
	handler.signalCh <- syscall.SIGTERM

	// Wait for handler to process
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&called) != 1 {
		t.Error("OnShutdown should have been called on SIGTERM")
	}
}

func TestSetupSignalHandler_SIGINT_CallsOnShutdown(t *testing.T) {
	var called int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		GracefulTimeout:  1 * time.Second,
		ForceKillTimeout: 500 * time.Millisecond,
		OnShutdown: func() {
			atomic.StoreInt32(&called, 1)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Simulate SIGINT
	handler.signalCh <- syscall.SIGINT

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&called) != 1 {
		t.Error("OnShutdown should have been called on SIGINT")
	}
}

func TestSetupSignalHandler_SIGHUP_CallsOnReload(t *testing.T) {
	var reloadCalled int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		GracefulTimeout:  1 * time.Second,
		ForceKillTimeout: 500 * time.Millisecond,
		OnShutdown:       func() {},
		OnReload: func() {
			atomic.StoreInt32(&reloadCalled, 1)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Simulate SIGHUP
	handler.signalCh <- syscall.SIGHUP

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&reloadCalled) != 1 {
		t.Error("OnReload should have been called on SIGHUP")
	}
}

func TestSetupSignalHandler_DefaultTimeouts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		OnShutdown: func() {},
	}

	handler := SetupSignalHandler(ctx, opts)

	if handler.opts.GracefulTimeout != 30*time.Second {
		t.Errorf("default GracefulTimeout = %v, want 30s", handler.opts.GracefulTimeout)
	}
	if handler.opts.ForceKillTimeout != 5*time.Second {
		t.Errorf("default ForceKillTimeout = %v, want 5s", handler.opts.ForceKillTimeout)
	}
}

func TestSetupSignalHandler_ShutdownOnlyCalledOnce(t *testing.T) {
	var callCount int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		GracefulTimeout:  1 * time.Second,
		ForceKillTimeout: 500 * time.Millisecond,
		OnShutdown: func() {
			atomic.AddInt32(&callCount, 1)
		},
	}

	handler := SetupSignalHandler(ctx, opts)

	// Send multiple signals
	handler.signalCh <- syscall.SIGTERM
	time.Sleep(50 * time.Millisecond)
	handler.signalCh <- syscall.SIGTERM
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("OnShutdown called %d times, want 1", atomic.LoadInt32(&callCount))
	}
}

func TestSetupSignalHandler_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	opts := SignalHandlerOptions{
		GracefulTimeout:  1 * time.Second,
		ForceKillTimeout: 500 * time.Millisecond,
		OnShutdown:       func() {},
	}

	_ = SetupSignalHandler(ctx, opts)

	// Cancel context — handler goroutine should exit cleanly
	cancel()
	time.Sleep(50 * time.Millisecond)
	// No deadlock or panic = pass
}

func TestSignalHandler_IsShuttingDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := SignalHandlerOptions{
		GracefulTimeout:  1 * time.Second,
		ForceKillTimeout: 500 * time.Millisecond,
		OnShutdown:       func() {},
	}

	handler := SetupSignalHandler(ctx, opts)

	if handler.IsShuttingDown() {
		t.Error("should not be shutting down initially")
	}

	handler.signalCh <- syscall.SIGTERM
	time.Sleep(100 * time.Millisecond)

	if !handler.IsShuttingDown() {
		t.Error("should be shutting down after SIGTERM")
	}
}
