package runner

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

// =============================================================================
// Stalled-reconnect warnings
//
// Both SSE listeners logged reconnect activity at Debug only, so a stream that
// died and never came back produced no output at the default level. A runner
// sat deaf to every pushed dispatch for 17 hours on 2026-09-03 without one
// line above Debug. These pin the escalation.
// =============================================================================

// captureWarnAttempts records which attempt numbers produce a Warn.
func captureWarnAttempts(t *testing.T, attempts int) []int {
	t.Helper()

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var warned []int
	current := 0
	slog.SetDefault(slog.New(&capturingHandler{
		onRecord: func(r slog.Record) {
			if r.Level == slog.LevelWarn {
				warned = append(warned, current)
			}
		},
	}))

	for i := 1; i <= attempts; i++ {
		current = i
		warnStalledReconnect("runner", i, time.Second, "runner_id", "runner_x")
	}
	return warned
}

func TestWarnStalledReconnect_EscalatesAtThresholdThenPeriodically(t *testing.T) {
	got := captureWarnAttempts(t, 25)

	// Threshold is 3, then every 10th attempt after it.
	want := []int{3, 13, 23}
	if len(got) != len(want) {
		t.Fatalf("warned on attempts %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("warned on attempts %v, want %v", got, want)
		}
	}
}

func TestWarnStalledReconnect_SilentBelowThreshold(t *testing.T) {
	if got := captureWarnAttempts(t, reconnectWarnAfter-1); len(got) != 0 {
		t.Fatalf("warned on attempts %v; a stream that reconnects promptly must stay quiet", got)
	}
}

// capturingHandler is a minimal slog.Handler that reports every record.
type capturingHandler struct {
	onRecord func(slog.Record)
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.onRecord(r)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *capturingHandler) WithGroup(string) slog.Handler { return h }

// =============================================================================
// Re-registration after the API forgets the runner
//
// `kill <pid> && brain runner start` races: && fires when the signal is
// delivered, not when the old process exits, so the predecessor's deregister
// can land after the successor registers — and both resolve the same persisted
// runner id. The successor was then invisible to the API forever.
// =============================================================================

func newRecoveryRunner(t *testing.T) (*TaskRunner, *mockClient) {
	t.Helper()
	client := newMockClient()
	return newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr()), client
}

func registerCount(c *mockClient) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.registerCalls)
}

func TestReregisterIfUnknown_RegistersOn404(t *testing.T) {
	tr, client := newRecoveryRunner(t)

	tr.reregisterIfUnknown(context.Background(), &APIError{
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"Not Found","message":"runner \"runner_x\" not found"}`,
	})

	if got := registerCount(client); got != 1 {
		t.Fatalf("register calls = %d, want 1: a 404 means the registry row is gone", got)
	}
}

func TestReregisterIfUnknown_IgnoresOtherFailures(t *testing.T) {
	tr, client := newRecoveryRunner(t)
	ctx := context.Background()

	// A 500 is not proof the row is missing, and neither is a transport error.
	tr.reregisterIfUnknown(ctx, &APIError{StatusCode: http.StatusInternalServerError, Body: "boom"})
	tr.reregisterIfUnknown(ctx, context.DeadlineExceeded)

	if got := registerCount(client); got != 0 {
		t.Fatalf("register calls = %d, want 0: only a 404 is authoritative", got)
	}
}

func TestReregisterIfUnknown_CooldownBoundsRetries(t *testing.T) {
	tr, client := newRecoveryRunner(t)
	ctx := context.Background()
	notFound := &APIError{StatusCode: http.StatusNotFound, Body: "not found"}

	for i := 0; i < 5; i++ {
		tr.reregisterIfUnknown(ctx, notFound)
	}

	if got := registerCount(client); got != 1 {
		t.Fatalf("register calls = %d, want 1: the cooldown must keep a failing endpoint on a leash", got)
	}

	// Past the cooldown it tries again.
	tr.mu.Lock()
	tr.lastReregisterAt = time.Now().Add(-2 * reregisterCooldown)
	tr.mu.Unlock()

	tr.reregisterIfUnknown(ctx, notFound)
	if got := registerCount(client); got != 2 {
		t.Fatalf("register calls = %d, want 2 after the cooldown elapsed", got)
	}
}
